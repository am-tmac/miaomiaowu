package notify

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"miaomiaowu/internal/logger"
)

// Failure cases: Markdown parsing, sensitive query parameters, transport URL
// disclosure, API ok=false (including HTTP 200), malformed JSON, silent errors.
func TestTelegramHTMLAndBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.RawQuery != "" {
			t.Errorf("expected POST without query, got %s %s", r.Method, r.URL.RawQuery)
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if got := r.Form.Get("parse_mode"); got != "HTML" {
			t.Errorf("parse_mode = %q, want HTML", got)
		}
		if got := r.Form.Get("text"); got != "<b>a_b*c[d]&lt;x&gt;&amp;</b>\nbody" {
			t.Errorf("text = %q", got)
		}
		io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()
	old := httpClient
	httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r.URL.Scheme = "http"
		r.URL.Host = strings.TrimPrefix(srv.URL, "http://")
		return http.DefaultTransport.RoundTrip(r)
	})}
	defer func() { httpClient = old }()
	n := New(Config{Enabled: true, BotToken: "secret-token", ChatID: "123", NotifyLogin: true})
	if err := n.Send(context.Background(), Event{Type: EventLogin, Title: "a_b*c[d]<x>&", Message: "body"}); err != nil {
		t.Fatal(err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTelegramTransportRedactionAndWarning(t *testing.T) {
	old := httpClient
	httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("connection failed: " + r.URL.String())
	})}
	defer func() { httpClient = old }()
	var logs bytes.Buffer
	l := logger.GetLogger()
	oldLogger := l.Logger
	l.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	defer func() { l.Logger = oldLogger }()
	n := New(Config{Enabled: true, BotToken: "secret-token", ChatID: "123", NotifyLogin: true})
	err := n.Send(context.Background(), Event{Type: EventLogin, Title: "login"})
	if err == nil {
		t.Fatal("expected transport error")
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Errorf("token leaked: %s", err)
	}
	if !strings.Contains(logs.String(), "WARN") || !strings.Contains(logs.String(), "login") {
		t.Errorf("missing event warning: %q", logs.String())
	}
	if strings.Contains(logs.String(), "secret-token") {
		t.Errorf("token leaked in log")
	}
}

func TestTelegramAPIResponse(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		wantErr    bool
	}{
		{"success", `{"ok":true}`, 200, false},
		{"rejected200", `{"ok":false,"description":"bad request"}`, 200, true},
		{"rejected400", `{"ok":false}`, 400, true},
		{"malformed", `not json`, 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := httpClient
			httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})}
			defer func() { httpClient = old }()
			err := sendTelegram(context.Background(), "secret-token", "123", "hello")
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

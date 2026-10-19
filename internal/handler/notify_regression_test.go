package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"miaomiaowu/internal/auth"
	"miaomiaowu/internal/notify"
	"miaomiaowu/internal/storage"
)

type notifyTransport func(*http.Request) (*http.Response, error)

func (f notifyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Failure cases: request cancellation kills login notification; no per-send
// deadline; user-controlled HTML/Markdown corrupts the message.
func TestLoginNotificationSurvivesRequestCancellation(t *testing.T) {
	oldN, oldTransport := globalNotifier, http.DefaultTransport
	defer func() { globalNotifier = oldN; http.DefaultTransport = oldTransport }()
	InitNotifier(notify.Config{Enabled: true, BotToken: "test-token", ChatID: "123", NotifyLogin: true})
	released := make(chan struct{})
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("parse_mode") != "HTML" || !strings.Contains(r.Form.Get("text"), "<code>a_b*c[d]&lt;x&gt;&amp;</code>") {
			t.Errorf("unsafe login form: %v", r.Form)
		}
		io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()
	http.DefaultTransport = notifyTransport(func(r *http.Request) (*http.Response, error) {
		defer close(done)
		<-released
		if err := r.Context().Err(); err != nil {
			t.Errorf("notification canceled with request: %v", err)
			return nil, err
		}
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 15*time.Second {
			t.Error("missing bounded send context")
		}
		r.URL.Scheme = "http"
		r.URL.Host = strings.TrimPrefix(srv.URL, "http://")
		return oldTransport.RoundTrip(r)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/login", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	issueLoginSession(w, req, auth.NewTokenStore(time.Hour), nil, storage.User{Username: "a_b*c[d]<x>&"}, false)
	if w.Code != 200 {
		t.Fatalf("login status %d", w.Code)
	}
	cancel()
	close(released)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("notification did not complete")
	}
}

// Failure cases: invalid hours/minutes, missing zero padding, trailing data.
// Empty values retain the existing setting for compatibility.
func TestNotifyConfigValidatesDailyTime(t *testing.T) {
	repo, err := storage.NewTrafficRepository(filepath.Join(t.TempDir(), "notify.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	for _, value := range []string{"24:00", "08:60", "8:00", "08:0", "garbage", "08:00:00"} {
		t.Run(value, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPut, "/notify", strings.NewReader(`{"notify_daily_traffic_time":"`+value+`"}`))
			w := httptest.NewRecorder()
			handleUpdateNotifyConfig(w, r, repo)
			if w.Code != 400 {
				t.Errorf("status = %d, want 400", w.Code)
			}
		})
	}
	for _, value := range []string{"00:00", "08:00", "23:59", ""} {
		r := httptest.NewRequest(http.MethodPut, "/notify", strings.NewReader(`{"notify_daily_traffic_time":"`+value+`"}`))
		w := httptest.NewRecorder()
		handleUpdateNotifyConfig(w, r, repo)
		if w.Code != 200 {
			t.Errorf("valid %q: status %d", value, w.Code)
		}
	}
}

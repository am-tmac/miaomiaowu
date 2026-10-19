package handler

import (
	"net/http"
	"strings"
	"testing"
)

type geoIPRoundTripFunc func(*http.Request) (*http.Response, error)

func (f geoIPRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGeoIPDisabledWithoutConfiguredToken(t *testing.T) {
	t.Setenv("IPINFO_TOKEN", "")
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	calls := 0
	http.DefaultTransport = geoIPRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
	})
	if got := getGeoIPCountryCode("192.0.2.201"); got != "" {
		t.Fatalf("disabled lookup returned %q", got)
	}
	if calls != 0 {
		t.Fatalf("disabled GeoIP sent %d external requests", calls)
	}
}

func TestGeoIPUsesConfiguredCredential(t *testing.T) {
	t.Setenv("IPINFO_TOKEN", "test & token")
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	calls := 0
	http.DefaultTransport = geoIPRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Query().Get("token") != "test & token" {
			t.Errorf("configured credential not properly encoded")
		}
		return &http.Response{StatusCode: 200, Body: ioNopCloser("{\"country_code\":\"jp\"}"), Header: make(http.Header)}, nil
	})
	if got := getGeoIPCountryCode("192.0.2.202"); got != "JP" {
		t.Fatalf("country = %q", got)
	}
	if calls != 1 {
		t.Fatalf("requests = %d", calls)
	}
}

type geoIPReadCloser struct{ *strings.Reader }

func (geoIPReadCloser) Close() error       { return nil }
func ioNopCloser(s string) geoIPReadCloser { return geoIPReadCloser{strings.NewReader(s)} }

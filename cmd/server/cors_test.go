package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCORSDefaultDoesNotGrantCrossOriginAccess(t *testing.T) {
	for _, raw := range []string{"", "   ", " , , "} {
		t.Run("config="+raw, func(t *testing.T) {
			origins := parseAllowedOrigins(raw)
			assertNoCORSGrant(t, origins)
		})
	}
	t.Run("nil origins", func(t *testing.T) { assertNoCORSGrant(t, nil) })
	t.Run("empty origins", func(t *testing.T) { assertNoCORSGrant(t, []string{}) })
}

func TestCORSExplicitOriginsAndSameOrigin(t *testing.T) {
	for _, tc := range []struct{ name, config, origin, want string }{
		{"explicit wildcard", " * ", "https://other.example", "*"},
		{"allowed origin", "https://one.example, https://two.example", "https://two.example", "https://two.example"},
		{"disallowed origin", "https://one.example", "https://other.example", ""},
		{"not a prefix match", "https://one.example", "https://one.example.attacker.test", ""},
		{"same origin", "", "https://panel.example", ""},
		{"no Origin", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range []string{http.MethodGet, http.MethodOptions} {
				called := false
				h := withCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; _, _ = w.Write([]byte("ok")) }), parseAllowedOrigins(tc.config))
				req := httptest.NewRequest(method, "https://panel.example/api/user/profile", nil)
				req.Header.Set("Origin", tc.origin)
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if got := rec.Header().Get("Access-Control-Allow-Origin"); got != tc.want {
					t.Errorf("%s origin=%q want=%q", method, got, tc.want)
				}
				if tc.want != "" && !strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "MM-Authorization") {
					t.Error("configured origin cannot use session header")
				}
				if tc.want != "" && tc.want != "*" && !strings.Contains(rec.Header().Get("Vary"), "Origin") {
					t.Error("allowlist response missing Vary: Origin")
				}
				if method == http.MethodGet && (!called || rec.Body.String() != "ok") {
					t.Error("normal request did not reach handler")
				}
				if method == http.MethodOptions && (called || rec.Code != http.StatusNoContent) {
					t.Error("preflight should end at CORS middleware")
				}
			}
		})
	}
}

func TestGetAllowedOriginsEnvironment(t *testing.T) {
	for _, tc := range []struct{ name, primary, fallback, want string }{
		{"secure default", "", "", ""},
		{"fallback", "", "https://fallback.example", "https://fallback.example"},
		{"primary wins", "https://primary.example", "https://fallback.example", "https://primary.example"},
		{"explicit wildcard", "*", "", "*"},
		{"blank primary fails closed", " , ", "*", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ALLOWED_ORIGINS", tc.primary)
			t.Setenv("FRONTEND_ORIGIN", tc.fallback)
			if got := strings.Join(getAllowedOrigins(), ","); got != tc.want {
				t.Errorf("origins=%q want=%q", got, tc.want)
			}
		})
	}
}

func assertNoCORSGrant(t *testing.T, origins []string) {
	t.Helper()
	for _, method := range []string{http.MethodGet, http.MethodOptions} {
		called := false
		handler := withCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(http.StatusOK) }), origins)
		req := httptest.NewRequest(method, "https://panel.example/api/user/profile", nil)
		req.Header.Set("Origin", "https://attacker.example")
		req.Header.Set("Access-Control-Request-Headers", "MM-Authorization")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("%s default grants origin %q", method, got)
		}
		if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "" {
			t.Errorf("%s default grants headers %q", method, got)
		}
		if method == http.MethodGet && !called {
			t.Error("CORS must not block normal server-side handling")
		}
	}
}

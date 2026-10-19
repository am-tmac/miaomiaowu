package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequireTokenRejectsURLSessionToken(t *testing.T) {
	store := NewTokenStore(time.Hour)
	token, _, err := store.Issue("admin")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/user/profile", "/api/admin/update/apply-sse"} {
		t.Run(path, func(t *testing.T) {
			called := false
			handler := RequireToken(store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
			req := httptest.NewRequest(http.MethodGet, path+"?token="+token, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized || called {
				t.Fatalf("URL session authorized: status=%d handlerCalled=%v", rec.Code, called)
			}
		})
	}
}

func TestRequireTokenHeaderSupportsSSE(t *testing.T) {
	store := NewTokenStore(time.Hour)
	token, _, err := store.Issue("admin")
	if err != nil {
		t.Fatal(err)
	}
	handler := RequireToken(store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if username := UsernameFromContext(r.Context()); username != "admin" {
			t.Errorf("username = %q", username)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"step\":\"done\"}\n\n"))
		w.(http.Flusher).Flush()
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/admin/update/apply-sse?token=unrelated-subscription-token", nil)
	req.Header.Set(AuthHeader, token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !rec.Flushed || rec.Body.String() != "data: {\"step\":\"done\"}\n\n" {
		t.Fatalf("header SSE failed: status=%d flushed=%v body=%q", rec.Code, rec.Flushed, rec.Body.String())
	}
}

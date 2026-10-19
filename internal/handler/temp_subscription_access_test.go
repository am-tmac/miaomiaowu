package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestTemporarySubscriptionCreateThenAccess(t *testing.T) {
	originalStore := tempSubStore
	tempSubStore = &TempSubscriptionStore{subscriptions: make(map[string]*TempSubscription)}
	t.Cleanup(func() { tempSubStore = originalStore })

	for _, userAgent := range []string{"Mihomo/1.19", "ClashMetaForAndroid/2.11"} {
		t.Run(userAgent, func(t *testing.T) {
			create := httptest.NewRequest(http.MethodPost, "/api/temp-subscription", strings.NewReader(`{"proxies":[{"name":"regression-proxy","type":"ss","server":"example.com","port":443,"cipher":"aes-128-gcm","password":"test"}],"max_access":2,"expire_seconds":60}`))
			created := httptest.NewRecorder()
			NewTempSubscriptionHandler().ServeHTTP(created, create)
			if created.Code != http.StatusOK {
				t.Fatalf("create status = %d; body = %s", created.Code, created.Body.String())
			}
			var subscription CreateTempSubResponse
			if err := json.Unmarshal(created.Body.Bytes(), &subscription); err != nil {
				t.Fatal(err)
			}
			if len(subscription.ID) != 32 || subscription.URL != "/t/"+subscription.ID {
				t.Fatalf("unexpected created subscription: %+v", subscription)
			}

			request := httptest.NewRequest(http.MethodGet, subscription.URL, nil)
			request.Header.Set("User-Agent", userAgent)
			response := httptest.NewRecorder()
			NewTempSubscriptionAccessHandler().ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("access to created subscription status = %d, want 200; body = %s", response.Code, response.Body.String())
			}
			var body struct {
				Proxies []map[string]any `yaml:"proxies"`
			}
			if err := yaml.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if len(body.Proxies) != 1 || body.Proxies[0]["name"] != "regression-proxy" {
				t.Fatalf("access returned unexpected proxies: %+v", body.Proxies)
			}
			if got := tempSubStore.subscriptions[subscription.ID].AccessCount; got != 1 {
				t.Fatalf("access count = %d, want 1", got)
			}
		})
	}
}

func TestTemporarySubscriptionAccessRejectsMalformedIDs(t *testing.T) {
	originalStore := tempSubStore
	tempSubStore = &TempSubscriptionStore{subscriptions: make(map[string]*TempSubscription)}
	t.Cleanup(func() { tempSubStore = originalStore })

	for _, id := range []string{"", "1234abcd", strings.Repeat("a", 31), strings.Repeat("a", 33), strings.Repeat("A", 32), strings.Repeat("g", 32), strings.Repeat("a", 31) + "-"} {
		t.Run(id, func(t *testing.T) {
			// Seed malformed records so a 404 proves validation, not a missing store entry.
			sub := &TempSubscription{ID: id, MaxAccess: 2, ExpireAt: time.Now().Add(time.Minute)}
			tempSubStore.subscriptions[id] = sub
			request := httptest.NewRequest(http.MethodGet, "/t/"+id, nil)
			request.Header.Set("User-Agent", "Mihomo/1.19")
			response := httptest.NewRecorder()
			NewTempSubscriptionAccessHandler().ServeHTTP(response, request)
			if response.Code != http.StatusNotFound {
				t.Errorf("malformed ID status = %d, want 404", response.Code)
			}
			if sub.AccessCount != 0 {
				t.Errorf("malformed ID consumed %d accesses", sub.AccessCount)
			}
		})
	}
}

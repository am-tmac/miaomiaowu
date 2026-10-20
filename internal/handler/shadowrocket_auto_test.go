package handler

import (
	"net/http/httptest"
	"testing"
)

func TestShadowrocketAutomaticSubscriptionUsesURIList(t *testing.T) {
	for _, ua := range []string{"Shadowrocket/2070 CFNetwork/1410 Darwin/22.6.0", "Shadowrocket/2.2.80", "shadowrocket"} {
		r := httptest.NewRequest("GET", "/api/clash/subscribe?t=auto", nil)
		r.Header.Set("User-Agent", ua)
		if got := resolveClientType(r); got != "v2ray" {
			t.Errorf("UA %q auto subscription format = %q, want base64 URI list v2ray", ua, got)
		}
	}
}

func TestShadowrocketExplicitConfigurationStillAvailable(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/clash/subscribe?t=shadowrocket", nil)
	r.Header.Set("User-Agent", "Shadowrocket/2070")
	if got := resolveClientType(r); got != "shadowrocket" {
		t.Fatalf("explicit configuration format changed: %q", got)
	}
}

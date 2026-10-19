package handler

import (
	"net/http/httptest"
	"testing"
)

func TestGetClientIPTrustedForwarding(t *testing.T) {
	tests := []struct {
		name, proxies, remote, xff, realIP, cf, want string
	}{
		{"trusted exact IPv4", "10.0.0.2", "10.0.0.2:1234", "203.0.113.7", "", "", "203.0.113.7"},
		{"trusted CIDR chain", "10.0.0.0/24", "10.0.0.2:1234", "203.0.113.7, 10.0.0.3", "", "", "203.0.113.7"},
		{"untrusted socket", "10.0.0.0/24", "198.51.100.20:1234", "203.0.113.7", "203.0.113.8", "203.0.113.9", "198.51.100.20"},
		{"stop at untrusted hop", "10.0.0.0/24", "10.0.0.2:1234", "203.0.113.99, 198.51.100.20, 10.0.0.3", "", "", "198.51.100.20"},
		{"CF cannot override XFF", "10.0.0.2", "10.0.0.2:1234", "203.0.113.7", "203.0.113.8", "203.0.113.99", "203.0.113.7"},
		{"CF alone ignored even from trusted proxy", "10.0.0.2", "10.0.0.2:1234", "", "", "203.0.113.99", "10.0.0.2"},
		{"trusted real IP fallback", "10.0.0.2", "10.0.0.2:1234", "", " 203.0.113.8 ", "203.0.113.99", "203.0.113.8"},
		{"malformed XFF fails closed", "10.0.0.2", "10.0.0.2:1234", "unknown", "203.0.113.8", "", "10.0.0.2"},
		{"empty XFF hop fails closed", "10.0.0.2", "10.0.0.2:1234", "203.0.113.7,", "", "", "10.0.0.2"},
		{"XFF port rejected", "10.0.0.2", "10.0.0.2:1234", "203.0.113.7:1234", "", "", "10.0.0.2"},
		{"invalid real IP", "10.0.0.2", "10.0.0.2:1234", "", "unknown", "", "10.0.0.2"},
		{"invalid config disables all trust", "10.0.0.2,bad", "10.0.0.2:1234", "203.0.113.7", "", "", "10.0.0.2"},
		{"hostname config rejected", "localhost", "127.0.0.1:1234", "203.0.113.7", "", "", "127.0.0.1"},
		{"empty config entry rejected", "10.0.0.2,", "10.0.0.2:1234", "203.0.113.7", "", "", "10.0.0.2"},
		{"IPv6 trusted CIDR", "2001:db8:1::/48", "[2001:db8:1::2]:1234", "2001:db8:2::7", "", "", "2001:db8:2::7"},
		{"IPv6 bare socket", "", "2001:db8:1::2", "", "", "", "2001:db8:1::2"},
		{"mapped socket IPv4", "10.0.0.2", "[::ffff:10.0.0.2]:1234", "::ffff:203.0.113.7", "", "", "203.0.113.7"},
		{"whitespace config and multiple networks", " 192.0.2.1, 10.0.0.0/24 ", "10.0.0.2:1234", " 203.0.113.7 , 10.0.0.3 ", "", "", "203.0.113.7"},
		{"invalid socket never trusts headers", "10.0.0.2", "bad:peer", "203.0.113.7", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TRUSTED_PROXIES", tt.proxies)
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.remote
			if tt.xff != "" {
				r.Header.Set("X-Forwarded-For", tt.xff)
			}
			if tt.realIP != "" {
				r.Header.Set("X-Real-IP", tt.realIP)
			}
			if tt.cf != "" {
				r.Header.Set("CF-Connecting-IP", tt.cf)
			}
			if got := GetClientIP(r); got != tt.want {
				t.Fatalf("GetClientIP = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetClientIPMultipleForwardingFields(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/24")
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.2:1234"
	r.Header.Add("X-Forwarded-For", "203.0.113.99")
	r.Header.Add("X-Forwarded-For", "198.51.100.20, 10.0.0.3")
	if got := GetClientIP(r); got != "198.51.100.20" {
		t.Fatalf("GetClientIP = %q, want rightmost untrusted hop across fields", got)
	}
	r.Header.Del("X-Forwarded-For")
	r.Header.Add("X-Real-IP", "203.0.113.99")
	r.Header.Add("X-Real-IP", "198.51.100.20")
	if got := GetClientIP(r); got != "10.0.0.2" {
		t.Fatalf("GetClientIP = %q, want socket peer for ambiguous X-Real-IP", got)
	}
	r.Header.Set("X-Forwarded-For", "")
	r.Header.Set("X-Real-IP", "203.0.113.99")
	if got := GetClientIP(r); got != "10.0.0.2" {
		t.Fatalf("GetClientIP = %q, want socket peer for empty XFF", got)
	}
}

func TestGetClientIPIgnoresHeadersWithoutTrustedProxies(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "")
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "198.51.100.20:4321"
	r.Header.Set("CF-Connecting-IP", "203.0.113.1")
	r.Header.Set("X-Forwarded-For", "203.0.113.2")
	r.Header.Set("X-Real-IP", "203.0.113.3")
	if got := GetClientIP(r); got != "198.51.100.20" {
		t.Fatalf("GetClientIP = %q, want socket peer, ignoring spoofed headers", got)
	}
}

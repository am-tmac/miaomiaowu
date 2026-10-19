package main

import (
	"strings"
	"testing"
)

func TestRootRouterAccepts128BitTemporarySubscription(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"t/" + strings.Repeat("a", 32), true},
		{"t/0123456789abcdef0123456789abcdef", true},
		{"t/12345678", false},
		{"t/" + strings.Repeat("g", 32), false},
		{"t/" + strings.Repeat("a", 31) + "/", false},
		{strings.Repeat("a", 34), false},
	} {
		if got := isTemporarySubscriptionPath(tc.path); got != tc.want {
			t.Errorf("route %q = %v; want %v", tc.path, got, tc.want)
		}
	}
}

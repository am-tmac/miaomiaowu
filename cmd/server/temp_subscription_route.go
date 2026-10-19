package main

import "strings"

// isTemporarySubscriptionPath matches the 128-bit bearer-code route only.
func isTemporarySubscriptionPath(path string) bool {
	if !strings.HasPrefix(path, "t/") || len(path) != 34 {
		return false
	}
	for _, c := range path[2:] {
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

package handler

import (
	"encoding/hex"
	"testing"
)

func TestTemporarySubscriptionCodeHas128BitEntropy(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		code := generateTempSubCode()
		raw, err := hex.DecodeString(code)
		if err != nil || len(raw) != 16 {
			t.Fatalf("temporary bearer code must contain 16 random bytes; got %d characters", len(code))
		}
		if seen[code] {
			t.Fatal("duplicate temporary code")
		}
		seen[code] = true
	}
}

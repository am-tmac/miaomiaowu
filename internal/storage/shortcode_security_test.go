package storage

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestShortCodesRejectBiasedTail(t *testing.T) {
	for _, tc := range []struct {
		name     string
		generate func() (string, error)
		input    []byte
		want     string
	}{
		{"file", generateFileShortCode, []byte{248, 249, 250, 251, 252, 253, 254, 255, 0, 51, 247}, "aZ9"},
		{"user", generateUserShortCode, []byte{0, 248, 249, 250, 251, 252, 253, 254, 255, 0, 51, 247}, "aZ9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := rand.Reader
			rand.Reader = bytes.NewReader(tc.input)
			defer func() { rand.Reader = old }()
			got, err := tc.generate()
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q after rejecting bytes 248..255", got, tc.want)
			}
		})
	}
}

func TestShortCodeReaderExhaustion(t *testing.T) {
	if got, err := generateShortCode(bytes.NewReader([]byte{248, 255}), 1); got != "" || !errors.Is(err, io.EOF) {
		t.Fatalf("exhausted rejected bytes: got %q, error %v", got, err)
	}
}

func TestShortCodeLengthsRemainCompatible(t *testing.T) {
	for _, b := range []byte{0, 7, 8, 255} {
		input := append([]byte{b}, bytes.Repeat([]byte{0}, 10)...)
		old := rand.Reader
		rand.Reader = bytes.NewReader(input)
		got, err := generateUserShortCode()
		rand.Reader = old
		if err != nil {
			t.Fatal(err)
		}
		if want := 3 + int(b)%8; got != strings.Repeat("a", want) {
			t.Fatalf("length byte %d gave %q, want length %d", b, got, want)
		}
	}
}

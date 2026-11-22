package main

import (
	"bytes"
	"strings"
	"testing"
)

// fakeCM has no way to hand out a token: the banner interface omits them.
type fakeCM struct{}

func (fakeCM) Fingerprint() string    { return "fp123" }
func (fakeCM) TokenPath() string      { return "/f/tls/token.txt" }
func (fakeCM) AdminTokenPath() string { return "/f/tls/admin-token.txt" }

func TestBannerNamesFilesNotTokens(t *testing.T) {
	var b bytes.Buffer
	printBanner(&b, 7443, fakeCM{}, "/f", "signage://127.0.0.1:7443?fp=fp1")
	s := b.String()
	for _, w := range []string{":7443", "fp123", "/f/tls/token.txt", "/f/tls/admin-token.txt", "signage://127.0.0.1:7443?fp=fp1"} {
		if !strings.Contains(s, w) {
			t.Fatalf("missing %q in %q", w, s)
		}
	}
	if strings.Contains(s, "token=") {
		t.Fatalf("banner carries a token parameter: %q", s)
	}
}

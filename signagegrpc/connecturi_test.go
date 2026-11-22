package signagegrpc

import (
	"strings"
	"testing"
)

func TestConnectURIHasNoToken(t *testing.T) {
	cm, err := NewCertManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cm: cm}
	u := s.ConnectURI("")
	if !strings.HasPrefix(u, "signage://127.0.0.1:") || !strings.Contains(u, "fp=") {
		t.Fatalf("unexpected URI %q", u)
	}
	if strings.Contains(u, "token") || strings.Contains(u, cm.Token()) || strings.Contains(u, cm.AdminToken()) {
		t.Fatalf("URI carries a token: %q", u)
	}
	if !strings.HasSuffix(cm.TokenPath(), "token.txt") || !strings.HasSuffix(cm.AdminTokenPath(), "admin-token.txt") {
		t.Fatalf("paths %q %q", cm.TokenPath(), cm.AdminTokenPath())
	}
}

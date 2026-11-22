package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestConnectRefusesTokenInURL(t *testing.T) {
	var out bytes.Buffer
	err := connectTo(&out, []string{"signage://h:1?fp=ab&token=SECRETVALUE"})
	if err == nil || !strings.Contains(err.Error(), "--token-from") {
		t.Fatalf("want a refusal naming --token-from, got %v", err)
	}
	if strings.Contains(err.Error(), "SECRETVALUE") || strings.Contains(out.String(), "SECRETVALUE") {
		t.Fatalf("token echoed: %q / %v", out.String(), err)
	}
}

func TestConnectEmptyTokenAlsoRefused(t *testing.T) {
	if err := connectTo(&bytes.Buffer{}, []string{"signage://h:1?token="}); err == nil {
		t.Fatal("an empty token= parameter must be refused too")
	}
}

func TestConnectPrintsHostPortFingerprint(t *testing.T) {
	var out bytes.Buffer
	if err := connectTo(&out, []string{"signage://10.0.0.5:7443?fp=abcd"}); err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"host:        10.0.0.5", "port:        7443", "fingerprint: abcd"} {
		if !strings.Contains(out.String(), w) {
			t.Fatalf("missing %q in %q", w, out.String())
		}
	}
	if strings.Contains(out.String(), "token") {
		t.Fatalf("output mentions a token: %q", out.String())
	}
}

func TestConnectUsageAndScheme(t *testing.T) {
	if connectTo(&bytes.Buffer{}, nil) == nil {
		t.Fatal("no args must fail")
	}
	if connectTo(&bytes.Buffer{}, []string{"http://h:1"}) == nil {
		t.Fatal("wrong scheme must fail")
	}
}

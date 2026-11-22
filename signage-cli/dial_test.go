package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func parse(t *testing.T, args ...string) (*commonFlags, *bytes.Buffer, error) {
	t.Helper()
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	c := &commonFlags{}
	registerCommonFlags(fs, c)
	var errb bytes.Buffer
	c.stderr = &errb
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	return c, &errb, c.validate()
}

func TestPlainTokenStillWorksAndWarns(t *testing.T) {
	for _, flagName := range []string{"--token", "-t"} {
		c, errb, err := parse(t, "-H", "h", flagName, "abc")
		if err != nil || c.token != "abc" {
			t.Fatalf("%s: token %q, %v", flagName, c.token, err)
		}
		if !strings.Contains(errb.String(), "deprecated") || !strings.Contains(errb.String(), "--token-from") {
			t.Fatalf("%s: no warning: %q", flagName, errb.String())
		}
	}
}

func TestTokenFromFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tok")
	if err := os.WriteFile(p, []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, errb, err := parse(t, "-H", "h", "--token-from", "file:"+p)
	if err != nil || c.token != "s3cret" {
		t.Fatalf("token %q, %v", c.token, err)
	}
	if errb.Len() != 0 {
		t.Fatalf("unexpected warning %q", errb.String())
	}
}

func TestExclusive(t *testing.T) {
	for _, plain := range []string{"--token", "-t"} {
		if _, _, err := parse(t, "-H", "h", plain, "a", "--token-from", "file:/x"); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
			t.Fatalf("%s: err = %v", plain, err)
		}
	}
}

func TestNoTokenRefused(t *testing.T) {
	if _, _, err := parse(t, "-H", "h"); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("err = %v", err)
	}
}

func TestMissingHostRefused(t *testing.T) {
	if _, _, err := parse(t, "--token-from", "file:/x"); err == nil || !strings.Contains(err.Error(), "--host") {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenErrorNamesFlagNotSecret(t *testing.T) {
	_, _, err := parse(t, "-H", "h", "--token-from", "file:/nonexistent/zzz")
	if err == nil || !strings.Contains(err.Error(), "--token-from") || strings.Contains(err.Error(), "empty") {
		t.Fatalf("err = %v", err)
	}
}

func TestEmptySecretRefused(t *testing.T) {
	old := openSecret
	defer func() { openSecret = old }()
	openSecret = func(context.Context, string) ([]byte, error) { return []byte("\r\n"), nil }
	if _, _, err := parse(t, "-H", "h", "--token-from", "x:y"); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenFailurePropagates(t *testing.T) {
	old := openSecret
	defer func() { openSecret = old }()
	want := errors.New("boom")
	openSecret = func(context.Context, string) ([]byte, error) { return nil, want }
	if _, _, err := parse(t, "-H", "h", "--token-from", "x:y"); !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
}

func TestWarnsOnRealStderrWhenNoWriterSet(t *testing.T) {
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	c := &commonFlags{}
	registerCommonFlags(fs, c)
	if err := fs.Parse([]string{"-H", "h", "-t", "a"}); err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	err := c.validate()
	os.Stderr = old
	w.Close()
	var got bytes.Buffer
	_, _ = io.Copy(&got, r)
	if err != nil || !strings.Contains(got.String(), "deprecated") {
		t.Fatalf("err %v stderr %q", err, got.String())
	}
}

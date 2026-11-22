package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/go-authn/secretsource/secretflag"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

type commonFlags struct {
	host   string
	port   int
	token  string
	fs     *flag.FlagSet
	stderr io.Writer

	tokenBinding *secretflag.Binding
	certFile     string
	clientCert   string
	clientKey    string
	insecure     bool
}

func registerCommonFlags(fs *flag.FlagSet, c *commonFlags) {
	fs.StringVar(&c.host, "host", "", "server host")
	fs.StringVar(&c.host, "H", "", "server host (alias)")
	fs.IntVar(&c.port, "port", 50051, "server port")
	fs.IntVar(&c.port, "p", 50051, "server port (alias)")
	c.fs = fs
	c.tokenBinding = secretflag.AddStd(fs, warnWriter{c}, "token", &c.token, "bearer token", "t")
	fs.StringVar(&c.certFile, "cert", "", "server cert PEM to trust")
	fs.StringVar(&c.clientCert, "client-cert", "", "client cert PEM for mTLS")
	fs.StringVar(&c.clientKey, "client-key", "", "client key PEM for mTLS")
	fs.BoolVar(&c.insecure, "insecure", false, "skip TLS (dev)")
}

func (c *commonFlags) validate() error {
	if c.host == "" {
		return errors.New("--host (-H) is required")
	}
	if err := c.resolveToken(context.Background()); err != nil {
		return err
	}
	if c.token == "" {
		return errors.New("--token-from (or the deprecated --token/-t) is required")
	}
	return nil
}

// warnWriter sends the deprecation warning to commonFlags.stderr, read at
// write time (tests set it after the flags are registered), else os.Stderr.
type warnWriter struct{ c *commonFlags }

func (w warnWriter) Write(p []byte) (int, error) {
	out := w.c.stderr
	if out == nil {
		out = os.Stderr
	}
	return out.Write(p)
}

// resolveToken applies --token-from, refuses it together with the plain
// --token/-t, and warns on stderr when the plain flag is used: its value is
// visible in ps and in /proc/PID/cmdline.
func (c *commonFlags) resolveToken(ctx context.Context) error {
	return secretflag.Resolve(ctx, c.tokenBinding)
}

func (c *commonFlags) dial(ctx context.Context) (*grpc.ClientConn, context.Context, error) {
	target := fmt.Sprintf("%s:%d", c.host, c.port)
	var creds credentials.TransportCredentials
	if c.insecure {
		creds = insecure.NewCredentials()
	} else {
		tc, err := c.tlsConfig()
		if err != nil {
			return nil, nil, err
		}
		creds = credentials.NewTLS(tc)
	}
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, nil, fmt.Errorf("dial %s: %w", target, err)
	}
	md := metadata.New(map[string]string{"authorization": "Bearer " + c.token})
	return conn, metadata.NewOutgoingContext(ctx, md), nil
}

func (c *commonFlags) tlsConfig() (*tls.Config, error) {
	tc := &tls.Config{MinVersion: tls.VersionTLS12}
	if c.certFile != "" {
		pemBytes, err := os.ReadFile(c.certFile)
		if err != nil {
			return nil, fmt.Errorf("read --cert: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pemBytes) {
			return nil, fmt.Errorf("--cert: no CERTIFICATE block in %s", c.certFile)
		}
		tc.RootCAs = pool
	}
	if c.clientCert != "" || c.clientKey != "" {
		if c.clientCert == "" || c.clientKey == "" {
			return nil, errors.New("--client-cert and --client-key must be set together")
		}
		ckp, err := tls.LoadX509KeyPair(c.clientCert, c.clientKey)
		if err != nil {
			return nil, fmt.Errorf("load client keypair: %w", err)
		}
		tc.Certificates = []tls.Certificate{ckp}
	}
	return tc, nil
}

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

type commonFlags struct {
	host       string
	port       int
	token      string
	certFile   string
	clientCert string
	clientKey  string
	insecure   bool
}

func registerCommonFlags(fs *flag.FlagSet, c *commonFlags) {
	fs.StringVar(&c.host, "host", "", "server host")
	fs.StringVar(&c.host, "H", "", "server host (alias)")
	fs.IntVar(&c.port, "port", 50051, "server port")
	fs.IntVar(&c.port, "p", 50051, "server port (alias)")
	fs.StringVar(&c.token, "token", "", "bearer token")
	fs.StringVar(&c.token, "t", "", "bearer token (alias)")
	fs.StringVar(&c.certFile, "cert", "", "server cert PEM to trust")
	fs.StringVar(&c.clientCert, "client-cert", "", "client cert PEM for mTLS")
	fs.StringVar(&c.clientKey, "client-key", "", "client key PEM for mTLS")
	fs.BoolVar(&c.insecure, "insecure", false, "skip TLS (dev)")
}

func (c *commonFlags) validate() error {
	if c.host == "" {
		return errors.New("--host (-H) is required")
	}
	if c.token == "" {
		return errors.New("--token (-t) is required")
	}
	return nil
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

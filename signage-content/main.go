// Command signage-content runs the central rendering server. It serves
// the Content gRPC service that signage-agent puller clients query.
//
// The renderer drives a headless Chromium via chromedp; it screenshots a
// per-device URL at the panel's resolution and returns the PNG. Devices
// are configured through a JSON file (--devices) in the form:
//
//	{ "devices": [
//	    {"id":"lobby-01","url":"http://internal/dash/lobby","width":1872,"height":1404,"poll_interval_seconds":60},
//	    ...
//	] }
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-sicp/go-signage/signagecontent"
)

const serverVersion = "0.1.0"

func main() {
	var (
		port        int
		devicesPath string
		tlsCert     string
		tlsKey      string
	)
	flag.IntVar(&port, "port", signagecontent.DefaultPort, "gRPC listen port")
	flag.StringVar(&devicesPath, "devices", "devices.json", "path to devices configuration JSON")
	flag.StringVar(&tlsCert, "tls-cert", "", "path to TLS certificate PEM (omit for plaintext, dev only)")
	flag.StringVar(&tlsKey, "tls-key", "", "path to TLS private key PEM")
	flag.Parse()

	store, err := signagecontent.LoadStoreFromJSON(devicesPath)
	if err != nil {
		log.Fatalf("load store: %v", err)
	}

	renderer, err := signagecontent.NewChromedpRenderer()
	if err != nil {
		log.Fatalf("renderer: %v", err)
	}
	defer renderer.Close()

	svc := signagecontent.NewService(store, renderer)

	cfg := signagecontent.Config{
		Service:       svc,
		Port:          port,
		ServerVersion: serverVersion,
	}
	if tlsCert != "" {
		cer, err := os.ReadFile(tlsCert)
		if err != nil {
			log.Fatalf("read --tls-cert: %v", err)
		}
		key, err := os.ReadFile(tlsKey)
		if err != nil {
			log.Fatalf("read --tls-key: %v", err)
		}
		cfg.TLSCertPEM = cer
		cfg.TLSKeyPEM = key
	}

	srv, err := signagecontent.New(cfg)
	if err != nil {
		log.Fatalf("server new: %v", err)
	}
	if err := srv.Start(); err != nil {
		log.Fatalf("server start: %v", err)
	}
	defer srv.Stop()

	fmt.Printf("signage-content listening on :%d\n", srv.ListeningPort())
	fmt.Printf("  devices loaded: %d\n", len(store.List()))
	if tlsCert == "" {
		fmt.Printf("  WARNING: TLS disabled (dev only). Set --tls-cert/--tls-key for production.\n")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	<-ctx.Done()
	log.Println("signage-content shutting down")
}

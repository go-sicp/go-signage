// Command signage-agent runs on a Pi Zero 2W (or any Linux box wired to
// an IT8951 controller). It opens the display, starts a Display gRPC
// server (push), and a signagepull.Puller goroutine (pull). On non-Linux
// hosts it boots without a panel for development.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/go-sicp/go-signage/signagegrpc"
	"github.com/go-sicp/go-signage/signagelib"
	"github.com/go-sicp/go-signage/signagepull"
)

const serverVersion = "0.1.0"

func main() {
	var (
		filesDir     string
		port         int
		mtls         bool
		mdnsOff      bool
		spiPort      string
		resetPin     string
		busyPin      string
		spiHz        int64
		vcomMV       int
		contentAddr  string
		deviceID     string
		pullInterval int
		caPEMPath    string
		tokenPath    string
		insecurePull bool
	)
	flag.StringVar(&filesDir, "files-dir", defaultFilesDir(), "directory for TLS material and tokens")
	flag.IntVar(&port, "port", signagegrpc.DefaultPort, "Display gRPC listen port")
	flag.BoolVar(&mtls, "mtls", true, "require client cert (mTLS)")
	flag.BoolVar(&mdnsOff, "no-mdns", false, "disable mDNS advertisement")
	flag.StringVar(&spiPort, "spi-port", "", "SPI bus name (empty = first available)")
	flag.StringVar(&resetPin, "rst-pin", "", "GPIO name for IT8951 RST (default GPIO17)")
	flag.StringVar(&busyPin, "busy-pin", "", "GPIO name for IT8951 BUSY/HRDY (default GPIO24)")
	flag.Int64Var(&spiHz, "spi-hz", 0, "SPI clock in Hz (0 = 12 MHz)")
	flag.IntVar(&vcomMV, "vcom-mv", 0, "panel VCOM in millivolts (0 = leave EEPROM default)")
	flag.StringVar(&contentAddr, "content-server", "", "central content server addr (host:port); enables pull mode if set")
	flag.StringVar(&deviceID, "device-id", "", "device identifier for pull mode (default: hostname)")
	flag.IntVar(&pullInterval, "pull-interval", 60, "seconds between pull polls")
	flag.StringVar(&caPEMPath, "content-ca", "", "path to PEM file trusted for content server TLS")
	flag.StringVar(&tokenPath, "content-token", "", "path to bearer token used against content server")
	flag.BoolVar(&insecurePull, "content-insecure", false, "skip TLS to content server (dev only)")
	flag.Parse()

	if err := os.MkdirAll(filesDir, 0o700); err != nil {
		log.Fatalf("mkdir files-dir: %v", err)
	}

	display, err := openDisplay(spiPort, resetPin, busyPin, spiHz, vcomMV)
	if err != nil {
		log.Printf("warning: display not opened: %v (Health/GetInfo will report missing device)", err)
	} else {
		defer display.Close()
	}

	puller, err := signagepull.New(signagepull.Config{
		Display:     display,
		ServerCAPEM: readFileOrEmpty(caPEMPath),
		Token:       readStringOrEmpty(tokenPath),
		Insecure:    insecurePull,
	})
	if err != nil {
		log.Fatalf("puller new: %v", err)
	}

	if contentAddr != "" {
		id := deviceID
		if id == "" {
			id = hostnameFallback()
		}
		if err := puller.SetPullMode(true, contentAddr, id, pullInterval); err != nil {
			log.Fatalf("set pull mode: %v", err)
		}
	}

	srv, err := signagegrpc.New(signagegrpc.Config{
		Display:       display,
		Pull:          puller,
		FilesDir:      filesDir,
		Port:          port,
		ServerVersion: serverVersion,
		EnableMTLS:    mtls,
		EnableMDNS:    !mdnsOff,
	})
	if err != nil {
		log.Fatalf("server new: %v", err)
	}
	if err := srv.Start(); err != nil {
		log.Fatalf("server start: %v", err)
	}
	defer srv.Stop()

	cm := srv.CertManager()
	fmt.Printf("signage-agent listening on :%d\n", srv.ListeningPort())
	fmt.Printf("  fingerprint:  %s\n", cm.Fingerprint())
	fmt.Printf("  token:        %s\n", cm.Token())
	fmt.Printf("  admin token:  %s\n", cm.AdminToken())
	fmt.Printf("  files dir:    %s\n", filesDir)
	fmt.Printf("  connect URI:  %s\n", srv.ConnectURI(""))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		if err := puller.Run(ctx); err != nil && err != context.Canceled {
			log.Printf("puller exited: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("signage-agent shutting down")
}

func openDisplay(spiPort, rst, busy string, hz int64, vcom int) (signagelib.Display, error) {
	return signagelib.Open(signagelib.Config{
		SPIPort:  spiPort,
		ResetPin: rst,
		BusyPin:  busy,
		SPIHz:    hz,
		VCOMmV:   vcom,
	})
}

func defaultFilesDir() string {
	if v := os.Getenv("SIGNAGE_FILES_DIR"); v != "" {
		return v
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".signage")
	}
	return "/var/lib/signage"
}

func hostnameFallback() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "device"
}

func readFileOrEmpty(path string) []byte {
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		log.Printf("warning: read %s: %v", path, err)
		return nil
	}
	return b
}

func readStringOrEmpty(path string) string {
	b := readFileOrEmpty(path)
	if b == nil {
		return ""
	}
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	return string(b)
}

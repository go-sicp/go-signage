package signagegrpc

import (
	"fmt"
	"os"

	"github.com/grandcat/zeroconf"
)

// NewMdnsRegistrar returns a registrar backed by github.com/grandcat/zeroconf.
func NewMdnsRegistrar() MdnsRegistrar {
	return &zcRegistrar{}
}

type zcRegistrar struct {
	mdnsBase
	server *zeroconf.Server
}

func (z *zcRegistrar) Register(port int, panelW, panelH int32, certFingerprint, version string) error {
	host, _ := os.Hostname()
	if host == "" {
		host = "device"
	}
	instance := HostnameInstance(host)
	txt := []string{
		"version=" + version,
		fmt.Sprintf("panel=%dx%d", panelW, panelH),
		"fingerprint=" + truncatedFingerprint(certFingerprint),
	}
	srv, err := zeroconf.Register(instance, ServiceType, "local.", port, txt, nil)
	if err != nil {
		return fmt.Errorf("zeroconf register: %w", err)
	}
	z.mu.Lock()
	z.name = instance
	z.server = srv
	z.mu.Unlock()
	return nil
}

func (z *zcRegistrar) Unregister() {
	z.mu.Lock()
	srv := z.server
	z.server = nil
	z.name = ""
	z.mu.Unlock()
	if srv != nil {
		srv.Shutdown()
	}
}

// truncatedFingerprint keeps only the first 16 hex bytes (47 chars with
// the colon separators) so the TXT record stays under the 255-byte mDNS
// limit even with extra fields.
func truncatedFingerprint(fp string) string {
	if len(fp) <= 47 {
		return fp
	}
	return fp[:47]
}

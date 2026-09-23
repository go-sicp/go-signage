package signagegrpc

import (
	"fmt"
	"sync"
)

// MdnsRegistrar advertises the gRPC server as `_signage._tcp.` on the LAN
// with TXT records `version`, `panel`, and `fingerprint`.
type MdnsRegistrar interface {
	Register(port int, panelW, panelH int32, certFingerprint, version string) error
	Unregister()
	RegisteredName() string
}

const ServiceType = "_signage._tcp."

type mdnsBase struct {
	mu   sync.Mutex
	name string
}

func (m *mdnsBase) RegisteredName() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.name
}

// HostnameInstance composes the instance name advertised on mDNS.
func HostnameInstance(prefix string) string {
	if prefix == "" {
		prefix = "go-signage"
	}
	return fmt.Sprintf("%s", prefix)
}

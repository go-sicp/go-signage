//go:build !linux

package signagelib

import "errors"

var errNotLinux = errors.New("signagelib: only available on linux (periph.io SPI + GPIO)")

// Open returns an error on non-Linux hosts. The signage-agent main on
// darwin therefore wires a nil Display, and the gRPC service degrades
// gracefully (Health and GetInfo report the missing device).
func Open(_ Config) (Display, error) { return nil, errNotLinux }

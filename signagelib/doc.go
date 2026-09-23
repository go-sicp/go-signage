// Package signagelib drives a GoodDisplay e-paper panel through an IT8951
// controller over SPI plus a small set of GPIO pins (RST, BUSY/HRDY).
//
// The package is split between a generic Display interface (display.go)
// and a Linux-only concrete implementation (display_linux.go, it8951.go)
// that uses periph.io for SPI and GPIO. On non-Linux hosts the build pulls
// in display_stub.go so dependent packages can still vet and compile for
// development on macOS.
//
// The IT8951 protocol implementation in it8951.go is written from the
// publicly documented SPI/I80 command set. It must be exercised against
// real hardware before production use; key timing parameters (SPI clock,
// HRDY polling cadence) may need tuning for a specific HAT revision.
package signagelib

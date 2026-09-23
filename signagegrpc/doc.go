// Package signagegrpc hosts the Display gRPC service that runs on each
// Pi. It owns TLS material (self-signed, persisted on disk under
// FilesDir/tls/), bearer token validation, and an mDNS advertisement of
// the service as `_signage._tcp.` on the LAN.
//
// The service delegates panel I/O to a signagelib.Display and exposes a
// PullController hook that the agent main wires to its signagepull
// goroutine, so SetPullMode / GetPullMode RPCs can reconfigure the puller
// at runtime.
package signagegrpc

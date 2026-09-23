// Package signagepull is the Pi-side client that pulls frames from a
// central Content gRPC server, decodes the returned PNG, packs it for the
// IT8951, and pushes it through a signagelib.Display.
//
// It implements signagegrpc.PullController so the agent main can wire it
// to the Display service: SetPullMode/GetPullMode RPCs end up reconfiguring
// the puller goroutine without restarting the process.
package signagepull

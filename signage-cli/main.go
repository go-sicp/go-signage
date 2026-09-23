// Command signage-cli is the operator CLI for go-signage. It speaks the
// Display gRPC service over TLS+bearer to a Pi running signage-agent and
// browses `_signage._tcp.` for fleet discovery.
package main

import (
	"fmt"
	"os"
)

const usage = `signage-cli — operator CLI for go-signage

Usage:
  signage-cli <command> [flags...]

Commands:
  info                    Print panel geometry and IT8951 firmware/LUT version.
  health                  Print server health.
  load-image --png <file> [--mode GC16] [--bpp 4]
                          Push a PNG to the panel and trigger Refresh.
  refresh --mode <Mode>   Trigger a refresh on the resident image.
  sleep | standby         Power down the controller.
  set-pull-mode --enable [--content host:port] [--device id] [--interval 60]
  get-pull-mode
  rotate                  Force-rotate every credential (admin).
  reissue --out ./tls     Reissue client cert+key (admin).
  metrics                 Print Prometheus metrics.
  discover [--timeout 5] [--first]
  connect signage://...

Common flags (all RPC commands):
  -H, --host        Server host (required)
  -p, --port        Server port (default 50051)
  -t, --token       Bearer token (required unless --insecure-anon)
      --cert        Server cert PEM to trust (skip if --insecure)
      --client-cert Client cert PEM for mTLS
      --client-key  Client key PEM for mTLS
      --insecure    Skip TLS (dev only)
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "info":
		err = cmdInfo(args)
	case "health":
		err = cmdHealth(args)
	case "load-image":
		err = cmdLoadImage(args)
	case "refresh":
		err = cmdRefresh(args)
	case "sleep":
		err = cmdSleep(args)
	case "standby":
		err = cmdStandby(args)
	case "set-pull-mode":
		err = cmdSetPullMode(args)
	case "get-pull-mode":
		err = cmdGetPullMode(args)
	case "rotate":
		err = cmdRotate(args)
	case "reissue":
		err = cmdReissue(args)
	case "metrics":
		err = cmdMetrics(args)
	case "discover":
		err = cmdDiscover(args)
	case "connect":
		err = cmdConnect(args)
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

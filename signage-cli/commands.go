package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/grandcat/zeroconf"

	"github.com/go-sicp/go-signage/signagelib"
	pb "github.com/go-sicp/go-signage/signageproto"
)

const cliCallTimeout = 60 * time.Second

func cmdInfo(args []string) error {
	fs := flag.NewFlagSet("info", flag.ExitOnError)
	c := &commonFlags{}
	registerCommonFlags(fs, c)
	_ = fs.Parse(args)
	if err := c.validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cliCallTimeout)
	defer cancel()
	conn, ctx, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	cli := pb.NewDisplayClient(conn)
	info, err := cli.GetInfo(ctx, &emptypb.Empty{})
	if err != nil {
		return err
	}
	fmt.Printf("width:                %d\n", info.Width)
	fmt.Printf("height:               %d\n", info.Height)
	fmt.Printf("image_buffer_address: 0x%08X\n", info.ImageBufferAddress)
	fmt.Printf("firmware_version:     %s\n", info.FirmwareVersion)
	fmt.Printf("lut_version:          %s\n", info.LutVersion)
	fmt.Printf("server_version:       %s\n", info.AppVersion)
	return nil
}

func cmdHealth(args []string) error {
	fs := flag.NewFlagSet("health", flag.ExitOnError)
	c := &commonFlags{}
	registerCommonFlags(fs, c)
	_ = fs.Parse(args)
	if err := c.validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cliCallTimeout)
	defer cancel()
	conn, ctx, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	cli := pb.NewDisplayClient(conn)
	h, err := cli.Health(ctx, &emptypb.Empty{})
	if err != nil {
		return err
	}
	fmt.Printf("device_open:       %v\n", h.DeviceOpen)
	fmt.Printf("server_version:    %s\n", h.ServerVersion)
	fmt.Printf("uptime_seconds:    %d\n", h.UptimeSeconds)
	fmt.Printf("pull_mode_enabled: %v\n", h.PullModeEnabled)
	if h.LastError != "" {
		fmt.Printf("last_error:        %s\n", h.LastError)
	}
	return nil
}

func cmdLoadImage(args []string) error {
	fs := flag.NewFlagSet("load-image", flag.ExitOnError)
	c := &commonFlags{}
	registerCommonFlags(fs, c)
	var (
		pngPath  string
		modeName string
		bpp      int
		chunkSz  int
	)
	fs.StringVar(&pngPath, "png", "", "path to PNG to push (required)")
	fs.StringVar(&modeName, "mode", "GC16", "Refresh mode (GC16, GL16, DU, A2, ...)")
	fs.IntVar(&bpp, "bpp", 4, "bits per pixel (1, 2, 4 or 8)")
	fs.IntVar(&chunkSz, "chunk-bytes", 65536, "max bytes per stream chunk")
	_ = fs.Parse(args)
	if err := c.validate(); err != nil {
		return err
	}
	if pngPath == "" {
		return errors.New("--png is required")
	}
	mode, ok := parseWaveform(modeName)
	if !ok {
		return fmt.Errorf("--mode: unknown waveform %q", modeName)
	}
	pngBytes, err := os.ReadFile(pngPath)
	if err != nil {
		return fmt.Errorf("read --png: %w", err)
	}

	packed, w, h, err := packForBPP(pngBytes, bpp)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), cliCallTimeout)
	defer cancel()
	conn, ctx, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	cli := pb.NewDisplayClient(conn)
	stream, err := cli.LoadImage(ctx)
	if err != nil {
		return err
	}
	header := &pb.LoadImageChunk{Payload: &pb.LoadImageChunk_Header{
		Header: &pb.LoadImageHeader{
			Mode:         mode,
			Region:       &pb.Region{X: 0, Y: 0, W: w, H: h},
			BitsPerPixel: int32(bpp),
			TotalBytes:   int64(len(packed)),
		},
	}}
	if err := stream.Send(header); err != nil {
		return err
	}
	for off := 0; off < len(packed); off += chunkSz {
		end := off + chunkSz
		if end > len(packed) {
			end = len(packed)
		}
		ck := &pb.LoadImageChunk{Payload: &pb.LoadImageChunk_Data{Data: packed[off:end]}}
		if err := stream.Send(ck); err != nil {
			return err
		}
	}
	resp, err := stream.CloseAndRecv()
	if err != nil {
		return err
	}
	fmt.Printf("frame_hash: %s\nelapsed_ms: %d\n", resp.FrameHash, resp.ElapsedMs)

	r, err := cli.Refresh(ctx, &pb.RefreshRequest{Mode: mode})
	if err != nil {
		return err
	}
	fmt.Printf("refresh_elapsed_ms: %d\n", r.ElapsedMs)
	return nil
}

func cmdRefresh(args []string) error {
	fs := flag.NewFlagSet("refresh", flag.ExitOnError)
	c := &commonFlags{}
	registerCommonFlags(fs, c)
	var modeName string
	fs.StringVar(&modeName, "mode", "GC16", "Refresh mode")
	_ = fs.Parse(args)
	if err := c.validate(); err != nil {
		return err
	}
	mode, ok := parseWaveform(modeName)
	if !ok {
		return fmt.Errorf("--mode: unknown waveform %q", modeName)
	}
	ctx, cancel := context.WithTimeout(context.Background(), cliCallTimeout)
	defer cancel()
	conn, ctx, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	cli := pb.NewDisplayClient(conn)
	r, err := cli.Refresh(ctx, &pb.RefreshRequest{Mode: mode})
	if err != nil {
		return err
	}
	fmt.Printf("elapsed_ms: %d\n", r.ElapsedMs)
	return nil
}

func cmdSleep(args []string) error   { return simpleEmpty(args, "sleep") }
func cmdStandby(args []string) error { return simpleEmpty(args, "standby") }
func cmdRotate(args []string) error  { return simpleEmpty(args, "rotate") }
func cmdMetrics(args []string) error {
	fs := flag.NewFlagSet("metrics", flag.ExitOnError)
	c := &commonFlags{}
	registerCommonFlags(fs, c)
	_ = fs.Parse(args)
	if err := c.validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cliCallTimeout)
	defer cancel()
	conn, ctx, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	cli := pb.NewDisplayClient(conn)
	m, err := cli.GetMetrics(ctx, &emptypb.Empty{})
	if err != nil {
		return err
	}
	fmt.Print(m.PromText)
	return nil
}

func simpleEmpty(args []string, name string) error {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	c := &commonFlags{}
	registerCommonFlags(fs, c)
	_ = fs.Parse(args)
	if err := c.validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cliCallTimeout)
	defer cancel()
	conn, ctx, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	cli := pb.NewDisplayClient(conn)
	switch name {
	case "sleep":
		_, err = cli.Sleep(ctx, &emptypb.Empty{})
	case "standby":
		_, err = cli.Standby(ctx, &emptypb.Empty{})
	case "rotate":
		var resp *pb.RotateResponse
		resp, err = cli.Rotate(ctx, &emptypb.Empty{})
		if err == nil {
			fmt.Printf("scheduled_at_ms: %d\n", resp.ScheduledAtMs)
		}
	}
	if err != nil {
		return err
	}
	if name != "rotate" {
		fmt.Println("ok")
	}
	return nil
}

func cmdReissue(args []string) error {
	fs := flag.NewFlagSet("reissue", flag.ExitOnError)
	c := &commonFlags{}
	registerCommonFlags(fs, c)
	var outDir string
	fs.StringVar(&outDir, "out", "tls", "directory to write the new client cert+key into")
	_ = fs.Parse(args)
	if err := c.validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cliCallTimeout)
	defer cancel()
	conn, ctx, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	cli := pb.NewDisplayClient(conn)
	resp, err := cli.ReissueClientCert(ctx, &emptypb.Empty{})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "client-cert.pem"), []byte(resp.ClientCertPem), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "client-key.pem"), []byte(resp.ClientKeyPem), 0o600); err != nil {
		return err
	}
	fmt.Printf("client_cert_fingerprint: %s\nserver_restart_in_ms: %d\nwrote: %s\n",
		resp.ClientCertFingerprint, resp.ServerRestartInMs, outDir)
	return nil
}

func cmdSetPullMode(args []string) error {
	fs := flag.NewFlagSet("set-pull-mode", flag.ExitOnError)
	c := &commonFlags{}
	registerCommonFlags(fs, c)
	var (
		enable   bool
		disable  bool
		content  string
		device   string
		interval int
	)
	fs.BoolVar(&enable, "enable", false, "enable pull mode")
	fs.BoolVar(&disable, "disable", false, "disable pull mode")
	fs.StringVar(&content, "content", "", "content server addr (host:port)")
	fs.StringVar(&device, "device", "", "device id to advertise to content server")
	fs.IntVar(&interval, "interval", 60, "poll interval in seconds")
	_ = fs.Parse(args)
	if err := c.validate(); err != nil {
		return err
	}
	if enable == disable {
		return errors.New("exactly one of --enable / --disable is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), cliCallTimeout)
	defer cancel()
	conn, ctx, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	cli := pb.NewDisplayClient(conn)
	_, err = cli.SetPullMode(ctx, &pb.SetPullModeRequest{
		Enabled:             enable,
		ContentServerAddr:   content,
		DeviceId:            device,
		PollIntervalSeconds: int32(interval),
	})
	if err != nil {
		return err
	}
	fmt.Println("ok")
	return nil
}

func cmdGetPullMode(args []string) error {
	fs := flag.NewFlagSet("get-pull-mode", flag.ExitOnError)
	c := &commonFlags{}
	registerCommonFlags(fs, c)
	_ = fs.Parse(args)
	if err := c.validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cliCallTimeout)
	defer cancel()
	conn, ctx, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	cli := pb.NewDisplayClient(conn)
	st, err := cli.GetPullMode(ctx, &emptypb.Empty{})
	if err != nil {
		return err
	}
	fmt.Printf("enabled:               %v\n", st.Enabled)
	fmt.Printf("content_server_addr:   %s\n", st.ContentServerAddr)
	fmt.Printf("device_id:             %s\n", st.DeviceId)
	fmt.Printf("poll_interval_seconds: %d\n", st.PollIntervalSeconds)
	fmt.Printf("last_pull_unix:        %d\n", st.LastPullUnix)
	fmt.Printf("last_frame_hash:       %s\n", st.LastFrameHash)
	if st.LastError != "" {
		fmt.Printf("last_error:            %s\n", st.LastError)
	}
	return nil
}

func cmdDiscover(args []string) error {
	fs := flag.NewFlagSet("discover", flag.ExitOnError)
	var timeout int
	var first bool
	fs.IntVar(&timeout, "timeout", 5, "browse duration in seconds")
	fs.BoolVar(&first, "first", false, "exit on the first match")
	_ = fs.Parse(args)

	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return fmt.Errorf("zeroconf resolver: %w", err)
	}
	entries := make(chan *zeroconf.ServiceEntry)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()
	if err := resolver.Browse(ctx, "_signage._tcp.", "local.", entries); err != nil {
		return fmt.Errorf("browse: %w", err)
	}
	for e := range entries {
		ip := ""
		if len(e.AddrIPv4) > 0 {
			ip = e.AddrIPv4[0].String()
		} else if len(e.AddrIPv6) > 0 {
			ip = e.AddrIPv6[0].String()
		}
		fmt.Printf("%s\t%s:%d\t%s\n", e.Instance, ip, e.Port, strings.Join(e.Text, " "))
		if first {
			return nil
		}
	}
	return nil
}

func cmdConnect(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: signage-cli connect signage://...")
	}
	u, err := url.Parse(args[0])
	if err != nil {
		return fmt.Errorf("parse uri: %w", err)
	}
	if u.Scheme != "signage" {
		return fmt.Errorf("expected signage:// scheme, got %q", u.Scheme)
	}
	q := u.Query()
	fmt.Printf("host:        %s\n", u.Hostname())
	fmt.Printf("port:        %s\n", u.Port())
	fmt.Printf("token:       %s\n", q.Get("token"))
	fmt.Printf("fingerprint: %s\n", q.Get("fp"))
	return nil
}

func parseWaveform(s string) (pb.WaveformMode, bool) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "INIT":
		return pb.WaveformMode_WAVEFORM_MODE_INIT, true
	case "DU":
		return pb.WaveformMode_WAVEFORM_MODE_DU, true
	case "GC16":
		return pb.WaveformMode_WAVEFORM_MODE_GC16, true
	case "GL16":
		return pb.WaveformMode_WAVEFORM_MODE_GL16, true
	case "GLR16":
		return pb.WaveformMode_WAVEFORM_MODE_GLR16, true
	case "GLD16":
		return pb.WaveformMode_WAVEFORM_MODE_GLD16, true
	case "A2":
		return pb.WaveformMode_WAVEFORM_MODE_A2, true
	}
	return pb.WaveformMode_WAVEFORM_MODE_UNSPECIFIED, false
}

func packForBPP(pngBytes []byte, bpp int) ([]byte, int32, int32, error) {
	if bpp != 4 {
		return nil, 0, 0, fmt.Errorf("unsupported --bpp %d (only 4 is wired in the CLI; agent supports 2/4/8)", bpp)
	}
	img, err := signagelib.PackPNGGrayscale4(pngBytes)
	if err != nil {
		return nil, 0, 0, err
	}
	return img.Pixels(), img.Width(), img.Height(), nil
}

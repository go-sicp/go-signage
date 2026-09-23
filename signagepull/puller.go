package signagepull

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"github.com/go-sicp/go-signage/signagegrpc"
	"github.com/go-sicp/go-signage/signagelib"
	pb "github.com/go-sicp/go-signage/signageproto"
)

// Config carries connection parameters that do not change at runtime.
// SetPullMode mutates the per-pull settings (enabled, server addr, device
// id, interval); credentials and panel binding are fixed at construction.
type Config struct {
	Display     signagelib.Display
	Token       string
	ServerCAPEM []byte // optional, for self-signed content server
	Insecure    bool   // dev only; skip TLS
	Logger      *log.Logger
}

// Puller polls a Content service and pushes the returned PNG to a Display.
// It implements signagegrpc.PullController.
type Puller struct {
	cfg Config

	mu          sync.Mutex
	enabled     bool
	contentAddr string
	deviceID    string
	interval    time.Duration
	lastHash    string
	lastErr     string
	lastPullAt  time.Time
}

func New(cfg Config) (*Puller, error) {
	if cfg.Logger == nil {
		cfg.Logger = log.New(os.Stderr, "signagepull: ", log.LstdFlags)
	}
	return &Puller{cfg: cfg}, nil
}

// Run blocks until ctx is done. The goroutine is idle when pull mode is
// disabled; SetPullMode wakes it up.
func (p *Puller) Run(ctx context.Context) error {
	for {
		p.mu.Lock()
		enabled := p.enabled
		interval := p.interval
		p.mu.Unlock()
		if !enabled {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(1 * time.Second):
				continue
			}
		}

		if err := p.pollOnce(ctx); err != nil {
			p.recordErr(err)
			p.cfg.Logger.Printf("poll: %v", err)
		}

		if interval <= 0 {
			interval = 60 * time.Second
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

func (p *Puller) pollOnce(ctx context.Context) error {
	p.mu.Lock()
	addr := p.contentAddr
	deviceID := p.deviceID
	hash := p.lastHash
	p.mu.Unlock()
	if addr == "" {
		return errors.New("content server address not set")
	}
	if deviceID == "" {
		return errors.New("device id not set")
	}

	conn, err := p.dial(addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	cli := pb.NewContentClient(conn)

	if p.cfg.Token != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+p.cfg.Token)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var w, h int32
	if p.cfg.Display != nil {
		if info, err := p.cfg.Display.Info(); err == nil {
			w, h = info.Width(), info.Height()
		}
	}

	resp, err := cli.GetFrame(ctx, &pb.GetFrameRequest{
		DeviceId:         deviceID,
		CurrentFrameHash: hash,
		WidthHint:        w,
		HeightHint:       h,
	})
	if err != nil {
		return fmt.Errorf("getFrame: %w", err)
	}
	p.mu.Lock()
	p.lastPullAt = time.Now()
	if resp.NextPollSeconds > 0 {
		p.interval = time.Duration(resp.NextPollSeconds) * time.Second
	}
	p.mu.Unlock()

	if resp.Unchanged {
		return nil
	}
	return p.applyFrame(resp.Png, resp.FrameHash, translateMode(resp.SuggestedMode))
}

func (p *Puller) applyFrame(png []byte, hash string, mode signagelib.WaveformMode) error {
	if p.cfg.Display == nil {
		return errors.New("no display attached")
	}
	img, err := signagelib.PackPNGGrayscale4(png)
	if err != nil {
		return fmt.Errorf("pack png: %w", err)
	}
	if err := p.cfg.Display.LoadImage(img, signagelib.Region{}); err != nil {
		return fmt.Errorf("loadImage: %w", err)
	}
	if err := p.cfg.Display.Refresh(mode, signagelib.Region{}); err != nil {
		return fmt.Errorf("refresh: %w", err)
	}
	p.mu.Lock()
	p.lastHash = hash
	p.lastErr = ""
	p.mu.Unlock()
	return nil
}

func (p *Puller) dial(addr string) (*grpc.ClientConn, error) {
	var creds credentials.TransportCredentials
	if p.cfg.Insecure {
		creds = insecure.NewCredentials()
	} else {
		tc := &tls.Config{MinVersion: tls.VersionTLS12}
		if len(p.cfg.ServerCAPEM) > 0 {
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(p.cfg.ServerCAPEM) {
				return nil, errors.New("invalid CA PEM in Config.ServerCAPEM")
			}
			tc.RootCAs = pool
		}
		creds = credentials.NewTLS(tc)
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	return conn, nil
}

func (p *Puller) recordErr(err error) {
	p.mu.Lock()
	p.lastErr = err.Error()
	p.mu.Unlock()
}

// SetPullMode satisfies signagegrpc.PullController.
func (p *Puller) SetPullMode(enabled bool, contentAddr, deviceID string, intervalSec int) error {
	if enabled {
		if contentAddr == "" {
			return errors.New("content server address required when enabling")
		}
		if deviceID == "" {
			return errors.New("device id required when enabling")
		}
	}
	if intervalSec < 0 {
		return errors.New("interval must be non-negative")
	}
	if intervalSec == 0 {
		intervalSec = 60
	}
	p.mu.Lock()
	p.enabled = enabled
	p.contentAddr = contentAddr
	p.deviceID = deviceID
	p.interval = time.Duration(intervalSec) * time.Second
	p.lastErr = ""
	p.mu.Unlock()
	return nil
}

// GetPullMode satisfies signagegrpc.PullController.
func (p *Puller) GetPullMode() signagegrpc.PullModeStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return signagegrpc.PullModeStatus{
		Enabled:             p.enabled,
		ContentServerAddr:   p.contentAddr,
		DeviceID:            p.deviceID,
		PollIntervalSeconds: int(p.interval.Seconds()),
		LastPullUnix:        p.lastPullAt.Unix(),
		LastFrameHash:       p.lastHash,
		LastError:           p.lastErr,
	}
}

func translateMode(m pb.WaveformMode) signagelib.WaveformMode {
	switch m {
	case pb.WaveformMode_WAVEFORM_MODE_INIT:
		return signagelib.WaveformInit
	case pb.WaveformMode_WAVEFORM_MODE_DU:
		return signagelib.WaveformDU
	case pb.WaveformMode_WAVEFORM_MODE_GC16:
		return signagelib.WaveformGC16
	case pb.WaveformMode_WAVEFORM_MODE_GL16:
		return signagelib.WaveformGL16
	case pb.WaveformMode_WAVEFORM_MODE_GLR16:
		return signagelib.WaveformGLR16
	case pb.WaveformMode_WAVEFORM_MODE_GLD16:
		return signagelib.WaveformGLD16
	case pb.WaveformMode_WAVEFORM_MODE_A2:
		return signagelib.WaveformA2
	}
	return signagelib.WaveformGC16
}

var _ signagegrpc.PullController = (*Puller)(nil)

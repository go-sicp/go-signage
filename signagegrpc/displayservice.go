package signagegrpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/go-sicp/go-signage/signagelib"
	pb "github.com/go-sicp/go-signage/signageproto"
)

// DisplayService implements pb.DisplayServer on top of signagelib.Display
// and an optional PullController.
type DisplayService struct {
	pb.UnimplementedDisplayServer

	display       signagelib.Display
	pull          PullController
	cm            *CertManager
	serverVersion string
	startedAt     time.Time
	rotateRequest func()

	mu        sync.Mutex
	lastError string
}

func NewDisplayService(d signagelib.Display, p PullController, cm *CertManager, version string, rotateRequest func()) *DisplayService {
	return &DisplayService{
		display:       d,
		pull:          p,
		cm:            cm,
		serverVersion: version,
		startedAt:     time.Now(),
		rotateRequest: rotateRequest,
	}
}

func (s *DisplayService) GetInfo(ctx context.Context, _ *emptypb.Empty) (*pb.DisplayInfo, error) {
	if s.display == nil {
		return nil, status.Error(codes.FailedPrecondition, "display not opened")
	}
	info, err := s.display.Info()
	if err != nil {
		s.recordErr(err)
		return nil, status.Errorf(codes.Internal, "display.info: %v", err)
	}
	return &pb.DisplayInfo{
		Width:              info.Width(),
		Height:             info.Height(),
		ImageBufferAddress: info.ImageBufferAddress(),
		FirmwareVersion:    info.FirmwareVersion(),
		LutVersion:         info.LUTVersion(),
		AppVersion:         s.serverVersion,
	}, nil
}

func (s *DisplayService) Health(_ context.Context, _ *emptypb.Empty) (*pb.HealthResponse, error) {
	s.mu.Lock()
	last := s.lastError
	s.mu.Unlock()
	pullEnabled := false
	if s.pull != nil {
		pullEnabled = s.pull.GetPullMode().Enabled
	}
	return &pb.HealthResponse{
		DeviceOpen:      s.display != nil,
		LastError:       last,
		ServerVersion:   s.serverVersion,
		UptimeSeconds:   int64(time.Since(s.startedAt).Seconds()),
		PullModeEnabled: pullEnabled,
	}, nil
}

func (s *DisplayService) LoadImage(stream pb.Display_LoadImageServer) error {
	if s.display == nil {
		return status.Error(codes.FailedPrecondition, "display not opened")
	}
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	header, ok := first.GetPayload().(*pb.LoadImageChunk_Header)
	if !ok || header.Header == nil {
		return status.Error(codes.InvalidArgument, "first chunk must be LoadImageHeader")
	}
	if header.Header.TotalBytes <= 0 {
		return status.Error(codes.InvalidArgument, "total_bytes must be > 0")
	}
	bpp := header.Header.BitsPerPixel
	if bpp != 2 && bpp != 4 && bpp != 8 {
		return status.Errorf(codes.InvalidArgument, "bits_per_pixel %d invalid (2,4,8 supported by IT8951 LD_IMG_AREA)", bpp)
	}

	t0 := time.Now()
	buf := make([]byte, 0, header.Header.TotalBytes)
	hasher := sha256.New()
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		data, ok := chunk.GetPayload().(*pb.LoadImageChunk_Data)
		if !ok {
			return status.Error(codes.InvalidArgument, "expected data chunk after header")
		}
		if int64(len(buf)+len(data.Data)) > header.Header.TotalBytes {
			return status.Error(codes.InvalidArgument, "chunk overflows total_bytes")
		}
		buf = append(buf, data.Data...)
		hasher.Write(data.Data)
	}
	if int64(len(buf)) != header.Header.TotalBytes {
		return status.Errorf(codes.InvalidArgument, "got %d bytes, want %d", len(buf), header.Header.TotalBytes)
	}

	r := signagelib.Region{}
	if h := header.Header.Region; h != nil {
		r = signagelib.NewRegion(h.X, h.Y, h.W, h.H)
	}
	w, h := dimsFromHeader(header.Header, r, header.Header.TotalBytes, bpp)
	img := signagelib.NewPackedImage(buf, int8(bpp), w, h)
	if err := s.display.LoadImage(img, r); err != nil {
		s.recordErr(err)
		return status.Errorf(codes.Internal, "loadImage: %v", err)
	}
	return stream.SendAndClose(&pb.LoadImageResponse{
		FrameHash: hex.EncodeToString(hasher.Sum(nil)),
		ElapsedMs: time.Since(t0).Milliseconds(),
	})
}

// dimsFromHeader recovers (width, height) from the header. If a region is
// provided, that fixes the dimensions. Otherwise we assume a square-ish
// fallback which the IT8951 will fail loudly on if wrong.
func dimsFromHeader(h *pb.LoadImageHeader, r signagelib.Region, total int64, bpp int32) (int32, int32) {
	if !r.IsZero() {
		return r.W(), r.H()
	}
	pixels := total * 8 / int64(bpp)
	side := int32(1)
	for side*side < int32(pixels) {
		side++
	}
	return side, side
}

func (s *DisplayService) Refresh(_ context.Context, req *pb.RefreshRequest) (*pb.RefreshResponse, error) {
	if s.display == nil {
		return nil, status.Error(codes.FailedPrecondition, "display not opened")
	}
	r := signagelib.Region{}
	if h := req.Region; h != nil {
		r = signagelib.NewRegion(h.X, h.Y, h.W, h.H)
	}
	t0 := time.Now()
	if err := s.display.Refresh(translateMode(req.Mode), r); err != nil {
		s.recordErr(err)
		return nil, status.Errorf(codes.Internal, "refresh: %v", err)
	}
	return &pb.RefreshResponse{ElapsedMs: time.Since(t0).Milliseconds()}, nil
}

func (s *DisplayService) Sleep(_ context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	if s.display == nil {
		return nil, status.Error(codes.FailedPrecondition, "display not opened")
	}
	if err := s.display.Sleep(); err != nil {
		s.recordErr(err)
		return nil, status.Errorf(codes.Internal, "sleep: %v", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *DisplayService) Standby(_ context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	if s.display == nil {
		return nil, status.Error(codes.FailedPrecondition, "display not opened")
	}
	if err := s.display.Standby(); err != nil {
		s.recordErr(err)
		return nil, status.Errorf(codes.Internal, "standby: %v", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *DisplayService) SetPullMode(_ context.Context, req *pb.SetPullModeRequest) (*emptypb.Empty, error) {
	if s.pull == nil {
		return nil, status.Error(codes.FailedPrecondition, "pull controller not configured")
	}
	if err := s.pull.SetPullMode(req.Enabled, req.ContentServerAddr, req.DeviceId, int(req.PollIntervalSeconds)); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "setPullMode: %v", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *DisplayService) GetPullMode(_ context.Context, _ *emptypb.Empty) (*pb.PullModeStatus, error) {
	if s.pull == nil {
		return nil, status.Error(codes.FailedPrecondition, "pull controller not configured")
	}
	st := s.pull.GetPullMode()
	return &pb.PullModeStatus{
		Enabled:             st.Enabled,
		ContentServerAddr:   st.ContentServerAddr,
		DeviceId:            st.DeviceID,
		PollIntervalSeconds: int32(st.PollIntervalSeconds),
		LastPullUnix:        st.LastPullUnix,
		LastFrameHash:       st.LastFrameHash,
		LastError:           st.LastError,
	}, nil
}

func (s *DisplayService) Rotate(_ context.Context, _ *emptypb.Empty) (*pb.RotateResponse, error) {
	if err := s.cm.Rotate(); err != nil {
		return nil, status.Errorf(codes.Internal, "rotate: %v", err)
	}
	at := time.Now().UnixMilli()
	if s.rotateRequest != nil {
		go s.rotateRequest()
	}
	return &pb.RotateResponse{ScheduledAtMs: at}, nil
}

func (s *DisplayService) ReissueClientCert(_ context.Context, _ *emptypb.Empty) (*pb.ReissueClientCertResponse, error) {
	if err := s.cm.ReissueClientCert(); err != nil {
		return nil, status.Errorf(codes.Internal, "reissue: %v", err)
	}
	resp := &pb.ReissueClientCertResponse{
		ClientCertPem:         string(s.cm.ClientCertPEM()),
		ClientKeyPem:          string(s.cm.ClientKeyPEM()),
		ClientCertFingerprint: s.cm.ClientFingerprint(),
		ServerRestartInMs:     2000,
	}
	if s.rotateRequest != nil {
		go func() {
			time.Sleep(2 * time.Second)
			s.rotateRequest()
		}()
	}
	return resp, nil
}

func (s *DisplayService) GetMetrics(_ context.Context, _ *emptypb.Empty) (*pb.GetMetricsResponse, error) {
	out := fmt.Sprintf(
		"# HELP signage_uptime_seconds Server uptime in seconds.\n"+
			"# TYPE signage_uptime_seconds counter\n"+
			"signage_uptime_seconds %d\n"+
			"# HELP signage_device_open Whether the IT8951 controller is opened.\n"+
			"# TYPE signage_device_open gauge\n"+
			"signage_device_open %d\n",
		int64(time.Since(s.startedAt).Seconds()),
		boolToInt(s.display != nil),
	)
	return &pb.GetMetricsResponse{PromText: out}, nil
}

func (s *DisplayService) recordErr(err error) {
	s.mu.Lock()
	s.lastError = err.Error()
	s.mu.Unlock()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
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

var _ pb.DisplayServer = (*DisplayService)(nil)

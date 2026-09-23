package signagecontent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/go-sicp/go-signage/signageproto"
)

// Service implements pb.ContentServer. It owns a Store and a Renderer.
type Service struct {
	pb.UnimplementedContentServer

	store    Store
	renderer Renderer
	clock    func() time.Time

	cacheMu sync.RWMutex
	cache   map[string]cachedFrame
}

type cachedFrame struct {
	png        []byte
	hash       string
	renderedAt time.Time
}

// NewService binds a Store and a Renderer to the gRPC service.
func NewService(store Store, renderer Renderer) *Service {
	return &Service{
		store:    store,
		renderer: renderer,
		clock:    time.Now,
		cache:    map[string]cachedFrame{},
	}
}

// renderTTL caps how long a cached frame stays fresh between full re-
// renders. Tuned for signage where content changes on the order of
// minutes; faster cadences are obtained by lowering this.
const renderTTL = 30 * time.Second

func (s *Service) GetFrame(ctx context.Context, req *pb.GetFrameRequest) (*pb.GetFrameResponse, error) {
	dev, ok := s.store.Get(req.DeviceId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "device %q not registered", req.DeviceId)
	}
	w, h := dev.Width, dev.Height
	if req.WidthHint > 0 && req.HeightHint > 0 {
		w, h = int(req.WidthHint), int(req.HeightHint)
	}

	hashed, png, err := s.frameFor(ctx, dev, w, h)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "render: %v", err)
	}
	if hashed == req.CurrentFrameHash {
		return &pb.GetFrameResponse{
			Unchanged:       true,
			FrameHash:       hashed,
			NextPollSeconds: int32(dev.PollIntervalSeconds),
			SuggestedMode:   dev.SuggestedMode,
		}, nil
	}
	return &pb.GetFrameResponse{
		Unchanged:       false,
		FrameHash:       hashed,
		Png:             png,
		NextPollSeconds: int32(dev.PollIntervalSeconds),
		SuggestedMode:   dev.SuggestedMode,
	}, nil
}

func (s *Service) frameFor(ctx context.Context, dev DeviceConfig, w, h int) (string, []byte, error) {
	s.cacheMu.RLock()
	cached, ok := s.cache[dev.ID]
	s.cacheMu.RUnlock()
	if ok && s.clock().Sub(cached.renderedAt) < renderTTL {
		return cached.hash, cached.png, nil
	}
	png, err := s.renderer.RenderURL(ctx, dev.URL, w, h)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(png)
	hash := hex.EncodeToString(sum[:])
	s.cacheMu.Lock()
	s.cache[dev.ID] = cachedFrame{png: png, hash: hash, renderedAt: s.clock()}
	s.cacheMu.Unlock()
	return hash, png, nil
}

func (s *Service) ReportStatus(_ context.Context, req *pb.ReportStatusRequest) (*pb.ReportStatusResponse, error) {
	dev, ok := s.store.Get(req.DeviceId)
	next := int32(60)
	if ok && dev.PollIntervalSeconds > 0 {
		next = int32(dev.PollIntervalSeconds)
	}
	return &pb.ReportStatusResponse{NextReportSeconds: next}, nil
}

var _ pb.ContentServer = (*Service)(nil)

package signagecontent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	pb "github.com/go-sicp/go-signage/signageproto"
)

// DeviceConfig drives the rendering for a single Pi.
type DeviceConfig struct {
	ID                  string          `json:"id"`
	Name                string          `json:"name"`
	URL                 string          `json:"url"`
	Width               int             `json:"width"`
	Height              int             `json:"height"`
	PollIntervalSeconds int             `json:"poll_interval_seconds"`
	SuggestedMode       pb.WaveformMode `json:"-"`
}

// Store is the device-config registry. The default in-memory implementation
// is enough for small fleets; a SQL-backed implementation could be plugged
// in later by satisfying this interface.
type Store interface {
	Get(id string) (DeviceConfig, bool)
	Put(cfg DeviceConfig) error
	List() []DeviceConfig
}

type memStore struct {
	mu      sync.RWMutex
	devices map[string]DeviceConfig
}

func NewMemStore() Store { return &memStore{devices: map[string]DeviceConfig{}} }

func (s *memStore) Get(id string) (DeviceConfig, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.devices[id]
	return c, ok
}

func (s *memStore) Put(cfg DeviceConfig) error {
	if cfg.ID == "" {
		return errors.New("device id required")
	}
	if cfg.URL == "" {
		return errors.New("device url required")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return errors.New("device width/height required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.devices[cfg.ID] = cfg
	return nil
}

func (s *memStore) List() []DeviceConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]DeviceConfig, 0, len(s.devices))
	for _, c := range s.devices {
		out = append(out, c)
	}
	return out
}

// LoadStoreFromJSON reads a JSON file shaped like {"devices":[{...}]} and
// pre-populates a memStore. Missing file is a no-op (returns empty store).
func LoadStoreFromJSON(path string) (Store, error) {
	s := NewMemStore()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var doc struct {
		Devices []DeviceConfig `json:"devices"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for _, c := range doc.Devices {
		if c.PollIntervalSeconds == 0 {
			c.PollIntervalSeconds = 60
		}
		if c.SuggestedMode == pb.WaveformMode_WAVEFORM_MODE_UNSPECIFIED {
			c.SuggestedMode = pb.WaveformMode_WAVEFORM_MODE_GC16
		}
		if err := s.Put(c); err != nil {
			return nil, fmt.Errorf("device %s: %w", c.ID, err)
		}
	}
	return s, nil
}

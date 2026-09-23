package signagecontent

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/go-sicp/go-signage/signageproto"
)

// Config is the construction-time settings of the central server.
type Config struct {
	Service       *Service
	Port          int
	TLSCertPEM    []byte // optional; empty = plaintext (dev only)
	TLSKeyPEM     []byte
	MaxRecvBytes  int
	ServerVersion string
}

const (
	DefaultPort         = 50061
	DefaultMaxRecvBytes = 16 * 1024 * 1024
)

type Server struct {
	cfg     Config
	mu      sync.Mutex
	srv     *grpc.Server
	lis     net.Listener
	running atomic.Bool
	port    int
}

func New(cfg Config) (*Server, error) {
	if cfg.Service == nil {
		return nil, errors.New("signagecontent: Config.Service is required")
	}
	if cfg.Port == 0 {
		cfg.Port = DefaultPort
	}
	if cfg.MaxRecvBytes == 0 {
		cfg.MaxRecvBytes = DefaultMaxRecvBytes
	}
	if cfg.ServerVersion == "" {
		cfg.ServerVersion = "0.1.0"
	}
	return &Server{cfg: cfg}, nil
}

func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running.Load() {
		return errors.New("signagecontent: server already running")
	}

	var creds credentials.TransportCredentials
	if len(s.cfg.TLSCertPEM) == 0 {
		creds = insecure.NewCredentials()
	} else {
		cer, err := tls.X509KeyPair(s.cfg.TLSCertPEM, s.cfg.TLSKeyPEM)
		if err != nil {
			return fmt.Errorf("signagecontent: load keypair: %w", err)
		}
		creds = credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{cer},
			MinVersion:   tls.VersionTLS12,
		})
	}

	srv := grpc.NewServer(
		grpc.Creds(creds),
		grpc.MaxRecvMsgSize(s.cfg.MaxRecvBytes),
	)
	pb.RegisterContentServer(srv, s.cfg.Service)

	addr := fmt.Sprintf(":%d", s.cfg.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("signagecontent: listen %s: %w", addr, err)
	}
	if tcp, ok := lis.Addr().(*net.TCPAddr); ok {
		s.port = tcp.Port
	}
	s.srv = srv
	s.lis = lis
	s.running.Store(true)
	go func() {
		_ = srv.Serve(lis)
	}()
	return nil
}

func (s *Server) Stop() error {
	s.mu.Lock()
	srv := s.srv
	s.srv = nil
	s.mu.Unlock()
	if !s.running.Load() {
		return nil
	}
	s.running.Store(false)
	if srv != nil {
		stopped := make(chan struct{})
		go func() {
			srv.GracefulStop()
			close(stopped)
		}()
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			srv.Stop()
		}
	}
	return nil
}

func (s *Server) ListeningPort() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

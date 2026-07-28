package afv

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/renorris/openfsd/internal/db"
)

// Server is the AFV REST + UDP voice service.
type Server struct {
	cfg       *Config
	users     db.UserRepository
	configKV  db.ConfigRepository
	jwtSecret []byte
	reg       *Registry
	authFail  *authFailLimiter

	httpServer *http.Server
	udpConn    net.PacketConn
	udpMu      sync.Mutex

	// apiAddr is the actual bound HTTP address (set after listen; :0 safe).
	apiMu   sync.RWMutex
	apiAddr string
}

// New constructs an AFV server from already-opened dependencies (tests + NewDefault).
func New(cfg *Config, users db.UserRepository, configKV db.ConfigRepository, jwtSecret []byte) *Server {
	if cfg == nil {
		cfg = &Config{}
	}
	return &Server{
		cfg:       cfg,
		users:     users,
		configKV:  configKV,
		jwtSecret: jwtSecret,
		reg:       newRegistry(cfg),
		authFail:  newAuthFailLimiter(cfg.AuthFailMax, cfg.AuthFailWindow),
	}
}

// Run starts HTTP API, UDP voice, and reaper until ctx is cancelled.
// If any subsystem fails early, siblings are cancelled so Run returns.
func (s *Server) Run(ctx context.Context) error {
	if s.cfg.UDPAdvertiseIPv4 == "" {
		return fmt.Errorf("AFV_UDP_ADVERTISE_IPV4 is required")
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	mux := s.routes()
	s.httpServer = &http.Server{
		Addr:              s.cfg.APIListen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 3)
	var wg sync.WaitGroup

	// HTTP
	wg.Add(1)
	go func() {
		defer wg.Done()
		ln, err := net.Listen("tcp", s.cfg.APIListen)
		if err != nil {
			errCh <- fmt.Errorf("afv api listen: %w", err)
			cancel()
			return
		}
		bound := ln.Addr().String()
		slog.Info("AFV API listening", "addr", bound)
		s.apiMu.Lock()
		s.apiAddr = bound
		s.apiMu.Unlock()

		go func() {
			<-runCtx.Done()
			shctx, shCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shCancel()
			_ = s.httpServer.Shutdown(shctx)
		}()

		var serveErr error
		if s.cfg.TLSCertFile != "" && s.cfg.TLSKeyFile != "" {
			serveErr = s.httpServer.ServeTLS(ln, s.cfg.TLSCertFile, s.cfg.TLSKeyFile)
		} else {
			serveErr = s.httpServer.Serve(ln)
		}
		if serveErr != nil && serveErr != http.ErrServerClosed {
			errCh <- serveErr
			cancel()
			return
		}
		errCh <- nil
	}()

	// UDP
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := s.runUDP(runCtx)
		if err != nil && runCtx.Err() == nil {
			errCh <- err
			cancel()
			return
		}
		errCh <- nil
	}()

	// Reaper
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.runReaper(runCtx)
		errCh <- nil
	}()

	var first error
	for i := 0; i < 3; i++ {
		if err := <-errCh; err != nil && first == nil {
			first = err
			cancel()
		}
	}
	// Ensure UDP is closed if still open (reaper/http already exit on cancel).
	s.udpMu.Lock()
	if s.udpConn != nil {
		_ = s.udpConn.Close()
	}
	s.udpMu.Unlock()
	wg.Wait()
	return first
}

// APIAddr returns the HTTP listen address (updated after bind when using :0).
func (s *Server) APIAddr() string {
	s.apiMu.RLock()
	defer s.apiMu.RUnlock()
	if s.apiAddr != "" {
		return s.apiAddr
	}
	return s.cfg.APIListen
}

// Registry returns the session registry (tests).
func (s *Server) Registry() *Registry {
	return s.reg
}

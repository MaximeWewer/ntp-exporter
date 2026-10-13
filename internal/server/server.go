package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/logger"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/exporter-toolkit/web"
)

// Server represents the HTTP server
type Server struct {
	config    *config.Config
	registry  *prometheus.Registry
	readiness ReadinessFunc
	probe     http.Handler
	server    *http.Server
}

// New creates a new HTTP server
func New(cfg *config.Config, registry *prometheus.Registry, readiness ReadinessFunc) *Server {
	return &Server{
		config:    cfg,
		registry:  registry,
		readiness: readiness,
	}
}

// SetProbeHandler serves the multi-target /probe endpoint
func (s *Server) SetProbeHandler(h http.Handler) {
	s.probe = h
}

// Handler returns the HTTP handler with every route and middleware
func (s *Server) Handler() http.Handler {
	handlers := NewHandlers(s.config, s.registry, s.readiness)

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", handlers.MetricsHandler)
	mux.HandleFunc("/healthz", handlers.LivenessHandler)
	mux.HandleFunc("/readyz", handlers.ReadinessHandler)
	if s.probe != nil {
		mux.Handle("/probe", s.probe)
	}
	mux.HandleFunc("/", handlers.IndexHandler)

	return withMiddleware(mux)
}

// Start starts the HTTP server
func (s *Server) Start(ctx context.Context) error {
	handler := s.Handler()

	// Configure server
	addr := net.JoinHostPort(s.config.Server.Address, strconv.Itoa(s.config.Server.Port))
	s.server = &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadTimeout:       s.config.Server.ReadTimeout,
		ReadHeaderTimeout: s.config.Server.ReadTimeout,
		WriteTimeout:      s.config.Server.WriteTimeout,
		// Requests in flight (a /probe burst) end with the exporter
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	errChan := make(chan error, 1)
	// Standard Prometheus web configuration (TLS, basic auth, headers)
	// https://prometheus.io/docs/prometheus/latest/configuration/https/
	listen := []string{addr}
	systemd := false
	webConfig := s.config.Server.WebConfigFile
	flags := &web.FlagConfig{WebListenAddresses: &listen, WebSystemdSocket: &systemd, WebConfigFile: &webConfig}
	// exporter-toolkit logs the listen address with the logger of the exporter
	go func() { errChan <- web.ListenAndServe(s.server, flags, logger.Slog().With("package", "server")) }()

	// Wait for context cancellation or server error
	select {
	case <-ctx.Done():
		logger.Info("server", "Shutting down HTTP server")
		return s.Shutdown(context.Background())
	case err := <-errChan:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP server failed on %s: %w", s.server.Addr, err)
		}
		return nil
	}
}

// Shutdown gracefully shuts down the HTTP server
func (s *Server) Shutdown(ctx context.Context) error {
	if s.server == nil {
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := s.server.Shutdown(shutdownCtx); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("server shutdown timeout after 10s: %w", err)
		}
		return fmt.Errorf("server shutdown failed: %w", err)
	}

	logger.Info("server", "HTTP server stopped")
	return nil
}

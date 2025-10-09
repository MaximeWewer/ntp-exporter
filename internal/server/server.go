package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/pkg/logger"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

// Server represents the HTTP server
type Server struct {
	config   *config.Config
	registry *prometheus.Registry
	metrics  *metrics.NTPMetrics
	server   *http.Server
}

// New creates a new HTTP server
func New(cfg *config.Config, registry *prometheus.Registry, m *metrics.NTPMetrics) *Server {
	return &Server{
		config:   cfg,
		registry: registry,
		metrics:  m,
	}
}

// Start starts the HTTP server
func (s *Server) Start(ctx context.Context) error {
	// Create router
	mux := http.NewServeMux()

	// Register handlers
	handlers := NewHandlers(s.config, s.registry)

	mux.HandleFunc("/metrics", handlers.MetricsHandler)
	mux.HandleFunc("/health", handlers.HealthHandler)
	mux.HandleFunc("/", handlers.IndexHandler)

	// Apply middleware
	middleware := NewMiddleware(s.config, s.metrics)
	handler := middleware.Apply(mux)

	// Configure server
	addr := s.config.Server.Address + ":" + strconv.Itoa(s.config.Server.Port)
	s.server = &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  s.config.Server.ReadTimeout,
		WriteTimeout: s.config.Server.WriteTimeout,
	}

	// Configure TLS if enabled
	if s.config.Server.TLSEnabled {
		s.server.TLSConfig = createSecureTLSConfig()
		logger.Infof("server", "Starting HTTPS server on %s with TLS 1.2+", addr)
	} else {
		logger.Infof("server", "Starting HTTP server on %s", addr)
	}

	// Start server
	errChan := make(chan error, 1)
	go func() {
		if s.config.Server.TLSEnabled {
			errChan <- s.server.ListenAndServeTLS(
				s.config.Server.TLSCertFile,
				s.config.Server.TLSKeyFile,
			)
		} else {
			errChan <- s.server.ListenAndServe()
		}
	}()

	// Wait for context cancellation or server error
	select {
	case <-ctx.Done():
		logger.Info("server", "Shutting down HTTP server")
		return s.Shutdown(context.Background())
	case err := <-errChan:
		if err != nil && err != http.ErrServerClosed {
			logger.Error("server", "Server error", err)
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
		logger.Error("server", "Server shutdown failed", err)
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("server shutdown timeout after 10s: %w", err)
		}
		return fmt.Errorf("server shutdown failed: %w", err)
	}

	logger.Info("server", "HTTP server stopped")
	return nil
}

// createSecureTLSConfig creates a secure TLS configuration
// Following best practices for TLS 1.2+ with secure cipher suites
func createSecureTLSConfig() *tls.Config {
	return &tls.Config{
		// Minimum TLS version 1.2
		MinVersion: tls.VersionTLS12,

		// Preferred elliptic curves for ECDHE
		CurvePreferences: []tls.CurveID{
			tls.CurveP521,
			tls.CurveP384,
			tls.CurveP256,
		},

		// Prefer server cipher suites
		PreferServerCipherSuites: true,

		// Secure cipher suites (TLS 1.2+)
		// Prioritize ECDHE for Perfect Forward Secrecy
		CipherSuites: []uint16{
			// TLS 1.3 cipher suites (automatically used if available)

			// TLS 1.2 cipher suites with ECDHE (PFS)
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,

			// ChaCha20-Poly1305 (modern, fast on ARM)
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
		},
	}
}

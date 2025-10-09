package server

import (
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/pkg/logger"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
)

// Middleware manages HTTP middleware
type Middleware struct {
	config  *config.Config
	metrics *metrics.NTPMetrics
}

// NewMiddleware creates a new middleware instance
func NewMiddleware(cfg *config.Config, m *metrics.NTPMetrics) *Middleware {
	return &Middleware{
		config:  cfg,
		metrics: m,
	}
}

// Apply applies all middleware to the handler
func (m *Middleware) Apply(next http.Handler) http.Handler {
	handler := next

	// Apply middleware in reverse order (they wrap each other)
	handler = m.recoveryMiddleware(handler)
	handler = m.metricsMiddleware(handler)
	handler = m.loggingMiddleware(handler)

	if m.config.Server.EnableCORS {
		handler = m.corsMiddleware(handler)
	}

	return handler
}

// loggingMiddleware logs HTTP requests
func (m *Middleware) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create response writer wrapper to capture status code
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(rw, r)

		// Use logger.HTTP function
		logger.HTTP(r.Method, r.URL.Path, rw.statusCode, time.Since(start), r.RemoteAddr)
	})
}

// metricsMiddleware updates runtime metrics
func (m *Middleware) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Update runtime metrics
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)

		// Basic memory metrics
		m.metrics.ExporterMemoryUsageBytes.Set(float64(memStats.Alloc))
		m.metrics.ExporterGoroutinesCount.Set(float64(runtime.NumGoroutine()))

		// Advanced memory metrics
		m.metrics.MemoryAllocatedBytes.Set(float64(memStats.TotalAlloc))
		m.metrics.MemoryHeapBytes.Set(float64(memStats.HeapInuse))
		m.metrics.MemoryStackBytes.Set(float64(memStats.StackInuse))

		// GC metrics
		if memStats.NumGC > 0 {
			// GC pause time in seconds
			gcPause := float64(memStats.PauseNs[(memStats.NumGC+255)%256]) / 1e9
			m.metrics.GCDurationSeconds.Observe(gcPause)
		}

		next.ServeHTTP(w, r)
	})
}

// corsMiddleware adds CORS headers
func (m *Middleware) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Validate origin against whitelist
		if m.isAllowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Max-Age", "3600")
		} else if origin != "" {
			// Log blocked origins for monitoring
			logger.SafeWarn("security", "CORS request blocked", map[string]interface{}{
				"origin": origin,
				"path":   r.URL.Path,
			})
		}

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// isAllowedOrigin checks if the origin is in the whitelist
func (m *Middleware) isAllowedOrigin(origin string) bool {
	// If list empty, no CORS
	if len(m.config.Server.AllowedOrigins) == 0 {
		return false
	}

	// Check exact match
	for _, allowed := range m.config.Server.AllowedOrigins {
		if allowed == origin {
			return true
		}

		// Support wildcard subdomain: *.example.com
		if strings.HasPrefix(allowed, "*.") {
			domain := allowed[2:]
			if strings.HasSuffix(origin, "."+domain) || origin == "https://"+domain {
				return true
			}
		}
	}

	return false
}

// recoveryMiddleware recovers from panics
func (m *Middleware) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				logger.SafeError("server", "Panic recovered", nil, map[string]interface{}{
					"panic":  err,
					"method": r.Method,
					"path":   r.URL.Path,
				})

				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error":"internal server error"}`))
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

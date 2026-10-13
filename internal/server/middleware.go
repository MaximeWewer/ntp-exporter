package server

import (
	"net/http"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/logger"
)

// withMiddleware wraps the handler with request logging and panic recovery
func withMiddleware(next http.Handler) http.Handler {
	return loggingMiddleware(recoveryMiddleware(next))
}

// loggingMiddleware logs HTTP requests
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)
		logger.HTTP(r.Method, r.URL.Path, rw.statusCode, time.Since(start), r.RemoteAddr)
	})
}

// recoveryMiddleware recovers from panics
func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				logger.Error("server", "Panic recovered", nil,
					"panic", err,
					"method", r.Method,
					"path", r.URL.Path,
				)
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"internal server error"}`))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// responseWriter wraps http.ResponseWriter to capture the status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

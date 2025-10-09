package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
	"github.com/stretchr/testify/assert"
)

func TestNewMiddleware(t *testing.T) {
	cfg := &config.Config{}

	m := metrics.NewNTPMetrics()
	mw := NewMiddleware(cfg, m)

	assert.NotNil(t, mw)
	assert.NotNil(t, mw.config)
}

func TestMiddleware_Apply(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			EnableCORS: false,
		},
	}
	m := metrics.NewNTPMetrics()
	mw := NewMiddleware(cfg, m)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test"))
	})

	wrapped := mw.Apply(handler)

	assert.NotNil(t, wrapped)
}

func TestMiddleware_Apply_WithCORS(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			EnableCORS:     true,
			AllowedOrigins: []string{"*"},
		},
	}
	m := metrics.NewNTPMetrics()
	mw := NewMiddleware(cfg, m)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := mw.Apply(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "*")
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, w.Header().Get("Access-Control-Allow-Methods"), "GET")
}

func TestMiddleware_LoggingMiddleware(t *testing.T) {
	cfg := &config.Config{}
	m := metrics.NewNTPMetrics()
	mw := NewMiddleware(cfg, m)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	wrapped := mw.loggingMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "*")
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "success", w.Body.String())
}

func TestMiddleware_LoggingMiddleware_StatusCodes(t *testing.T) {
	cfg := &config.Config{}
	m := metrics.NewNTPMetrics()
	mw := NewMiddleware(cfg, m)

	tests := []struct {
		name           string
		statusCode     int
		expectedStatus int
	}{
		{"ok", http.StatusOK, http.StatusOK},
		{"not_found", http.StatusNotFound, http.StatusNotFound},
		{"internal_error", http.StatusInternalServerError, http.StatusInternalServerError},
		{"created", http.StatusCreated, http.StatusCreated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			})

			wrapped := mw.loggingMiddleware(handler)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Origin", "*")
			w := httptest.NewRecorder()

			wrapped.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestMiddleware_MetricsMiddleware(t *testing.T) {
	cfg := &config.Config{}
	m := metrics.NewNTPMetrics()
	mw := NewMiddleware(cfg, m)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := mw.metricsMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "*")
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	// Metrics should be updated but we can't easily test the values
}

func TestMiddleware_CORSMiddleware(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			EnableCORS:     true,
			AllowedOrigins: []string{"https://example.com"},
		},
	}
	m := metrics.NewNTPMetrics()
	mw := NewMiddleware(cfg, m)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := mw.corsMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "https://example.com", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, w.Header().Get("Access-Control-Allow-Methods"), "GET")
	assert.Equal(t, "Content-Type", w.Header().Get("Access-Control-Allow-Headers"))
}

func TestMiddleware_CORSMiddleware_OPTIONS(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			EnableCORS:     true,
			AllowedOrigins: []string{"https://example.com"},
		},
	}
	m := metrics.NewNTPMetrics()
	mw := NewMiddleware(cfg, m)

	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	wrapped := mw.corsMiddleware(handler)

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.False(t, handlerCalled, "Handler should not be called for OPTIONS")
	assert.Equal(t, "https://example.com", w.Header().Get("Access-Control-Allow-Origin"))
}

func TestMiddleware_RecoveryMiddleware(t *testing.T) {
	cfg := &config.Config{}
	m := metrics.NewNTPMetrics()
	mw := NewMiddleware(cfg, m)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	wrapped := mw.recoveryMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "*")
	w := httptest.NewRecorder()

	// Should not panic
	assert.NotPanics(t, func() {
		wrapped.ServeHTTP(w, req)
	})

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "internal server error")
}

func TestMiddleware_RecoveryMiddleware_NoError(t *testing.T) {
	cfg := &config.Config{}
	m := metrics.NewNTPMetrics()
	mw := NewMiddleware(cfg, m)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	wrapped := mw.recoveryMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "*")
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "success", w.Body.String())
}

func TestResponseWriter_WriteHeader(t *testing.T) {
	w := httptest.NewRecorder()
	rw := &responseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}

	rw.WriteHeader(http.StatusNotFound)

	assert.Equal(t, http.StatusNotFound, rw.statusCode)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestResponseWriter_DefaultStatusCode(t *testing.T) {
	w := httptest.NewRecorder()
	rw := &responseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}

	// Write without calling WriteHeader
	rw.Write([]byte("test"))

	assert.Equal(t, http.StatusOK, rw.statusCode)
}

func TestMiddleware_FullStack(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			EnableCORS:     true,
			AllowedOrigins: []string{"*"},
		},
	}
	m := metrics.NewNTPMetrics()
	mw := NewMiddleware(cfg, m)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	})

	wrapped := mw.Apply(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "*")
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "test response", w.Body.String())
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
}

func TestMiddleware_ChainOrder(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			EnableCORS: false,
		},
	}
	m := metrics.NewNTPMetrics()
	mw := NewMiddleware(cfg, m)

	callOrder := []string{}

	// Create handler that tracks middleware execution order
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callOrder = append(callOrder, "handler")
		w.WriteHeader(http.StatusOK)
	})

	// Manually apply middleware in order to track execution
	h1 := mw.loggingMiddleware(handler)
	h2 := mw.metricsMiddleware(h1)
	h3 := mw.recoveryMiddleware(h2)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "*")
	w := httptest.NewRecorder()

	h3.ServeHTTP(w, req)

	// Handler should be called
	assert.Contains(t, callOrder, "handler")
}

func TestMiddleware_ConcurrentRequests(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			EnableCORS:     true,
			AllowedOrigins: []string{"*"},
		},
	}
	m := metrics.NewNTPMetrics()
	mw := NewMiddleware(cfg, m)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	wrapped := mw.Apply(handler)

	concurrency := 50
	done := make(chan bool, concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Origin", "*")
			w := httptest.NewRecorder()

			wrapped.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			done <- true
		}()
	}

	for i := 0; i < concurrency; i++ {
		<-done
	}
}

func BenchmarkMiddleware_Apply(b *testing.B) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			EnableCORS:     true,
			AllowedOrigins: []string{"*"},
		},
	}
	m := metrics.NewNTPMetrics()
	mw := NewMiddleware(cfg, m)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := mw.Apply(handler)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "*")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, req)
	}
}

func BenchmarkMiddleware_LoggingMiddleware(b *testing.B) {
	cfg := &config.Config{}
	m := metrics.NewNTPMetrics()
	mw := NewMiddleware(cfg, m)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := mw.loggingMiddleware(handler)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "*")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, req)
	}
}

func BenchmarkMiddleware_RecoveryMiddleware(b *testing.B) {
	cfg := &config.Config{}
	m := metrics.NewNTPMetrics()
	mw := NewMiddleware(cfg, m)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := mw.recoveryMiddleware(handler)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "*")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, req)
	}
}

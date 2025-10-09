package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

func TestNewHandlers(t *testing.T) {
	cfg := &config.Config{}
	registry := prometheus.NewRegistry()

	handlers := NewHandlers(cfg, registry)

	assert.NotNil(t, handlers)
	assert.NotNil(t, handlers.config)
	assert.NotNil(t, handlers.registry)
}

func TestHandlers_MetricsHandler(t *testing.T) {
	cfg := &config.Config{}
	registry := prometheus.NewRegistry()

	// Register a test metric
	testGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "test_metric",
		Help: "Test metric",
	})
	registry.MustRegister(testGauge)
	testGauge.Set(42)

	handlers := NewHandlers(cfg, registry)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()

	handlers.MetricsHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, w.Body.String(), "test_metric")
	assert.Contains(t, w.Body.String(), "42")
}

func TestHandlers_MetricsHandler_EmptyRegistry(t *testing.T) {
	cfg := &config.Config{}
	registry := prometheus.NewRegistry()
	handlers := NewHandlers(cfg, registry)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()

	handlers.MetricsHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	// Empty registry will return empty or minimal output
	// Just check that handler doesn't crash
	_ = w.Body.String()
}

func TestHandlers_HealthHandler(t *testing.T) {
	cfg := &config.Config{}
	registry := prometheus.NewRegistry()
	handlers := NewHandlers(cfg, registry)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	handlers.HealthHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	assert.Contains(t, w.Body.String(), "healthy")
	assert.Contains(t, w.Body.String(), "ntp-exporter")
}

func TestHandlers_HealthHandler_JSON(t *testing.T) {
	cfg := &config.Config{}
	registry := prometheus.NewRegistry()
	handlers := NewHandlers(cfg, registry)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	handlers.HealthHandler(w, req)

	body := w.Body.String()
	assert.Contains(t, body, `"status":"healthy"`)
	assert.Contains(t, body, `"service":"ntp-exporter"`)
}

func TestHandlers_IndexHandler(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers:          []string{"pool.ntp.org", "time.google.com"},
			Pools:            []config.PoolConfig{{Name: "pool.ntp.org"}},
			SamplesPerServer: 3,
			Timeout:          5 * time.Second,
			Version:          4,
		},
	}
	registry := prometheus.NewRegistry()
	handlers := NewHandlers(cfg, registry)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handlers.IndexHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/html", resp.Header.Get("Content-Type"))

	body := w.Body.String()
	assert.Contains(t, body, "NTP Prometheus Exporter")
	assert.Contains(t, body, "/metrics")
	assert.Contains(t, body, "/health")
	assert.Contains(t, body, "2")  // Number of servers
	assert.Contains(t, body, "1")  // Number of pools
	assert.Contains(t, body, "3")  // Samples per server
	assert.Contains(t, body, "5s") // Timeout
	assert.Contains(t, body, "4")  // NTP version
}

func TestHandlers_IndexHandler_NotFound(t *testing.T) {
	cfg := &config.Config{}
	registry := prometheus.NewRegistry()
	handlers := NewHandlers(cfg, registry)

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()

	handlers.IndexHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHandlers_IndexHandler_RootPath(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers: []string{"pool.ntp.org"},
			Timeout: 5 * time.Second,
			Version: 4,
		},
	}
	registry := prometheus.NewRegistry()
	handlers := NewHandlers(cfg, registry)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handlers.IndexHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "NTP Prometheus Exporter")
}

func TestLoggerAdapter_Println(t *testing.T) {
	adapter := &loggerAdapter{}

	// Should not panic
	assert.NotPanics(t, func() {
		adapter.Println("test message")
	})

	assert.NotPanics(t, func() {
		adapter.Println("test", "multiple", "args")
	})

	assert.NotPanics(t, func() {
		adapter.Println(assert.AnError)
	})
}

func TestHandlers_AllEndpoints(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers: []string{"pool.ntp.org"},
			Timeout: 5 * time.Second,
			Version: 4,
		},
	}
	registry := prometheus.NewRegistry()
	handlers := NewHandlers(cfg, registry)

	tests := []struct {
		name           string
		path           string
		handler        http.HandlerFunc
		expectedStatus int
		expectedType   string
	}{
		{
			name:           "metrics",
			path:           "/metrics",
			handler:        handlers.MetricsHandler,
			expectedStatus: http.StatusOK,
			expectedType:   "",
		},
		{
			name:           "health",
			path:           "/health",
			handler:        handlers.HealthHandler,
			expectedStatus: http.StatusOK,
			expectedType:   "application/json",
		},
		{
			name:           "index",
			path:           "/",
			handler:        handlers.IndexHandler,
			expectedStatus: http.StatusOK,
			expectedType:   "text/html",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			tt.handler(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.expectedType != "" {
				assert.Equal(t, tt.expectedType, w.Header().Get("Content-Type"))
			}
		})
	}
}

func TestHandlers_ConcurrentRequests(t *testing.T) {
	cfg := &config.Config{}
	registry := prometheus.NewRegistry()
	handlers := NewHandlers(cfg, registry)

	concurrency := 100
	done := make(chan bool, concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			w := httptest.NewRecorder()
			handlers.HealthHandler(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			done <- true
		}()
	}

	for i := 0; i < concurrency; i++ {
		<-done
	}
}

func BenchmarkHandlers_MetricsHandler(b *testing.B) {
	cfg := &config.Config{}
	registry := prometheus.NewRegistry()
	handlers := NewHandlers(cfg, registry)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handlers.MetricsHandler(w, req)
	}
}

func BenchmarkHandlers_HealthHandler(b *testing.B) {
	cfg := &config.Config{}
	registry := prometheus.NewRegistry()
	handlers := NewHandlers(cfg, registry)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handlers.HealthHandler(w, req)
	}
}

func BenchmarkHandlers_IndexHandler(b *testing.B) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers: []string{"pool.ntp.org"},
			Timeout: 5 * time.Second,
			Version: 4,
		},
	}
	registry := prometheus.NewRegistry()
	handlers := NewHandlers(cfg, registry)

	req := httptest.NewRequest(http.MethodGet, "/", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handlers.IndexHandler(w, req)
	}
}

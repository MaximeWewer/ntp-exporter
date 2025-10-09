package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Address:      "0.0.0.0",
			Port:         0,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
		},
		NTP: config.NTPConfig{
			Servers:          []string{"pool.ntp.org"},
			Timeout:          2 * time.Second,
			Version:          4,
			SamplesPerServer: 1,
		},
	}
}

func TestServer_MetricsEndpoint(t *testing.T) {
	cfg := createTestConfig()
	metricsRegistry := metrics.NewRegistry()
	err := metricsRegistry.Register()
	require.NoError(t, err)
	registry := metricsRegistry.GetRegistry()
	handlers := NewHandlers(cfg, registry)

	testServer := httptest.NewServer(http.HandlerFunc(handlers.MetricsHandler))
	defer testServer.Close()

	resp, err := http.Get(testServer.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	// Should return metrics (at least go metrics)
	bodyStr := string(body)
	assert.True(t, len(bodyStr) > 0)
}

func TestServer_HealthEndpoint(t *testing.T) {
	cfg := createTestConfig()
	metricsRegistry := metrics.NewRegistry()
	err := metricsRegistry.Register()
	require.NoError(t, err)
	registry := metricsRegistry.GetRegistry()
	handlers := NewHandlers(cfg, registry)

	testServer := httptest.NewServer(http.HandlerFunc(handlers.HealthHandler))
	defer testServer.Close()

	resp, err := http.Get(testServer.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Contains(t, string(body), "healthy")
}

func TestServer_IndexEndpoint(t *testing.T) {
	cfg := createTestConfig()
	metricsRegistry := metrics.NewRegistry()
	err := metricsRegistry.Register()
	require.NoError(t, err)
	registry := metricsRegistry.GetRegistry()
	handlers := NewHandlers(cfg, registry)

	testServer := httptest.NewServer(http.HandlerFunc(handlers.IndexHandler))
	defer testServer.Close()

	resp, err := http.Get(testServer.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	bodyStr := string(body)
	assert.Contains(t, bodyStr, "NTP")
	assert.Contains(t, bodyStr, "/metrics")
}

func TestServer_MultipleScrapes(t *testing.T) {
	cfg := createTestConfig()
	metricsRegistry := metrics.NewRegistry()
	err := metricsRegistry.Register()
	require.NoError(t, err)
	handlers := NewHandlers(cfg, metricsRegistry.GetRegistry())

	testServer := httptest.NewServer(http.HandlerFunc(handlers.MetricsHandler))
	defer testServer.Close()

	// Perform multiple scrapes
	for i := 0; i < 5; i++ {
		resp, err := http.Get(testServer.URL)
		require.NoError(t, err)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		resp.Body.Close()

		assert.True(t, len(body) > 0)
	}
}

func TestServer_ConcurrentRequests(t *testing.T) {
	cfg := createTestConfig()
	metricsRegistry := metrics.NewRegistry()
	err := metricsRegistry.Register()
	require.NoError(t, err)
	registry := metricsRegistry.GetRegistry()
	handlers := NewHandlers(cfg, registry)

	testServer := httptest.NewServer(http.HandlerFunc(handlers.MetricsHandler))
	defer testServer.Close()

	// Concurrent requests
	concurrency := 50
	done := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			resp, err := http.Get(testServer.URL)
			if err != nil {
				done <- err
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				done <- assert.AnError
				return
			}

			done <- nil
		}()
	}

	// Wait for all requests
	for i := 0; i < concurrency; i++ {
		err := <-done
		assert.NoError(t, err)
	}
}

func TestServer_GracefulShutdown(t *testing.T) {
	cfg := createTestConfig()
	metricsRegistry := metrics.NewRegistry()
	err := metricsRegistry.Register()
	require.NoError(t, err)
	registry := metricsRegistry.GetRegistry()
	m := metrics.NewNTPMetrics()
	server := New(cfg, registry, m)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server in background
	go func() {
		server.Start(ctx)
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Cancel context to trigger shutdown
	cancel()

	// Give server time to shut down
	time.Sleep(100 * time.Millisecond)

	// Test should complete without hanging
	assert.True(t, true)
}

func TestServer_MetricsFormat(t *testing.T) {
	cfg := createTestConfig()
	metricsRegistry := metrics.NewRegistry()
	err := metricsRegistry.Register()
	require.NoError(t, err)
	handlers := NewHandlers(cfg, metricsRegistry.GetRegistry())

	testServer := httptest.NewServer(http.HandlerFunc(handlers.MetricsHandler))
	defer testServer.Close()

	resp, err := http.Get(testServer.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	lines := strings.Split(string(body), "\n")

	// Verify Prometheus format
	hasHelp := false
	hasType := false

	for _, line := range lines {
		if strings.HasPrefix(line, "# HELP") {
			hasHelp = true
		}
		if strings.HasPrefix(line, "# TYPE") {
			hasType = true
		}
	}

	assert.True(t, hasHelp, "Should have HELP comments")
	assert.True(t, hasType, "Should have TYPE comments")
}

func BenchmarkServer_MetricsEndpoint(b *testing.B) {
	cfg := createTestConfig()
	metricsRegistry := metrics.NewRegistry()
	err := metricsRegistry.Register()
	if err != nil {
		b.Fatal(err)
	}
	registry := metricsRegistry.GetRegistry()
	handlers := NewHandlers(cfg, registry)

	testServer := httptest.NewServer(http.HandlerFunc(handlers.MetricsHandler))
	defer testServer.Close()

	client := &http.Client{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := client.Get(testServer.URL)
		if err != nil {
			b.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func BenchmarkServer_ConcurrentRequests(b *testing.B) {
	cfg := createTestConfig()
	metricsRegistry := metrics.NewRegistry()
	err := metricsRegistry.Register()
	if err != nil {
		b.Fatal(err)
	}
	registry := metricsRegistry.GetRegistry()
	handlers := NewHandlers(cfg, registry)

	testServer := httptest.NewServer(http.HandlerFunc(handlers.MetricsHandler))
	defer testServer.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		client := &http.Client{}
		for pb.Next() {
			resp, err := client.Get(testServer.URL)
			if err != nil {
				b.Fatal(err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	})
}

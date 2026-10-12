package server

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestServer(t *testing.T, readiness ReadinessFunc) *httptest.Server {
	t.Helper()
	reg := metrics.NewRegistryWithConfig("ntp", "")
	require.NoError(t, reg.Register())
	reg.GetMetrics().ExporterScrapesTotal.WithLabelValues("success").Inc()

	ts := httptest.NewServer(New(config.DefaultConfig(), reg.GetRegistry(), readiness).Handler())
	t.Cleanup(ts.Close)
	return ts
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body)
}

func TestServer_Metrics(t *testing.T) {
	ts := newTestServer(t, nil)

	code, body := get(t, ts.URL+"/metrics")
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, "ntp_exporter_scrapes_total")
	assert.Contains(t, body, "go_goroutines")
	assert.NotContains(t, body, "ntp_exporter_memory_usage_bytes", "runtime metrics come from the Go collector")
	assert.NotContains(t, body, "ntp_exporter_gc_duration_seconds")
}

func TestServer_Liveness(t *testing.T) {
	ts := newTestServer(t, func() error { return errors.New("not ready") })

	for _, path := range []string{"/healthz", "/health"} {
		code, body := get(t, ts.URL+path)
		assert.Equal(t, http.StatusOK, code, path)
		assert.Contains(t, body, "alive")
	}
}

func TestServer_Readiness(t *testing.T) {
	ready := errors.New("no collection cycle completed yet")
	ts := newTestServer(t, func() error { return ready })

	code, body := get(t, ts.URL+"/readyz")
	assert.Equal(t, http.StatusServiceUnavailable, code)
	assert.Contains(t, body, "no collection cycle completed yet")

	ready = nil
	code, body = get(t, ts.URL+"/readyz")
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, `"ready"`)
}

func TestServer_IndexAndNotFound(t *testing.T) {
	ts := newTestServer(t, nil)

	code, body := get(t, ts.URL+"/")
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, "/readyz")

	code, _ = get(t, ts.URL+"/missing")
	assert.Equal(t, http.StatusNotFound, code)
}

func TestServer_NoCORSHeaders(t *testing.T) {
	ts := newTestServer(t, nil)
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/metrics", nil)
	req.Header.Set("Origin", "https://example.org")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"))
}

func TestRecoveryMiddleware(t *testing.T) {
	h := withMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestQuote(t *testing.T) {
	assert.Equal(t, `"a \"b\" \\ c\n"`, quote("a \"b\" \\ c\n"))
}

func TestServer_StartAndShutdown(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	cfg := config.DefaultConfig()
	cfg.Server.Address = "127.0.0.1"
	cfg.Server.Port = port
	reg := metrics.NewRegistryWithConfig("ntp", "")
	require.NoError(t, reg.Register())
	srv := New(cfg, reg.GetRegistry(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx) }()

	require.Eventually(t, func() bool {
		resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/healthz")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 2*time.Second, 20*time.Millisecond)

	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop")
	}
}

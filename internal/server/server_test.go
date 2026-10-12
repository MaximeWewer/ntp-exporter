package server

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
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

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestServer_WebConfigBasicAuth(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	require.NoError(t, err)
	webConfig := filepath.Join(t.TempDir(), "web.yml")
	require.NoError(t, os.WriteFile(webConfig, []byte("basic_auth_users:\n  prometheus: "+string(hash)+"\n"), 0o600))

	cfg := config.DefaultConfig()
	cfg.Server.Address = "127.0.0.1"
	cfg.Server.Port = freePort(t)
	cfg.Server.WebConfigFile = webConfig
	reg := metrics.NewRegistryWithConfig("ntp", "")
	require.NoError(t, reg.Register())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = New(cfg, reg.GetRegistry(), nil).Start(ctx) }()

	url := "http://127.0.0.1:" + strconv.Itoa(cfg.Server.Port) + "/metrics"
	require.Eventually(t, func() bool {
		resp, err := http.Get(url)
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusUnauthorized
	}, 2*time.Second, 20*time.Millisecond, "metrics require authentication")

	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.SetBasicAuth("prometheus", "secret")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestServer_ProbeRoute(t *testing.T) {
	reg := metrics.NewRegistryWithConfig("ntp", "")
	cfg := config.DefaultConfig()

	without := httptest.NewServer(New(cfg, reg.GetRegistry(), nil).Handler())
	t.Cleanup(without.Close)
	code, _ := get(t, without.URL+"/probe?target=time.example.org")
	assert.Equal(t, http.StatusNotFound, code, "no route without probe handler")

	srv := New(cfg, reg.GetRegistry(), nil)
	srv.SetProbeHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("probed " + r.URL.Query().Get("target")))
	}))
	with := httptest.NewServer(srv.Handler())
	t.Cleanup(with.Close)

	code, body := get(t, with.URL+"/probe?target=time.example.org")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "probed time.example.org", body)

	_, index := get(t, with.URL+"/")
	assert.Contains(t, index, "/probe?target=")

	disabled := false
	cfg.ProbeEndpoint.Enabled = &disabled
	_, index = get(t, without.URL+"/")
	assert.NotContains(t, index, "/probe?target=")
}

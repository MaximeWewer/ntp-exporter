package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/collector"
	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/ntp/ntptest"
	"github.com/maximewewer/ntp-exporter/internal/probe"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_FromFile(t *testing.T) {
	configFile := t.TempDir() + "/test-config.yaml"
	configContent := `
server:
  port: 9559
ntp:
  servers:
    - pool.ntp.org
logging:
  level: info
`
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0o644))

	cfg, err := loadConfig(configFile)
	require.NoError(t, err)
	assert.Equal(t, 9559, cfg.Server.Port)
}

func TestLoadConfig_FromEnv(t *testing.T) {
	cfg, err := loadConfig("")
	require.NoError(t, err)
	assert.NotNil(t, cfg)
}

func newMockedLoop(t *testing.T) (*config.Config, *probe.Prober, *ntptest.MockNTPClient) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.NTP.Servers = []string{"time.example.org"}
	cfg.NTP.SamplesPerServer = 1
	mock := ntptest.NewMockNTPClient()
	mock.SetupSuccessfulServer("time.example.org", time.Millisecond, 2)
	prober := probe.New(cfg, mock)
	prober.SetResolver(identityResolver{})
	return cfg, prober, mock
}

func TestRunCollectionLoop_ContextCancellation(t *testing.T) {
	cfg, prober, _ := newMockedLoop(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := runCollectionLoop(ctx, cfg, prober, collector.NewExporter(cfg, metrics.NewNTPMetricsWithConfig("ntp", "")))
	assert.NoError(t, err, "Collection loop should stop gracefully on context cancellation")
}

func TestRunCollectionLoop_WithTimeout(t *testing.T) {
	cfg, prober, mock := newMockedLoop(t)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err := runCollectionLoop(ctx, cfg, prober, collector.NewExporter(cfg, metrics.NewNTPMetricsWithConfig("ntp", "")))
	assert.NoError(t, err)
	assert.Equal(t, 1, mock.GetCallCount("time.example.org"), "initial cycle queries the server once")
}

// identityResolver resolves every host to itself, so that the mock keyed by
// host name receives the queries
type identityResolver struct{}

func (identityResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	return []string{host}, nil
}

func TestRunHealthcheck(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	host, port, err := net.SplitHostPort(strings.TrimPrefix(ts.URL, "http://"))
	require.NoError(t, err)
	cfg := config.DefaultConfig()
	cfg.Server.Address = host
	cfg.Server.Port, _ = strconv.Atoi(port)

	assert.Equal(t, 0, runHealthcheck(cfg, nil))
	assert.Equal(t, 1, runHealthcheck(cfg, errors.New("bad config")))

	ts.Close()
	assert.Equal(t, 1, runHealthcheck(cfg, nil), "server down")
}

func TestApplyWebFlags(t *testing.T) {
	cfg := config.DefaultConfig()
	require.NoError(t, applyWebFlags(cfg, "", ":19100"))
	assert.Equal(t, "0.0.0.0", cfg.Server.Address)
	assert.Equal(t, 19100, cfg.Server.Port)

	require.NoError(t, applyWebFlags(cfg, "", "127.0.0.1:9200"))
	assert.Equal(t, "127.0.0.1", cfg.Server.Address)

	assert.Error(t, applyWebFlags(cfg, "", "no-port"))
	assert.Error(t, applyWebFlags(cfg, "", ":99999"))

	missing := filepath.Join(t.TempDir(), "missing.yml")
	assert.ErrorContains(t, applyWebFlags(config.DefaultConfig(), missing, ""), "web configuration")

	valid := filepath.Join(t.TempDir(), "web.yml")
	require.NoError(t, os.WriteFile(valid, []byte("http_server_config:\n  http2: true\n"), 0o600))
	c := config.DefaultConfig()
	require.NoError(t, applyWebFlags(c, valid, ""))
	assert.Equal(t, valid, c.Server.WebConfigFile)
}

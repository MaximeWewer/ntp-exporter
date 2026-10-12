package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/collector"
	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/ntp"
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

func newMockedLoop(t *testing.T) (*config.Config, *probe.Prober, *ntp.MockNTPClient) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.NTP.Servers = []string{"time.example.org"}
	cfg.NTP.SamplesPerServer = 1
	mock := ntp.NewMockNTPClient()
	mock.SetupSuccessfulServer("time.example.org", time.Millisecond, 2)
	return cfg, probe.New(cfg, mock), mock
}

func TestRunCollectionLoop_ContextCancellation(t *testing.T) {
	cfg, prober, _ := newMockedLoop(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := runCollectionLoop(ctx, cfg, prober, collector.NewExporter(cfg, metrics.NewNTPMetrics()))
	assert.NoError(t, err, "Collection loop should stop gracefully on context cancellation")
}

func TestRunCollectionLoop_WithTimeout(t *testing.T) {
	cfg, prober, mock := newMockedLoop(t)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err := runCollectionLoop(ctx, cfg, prober, collector.NewExporter(cfg, metrics.NewNTPMetrics()))
	assert.NoError(t, err)
	assert.Equal(t, 1, mock.GetCallCount("time.example.org"), "initial cycle queries the server once")
}

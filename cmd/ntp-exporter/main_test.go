package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/collector"
	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
	"github.com/stretchr/testify/assert"
)

func TestLoadConfig_FromFile(t *testing.T) {
	// Create temp config file
	tmpDir := t.TempDir()
	configFile := tmpDir + "/test-config.yaml"

	configContent := `
server:
  port: 9559
ntp:
  servers:
    - pool.ntp.org
logging:
  level: info
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	assert.NoError(t, err)

	cfg, err := loadConfig(configFile)
	assert.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, 9559, cfg.Server.Port)
}

func TestLoadConfig_FromEnv(t *testing.T) {
	// Test with empty file (loads from env)
	cfg, err := loadConfig("")
	assert.NoError(t, err)
	assert.NotNil(t, cfg)
}

func TestCollectMetrics(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping network test in short mode")
	}

	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers:          []string{"pool.ntp.org"},
			Timeout:          5 * time.Second,
			Version:          4,
			SamplesPerServer: 1,
		},
	}

	m := metrics.NewNTPMetrics()
	collectorRegistry := collector.NewRegistry()
	collectorRegistry.Register(collector.NewBaseCollector(cfg, m))
	collectorRegistry.Register(collector.NewQualityCollector(cfg, m))
	collectorRegistry.Register(collector.NewSecurityCollector(cfg, m))

	ctx := context.Background()

	// Should not panic
	err := collectorRegistry.CollectAll(ctx)

	// May fail due to network, but shouldn't panic
	if err != nil {
		t.Logf("Collect failed (expected in some environments): %v", err)
	}
}

func TestRunCollectionLoop_ContextCancellation(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers:          []string{"pool.ntp.org"},
			Timeout:          2 * time.Second,
			Version:          4,
			SamplesPerServer: 1,
		},
	}

	m := metrics.NewNTPMetrics()
	collectorRegistry := collector.NewRegistry()
	collectorRegistry.Register(collector.NewBaseCollector(cfg, m))
	collectorRegistry.Register(collector.NewQualityCollector(cfg, m))
	collectorRegistry.Register(collector.NewSecurityCollector(cfg, m))

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	err := runCollectionLoop(ctx, cfg, collectorRegistry)

	assert.NoError(t, err, "Collection loop should stop gracefully on context cancellation")
}

func TestCollectMetrics_EmptyServers(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers:          []string{},
			Timeout:          2 * time.Second,
			Version:          4,
			SamplesPerServer: 1,
		},
	}

	m := metrics.NewNTPMetrics()
	collectorRegistry := collector.NewRegistry()
	collectorRegistry.Register(collector.NewBaseCollector(cfg, m))
	collectorRegistry.Register(collector.NewQualityCollector(cfg, m))
	collectorRegistry.Register(collector.NewSecurityCollector(cfg, m))

	ctx := context.Background()

	// Should handle empty servers gracefully
	err := collectorRegistry.CollectAll(ctx)

	// Base collector might error with no servers, which is expected
	_ = err
}

func TestRunCollectionLoop_WithTimeout(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers:          []string{"pool.ntp.org"},
			Timeout:          2 * time.Second,
			Version:          4,
			SamplesPerServer: 1,
		},
	}

	m := metrics.NewNTPMetrics()
	collectorRegistry := collector.NewRegistry()
	collectorRegistry.Register(collector.NewBaseCollector(cfg, m))
	collectorRegistry.Register(collector.NewQualityCollector(cfg, m))
	collectorRegistry.Register(collector.NewSecurityCollector(cfg, m))

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err := runCollectionLoop(ctx, cfg, collectorRegistry)

	// Should stop cleanly on context timeout
	assert.NoError(t, err)
}

func TestCollectMetrics_NetworkFailure(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers:          []string{"192.0.2.1"}, // TEST-NET-1, should fail
			Timeout:          1 * time.Second,
			Version:          4,
			SamplesPerServer: 1,
		},
	}

	m := metrics.NewNTPMetrics()
	collectorRegistry := collector.NewRegistry()
	collectorRegistry.Register(collector.NewBaseCollector(cfg, m))
	collectorRegistry.Register(collector.NewQualityCollector(cfg, m))
	collectorRegistry.Register(collector.NewSecurityCollector(cfg, m))

	ctx := context.Background()

	// Should handle network failure gracefully
	_ = collectorRegistry.CollectAll(ctx)
}

func BenchmarkCollectMetrics(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping network benchmark in short mode")
	}

	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers:          []string{"pool.ntp.org"},
			Timeout:          5 * time.Second,
			Version:          4,
			SamplesPerServer: 1,
		},
	}

	m := metrics.NewNTPMetrics()
	collectorRegistry := collector.NewRegistry()
	collectorRegistry.Register(collector.NewBaseCollector(cfg, m))
	collectorRegistry.Register(collector.NewQualityCollector(cfg, m))
	collectorRegistry.Register(collector.NewSecurityCollector(cfg, m))

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collectorRegistry.CollectAll(ctx)
	}
}

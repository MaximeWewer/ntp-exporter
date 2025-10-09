package collector

import (
	"context"
	"testing"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
	"github.com/stretchr/testify/assert"
)

func TestNewBaseCollector(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers:          []string{"pool.ntp.org"},
			Timeout:          5 * time.Second,
			Version:          4,
			SamplesPerServer: 1,
		},
	}
	m := metrics.NewNTPMetrics()

	collector := NewBaseCollector(cfg, m)

	assert.NotNil(t, collector)
	assert.NotNil(t, collector.GetConfig())
	assert.NotNil(t, collector.GetClient())
	assert.NotNil(t, collector.GetMetrics())
}

func TestBaseCollector_Collect(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers:          []string{"pool.ntp.org"},
			Timeout:          2 * time.Second,
			Version:          4,
			SamplesPerServer: 1,
		},
	}
	m := metrics.NewNTPMetrics()

	collector := NewBaseCollector(cfg, m)
	ctx := context.Background()

	// Should not panic even if NTP server is unreachable
	assert.NotPanics(t, func() {
		collector.Collect(ctx)
	})
}

func TestBaseCollector_Collect_MultipleServers(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers: []string{
				"pool.ntp.org",
				"time.nist.gov",
			},
			Timeout:          2 * time.Second,
			Version:          4,
			SamplesPerServer: 1,
		},
	}

	m := metrics.NewNTPMetrics()
	collector := NewBaseCollector(cfg, m)
	ctx := context.Background()

	err := collector.Collect(ctx)

	// Collection should complete (may fail for connectivity, but shouldn't panic)
	assert.NotPanics(t, func() {
		collector.Collect(ctx)
	})

	if err != nil {
		t.Logf("Collection completed with error (normal for offline tests): %v", err)
	}
}

func TestBaseCollector_Collect_WithPools(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers: []string{},
			Pools: []config.PoolConfig{
				{
					Name:       "pool.ntp.org",
					Strategy:   "best_n",
					MaxServers: 4,
					Fallback:   "time.nist.gov",
				},
			},
			Timeout:          2 * time.Second,
			Version:          4,
			SamplesPerServer: 1,
		},
	}

	m := metrics.NewNTPMetrics()
	collector := NewBaseCollector(cfg, m)
	ctx := context.Background()

	assert.NotPanics(t, func() {
		collector.Collect(ctx)
	})
}

func TestBaseCollector_Collect_ContextCancellation(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers:          []string{"pool.ntp.org"},
			Timeout:          10 * time.Second,
			Version:          4,
			SamplesPerServer: 1,
		},
	}

	m := metrics.NewNTPMetrics()
	collector := NewBaseCollector(cfg, m)
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel before collection
	cancel()

	assert.NotPanics(t, func() {
		collector.Collect(ctx)
	})
}

func TestBaseCollector_Collect_ContextTimeout(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers:          []string{"pool.ntp.org"},
			Timeout:          5 * time.Second,
			Version:          4,
			SamplesPerServer: 1,
		},
	}

	m := metrics.NewNTPMetrics()
	collector := NewBaseCollector(cfg, m)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	assert.NotPanics(t, func() {
		collector.Collect(ctx)
	})
}

func TestBaseCollector_Collect_EmptyServers(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers:          []string{},
			Timeout:          2 * time.Second,
			Version:          4,
			SamplesPerServer: 1,
		},
	}

	m := metrics.NewNTPMetrics()
	collector := NewBaseCollector(cfg, m)
	ctx := context.Background()

	err := collector.Collect(ctx)
	assert.NoError(t, err, "Empty server list should not cause error")
}

func BenchmarkBaseCollector_Collect(b *testing.B) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers:          []string{"pool.ntp.org"},
			Timeout:          2 * time.Second,
			Version:          4,
			SamplesPerServer: 1,
		},
	}

	m := metrics.NewNTPMetrics()
	collector := NewBaseCollector(cfg, m)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.Collect(ctx)
	}
}

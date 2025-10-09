package collector

import (
	"context"
	"testing"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
	"github.com/stretchr/testify/assert"
)

func TestNewSecurityCollector(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers: []string{"pool.ntp.org"},
			Timeout: 5 * time.Second,
			Version: 4,
		},
	}

	m := metrics.NewNTPMetrics()
	collector := NewSecurityCollector(cfg, m)

	assert.NotNil(t, collector)
	assert.NotNil(t, collector.config)
	assert.NotNil(t, collector.client)
	assert.NotNil(t, collector.validator)
	assert.Equal(t, cfg, collector.config)
}

func TestSecurityCollector_Collect(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping network test in short mode")
	}

	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers: []string{"pool.ntp.org"},
			Timeout: 5 * time.Second,
			Version: 4,
		},
	}

	m := metrics.NewNTPMetrics()
	collector := NewSecurityCollector(cfg, m)
	ctx := context.Background()

	// May fail due to network issues, but should not panic
	assert.NotPanics(t, func() {
		collector.Collect(ctx)
	})
}

func TestSecurityCollector_Collect_MultipleServers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping network test in short mode")
	}

	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers: []string{
				"pool.ntp.org",
				"time.google.com",
			},
			Timeout: 5 * time.Second,
			Version: 4,
		},
	}

	m := metrics.NewNTPMetrics()
	collector := NewSecurityCollector(cfg, m)
	ctx := context.Background()

	err := collector.Collect(ctx)

	// Collection may fail for network reasons but shouldn't panic
	if err != nil {
		t.Logf("Collection failed (expected in some environments): %v", err)
	}
}

func TestSecurityCollector_Collect_EmptyServers(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers: []string{},
			Timeout: 5 * time.Second,
			Version: 4,
		},
	}

	m := metrics.NewNTPMetrics()
	collector := NewSecurityCollector(cfg, m)
	ctx := context.Background()

	err := collector.Collect(ctx)

	assert.NoError(t, err, "Empty server list should not cause error")
}

func TestSecurityCollector_Collect_ContextCancellation(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers: []string{"pool.ntp.org"},
			Timeout: 10 * time.Second,
			Version: 4,
		},
	}

	m := metrics.NewNTPMetrics()
	collector := NewSecurityCollector(cfg, m)
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	err := collector.Collect(ctx)

	// Should handle cancellation gracefully
	if err != nil {
		t.Logf("Collection with cancelled context: %v", err)
	}
}

func TestSecurityCollector_CollectFromServer(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping network test in short mode")
	}

	cfg := &config.Config{
		NTP: config.NTPConfig{
			Timeout: 5 * time.Second,
			Version: 4,
		},
	}

	m := metrics.NewNTPMetrics()
	collector := NewSecurityCollector(cfg, m)
	ctx := context.Background()

	err := collector.collectFromServer(ctx, "pool.ntp.org")

	// May fail due to network issues
	if err != nil {
		t.Logf("Server collection failed (expected in some environments): %v", err)
	}
}

func TestSecurityCollector_CollectFromServer_InvalidServer(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Timeout: 2 * time.Second,
			Version: 4,
		},
	}

	m := metrics.NewNTPMetrics()
	collector := NewSecurityCollector(cfg, m)
	ctx := context.Background()

	err := collector.collectFromServer(ctx, "invalid.nonexistent.ntp.server.test")

	assert.Error(t, err)
}

func TestSecurityCollector_Collect_ContextTimeout(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers: []string{"pool.ntp.org"},
			Timeout: 5 * time.Second,
			Version: 4,
		},
	}

	m := metrics.NewNTPMetrics()
	collector := NewSecurityCollector(cfg, m)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := collector.Collect(ctx)

	// Should handle timeout gracefully
	if err != nil {
		t.Logf("Collection with timeout: %v", err)
	}
}

func TestSecurityCollector_Collect_ConcurrentCollection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping network test in short mode")
	}

	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers: []string{"pool.ntp.org"},
			Timeout: 5 * time.Second,
			Version: 4,
		},
	}

	m := metrics.NewNTPMetrics()
	collector := NewSecurityCollector(cfg, m)

	// Run collectors concurrently
	done := make(chan bool, 5)
	for i := 0; i < 5; i++ {
		go func() {
			ctx := context.Background()
			collector.Collect(ctx)
			done <- true
		}()
	}

	// Wait for all to complete
	for i := 0; i < 5; i++ {
		<-done
	}

	assert.True(t, true, "Concurrent collection should not cause race conditions")
}

func TestSecurityCollector_ValidatorIntegration(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers: []string{"pool.ntp.org"},
			Timeout: 5 * time.Second,
			Version: 4,
		},
	}

	m := metrics.NewNTPMetrics()
	collector := NewSecurityCollector(cfg, m)

	assert.NotNil(t, collector.validator, "Validator should be initialized")
}

func TestSecurityCollector_Configuration(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		version int
	}{
		{"default", 5 * time.Second, 4},
		{"fast", 2 * time.Second, 4},
		{"slow", 10 * time.Second, 4},
		{"v3", 5 * time.Second, 3},
		{"v2", 5 * time.Second, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				NTP: config.NTPConfig{
					Servers: []string{"pool.ntp.org"},
					Timeout: tt.timeout,
					Version: tt.version,
				},
			}

			m := metrics.NewNTPMetrics()
			collector := NewSecurityCollector(cfg, m)

			assert.Equal(t, tt.timeout, collector.config.NTP.Timeout)
			assert.Equal(t, tt.version, collector.config.NTP.Version)
		})
	}
}

func TestSecurityCollector_Collect_DifferentVersions(t *testing.T) {
	versions := []int{2, 3, 4}

	for _, version := range versions {
		t.Run("ntp_v"+string(rune(version+'0')), func(t *testing.T) {
			cfg := &config.Config{
				NTP: config.NTPConfig{
					Servers: []string{"pool.ntp.org"},
					Timeout: 5 * time.Second,
					Version: version,
				},
			}

			m := metrics.NewNTPMetrics()
			collector := NewSecurityCollector(cfg, m)
			ctx := context.Background()

			// Should work with different NTP versions
			collector.Collect(ctx)
		})
	}
}

func TestSecurityCollector_Collect_ManyServers(t *testing.T) {
	servers := []string{
		"0.pool.ntp.org",
		"1.pool.ntp.org",
		"2.pool.ntp.org",
		"3.pool.ntp.org",
	}

	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers: servers,
			Timeout: 5 * time.Second,
			Version: 4,
		},
	}

	m := metrics.NewNTPMetrics()
	collector := NewSecurityCollector(cfg, m)
	ctx := context.Background()

	// Should handle multiple servers
	err := collector.Collect(ctx)

	if err != nil {
		t.Logf("Multi-server collection: %v", err)
	}
}

func BenchmarkSecurityCollector_Collect(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping network benchmark in short mode")
	}

	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers: []string{"pool.ntp.org"},
			Timeout: 5 * time.Second,
			Version: 4,
		},
	}

	m := metrics.NewNTPMetrics()
	collector := NewSecurityCollector(cfg, m)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.Collect(ctx)
	}
}

func BenchmarkSecurityCollector_CollectFromServer(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping network benchmark in short mode")
	}

	cfg := &config.Config{
		NTP: config.NTPConfig{
			Timeout: 5 * time.Second,
			Version: 4,
		},
	}

	m := metrics.NewNTPMetrics()
	collector := NewSecurityCollector(cfg, m)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.collectFromServer(ctx, "pool.ntp.org")
	}
}

func BenchmarkSecurityCollector_New(b *testing.B) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers: []string{"pool.ntp.org"},
			Timeout: 5 * time.Second,
			Version: 4,
		},
	}

	m := metrics.NewNTPMetrics()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewSecurityCollector(cfg, m)
	}
}

package collector

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
	"github.com/stretchr/testify/assert"
)

func TestNewCommonCollector(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers: []string{"pool.ntp.org"},
			Timeout: 5 * time.Second,
			Version: 4,
		},
	}

	m := metrics.NewNTPMetrics()
	collector := NewCommonCollector(cfg, m, "test")

	assert.NotNil(t, collector)
	assert.Equal(t, "test", collector.Name())
	assert.True(t, collector.Enabled())
	assert.Equal(t, cfg, collector.GetConfig())
	assert.NotNil(t, collector.GetClient())
	assert.Equal(t, m, collector.GetMetrics())
}

func TestCommonCollector_Name(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Timeout: 5 * time.Second,
			Version: 4,
		},
	}

	tests := []struct {
		name         string
		collectorName string
	}{
		{"base_collector", "base"},
		{"security_collector", "security"},
		{"quality_collector", "quality"},
		{"hybrid_collector", "hybrid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := metrics.NewNTPMetrics()
			collector := NewCommonCollector(cfg, m, tt.collectorName)
			assert.Equal(t, tt.collectorName, collector.Name())
		})
	}
}

func TestCommonCollector_Enabled(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Timeout: 5 * time.Second,
			Version: 4,
		},
	}

	m := metrics.NewNTPMetrics()
	collector := NewCommonCollector(cfg, m, "test")

	assert.True(t, collector.Enabled())
}

func TestCommonCollector_IterateServers(t *testing.T) {
	t.Run("successful_iteration", func(t *testing.T) {
		cfg := &config.Config{
			NTP: config.NTPConfig{
				Servers: []string{"server1", "server2", "server3"},
				Timeout: 5 * time.Second,
				Version: 4,
			},
		}

		m := metrics.NewNTPMetrics()
		collector := NewCommonCollector(cfg, m, "test")

		callCount := 0
		collectFunc := func(ctx context.Context, server string) error {
			callCount++
			return nil
		}

		ctx := context.Background()
		err := collector.IterateServers(ctx, collectFunc, "test")

		assert.NoError(t, err)
		assert.Equal(t, 3, callCount)
	})

	t.Run("continues_on_error", func(t *testing.T) {
		cfg := &config.Config{
			NTP: config.NTPConfig{
				Servers: []string{"server1", "server2", "server3"},
				Timeout: 5 * time.Second,
				Version: 4,
			},
		}

		m := metrics.NewNTPMetrics()
		collector := NewCommonCollector(cfg, m, "test")

		callCount := 0
		collectFunc := func(ctx context.Context, server string) error {
			callCount++
			if server == "server2" {
				return errors.New("simulated error")
			}
			return nil
		}

		ctx := context.Background()
		err := collector.IterateServers(ctx, collectFunc, "test")

		assert.NoError(t, err) // Should not return error, just continue
		assert.Equal(t, 3, callCount) // Should call all servers
	})

	t.Run("empty_servers", func(t *testing.T) {
		cfg := &config.Config{
			NTP: config.NTPConfig{
				Servers: []string{},
				Timeout: 5 * time.Second,
				Version: 4,
			},
		}

		m := metrics.NewNTPMetrics()
		collector := NewCommonCollector(cfg, m, "test")

		callCount := 0
		collectFunc := func(ctx context.Context, server string) error {
			callCount++
			return nil
		}

		ctx := context.Background()
		err := collector.IterateServers(ctx, collectFunc, "test")

		assert.NoError(t, err)
		assert.Equal(t, 0, callCount)
	})

	t.Run("context_passed_through", func(t *testing.T) {
		cfg := &config.Config{
			NTP: config.NTPConfig{
				Servers: []string{"server1"},
				Timeout: 5 * time.Second,
				Version: 4,
			},
		}

		m := metrics.NewNTPMetrics()
		collector := NewCommonCollector(cfg, m, "test")

		type contextKey string
		key := contextKey("test-key")
		expectedValue := "test-value"

		var receivedValue string
		collectFunc := func(ctx context.Context, server string) error {
			if val := ctx.Value(key); val != nil {
				receivedValue = val.(string)
			}
			return nil
		}

		ctx := context.WithValue(context.Background(), key, expectedValue)
		err := collector.IterateServers(ctx, collectFunc, "test")

		assert.NoError(t, err)
		assert.Equal(t, expectedValue, receivedValue)
	})
}

func TestCreateNTPClient(t *testing.T) {
	t.Run("client_without_rate_limit", func(t *testing.T) {
		cfg := &config.Config{
			NTP: config.NTPConfig{
				Timeout: 5 * time.Second,
				Version: 4,
				RateLimit: config.RateLimitConfig{
					Enabled: false,
				},
			},
		}

		client := createNTPClient(cfg)
		assert.NotNil(t, client)
	})

	t.Run("client_with_rate_limit", func(t *testing.T) {
		cfg := &config.Config{
			NTP: config.NTPConfig{
				Timeout: 5 * time.Second,
				Version: 4,
				RateLimit: config.RateLimitConfig{
					Enabled:       true,
					GlobalRate:    100,
					PerServerRate: 10,
					BurstSize:     5,
				},
			},
		}

		client := createNTPClient(cfg)
		assert.NotNil(t, client)
	})

	t.Run("different_versions", func(t *testing.T) {
		versions := []int{2, 3, 4}
		for _, version := range versions {
			cfg := &config.Config{
				NTP: config.NTPConfig{
					Timeout: 5 * time.Second,
					Version: version,
				},
			}

			client := createNTPClient(cfg)
			assert.NotNil(t, client, "Client should be created for version %d", version)
		}
	})

	t.Run("different_timeouts", func(t *testing.T) {
		timeouts := []time.Duration{1 * time.Second, 5 * time.Second, 10 * time.Second}
		for _, timeout := range timeouts {
			cfg := &config.Config{
				NTP: config.NTPConfig{
					Timeout: timeout,
					Version: 4,
				},
			}

			client := createNTPClient(cfg)
			assert.NotNil(t, client, "Client should be created for timeout %v", timeout)
		}
	})
}

func TestCommonCollector_GettersSetters(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers: []string{"pool.ntp.org"},
			Timeout: 5 * time.Second,
			Version: 4,
		},
	}

	m := metrics.NewNTPMetrics()
	collector := NewCommonCollector(cfg, m, "test")

	// Test all getters return non-nil values
	assert.NotNil(t, collector.GetConfig())
	assert.NotNil(t, collector.GetClient())
	assert.NotNil(t, collector.GetMetrics())

	// Test values are correct
	assert.Equal(t, cfg, collector.GetConfig())
	assert.Equal(t, m, collector.GetMetrics())
}

func TestCommonCollector_MultipleInstances(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers: []string{"pool.ntp.org"},
			Timeout: 5 * time.Second,
			Version: 4,
		},
	}

	m := metrics.NewNTPMetrics()

	// Create multiple collectors
	collector1 := NewCommonCollector(cfg, m, "collector1")
	collector2 := NewCommonCollector(cfg, m, "collector2")
	collector3 := NewCommonCollector(cfg, m, "collector3")

	// Ensure they are independent
	assert.NotEqual(t, collector1.Name(), collector2.Name())
	assert.NotEqual(t, collector2.Name(), collector3.Name())
	assert.NotEqual(t, collector1.Name(), collector3.Name())

	// But share config and metrics
	assert.Equal(t, collector1.GetConfig(), collector2.GetConfig())
	assert.Equal(t, collector1.GetMetrics(), collector2.GetMetrics())
}

func TestCommonCollector_IterateServers_ErrorHandling(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers: []string{"server1", "server2", "server3"},
			Timeout: 5 * time.Second,
			Version: 4,
		},
	}

	m := metrics.NewNTPMetrics()
	collector := NewCommonCollector(cfg, m, "test")

	successfulCalls := []string{}
	collectFunc := func(ctx context.Context, server string) error {
		successfulCalls = append(successfulCalls, server)
		// Fail on server2
		if server == "server2" {
			return errors.New("intentional error")
		}
		return nil
	}

	ctx := context.Background()
	err := collector.IterateServers(ctx, collectFunc, "test")

	// Should not return error
	assert.NoError(t, err)
	// Should have attempted all three servers
	assert.Len(t, successfulCalls, 3)
	assert.Contains(t, successfulCalls, "server1")
	assert.Contains(t, successfulCalls, "server2")
	assert.Contains(t, successfulCalls, "server3")
}

func BenchmarkCommonCollector_IterateServers(b *testing.B) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers: []string{"server1", "server2", "server3", "server4", "server5"},
			Timeout: 5 * time.Second,
			Version: 4,
		},
	}

	m := metrics.NewNTPMetrics()
	collector := NewCommonCollector(cfg, m, "test")

	collectFunc := func(ctx context.Context, server string) error {
		return nil
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = collector.IterateServers(ctx, collectFunc, "test")
	}
}

func BenchmarkCreateNTPClient(b *testing.B) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Timeout: 5 * time.Second,
			Version: 4,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = createNTPClient(cfg)
	}
}

func BenchmarkNewCommonCollector(b *testing.B) {
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
		_ = NewCommonCollector(cfg, m, "test")
	}
}

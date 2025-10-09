package collector

import (
	"context"
	"fmt"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/ntp"
	"github.com/maximewewer/ntp-exporter/pkg/logger"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
)

// CommonCollector provides shared functionality for all collectors
type CommonCollector struct {
	config  *config.Config
	client  ntp.NTPQuerier
	metrics *metrics.NTPMetrics
	enabled bool
	name    string
}

// NewCommonCollector creates a new common collector base
func NewCommonCollector(cfg *config.Config, m *metrics.NTPMetrics, name string) *CommonCollector {
	return &CommonCollector{
		config:  cfg,
		client:  createNTPClient(cfg),
		metrics: m,
		enabled: true,
		name:    name,
	}
}

// Name returns the collector name
func (c *CommonCollector) Name() string {
	return c.name
}

// Enabled returns whether the collector is enabled
func (c *CommonCollector) Enabled() bool {
	return c.enabled
}

// GetConfig returns the configuration
func (c *CommonCollector) GetConfig() *config.Config {
	return c.config
}

// GetClient returns the NTP client
func (c *CommonCollector) GetClient() ntp.NTPQuerier {
	return c.client
}

// GetMetrics returns the metrics registry
func (c *CommonCollector) GetMetrics() *metrics.NTPMetrics {
	return c.metrics
}

// IterateServers iterates over all configured servers and collects metrics
// The collectFunc is called for each server to perform the actual collection
func (c *CommonCollector) IterateServers(ctx context.Context, collectFunc func(context.Context, string) error, metricType string) error {
	logger.Infof("collector", "Starting %s metrics collection with %d servers", metricType, len(c.config.NTP.Servers))

	for _, server := range c.config.NTP.Servers {
		if err := collectFunc(ctx, server); err != nil {
			logger.SafeWarn("collector", fmt.Sprintf("Failed to collect %s metrics", metricType), map[string]interface{}{
				"server": server,
				"error":  err.Error(),
			})
			continue
		}
	}

	return nil
}

// createNTPClient creates an NTP client based on configuration
// Wraps client with circuit breaker for fault tolerance
func createNTPClient(cfg *config.Config) ntp.NTPQuerier {
	var baseClient ntp.NTPQuerier

	if cfg.NTP.RateLimit.Enabled {
		baseClient = ntp.NewClientWithRateLimit(
			cfg.NTP.Timeout,
			cfg.NTP.Version,
			cfg.NTP.RateLimit.GlobalRate,
			cfg.NTP.RateLimit.PerServerRate,
			cfg.NTP.RateLimit.BurstSize,
		)
	} else {
		baseClient = ntp.NewClient(
			cfg.NTP.Timeout,
			cfg.NTP.Version,
		)
	}

	// Wrap with circuit breaker if enabled (enabled by default)
	if cfg.NTP.CircuitBreaker.Enabled {
		cbConfig := ntp.NewCircuitBreakerConfigWithThreshold(
			cfg.NTP.CircuitBreaker.MaxRequests,
			cfg.NTP.CircuitBreaker.Interval,
			cfg.NTP.CircuitBreaker.Timeout,
			cfg.NTP.CircuitBreaker.FailureThreshold,
		)
		return ntp.NewCircuitBreakerClient(baseClient, cbConfig)
	}

	return baseClient
}

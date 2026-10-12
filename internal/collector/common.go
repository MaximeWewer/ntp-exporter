// Package collector publishes the measurements of a probe cycle as Prometheus metrics.
//
// The probe package queries every target once per cycle; each publisher of
// this package (base, quality, security, hybrid) consumes the same
// measurements, so no publisher sends NTP packets on its own.
package collector

import (
	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
)

// CommonCollector provides shared functionality for all publishers
type CommonCollector struct {
	config  *config.Config
	metrics *metrics.NTPMetrics
	name    string
}

// NewCommonCollector creates a new common publisher base
func NewCommonCollector(cfg *config.Config, m *metrics.NTPMetrics, name string) *CommonCollector {
	return &CommonCollector{
		config:  cfg,
		metrics: m,
		name:    name,
	}
}

// Name returns the publisher name
func (c *CommonCollector) Name() string {
	return c.name
}

// GetConfig returns the configuration
func (c *CommonCollector) GetConfig() *config.Config {
	return c.config
}

// GetMetrics returns the metrics registry
func (c *CommonCollector) GetMetrics() *metrics.NTPMetrics {
	return c.metrics
}

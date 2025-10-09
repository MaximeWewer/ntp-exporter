package collector

import (
	"context"
	"fmt"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/ntp"
	"github.com/maximewewer/ntp-exporter/pkg/logger"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
)

// QualityCollector collects NTP quality metrics (jitter, stability, etc.)
type QualityCollector struct {
	*CommonCollector
}

// NewQualityCollector creates a new quality metrics collector
func NewQualityCollector(cfg *config.Config, m *metrics.NTPMetrics) *QualityCollector {
	return &QualityCollector{
		CommonCollector: NewCommonCollector(cfg, m, "quality"),
	}
}

// Collect collects quality metrics from all configured servers
func (c *QualityCollector) Collect(ctx context.Context) error {
	return c.IterateServers(ctx, c.collectFromServer, "quality")
}

// collectFromServer collects quality metrics from a single server
func (c *QualityCollector) collectFromServer(ctx context.Context, server string) error {
	cfg := c.GetConfig()
	client := c.GetClient()
	m := c.GetMetrics()

	var responses []*ntp.Response
	var err error
	var stats *ntp.Statistics

	// Use adaptive sampling if enabled
	if cfg.NTP.AdaptiveSampling.Enabled {
		sampler := ntp.NewAdaptiveSampler(ntp.AdaptiveSamplingConfig{
			DefaultSamples:   cfg.NTP.AdaptiveSampling.DefaultSamples,
			HighDriftSamples: cfg.NTP.AdaptiveSampling.HighDriftSamples,
			DriftThreshold:   cfg.NTP.AdaptiveSampling.DriftThreshold,
			MaxDuration:      cfg.NTP.AdaptiveSampling.MaxDuration,
		}, client)

		responses, err = sampler.Sample(ctx, server)
		if err != nil {
			logger.Error("collector", "Adaptive sampling failed", err)
			return fmt.Errorf("failed to adaptively sample server %s: %w", server, err)
		}

		// Calculate statistics with adaptive sample count
		stats = ntp.CalculateStatistics(responses, len(responses))

		logger.SafeDebug("collector", "Adaptive sampling completed", map[string]interface{}{
			"server":  server,
			"samples": len(responses),
		})
	} else {
		// Standard fixed sampling
		responses, err = client.QueryMultiple(ctx, server, cfg.NTP.SamplesPerServer)
		if err != nil {
			logger.Error("collector", "Multiple queries failed", err)
			return fmt.Errorf("failed to query multiple samples from server %s: %w", server, err)
		}

		// Calculate statistics
		stats = ntp.CalculateStatistics(responses, cfg.NTP.SamplesPerServer)
	}

	// Update quality metrics
	m.JitterSeconds.WithLabelValues(server).Set(stats.Jitter.Seconds())
	m.StabilitySeconds.WithLabelValues(server).Set(stats.StdDevOffset.Seconds())
	m.AsymmetrySeconds.WithLabelValues(server).Set(stats.Asymmetry.Seconds())
	m.SamplesCount.WithLabelValues(server).Set(float64(stats.SamplesCount))
	m.PacketLossRatio.WithLabelValues(server).Set(stats.PacketLossRatio)

	logger.SafeDebug("collector", "Quality metrics updated", map[string]interface{}{
		"server":    server,
		"jitter":    stats.Jitter.Seconds(),
		"stability": stats.StdDevOffset.Seconds(),
		"samples":   stats.SamplesCount,
	})

	return nil
}

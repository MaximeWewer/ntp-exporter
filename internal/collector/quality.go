package collector

import (
	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/probe"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
)

// QualityCollector publishes NTP quality metrics (jitter, stability, loss)
type QualityCollector struct {
	*CommonCollector
}

// NewQualityCollector creates a new quality metrics publisher
func NewQualityCollector(cfg *config.Config, m *metrics.NTPMetrics) *QualityCollector {
	return &QualityCollector{
		CommonCollector: NewCommonCollector(cfg, m, "quality"),
	}
}

// Publish updates quality metrics from the cycle measurements.
// Invalid responses (KoD, unsynchronized server...) count as lost samples.
func (c *QualityCollector) Publish(result *probe.CycleResult) {
	m := c.GetMetrics()

	for _, meas := range result.Measurements {
		server := meas.Target.Server
		stats := meas.Stats()

		m.PacketLossRatio.WithLabelValues(server).Set(stats.PacketLossRatio)
		m.SamplesCount.WithLabelValues(server).Set(float64(stats.SamplesCount))
		if stats.SamplesCount == 0 {
			continue
		}

		m.JitterSeconds.WithLabelValues(server).Set(stats.Jitter.Seconds())
		m.StabilitySeconds.WithLabelValues(server).Set(stats.StdDevOffset.Seconds())
		m.AsymmetrySeconds.WithLabelValues(server).Set(stats.Asymmetry.Seconds())
	}
}

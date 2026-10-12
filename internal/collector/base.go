package collector

import (
	"strconv"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/ntp"
	"github.com/maximewewer/ntp-exporter/internal/probe"
	"github.com/maximewewer/ntp-exporter/pkg/logger"
	"github.com/maximewewer/ntp-exporter/pkg/mathutil"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
)

// BaseCollector publishes standard NTP metrics (offset, RTT, stratum, pools)
type BaseCollector struct {
	*CommonCollector
}

// NewBaseCollector creates a new base NTP publisher
func NewBaseCollector(cfg *config.Config, m *metrics.NTPMetrics) *BaseCollector {
	return &BaseCollector{
		CommonCollector: NewCommonCollector(cfg, m, "base"),
	}
}

// Publish updates base metrics from the cycle measurements
func (c *BaseCollector) Publish(result *probe.CycleResult) {
	m := c.GetMetrics()

	successCount := 0
	for _, meas := range result.Measurements {
		if !meas.Reachable() {
			m.ServerReachable.WithLabelValues(meas.Target.Server).Set(0)
			continue
		}

		successCount++
		m.ServerReachable.WithLabelValues(meas.Target.Server).Set(1)

		// Invalid responses (KoD, unsynchronized server, failed validation) are
		// not time measurements: keep the previous values instead of publishing garbage
		best := meas.Best()
		if best == nil {
			logger.SafeDebug("collector", "No valid NTP response", map[string]interface{}{
				"server": meas.Target.Server,
				"reason": meas.Responses[0].InvalidReason(),
			})
			continue
		}
		c.updateMetrics(best)
	}

	for _, pool := range result.Pools {
		c.publishPool(result, pool)
	}

	m.ExporterScrapeDuration.Observe(result.Duration.Seconds())
	if successCount > 0 {
		m.ExporterScrapesTotal.WithLabelValues("success").Inc()
	} else {
		m.ExporterScrapesTotal.WithLabelValues("failure").Inc()
	}

	logger.SafeDebug("collector", "NTP measurements published", map[string]interface{}{
		"targets":   len(result.Measurements),
		"reachable": successCount,
		"duration":  result.Duration.Seconds(),
	})
}

// publishPool updates pool-level metrics
func (c *BaseCollector) publishPool(result *probe.CycleResult, pool probe.PoolResult) {
	m := c.GetMetrics()

	m.PoolServersTotal.WithLabelValues(pool.Name).Set(float64(pool.Total))
	m.PoolDNSResolutionSeconds.WithLabelValues(pool.Name).Set(pool.DNSDuration.Seconds())

	active := 0
	var bestOffset *ntp.Response
	for _, meas := range result.PoolMeasurements(pool.Name) {
		best := meas.Best()
		if best == nil {
			continue
		}
		active++
		if bestOffset == nil || mathutil.AbsDuration(best.Offset) < mathutil.AbsDuration(bestOffset.Offset) {
			bestOffset = best
		}
	}

	m.PoolServersActive.WithLabelValues(pool.Name).Set(float64(active))
	if bestOffset != nil {
		m.PoolBestOffsetSeconds.WithLabelValues(pool.Name).Set(bestOffset.Offset.Seconds())
	}
}

// updateMetrics updates Prometheus metrics from a valid NTP response
func (c *BaseCollector) updateMetrics(resp *ntp.Response) {
	cfg := c.GetConfig()
	m := c.GetMetrics()

	m.OffsetSeconds.WithLabelValues(
		resp.Server,
		strconv.Itoa(int(resp.Stratum)),
		strconv.Itoa(cfg.NTP.Version),
	).Set(resp.Offset.Seconds())

	// Check if offset exceeds configured threshold
	offsetExceeded := 0.0
	if resp.Offset.Abs() > cfg.NTP.MaxClockOffset {
		offsetExceeded = 1.0
	}
	m.ClockOffsetExceeded.WithLabelValues(resp.Server).Set(offsetExceeded)

	m.RTTSeconds.WithLabelValues(resp.Server).Set(resp.RTT.Seconds())
	m.Stratum.WithLabelValues(resp.Server).Set(float64(resp.Stratum))
	m.ReferenceTimestamp.WithLabelValues(resp.Server).Set(float64(resp.ReferenceTime.Unix()))
	m.RootDelay.WithLabelValues(resp.Server).Set(resp.RootDelay.Seconds())
	m.RootDispersion.WithLabelValues(resp.Server).Set(resp.RootDispersion.Seconds())
	m.RootDistance.WithLabelValues(resp.Server).Set(resp.RootDistance.Seconds())
	m.Precision.WithLabelValues(resp.Server).Set(resp.Precision.Seconds())
	m.LeapIndicator.WithLabelValues(resp.Server).Set(float64(resp.LeapIndicator))
}

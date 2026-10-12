package collector

import (
	"math"
	"os"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/ntp"
	"github.com/maximewewer/ntp-exporter/internal/probe"
	"github.com/maximewewer/ntp-exporter/pkg/logger"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
)

// KernelReader reads the kernel time state
type KernelReader interface {
	Read() (*ntp.KernelTimex, error)
}

// HybridCollector publishes kernel metrics and their correlation with NTP measurements.
// It is designed for agent mode (DaemonSet), where the node clock is observed.
type HybridCollector struct {
	*CommonCollector
	kernelReader KernelReader
	nodeName     string
}

// NewHybridCollector creates a new hybrid metrics publisher
func NewHybridCollector(cfg *config.Config, m *metrics.NTPMetrics) *HybridCollector {
	// Get node name from environment (set by DaemonSet)
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			nodeName = "unknown"
		} else {
			nodeName = hostname
		}
	}

	return &HybridCollector{
		CommonCollector: NewCommonCollector(cfg, m, "hybrid"),
		kernelReader:    ntp.NewKernelReader(cfg.NTP.EnableKernel),
		nodeName:        nodeName,
	}
}

// Publish reads the kernel state and correlates it with the cycle measurements
func (c *HybridCollector) Publish(result *probe.CycleResult) {
	kernelState, err := c.kernelReader.Read()
	if err != nil {
		logger.SafeWarn("collector", "Failed to read kernel timex state", map[string]interface{}{
			"error": err.Error(),
		})
		return
	}

	c.updateKernelMetrics(kernelState)

	for _, meas := range result.Measurements {
		if best := meas.Best(); best != nil {
			c.correlate(meas.Target.Server, best, kernelState)
		}
	}
}

// updateKernelMetrics updates kernel-specific metrics
func (c *HybridCollector) updateKernelMetrics(kernelState *ntp.KernelTimex) {
	m := c.GetMetrics()

	m.KernelOffsetSeconds.WithLabelValues(c.nodeName).Set(kernelState.GetOffsetSeconds())
	m.KernelFrequencyPPM.WithLabelValues(c.nodeName).Set(kernelState.GetFrequencyPPM())
	m.KernelMaxErrorSeconds.WithLabelValues(c.nodeName).Set(kernelState.GetMaxErrorSeconds())
	m.KernelEstErrorSeconds.WithLabelValues(c.nodeName).Set(kernelState.GetEstErrorSeconds())
	m.KernelPrecisionSeconds.WithLabelValues(c.nodeName).Set(kernelState.GetPrecisionSeconds())
	m.KernelStatusCode.WithLabelValues(c.nodeName).Set(float64(kernelState.StatusCode))

	// Sync status: only set the current status with value 1
	if kernelState.IsSynchronized() {
		m.KernelSyncStatus.WithLabelValues(c.nodeName, "synchronized").Set(1)
		m.KernelSyncStatus.DeleteLabelValues(c.nodeName, "unsynchronized")
	} else {
		m.KernelSyncStatus.WithLabelValues(c.nodeName, "unsynchronized").Set(1)
		m.KernelSyncStatus.DeleteLabelValues(c.nodeName, "synchronized")
	}
}

// correlate compares an NTP measurement with the kernel state
func (c *HybridCollector) correlate(server string, resp *ntp.Response, kernelState *ntp.KernelTimex) {
	m := c.GetMetrics()

	ntpOffset := resp.Offset.Seconds()
	kernelOffset := kernelState.GetOffsetSeconds()

	m.NTPKernelDivergence.WithLabelValues(c.nodeName, server).Set(math.Abs(ntpOffset - kernelOffset))
	m.NTPKernelCoherence.WithLabelValues(c.nodeName, server).Set(calculateCoherence(ntpOffset, kernelOffset))
}

// calculateCoherence calculates a coherence score between NTP and kernel offsets
// Returns a value between 0 and 1, where:
// - 1.0 = perfect agreement (< 1ms difference)
// - 0.9-1.0 = excellent (1-5ms difference)
// - 0.7-0.9 = good (5-10ms difference)
// - 0.5-0.7 = acceptable (10-50ms difference)
// - 0.0-0.5 = poor (> 50ms difference)
func calculateCoherence(ntpOffset, kernelOffset float64) float64 {
	divergence := math.Abs(ntpOffset - kernelOffset)

	const (
		perfectThreshold    = 0.001
		excellentThreshold  = 0.005
		goodThreshold       = 0.010
		acceptableThreshold = 0.050
	)

	switch {
	case divergence < perfectThreshold:
		return 1.0
	case divergence < excellentThreshold:
		return 1.0 - ((divergence - perfectThreshold) / (excellentThreshold - perfectThreshold) * 0.1)
	case divergence < goodThreshold:
		return 0.9 - ((divergence - excellentThreshold) / (goodThreshold - excellentThreshold) * 0.2)
	case divergence < acceptableThreshold:
		return 0.7 - ((divergence - goodThreshold) / (acceptableThreshold - goodThreshold) * 0.2)
	default:
		return 0.5 * math.Exp(-divergence*5)
	}
}

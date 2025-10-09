package collector

import (
	"context"
	"math"
	"os"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/ntp"
	"github.com/maximewewer/ntp-exporter/pkg/logger"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
)

// HybridCollector collects both NTP and kernel metrics for correlation analysis
// This collector is designed for Agent mode (DaemonSet) where kernel access is available
type HybridCollector struct {
	*CommonCollector
	kernelReader *ntp.KernelReader
	nodeName     string
}

// NewHybridCollector creates a new hybrid metrics collector
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

// Collect collects both NTP and kernel metrics and correlates them
func (c *HybridCollector) Collect(ctx context.Context) error {
	start := time.Now()
	defer func() {
		c.GetMetrics().CollectorDurationSeconds.WithLabelValues(c.Name()).Observe(time.Since(start).Seconds())
	}()

	cfg := c.GetConfig()

	// Skip if kernel monitoring is not enabled
	if !cfg.NTP.EnableKernel {
		logger.SafeDebug("collector", "Kernel monitoring disabled, skipping hybrid collection", nil)
		return nil
	}

	// Read kernel state
	kernelState, err := c.kernelReader.Read()
	if err != nil {
		logger.SafeWarn("collector", "Failed to read kernel timex state", map[string]interface{}{
			"error": err.Error(),
		})
		// Don't fail the collection, just skip kernel metrics
		return nil
	}

	// Update kernel metrics
	c.updateKernelMetrics(kernelState)

	// Collect NTP metrics and calculate divergence
	return c.IterateServers(ctx, func(ctx context.Context, server string) error {
		return c.collectAndCorrelate(ctx, server, kernelState)
	}, "hybrid")
}

// updateKernelMetrics updates kernel-specific metrics
func (c *HybridCollector) updateKernelMetrics(kernelState *ntp.KernelTimex) {
	m := c.GetMetrics()

	// Basic kernel metrics
	m.KernelOffsetSeconds.WithLabelValues(c.nodeName).Set(kernelState.GetOffsetSeconds())
	m.KernelFrequencyPPM.WithLabelValues(c.nodeName).Set(kernelState.GetFrequencyPPM())
	m.KernelMaxErrorSeconds.WithLabelValues(c.nodeName).Set(kernelState.GetMaxErrorSeconds())
	m.KernelEstErrorSeconds.WithLabelValues(c.nodeName).Set(kernelState.GetEstErrorSeconds())
	m.KernelPrecisionSeconds.WithLabelValues(c.nodeName).Set(kernelState.GetPrecisionSeconds())
	m.KernelStatusCode.WithLabelValues(c.nodeName).Set(float64(kernelState.StatusCode))

	// Sync status (1=synced, 0=unsynced)
	syncValue := float64(0)
	if kernelState.IsSynchronized() {
		syncValue = 1
	}
	m.KernelSyncStatus.WithLabelValues(c.nodeName, kernelState.SyncStatus).Set(syncValue)

	logger.SafeDebug("collector", "Kernel metrics updated", map[string]interface{}{
		"node":         c.nodeName,
		"offset_us":    kernelState.Offset.Microseconds(),
		"freq_ppm":     kernelState.GetFrequencyPPM(),
		"synchronized": kernelState.IsSynchronized(),
		"status":       kernelState.SyncStatus,
	})
}

// collectAndCorrelate collects NTP metrics and correlates with kernel state
func (c *HybridCollector) collectAndCorrelate(ctx context.Context, server string, kernelState *ntp.KernelTimex) error {
	client := c.GetClient()
	cfg := c.GetConfig()
	m := c.GetMetrics()

	// Query NTP server for offset
	resp, err := client.Query(ctx, server)
	if err != nil {
		logger.SafeDebug("ntp", "NTP query failed for correlation", map[string]interface{}{
			"server": server,
			"error":  err.Error(),
		})
		return err
	}

	ntpOffset := resp.Offset.Seconds()
	kernelOffset := kernelState.GetOffsetSeconds()

	// Calculate divergence (absolute difference)
	divergence := math.Abs(ntpOffset - kernelOffset)

	// Calculate coherence score (0-1, where 1 is perfect agreement)
	coherence := calculateCoherence(ntpOffset, kernelOffset, cfg)

	// Update metrics
	m.NTPKernelDivergence.WithLabelValues(c.nodeName, server).Set(divergence)
	m.NTPKernelCoherence.WithLabelValues(c.nodeName, server).Set(coherence)

	logger.SafeDebug("collector", "NTP-Kernel correlation updated", map[string]interface{}{
		"node":          c.nodeName,
		"server":        server,
		"ntp_offset":    ntpOffset,
		"kernel_offset": kernelOffset,
		"divergence":    divergence,
		"coherence":     coherence,
	})

	// Log warnings for significant divergence
	if divergence > 0.010 { // 10ms threshold
		logger.SafeWarn("collector", "Significant NTP-Kernel divergence detected", map[string]interface{}{
			"node":          c.nodeName,
			"server":        server,
			"divergence":    divergence,
			"ntp_offset":    ntpOffset,
			"kernel_offset": kernelOffset,
		})
	}

	return nil
}

// calculateCoherence calculates a coherence score between NTP and kernel offsets
// Returns a value between 0 and 1, where:
// - 1.0 = perfect agreement (< 1ms difference)
// - 0.9-1.0 = excellent (1-5ms difference)
// - 0.7-0.9 = good (5-10ms difference)
// - 0.5-0.7 = acceptable (10-50ms difference)
// - 0.0-0.5 = poor (> 50ms difference)
func calculateCoherence(ntpOffset, kernelOffset float64, cfg *config.Config) float64 {
	divergence := math.Abs(ntpOffset - kernelOffset)

	// Thresholds in seconds
	const (
		perfectThreshold    = 0.001 // 1ms
		excellentThreshold  = 0.005 // 5ms
		goodThreshold       = 0.010 // 10ms
		acceptableThreshold = 0.050 // 50ms
	)

	if divergence < perfectThreshold {
		return 1.0
	} else if divergence < excellentThreshold {
		// Linear interpolation between 1.0 and 0.9
		return 1.0 - ((divergence - perfectThreshold) / (excellentThreshold - perfectThreshold) * 0.1)
	} else if divergence < goodThreshold {
		// Linear interpolation between 0.9 and 0.7
		return 0.9 - ((divergence - excellentThreshold) / (goodThreshold - excellentThreshold) * 0.2)
	} else if divergence < acceptableThreshold {
		// Linear interpolation between 0.7 and 0.5
		return 0.7 - ((divergence - goodThreshold) / (acceptableThreshold - goodThreshold) * 0.2)
	} else {
		// Exponential decay for poor coherence
		// At 100ms = 0.25, at 500ms = 0.05, at 1s = 0.0
		score := 0.5 * math.Exp(-divergence*5)
		if score < 0 {
			return 0
		}
		return score
	}
}

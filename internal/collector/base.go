// Package collector provides specialized NTP metrics collectors.
//
// The package includes three main collector types:
//   - BaseCollector: Collects standard NTP metrics (offset, RTT, stratum)
//   - QualityCollector: Collects quality metrics (jitter, stability, packet loss)
//   - SecurityCollector: Collects security metrics (trust scores, anomalies)
//
// All collectors implement the Collector interface and can be managed through
// a Registry for coordinated metrics collection.
//
// Usage:
//
//	cfg := config.Load("config.yaml")
//	registry := collector.NewRegistry()
//	registry.Register(collector.NewBaseCollector(cfg))
//	if err := registry.CollectAll(ctx); err != nil {
//	    log.Fatal(err)
//	}
package collector

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/ntp"
	"github.com/maximewewer/ntp-exporter/pkg/logger"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
)

// BaseCollector collects standard NTP metrics
type BaseCollector struct {
	*CommonCollector
}

// NewBaseCollector creates a new base NTP collector
func NewBaseCollector(cfg *config.Config, m *metrics.NTPMetrics) *BaseCollector {
	return &BaseCollector{
		CommonCollector: NewCommonCollector(cfg, m, "base"),
	}
}

// Collect collects NTP metrics from all configured servers
func (c *BaseCollector) Collect(ctx context.Context) error {
	start := time.Now()
	defer func() {
		// Record collector duration
		c.GetMetrics().CollectorDurationSeconds.WithLabelValues(c.Name()).Observe(time.Since(start).Seconds())
	}()

	cfg := c.GetConfig()
	m := c.GetMetrics()

	logger.Infof("collector", "Starting NTP collection with %d servers", len(cfg.NTP.Servers))

	successCount := 0
	failCount := 0

	// Collect from individual servers
	for _, server := range cfg.NTP.Servers {
		if err := c.collectFromServer(ctx, server); err != nil {
			logger.SafeWarn("collector", "Failed to collect from server", map[string]interface{}{
				"server": server,
				"error":  err.Error(),
			})
			failCount++
			m.ServerReachable.WithLabelValues(server).Set(0)
		} else {
			successCount++
			m.ServerReachable.WithLabelValues(server).Set(1)
		}
	}

	// Collect from pools
	for _, pool := range cfg.NTP.Pools {
		if err := c.collectFromPool(ctx, pool); err != nil {
			logger.SafeWarn("collector", "Failed to collect from pool", map[string]interface{}{
				"pool":  pool.Name,
				"error": err.Error(),
			})
			failCount++
		} else {
			successCount++
		}
	}

	duration := time.Since(start)
	m.ExporterScrapeDuration.Observe(duration.Seconds())

	if successCount > 0 {
		m.ExporterScrapesTotal.WithLabelValues("success").Inc()
	} else {
		m.ExporterScrapesTotal.WithLabelValues("failure").Inc()
	}

	logger.SafeInfo("collector", "NTP collection completed", map[string]interface{}{
		"success":  successCount,
		"failed":   failCount,
		"duration": duration.Seconds(),
	})

	return nil
}

// collectFromServer collects metrics from a single NTP server
func (c *BaseCollector) collectFromServer(ctx context.Context, server string) error {
	// Query NTP server
	resp, err := c.GetClient().Query(ctx, server)
	if err != nil {
		logger.Error("collector", "Query failed", err)
		return fmt.Errorf("failed to query NTP server %s: %w", server, err)
	}

	// Update metrics
	c.updateMetrics(resp)

	return nil
}

// collectFromPool collects metrics from an NTP pool
func (c *BaseCollector) collectFromPool(ctx context.Context, poolCfg config.PoolConfig) error {
	cfg := c.GetConfig()
	m := c.GetMetrics()

	pool := ntp.NewPool(
		poolCfg.Name,
		poolCfg.Strategy,
		poolCfg.MaxServers,
		poolCfg.Fallback,
		c.GetClient(),
	)

	// Enable worker pool if configured and strategy is 'all'
	if cfg.NTP.WorkerPool.Enabled && poolCfg.Strategy == "all" {
		pool.EnableWorkerPool(cfg.NTP.WorkerPool.Size)
	}

	resp, err := pool.Query(ctx, cfg.NTP.SamplesPerServer)
	if err != nil {
		logger.Error("collector", "Pool query failed", err)
		return fmt.Errorf("failed to query NTP pool %s: %w", poolCfg.Name, err)
	}

	// Update pool metrics
	m.PoolServersActive.WithLabelValues(resp.PoolName).Set(float64(resp.ActiveServers))
	m.PoolServersTotal.WithLabelValues(resp.PoolName).Set(float64(resp.TotalServers))
	m.PoolDNSResolutionSeconds.WithLabelValues(resp.PoolName).Set(resp.DNSResolution.Seconds())
	m.PoolBestOffsetSeconds.WithLabelValues(resp.PoolName).Set(resp.BestOffset.Seconds())

	// Update metrics for each server in the pool
	for _, serverResp := range resp.Responses {
		c.updateMetrics(serverResp)
	}

	return nil
}

// updateMetrics updates Prometheus metrics from an NTP response
func (c *BaseCollector) updateMetrics(resp *ntp.Response) {
	cfg := c.GetConfig()
	m := c.GetMetrics()

	labels := map[string]string{
		"server":  resp.Server,
		"stratum": strconv.Itoa(int(resp.Stratum)),
		"version": strconv.Itoa(cfg.NTP.Version),
	}

	// Base metrics
	m.OffsetSeconds.WithLabelValues(
		labels["server"],
		labels["stratum"],
		labels["version"],
	).Set(resp.Offset.Seconds())

	m.RTTSeconds.WithLabelValues(resp.Server).Set(resp.RTT.Seconds())
	m.Stratum.WithLabelValues(resp.Server).Set(float64(resp.Stratum))
	m.ReferenceTimestamp.WithLabelValues(resp.Server).Set(float64(resp.ReferenceTime.Unix()))
	m.RootDelay.WithLabelValues(resp.Server).Set(resp.RootDelay.Seconds())
	m.RootDispersion.WithLabelValues(resp.Server).Set(resp.RootDispersion.Seconds())
	m.RootDistance.WithLabelValues(resp.Server).Set(resp.RootDistance.Seconds())
	m.Precision.WithLabelValues(resp.Server).Set(resp.Precision.Seconds())
	m.LeapIndicator.WithLabelValues(resp.Server).Set(float64(resp.LeapIndicator))

	logger.SafeDebug("collector", "Metrics updated", map[string]interface{}{
		"server":  resp.Server,
		"offset":  resp.Offset.Seconds(),
		"rtt":     resp.RTT.Seconds(),
		"stratum": resp.Stratum,
	})
}

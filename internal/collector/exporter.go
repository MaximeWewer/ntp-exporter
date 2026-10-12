// Package collector exposes the measurements of the last probe cycle as
// Prometheus metrics.
//
// The probe package queries every target once per cycle. The Exporter keeps
// a snapshot of the last cycle and emits constant metrics from it at scrape
// time: a target that stops answering no longer exposes stale measurements,
// and a pool member that disappears from DNS no longer exposes any series.
package collector

import (
	"math"
	"os"
	"sync"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/ntp"
	"github.com/maximewewer/ntp-exporter/internal/probe"
	"github.com/maximewewer/ntp-exporter/pkg/logger"
	"github.com/maximewewer/ntp-exporter/pkg/mathutil"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

// KernelReader reads the kernel time state
type KernelReader interface {
	Read() (*ntp.KernelTimex, error)
}

// Exporter is a prometheus.Collector built on the last probe cycle
type Exporter struct {
	cfg       *config.Config
	metrics   *metrics.NTPMetrics
	descs     *descs
	validator *ntp.Validator

	// Kernel state, only read when kernel monitoring is enabled
	kernelReader KernelReader
	nodeName     string

	mu          sync.RWMutex
	last        *probe.CycleResult
	kernelState *ntp.KernelTimex
	lastSuccess map[string]time.Time
}

// NewExporter creates the exporter. Counters and exporter operational
// metrics remain owned by m.
func NewExporter(cfg *config.Config, m *metrics.NTPMetrics) *Exporter {
	e := &Exporter{
		cfg:         cfg,
		metrics:     m,
		descs:       newDescs(cfg.Metrics.Namespace, cfg.Metrics.Subsystem),
		validator:   ntp.NewValidator(),
		lastSuccess: make(map[string]time.Time),
	}

	if cfg.NTP.EnableKernel {
		e.kernelReader = ntp.NewKernelReader(true)
		e.nodeName = nodeName()
	}

	return e
}

// nodeName returns the node name set by the DaemonSet, or the hostname
func nodeName() string {
	if name := os.Getenv("NODE_NAME"); name != "" {
		return name
	}
	if hostname, err := os.Hostname(); err == nil {
		return hostname
	}
	return "unknown"
}

// Update stores the result of a cycle and updates counters
func (e *Exporter) Update(result *probe.CycleResult) {
	var kernelState *ntp.KernelTimex
	if e.kernelReader != nil {
		state, err := e.kernelReader.Read()
		if err != nil {
			logger.SafeWarn("collector", "Failed to read kernel timex state", map[string]interface{}{
				"error": err.Error(),
			})
		}
		kernelState = state
	}

	e.updateCounters(result)

	e.mu.Lock()
	defer e.mu.Unlock()

	current := make(map[string]bool, len(result.Measurements))
	for _, meas := range result.Measurements {
		current[meas.Target.Server] = true
		if meas.Best() != nil {
			e.lastSuccess[meas.Target.Server] = meas.Timestamp
		}
	}

	// Forget targets that are no longer probed (pool members gone from DNS)
	for server := range e.lastSuccess {
		if !current[server] {
			delete(e.lastSuccess, server)
		}
	}
	if e.last != nil {
		for _, meas := range e.last.Measurements {
			if !current[meas.Target.Server] {
				e.deleteServerCounters(meas.Target.Server)
			}
		}
	}

	e.last = result
	e.kernelState = kernelState
}

// updateCounters increments cumulative counters and exporter metrics
func (e *Exporter) updateCounters(result *probe.CycleResult) {
	m := e.metrics

	successCount := 0
	for _, meas := range result.Measurements {
		if meas.Reachable() {
			successCount++
		}
		// A skipped target carries its previous measurement: already counted
		if meas.Skipped {
			continue
		}
		for _, resp := range meas.Responses {
			server := meas.Target.Server
			if resp.IsKissOfDeath() {
				m.KissOfDeathTotal.WithLabelValues(server, resp.KissCode).Inc()
				logger.SafeWarn("collector", "Kiss-of-Death received", map[string]interface{}{
					"server":   server,
					"kod_code": resp.KissCode,
				})
			}
			if resp.IsSuspicious() {
				m.ServerSuspiciousTotal.WithLabelValues(server, e.validator.GetSuspicionReason(resp)).Inc()
			}
			if !resp.IsValid() {
				m.MalformedResponsesTotal.WithLabelValues(server).Inc()
			}
		}
	}

	m.ExporterScrapeDuration.Observe(result.Duration.Seconds())
	m.ExporterCycleDuration.Set(result.Duration.Seconds())
	if result.Overrun {
		m.ExporterCycleOverrunTotal.Inc()
	}
	if successCount > 0 {
		m.ExporterScrapesTotal.WithLabelValues("success").Inc()
	} else {
		m.ExporterScrapesTotal.WithLabelValues("failure").Inc()
	}
}

// deleteServerCounters removes the counter series of a target no longer probed
func (e *Exporter) deleteServerCounters(server string) {
	labels := prometheus.Labels{"server": server}
	e.metrics.KissOfDeathTotal.DeletePartialMatch(labels)
	e.metrics.ServerSuspiciousTotal.DeletePartialMatch(labels)
	e.metrics.MalformedResponsesTotal.DeletePartialMatch(labels)
}

// Describe implements prometheus.Collector
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range e.descs.all() {
		ch <- d
	}
}

// Collect implements prometheus.Collector
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.last != nil {
		for _, meas := range e.last.Measurements {
			e.collectMeasurement(ch, meas)
		}
		for _, pool := range e.last.Pools {
			e.collectPool(ch, pool)
		}
	}

	if e.kernelState != nil {
		e.collectKernel(ch)
	}
}

func gauge(ch chan<- prometheus.Metric, desc *prometheus.Desc, value float64, labels ...string) {
	ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, value, labels...)
}

// collectMeasurement emits the metrics of one target
func (e *Exporter) collectMeasurement(ch chan<- prometheus.Metric, meas *probe.Measurement) {
	d := e.descs
	server := meas.Target.Server

	if meas.SuspendCode != "" {
		gauge(ch, d.up, 0, server)
		gauge(ch, d.suspended, 1, server, meas.SuspendCode)
		if ts, ok := e.lastSuccess[server]; ok {
			gauge(ch, d.lastSuccess, float64(ts.UnixNano())/1e9, server)
		}
		return
	}

	up := 0.0
	if meas.Reachable() {
		up = 1
	}
	gauge(ch, d.up, up, server)
	if ts, ok := e.lastSuccess[server]; ok {
		gauge(ch, d.lastSuccess, float64(ts.UnixNano())/1e9, server)
	}

	stats := meas.Stats()
	gauge(ch, d.packetLoss, stats.PacketLossRatio, server)
	gauge(ch, d.samples, float64(stats.SamplesCount), server)

	if meas.Reachable() {
		ref := meas.Best()
		if ref == nil {
			ref = meas.Responses[0]
		}
		gauge(ch, d.trustScore, e.validator.Validate(ref).TrustScore, server)
	}

	// Invalid responses (KoD, unsynchronized server, failed validation) are
	// not time measurements: nothing is published for them
	best := meas.Best()
	if best == nil {
		return
	}

	offsetExceeded := 0.0
	if best.Offset.Abs() > e.cfg.NTP.MaxClockOffset {
		offsetExceeded = 1
	}

	gauge(ch, d.offset, best.Offset.Seconds(), server)
	gauge(ch, d.offsetMedian, stats.MedianOffset.Seconds(), server)
	gauge(ch, d.offsetExceeded, offsetExceeded, server)
	gauge(ch, d.rtt, best.RTT.Seconds(), server)
	gauge(ch, d.stratum, float64(best.Stratum), server)
	if !best.ReferenceTime.IsZero() && !best.Time.IsZero() {
		gauge(ch, d.referenceAge, best.Time.Sub(best.ReferenceTime).Seconds(), server)
	}
	gauge(ch, d.rootDelay, best.RootDelay.Seconds(), server)
	gauge(ch, d.rootDispersion, best.RootDispersion.Seconds(), server)
	gauge(ch, d.rootDistance, best.RootDistance.Seconds(), server)
	gauge(ch, d.precision, best.Precision.Seconds(), server)
	gauge(ch, d.leapIndicator, float64(best.LeapIndicator), server)

	gauge(ch, d.jitter, stats.Jitter.Seconds(), server)
	gauge(ch, d.offsetStdDev, stats.StdDevOffset.Seconds(), server)
	gauge(ch, d.rttStdDev, stats.StdDevRTT.Seconds(), server)

	if e.kernelState != nil {
		ntpOffset := best.Offset.Seconds()
		kernelOffset := e.kernelState.GetOffsetSeconds()
		gauge(ch, d.kernelDivergence, math.Abs(ntpOffset-kernelOffset), e.nodeName, server)
		gauge(ch, d.kernelCoherence, calculateCoherence(ntpOffset, kernelOffset), e.nodeName, server)
	}
}

// collectPool emits pool-level metrics
func (e *Exporter) collectPool(ch chan<- prometheus.Metric, pool probe.PoolResult) {
	d := e.descs

	gauge(ch, d.poolTotal, float64(pool.Total), pool.Name)
	gauge(ch, d.poolDNS, pool.DNSDuration.Seconds(), pool.Name)

	active := 0
	var bestOffset *ntp.Response
	for _, meas := range e.last.PoolMeasurements(pool.Name) {
		best := meas.Best()
		if best == nil {
			continue
		}
		active++
		if bestOffset == nil || mathutil.AbsDuration(best.Offset) < mathutil.AbsDuration(bestOffset.Offset) {
			bestOffset = best
		}
	}

	gauge(ch, d.poolActive, float64(active), pool.Name)
	if bestOffset != nil {
		gauge(ch, d.poolBestOffset, bestOffset.Offset.Seconds(), pool.Name)
	}
}

// collectKernel emits kernel state metrics
func (e *Exporter) collectKernel(ch chan<- prometheus.Metric) {
	d := e.descs
	k := e.kernelState
	node := e.nodeName

	gauge(ch, d.kernelOffset, k.GetOffsetSeconds(), node)
	gauge(ch, d.kernelFrequency, k.GetFrequencyPPM(), node)
	gauge(ch, d.kernelMaxError, k.GetMaxErrorSeconds(), node)
	gauge(ch, d.kernelEstError, k.GetEstErrorSeconds(), node)
	gauge(ch, d.kernelPrecision, k.GetPrecisionSeconds(), node)
	gauge(ch, d.kernelStatusCode, float64(k.StatusCode), node)

	status := "unsynchronized"
	if k.IsSynchronized() {
		status = "synchronized"
	}
	gauge(ch, d.kernelSyncStatus, 1, node, status)
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

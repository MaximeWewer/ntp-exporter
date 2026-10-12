// Package collector exposes the measurements of the last probe cycle as
// Prometheus metrics.
//
// The probe package queries every target once per cycle. The Exporter keeps
// a snapshot of the last cycle and emits constant metrics from it at scrape
// time: a target that stops answering no longer exposes stale measurements,
// and a pool member that disappears from DNS no longer exposes any series.
package collector

import (
	"errors"
	"fmt"
	"os"
	"strconv"
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
	cfg     *config.Config
	metrics *metrics.NTPMetrics
	descs   *descs

	// Kernel state, only read when kernel monitoring is enabled
	kernelReader KernelReader
	nodeName     string

	mu          sync.RWMutex
	updatedAt   time.Time
	last        *probe.CycleResult
	consensus   *probe.Consensus
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
		lastSuccess: make(map[string]time.Time),
	}

	if cfg.NTP.Kernel.Enabled {
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

	var consensus *probe.Consensus
	if e.cfg.NTP.ConsensusEnabled() {
		consensus = probe.ComputeConsensus(result.Measurements)
		if consensus != nil && len(consensus.Falsetickers) > 0 {
			servers := make([]string, 0, len(consensus.Falsetickers))
			for s := range consensus.Falsetickers {
				servers = append(servers, s)
			}
			logger.SafeWarn("collector", "Falsetickers detected", map[string]interface{}{
				"servers": servers,
			})
		}
	}

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

	e.updatedAt = time.Now()
	e.last = result
	e.consensus = consensus
	e.kernelState = kernelState
}

// Ready returns nil when a cycle has completed recently (within 3 intervals)
func (e *Exporter) Ready() error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.last == nil {
		return errors.New("no collection cycle completed yet")
	}
	if age := time.Since(e.updatedAt); age > 3*e.cfg.NTP.ScrapeInterval {
		return fmt.Errorf("last collection cycle is %s old", age.Round(time.Second))
	}
	return nil
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
		server := meas.Target.Server
		for _, resp := range meas.Responses {
			if resp.IsKissOfDeath() {
				m.KissOfDeathTotal.WithLabelValues(server, resp.KissCode).Inc()
			}
			if reason := resp.InvalidReason(); reason != "" {
				m.InvalidResponsesTotal.WithLabelValues(server, reason).Inc()
			}
		}
		if lost := meas.Sent - len(meas.Responses); lost > 0 {
			m.InvalidResponsesTotal.WithLabelValues(server, ntp.ReasonTimeout).Add(float64(lost))
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
	e.metrics.InvalidResponsesTotal.DeletePartialMatch(labels)
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

	if e.consensus != nil {
		e.collectConsensus(ch)
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
		valid := 0.0
		if meas.Best() != nil {
			valid = 1
		}
		gauge(ch, d.responseValid, valid, server)
	}

	// Invalid responses (KoD, unsynchronized server, failed validation) are
	// not time measurements: nothing is published for them
	best := meas.Best()
	if best == nil {
		return
	}

	gauge(ch, d.info, 1, server, meas.Target.Address, meas.Target.Pool, best.ReferenceIDString(),
		strconv.Itoa(int(best.Stratum)), strconv.Itoa(int(best.LeapIndicator)), strconv.Itoa(e.cfg.NTP.Version))

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

	if e.kernelState != nil && e.kernelState.IsSynchronized() {
		e.collectBoundViolation(ch, server, best)
	}
}

// collectBoundViolation compares the offset measured from the outside with
// the error bounds the kernel announces. The root distance of the server
// (which includes half the round-trip delay) is the measurement uncertainty:
// a violation means the time daemon underestimates its own error.
func (e *Exporter) collectBoundViolation(ch chan<- prometheus.Metric, server string, best *ntp.Response) {
	excess := best.Offset.Abs() - best.RootDistance
	for _, bound := range []struct {
		name  string
		value time.Duration
	}{
		{"maxerror", e.kernelState.MaxError},
		{"esterror", e.kernelState.EstError},
	} {
		violated := 0.0
		if excess > bound.value {
			violated = 1
		}
		gauge(ch, e.descs.kernelBoundViolation, violated, e.nodeName, server, bound.name)
	}
}

// collectConsensus emits the consensus and the per-server agreement
func (e *Exporter) collectConsensus(ch chan<- prometheus.Metric) {
	d := e.descs
	c := e.consensus

	gauge(ch, d.consensusOffset, c.Offset.Seconds())
	gauge(ch, d.consensusServers, float64(c.Servers))
	gauge(ch, d.consensusChimers, float64(c.Truechimers))

	for server, offset := range c.Offsets {
		gauge(ch, d.offsetFromConsensus, (offset - c.Offset).Seconds(), server)
		falseticker := 0.0
		if c.Falsetickers[server] {
			falseticker = 1
		}
		gauge(ch, d.falseticker, falseticker, server)
	}
}

// collectPool emits pool-level metrics
func (e *Exporter) collectPool(ch chan<- prometheus.Metric, pool probe.PoolResult) {
	d := e.descs

	gauge(ch, d.poolTotal, float64(pool.Total), pool.Name)
	gauge(ch, d.poolDNS, pool.DNSDuration.Seconds(), pool.Name)

	var offsets []float64
	for _, meas := range e.last.PoolMeasurements(pool.Name) {
		if best := meas.Best(); best != nil {
			offsets = append(offsets, best.Offset.Seconds())
		}
	}

	gauge(ch, d.poolActive, float64(len(offsets)), pool.Name)
	if len(offsets) > 0 {
		gauge(ch, d.poolOffset, mathutil.Median(offsets), pool.Name)
	}
}

// collectKernel emits kernel state metrics
func (e *Exporter) collectKernel(ch chan<- prometheus.Metric) {
	d := e.descs
	k := e.kernelState
	node := e.nodeName

	synchronized := 0.0
	if k.IsSynchronized() {
		synchronized = 1
	}
	gauge(ch, d.kernelSyncStatus, synchronized, node)

	if e.cfg.NTP.Kernel.ViolationOnly {
		return
	}

	gauge(ch, d.kernelState, 1, node, k.StateName())
	gauge(ch, d.kernelOffset, k.GetOffsetSeconds(), node)
	gauge(ch, d.kernelFrequency, k.GetFrequencyPPM(), node)
	gauge(ch, d.kernelMaxError, k.GetMaxErrorSeconds(), node)
	gauge(ch, d.kernelEstError, k.GetEstErrorSeconds(), node)
	gauge(ch, d.kernelTAI, float64(k.TAI), node)
}

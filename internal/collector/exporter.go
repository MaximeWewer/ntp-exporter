// Package collector exposes the measurements of the last probe cycle as
// Prometheus metrics.
//
// The probe package queries every target once per cycle. The Exporter keeps
// a snapshot of the last cycle and emits constant metrics from it at scrape
// time: a target that stops answering no longer exposes stale measurements,
// and a pool member that disappears from DNS no longer exposes any series.
package collector

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/logger"
	"github.com/maximewewer/ntp-exporter/internal/metrics"
	"github.com/maximewewer/ntp-exporter/internal/ntp"
	"github.com/maximewewer/ntp-exporter/internal/probe"
	"github.com/prometheus/client_golang/prometheus"
)

// KernelReader reads the kernel time state
type KernelReader interface {
	Read() (*ntp.KernelTimex, error)
}

// ChronyReader reads the tracking report of chronyd
type ChronyReader interface {
	Tracking(ctx context.Context) (*ntp.ChronyTracking, error)
}

// chronyState is the result of the last tracking request
type chronyState struct {
	tracking *ntp.ChronyTracking
	err      error
}

// Exporter is a prometheus.Collector built on the last probe cycle
type Exporter struct {
	cfg     *config.Config
	metrics *metrics.NTPMetrics
	descs   *descs

	// Kernel state, only read when kernel monitoring is enabled
	kernelReader KernelReader
	chronyReader ChronyReader

	mu          sync.RWMutex
	updatedAt   time.Time
	last        *probe.CycleResult
	consensus   *probe.Consensus
	kernelState *ntp.KernelTimex
	chrony      *chronyState
	lastSuccess map[string]time.Time
}

// NewExporter creates the exporter. Counters and exporter operational
// metrics remain owned by m.
func NewExporter(cfg *config.Config, m *metrics.NTPMetrics) *Exporter {
	e := &Exporter{
		cfg:         cfg,
		metrics:     m,
		descs:       newDescs(),
		lastSuccess: make(map[string]time.Time),
	}

	if cfg.KernelEnabled() {
		e.kernelReader = ntp.NewKernelReader(true)
	}
	if enabled, auto := cfg.ChronyEnabled(); enabled {
		// The address is validated with the configuration
		if client, err := ntp.NewChronyClient(cfg.NTP.Chrony.Address, cfg.NTP.Timeout); err == nil {
			e.chronyReader = client
			if auto && !chronyAnswers(cfg.NTP.Chrony.Address) {
				e.chronyReader = nil
			}
		}
	}

	return e
}

// chronyDetectTimeout bounds the detection of chronyd at startup
const chronyDetectTimeout = time.Second

// chronyAnswers tells whether chronyd answers a tracking request, to enable
// the chrony option by default in agent mode only where chronyd runs
func chronyAnswers(address string) bool {
	client, err := ntp.NewChronyClient(address, chronyDetectTimeout)
	if err == nil {
		_, err = client.Tracking(context.Background())
	}
	if err != nil {
		logger.SafeInfo("collector", "chronyd not found, chrony option disabled", map[string]interface{}{
			"address": address,
			"error":   err.Error(),
		})
		return false
	}
	logger.SafeInfo("collector", "chronyd found, chrony option enabled", map[string]interface{}{"address": address})
	return true
}

// SetChronyReader replaces the chronyd client
func (e *Exporter) SetChronyReader(r ChronyReader) {
	e.chronyReader = r
}

// NodeName returns the node name set by the DaemonSet, or the hostname: the
// node label of every metric in agent mode
func NodeName() string {
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

	var chrony *chronyState
	if e.chronyReader != nil {
		ctx, cancel := context.WithTimeout(context.Background(), e.cfg.NTP.Timeout)
		tracking, err := e.chronyReader.Tracking(ctx)
		cancel()
		if err != nil {
			logger.SafeWarn("collector", "Failed to read chronyd tracking report", map[string]interface{}{
				"address": e.cfg.NTP.Chrony.Address,
				"error":   err.Error(),
			})
		}
		chrony = &chronyState{tracking: tracking, err: err}
	}

	e.updateCounters(result)

	var consensus *probe.Consensus
	if e.cfg.ConsensusEnabled() {
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
	e.chrony = chrony
}

// Ready returns nil when a cycle has completed recently (within 3 intervals)
func (e *Exporter) Ready() error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.last == nil {
		return errors.New("no collection cycle completed yet")
	}
	if age := time.Since(e.updatedAt); age > 3*e.cfg.NTP.CollectionInterval {
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
		// Created at 0 so that increase() sees the first unsynchronized answer
		m.InvalidResponsesTotal.WithLabelValues(server, ntp.ReasonUnsync)
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

	m.ExporterCycleDuration.Observe(result.Duration.Seconds())
	if result.Overrun {
		m.ExporterCycleOverrunTotal.Inc()
	}
	if successCount > 0 {
		m.ExporterCyclesTotal.WithLabelValues("success").Inc()
	} else {
		m.ExporterCyclesTotal.WithLabelValues("failure").Inc()
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

	if e.chrony != nil {
		e.collectChrony(ch)
	}
}

// collectChrony emits the chronyd tracking report
func (e *Exporter) collectChrony(ch chan<- prometheus.Metric) {
	d := e.descs
	t := e.chrony.tracking

	if e.chrony.err != nil || t == nil {
		gauge(ch, d.chronyUp, 0)
		return
	}
	gauge(ch, d.chronyUp, 1)

	gauge(ch, d.chronyInfo, 1, t.RefIDHex(), t.Source)
	gauge(ch, d.chronyStratum, float64(t.Stratum))
	gauge(ch, d.chronyLeapStatus, float64(t.LeapStatus))
	if !t.RefTime.IsZero() {
		gauge(ch, d.chronyRefTime, float64(t.RefTime.UnixNano())/1e9)
	}
	gauge(ch, d.chronyCorrection, t.CurrentCorrection)
	gauge(ch, d.chronyRootDispersion, t.RootDispersion)
}

func gauge(ch chan<- prometheus.Metric, desc *prometheus.Desc, value float64, labels ...string) {
	ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, value, labels...)
}

// collectMeasurement emits the metrics of one target
func (e *Exporter) collectMeasurement(ch chan<- prometheus.Metric, meas *probe.Measurement) {
	server := meas.Target.Server
	if ts, ok := e.lastSuccess[server]; ok {
		gauge(ch, e.descs.lastSuccess, float64(ts.UnixNano())/1e9, server)
	}

	writeMeasurement(ch, e.descs, meas)
}

// writeMeasurement emits the metrics of one measurement and returns its
// reference sample, or nil when no valid time measurement was received
func writeMeasurement(ch chan<- prometheus.Metric, d *descs, meas *probe.Measurement) *ntp.Response {
	server := meas.Target.Server

	if meas.NTS != nil {
		success := 0.0
		if meas.NTS.KESuccess {
			success = 1
		}
		gauge(ch, d.ntsKESuccess, success, server)
		if !meas.NTS.CertExpiry.IsZero() {
			gauge(ch, d.ntsCertExpiry, float64(meas.NTS.CertExpiry.Unix()), server)
		}
	}

	if meas.SuspendCode != "" {
		gauge(ch, d.up, 0, server)
		gauge(ch, d.suspended, 1, server, meas.SuspendCode)
		return nil
	}

	up := 0.0
	if meas.Reachable() {
		up = 1
	}
	gauge(ch, d.up, up, server)
	if meas.BackoffFactor > 0 {
		gauge(ch, d.backoff, float64(meas.BackoffFactor), server)
	}

	// Not queried (RATE backoff) and no previous measurement: nothing was
	// sent, so nothing was lost
	if meas.Skipped && !meas.Reachable() {
		return nil
	}

	stats := meas.Stats()
	gauge(ch, d.packetLoss, lossRatio(meas), server)
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
		return nil
	}

	gauge(ch, d.info, 1, server, meas.Target.Address, meas.Target.Pool, best.ReferenceIDString(),
		strconv.Itoa(best.Version))

	gauge(ch, d.offset, best.Offset.Seconds(), server)
	gauge(ch, d.rtt, best.RTT.Seconds(), server)
	gauge(ch, d.stratum, float64(best.Stratum), server)
	gauge(ch, d.referenceAge, best.Time.Sub(best.ReferenceTime).Seconds(), server)
	gauge(ch, d.rootDelay, best.RootDelay.Seconds(), server)
	gauge(ch, d.rootDispersion, best.RootDispersion.Seconds(), server)
	gauge(ch, d.rootDistance, best.RootDistance.Seconds(), server)
	gauge(ch, d.precision, best.Precision.Seconds(), server)
	gauge(ch, d.leapIndicator, float64(best.LeapIndicator), server)

	// Jitter needs two samples: a single one would read as perfect stability
	if stats.SamplesCount >= 2 {
		gauge(ch, d.jitter, stats.Jitter.Seconds(), server)
	}

	return best
}

// lossRatio returns the share of the queries sent that got no answer at all;
// invalid answers are counted in ntp_invalid_responses_total
func lossRatio(meas *probe.Measurement) float64 {
	if meas.Sent == 0 {
		return 1
	}
	lost := meas.Sent - len(meas.Responses)
	return float64(max(lost, 0)) / float64(meas.Sent)
}

// referenceSample returns the valid sample with the lowest root distance
// among the reference servers of the agent: the most precise outside view of
// the host clock
func (e *Exporter) referenceSample() *ntp.Response {
	if e.last == nil {
		return nil
	}
	var ref *ntp.Response
	for _, meas := range e.last.Measurements {
		if meas.SuspendCode != "" {
			continue
		}
		if best := meas.Best(); best != nil && (ref == nil || best.RootDistance < ref.RootDistance) {
			ref = best
		}
	}
	return ref
}

// collectBoundViolation compares the offset measured against the reference
// server with the maximum error the kernel announces. The root distance of
// the server (which includes half the round-trip delay) is the measurement
// uncertainty: a violation means the time daemon underestimates its error.
func (e *Exporter) collectBoundViolation(ch chan<- prometheus.Metric) {
	ref := e.referenceSample()
	if ref == nil || !e.kernelState.IsSynchronized() {
		return
	}
	violated := 0.0
	if ref.Offset.Abs()-ref.RootDistance > e.kernelState.MaxError {
		violated = 1
	}
	gauge(ch, e.descs.kernelBoundViolation, violated)
}

// collectConsensus emits the consensus and the per-server agreement
func (e *Exporter) collectConsensus(ch chan<- prometheus.Metric) {
	d := e.descs
	c := e.consensus

	gauge(ch, d.consensusServers, float64(c.Servers))
	gauge(ch, d.consensusChimers, float64(c.Truechimers))
	if !c.Majority {
		// Without majority there is no reference: nothing to compare with
		gauge(ch, d.consensusMajority, 0)
		return
	}
	gauge(ch, d.consensusMajority, 1)
	gauge(ch, d.consensusOffset, c.Offset.Seconds())

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

	active := 0
	for _, meas := range e.last.PoolMeasurements(pool.Name) {
		if meas.Best() != nil {
			active++
		}
	}
	gauge(ch, d.poolActive, float64(active), pool.Name)
}

// collectKernel emits kernel state metrics
func (e *Exporter) collectKernel(ch chan<- prometheus.Metric) {
	d := e.descs
	k := e.kernelState

	synchronized := 0.0
	if k.IsSynchronized() {
		synchronized = 1
	}
	gauge(ch, d.kernelSyncStatus, synchronized)
	e.collectBoundViolation(ch)
}

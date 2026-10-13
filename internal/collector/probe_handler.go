package collector

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/ntp"
	"github.com/maximewewer/ntp-exporter/internal/probe"
	"github.com/maximewewer/ntp-exporter/pkg/logger"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// defaultProbeTimeout is used when Prometheus does not announce its scrape timeout
	defaultProbeTimeout = 10 * time.Second
	// probeTimeoutOffset leaves time to encode and send the response
	probeTimeoutOffset = 500 * time.Millisecond
	// maxProbeSamples bounds the samples parameter, like samples_per_server
	maxProbeSamples = 20
)

// ProbeHandler serves /probe?target=host[:port][&samples=N]: one burst against
// the target, within the scrape timeout announced by Prometheus. Like
// blackbox_exporter, Prometheus drives the targets through relabel_configs,
// so that probe replicas hold no target list.
type ProbeHandler struct {
	cfg    *config.Config
	prober *probe.Prober
	descs  *descs
	// slots bounds the number of bursts in flight
	slots chan struct{}
}

// NewProbeHandler creates the /probe handler
func NewProbeHandler(cfg *config.Config, prober *probe.Prober) *ProbeHandler {
	return &ProbeHandler{
		cfg:    cfg,
		prober: prober,
		descs:  newDescs(cfg.Metrics.Namespace, cfg.Metrics.Subsystem),
		slots:  make(chan struct{}, cfg.NTP.MaxConcurrency),
	}
}

// ServeHTTP implements http.Handler
func (h *ProbeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()

	target := params.Get("target")
	if err := validateTarget(target); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	samples := h.cfg.NTP.SamplesPerServer
	if s := params.Get("samples"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > maxProbeSamples {
			http.Error(w, "samples must be between 1 and "+strconv.Itoa(maxProbeSamples), http.StatusBadRequest)
			return
		}
		samples = n
	}

	timeout := probeTimeout(r, h.cfg.Server.WriteTimeout)
	samples = fitSamples(samples, timeout, h.cfg.NTP.Timeout, h.cfg.NTP.SampleSpacing)

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	case <-ctx.Done():
		http.Error(w, "too many probes in flight", http.StatusServiceUnavailable)
		return
	}

	start := time.Now()
	meas := h.prober.Probe(ctx, target, samples)
	duration := time.Since(start)

	logger.SafeDebug("probe", "Probe request served", map[string]interface{}{
		"target":   target,
		"address":  meas.Target.Address,
		"samples":  samples,
		"success":  meas.Best() != nil,
		"duration": duration.Seconds(),
	})

	registry := prometheus.NewRegistry()
	registerer := prometheus.WrapRegistererWith(h.cfg.Metrics.Labels, registry)
	registerer.MustRegister(&targetCollector{cfg: h.cfg, descs: h.descs, meas: meas, duration: duration})
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(w, r)
}

// validateTarget accepts a host name or an IP address, with an optional port
func validateTarget(target string) error {
	if target == "" {
		return errors.New("target parameter is missing")
	}
	target = strings.TrimPrefix(target, ntp.NTSScheme)
	if strings.ContainsAny(target, "/ ") {
		return errors.New("target must be host, host:port or nts://host[:port]")
	}

	host, port, err := net.SplitHostPort(target)
	if err != nil {
		host, port = target, ""
	}
	if host == "" || len(host) > 253 {
		return errors.New("invalid target host")
	}
	if port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return errors.New("invalid target port")
		}
	}
	return nil
}

// probeTimeout returns the time budget of a request: the scrape timeout
// announced by Prometheus minus a small offset, capped by the write timeout of
// the server (a later response would be lost, and a forged header would hold
// a probe slot)
func probeTimeout(r *http.Request, writeTimeout time.Duration) time.Duration {
	timeout := defaultProbeTimeout
	if v := r.Header.Get("X-Prometheus-Scrape-Timeout-Seconds"); v != "" {
		if s, err := strconv.ParseFloat(v, 64); err == nil && s > 0 {
			timeout = time.Duration(s * float64(time.Second))
		}
	}
	if writeTimeout > 0 {
		timeout = min(timeout, writeTimeout)
	}
	if timeout > 2*probeTimeoutOffset {
		timeout -= probeTimeoutOffset
	}
	return timeout
}

// fitSamples lowers the number of samples so that the burst fits in the
// time budget: (samples - 1) spacings plus one query timeout
func fitSamples(samples int, budget, queryTimeout, spacing time.Duration) int {
	if spacing <= 0 || samples <= 1 {
		return samples
	}
	fit := 1 + int((budget-queryTimeout)/spacing)
	if fit < 1 {
		fit = 1
	}
	return min(samples, fit)
}

// targetCollector exposes the measurement of a single /probe request
type targetCollector struct {
	cfg      *config.Config
	descs    *descs
	meas     *probe.Measurement
	duration time.Duration
}

// Describe implements prometheus.Collector
func (c *targetCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range c.descs.all() {
		ch <- d
	}
	ch <- c.descs.probeSuccess
	ch <- c.descs.probeDuration
}

// Collect implements prometheus.Collector
func (c *targetCollector) Collect(ch chan<- prometheus.Metric) {
	success := 0.0
	if writeMeasurement(ch, c.descs, c.cfg, c.meas) != nil {
		success = 1
	}
	gauge(ch, c.descs.probeSuccess, success)
	gauge(ch, c.descs.probeDuration, c.duration.Seconds())
}

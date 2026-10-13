package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// NTPMetrics holds the cumulative counters and the exporter operational
// metrics. Measurement metrics (offset, stratum, quality, kernel...) are
// emitted at scrape time from the last probe cycle by the collector package.
type NTPMetrics struct {
	// Response counters
	KissOfDeathTotal      *prometheus.CounterVec
	InvalidResponsesTotal *prometheus.CounterVec

	// Exporter operational metrics
	ExporterBuildInfo         *prometheus.GaugeVec
	ExporterCycleDuration     prometheus.Histogram
	ExporterCyclesTotal       *prometheus.CounterVec
	ExporterServersConfigured prometheus.Gauge
	ExporterCycleOverrunTotal prometheus.Counter
}

// Namespace prefixes every metric of the exporter
const Namespace = "ntp"

// NewNTPMetrics creates the counters and operational metrics of the exporter
func NewNTPMetrics() *NTPMetrics {
	namespace := Namespace
	return &NTPMetrics{
		KissOfDeathTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "kiss_of_death_total",
				Help:      "Total number of Kiss-of-Death packets received",
			},
			[]string{"server", "code"},
		),
		InvalidResponsesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "invalid_responses_total",
				Help:      "Total number of queries without a valid time measurement, by reason (kod, unsync, stratum, root_distance, stale_reference, invalid_time, other, timeout)",
			},
			[]string{"server", "reason"},
		),

		ExporterBuildInfo: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "exporter",
				Name:      "build_info",
				Help:      "Build information for the exporter",
			},
			[]string{"version", "commit", "go_version"},
		),
		ExporterCycleDuration: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: "exporter",
				Name:      "cycle_duration_seconds",
				Help:      "Duration of the NTP collection cycles in seconds",
				Buckets:   []float64{0.01, 0.05, 0.1, 0.5, 1.0, 2.0, 5.0, 10.0, 30.0},
			},
		),
		ExporterCyclesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "exporter",
				Name:      "cycles_total",
				Help:      "Total number of collection cycles, with at least one answering target (success) or none (failure)",
			},
			[]string{"status"},
		),
		ExporterServersConfigured: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "exporter",
				Name:      "servers_configured",
				Help:      "Number of NTP servers and pools configured",
			},
		),
		ExporterCycleOverrunTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "exporter",
				Name:      "cycle_overrun_total",
				Help:      "Total number of collection cycles cut at their deadline before every target answered",
			},
		),
	}
}

// getAllMetrics returns all metric collectors
func (m *NTPMetrics) getAllMetrics() []prometheus.Collector {
	return []prometheus.Collector{
		m.KissOfDeathTotal,
		m.InvalidResponsesTotal,

		m.ExporterBuildInfo,
		m.ExporterCycleDuration,
		m.ExporterCyclesTotal,
		m.ExporterServersConfigured,
		m.ExporterCycleOverrunTotal,
	}
}

// Describe implements prometheus.Collector interface
func (m *NTPMetrics) Describe(ch chan<- *prometheus.Desc) {
	for _, metric := range m.getAllMetrics() {
		metric.Describe(ch)
	}
}

// Collect implements prometheus.Collector interface
func (m *NTPMetrics) Collect(ch chan<- prometheus.Metric) {
	for _, metric := range m.getAllMetrics() {
		metric.Collect(ch)
	}
}

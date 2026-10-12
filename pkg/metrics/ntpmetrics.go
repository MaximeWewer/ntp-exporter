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
	ExporterScrapeDuration    prometheus.Histogram
	ExporterScrapesTotal      *prometheus.CounterVec
	ExporterServersConfigured prometheus.Gauge
	ExporterCycleDuration     prometheus.Gauge
	ExporterCycleOverrunTotal prometheus.Counter
	ExporterMemoryUsageBytes  prometheus.Gauge
	ExporterGoroutinesCount   prometheus.Gauge

	// Advanced memory and GC metrics
	GCDurationSeconds    prometheus.Summary
	MemoryAllocatedBytes prometheus.Gauge
	MemoryHeapBytes      prometheus.Gauge
	MemoryStackBytes     prometheus.Gauge
}

// NewNTPMetricsWithConfig creates and initializes the exporter metrics with custom namespace and subsystem
func NewNTPMetricsWithConfig(namespace, subsystem string) *NTPMetrics {
	return &NTPMetrics{
		KissOfDeathTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "kiss_of_death_total",
				Help:      "Total number of Kiss-of-Death packets received",
			},
			[]string{"server", "code"},
		),
		InvalidResponsesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
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
		ExporterScrapeDuration: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: "exporter",
				Name:      "scrape_duration_seconds",
				Help:      "Duration of NTP collection cycles in seconds",
				Buckets:   []float64{0.01, 0.05, 0.1, 0.5, 1.0, 2.0, 5.0, 10.0, 30.0},
			},
		),
		ExporterScrapesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "exporter",
				Name:      "scrapes_total",
				Help:      "Total number of collection cycles",
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
		ExporterCycleDuration: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "exporter",
				Name:      "cycle_duration_seconds",
				Help:      "Duration of the last collection cycle in seconds",
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
		ExporterMemoryUsageBytes: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "exporter",
				Name:      "memory_usage_bytes",
				Help:      "Memory usage of the exporter in bytes",
			},
		),
		ExporterGoroutinesCount: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "exporter",
				Name:      "goroutines_count",
				Help:      "Number of active goroutines",
			},
		),

		GCDurationSeconds: prometheus.NewSummary(
			prometheus.SummaryOpts{
				Namespace:  namespace,
				Subsystem:  "exporter",
				Name:       "gc_duration_seconds",
				Help:       "Garbage collection duration in seconds",
				Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
			},
		),
		MemoryAllocatedBytes: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "exporter",
				Name:      "memory_allocated_bytes",
				Help:      "Memory allocated by Go runtime in bytes",
			},
		),
		MemoryHeapBytes: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "exporter",
				Name:      "memory_heap_bytes",
				Help:      "Heap memory in use in bytes",
			},
		),
		MemoryStackBytes: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "exporter",
				Name:      "memory_stack_bytes",
				Help:      "Stack memory in use in bytes",
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
		m.ExporterScrapeDuration,
		m.ExporterScrapesTotal,
		m.ExporterServersConfigured,
		m.ExporterCycleDuration,
		m.ExporterCycleOverrunTotal,
		m.ExporterMemoryUsageBytes,
		m.ExporterGoroutinesCount,

		m.GCDurationSeconds,
		m.MemoryAllocatedBytes,
		m.MemoryHeapBytes,
		m.MemoryStackBytes,
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

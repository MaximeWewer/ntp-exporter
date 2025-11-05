package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// NTPMetrics encapsulates all NTP exporter metrics
type NTPMetrics struct {
	// Base NTP Metrics
	OffsetSeconds       *prometheus.GaugeVec
	ClockOffsetExceeded *prometheus.GaugeVec // 1 if offset exceeds max threshold, 0 otherwise
	RTTSeconds          *prometheus.GaugeVec
	ServerReachable     *prometheus.GaugeVec
	Stratum             *prometheus.GaugeVec
	ReferenceTimestamp  *prometheus.GaugeVec
	RootDelay           *prometheus.GaugeVec
	RootDispersion      *prometheus.GaugeVec
	RootDistance        *prometheus.GaugeVec
	Precision           *prometheus.GaugeVec
	LeapIndicator       *prometheus.GaugeVec

	// Quality Metrics
	JitterSeconds    *prometheus.GaugeVec
	StabilitySeconds *prometheus.GaugeVec
	AsymmetrySeconds *prometheus.GaugeVec
	SamplesCount     *prometheus.GaugeVec
	PacketLossRatio  *prometheus.GaugeVec

	// Security Metrics
	ServerSuspiciousTotal   *prometheus.CounterVec
	KissOfDeathTotal        *prometheus.CounterVec
	MalformedResponsesTotal *prometheus.CounterVec
	ServerTrustScore        *prometheus.GaugeVec

	// Pool Metrics
	PoolServersActive        *prometheus.GaugeVec
	PoolServersTotal         *prometheus.GaugeVec
	PoolDNSResolutionSeconds *prometheus.GaugeVec
	PoolBestOffsetSeconds    *prometheus.GaugeVec

	// Exporter Operational Metrics
	ExporterBuildInfo             *prometheus.GaugeVec
	ExporterScrapeDuration        prometheus.Histogram
	ExporterScrapesTotal          *prometheus.CounterVec
	ExporterServersConfigured     prometheus.Gauge
	ExporterDNSResolutionDuration *prometheus.HistogramVec
	ExporterMemoryUsageBytes      prometheus.Gauge
	ExporterGoroutinesCount       prometheus.Gauge

	// Performance Metrics
	QueryDurationSeconds     *prometheus.HistogramVec
	CollectorDurationSeconds *prometheus.HistogramVec

	// Advanced Memory and GC Metrics
	GCDurationSeconds    prometheus.Summary
	MemoryAllocatedBytes prometheus.Gauge
	MemoryHeapBytes      prometheus.Gauge
	MemoryStackBytes     prometheus.Gauge
	GCCountTotal         prometheus.Counter

	// Kernel NTP State Metrics (Linux only, Agent mode)
	KernelOffsetSeconds    *prometheus.GaugeVec
	KernelFrequencyPPM     *prometheus.GaugeVec
	KernelMaxErrorSeconds  *prometheus.GaugeVec
	KernelEstErrorSeconds  *prometheus.GaugeVec
	KernelPrecisionSeconds *prometheus.GaugeVec
	KernelSyncStatus       *prometheus.GaugeVec
	KernelStatusCode       *prometheus.GaugeVec

	// Hybrid Mode Metrics - Correlation between NTP and Kernel
	NTPKernelDivergence *prometheus.GaugeVec
	NTPKernelCoherence  *prometheus.GaugeVec
}

// NewNTPMetricsWithConfig creates and initializes all NTP exporter metrics with custom namespace and subsystem
func NewNTPMetricsWithConfig(namespace, subsystem string) *NTPMetrics {
	return &NTPMetrics{
		// Base NTP Metrics
		OffsetSeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "offset_seconds",
				Help:      "Time offset between local clock and NTP server in seconds",
			},
			[]string{"server", "stratum", "version"},
		),
		ClockOffsetExceeded: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "clock_offset_exceeded",
				Help:      "Whether the clock offset exceeds the configured threshold (1 = exceeded, 0 = within limits)",
			},
			[]string{"server"},
		),
		RTTSeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "rtt_seconds",
				Help:      "Round-trip time to NTP server in seconds",
			},
			[]string{"server"},
		),
		ServerReachable: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "server_reachable",
				Help:      "Whether the NTP server is reachable (1) or not (0)",
			},
			[]string{"server"},
		),
		Stratum: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "stratum",
				Help:      "NTP server stratum level (0-16)",
			},
			[]string{"server"},
		),
		ReferenceTimestamp: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "reference_timestamp_seconds",
				Help:      "Reference timestamp of the NTP server in Unix seconds",
			},
			[]string{"server"},
		),
		RootDelay: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "root_delay_seconds",
				Help:      "Root delay of the NTP server in seconds",
			},
			[]string{"server"},
		),
		RootDispersion: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "root_dispersion_seconds",
				Help:      "Root dispersion of the NTP server in seconds",
			},
			[]string{"server"},
		),
		RootDistance: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "root_distance_seconds",
				Help:      "Calculated root distance in seconds",
			},
			[]string{"server"},
		),
		Precision: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "precision_seconds",
				Help:      "Precision of the NTP server in seconds",
			},
			[]string{"server"},
		),
		LeapIndicator: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "leap_indicator",
				Help:      "Leap second indicator (0=no warning, 1=61s, 2=59s, 3=unsync)",
			},
			[]string{"server"},
		),

		// Quality Metrics
		JitterSeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "jitter_seconds",
				Help:      "Jitter calculated from multiple samples in seconds",
			},
			[]string{"server"},
		),
		StabilitySeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "stability_seconds",
				Help:      "Stability of time offset (standard deviation) in seconds",
			},
			[]string{"server"},
		),
		AsymmetrySeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "asymmetry_seconds",
				Help:      "Network asymmetry in seconds",
			},
			[]string{"server"},
		),
		SamplesCount: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "samples_count",
				Help:      "Number of samples used for calculation",
			},
			[]string{"server"},
		),
		PacketLossRatio: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "packet_loss_ratio",
				Help:      "Packet loss ratio during measurements (0-1)",
			},
			[]string{"server"},
		),

		// Security Metrics
		ServerSuspiciousTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "server_suspicious_total",
				Help:      "Total number of suspicious server detections",
			},
			[]string{"server", "reason"},
		),
		KissOfDeathTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "kiss_of_death_total",
				Help:      "Total number of Kiss-of-Death packets received",
			},
			[]string{"server", "code"},
		),
		MalformedResponsesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "malformed_responses_total",
				Help:      "Total number of malformed NTP responses",
			},
			[]string{"server"},
		),
		ServerTrustScore: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "server_trust_score",
				Help:      "Trust score for the NTP server (0-1)",
			},
			[]string{"server"},
		),

		// Pool Metrics
		PoolServersActive: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "pool",
				Name:      "servers_active",
				Help:      "Number of active servers in the pool",
			},
			[]string{"pool"},
		),
		PoolServersTotal: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "pool",
				Name:      "servers_total",
				Help:      "Total number of servers in the pool",
			},
			[]string{"pool"},
		),
		PoolDNSResolutionSeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "pool",
				Name:      "dns_resolution_seconds",
				Help:      "DNS resolution duration for pool in seconds",
			},
			[]string{"pool"},
		),
		PoolBestOffsetSeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "pool",
				Name:      "best_offset_seconds",
				Help:      "Best offset from pool servers in seconds",
			},
			[]string{"pool"},
		),

		// Exporter Operational Metrics
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
				Help:      "Duration of NTP scrape in seconds",
				Buckets:   []float64{0.01, 0.05, 0.1, 0.5, 1.0, 2.0, 5.0},
			},
		),
		ExporterScrapesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "exporter",
				Name:      "scrapes_total",
				Help:      "Total number of scrapes",
			},
			[]string{"status"},
		),
		ExporterServersConfigured: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "exporter",
				Name:      "servers_configured",
				Help:      "Number of NTP servers configured",
			},
		),
		ExporterDNSResolutionDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: "exporter",
				Name:      "dns_resolution_duration_seconds",
				Help:      "DNS resolution duration in seconds",
				Buckets:   []float64{0.001, 0.01, 0.05, 0.1, 0.5, 1.0},
			},
			[]string{"server"},
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

		// Performance Metrics
		QueryDurationSeconds: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "query_duration_seconds",
				Help:      "NTP query duration distribution in seconds",
				Buckets:   prometheus.ExponentialBuckets(0.001, 2, 10), // 1ms to ~1s
			},
			[]string{"server", "status"},
		),
		CollectorDurationSeconds: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: "exporter",
				Name:      "collector_duration_seconds",
				Help:      "Collector execution duration in seconds",
				Buckets:   []float64{0.1, 0.5, 1.0, 2.0, 5.0, 10.0, 30.0},
			},
			[]string{"collector"},
		),

		// Advanced Memory and GC Metrics
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
		GCCountTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "exporter",
				Name:      "gc_count_total",
				Help:      "Total number of garbage collections",
			},
		),

		// Kernel NTP State Metrics (Linux only, Agent mode)
		KernelOffsetSeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "kernel_offset_seconds",
				Help:      "Kernel time offset in seconds (from adjtimex syscall)",
			},
			[]string{"node"},
		),
		KernelFrequencyPPM: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "kernel_frequency_ppm",
				Help:      "Kernel frequency adjustment in PPM",
			},
			[]string{"node"},
		),
		KernelMaxErrorSeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "kernel_max_error_seconds",
				Help:      "Kernel maximum time error in seconds",
			},
			[]string{"node"},
		),
		KernelEstErrorSeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "kernel_est_error_seconds",
				Help:      "Kernel estimated time error in seconds",
			},
			[]string{"node"},
		),
		KernelPrecisionSeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "kernel_precision_seconds",
				Help:      "Kernel clock precision in seconds",
			},
			[]string{"node"},
		),
		KernelSyncStatus: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "kernel_sync_status",
				Help:      "Kernel synchronization status (1=synchronized, 0=unsynchronized)",
			},
			[]string{"node", "status"},
		),
		KernelStatusCode: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "kernel_status_code",
				Help:      "Kernel NTP status code",
			},
			[]string{"node"},
		),

		// Hybrid Mode Metrics - Correlation between NTP and Kernel
		NTPKernelDivergence: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "kernel_divergence_seconds",
				Help:      "Absolute difference between NTP offset and kernel offset in seconds",
			},
			[]string{"node", "server"},
		),
		NTPKernelCoherence: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "kernel_coherence_score",
				Help:      "Coherence score between NTP and kernel measurements (0-1, higher is better)",
			},
			[]string{"node", "server"},
		),
	}
}

// NewNTPMetrics creates and initializes all NTP exporter metrics with default namespace and subsystem
func NewNTPMetrics() *NTPMetrics {
	return NewNTPMetricsWithConfig("ntp", "")
}

// getAllMetrics returns all metric collectors
func (m *NTPMetrics) getAllMetrics() []prometheus.Collector {
	return []prometheus.Collector{
		// Base metrics
		m.OffsetSeconds,
		m.ClockOffsetExceeded,
		m.RTTSeconds,
		m.ServerReachable,
		m.Stratum,
		m.ReferenceTimestamp,
		m.RootDelay,
		m.RootDispersion,
		m.RootDistance,
		m.Precision,
		m.LeapIndicator,

		// Quality metrics
		m.JitterSeconds,
		m.StabilitySeconds,
		m.AsymmetrySeconds,
		m.SamplesCount,
		m.PacketLossRatio,

		// Security metrics
		m.ServerSuspiciousTotal,
		m.KissOfDeathTotal,
		m.MalformedResponsesTotal,
		m.ServerTrustScore,

		// Pool metrics
		m.PoolServersActive,
		m.PoolServersTotal,
		m.PoolDNSResolutionSeconds,
		m.PoolBestOffsetSeconds,

		// Exporter operational metrics
		m.ExporterBuildInfo,
		m.ExporterScrapeDuration,
		m.ExporterScrapesTotal,
		m.ExporterServersConfigured,
		m.ExporterDNSResolutionDuration,
		m.ExporterMemoryUsageBytes,
		m.ExporterGoroutinesCount,
		m.QueryDurationSeconds,
		m.CollectorDurationSeconds,
		m.GCDurationSeconds,
		m.MemoryAllocatedBytes,
		m.MemoryHeapBytes,
		m.MemoryStackBytes,
		m.GCCountTotal,

		// Kernel metrics
		m.KernelOffsetSeconds,
		m.KernelFrequencyPPM,
		m.KernelMaxErrorSeconds,
		m.KernelEstErrorSeconds,
		m.KernelPrecisionSeconds,
		m.KernelSyncStatus,
		m.KernelStatusCode,

		// Hybrid metrics
		m.NTPKernelDivergence,
		m.NTPKernelCoherence,
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

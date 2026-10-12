package collector

import (
	"github.com/prometheus/client_golang/prometheus"
)

// descs holds the descriptors of every metric derived from measurements
type descs struct {
	// Target health
	up          *prometheus.Desc
	lastSuccess *prometheus.Desc
	suspended   *prometheus.Desc

	// Base NTP metrics
	offset         *prometheus.Desc
	offsetMedian   *prometheus.Desc
	offsetExceeded *prometheus.Desc
	rtt            *prometheus.Desc
	stratum        *prometheus.Desc
	referenceTime  *prometheus.Desc
	rootDelay      *prometheus.Desc
	rootDispersion *prometheus.Desc
	rootDistance   *prometheus.Desc
	precision      *prometheus.Desc
	leapIndicator  *prometheus.Desc

	// Quality metrics
	jitter     *prometheus.Desc
	stability  *prometheus.Desc
	asymmetry  *prometheus.Desc
	samples    *prometheus.Desc
	packetLoss *prometheus.Desc

	// Security metrics
	trustScore *prometheus.Desc

	// Pool metrics
	poolActive     *prometheus.Desc
	poolTotal      *prometheus.Desc
	poolDNS        *prometheus.Desc
	poolBestOffset *prometheus.Desc

	// Kernel metrics
	kernelOffset     *prometheus.Desc
	kernelFrequency  *prometheus.Desc
	kernelMaxError   *prometheus.Desc
	kernelEstError   *prometheus.Desc
	kernelPrecision  *prometheus.Desc
	kernelSyncStatus *prometheus.Desc
	kernelStatusCode *prometheus.Desc
	kernelDivergence *prometheus.Desc
	kernelCoherence  *prometheus.Desc
}

func newDescs(namespace, subsystem string) *descs {
	d := func(sub, name, help string, labels ...string) *prometheus.Desc {
		return prometheus.NewDesc(prometheus.BuildFQName(namespace, sub, name), help, labels, nil)
	}
	server := []string{"server"}
	pool := []string{"pool"}
	node := []string{"node"}

	return &descs{
		up:          d(subsystem, "up", "Whether the NTP server answered during the last cycle (1) or not (0)", server...),
		lastSuccess: d(subsystem, "last_success_timestamp_seconds", "Unix time of the last cycle with a valid response from the server", server...),
		suspended:   d(subsystem, "target_suspended", "Whether the server is no longer queried after a DENY or RSTR Kiss-o'-Death (1) or not (0)", "server", "code"),

		offset:         d(subsystem, "offset_seconds", "Time offset between local clock and NTP server in seconds, from the sample with the lowest delay (RFC 5905 clock filter)", server...),
		offsetMedian:   d(subsystem, "offset_median_seconds", "Median time offset of the valid samples of the last burst in seconds", server...),
		offsetExceeded: d(subsystem, "clock_offset_exceeded", "Whether the clock offset exceeds the configured threshold (1 = exceeded, 0 = within limits)", server...),
		rtt:            d(subsystem, "rtt_seconds", "Round-trip time to NTP server in seconds", server...),
		stratum:        d(subsystem, "stratum", "NTP server stratum level (1-15)", server...),
		referenceTime:  d(subsystem, "reference_timestamp_seconds", "Reference timestamp of the NTP server in Unix seconds", server...),
		rootDelay:      d(subsystem, "root_delay_seconds", "Root delay of the NTP server in seconds", server...),
		rootDispersion: d(subsystem, "root_dispersion_seconds", "Root dispersion of the NTP server in seconds", server...),
		rootDistance:   d(subsystem, "root_distance_seconds", "Calculated root distance in seconds", server...),
		precision:      d(subsystem, "precision_seconds", "Precision of the NTP server in seconds", server...),
		leapIndicator:  d(subsystem, "leap_indicator", "Leap second indicator (0=no warning, 1=61s, 2=59s, 3=unsync)", server...),

		jitter:     d(subsystem, "jitter_seconds", "Jitter calculated from multiple samples in seconds", server...),
		stability:  d(subsystem, "stability_seconds", "Stability of time offset (standard deviation) in seconds", server...),
		asymmetry:  d(subsystem, "asymmetry_seconds", "Network asymmetry in seconds", server...),
		samples:    d(subsystem, "samples_count", "Number of valid samples used for calculation", server...),
		packetLoss: d(subsystem, "packet_loss_ratio", "Ratio of queries without valid response during the last cycle (0-1)", server...),

		trustScore: d(subsystem, "server_trust_score", "Trust score for the NTP server (0-1)", server...),

		poolActive:     d("pool", "servers_active", "Number of pool servers with a valid response", pool...),
		poolTotal:      d("pool", "servers_total", "Total number of servers returned by the pool resolution", pool...),
		poolDNS:        d("pool", "dns_resolution_seconds", "DNS resolution duration for pool in seconds", pool...),
		poolBestOffset: d("pool", "best_offset_seconds", "Best offset from pool servers in seconds", pool...),

		kernelOffset:     d(subsystem, "kernel_offset_seconds", "Kernel time offset in seconds (from adjtimex syscall)", node...),
		kernelFrequency:  d(subsystem, "kernel_frequency_ppm", "Kernel frequency adjustment in PPM", node...),
		kernelMaxError:   d(subsystem, "kernel_max_error_seconds", "Kernel maximum time error in seconds", node...),
		kernelEstError:   d(subsystem, "kernel_est_error_seconds", "Kernel estimated time error in seconds", node...),
		kernelPrecision:  d(subsystem, "kernel_precision_seconds", "Kernel clock precision in seconds", node...),
		kernelSyncStatus: d(subsystem, "kernel_sync_status", "Kernel synchronization status (1=synchronized, 0=unsynchronized)", "node", "status"),
		kernelStatusCode: d(subsystem, "kernel_status_code", "Kernel NTP status code", node...),
		kernelDivergence: d(subsystem, "kernel_divergence_seconds", "Absolute difference between NTP offset and kernel offset in seconds", "node", "server"),
		kernelCoherence:  d(subsystem, "kernel_coherence_score", "Coherence score between NTP and kernel measurements (0-1, higher is better)", "node", "server"),
	}
}

// all returns every descriptor
func (d *descs) all() []*prometheus.Desc {
	return []*prometheus.Desc{
		d.up, d.lastSuccess, d.suspended,
		d.offset, d.offsetMedian, d.offsetExceeded, d.rtt, d.stratum, d.referenceTime,
		d.rootDelay, d.rootDispersion, d.rootDistance, d.precision, d.leapIndicator,
		d.jitter, d.stability, d.asymmetry, d.samples, d.packetLoss,
		d.trustScore,
		d.poolActive, d.poolTotal, d.poolDNS, d.poolBestOffset,
		d.kernelOffset, d.kernelFrequency, d.kernelMaxError, d.kernelEstError,
		d.kernelPrecision, d.kernelSyncStatus, d.kernelStatusCode,
		d.kernelDivergence, d.kernelCoherence,
	}
}

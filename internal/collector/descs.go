package collector

import (
	"github.com/maximewewer/ntp-exporter/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

// descs holds the descriptors of every metric derived from measurements
type descs struct {
	// Target health
	up          *prometheus.Desc
	lastSuccess *prometheus.Desc
	suspended   *prometheus.Desc

	// Server identity
	info *prometheus.Desc

	// Base NTP metrics
	offset         *prometheus.Desc
	rtt            *prometheus.Desc
	stratum        *prometheus.Desc
	referenceAge   *prometheus.Desc
	rootDelay      *prometheus.Desc
	rootDispersion *prometheus.Desc
	rootDistance   *prometheus.Desc
	precision      *prometheus.Desc
	leapIndicator  *prometheus.Desc

	// Quality metrics
	jitter     *prometheus.Desc
	samples    *prometheus.Desc
	packetLoss *prometheus.Desc

	// Validity
	responseValid *prometheus.Desc

	// NTS key exchange
	ntsKESuccess  *prometheus.Desc
	ntsCertExpiry *prometheus.Desc

	// /probe endpoint
	probeSuccess  *prometheus.Desc
	probeDuration *prometheus.Desc

	// Consensus metrics
	consensusOffset     *prometheus.Desc
	consensusServers    *prometheus.Desc
	consensusChimers    *prometheus.Desc
	consensusMajority   *prometheus.Desc
	offsetFromConsensus *prometheus.Desc
	falseticker         *prometheus.Desc

	// Pool metrics
	poolActive *prometheus.Desc
	poolTotal  *prometheus.Desc

	// Kernel metrics
	kernelSyncStatus     *prometheus.Desc
	kernelBoundViolation *prometheus.Desc

	// chronyd tracking report
	chronyUp             *prometheus.Desc
	chronyInfo           *prometheus.Desc
	chronyStratum        *prometheus.Desc
	chronyLeapStatus     *prometheus.Desc
	chronyRefTime        *prometheus.Desc
	chronyCorrection     *prometheus.Desc
	chronyRootDispersion *prometheus.Desc
}

func newDescs() *descs {
	d := func(sub, name, help string, labels ...string) *prometheus.Desc {
		return prometheus.NewDesc(prometheus.BuildFQName(metrics.Namespace, sub, name), help, labels, nil)
	}
	const subsystem = ""
	server := []string{"server"}
	pool := []string{"pool"}

	return &descs{
		up:          d(subsystem, "up", "Whether the NTP server answered during the last cycle (1) or not (0)", server...),
		lastSuccess: d(subsystem, "last_success_timestamp_seconds", "Unix time of the last cycle with a valid response from the server", server...),
		suspended:   d(subsystem, "target_suspended", "Whether the server is no longer queried after a DENY or RSTR Kiss-o'-Death (1) or not (0)", "server", "code"),

		info: d(subsystem, "server_info", "Identity of the NTP server from its last valid response: queried address, pool, reference ID and NTP version of the response (value is always 1)",
			"server", "address", "pool", "refid", "version"),

		offset:         d(subsystem, "offset_seconds", "Time offset between local clock and NTP server in seconds, from the sample with the lowest delay (RFC 5905 clock filter); positive when the local clock is behind", server...),
		rtt:            d(subsystem, "rtt_seconds", "Round-trip time to NTP server in seconds", server...),
		stratum:        d(subsystem, "stratum", "NTP server stratum level (1-15)", server...),
		referenceAge:   d(subsystem, "reference_age_seconds", "Time elapsed since the server clock was last set or corrected (transmit time minus reference time) in seconds", server...),
		rootDelay:      d(subsystem, "root_delay_seconds", "Root delay of the NTP server in seconds", server...),
		rootDispersion: d(subsystem, "root_dispersion_seconds", "Root dispersion of the NTP server in seconds", server...),
		rootDistance:   d(subsystem, "root_distance_seconds", "Synchronization distance to the stratum 1 source in seconds: (rtt + root delay) / 2 + root dispersion, an upper bound of the server error", server...),
		precision:      d(subsystem, "precision_seconds", "Precision of the NTP server in seconds", server...),
		leapIndicator:  d(subsystem, "leap_indicator", "Leap second indicator (0=no warning, 1=61s, 2=59s, 3=unsync)", server...),

		jitter:     d(subsystem, "jitter_seconds", "RFC 5905 jitter: RMS of the offset differences between the lowest-delay sample and the other samples, in seconds", server...),
		samples:    d(subsystem, "samples_count", "Number of valid samples used for calculation", server...),
		packetLoss: d(subsystem, "packet_loss_ratio", "Ratio of queries left without any response during the last cycle (0-1); invalid responses are counted in invalid_responses_total", server...),

		responseValid: d(subsystem, "response_valid", "Whether the server returned at least one valid time measurement during the last cycle (1) or only invalid responses (0)", server...),

		ntsKESuccess:  d(subsystem, "nts_ke_success", "Whether the last NTS key exchange with the server succeeded (1) or not (0)", server...),
		ntsCertExpiry: d(subsystem, "nts_cert_expiry_timestamp_seconds", "Unix time at which the certificate of the NTS key exchange server expires", server...),

		probeSuccess:  d(subsystem, "probe_success", "Whether the /probe request got a valid time measurement from the target (1) or not (0)"),
		probeDuration: d(subsystem, "probe_duration_seconds", "Duration of the /probe request burst in seconds"),

		consensusOffset:     d(subsystem, "consensus_offset_seconds", "Offset of the local clock against the consensus of the servers (truechimers weighted by 1/root distance) in seconds, only with a majority"),
		consensusServers:    d(subsystem, "consensus_servers", "Number of valid servers taking part in the consensus"),
		consensusChimers:    d(subsystem, "consensus_truechimers", "Number of servers agreeing with the consensus; without majority, the largest number of servers agreeing with each other"),
		consensusMajority:   d(subsystem, "consensus_majority", "1 when more than half of the servers agree, so that the consensus offset and the falsetickers are meaningful"),
		offsetFromConsensus: d(subsystem, "offset_from_consensus_seconds", "Offset of the server against the consensus in seconds, independent of the local clock", server...),
		falseticker:         d(subsystem, "falseticker", "Whether the server correctness interval misses the majority intersection (1) or not (0)", server...),

		poolActive: d("pool", "servers_active", "Number of pool servers with a valid response", pool...),
		poolTotal:  d("pool", "servers_total", "Total number of servers returned by the pool resolution", pool...),

		kernelSyncStatus: d(subsystem, "kernel_sync_status", "Whether the kernel clock is synchronized (1) or not (0, STA_UNSYNC set)"),
		kernelBoundViolation: d(subsystem, "kernel_error_bound_violated",
			"Whether the offset measured against the reference server (lowest root distance) exceeds the maximum error announced by the kernel, beyond its root distance (1) or not (0)"),

		chronyUp:             d(subsystem, "chrony_up", "Whether the chronyd tracking report could be read (1) or not (0)"),
		chronyInfo:           d(subsystem, "chrony_tracking_info", "Source selected by chronyd: reference ID (hex) and address or reference clock name (value is always 1)", "refid", "source"),
		chronyStratum:        d(subsystem, "chrony_stratum", "Stratum of the local clock disciplined by chronyd"),
		chronyLeapStatus:     d(subsystem, "chrony_leap_status", "chronyd leap status (0=normal, 1=insert second, 2=delete second, 3=not synchronised)"),
		chronyRefTime:        d(subsystem, "chrony_reference_timestamp_seconds", "Unix time of the last measurement used by chronyd to update the clock"),
		chronyCorrection:     d(subsystem, "chrony_system_time_offset_seconds", "Offset chronyd is still slewing out of the system clock in seconds (positive: the system clock is slow)"),
		chronyRootDispersion: d(subsystem, "chrony_root_dispersion_seconds", "Root dispersion of the local clock in seconds"),
	}
}

// all returns every descriptor
func (d *descs) all() []*prometheus.Desc {
	return []*prometheus.Desc{
		d.up, d.lastSuccess, d.suspended, d.info,
		d.offset, d.rtt, d.stratum, d.referenceAge,
		d.rootDelay, d.rootDispersion, d.rootDistance, d.precision, d.leapIndicator,
		d.jitter, d.samples, d.packetLoss,
		d.responseValid, d.ntsKESuccess, d.ntsCertExpiry,
		d.consensusOffset, d.consensusServers, d.consensusChimers, d.consensusMajority, d.offsetFromConsensus, d.falseticker,
		d.poolActive, d.poolTotal,
		d.kernelSyncStatus, d.kernelBoundViolation,
		d.chronyUp, d.chronyInfo, d.chronyStratum, d.chronyLeapStatus, d.chronyRefTime,
		d.chronyCorrection, d.chronyRootDispersion,
	}
}

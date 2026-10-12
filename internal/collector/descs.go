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

	// Server identity
	info *prometheus.Desc

	// Base NTP metrics
	offset         *prometheus.Desc
	offsetMedian   *prometheus.Desc
	offsetExceeded *prometheus.Desc
	rtt            *prometheus.Desc
	stratum        *prometheus.Desc
	referenceAge   *prometheus.Desc
	rootDelay      *prometheus.Desc
	rootDispersion *prometheus.Desc
	rootDistance   *prometheus.Desc
	precision      *prometheus.Desc
	leapIndicator  *prometheus.Desc

	// Quality metrics
	jitter       *prometheus.Desc
	offsetStdDev *prometheus.Desc
	rttStdDev    *prometheus.Desc
	samples      *prometheus.Desc
	packetLoss   *prometheus.Desc

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
	offsetFromConsensus *prometheus.Desc
	falseticker         *prometheus.Desc

	// Pool metrics
	poolActive *prometheus.Desc
	poolTotal  *prometheus.Desc
	poolDNS    *prometheus.Desc
	poolOffset *prometheus.Desc

	// Kernel metrics
	kernelSyncStatus     *prometheus.Desc
	kernelState          *prometheus.Desc
	kernelOffset         *prometheus.Desc
	kernelFrequency      *prometheus.Desc
	kernelMaxError       *prometheus.Desc
	kernelEstError       *prometheus.Desc
	kernelTAI            *prometheus.Desc
	kernelBoundViolation *prometheus.Desc

	// chronyd tracking report
	chronyUp                *prometheus.Desc
	chronyInfo              *prometheus.Desc
	chronyStratum           *prometheus.Desc
	chronyLeapStatus        *prometheus.Desc
	chronyRefTime           *prometheus.Desc
	chronyCorrection        *prometheus.Desc
	chronyLastOffset        *prometheus.Desc
	chronyRMSOffset         *prometheus.Desc
	chronyFrequency         *prometheus.Desc
	chronyResidualFrequency *prometheus.Desc
	chronySkew              *prometheus.Desc
	chronyRootDelay         *prometheus.Desc
	chronyRootDispersion    *prometheus.Desc
	chronyUpdateInterval    *prometheus.Desc
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

		info: d(subsystem, "server_info", "Identity of the NTP server from its last valid response (value is always 1)",
			"server", "address", "pool", "refid", "stratum", "leap", "version"),

		offset:         d(subsystem, "offset_seconds", "Time offset between local clock and NTP server in seconds, from the sample with the lowest delay (RFC 5905 clock filter)", server...),
		offsetMedian:   d(subsystem, "offset_median_seconds", "Median time offset of the valid samples of the last burst in seconds", server...),
		offsetExceeded: d(subsystem, "clock_offset_exceeded", "Whether the clock offset exceeds the configured threshold (1 = exceeded, 0 = within limits)", server...),
		rtt:            d(subsystem, "rtt_seconds", "Round-trip time to NTP server in seconds", server...),
		stratum:        d(subsystem, "stratum", "NTP server stratum level (1-15)", server...),
		referenceAge:   d(subsystem, "reference_age_seconds", "Time elapsed since the server clock was last set or corrected (transmit time minus reference time) in seconds", server...),
		rootDelay:      d(subsystem, "root_delay_seconds", "Root delay of the NTP server in seconds", server...),
		rootDispersion: d(subsystem, "root_dispersion_seconds", "Root dispersion of the NTP server in seconds", server...),
		rootDistance:   d(subsystem, "root_distance_seconds", "Synchronization distance to the stratum 1 source (RFC 5905: root delay/2 + root dispersion + measurement delay and dispersion) in seconds", server...),
		precision:      d(subsystem, "precision_seconds", "Precision of the NTP server in seconds", server...),
		leapIndicator:  d(subsystem, "leap_indicator", "Leap second indicator (0=no warning, 1=61s, 2=59s, 3=unsync)", server...),

		jitter:       d(subsystem, "jitter_seconds", "RFC 5905 jitter: RMS of the offset differences between the lowest-delay sample and the other samples, in seconds", server...),
		offsetStdDev: d(subsystem, "offset_stddev_seconds", "Standard deviation of the offsets of the last burst in seconds", server...),
		rttStdDev:    d(subsystem, "rtt_stddev_seconds", "Standard deviation of the round-trip delays of the last burst in seconds", server...),
		samples:      d(subsystem, "samples_count", "Number of valid samples used for calculation", server...),
		packetLoss:   d(subsystem, "packet_loss_ratio", "Ratio of queries without valid response during the last cycle (0-1)", server...),

		responseValid: d(subsystem, "response_valid", "Whether the server returned at least one valid time measurement during the last cycle (1) or only invalid responses (0)", server...),

		ntsKESuccess:  d(subsystem, "nts_ke_success", "Whether the last NTS key exchange with the server succeeded (1) or not (0)", server...),
		ntsCertExpiry: d(subsystem, "nts_cert_expiry_timestamp_seconds", "Unix time at which the certificate of the NTS key exchange server expires", server...),

		probeSuccess:  d(subsystem, "probe_success", "Whether the /probe request got a valid time measurement from the target (1) or not (0)"),
		probeDuration: d(subsystem, "probe_duration_seconds", "Duration of the /probe request burst in seconds"),

		consensusOffset:     d(subsystem, "consensus_offset_seconds", "Offset of the local clock against the consensus of the servers (median of the truechimers) in seconds"),
		consensusServers:    d(subsystem, "consensus_servers", "Number of valid servers taking part in the consensus"),
		consensusChimers:    d(subsystem, "consensus_truechimers", "Number of servers agreeing with the consensus"),
		offsetFromConsensus: d(subsystem, "offset_from_consensus_seconds", "Offset of the server against the consensus in seconds, independent of the local clock", server...),
		falseticker:         d(subsystem, "falseticker", "Whether the server correctness interval misses the majority intersection (1) or not (0)", server...),

		poolActive: d("pool", "servers_active", "Number of pool servers with a valid response", pool...),
		poolTotal:  d("pool", "servers_total", "Total number of servers returned by the pool resolution", pool...),
		poolDNS:    d("pool", "dns_resolution_seconds", "DNS resolution duration for pool in seconds", pool...),
		poolOffset: d("pool", "offset_median_seconds", "Median offset of the valid pool servers in seconds", pool...),

		kernelSyncStatus: d(subsystem, "kernel_sync_status", "Whether the kernel clock is synchronized (1) or not (0, STA_UNSYNC set)", node...),
		kernelState:      d(subsystem, "kernel_state", "Kernel clock state returned by adjtimex (value is always 1)", "node", "state"),
		kernelOffset:     d(subsystem, "kernel_offset_seconds", "Residual offset the kernel PLL still has to correct in seconds", node...),
		kernelFrequency:  d(subsystem, "kernel_frequency_ppm", "Kernel frequency adjustment in PPM", node...),
		kernelMaxError:   d(subsystem, "kernel_max_error_seconds", "Maximum error announced by the kernel clock in seconds", node...),
		kernelEstError:   d(subsystem, "kernel_est_error_seconds", "Estimated error announced by the kernel clock in seconds", node...),
		kernelTAI:        d(subsystem, "kernel_tai_offset_seconds", "TAI-UTC offset known by the kernel in seconds", node...),
		kernelBoundViolation: d(subsystem, "kernel_error_bound_violated",
			"Whether the offset measured against the server exceeds the error bound announced by the kernel, beyond the measurement uncertainty (1) or not (0)",
			"node", "server", "bound"),

		chronyUp:                d(subsystem, "chrony_up", "Whether the chronyd tracking report could be read (1) or not (0)", node...),
		chronyInfo:              d(subsystem, "chrony_tracking_info", "Source selected by chronyd: reference ID (hex) and address or reference clock name (value is always 1)", "node", "refid", "source"),
		chronyStratum:           d(subsystem, "chrony_stratum", "Stratum of the local clock disciplined by chronyd", node...),
		chronyLeapStatus:        d(subsystem, "chrony_leap_status", "chronyd leap status (0=normal, 1=insert second, 2=delete second, 3=not synchronised)", node...),
		chronyRefTime:           d(subsystem, "chrony_reference_timestamp_seconds", "Unix time of the last measurement used by chronyd to update the clock", node...),
		chronyCorrection:        d(subsystem, "chrony_system_time_offset_seconds", "Offset chronyd is still slewing out of the system clock in seconds (positive: the system clock is slow)", node...),
		chronyLastOffset:        d(subsystem, "chrony_last_offset_seconds", "Offset estimated by chronyd at the last clock update in seconds", node...),
		chronyRMSOffset:         d(subsystem, "chrony_rms_offset_seconds", "Long-term average of the offsets estimated by chronyd in seconds", node...),
		chronyFrequency:         d(subsystem, "chrony_frequency_ppm", "Frequency error of the system clock corrected by chronyd in PPM (negative: the clock runs slow without correction, like chronyc)", node...),
		chronyResidualFrequency: d(subsystem, "chrony_residual_frequency_ppm", "Residual frequency of the selected source in PPM", node...),
		chronySkew:              d(subsystem, "chrony_skew_ppm", "Estimated error bound of the frequency in PPM", node...),
		chronyRootDelay:         d(subsystem, "chrony_root_delay_seconds", "Root delay of the local clock to the stratum 1 source in seconds", node...),
		chronyRootDispersion:    d(subsystem, "chrony_root_dispersion_seconds", "Root dispersion of the local clock in seconds", node...),
		chronyUpdateInterval:    d(subsystem, "chrony_update_interval_seconds", "Interval between the last two clock updates of chronyd in seconds", node...),
	}
}

// all returns every descriptor
func (d *descs) all() []*prometheus.Desc {
	return []*prometheus.Desc{
		d.up, d.lastSuccess, d.suspended, d.info,
		d.offset, d.offsetMedian, d.offsetExceeded, d.rtt, d.stratum, d.referenceAge,
		d.rootDelay, d.rootDispersion, d.rootDistance, d.precision, d.leapIndicator,
		d.jitter, d.offsetStdDev, d.rttStdDev, d.samples, d.packetLoss,
		d.responseValid, d.ntsKESuccess, d.ntsCertExpiry,
		d.consensusOffset, d.consensusServers, d.consensusChimers, d.offsetFromConsensus, d.falseticker,
		d.poolActive, d.poolTotal, d.poolDNS, d.poolOffset,
		d.kernelSyncStatus, d.kernelState, d.kernelOffset, d.kernelFrequency,
		d.kernelMaxError, d.kernelEstError, d.kernelTAI, d.kernelBoundViolation,
		d.chronyUp, d.chronyInfo, d.chronyStratum, d.chronyLeapStatus, d.chronyRefTime,
		d.chronyCorrection, d.chronyLastOffset, d.chronyRMSOffset, d.chronyFrequency,
		d.chronyResidualFrequency, d.chronySkew, d.chronyRootDelay, d.chronyRootDispersion,
		d.chronyUpdateInterval,
	}
}

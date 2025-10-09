package ntp

import "time"

// Query behavior constants
const (
	// DelayBetweenSamples is the delay between consecutive NTP samples
	DelayBetweenSamples = 100 * time.Millisecond

	// DefaultTimeout is the default timeout for NTP queries
	DefaultTimeout = 5 * time.Second
)

// Validation thresholds
const (
	// MaxAcceptableOffset is the maximum acceptable time offset
	MaxAcceptableOffset = 10 * time.Second

	// MaxAcceptableRTT is the maximum acceptable round-trip time
	MaxAcceptableRTT = 10 * time.Second

	// MaxClockSkewWarning is the threshold for clock skew warnings
	MaxClockSkewWarning = 1 * time.Hour

	// MaxRootDispersion is the maximum acceptable root dispersion
	MaxRootDispersion = 5 * time.Second

	// SuspiciousOffsetThreshold is the threshold for suspicious offsets
	SuspiciousOffsetThreshold = 3600 * time.Second // 1 hour
)

// Stratum limits
const (
	// MinValidStratum is the minimum valid stratum value
	MinValidStratum = 1

	// MaxValidStratum is the maximum valid stratum value
	MaxValidStratum = 15

	// InvalidStratum indicates an invalid stratum
	InvalidStratum = 16
)

// Statistical analysis constants
const (
	// MinSamplesForStats is the minimum number of samples needed for statistics
	MinSamplesForStats = 3

	// MaxSamplesPerQuery is the maximum number of samples per query
	MaxSamplesPerQuery = 20
)

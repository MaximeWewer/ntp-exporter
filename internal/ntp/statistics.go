package ntp

import (
	"math"
	"sort"
	"time"
)

// Statistics represents statistical calculations from multiple NTP samples
type Statistics struct {
	MedianOffset time.Duration
	MeanOffset   time.Duration
	// StdDevOffset is the sample standard deviation of the offsets
	StdDevOffset time.Duration
	// StdDevRTT is the sample standard deviation of the round-trip delays
	StdDevRTT time.Duration
	// Jitter is the RFC 5905 jitter: RMS of the offset differences between the
	// lowest-delay sample and the other samples
	Jitter          time.Duration
	SamplesCount    int
	PacketLossRatio float64
}

// CalculateStatistics computes statistics from multiple NTP responses
func CalculateStatistics(responses []*Response, totalSamples int) *Statistics {
	if len(responses) == 0 {
		return &Statistics{
			PacketLossRatio: 1.0,
		}
	}

	stats := &Statistics{
		SamplesCount:    len(responses),
		PacketLossRatio: calculatePacketLoss(len(responses), totalSamples),
	}

	// Extract offsets and RTTs using pooled slices
	offsetsPtr := GetFloat64Slice(len(responses))
	rttsPtr := GetFloat64Slice(len(responses))
	defer PutFloat64Slice(offsetsPtr)
	defer PutFloat64Slice(rttsPtr)

	offsets := *offsetsPtr
	rtts := *rttsPtr

	// Resize slices to exact length needed
	offsets = offsets[:len(responses)]
	rtts = rtts[:len(responses)]

	for i, resp := range responses {
		offsets[i] = resp.Offset.Seconds()
		rtts[i] = resp.RTT.Seconds()
	}

	// Calculate median offset
	stats.MedianOffset = time.Duration(median(offsets) * float64(time.Second))

	// Calculate mean offset
	stats.MeanOffset = time.Duration(mean(offsets) * float64(time.Second))

	// Calculate standard deviation (stability)
	stats.StdDevOffset = time.Duration(stdDev(offsets) * float64(time.Second))

	// Calculate delay variability
	stats.StdDevRTT = time.Duration(stdDev(rtts) * float64(time.Second))

	// Calculate RFC 5905 jitter around the lowest-delay sample
	stats.Jitter = time.Duration(jitter(offsets, rtts) * float64(time.Second))

	return stats
}

func calculatePacketLoss(received, total int) float64 {
	if total == 0 {
		return 0.0
	}
	lost := total - received
	if lost < 0 {
		lost = 0
	}
	return float64(lost) / float64(total)
}

// median calculates the median value of a slice of float64 values.
// For even-length slices, returns the average of the two middle values.
// Returns 0 if the slice is empty.
func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	// Create a copy and sort using pooled slice
	sortedPtr := GetFloat64Slice(len(values))
	defer PutFloat64Slice(sortedPtr)

	sorted := *sortedPtr
	sorted = sorted[:len(values)]
	copy(sorted, values)
	sortFloat64(sorted)

	n := len(sorted)
	if n%2 == 0 {
		return (sorted[n/2-1] + sorted[n/2]) / 2.0
	}
	return sorted[n/2]
}

// mean calculates the arithmetic mean (average) of a slice of float64 values.
// Returns 0 if the slice is empty.
func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// stdDev calculates the standard deviation of a slice of float64 values
// using the sample standard deviation formula (N-1 denominator).
// Returns 0 if the slice has 0 or 1 element.
func stdDev(values []float64) float64 {
	if len(values) <= 1 {
		return 0
	}

	m := mean(values)
	sumSquaredDiff := 0.0

	for _, v := range values {
		diff := v - m
		sumSquaredDiff += diff * diff
	}

	variance := sumSquaredDiff / float64(len(values)-1)
	return math.Sqrt(variance)
}

// jitter computes the RFC 5905 §10 jitter: the root mean square of the
// differences between the offset of the lowest-delay sample and the offsets
// of the other samples. Returns 0 with fewer than 2 samples.
func jitter(offsets, rtts []float64) float64 {
	n := len(offsets)
	if n < 2 {
		return 0
	}

	best := 0
	for i := range rtts {
		if rtts[i] < rtts[best] {
			best = i
		}
	}

	sum := 0.0
	for i, offset := range offsets {
		if i == best {
			continue
		}
		diff := offsets[best] - offset
		sum += diff * diff
	}
	return math.Sqrt(sum / float64(n-1))
}

// sortFloat64 sorts a slice of float64 values in-place using the standard library
// This is significantly faster than bubble sort (O(n log n) vs O(n²))
func sortFloat64(values []float64) {
	sort.Float64s(values)
}

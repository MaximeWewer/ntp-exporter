package ntp

import (
	"math"
	"sort"
	"time"
)

// Statistics represents statistical calculations from multiple NTP samples
type Statistics struct {
	MedianOffset    time.Duration
	MeanOffset      time.Duration
	StdDevOffset    time.Duration
	Jitter          time.Duration
	Asymmetry       time.Duration
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

	// Calculate jitter (RTT variability)
	stats.Jitter = time.Duration(stdDev(rtts) * float64(time.Second))

	// Calculate asymmetry (simplified as RTT variance)
	if len(rtts) >= 2 {
		stats.Asymmetry = time.Duration((max(rtts) - min(rtts)) * float64(time.Second))
	}

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

// min returns the minimum value from a slice of float64 values.
// Returns 0 if the slice is empty.
func min(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	minVal := values[0]
	for _, v := range values[1:] {
		if v < minVal {
			minVal = v
		}
	}
	return minVal
}

// max returns the maximum value from a slice of float64 values.
// Returns 0 if the slice is empty.
func max(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	maxVal := values[0]
	for _, v := range values[1:] {
		if v > maxVal {
			maxVal = v
		}
	}
	return maxVal
}

// sortFloat64 sorts a slice of float64 values in-place using the standard library
// This is significantly faster than bubble sort (O(n log n) vs O(n²))
func sortFloat64(values []float64) {
	sort.Float64s(values)
}

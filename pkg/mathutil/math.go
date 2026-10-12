package mathutil

import (
	"sort"
	"time"
)

// AbsFloat64 returns the absolute value of a float64
func AbsFloat64(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// AbsDuration returns the absolute value of a duration
func AbsDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// Min returns the minimum of two float64 values
func Min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// Max returns the maximum of two float64 values
func Max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// Clamp clamps a value between min and max
func Clamp(val, min, max float64) float64 {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

// MinDuration returns the minimum of two durations
func MinDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// MaxDuration returns the maximum of two durations
func MaxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

// Median returns the median of values (the mean of the two middle values for
// an even count), or 0 for an empty slice. The input is not modified.
func Median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 0 {
		return (sorted[n/2-1] + sorted[n/2]) / 2
	}
	return sorted[n/2]
}

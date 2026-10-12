package mathutil

import (
	"math"
	"testing"
	"time"
)

func TestAbsDuration(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Duration
		expected time.Duration
	}{
		{"positive", 5 * time.Second, 5 * time.Second},
		{"negative", -5 * time.Second, 5 * time.Second},
		{"zero", 0, 0},
		{"positive millisecond", 100 * time.Millisecond, 100 * time.Millisecond},
		{"negative millisecond", -100 * time.Millisecond, 100 * time.Millisecond},
		{"positive nanosecond", 1, 1},
		{"negative nanosecond", -1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AbsDuration(tt.input)
			if result != tt.expected {
				t.Errorf("AbsDuration(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestMin(t *testing.T) {
	tests := []struct {
		name     string
		a        float64
		b        float64
		expected float64
	}{
		{"a smaller", 1.0, 2.0, 1.0},
		{"b smaller", 2.0, 1.0, 1.0},
		{"equal", 5.0, 5.0, 5.0},
		{"negative a smaller", -10.0, -5.0, -10.0},
		{"negative b smaller", -5.0, -10.0, -10.0},
		{"mixed signs", -5.0, 5.0, -5.0},
		{"zero and positive", 0.0, 5.0, 0.0},
		{"zero and negative", 0.0, -5.0, -5.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Min(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("Min(%v, %v) = %v, want %v", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestClamp(t *testing.T) {
	tests := []struct {
		name     string
		val      float64
		min      float64
		max      float64
		expected float64
	}{
		{"within range", 5.0, 0.0, 10.0, 5.0},
		{"below min", -5.0, 0.0, 10.0, 0.0},
		{"above max", 15.0, 0.0, 10.0, 10.0},
		{"equals min", 0.0, 0.0, 10.0, 0.0},
		{"equals max", 10.0, 0.0, 10.0, 10.0},
		{"negative range within", -5.0, -10.0, 0.0, -5.0},
		{"negative range below", -15.0, -10.0, 0.0, -10.0},
		{"negative range above", 5.0, -10.0, 0.0, 0.0},
		{"zero range", 5.0, 5.0, 5.0, 5.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Clamp(tt.val, tt.min, tt.max)
			if result != tt.expected {
				t.Errorf("Clamp(%v, %v, %v) = %v, want %v", tt.val, tt.min, tt.max, result, tt.expected)
			}
		})
	}
}

// Benchmarks

func BenchmarkAbsDuration(b *testing.B) {
	values := []time.Duration{-5 * time.Second, 10 * time.Millisecond, -100 * time.Microsecond}
	for i := 0; i < b.N; i++ {
		_ = AbsDuration(values[i%len(values)])
	}
}

func BenchmarkMin(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = Min(float64(i), float64(i+1))
	}
}

func BenchmarkClamp(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = Clamp(float64(i), 0.0, 100.0)
	}
}

// Edge cases

func TestClampEdgeCases(t *testing.T) {
	// Test with infinity
	if Clamp(math.Inf(1), 0, 100) != 100 {
		t.Error("Clamp(+Inf, 0, 100) should be 100")
	}
	if Clamp(math.Inf(-1), 0, 100) != 0 {
		t.Error("Clamp(-Inf, 0, 100) should be 0")
	}
}

func TestMedian(t *testing.T) {
	if Median(nil) != 0 {
		t.Error("median of empty slice must be 0")
	}
	in := []float64{3, 1, 2}
	if Median(in) != 2 || in[0] != 3 {
		t.Error("odd median wrong or input modified")
	}
	if Median([]float64{4, 1, 3, 2}) != 2.5 {
		t.Error("even median must average the middle values")
	}
}

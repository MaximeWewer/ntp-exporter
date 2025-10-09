package mathutil

import (
	"math"
	"testing"
	"time"
)

func TestAbsFloat64(t *testing.T) {
	tests := []struct {
		name     string
		input    float64
		expected float64
	}{
		{"positive", 5.5, 5.5},
		{"negative", -5.5, 5.5},
		{"zero", 0, 0},
		{"large positive", 999999.99, 999999.99},
		{"large negative", -999999.99, 999999.99},
		{"small positive", 0.0001, 0.0001},
		{"small negative", -0.0001, 0.0001},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AbsFloat64(tt.input)
			if result != tt.expected {
				t.Errorf("AbsFloat64(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

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

func TestMax(t *testing.T) {
	tests := []struct {
		name     string
		a        float64
		b        float64
		expected float64
	}{
		{"a larger", 2.0, 1.0, 2.0},
		{"b larger", 1.0, 2.0, 2.0},
		{"equal", 5.0, 5.0, 5.0},
		{"negative a larger", -5.0, -10.0, -5.0},
		{"negative b larger", -10.0, -5.0, -5.0},
		{"mixed signs", -5.0, 5.0, 5.0},
		{"zero and positive", 0.0, 5.0, 5.0},
		{"zero and negative", 0.0, -5.0, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Max(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("Max(%v, %v) = %v, want %v", tt.a, tt.b, result, tt.expected)
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

func TestMinDuration(t *testing.T) {
	tests := []struct {
		name     string
		a        time.Duration
		b        time.Duration
		expected time.Duration
	}{
		{"a smaller", 1 * time.Second, 2 * time.Second, 1 * time.Second},
		{"b smaller", 2 * time.Second, 1 * time.Second, 1 * time.Second},
		{"equal", 5 * time.Second, 5 * time.Second, 5 * time.Second},
		{"negative a smaller", -10 * time.Second, -5 * time.Second, -10 * time.Second},
		{"negative b smaller", -5 * time.Second, -10 * time.Second, -10 * time.Second},
		{"mixed signs", -5 * time.Second, 5 * time.Second, -5 * time.Second},
		{"zero and positive", 0, 5 * time.Second, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MinDuration(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("MinDuration(%v, %v) = %v, want %v", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestMaxDuration(t *testing.T) {
	tests := []struct {
		name     string
		a        time.Duration
		b        time.Duration
		expected time.Duration
	}{
		{"a larger", 2 * time.Second, 1 * time.Second, 2 * time.Second},
		{"b larger", 1 * time.Second, 2 * time.Second, 2 * time.Second},
		{"equal", 5 * time.Second, 5 * time.Second, 5 * time.Second},
		{"negative a larger", -5 * time.Second, -10 * time.Second, -5 * time.Second},
		{"negative b larger", -10 * time.Second, -5 * time.Second, -5 * time.Second},
		{"mixed signs", -5 * time.Second, 5 * time.Second, 5 * time.Second},
		{"zero and positive", 0, 5 * time.Second, 5 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaxDuration(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("MaxDuration(%v, %v) = %v, want %v", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

// Benchmarks

func BenchmarkAbsFloat64(b *testing.B) {
	values := []float64{-123.456, 789.012, -0.001, 999999.99}
	for i := 0; i < b.N; i++ {
		_ = AbsFloat64(values[i%len(values)])
	}
}

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

func BenchmarkMax(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = Max(float64(i), float64(i+1))
	}
}

func BenchmarkClamp(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = Clamp(float64(i), 0.0, 100.0)
	}
}

// Edge cases

func TestAbsFloat64EdgeCases(t *testing.T) {
	// Test infinity
	if AbsFloat64(math.Inf(1)) != math.Inf(1) {
		t.Error("AbsFloat64(+Inf) should be +Inf")
	}
	if AbsFloat64(math.Inf(-1)) != math.Inf(1) {
		t.Error("AbsFloat64(-Inf) should be +Inf")
	}

	// Test NaN (NaN behavior is preserved)
	if !math.IsNaN(AbsFloat64(math.NaN())) {
		t.Error("AbsFloat64(NaN) should be NaN")
	}
}

func TestClampEdgeCases(t *testing.T) {
	// Test with infinity
	if Clamp(math.Inf(1), 0, 100) != 100 {
		t.Error("Clamp(+Inf, 0, 100) should be 100")
	}
	if Clamp(math.Inf(-1), 0, 100) != 0 {
		t.Error("Clamp(-Inf, 0, 100) should be 0")
	}
}

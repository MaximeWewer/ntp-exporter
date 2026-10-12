package ntp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCalculateStatistics_Empty(t *testing.T) {
	stats := CalculateStatistics([]*Response{}, 5)

	assert.NotNil(t, stats)
	assert.Equal(t, 0, stats.SamplesCount)
	assert.Equal(t, 1.0, stats.PacketLossRatio)
	assert.Equal(t, time.Duration(0), stats.MedianOffset)
	assert.Equal(t, time.Duration(0), stats.MeanOffset)
}

func TestCalculateStatistics_SingleSample(t *testing.T) {
	responses := []*Response{
		{
			Server: "pool.ntp.org",
			Offset: 100 * time.Millisecond,
			RTT:    50 * time.Millisecond,
		},
	}

	stats := CalculateStatistics(responses, 1)

	assert.Equal(t, 1, stats.SamplesCount)
	assert.Equal(t, 0.0, stats.PacketLossRatio)
	assert.Equal(t, 100*time.Millisecond, stats.MedianOffset)
	assert.Equal(t, 100*time.Millisecond, stats.MeanOffset)
	assert.Equal(t, time.Duration(0), stats.StdDevOffset) // Only one sample
	assert.Equal(t, time.Duration(0), stats.Jitter)       // Only one sample
	assert.Equal(t, time.Duration(0), stats.StdDevRTT)    // Only one sample
}

func TestCalculateStatistics_MultipleSamples(t *testing.T) {
	responses := []*Response{
		{Offset: 100 * time.Millisecond, RTT: 50 * time.Millisecond},
		{Offset: 150 * time.Millisecond, RTT: 60 * time.Millisecond},
		{Offset: 80 * time.Millisecond, RTT: 55 * time.Millisecond},
		{Offset: 120 * time.Millisecond, RTT: 52 * time.Millisecond},
		{Offset: 110 * time.Millisecond, RTT: 58 * time.Millisecond},
	}

	stats := CalculateStatistics(responses, 5)

	assert.Equal(t, 5, stats.SamplesCount)
	assert.Equal(t, 0.0, stats.PacketLossRatio)
	assert.Equal(t, 110*time.Millisecond, stats.MedianOffset)
	assert.Greater(t, stats.MeanOffset, time.Duration(0))
	assert.Greater(t, stats.StdDevOffset, time.Duration(0))
	assert.Greater(t, stats.Jitter, time.Duration(0))
	assert.Greater(t, stats.StdDevRTT, time.Duration(0))
}

func TestCalculateStatistics_PacketLoss(t *testing.T) {
	tests := []struct {
		name         string
		received     int
		total        int
		expectedLoss float64
	}{
		{"no_loss", 5, 5, 0.0},
		{"partial_loss", 3, 5, 0.4},
		{"complete_loss", 0, 5, 1.0},
		{"zero_total", 0, 0, 1.0}, // When received=0, it's treated as complete loss
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			responses := make([]*Response, tt.received)
			for i := 0; i < tt.received; i++ {
				responses[i] = &Response{
					Offset: 100 * time.Millisecond,
					RTT:    50 * time.Millisecond,
				}
			}

			stats := CalculateStatistics(responses, tt.total)
			assert.InDelta(t, tt.expectedLoss, stats.PacketLossRatio, 0.01)
		})
	}
}

func TestMedian(t *testing.T) {
	tests := []struct {
		name     string
		values   []float64
		expected float64
	}{
		{"empty", []float64{}, 0.0},
		{"single", []float64{5.0}, 5.0},
		{"odd_count", []float64{1.0, 3.0, 2.0}, 2.0},
		{"even_count", []float64{1.0, 2.0, 3.0, 4.0}, 2.5},
		{"unsorted", []float64{5.0, 1.0, 3.0, 2.0, 4.0}, 3.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := median(tt.values)
			assert.InDelta(t, tt.expected, result, 0.01)
		})
	}
}

func TestMean(t *testing.T) {
	tests := []struct {
		name     string
		values   []float64
		expected float64
	}{
		{"empty", []float64{}, 0.0},
		{"single", []float64{5.0}, 5.0},
		{"multiple", []float64{1.0, 2.0, 3.0, 4.0, 5.0}, 3.0},
		{"negative", []float64{-1.0, -2.0, -3.0}, -2.0},
		{"mixed", []float64{-1.0, 0.0, 1.0}, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mean(tt.values)
			assert.InDelta(t, tt.expected, result, 0.01)
		})
	}
}

func TestStdDev(t *testing.T) {
	tests := []struct {
		name     string
		values   []float64
		expected float64
	}{
		{"empty", []float64{}, 0.0},
		{"single", []float64{5.0}, 0.0},
		{"identical", []float64{5.0, 5.0, 5.0}, 0.0},
		{"simple", []float64{1.0, 2.0, 3.0, 4.0, 5.0}, 1.58},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stdDev(tt.values)
			assert.InDelta(t, tt.expected, result, 0.01)
		})
	}
}

func TestCalculatePacketLoss(t *testing.T) {
	tests := []struct {
		name     string
		received int
		total    int
		expected float64
	}{
		{"no_loss", 10, 10, 0.0},
		{"half_loss", 5, 10, 0.5},
		{"all_loss", 0, 10, 1.0},
		{"zero_total", 0, 0, 0.0},
		{"more_received_than_total", 10, 5, 0.0}, // Should be clamped
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculatePacketLoss(tt.received, tt.total)
			assert.InDelta(t, tt.expected, result, 0.01)
		})
	}
}

func TestCalculateStatistics_NegativeOffsets(t *testing.T) {
	responses := []*Response{
		{Offset: -100 * time.Millisecond, RTT: 50 * time.Millisecond},
		{Offset: -80 * time.Millisecond, RTT: 52 * time.Millisecond},
		{Offset: -120 * time.Millisecond, RTT: 48 * time.Millisecond},
	}

	stats := CalculateStatistics(responses, 3)

	assert.Equal(t, 3, stats.SamplesCount)
	assert.Equal(t, -100*time.Millisecond, stats.MedianOffset)
	assert.Less(t, stats.MeanOffset, time.Duration(0))
}

func BenchmarkCalculateStatistics(b *testing.B) {
	responses := make([]*Response, 10)
	for i := 0; i < 10; i++ {
		responses[i] = &Response{
			Offset: time.Duration(i*10) * time.Millisecond,
			RTT:    time.Duration(50+i) * time.Millisecond,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CalculateStatistics(responses, 10)
	}
}

func BenchmarkMedian(b *testing.B) {
	values := []float64{1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0, 10.0}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = median(values)
	}
}

func BenchmarkStdDev(b *testing.B) {
	values := []float64{1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0, 10.0}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = stdDev(values)
	}
}

func TestCalculateStatistics_RFC5905Jitter(t *testing.T) {
	ms := time.Millisecond
	responses := []*Response{
		{Offset: 5 * ms, RTT: 40 * ms},
		{Offset: 2 * ms, RTT: 12 * ms}, // lowest delay: reference sample
		{Offset: 8 * ms, RTT: 60 * ms},
		{Offset: 3 * ms, RTT: 15 * ms},
	}

	stats := CalculateStatistics(responses, 4)

	// sqrt(((2-5)^2 + (2-8)^2 + (2-3)^2) / 3) = sqrt(46/3) ms
	assert.InDelta(t, 3.9158, float64(stats.Jitter)/float64(ms), 1e-3)
	assert.Greater(t, stats.StdDevRTT, 20*ms)
}

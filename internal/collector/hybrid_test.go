package collector

import (
	"context"
	"testing"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
	"github.com/stretchr/testify/assert"
)

func TestNewHybridCollector(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers:          []string{"pool.ntp.org"},
			Timeout:          5 * time.Second,
			Version:          4,
			SamplesPerServer: 1,
			EnableKernel:     true,
		},
	}
	m := metrics.NewNTPMetrics()

	collector := NewHybridCollector(cfg, m)

	assert.NotNil(t, collector)
	assert.NotNil(t, collector.GetConfig())
	assert.NotNil(t, collector.GetClient())
	assert.NotNil(t, collector.GetMetrics())
	assert.NotNil(t, collector.kernelReader)
	assert.Equal(t, "hybrid", collector.Name())
}

func TestHybridCollector_Collect_KernelDisabled(t *testing.T) {
	cfg := &config.Config{
		NTP: config.NTPConfig{
			Servers:          []string{"pool.ntp.org"},
			Timeout:          5 * time.Second,
			Version:          4,
			SamplesPerServer: 1,
			EnableKernel:     false, // Disabled
		},
	}
	m := metrics.NewNTPMetrics()

	collector := NewHybridCollector(cfg, m)
	ctx := context.Background()

	// Should not error when kernel is disabled
	err := collector.Collect(ctx)
	assert.NoError(t, err)
}

func TestCalculateCoherence(t *testing.T) {
	cfg := &config.Config{}

	tests := []struct {
		name         string
		ntpOffset    float64
		kernelOffset float64
		expectedMin  float64
		expectedMax  float64
		description  string
	}{
		{
			name:         "perfect_agreement",
			ntpOffset:    0.0005,
			kernelOffset: 0.0005,
			expectedMin:  1.0,
			expectedMax:  1.0,
			description:  "Identical offsets should give perfect score",
		},
		{
			name:         "excellent_1ms",
			ntpOffset:    0.001,
			kernelOffset: 0.0,
			expectedMin:  0.9,
			expectedMax:  1.0,
			description:  "1ms difference should be excellent",
		},
		{
			name:         "good_8ms",
			ntpOffset:    0.008,
			kernelOffset: 0.0,
			expectedMin:  0.7,
			expectedMax:  0.9,
			description:  "8ms difference should be good",
		},
		{
			name:         "acceptable_30ms",
			ntpOffset:    0.030,
			kernelOffset: 0.0,
			expectedMin:  0.5,
			expectedMax:  0.7,
			description:  "30ms difference should be acceptable",
		},
		{
			name:         "poor_100ms",
			ntpOffset:    0.100,
			kernelOffset: 0.0,
			expectedMin:  0.0,
			expectedMax:  0.5,
			description:  "100ms difference should be poor",
		},
		{
			name:         "very_poor_1s",
			ntpOffset:    1.0,
			kernelOffset: 0.0,
			expectedMin:  0.0,
			expectedMax:  0.1,
			description:  "1s difference should be very poor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coherence := calculateCoherence(tt.ntpOffset, tt.kernelOffset, cfg)

			assert.GreaterOrEqual(t, coherence, tt.expectedMin,
				"%s: coherence %f should be >= %f", tt.description, coherence, tt.expectedMin)
			assert.LessOrEqual(t, coherence, tt.expectedMax,
				"%s: coherence %f should be <= %f", tt.description, coherence, tt.expectedMax)

			t.Logf("%s: coherence = %.3f (offset diff = %.6fs)",
				tt.name, coherence, tt.ntpOffset-tt.kernelOffset)
		})
	}
}

func TestCalculateCoherence_Symmetry(t *testing.T) {
	cfg := &config.Config{}

	// Coherence should be symmetric (same result regardless of which offset is larger)
	coherence1 := calculateCoherence(0.010, 0.005, cfg)
	coherence2 := calculateCoherence(0.005, 0.010, cfg)

	assert.InDelta(t, coherence1, coherence2, 0.001,
		"Coherence should be symmetric")
}

func BenchmarkHybridCollector_CalculateCoherence(b *testing.B) {
	cfg := &config.Config{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = calculateCoherence(0.008, 0.002, cfg)
	}
}

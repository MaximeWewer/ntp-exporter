package ntp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAdaptiveSampler(t *testing.T) {
	client := NewClient(5*time.Second, 4)

	sampler := NewAdaptiveSampler(AdaptiveSamplingConfig{}, client)
	assert.NotNil(t, sampler)
	assert.Equal(t, 3, sampler.config.DefaultSamples)
	assert.Equal(t, 10, sampler.config.HighDriftSamples)
	assert.Equal(t, 50*time.Millisecond, sampler.config.DriftThreshold)
	assert.Equal(t, 30*time.Second, sampler.config.MaxDuration)
	assert.Equal(t, 0.95, sampler.config.ConfidenceLevel)
}

func TestNewAdaptiveSampler_CustomConfig(t *testing.T) {
	client := NewClient(5*time.Second, 4)

	config := AdaptiveSamplingConfig{
		DefaultSamples:   5,
		HighDriftSamples: 20,
		DriftThreshold:   100 * time.Millisecond,
		MaxDuration:      60 * time.Second,
		ConfidenceLevel:  0.99,
	}

	sampler := NewAdaptiveSampler(config, client)
	assert.Equal(t, 5, sampler.config.DefaultSamples)
	assert.Equal(t, 20, sampler.config.HighDriftSamples)
	assert.Equal(t, 100*time.Millisecond, sampler.config.DriftThreshold)
	assert.Equal(t, 60*time.Second, sampler.config.MaxDuration)
	assert.Equal(t, 0.99, sampler.config.ConfidenceLevel)
}

func TestAdaptiveSampler_GetOptimalSampleCount(t *testing.T) {
	client := NewClient(5*time.Second, 4)
	sampler := NewAdaptiveSampler(AdaptiveSamplingConfig{
		DefaultSamples:   3,
		HighDriftSamples: 10,
		DriftThreshold:   50 * time.Millisecond,
	}, client)

	tests := []struct {
		name          string
		offset        time.Duration
		expectedCount int
	}{
		{"low_drift", 10 * time.Millisecond, 3},
		{"medium_drift", 40 * time.Millisecond, 3},
		{"high_drift", 60 * time.Millisecond, 10},
		{"very_high_drift", 200 * time.Millisecond, 10},
		{"negative_high_drift", -100 * time.Millisecond, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := sampler.GetOptimalSampleCount(tt.offset)
			assert.Equal(t, tt.expectedCount, count)
		})
	}
}

func TestAdaptiveSampler_CalculateConfidence(t *testing.T) {
	client := NewClient(5*time.Second, 4)
	sampler := NewAdaptiveSampler(AdaptiveSamplingConfig{
		HighDriftSamples: 10,
	}, client)

	tests := []struct {
		name              string
		stats             *Statistics
		sampleCount       int
		expectedMin       float64
		expectedMax       float64
	}{
		{
			name: "high_confidence",
			stats: &Statistics{
				StdDevOffset:    5 * time.Millisecond,
				Jitter:          10 * time.Millisecond,
				PacketLossRatio: 0.0,
			},
			sampleCount: 10,
			expectedMin: 0.9,
			expectedMax: 1.1, // Can exceed 1.0 due to bonus
		},
		{
			name: "low_confidence_high_stddev",
			stats: &Statistics{
				StdDevOffset:    50 * time.Millisecond,
				Jitter:          10 * time.Millisecond,
				PacketLossRatio: 0.0,
			},
			sampleCount: 3,
			expectedMin: 0.5,
			expectedMax: 0.9,
		},
		{
			name: "low_confidence_high_jitter",
			stats: &Statistics{
				StdDevOffset:    5 * time.Millisecond,
				Jitter:          100 * time.Millisecond,
				PacketLossRatio: 0.0,
			},
			sampleCount: 3,
			expectedMin: 0.7,
			expectedMax: 0.9,
		},
		{
			name: "low_confidence_packet_loss",
			stats: &Statistics{
				StdDevOffset:    5 * time.Millisecond,
				Jitter:          10 * time.Millisecond,
				PacketLossRatio: 0.3,
			},
			sampleCount: 3,
			expectedMin: 0.6,
			expectedMax: 0.8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			confidence := sampler.calculateConfidence(tt.stats, tt.sampleCount)
			assert.GreaterOrEqual(t, confidence, 0.0)
			assert.LessOrEqual(t, confidence, 1.0)
			assert.GreaterOrEqual(t, confidence, tt.expectedMin)
			assert.LessOrEqual(t, confidence, tt.expectedMax)
		})
	}
}

func TestAdaptiveSampler_Sample_MockClient(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("test.ntp.org", 5*time.Millisecond, 2)

	sampler := NewAdaptiveSampler(AdaptiveSamplingConfig{
		DefaultSamples:   3,
		HighDriftSamples: 10,
		DriftThreshold:   50 * time.Millisecond,
	}, mockClient)

	ctx := context.Background()
	responses, err := sampler.Sample(ctx, "test.ntp.org")

	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(responses), 3)
	// Should use default samples for low drift
	assert.LessOrEqual(t, len(responses), 3)
}

func TestAdaptiveSampler_Sample_HighDrift_MockClient(t *testing.T) {
	mockClient := NewMockNTPClient()
	// Setup server with high drift (100ms)
	mockClient.SetupSuccessfulServer("test.ntp.org", 100*time.Millisecond, 2)

	sampler := NewAdaptiveSampler(AdaptiveSamplingConfig{
		DefaultSamples:   3,
		HighDriftSamples: 10,
		DriftThreshold:   50 * time.Millisecond,
		MaxDuration:      30 * time.Second,
	}, mockClient)

	ctx := context.Background()
	responses, err := sampler.Sample(ctx, "test.ntp.org")

	require.NoError(t, err)
	// Should use high drift samples
	assert.GreaterOrEqual(t, len(responses), 10)
}

func TestAdaptiveSampler_Sample_ContextCancellation(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("test.ntp.org", 5*time.Millisecond, 2)

	sampler := NewAdaptiveSampler(AdaptiveSamplingConfig{}, mockClient)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := sampler.Sample(ctx, "test.ntp.org")
	assert.Error(t, err)
}

func TestAdaptiveSampler_SampleMultipleServers(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("server1.ntp.org", 5*time.Millisecond, 2)
	mockClient.SetupSuccessfulServer("server2.ntp.org", 10*time.Millisecond, 2)
	mockClient.SetupSuccessfulServer("server3.ntp.org", 15*time.Millisecond, 2)

	sampler := NewAdaptiveSampler(AdaptiveSamplingConfig{}, mockClient)

	ctx := context.Background()
	servers := []string{"server1.ntp.org", "server2.ntp.org", "server3.ntp.org"}

	results, err := sampler.SampleMultipleServers(ctx, servers)

	require.NoError(t, err)
	assert.Len(t, results, 3)

	for _, server := range servers {
		responses, exists := results[server]
		assert.True(t, exists)
		assert.GreaterOrEqual(t, len(responses), 3)
	}
}

func TestAdaptiveSampler_SampleMultipleServers_PartialFailure(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("good1.ntp.org", 5*time.Millisecond, 2)
	mockClient.SetupSuccessfulServer("good2.ntp.org", 10*time.Millisecond, 2)
	mockClient.SetError("bad.ntp.org", assert.AnError)

	sampler := NewAdaptiveSampler(AdaptiveSamplingConfig{}, mockClient)

	ctx := context.Background()
	servers := []string{"good1.ntp.org", "bad.ntp.org", "good2.ntp.org"}

	results, err := sampler.SampleMultipleServers(ctx, servers)

	require.NoError(t, err)
	// Should have results for 2 out of 3 servers
	assert.Len(t, results, 2)
	assert.Contains(t, results, "good1.ntp.org")
	assert.Contains(t, results, "good2.ntp.org")
	assert.NotContains(t, results, "bad.ntp.org")
}

func TestAdaptiveSampler_MaxDuration(t *testing.T) {
	mockClient := NewMockNTPClient()
	// Setup slow server
	mockClient.SetupHighLatencyServer("slow.ntp.org", 2*time.Second)

	sampler := NewAdaptiveSampler(AdaptiveSamplingConfig{
		DefaultSamples:   3,
		HighDriftSamples: 10,
		DriftThreshold:   50 * time.Millisecond,
		MaxDuration:      3 * time.Second, // Short max duration
	}, mockClient)

	ctx := context.Background()
	start := time.Now()

	responses, err := sampler.Sample(ctx, "slow.ntp.org")

	duration := time.Since(start)

	// Should respect max duration (allow generous margin for slow systems)
	assert.LessOrEqual(t, duration, 10*time.Second) // Allow generous margin
	// May have error or fewer responses due to timeout
	if err == nil {
		assert.LessOrEqual(t, len(responses), 5)
	}
}

func BenchmarkAdaptiveSampler_Sample(b *testing.B) {
	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("test.ntp.org", 5*time.Millisecond, 2)

	sampler := NewAdaptiveSampler(AdaptiveSamplingConfig{}, mockClient)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = sampler.Sample(ctx, "test.ntp.org")
	}
}

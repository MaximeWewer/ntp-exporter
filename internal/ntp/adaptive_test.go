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

func TestAdaptiveSampler_CalculateConfidence(t *testing.T) {
	client := NewClient(5*time.Second, 4)
	sampler := NewAdaptiveSampler(AdaptiveSamplingConfig{
		HighDriftSamples: 10,
	}, client)

	tests := []struct {
		name        string
		stats       *Statistics
		sampleCount int
		expectedMin float64
		expectedMax float64
	}{
		{
			name: "high_confidence",
			stats: &Statistics{
				StdDevOffset:    5 * time.Millisecond,
				StdDevRTT:       10 * time.Millisecond,
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
				StdDevRTT:       10 * time.Millisecond,
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
				StdDevRTT:       100 * time.Millisecond,
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
				StdDevRTT:       10 * time.Millisecond,
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

// fakeQuerier answers every query with a response carrying a fixed offset
type fakeQuerier struct {
	offset time.Duration
	err    error
	calls  int
}

func (f *fakeQuerier) Query(ctx context.Context, server string) (*Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.err != nil {
		return nil, f.err
	}
	f.calls++
	return &Response{Server: server, Stratum: 2, Offset: f.offset, RTT: 10 * time.Millisecond}, nil
}

func (f *fakeQuerier) QueryMultiple(ctx context.Context, server string, count int) ([]*Response, error) {
	var out []*Response
	for i := 0; i < count; i++ {
		r, err := f.Query(ctx, server)
		if err != nil {
			if len(out) == 0 {
				return nil, err
			}
			break
		}
		out = append(out, r)
	}
	return out, nil
}

func TestAdaptiveSampler_LowDriftUsesDefaultSamples(t *testing.T) {
	q := &fakeQuerier{offset: 5 * time.Millisecond}
	sampler := NewAdaptiveSampler(AdaptiveSamplingConfig{DefaultSamples: 3, HighDriftSamples: 10}, q)

	responses, sent, err := sampler.SampleWithCount(context.Background(), "test.ntp.org")

	require.NoError(t, err)
	assert.Len(t, responses, 3)
	assert.Equal(t, 3, sent)
}

func TestAdaptiveSampler_HighDriftAddsSamples(t *testing.T) {
	q := &fakeQuerier{offset: 100 * time.Millisecond}
	sampler := NewAdaptiveSampler(AdaptiveSamplingConfig{DefaultSamples: 3, HighDriftSamples: 10, DriftThreshold: 50 * time.Millisecond}, q)

	responses, sent, err := sampler.SampleWithCount(context.Background(), "test.ntp.org")

	require.NoError(t, err)
	assert.Len(t, responses, 10)
	assert.Equal(t, 10, sent)
}

func TestAdaptiveSampler_ContextCancellation(t *testing.T) {
	sampler := NewAdaptiveSampler(AdaptiveSamplingConfig{}, &fakeQuerier{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := sampler.SampleWithCount(ctx, "test.ntp.org")
	assert.Error(t, err)
}

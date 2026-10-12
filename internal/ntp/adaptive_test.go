package ntp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeQuerier answers every query with a valid response
type fakeQuerier struct {
	offset  time.Duration
	offsets []time.Duration // when set, offsets are returned in sequence
	err     error
	calls   int
}

func (f *fakeQuerier) Query(ctx context.Context, server string) (*Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.err != nil {
		return nil, f.err
	}
	offset := f.offset
	if len(f.offsets) > 0 {
		offset = f.offsets[f.calls%len(f.offsets)]
	}
	f.calls++
	return &Response{Server: server, Stratum: 2, Offset: offset, RTT: 10 * time.Millisecond}, nil
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

// partialQuerier answers only the first `answered` queries of each burst
type partialQuerier struct {
	fakeQuerier
	answered int
}

func (p *partialQuerier) QueryMultiple(ctx context.Context, server string, count int) ([]*Response, error) {
	return p.fakeQuerier.QueryMultiple(ctx, server, min(count, p.answered))
}

func TestNewAdaptiveSampler_Defaults(t *testing.T) {
	sampler := NewAdaptiveSampler(AdaptiveSamplingConfig{}, &fakeQuerier{})

	assert.Equal(t, 4, sampler.config.DefaultSamples)
	assert.Equal(t, 8, sampler.config.MaxSamples)
	assert.Equal(t, 10*time.Millisecond, sampler.config.DispersionThreshold)
	assert.Equal(t, 30*time.Second, sampler.config.MaxDuration)
}

func TestAdaptiveSampler_StableBurstUsesDefaultSamples(t *testing.T) {
	// A large but stable offset is a real offset: no extra sample
	q := &fakeQuerier{offset: 500 * time.Millisecond}
	sampler := NewAdaptiveSampler(AdaptiveSamplingConfig{DefaultSamples: 3, MaxSamples: 10}, q)

	responses, sent, err := sampler.SampleWithCount(context.Background(), "test.ntp.org")

	require.NoError(t, err)
	assert.Len(t, responses, 3)
	assert.Equal(t, 3, sent)
}

func TestAdaptiveSampler_NoisyBurstAddsSamples(t *testing.T) {
	q := &fakeQuerier{offsets: []time.Duration{0, 40 * time.Millisecond, -30 * time.Millisecond}}
	sampler := NewAdaptiveSampler(AdaptiveSamplingConfig{DefaultSamples: 3, MaxSamples: 10, DispersionThreshold: 10 * time.Millisecond}, q)

	responses, sent, err := sampler.SampleWithCount(context.Background(), "test.ntp.org")

	require.NoError(t, err)
	assert.Len(t, responses, 10)
	assert.Equal(t, 10, sent)
}

func TestAdaptiveSampler_LossAddsSamples(t *testing.T) {
	q := &partialQuerier{fakeQuerier: fakeQuerier{offset: time.Millisecond}, answered: 2}
	sampler := NewAdaptiveSampler(AdaptiveSamplingConfig{DefaultSamples: 3, MaxSamples: 6}, q)

	_, sent, err := sampler.SampleWithCount(context.Background(), "test.ntp.org")

	require.NoError(t, err)
	assert.Equal(t, 6, sent)
}

func TestAdaptiveSampler_ContextCancellation(t *testing.T) {
	sampler := NewAdaptiveSampler(AdaptiveSamplingConfig{}, &fakeQuerier{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := sampler.SampleWithCount(ctx, "test.ntp.org")
	assert.Error(t, err)
}

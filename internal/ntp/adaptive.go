package ntp

import (
	"context"
	"time"

	"github.com/maximewewer/ntp-exporter/pkg/logger"
)

// NTPQuerier is an interface for querying NTP servers
type NTPQuerier interface {
	Query(ctx context.Context, server string) (*Response, error)
	QueryMultiple(ctx context.Context, server string, count int) ([]*Response, error)
}

// AdaptiveSamplingConfig configures adaptive sampling behavior
type AdaptiveSamplingConfig struct {
	// DefaultSamples is the size of the initial burst
	DefaultSamples int
	// MaxSamples is the total number of samples when the burst is noisy
	MaxSamples int
	// DispersionThreshold is the offset standard deviation of the initial
	// burst above which more samples are taken
	DispersionThreshold time.Duration
	// MaxDuration bounds the whole sampling of a server
	MaxDuration time.Duration
}

// AdaptiveSampler takes more samples when the initial burst is noisy: the
// lowest-delay sample is then more likely to be close to the true offset.
// A large but stable offset is a real offset: it does not trigger more samples.
type AdaptiveSampler struct {
	config  AdaptiveSamplingConfig
	querier NTPQuerier
}

// NewAdaptiveSampler creates a new adaptive sampler
func NewAdaptiveSampler(config AdaptiveSamplingConfig, querier NTPQuerier) *AdaptiveSampler {
	if config.DefaultSamples == 0 {
		config.DefaultSamples = 4
	}
	if config.MaxSamples == 0 {
		config.MaxSamples = 8
	}
	if config.DispersionThreshold == 0 {
		config.DispersionThreshold = 10 * time.Millisecond
	}
	if config.MaxDuration == 0 {
		config.MaxDuration = 30 * time.Second
	}

	return &AdaptiveSampler{
		config:  config,
		querier: querier,
	}
}

// SampleWithCount performs adaptive sampling and also returns the number of
// queries sent, so that packet loss can be computed on sent packets
func (a *AdaptiveSampler) SampleWithCount(ctx context.Context, server string) ([]*Response, int, error) {
	start := time.Now()

	sent := a.config.DefaultSamples
	responses, err := a.querier.QueryMultiple(ctx, server, sent)
	if err != nil {
		return nil, sent, err
	}

	extra := a.config.MaxSamples - sent
	if extra <= 0 {
		return responses, sent, nil
	}

	stats := CalculateStatistics(ValidResponses(responses), sent)
	noisy := stats.StdDevOffset > a.config.DispersionThreshold || stats.PacketLossRatio > 0
	if !noisy {
		return responses, sent, nil
	}

	remaining := a.config.MaxDuration - time.Since(start)
	if remaining <= 0 {
		return responses, sent, nil
	}

	logger.SafeDebug("adaptive", "Noisy burst, taking more samples", map[string]interface{}{
		"server":      server,
		"stddev":      stats.StdDevOffset.Seconds(),
		"packet_loss": stats.PacketLossRatio,
		"extra":       extra,
	})

	samplingCtx, cancel := context.WithTimeout(ctx, remaining)
	defer cancel()

	more, err := a.querier.QueryMultiple(samplingCtx, server, extra)
	sent += extra
	if err == nil || len(more) > 0 {
		responses = append(responses, more...)
	}

	return responses, sent, nil
}

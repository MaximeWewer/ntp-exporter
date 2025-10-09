package ntp

import (
	"context"
	"time"

	"github.com/maximewewer/ntp-exporter/pkg/logger"
	"github.com/maximewewer/ntp-exporter/pkg/mathutil"
)

// AdaptiveSamplingConfig configures adaptive sampling behavior
type AdaptiveSamplingConfig struct {
	DefaultSamples   int           // Default number of samples (3-5)
	HighDriftSamples int           // Samples when high drift detected (10-20)
	DriftThreshold   time.Duration // Threshold for high drift (50ms)
	MaxDuration      time.Duration // Maximum duration for sampling (30s)
	ConfidenceLevel  float64       // Required confidence level (0.95)
}

// NTPQuerier is an interface for querying NTP servers
type NTPQuerier interface {
	Query(ctx context.Context, server string) (*Response, error)
	QueryMultiple(ctx context.Context, server string, count int) ([]*Response, error)
}

// AdaptiveSampler implements intelligent sampling that adapts to drift conditions
type AdaptiveSampler struct {
	config  AdaptiveSamplingConfig
	querier NTPQuerier
}

// NewAdaptiveSampler creates a new adaptive sampler
func NewAdaptiveSampler(config AdaptiveSamplingConfig, querier NTPQuerier) *AdaptiveSampler {
	// Set defaults if not provided
	if config.DefaultSamples == 0 {
		config.DefaultSamples = 3
	}
	if config.HighDriftSamples == 0 {
		config.HighDriftSamples = 10
	}
	if config.DriftThreshold == 0 {
		config.DriftThreshold = 50 * time.Millisecond
	}
	if config.MaxDuration == 0 {
		config.MaxDuration = 30 * time.Second
	}
	if config.ConfidenceLevel == 0 {
		config.ConfidenceLevel = 0.95
	}

	return &AdaptiveSampler{
		config:  config,
		querier: querier,
	}
}

// Sample performs adaptive sampling from an NTP server
func (a *AdaptiveSampler) Sample(ctx context.Context, server string) ([]*Response, error) {
	start := time.Now()

	// Phase 1: Initial sampling with default samples
	responses, err := a.querier.QueryMultiple(ctx, server, a.config.DefaultSamples)
	if err != nil {
		return nil, err
	}

	if len(responses) == 0 {
		logger.SafeWarn("adaptive", "No responses received from initial sampling", map[string]interface{}{
			"server": server,
		})
		return responses, nil
	}

	// Calculate initial statistics
	stats := CalculateStatistics(responses, a.config.DefaultSamples)

	logger.SafeDebug("adaptive", "Initial sampling completed", map[string]interface{}{
		"server":       server,
		"samples":      len(responses),
		"median_drift": stats.MedianOffset.Seconds(),
		"stddev":       stats.StdDevOffset.Seconds(),
	})

	// Phase 2: Check if high drift detected - need more samples
	absMedian := mathutil.AbsDuration(stats.MedianOffset)
	if absMedian > a.config.DriftThreshold {
		logger.SafeInfo("adaptive", "High drift detected, increasing samples", map[string]interface{}{
			"server":         server,
			"drift":          absMedian.Seconds(),
			"threshold":      a.config.DriftThreshold.Seconds(),
			"extra_samples":  a.config.HighDriftSamples - a.config.DefaultSamples,
		})

		// Check if we have time remaining
		elapsed := time.Since(start)
		if elapsed < a.config.MaxDuration {
			// Take additional samples
			additionalSamples := a.config.HighDriftSamples - a.config.DefaultSamples

			// Create new context with remaining time budget
			remainingTime := a.config.MaxDuration - elapsed
			samplingCtx, cancel := context.WithTimeout(ctx, remainingTime)
			defer cancel()

			extraResponses, err := a.querier.QueryMultiple(samplingCtx, server, additionalSamples)
			if err == nil && len(extraResponses) > 0 {
				// Combine with initial responses
				responses = append(responses, extraResponses...)

				// Recalculate statistics
				stats = CalculateStatistics(responses, len(responses))

				logger.SafeInfo("adaptive", "Additional sampling completed", map[string]interface{}{
					"server":         server,
					"total_samples":  len(responses),
					"new_median":     stats.MedianOffset.Seconds(),
					"new_stddev":     stats.StdDevOffset.Seconds(),
				})
			}
		} else {
			logger.SafeWarn("adaptive", "Max duration reached, skipping additional sampling", map[string]interface{}{
				"server":  server,
				"elapsed": elapsed.Seconds(),
			})
		}
	}

	// Phase 3: Check confidence level
	confidence := a.calculateConfidence(stats, len(responses))
	logger.SafeDebug("adaptive", "Sampling confidence calculated", map[string]interface{}{
		"server":     server,
		"confidence": confidence,
		"required":   a.config.ConfidenceLevel,
		"samples":    len(responses),
	})

	return responses, nil
}

// calculateConfidence calculates confidence score based on statistics
// Returns value between 0 and 1, where 1 is highest confidence
func (a *AdaptiveSampler) calculateConfidence(stats *Statistics, sampleCount int) float64 {
	confidence := 1.0

	// Reduce confidence if stddev is high (unstable)
	// High stddev indicates inconsistent measurements
	if stats.StdDevOffset > 10*time.Millisecond {
		// Reduce confidence by up to 0.3 for very high stddev
		stddevPenalty := mathutil.Min(0.3, stats.StdDevOffset.Seconds()/0.100) // 100ms = max penalty
		confidence -= stddevPenalty
	}

	// Reduce confidence if jitter is high (network instability)
	if stats.Jitter > 20*time.Millisecond {
		jitterPenalty := mathutil.Min(0.2, stats.Jitter.Seconds()/0.200) // 200ms = max penalty
		confidence -= jitterPenalty
	}

	// Reduce confidence if packet loss is high
	if stats.PacketLossRatio > 0.1 { // > 10% loss
		lossPenalty := mathutil.Min(0.3, stats.PacketLossRatio)
		confidence -= lossPenalty
	}

	// Boost confidence with more samples
	if sampleCount >= a.config.HighDriftSamples {
		confidence += 0.1
	}

	// Clamp to [0, 1]
	return mathutil.Clamp(confidence, 0.0, 1.0)
}

// SampleMultipleServers performs adaptive sampling on multiple servers
func (a *AdaptiveSampler) SampleMultipleServers(ctx context.Context, servers []string) (map[string][]*Response, error) {
	results := make(map[string][]*Response)

	for _, server := range servers {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		responses, err := a.Sample(ctx, server)
		if err != nil {
			logger.SafeWarn("adaptive", "Failed to sample server", map[string]interface{}{
				"server": server,
				"error":  err.Error(),
			})
			continue
		}

		results[server] = responses
	}

	return results, nil
}

// GetOptimalSampleCount returns the optimal number of samples based on initial measurement
func (a *AdaptiveSampler) GetOptimalSampleCount(initialOffset time.Duration) int {
	absOffset := mathutil.AbsDuration(initialOffset)

	if absOffset > a.config.DriftThreshold {
		return a.config.HighDriftSamples
	}

	return a.config.DefaultSamples
}

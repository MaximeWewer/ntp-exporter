package probe

import (
	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/ntp"
)

// NewClient creates the NTP client shared by the whole exporter.
// It is wrapped with a circuit breaker when enabled.
func NewClient(cfg *config.Config) ntp.NTPQuerier {
	var baseClient ntp.NTPQuerier

	if cfg.NTP.RateLimit.Enabled {
		baseClient = ntp.NewClientWithRateLimit(
			cfg.NTP.Timeout,
			cfg.NTP.Version,
			cfg.NTP.RateLimit.GlobalRate,
			cfg.NTP.RateLimit.PerServerRate,
			cfg.NTP.RateLimit.BurstSize,
		)
	} else {
		baseClient = ntp.NewClient(cfg.NTP.Timeout, cfg.NTP.Version)
	}

	if cfg.NTP.CircuitBreaker.Enabled {
		cbConfig := ntp.NewCircuitBreakerConfigWithThreshold(
			cfg.NTP.CircuitBreaker.MaxRequests,
			cfg.NTP.CircuitBreaker.Interval,
			cfg.NTP.CircuitBreaker.Timeout,
			cfg.NTP.CircuitBreaker.FailureThreshold,
		)
		return ntp.NewCircuitBreakerClient(baseClient, cbConfig)
	}

	return baseClient
}

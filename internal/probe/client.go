package probe

import (
	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/ntp"
)

// NewClient creates the NTP client shared by the whole exporter.
// Resilience is handled by the prober (Kiss-o'-Death backoff), not by the client.
func NewClient(cfg *config.Config) ntp.NTPQuerier {
	if cfg.NTP.RateLimit.Enabled {
		return ntp.NewClientWithRateLimit(
			cfg.NTP.Timeout,
			cfg.NTP.Version,
			cfg.NTP.RateLimit.GlobalRate,
			cfg.NTP.RateLimit.PerServerRate,
			cfg.NTP.RateLimit.BurstSize,
		)
	}
	return ntp.NewClient(cfg.NTP.Timeout, cfg.NTP.Version)
}

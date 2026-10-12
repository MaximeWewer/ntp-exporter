package probe

import (
	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/ntp"
)

// NewClient creates the NTP client shared by the whole exporter.
// Resilience is handled by the prober (Kiss-o'-Death backoff), not by the client.
func NewClient(cfg *config.Config) ntp.NTPQuerier {
	client := ntp.NewClient(cfg.NTP.Timeout, cfg.NTP.Version)
	client.SetSampleSpacing(cfg.NTP.SampleSpacing)
	return client
}

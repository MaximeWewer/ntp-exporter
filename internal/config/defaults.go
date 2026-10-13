package config

import (
	"time"

	"github.com/maximewewer/ntp-exporter/internal/ntp"
)

// ApplyDefaults sets default values for unspecified configuration fields
func ApplyDefaults(cfg *Config) {
	// Server defaults
	if cfg.Server.Address == "" {
		cfg.Server.Address = "0.0.0.0"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 9559
	}
	if cfg.Server.ReadTimeout == 0 {
		cfg.Server.ReadTimeout = 10 * time.Second
	}
	if cfg.Server.WriteTimeout == 0 {
		cfg.Server.WriteTimeout = 10 * time.Second
	}

	// NTP defaults: no implicit server, the exporter only queries what it is
	// told to
	for i := range cfg.NTP.Pools {
		if cfg.NTP.Pools[i].MaxServers == 0 {
			cfg.NTP.Pools[i].MaxServers = 4
		}
	}
	if cfg.NTP.Timeout == 0 {
		cfg.NTP.Timeout = 5 * time.Second
	}
	if cfg.NTP.Version == 0 {
		cfg.NTP.Version = 4
	}
	if cfg.NTP.SamplesPerServer == 0 {
		cfg.NTP.SamplesPerServer = 4
	}
	if cfg.NTP.Chrony.Address == "" {
		cfg.NTP.Chrony.Address = ntp.DefaultChronyAddress
	}
	if cfg.NTP.IPFamily == "" {
		cfg.NTP.IPFamily = IPFamilyAuto
	}
	if cfg.NTP.SampleSpacing == 0 {
		cfg.NTP.SampleSpacing = 2 * time.Second
	}
	if cfg.NTP.MaxConcurrency == 0 {
		cfg.NTP.MaxConcurrency = 10
	}
	if cfg.NTP.LegacyScrapeInterval != 0 {
		if cfg.NTP.CollectionInterval == 0 {
			cfg.NTP.CollectionInterval = cfg.NTP.LegacyScrapeInterval
		}
		cfg.deprecations = append(cfg.deprecations, "ntp.scrape_interval is deprecated: use ntp.collection_interval")
		cfg.NTP.LegacyScrapeInterval = 0
	}
	if cfg.NTP.CollectionInterval == 0 {
		cfg.NTP.CollectionInterval = 60 * time.Second
	}
	if cfg.NTP.MaxRootDistance == 0 {
		cfg.NTP.MaxRootDistance = DefaultMaxRootDistance
	}

	// Kiss-o'-Death defaults
	if cfg.NTP.KoD.MaxBackoff == 0 {
		cfg.NTP.KoD.MaxBackoff = 1024 * time.Second
	}
	if cfg.NTP.KoD.SuspendDuration == 0 {
		cfg.NTP.KoD.SuspendDuration = 1 * time.Hour
	}

	// DNS cache defaults for pool resolutions
	if cfg.NTP.DNSCache.MinTTL == 0 {
		cfg.NTP.DNSCache.MinTTL = 5 * time.Minute
	}
	if cfg.NTP.DNSCache.MaxTTL == 0 {
		cfg.NTP.DNSCache.MaxTTL = 60 * time.Minute
	}

	// Logging defaults
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Logging.Format == "" {
		cfg.Logging.Format = "json"
	}

	// Metrics defaults
	if cfg.Metrics.Labels == nil {
		cfg.Metrics.Labels = make(map[string]string)
	}
}

// DefaultConfig returns a probe mode configuration with all defaults applied
func DefaultConfig() *Config {
	cfg := &Config{Mode: ModeProbe}
	ApplyDefaults(cfg)
	return cfg
}

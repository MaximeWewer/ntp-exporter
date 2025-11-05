package config

import "time"

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
	// Default CORS origins (empty = no CORS)
	if cfg.Server.AllowedOrigins == nil {
		cfg.Server.AllowedOrigins = []string{}
	}

	// NTP defaults
	if len(cfg.NTP.Servers) == 0 && len(cfg.NTP.Pools) == 0 {
		cfg.NTP.Servers = []string{
			"pool.ntp.org",
			"time.google.com",
		}
	}
	if cfg.NTP.Timeout == 0 {
		cfg.NTP.Timeout = 5 * time.Second
	}
	if cfg.NTP.Version == 0 {
		cfg.NTP.Version = 4
	}
	if cfg.NTP.SamplesPerServer == 0 {
		cfg.NTP.SamplesPerServer = 3
	}
	if cfg.NTP.MaxConcurrency == 0 {
		cfg.NTP.MaxConcurrency = 10
	}
	if cfg.NTP.ScrapeInterval == 0 {
		cfg.NTP.ScrapeInterval = 30 * time.Second
	}
	if cfg.NTP.MaxClockOffset == 0 {
		cfg.NTP.MaxClockOffset = 100 * time.Millisecond
	}

	// Rate limiting defaults
	if cfg.NTP.RateLimit.GlobalRate == 0 {
		cfg.NTP.RateLimit.GlobalRate = 1000
	}
	if cfg.NTP.RateLimit.PerServerRate == 0 {
		cfg.NTP.RateLimit.PerServerRate = 60
	}
	if cfg.NTP.RateLimit.BurstSize == 0 {
		cfg.NTP.RateLimit.BurstSize = 10
	}
	if cfg.NTP.RateLimit.BackoffDuration == 0 {
		cfg.NTP.RateLimit.BackoffDuration = 1 * time.Minute
	}

	// Circuit breaker defaults (enabled by default for fault tolerance)
	cfg.NTP.CircuitBreaker.Enabled = true // Always enabled
	if cfg.NTP.CircuitBreaker.MaxRequests == 0 {
		cfg.NTP.CircuitBreaker.MaxRequests = 3
	}
	if cfg.NTP.CircuitBreaker.Interval == 0 {
		cfg.NTP.CircuitBreaker.Interval = 60 * time.Second
	}
	if cfg.NTP.CircuitBreaker.Timeout == 0 {
		cfg.NTP.CircuitBreaker.Timeout = 30 * time.Second
	}
	if cfg.NTP.CircuitBreaker.FailureThreshold == 0 {
		cfg.NTP.CircuitBreaker.FailureThreshold = 0.6 // 60%
	}

	// Adaptive sampling defaults (disabled by default)
	if cfg.NTP.AdaptiveSampling.DefaultSamples == 0 {
		cfg.NTP.AdaptiveSampling.DefaultSamples = 3
	}
	if cfg.NTP.AdaptiveSampling.HighDriftSamples == 0 {
		cfg.NTP.AdaptiveSampling.HighDriftSamples = 10
	}
	if cfg.NTP.AdaptiveSampling.DriftThreshold == 0 {
		cfg.NTP.AdaptiveSampling.DriftThreshold = 50 * time.Millisecond
	}
	if cfg.NTP.AdaptiveSampling.MaxDuration == 0 {
		cfg.NTP.AdaptiveSampling.MaxDuration = 30 * time.Second
	}

	// Worker pool defaults (disabled by default, uses sequential processing)
	if cfg.NTP.WorkerPool.Size == 0 {
		cfg.NTP.WorkerPool.Size = 5
	}

	// DNS cache defaults (enabled by default for performance)
	cfg.NTP.DNSCache.Enabled = true // Always enabled
	if cfg.NTP.DNSCache.MinTTL == 0 {
		cfg.NTP.DNSCache.MinTTL = 5 * time.Minute
	}
	if cfg.NTP.DNSCache.MaxTTL == 0 {
		cfg.NTP.DNSCache.MaxTTL = 60 * time.Minute
	}
	if cfg.NTP.DNSCache.CleanupWorkers == 0 {
		cfg.NTP.DNSCache.CleanupWorkers = 1
	}

	// Logging defaults
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Logging.Format == "" {
		cfg.Logging.Format = "json"
	}
	if cfg.Logging.Output == "" {
		cfg.Logging.Output = "stdout"
	}

	// Metrics defaults
	if cfg.Metrics.Namespace == "" {
		cfg.Metrics.Namespace = "ntp"
	}
	if cfg.Metrics.Labels == nil {
		cfg.Metrics.Labels = make(map[string]string)
	}
}

// DefaultConfig returns a configuration with all defaults applied
func DefaultConfig() *Config {
	cfg := &Config{}
	ApplyDefaults(cfg)
	return cfg
}

package config

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// Bounds for the collection interval
const (
	MinScrapeInterval = 5 * time.Second
	MaxScrapeInterval = 1 * time.Hour
)

// Validate checks if the configuration is valid
func Validate(cfg *Config) error {
	if err := validateServer(&cfg.Server); err != nil {
		return err
	}

	if err := validateNTP(&cfg.NTP); err != nil {
		return err
	}

	if err := validateLogging(&cfg.Logging); err != nil {
		return err
	}

	if err := validateMetrics(&cfg.Metrics); err != nil {
		return err
	}

	return nil
}

func validateServer(cfg *ServerConfig) error {
	if cfg.Port < 1 || cfg.Port > 65535 {
		return errors.New("port must be between 1 and 65535, got " + strconv.Itoa(cfg.Port))
	}

	if cfg.ReadTimeout < 1*time.Second || cfg.ReadTimeout > 60*time.Second {
		return errors.New("read_timeout must be between 1s and 60s")
	}

	if cfg.WriteTimeout < 1*time.Second || cfg.WriteTimeout > 60*time.Second {
		return errors.New("write_timeout must be between 1s and 60s")
	}

	if cfg.TLSEnabled {
		if cfg.TLSCertFile == "" {
			return errors.New("tls_cert_file is required when tls_enabled is true")
		}
		if cfg.TLSKeyFile == "" {
			return errors.New("tls_key_file is required when tls_enabled is true")
		}
	}

	return nil
}

func validateNTP(cfg *NTPConfig) error {
	if len(cfg.Servers) == 0 && len(cfg.Pools) == 0 {
		return errors.New("at least one NTP server or pool must be configured")
	}

	if cfg.Timeout < 1*time.Second || cfg.Timeout > 60*time.Second {
		return errors.New("timeout must be between 1s and 60s")
	}

	if cfg.Version < 2 || cfg.Version > 4 {
		return errors.New("ntp version must be 2, 3, or 4, got " + strconv.Itoa(cfg.Version))
	}

	if cfg.SamplesPerServer < 1 || cfg.SamplesPerServer > 20 {
		return errors.New("samples_per_server must be between 1 and 20, got " + strconv.Itoa(cfg.SamplesPerServer))
	}

	if cfg.ScrapeInterval < MinScrapeInterval || cfg.ScrapeInterval > MaxScrapeInterval {
		return errors.New("scrape_interval must be between " + MinScrapeInterval.String() + " and " + MaxScrapeInterval.String() + ", got " + cfg.ScrapeInterval.String())
	}

	if cfg.SampleSpacing < 0 {
		return errors.New("sample_spacing must not be negative, got " + cfg.SampleSpacing.String())
	}

	// A burst must fit in a cycle
	samples := cfg.SamplesPerServer
	if cfg.AdaptiveSampling.Enabled && cfg.AdaptiveSampling.HighDriftSamples > samples {
		samples = cfg.AdaptiveSampling.HighDriftSamples
	}
	if time.Duration(samples)*cfg.SampleSpacing >= cfg.ScrapeInterval {
		return errors.New("samples (" + strconv.Itoa(samples) + ") x sample_spacing (" + cfg.SampleSpacing.String() +
			") must be shorter than scrape_interval (" + cfg.ScrapeInterval.String() + ")")
	}

	if cfg.MaxClockOffset <= 0 {
		return errors.New("max_clock_offset must be positive, got " + cfg.MaxClockOffset.String())
	}

	if cfg.KoD.MaxBackoff < cfg.ScrapeInterval {
		return errors.New("kod.max_backoff must be at least scrape_interval, got " + cfg.KoD.MaxBackoff.String())
	}

	if cfg.KoD.SuspendDuration <= 0 {
		return errors.New("kod.suspend_duration must be positive, got " + cfg.KoD.SuspendDuration.String())
	}

	if cfg.MaxConcurrency < 1 || cfg.MaxConcurrency > 100 {
		return errors.New("max_concurrency must be between 1 and 100, got " + strconv.Itoa(cfg.MaxConcurrency))
	}

	// Validate pools
	for i, pool := range cfg.Pools {
		if pool.Name == "" {
			return errors.New("pool[" + strconv.Itoa(i) + "]: name is required")
		}
		if pool.Strategy != "" && pool.Strategy != "best_n" && pool.Strategy != "round_robin" && pool.Strategy != "all" {
			return errors.New("pool[" + strconv.Itoa(i) + "]: invalid strategy (must be best_n, round_robin, or all)")
		}
		if pool.MaxServers < 1 || pool.MaxServers > 20 {
			return errors.New("pool[" + strconv.Itoa(i) + "]: max_servers must be between 1 and 20, got " + strconv.Itoa(pool.MaxServers))
		}
	}

	if cfg.DNSCache.MinTTL <= 0 || cfg.DNSCache.MaxTTL < cfg.DNSCache.MinTTL {
		return errors.New("dns_cache requires 0 < min_ttl <= max_ttl")
	}

	return nil
}

func validateLogging(cfg *LoggingConfig) error {
	validLevels := map[string]bool{
		"trace": true,
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
		"fatal": true,
		"panic": true,
	}

	if !validLevels[cfg.Level] {
		return errors.New("invalid log level (must be trace, debug, info, warn, error, fatal, or panic)")
	}

	validFormats := map[string]bool{
		"json":    true,
		"console": true,
	}

	if !validFormats[cfg.Format] {
		return errors.New("invalid log format (must be json or console)")
	}

	if cfg.EnableFile && cfg.FilePath == "" {
		return errors.New("file_path is required when enable_file is true")
	}

	return nil
}

func validateMetrics(cfg *MetricsConfig) error {
	if cfg.Namespace == "" {
		return errors.New("namespace is required")
	}

	return nil
}

// PublicPoolMinInterval is the shortest collection interval considered
// reasonable for servers of the public NTP pool: one burst per minute, in
// line with the 64s minimum polling interval of NTP clients
const PublicPoolMinInterval = 60 * time.Second

// Warnings returns non-fatal configuration issues, such as polling the
// public NTP pool more often than its usage policy allows
func Warnings(cfg *Config) []string {
	var warnings []string
	if cfg.NTP.EnableKernel {
		warnings = append(warnings, "enable_kernel (NTP_ENABLE_KERNEL) is deprecated: use kernel.enabled (NTP_KERNEL_ENABLED)")
	}
	if cfg.NTP.ScrapeInterval >= PublicPoolMinInterval {
		return warnings
	}

	check := func(host string) {
		h := strings.ToLower(strings.TrimSuffix(host, "."))
		if h == "pool.ntp.org" || strings.HasSuffix(h, ".pool.ntp.org") {
			warnings = append(warnings, host+" is part of the public NTP pool: scrape_interval "+
				cfg.NTP.ScrapeInterval.String()+" is below the recommended "+PublicPoolMinInterval.String())
		}
	}
	for _, server := range cfg.NTP.Servers {
		check(server)
	}
	for _, pool := range cfg.NTP.Pools {
		check(pool.Name)
	}
	return warnings
}

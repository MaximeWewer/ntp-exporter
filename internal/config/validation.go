package config

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultMaxRootDistance is the MAXDIST of ntpd: a server whose root distance
// exceeds it is not selectable
const DefaultMaxRootDistance = 1500 * time.Millisecond

// Bounds for the collection interval
const (
	MinScrapeInterval = 5 * time.Second
	MaxScrapeInterval = 1 * time.Hour
)

// Address families accepted by ip_family
const (
	// IPFamilyAuto queries IPv6 or IPv4, whichever works
	IPFamilyAuto = "auto"
	IPFamilyIPv4 = "ipv4"
	IPFamilyIPv6 = "ipv6"
)

// Validate checks if the configuration is valid
func Validate(cfg *Config) error {
	if err := validateServer(&cfg.Server); err != nil {
		return err
	}

	if err := validateProbeEndpoint(cfg); err != nil {
		return err
	}

	if err := validateNTP(&cfg.NTP, cfg.ProbeEndpoint.Only); err != nil {
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

// validateServerName accepts host, host:port and nts://host[:port]
func validateServerName(server string) error {
	host := strings.TrimPrefix(server, NTSScheme)
	if host == "" || strings.ContainsAny(host, "/ ") {
		return errors.New("invalid server " + strconv.Quote(server) + ": expected host, host:port or nts://host[:port]")
	}
	return nil
}

// serverKey normalizes a server for duplicate detection: host names are case
// insensitive and the default port (123, or 4460 for NTS) is implied
func serverKey(server string) string {
	key := strings.ToLower(server)
	if strings.HasPrefix(key, NTSScheme) {
		return strings.TrimSuffix(key, ":4460")
	}
	if host, port, err := net.SplitHostPort(key); err == nil && port == "123" {
		return host
	}
	return key
}

// NTSScheme prefixes the servers queried with Network Time Security
const NTSScheme = "nts://"

// validateChronyAddress accepts udp://host:port and unix:///path
func validateChronyAddress(address string) error {
	u, err := url.Parse(address)
	if err != nil {
		return errors.New("chrony.address is invalid: " + err.Error())
	}
	switch {
	case u.Scheme == "udp" && u.Port() != "" && u.Hostname() != "":
		return nil
	case u.Scheme == "unix" && u.Path != "":
		return nil
	}
	return errors.New("chrony.address must be udp://host:port or unix:///path/to/chronyd.sock, got " + address)
}

func validateProbeEndpoint(cfg *Config) error {
	if !cfg.ProbeEndpoint.Only {
		return nil
	}
	if !cfg.ProbeEndpoint.IsEnabled() {
		return errors.New("probe_endpoint.only requires probe_endpoint.enabled")
	}
	if len(cfg.NTP.Servers) > 0 || len(cfg.NTP.Pools) > 0 {
		return errors.New("probe_endpoint.only excludes static servers and pools: targets come from /probe requests")
	}
	return nil
}

func validateNTP(cfg *NTPConfig, probeOnly bool) error {
	if len(cfg.Servers) == 0 && len(cfg.Pools) == 0 && !probeOnly {
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
	if time.Duration(samples)*cfg.SampleSpacing >= cfg.ScrapeInterval {
		return errors.New("samples (" + strconv.Itoa(samples) + ") x sample_spacing (" + cfg.SampleSpacing.String() +
			") must be shorter than scrape_interval (" + cfg.ScrapeInterval.String() + ")")
	}

	if cfg.MaxClockOffset <= 0 {
		return errors.New("max_clock_offset must be positive, got " + cfg.MaxClockOffset.String())
	}

	if cfg.MaxRootDistance < 100*time.Millisecond || cfg.MaxRootDistance > 16*time.Second {
		return errors.New("max_root_distance must be between 100ms and 16s, got " + cfg.MaxRootDistance.String())
	}

	if cfg.KoD.MaxBackoff < cfg.ScrapeInterval {
		return errors.New("kod.max_backoff must be at least scrape_interval, got " + cfg.KoD.MaxBackoff.String())
	}

	if cfg.KoD.SuspendDuration <= 0 {
		return errors.New("kod.suspend_duration must be positive, got " + cfg.KoD.SuspendDuration.String())
	}

	if cfg.Chrony.Enabled {
		if err := validateChronyAddress(cfg.Chrony.Address); err != nil {
			return err
		}
	}

	switch cfg.IPFamily {
	case "", IPFamilyAuto, IPFamilyIPv4, IPFamilyIPv6: // empty means auto
	default:
		return errors.New("ip_family must be auto, ipv4 or ipv6, got " + cfg.IPFamily)
	}

	if cfg.MaxConcurrency < 1 || cfg.MaxConcurrency > 100 {
		return errors.New("max_concurrency must be between 1 and 100, got " + strconv.Itoa(cfg.MaxConcurrency))
	}

	// Each server and pool is listed once: duplicates would expose the same
	// series twice and query the server twice per cycle
	servers := make(map[string]bool, len(cfg.Servers))
	for _, server := range cfg.Servers {
		if err := validateServerName(server); err != nil {
			return err
		}
		key := serverKey(server)
		if servers[key] {
			return errors.New("server " + strconv.Quote(server) + " is listed twice")
		}
		servers[key] = true
	}

	// Validate pools
	pools := make(map[string]bool, len(cfg.Pools))
	for i, pool := range cfg.Pools {
		if pool.Name == "" {
			return errors.New("pool[" + strconv.Itoa(i) + "]: name is required")
		}
		key := strings.ToLower(pool.Name)
		if pools[key] {
			return errors.New("pool " + strconv.Quote(pool.Name) + " is listed twice")
		}
		pools[key] = true
		if strings.Contains(pool.Name, "://") {
			return errors.New("pool[" + strconv.Itoa(i) + "]: name must be a DNS name (NTS is only supported for servers)")
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
	if cfg.Server.TLSEnabled {
		warnings = append(warnings, "tls_enabled, tls_cert_file and tls_key_file are deprecated: use web_config_file (--web.config.file)")
	}
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

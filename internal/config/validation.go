package config

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/ntp"
	"github.com/prometheus/common/model"
)

// DefaultMaxRootDistance is the MAXDIST of ntpd: a server whose root distance
// exceeds it is not selectable
const DefaultMaxRootDistance = 1500 * time.Millisecond

// Bounds for the collection interval
const (
	MinCollectionInterval = 5 * time.Second
	MaxCollectionInterval = 1 * time.Hour
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
	if err := validateMode(cfg); err != nil {
		return err
	}

	if err := validateServer(&cfg.Server); err != nil {
		return err
	}

	if err := validateProbeEndpoint(cfg); err != nil {
		return err
	}

	// Probe mode needs targets: servers, pools or /probe requests. An agent
	// may only read the kernel or chronyd.
	if err := validateNTP(&cfg.NTP, cfg.Mode == ModeAgent || cfg.ProbeEndpoint.IsEnabled()); err != nil {
		return err
	}

	if enabled, _ := cfg.ChronyEnabled(); enabled {
		if err := validateChronyAddress(cfg.NTP.Chrony.Address); err != nil {
			return err
		}
	}

	if err := validateLogging(&cfg.Logging); err != nil {
		return err
	}

	if err := validateMetrics(&cfg.Metrics); err != nil {
		return err
	}

	return nil
}

// validateMode requires an explicit mode and applies its guards: the kernel
// and chrony options describe the host the exporter runs on, they only make
// sense in agent mode
func validateMode(cfg *Config) error {
	switch cfg.Mode {
	case ModeProbe:
		if cfg.KernelEnabled() {
			return errors.New("the kernel option describes the clock of the host: it requires mode: agent")
		}
		if enabled, _ := cfg.ChronyEnabled(); enabled {
			return errors.New("the chrony option reads the local chronyd: it requires mode: agent")
		}
		return nil
	case ModeAgent:
		return validateAgent(cfg)
	case "":
		return errors.New("mode is required: set mode: probe (monitor NTP servers from a central place) " +
			"or mode: agent (measure the clock of this host), or NTP_EXPORTER_MODE")
	default:
		return errors.New("mode must be probe or agent, got " + strconv.Quote(cfg.Mode))
	}
}

// MaxAgentServers bounds the reference servers of an agent: every node of a
// fleet queries them, public servers must not receive a burst from each node
const MaxAgentServers = 2

// validateAgent keeps the agent on the clock of its host: a few reference
// servers, no pool, no consensus
func validateAgent(cfg *Config) error {
	if len(cfg.NTP.Servers) > MaxAgentServers {
		return errors.New("mode: agent accepts at most " + strconv.Itoa(MaxAgentServers) +
			" reference servers, preferably inside the site: monitor NTP servers with mode: probe")
	}
	if len(cfg.NTP.Pools) > 0 {
		return errors.New("mode: agent does not query pools: monitor them with mode: probe")
	}
	if cfg.NTP.Consensus.Enabled != nil && *cfg.NTP.Consensus.Enabled {
		return errors.New("mode: agent computes no consensus: it needs 3 servers, monitor them with mode: probe")
	}
	chrony, _ := cfg.ChronyEnabled()
	if len(cfg.NTP.Servers) == 0 && !cfg.KernelEnabled() && !chrony {
		return errors.New("mode: agent has nothing to measure: configure a reference server, the kernel or the chrony option")
	}
	if _, ok := cfg.Metrics.Labels["node"]; ok {
		return errors.New("metrics.labels must not set node in mode: agent: the exporter sets it (NODE_NAME or the hostname)")
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

	return nil
}

// validateServerName accepts host, host:port and nts://host[:port]
func validateServerName(server string) error {
	host := strings.TrimPrefix(server, ntp.NTSScheme)
	if host == "" || strings.ContainsAny(host, "/ ") {
		return errors.New("invalid server " + strconv.Quote(server) + ": expected host, host:port or nts://host[:port]")
	}
	return nil
}

// serverKey normalizes a server for duplicate detection: host names are case
// insensitive and the default port (123, or 4460 for NTS) is implied
func serverKey(server string) string {
	key := strings.ToLower(server)
	if strings.HasPrefix(key, ntp.NTSScheme) {
		return strings.TrimSuffix(key, ":4460")
	}
	if host, port, err := net.SplitHostPort(key); err == nil && port == "123" {
		return host
	}
	return key
}

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
	for _, entry := range cfg.ProbeEndpoint.AllowedTargets {
		entry = strings.TrimSpace(entry)
		if _, err := netip.ParsePrefix(entry); err == nil || entry == "" {
			continue
		}
		if _, err := regexp.Compile(entry); err != nil {
			return errors.New("probe_endpoint.allowed_targets: " + strconv.Quote(entry) +
				" is neither a CIDR range nor a regular expression: " + err.Error())
		}
	}
	return nil
}

func validateNTP(cfg *NTPConfig, noTargetNeeded bool) error {
	if len(cfg.Servers) == 0 && len(cfg.Pools) == 0 && !noTargetNeeded {
		return errors.New("nothing to probe: configure ntp.servers, ntp.pools or probe_endpoint")
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

	if cfg.CollectionInterval < MinCollectionInterval || cfg.CollectionInterval > MaxCollectionInterval {
		return errors.New("collection_interval must be between " + MinCollectionInterval.String() + " and " + MaxCollectionInterval.String() + ", got " + cfg.CollectionInterval.String())
	}

	if cfg.SampleSpacing < 0 {
		return errors.New("sample_spacing must not be negative, got " + cfg.SampleSpacing.String())
	}

	// A burst must fit in a cycle
	samples := cfg.SamplesPerServer
	if time.Duration(samples)*cfg.SampleSpacing >= cfg.CollectionInterval {
		return errors.New("samples (" + strconv.Itoa(samples) + ") x sample_spacing (" + cfg.SampleSpacing.String() +
			") must be shorter than collection_interval (" + cfg.CollectionInterval.String() + ")")
	}

	if cfg.MaxRootDistance < 100*time.Millisecond || cfg.MaxRootDistance > 16*time.Second {
		return errors.New("max_root_distance must be between 100ms and 16s, got " + cfg.MaxRootDistance.String())
	}

	if cfg.KoD.MaxBackoff < cfg.CollectionInterval {
		return errors.New("kod.max_backoff must be at least collection_interval, got " + cfg.KoD.MaxBackoff.String())
	}

	if cfg.KoD.SuspendDuration <= 0 {
		return errors.New("kod.suspend_duration must be positive, got " + cfg.KoD.SuspendDuration.String())
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
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}

	if !validLevels[cfg.Level] {
		return errors.New("invalid log level " + strconv.Quote(cfg.Level) + " (must be debug, info, warn or error)")
	}

	validFormats := map[string]bool{
		"json":    true,
		"console": true,
	}

	if !validFormats[cfg.Format] {
		return errors.New("invalid log format (must be json or console)")
	}

	return nil
}

func validateMetrics(cfg *MetricsConfig) error {
	for name := range cfg.Labels {
		if !model.LegacyValidation.IsValidLabelName(name) {
			return errors.New("metrics.labels: invalid label name " + strconv.Quote(name))
		}
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
	warnings := append([]string(nil), cfg.deprecations...)
	if cfg.NTP.CollectionInterval >= PublicPoolMinInterval {
		return warnings
	}

	check := func(host string) {
		h := strings.ToLower(strings.TrimSuffix(host, "."))
		if h == "pool.ntp.org" || strings.HasSuffix(h, ".pool.ntp.org") {
			warnings = append(warnings, host+" is part of the public NTP pool: collection_interval "+
				cfg.NTP.CollectionInterval.String()+" is below the recommended "+PublicPoolMinInterval.String())
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

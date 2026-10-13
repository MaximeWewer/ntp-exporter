// Package config provides configuration loading with explicit naming
//
// Available functions:
//
//	LoadFromEnvVarsOnly()                     - Environment variables ONLY
//	                                            Use: Docker, Kubernetes (no ConfigMap)
//
//	                                            Use: Local development, testing
//
//	LoadFromYamlWithEnvOverrides(path)        - YAML base + Environment overrides
//	                                            Use: Kubernetes (ConfigMap + env vars)
//	                                            Priority: Env Vars > YAML > Defaults
//
// Environment variables override the YAML file. Every variable is prefixed
// with NTP_EXPORTER_ and named after its YAML key: NTP_EXPORTER_MODE,
// NTP_EXPORTER_SERVERS (comma-separated), NTP_EXPORTER_POOLS (comma-separated
// name[:max_servers]), NTP_EXPORTER_COLLECTION_INTERVAL,
// NTP_EXPORTER_SAMPLES_PER_SERVER, NTP_EXPORTER_LOG_LEVEL,
// NTP_EXPORTER_METRICS_LABELS (comma-separated key=value)... The names used
// before the prefix (legacyEnv) are still accepted for one release, with a
// warning.
package config

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
)

// Deployment modes
const (
	// ModeProbe monitors NTP servers from a central place
	ModeProbe = "probe"
	// ModeAgent measures the clock of the host the exporter runs on
	ModeAgent = "agent"
)

// Config represents the complete application configuration
type Config struct {
	// Mode is probe or agent, required
	Mode string `yaml:"mode"`
	// deprecations lists the legacy names used by the configuration
	deprecations  []string
	Server        ServerConfig        `yaml:"server"`
	ProbeEndpoint ProbeEndpointConfig `yaml:"probe_endpoint"`
	NTP           NTPConfig           `yaml:"ntp"`
	Logging       LoggingConfig       `yaml:"logging"`
	Metrics       MetricsConfig       `yaml:"metrics"`
}

// ProbeEndpointConfig controls the multi-target /probe?target= endpoint, which
// lets Prometheus drive the targets through relabel_configs
type ProbeEndpointConfig struct {
	// Enabled serves the endpoint (default: false). Anyone reaching the
	// exporter can make it send NTP queries to any host: restrict it with
	// AllowedTargets or a network policy.
	Enabled *bool `yaml:"enabled"`
	// AllowedTargets lists the CIDR ranges and host name regular expressions
	// /probe requests may reach; empty means any address but the loopback,
	// link-local, multicast and unspecified ones
	AllowedTargets []string `yaml:"allowed_targets"`
}

// IsEnabled tells whether the /probe endpoint is served
func (c *ProbeEndpointConfig) IsEnabled() bool {
	return c.Enabled != nil && *c.Enabled
}

// ServerConfig contains HTTP server configuration
type ServerConfig struct {
	Address      string        `yaml:"address"`
	Port         int           `yaml:"port"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
	// WebConfigFile is a Prometheus web configuration file (TLS, basic auth)
	WebConfigFile string `yaml:"web_config_file"`
}

// NTPConfig contains NTP client configuration
type NTPConfig struct {
	Servers            []string      `yaml:"servers"`
	Pools              []PoolConfig  `yaml:"pools"`
	Timeout            time.Duration `yaml:"timeout"`
	Version            int           `yaml:"version"`
	SamplesPerServer   int           `yaml:"samples_per_server"`
	SampleSpacing      time.Duration `yaml:"sample_spacing"` // Delay between two samples sent to the same server
	MaxConcurrency     int           `yaml:"max_concurrency"`
	IPFamily           string        `yaml:"ip_family"` // auto, ipv4 or ipv6
	Kernel             KernelConfig  `yaml:"kernel"`
	Chrony             ChronyConfig  `yaml:"chrony"`
	CollectionInterval time.Duration `yaml:"collection_interval"` // Interval between two collection cycles
	// LegacyScrapeInterval is the former name of collection_interval
	LegacyScrapeInterval time.Duration `yaml:"scrape_interval"`
	// MaxRootDistance rejects the responses of servers whose synchronization
	// distance exceeds it (MAXDIST, 1.5s in ntpd)
	MaxRootDistance time.Duration   `yaml:"max_root_distance"`
	KoD             KoDConfig       `yaml:"kod"`
	Consensus       ConsensusConfig `yaml:"consensus"`
	DNSCache        DNSCacheConfig  `yaml:"dns_cache"`
}

// PoolConfig represents NTP pool configuration
type PoolConfig struct {
	Name string `yaml:"name"`
	// MaxServers is the number of addresses queried, the first of each DNS
	// answer
	MaxServers int `yaml:"max_servers"`
}

// KoDConfig contains the Kiss-o'-Death handling configuration (RFC 5905 §7.4)
type KoDConfig struct {
	// MaxBackoff is the longest polling interval reached after repeated RATE codes
	MaxBackoff time.Duration `yaml:"max_backoff"`
	// SuspendDuration is how long a target stays unqueried after DENY or RSTR
	SuspendDuration time.Duration `yaml:"suspend_duration"`
}

// KernelConfig controls the kernel clock state option of the agent mode
type KernelConfig struct {
	// Enabled reads the kernel clock state (read-only adjtimex, Linux only);
	// unset, it is enabled in agent mode on Linux
	Enabled *bool `yaml:"enabled"`
}

// ChronyConfig controls the reading of the local chronyd tracking report
type ChronyConfig struct {
	// Enabled reads the source selected by chronyd and the state of the
	// clock; unset, it is enabled in agent mode when chronyd answers
	Enabled *bool `yaml:"enabled"`
	// Address is udp://host:port (read-only commands are allowed from
	// localhost) or unix:///path/to/chronyd.sock
	Address string `yaml:"address"`
}

// ConsensusConfig controls the multi-server consensus and falseticker detection
type ConsensusConfig struct {
	// Enabled forces the consensus on or off. When unset, it is enabled as soon
	// as at least 3 servers or pools are configured.
	Enabled *bool `yaml:"enabled"`
}

// KernelEnabled tells whether the kernel clock state is read: explicitly
// enabled, or by default in agent mode on Linux
func (c *Config) KernelEnabled() bool {
	if c.NTP.Kernel.Enabled != nil {
		return *c.NTP.Kernel.Enabled
	}
	return c.Mode == ModeAgent && runtime.GOOS == "linux"
}

// ChronyEnabled tells whether the chronyd tracking report is read. auto is
// set when the option is not configured in agent mode: it is then enabled
// only if chronyd answers.
func (c *Config) ChronyEnabled() (enabled, auto bool) {
	if c.NTP.Chrony.Enabled != nil {
		return *c.NTP.Chrony.Enabled, false
	}
	return c.Mode == ModeAgent, c.Mode == ModeAgent
}

// ConsensusEnabled tells whether the consensus of the servers is computed: by
// default in probe mode, as soon as 3 targets (pool members included) answer
func (c *Config) ConsensusEnabled() bool {
	if c.NTP.Consensus.Enabled != nil {
		return *c.NTP.Consensus.Enabled
	}
	return c.Mode == ModeProbe
}

// DNSCacheConfig contains the cache configuration for pool DNS answers
type DNSCacheConfig struct {
	MinTTL time.Duration `yaml:"min_ttl"` // TTL after a resolution failure
	MaxTTL time.Duration `yaml:"max_ttl"` // TTL after consecutive successful resolutions
}

// LoggingConfig contains logging configuration
type LoggingConfig struct {
	// Level is debug, info, warn or error
	Level string `yaml:"level"`
	// Format is json or console; logs go to the standard output
	Format string `yaml:"format"`
}

// MetricsConfig contains Prometheus metrics configuration
type MetricsConfig struct {
	// Labels are constant labels added to every exporter metric
	Labels map[string]string `yaml:"labels"`
}

// LoadFromYamlWithEnvOverrides loads base config from YAML, then overrides with environment variables
// Use case: Kubernetes with ConfigMaps + env vars, Docker with config file + env vars
// Priority: Environment Variables > YAML File > Defaults
// Any error (missing file, invalid YAML, unknown key, invalid env value) is fatal:
// the exporter never silently falls back to default servers.
func LoadFromYamlWithEnvOverrides(path string) (*Config, error) {
	cfg, err := readYamlFile(path)
	if err != nil {
		return nil, err
	}

	if err := applyEnvOverrides(cfg); err != nil {
		return nil, err
	}

	ApplyDefaults(cfg)

	if err := Validate(cfg); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return cfg, nil
}

// LoadFromEnvVarsOnly loads configuration from environment variables only (no YAML file)
// Use case: Docker containers, Kubernetes pods without ConfigMaps
// Priority: Environment Variables > Defaults
func LoadFromEnvVarsOnly() (*Config, error) {
	cfg := &Config{}

	if err := applyEnvOverrides(cfg); err != nil {
		return nil, err
	}

	ApplyDefaults(cfg)

	if err := Validate(cfg); err != nil {
		return nil, fmt.Errorf("environment configuration validation failed: %w", err)
	}

	return cfg, nil
}

// readYamlFile parses a YAML file strictly: unknown keys are rejected
func readYamlFile(path string) (*Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the path is the operator-provided -config flag
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	cfg := &Config{}
	if err := yaml.UnmarshalWithOptions(data, cfg, yaml.DisallowUnknownField()); err != nil {
		return nil, fmt.Errorf("failed to parse YAML config file %s: %w", path, err)
	}

	return cfg, nil
}

// envReader reads typed environment variables and accumulates parse errors
type envReader struct {
	errs []error
	// deprecated lists the legacy variable names that were used
	deprecated []string
}

// legacyEnv maps the variables to their names before the NTP_EXPORTER_
// prefix, still accepted for one release
var legacyEnv = map[string]string{
	"NTP_EXPORTER_READ_TIMEOUT":                   "SERVER_READ_TIMEOUT",
	"NTP_EXPORTER_WRITE_TIMEOUT":                  "SERVER_WRITE_TIMEOUT",
	"NTP_EXPORTER_WEB_CONFIG_FILE":                "WEB_CONFIG_FILE",
	"NTP_EXPORTER_PROBE_ENDPOINT_ENABLED":         "PROBE_ENDPOINT_ENABLED",
	"NTP_EXPORTER_PROBE_ENDPOINT_ALLOWED_TARGETS": "PROBE_ENDPOINT_ALLOWED_TARGETS",
	"NTP_EXPORTER_SERVERS":                        "NTP_SERVERS",
	"NTP_EXPORTER_POOLS":                          "NTP_POOLS",
	"NTP_EXPORTER_KERNEL_ENABLED":                 "NTP_KERNEL_ENABLED",
	"NTP_EXPORTER_CHRONY_ENABLED":                 "NTP_CHRONY_ENABLED",
	"NTP_EXPORTER_CHRONY_ADDRESS":                 "NTP_CHRONY_ADDRESS",
	"NTP_EXPORTER_TIMEOUT":                        "NTP_TIMEOUT",
	"NTP_EXPORTER_NTP_VERSION":                    "NTP_VERSION",
	"NTP_EXPORTER_SAMPLES_PER_SERVER":             "NTP_SAMPLES",
	"NTP_EXPORTER_SAMPLE_SPACING":                 "NTP_SAMPLE_SPACING",
	"NTP_EXPORTER_MAX_CONCURRENCY":                "NTP_MAX_CONCURRENCY",
	"NTP_EXPORTER_IP_FAMILY":                      "NTP_IP_FAMILY",
	"NTP_EXPORTER_COLLECTION_INTERVAL":            "NTP_SCRAPE_INTERVAL",
	"NTP_EXPORTER_MAX_ROOT_DISTANCE":              "NTP_MAX_ROOT_DISTANCE",
	"NTP_EXPORTER_CONSENSUS_ENABLED":              "NTP_CONSENSUS_ENABLED",
	"NTP_EXPORTER_KOD_MAX_BACKOFF":                "NTP_KOD_MAX_BACKOFF",
	"NTP_EXPORTER_KOD_SUSPEND_DURATION":           "NTP_KOD_SUSPEND_DURATION",
	"NTP_EXPORTER_DNS_CACHE_MIN_TTL":              "DNS_CACHE_MIN_TTL",
	"NTP_EXPORTER_DNS_CACHE_MAX_TTL":              "DNS_CACHE_MAX_TTL",
	"NTP_EXPORTER_LOG_LEVEL":                      "LOG_LEVEL",
	"NTP_EXPORTER_LOG_FORMAT":                     "LOG_FORMAT",
	"NTP_EXPORTER_METRICS_LABELS":                 "METRICS_LABELS",
}

// lookup returns the value of a variable, or of its legacy name, and the
// name actually set
func (e *envReader) lookup(name string) (string, string, bool) {
	if v, ok := os.LookupEnv(name); ok && v != "" {
		return v, name, true
	}
	if legacy, ok := legacyEnv[name]; ok {
		if v, ok := os.LookupEnv(legacy); ok && v != "" {
			e.deprecated = append(e.deprecated, legacy+" is deprecated: use "+name)
			return v, legacy, true
		}
	}
	return "", name, false
}

func (e *envReader) fail(name, value string, err error) {
	e.errs = append(e.errs, fmt.Errorf("invalid value %q for %s: %w", value, name, err))
}

func (e *envReader) string(name string, dst *string) {
	if v, _, ok := e.lookup(name); ok {
		*dst = v
	}
}

func (e *envReader) list(name string, dst *[]string) {
	if v, _, ok := e.lookup(name); ok {
		*dst = parseCommaSeparated(v)
	}
}

func (e *envReader) int(name string, dst *int) {
	if v, name, ok := e.lookup(name); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			e.fail(name, v, err)
			return
		}
		*dst = n
	}
}

func (e *envReader) boolPtr(name string, dst **bool) {
	v, used, ok := e.lookup(name)
	if !ok {
		return
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		e.fail(used, v, err)
		return
	}
	*dst = &b
}

func (e *envReader) duration(name string, dst *time.Duration) {
	if v, name, ok := e.lookup(name); ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			e.fail(name, v, err)
			return
		}
		*dst = d
	}
}

func (e *envReader) labels(name string, dst *map[string]string) {
	v, name, ok := e.lookup(name)
	if !ok {
		return
	}
	labels := make(map[string]string)
	for _, item := range parseCommaSeparated(v) {
		key, value, found := strings.Cut(item, "=")
		if !found || strings.TrimSpace(key) == "" {
			e.fail(name, v, fmt.Errorf("label %q must be key=value", item))
			return
		}
		labels[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	*dst = labels
}

func (e *envReader) pools(name string, dst *[]PoolConfig) {
	v, name, ok := e.lookup(name)
	if !ok {
		return
	}
	pools, err := parsePools(v)
	if err != nil {
		e.fail(name, v, err)
		return
	}
	*dst = pools
}

// parsePools parses "name[:max_servers]" entries separated by commas
func parsePools(s string) ([]PoolConfig, error) {
	var pools []PoolConfig
	for _, item := range parseCommaSeparated(s) {
		parts := strings.Split(item, ":")
		if len(parts) > 2 || parts[0] == "" {
			return nil, fmt.Errorf("pool entry %q must be name[:max_servers]", item)
		}
		pool := PoolConfig{Name: parts[0]}
		if len(parts) > 1 {
			n, err := strconv.Atoi(parts[1])
			if err != nil {
				return nil, fmt.Errorf("pool entry %q: invalid max_servers: %w", item, err)
			}
			pool.MaxServers = n
		}
		pools = append(pools, pool)
	}
	return pools, nil
}

// applyEnvOverrides applies environment variable overrides to an existing config.
// Unparsable values are reported as errors instead of being silently ignored.
func applyEnvOverrides(cfg *Config) error {
	e := &envReader{}

	e.string("NTP_EXPORTER_MODE", &cfg.Mode)

	// SERVER
	e.string("NTP_EXPORTER_ADDRESS", &cfg.Server.Address)
	e.int("NTP_EXPORTER_PORT", &cfg.Server.Port)
	e.duration("NTP_EXPORTER_READ_TIMEOUT", &cfg.Server.ReadTimeout)
	e.duration("NTP_EXPORTER_WRITE_TIMEOUT", &cfg.Server.WriteTimeout)
	e.string("NTP_EXPORTER_WEB_CONFIG_FILE", &cfg.Server.WebConfigFile)

	// PROBE ENDPOINT
	e.boolPtr("NTP_EXPORTER_PROBE_ENDPOINT_ENABLED", &cfg.ProbeEndpoint.Enabled)
	e.list("NTP_EXPORTER_PROBE_ENDPOINT_ALLOWED_TARGETS", &cfg.ProbeEndpoint.AllowedTargets)

	// NTP
	e.list("NTP_EXPORTER_SERVERS", &cfg.NTP.Servers)
	e.pools("NTP_EXPORTER_POOLS", &cfg.NTP.Pools)
	e.boolPtr("NTP_EXPORTER_KERNEL_ENABLED", &cfg.NTP.Kernel.Enabled)
	e.boolPtr("NTP_EXPORTER_CHRONY_ENABLED", &cfg.NTP.Chrony.Enabled)
	e.string("NTP_EXPORTER_CHRONY_ADDRESS", &cfg.NTP.Chrony.Address)
	e.duration("NTP_EXPORTER_TIMEOUT", &cfg.NTP.Timeout)
	e.int("NTP_EXPORTER_NTP_VERSION", &cfg.NTP.Version)
	e.int("NTP_EXPORTER_SAMPLES_PER_SERVER", &cfg.NTP.SamplesPerServer)
	e.duration("NTP_EXPORTER_SAMPLE_SPACING", &cfg.NTP.SampleSpacing)
	e.int("NTP_EXPORTER_MAX_CONCURRENCY", &cfg.NTP.MaxConcurrency)
	e.string("NTP_EXPORTER_IP_FAMILY", &cfg.NTP.IPFamily)
	e.duration("NTP_EXPORTER_COLLECTION_INTERVAL", &cfg.NTP.CollectionInterval)
	e.duration("NTP_EXPORTER_MAX_ROOT_DISTANCE", &cfg.NTP.MaxRootDistance)

	// CONSENSUS
	e.boolPtr("NTP_EXPORTER_CONSENSUS_ENABLED", &cfg.NTP.Consensus.Enabled)

	// KISS-O'-DEATH
	e.duration("NTP_EXPORTER_KOD_MAX_BACKOFF", &cfg.NTP.KoD.MaxBackoff)
	e.duration("NTP_EXPORTER_KOD_SUSPEND_DURATION", &cfg.NTP.KoD.SuspendDuration)

	// DNS CACHE
	e.duration("NTP_EXPORTER_DNS_CACHE_MIN_TTL", &cfg.NTP.DNSCache.MinTTL)
	e.duration("NTP_EXPORTER_DNS_CACHE_MAX_TTL", &cfg.NTP.DNSCache.MaxTTL)

	// LOGGING
	e.string("NTP_EXPORTER_LOG_LEVEL", &cfg.Logging.Level)
	e.string("NTP_EXPORTER_LOG_FORMAT", &cfg.Logging.Format)

	// METRICS
	e.labels("NTP_EXPORTER_METRICS_LABELS", &cfg.Metrics.Labels)

	cfg.deprecations = append(cfg.deprecations, e.deprecated...)
	return errors.Join(e.errs...)
}

// parseCommaSeparated splits a comma-separated string, trimming spaces and
// dropping empty items
func parseCommaSeparated(s string) []string {
	var result []string
	for _, item := range strings.Split(s, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

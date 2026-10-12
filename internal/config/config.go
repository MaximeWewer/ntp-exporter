// Package config provides configuration loading with explicit naming
//
// Available functions:
//
//	LoadFromEnvVarsOnly()                     - Environment variables ONLY
//	                                            Use: Docker, Kubernetes (no ConfigMap)
//
//	LoadFromYamlFile(path)                    - YAML file ONLY (no env overrides)
//	                                            Use: Local development, testing
//
//	LoadFromYamlWithEnvOverrides(path)        - YAML base + Environment overrides
//	                                            Use: Kubernetes (ConfigMap + env vars)
//	                                            Priority: Env Vars > YAML > Defaults
//
// Environment variables supported:
//
//	SERVER:
//	  - NTP_EXPORTER_ADDRESS, NTP_EXPORTER_PORT
//	  - SERVER_READ_TIMEOUT, SERVER_WRITE_TIMEOUT
//	  - WEB_CONFIG_FILE (Prometheus web configuration: TLS, basic auth)
//	  - TLS_ENABLED, TLS_CERT_FILE, TLS_KEY_FILE (deprecated)
//
//	NTP:
//	  - NTP_SERVERS (comma-separated), NTP_TIMEOUT, NTP_VERSION
//	  - NTP_POOLS (comma-separated name[:strategy[:max_servers]])
//	  - NTP_SAMPLES, NTP_SAMPLE_SPACING, NTP_MAX_CONCURRENCY
//	  - NTP_IP_FAMILY (auto, ipv4 or ipv6)
//	  - NTP_KERNEL_ENABLED, NTP_KERNEL_VIOLATION_ONLY (NTP_ENABLE_KERNEL is deprecated)
//	  - NTP_CHRONY_ENABLED, NTP_CHRONY_ADDRESS
//	  - NTP_SCRAPE_INTERVAL, NTP_MAX_CLOCK_OFFSET
//
//	PROBE ENDPOINT:
//	  - PROBE_ENDPOINT_ENABLED, PROBE_ENDPOINT_ONLY
//
//	CONSENSUS:
//	  - NTP_CONSENSUS_ENABLED (default: enabled with 3+ servers or pools)
//
//	KISS-O'-DEATH:
//	  - NTP_KOD_MAX_BACKOFF, NTP_KOD_SUSPEND_DURATION
//
//
//	DNS_CACHE:
//	  - DNS_CACHE_MIN_TTL, DNS_CACHE_MAX_TTL
//
//	LOGGING:
//	  - LOG_LEVEL (trace|debug|info|warn|error|fatal|panic)
//	  - LOG_FORMAT (json|console), LOG_ENABLE_FILE, LOG_FILE_PATH
//
//	METRICS:
//	  - METRICS_NAMESPACE, METRICS_SUBSYSTEM
//	  - METRICS_LABELS (comma-separated key=value constant labels)
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
)

// Config represents the complete application configuration
type Config struct {
	Server        ServerConfig        `yaml:"server"`
	ProbeEndpoint ProbeEndpointConfig `yaml:"probe_endpoint"`
	NTP           NTPConfig           `yaml:"ntp"`
	Logging       LoggingConfig       `yaml:"logging"`
	Metrics       MetricsConfig       `yaml:"metrics"`
}

// ProbeEndpointConfig controls the multi-target /probe?target= endpoint, which
// lets Prometheus drive the targets through relabel_configs
type ProbeEndpointConfig struct {
	// Enabled serves the endpoint (default: true)
	Enabled *bool `yaml:"enabled"`
	// Only disables the static targets: no server nor pool is probed in the
	// background, targets only come from /probe requests
	Only bool `yaml:"only"`
}

// IsEnabled tells whether the /probe endpoint is served
func (c *ProbeEndpointConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

// ServerConfig contains HTTP server configuration
type ServerConfig struct {
	Address      string        `yaml:"address"`
	Port         int           `yaml:"port"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
	// WebConfigFile is a Prometheus web configuration file (TLS, basic auth)
	WebConfigFile string `yaml:"web_config_file"`
	// Legacy TLS options, superseded by WebConfigFile
	TLSEnabled  bool   `yaml:"tls_enabled"`
	TLSCertFile string `yaml:"tls_cert_file"`
	TLSKeyFile  string `yaml:"tls_key_file"`
}

// NTPConfig contains NTP client configuration
type NTPConfig struct {
	Servers          []string        `yaml:"servers"`
	Pools            []PoolConfig    `yaml:"pools"`
	Timeout          time.Duration   `yaml:"timeout"`
	Version          int             `yaml:"version"`
	SamplesPerServer int             `yaml:"samples_per_server"`
	SampleSpacing    time.Duration   `yaml:"sample_spacing"` // Delay between two samples sent to the same server
	MaxConcurrency   int             `yaml:"max_concurrency"`
	IPFamily         string          `yaml:"ip_family"`     // auto, ipv4 or ipv6
	EnableKernel     bool            `yaml:"enable_kernel"` // Deprecated: use kernel.enabled
	Kernel           KernelConfig    `yaml:"kernel"`
	Chrony           ChronyConfig    `yaml:"chrony"`
	ScrapeInterval   time.Duration   `yaml:"scrape_interval"`  // Interval between NTP collections
	MaxClockOffset   time.Duration   `yaml:"max_clock_offset"` // Maximum acceptable clock offset threshold
	KoD              KoDConfig       `yaml:"kod"`
	Consensus        ConsensusConfig `yaml:"consensus"`
	DNSCache         DNSCacheConfig  `yaml:"dns_cache"`
}

// PoolConfig represents NTP pool configuration
type PoolConfig struct {
	Name       string `yaml:"name"`
	Strategy   string `yaml:"strategy"`
	MaxServers int    `yaml:"max_servers"`
	Fallback   string `yaml:"fallback"`
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
	// Enabled reads the kernel clock state (read-only adjtimex, Linux only)
	Enabled bool `yaml:"enabled"`
	// ViolationOnly exposes only the error bound violation and the sync status,
	// for nodes where node_exporter already exposes node_timex_* metrics
	ViolationOnly bool `yaml:"violation_only"`
}

// ChronyConfig controls the reading of the local chronyd tracking report
type ChronyConfig struct {
	// Enabled reads the source selected by chronyd and the state of the clock
	Enabled bool `yaml:"enabled"`
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

// ConsensusEnabled tells whether the consensus must be computed
func (c *NTPConfig) ConsensusEnabled() bool {
	if c.Consensus.Enabled != nil {
		return *c.Consensus.Enabled
	}
	return len(c.Servers)+len(c.Pools) >= 3
}

// DNSCacheConfig contains the cache configuration for pool DNS answers
type DNSCacheConfig struct {
	MinTTL time.Duration `yaml:"min_ttl"` // TTL after a resolution failure
	MaxTTL time.Duration `yaml:"max_ttl"` // TTL after consecutive successful resolutions
}

// LoggingConfig contains logging configuration
type LoggingConfig struct {
	Level      string `yaml:"level"`
	Format     string `yaml:"format"`
	Output     string `yaml:"output"`
	EnableFile bool   `yaml:"enable_file"`
	FilePath   string `yaml:"file_path"`
}

// MetricsConfig contains Prometheus metrics configuration
type MetricsConfig struct {
	Namespace string `yaml:"namespace"`
	Subsystem string `yaml:"subsystem"`
	// Labels are constant labels added to every exporter metric
	Labels map[string]string `yaml:"labels"`
}

// LoadFromYamlFile reads configuration from a YAML file only (no env var overrides)
// Use case: Local development, testing
func LoadFromYamlFile(path string) (*Config, error) {
	cfg, err := readYamlFile(path)
	if err != nil {
		return nil, err
	}

	ApplyDefaults(cfg)

	if err := Validate(cfg); err != nil {
		return nil, fmt.Errorf("configuration validation failed for %s: %w", path, err)
	}

	return cfg, nil
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
}

func (e *envReader) fail(name, value string, err error) {
	e.errs = append(e.errs, fmt.Errorf("invalid value %q for %s: %w", value, name, err))
}

func (e *envReader) string(name string, dst *string) {
	if v, ok := os.LookupEnv(name); ok && v != "" {
		*dst = v
	}
}

func (e *envReader) list(name string, dst *[]string) {
	if v, ok := os.LookupEnv(name); ok && v != "" {
		*dst = parseCommaSeparated(v)
	}
}

func (e *envReader) int(name string, dst *int) {
	if v, ok := os.LookupEnv(name); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			e.fail(name, v, err)
			return
		}
		*dst = n
	}
}

func (e *envReader) bool(name string, dst *bool) {
	if v, ok := os.LookupEnv(name); ok && v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			e.fail(name, v, err)
			return
		}
		*dst = b
	}
}

func (e *envReader) boolPtr(name string, dst **bool) {
	var b bool
	if v, ok := os.LookupEnv(name); ok && v != "" {
		e.bool(name, &b)
		*dst = &b
	}
}

func (e *envReader) duration(name string, dst *time.Duration) {
	if v, ok := os.LookupEnv(name); ok && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			e.fail(name, v, err)
			return
		}
		*dst = d
	}
}

func (e *envReader) labels(name string, dst *map[string]string) {
	v, ok := os.LookupEnv(name)
	if !ok || v == "" {
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
	v, ok := os.LookupEnv(name)
	if !ok || v == "" {
		return
	}
	pools, err := parsePools(v)
	if err != nil {
		e.fail(name, v, err)
		return
	}
	*dst = pools
}

// parsePools parses "name[:strategy[:max_servers]]" entries separated by commas
func parsePools(s string) ([]PoolConfig, error) {
	var pools []PoolConfig
	for _, item := range parseCommaSeparated(s) {
		parts := strings.Split(item, ":")
		if len(parts) > 3 || parts[0] == "" {
			return nil, fmt.Errorf("pool entry %q must be name[:strategy[:max_servers]]", item)
		}
		pool := PoolConfig{Name: parts[0]}
		if len(parts) > 1 {
			pool.Strategy = parts[1]
		}
		if len(parts) > 2 {
			n, err := strconv.Atoi(parts[2])
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

	// SERVER
	e.string("NTP_EXPORTER_ADDRESS", &cfg.Server.Address)
	e.int("NTP_EXPORTER_PORT", &cfg.Server.Port)
	e.duration("SERVER_READ_TIMEOUT", &cfg.Server.ReadTimeout)
	e.duration("SERVER_WRITE_TIMEOUT", &cfg.Server.WriteTimeout)
	e.string("WEB_CONFIG_FILE", &cfg.Server.WebConfigFile)
	e.bool("TLS_ENABLED", &cfg.Server.TLSEnabled)
	e.string("TLS_CERT_FILE", &cfg.Server.TLSCertFile)
	e.string("TLS_KEY_FILE", &cfg.Server.TLSKeyFile)

	// PROBE ENDPOINT
	e.boolPtr("PROBE_ENDPOINT_ENABLED", &cfg.ProbeEndpoint.Enabled)
	e.bool("PROBE_ENDPOINT_ONLY", &cfg.ProbeEndpoint.Only)

	// NTP
	e.list("NTP_SERVERS", &cfg.NTP.Servers)
	e.pools("NTP_POOLS", &cfg.NTP.Pools)
	e.bool("NTP_ENABLE_KERNEL", &cfg.NTP.EnableKernel)
	e.bool("NTP_KERNEL_ENABLED", &cfg.NTP.Kernel.Enabled)
	e.bool("NTP_KERNEL_VIOLATION_ONLY", &cfg.NTP.Kernel.ViolationOnly)
	e.bool("NTP_CHRONY_ENABLED", &cfg.NTP.Chrony.Enabled)
	e.string("NTP_CHRONY_ADDRESS", &cfg.NTP.Chrony.Address)
	e.duration("NTP_TIMEOUT", &cfg.NTP.Timeout)
	e.int("NTP_VERSION", &cfg.NTP.Version)
	e.int("NTP_SAMPLES", &cfg.NTP.SamplesPerServer)
	e.duration("NTP_SAMPLE_SPACING", &cfg.NTP.SampleSpacing)
	e.int("NTP_MAX_CONCURRENCY", &cfg.NTP.MaxConcurrency)
	e.string("NTP_IP_FAMILY", &cfg.NTP.IPFamily)
	e.duration("NTP_SCRAPE_INTERVAL", &cfg.NTP.ScrapeInterval)
	e.duration("NTP_MAX_CLOCK_OFFSET", &cfg.NTP.MaxClockOffset)

	// CONSENSUS
	e.boolPtr("NTP_CONSENSUS_ENABLED", &cfg.NTP.Consensus.Enabled)

	// KISS-O'-DEATH
	e.duration("NTP_KOD_MAX_BACKOFF", &cfg.NTP.KoD.MaxBackoff)
	e.duration("NTP_KOD_SUSPEND_DURATION", &cfg.NTP.KoD.SuspendDuration)

	// DNS CACHE
	e.duration("DNS_CACHE_MIN_TTL", &cfg.NTP.DNSCache.MinTTL)
	e.duration("DNS_CACHE_MAX_TTL", &cfg.NTP.DNSCache.MaxTTL)

	// LOGGING
	e.string("LOG_LEVEL", &cfg.Logging.Level)
	e.string("LOG_FORMAT", &cfg.Logging.Format)
	e.bool("LOG_ENABLE_FILE", &cfg.Logging.EnableFile)
	e.string("LOG_FILE_PATH", &cfg.Logging.FilePath)

	// METRICS
	e.string("METRICS_NAMESPACE", &cfg.Metrics.Namespace)
	e.string("METRICS_SUBSYSTEM", &cfg.Metrics.Subsystem)
	e.labels("METRICS_LABELS", &cfg.Metrics.Labels)

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

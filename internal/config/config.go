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
//	  - TLS_ENABLED, TLS_CERT_FILE, TLS_KEY_FILE
//	  - ENABLE_CORS, ALLOWED_ORIGINS (comma-separated)
//
//	NTP:
//	  - NTP_SERVERS (comma-separated), NTP_TIMEOUT, NTP_VERSION
//	  - NTP_POOLS (comma-separated name[:strategy[:max_servers]])
//	  - NTP_SAMPLES, NTP_SAMPLE_SPACING, NTP_MAX_CONCURRENCY, NTP_ENABLE_KERNEL
//	  - NTP_SCRAPE_INTERVAL, NTP_MAX_CLOCK_OFFSET
//
//	KISS-O'-DEATH:
//	  - NTP_KOD_MAX_BACKOFF, NTP_KOD_SUSPEND_DURATION
//
//	RATE_LIMIT:
//	  - RATE_LIMIT_ENABLED, RATE_LIMIT_GLOBAL, RATE_LIMIT_PER_SERVER
//	  - RATE_LIMIT_BURST_SIZE, RATE_LIMIT_BACKOFF_DURATION
//
//	CIRCUIT_BREAKER:
//	  - CIRCUIT_BREAKER_ENABLED, CIRCUIT_BREAKER_MAX_REQUESTS
//	  - CIRCUIT_BREAKER_INTERVAL, CIRCUIT_BREAKER_TIMEOUT
//	  - CIRCUIT_BREAKER_FAILURE_THRESHOLD
//
//	ADAPTIVE_SAMPLING:
//	  - ADAPTIVE_SAMPLING_ENABLED, ADAPTIVE_SAMPLING_DEFAULT_SAMPLES
//	  - ADAPTIVE_SAMPLING_HIGH_DRIFT_SAMPLES, ADAPTIVE_SAMPLING_DRIFT_THRESHOLD
//	  - ADAPTIVE_SAMPLING_MAX_DURATION
//
//	WORKER_POOL:
//	  - WORKER_POOL_ENABLED, WORKER_POOL_SIZE
//
//	DNS_CACHE:
//	  - DNS_CACHE_ENABLED, DNS_CACHE_MIN_TTL, DNS_CACHE_MAX_TTL
//	  - DNS_CACHE_CLEANUP_WORKERS
//
//	LOGGING:
//	  - LOG_LEVEL (trace|debug|info|warn|error|fatal|panic)
//	  - LOG_FORMAT (json|console), LOG_ENABLE_FILE, LOG_FILE_PATH
//
//	METRICS:
//	  - METRICS_NAMESPACE, METRICS_SUBSYSTEM
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
	Server  ServerConfig  `yaml:"server"`
	NTP     NTPConfig     `yaml:"ntp"`
	Logging LoggingConfig `yaml:"logging"`
	Metrics MetricsConfig `yaml:"metrics"`
}

// ServerConfig contains HTTP server configuration
type ServerConfig struct {
	Address        string        `yaml:"address"`
	Port           int           `yaml:"port"`
	ReadTimeout    time.Duration `yaml:"read_timeout"`
	WriteTimeout   time.Duration `yaml:"write_timeout"`
	EnableCORS     bool          `yaml:"enable_cors"`
	AllowedOrigins []string      `yaml:"allowed_origins"`
	TLSEnabled     bool          `yaml:"tls_enabled"`
	TLSCertFile    string        `yaml:"tls_cert_file"`
	TLSKeyFile     string        `yaml:"tls_key_file"`
}

// NTPConfig contains NTP client configuration
type NTPConfig struct {
	Servers          []string               `yaml:"servers"`
	Pools            []PoolConfig           `yaml:"pools"`
	Timeout          time.Duration          `yaml:"timeout"`
	Version          int                    `yaml:"version"`
	SamplesPerServer int                    `yaml:"samples_per_server"`
	SampleSpacing    time.Duration          `yaml:"sample_spacing"` // Delay between two samples sent to the same server
	MaxConcurrency   int                    `yaml:"max_concurrency"`
	EnableKernel     bool                   `yaml:"enable_kernel"`
	ScrapeInterval   time.Duration          `yaml:"scrape_interval"`  // Interval between NTP collections
	MaxClockOffset   time.Duration          `yaml:"max_clock_offset"` // Maximum acceptable clock offset threshold
	RateLimit        RateLimitConfig        `yaml:"rate_limit"`
	CircuitBreaker   CircuitBreakerConfig   `yaml:"circuit_breaker"`
	KoD              KoDConfig              `yaml:"kod"`
	AdaptiveSampling AdaptiveSamplingConfig `yaml:"adaptive_sampling"`
	WorkerPool       WorkerPoolConfig       `yaml:"worker_pool"`
	DNSCache         DNSCacheConfig         `yaml:"dns_cache"`
}

// PoolConfig represents NTP pool configuration
type PoolConfig struct {
	Name       string `yaml:"name"`
	Strategy   string `yaml:"strategy"`
	MaxServers int    `yaml:"max_servers"`
	Fallback   string `yaml:"fallback"`
}

// RateLimitConfig contains rate limiting configuration
type RateLimitConfig struct {
	Enabled         bool          `yaml:"enabled"`
	GlobalRate      int           `yaml:"global_rate"`
	PerServerRate   int           `yaml:"per_server_rate"`
	BurstSize       int           `yaml:"burst_size"`
	BackoffDuration time.Duration `yaml:"backoff_duration"`
}

// KoDConfig contains the Kiss-o'-Death handling configuration (RFC 5905 §7.4)
type KoDConfig struct {
	// MaxBackoff is the longest polling interval reached after repeated RATE codes
	MaxBackoff time.Duration `yaml:"max_backoff"`
	// SuspendDuration is how long a target stays unqueried after DENY or RSTR
	SuspendDuration time.Duration `yaml:"suspend_duration"`
}

// CircuitBreakerConfig contains circuit breaker configuration
type CircuitBreakerConfig struct {
	Enabled          bool          `yaml:"enabled"`
	MaxRequests      uint32        `yaml:"max_requests"`
	Interval         time.Duration `yaml:"interval"`
	Timeout          time.Duration `yaml:"timeout"`
	FailureThreshold float64       `yaml:"failure_threshold"`
}

// AdaptiveSamplingConfig contains adaptive sampling configuration
type AdaptiveSamplingConfig struct {
	Enabled          bool          `yaml:"enabled"`
	DefaultSamples   int           `yaml:"default_samples"`
	HighDriftSamples int           `yaml:"high_drift_samples"`
	DriftThreshold   time.Duration `yaml:"drift_threshold"`
	MaxDuration      time.Duration `yaml:"max_duration"`
}

// WorkerPoolConfig contains worker pool configuration
type WorkerPoolConfig struct {
	Enabled bool `yaml:"enabled"`
	Size    int  `yaml:"size"`
}

// DNSCacheConfig contains DNS cache configuration
type DNSCacheConfig struct {
	Enabled        bool          `yaml:"enabled"`
	MinTTL         time.Duration `yaml:"min_ttl"`
	MaxTTL         time.Duration `yaml:"max_ttl"`
	CleanupWorkers int           `yaml:"cleanup_workers"`
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
	Namespace string            `yaml:"namespace"`
	Subsystem string            `yaml:"subsystem"`
	Labels    map[string]string `yaml:"labels"`
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
	data, err := os.ReadFile(path)
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

func (e *envReader) uint32(name string, dst *uint32) {
	if v, ok := os.LookupEnv(name); ok && v != "" {
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			e.fail(name, v, err)
			return
		}
		*dst = uint32(n)
	}
}

func (e *envReader) float(name string, dst *float64) {
	if v, ok := os.LookupEnv(name); ok && v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			e.fail(name, v, err)
			return
		}
		*dst = f
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
	e.bool("TLS_ENABLED", &cfg.Server.TLSEnabled)
	e.string("TLS_CERT_FILE", &cfg.Server.TLSCertFile)
	e.string("TLS_KEY_FILE", &cfg.Server.TLSKeyFile)
	e.bool("ENABLE_CORS", &cfg.Server.EnableCORS)
	e.list("ALLOWED_ORIGINS", &cfg.Server.AllowedOrigins)

	// NTP
	e.list("NTP_SERVERS", &cfg.NTP.Servers)
	e.pools("NTP_POOLS", &cfg.NTP.Pools)
	e.bool("NTP_ENABLE_KERNEL", &cfg.NTP.EnableKernel)
	e.duration("NTP_TIMEOUT", &cfg.NTP.Timeout)
	e.int("NTP_VERSION", &cfg.NTP.Version)
	e.int("NTP_SAMPLES", &cfg.NTP.SamplesPerServer)
	e.duration("NTP_SAMPLE_SPACING", &cfg.NTP.SampleSpacing)
	e.int("NTP_MAX_CONCURRENCY", &cfg.NTP.MaxConcurrency)
	e.duration("NTP_SCRAPE_INTERVAL", &cfg.NTP.ScrapeInterval)
	e.duration("NTP_MAX_CLOCK_OFFSET", &cfg.NTP.MaxClockOffset)

	// RATE LIMIT
	e.bool("RATE_LIMIT_ENABLED", &cfg.NTP.RateLimit.Enabled)
	e.int("RATE_LIMIT_GLOBAL", &cfg.NTP.RateLimit.GlobalRate)
	e.int("RATE_LIMIT_PER_SERVER", &cfg.NTP.RateLimit.PerServerRate)
	e.int("RATE_LIMIT_BURST_SIZE", &cfg.NTP.RateLimit.BurstSize)
	e.duration("RATE_LIMIT_BACKOFF_DURATION", &cfg.NTP.RateLimit.BackoffDuration)

	// KISS-O'-DEATH
	e.duration("NTP_KOD_MAX_BACKOFF", &cfg.NTP.KoD.MaxBackoff)
	e.duration("NTP_KOD_SUSPEND_DURATION", &cfg.NTP.KoD.SuspendDuration)

	// CIRCUIT BREAKER
	e.bool("CIRCUIT_BREAKER_ENABLED", &cfg.NTP.CircuitBreaker.Enabled)
	e.uint32("CIRCUIT_BREAKER_MAX_REQUESTS", &cfg.NTP.CircuitBreaker.MaxRequests)
	e.duration("CIRCUIT_BREAKER_INTERVAL", &cfg.NTP.CircuitBreaker.Interval)
	e.duration("CIRCUIT_BREAKER_TIMEOUT", &cfg.NTP.CircuitBreaker.Timeout)
	e.float("CIRCUIT_BREAKER_FAILURE_THRESHOLD", &cfg.NTP.CircuitBreaker.FailureThreshold)

	// ADAPTIVE SAMPLING
	e.bool("ADAPTIVE_SAMPLING_ENABLED", &cfg.NTP.AdaptiveSampling.Enabled)
	e.int("ADAPTIVE_SAMPLING_DEFAULT_SAMPLES", &cfg.NTP.AdaptiveSampling.DefaultSamples)
	e.int("ADAPTIVE_SAMPLING_HIGH_DRIFT_SAMPLES", &cfg.NTP.AdaptiveSampling.HighDriftSamples)
	e.duration("ADAPTIVE_SAMPLING_DRIFT_THRESHOLD", &cfg.NTP.AdaptiveSampling.DriftThreshold)
	e.duration("ADAPTIVE_SAMPLING_MAX_DURATION", &cfg.NTP.AdaptiveSampling.MaxDuration)

	// WORKER POOL
	e.bool("WORKER_POOL_ENABLED", &cfg.NTP.WorkerPool.Enabled)
	e.int("WORKER_POOL_SIZE", &cfg.NTP.WorkerPool.Size)

	// DNS CACHE
	e.bool("DNS_CACHE_ENABLED", &cfg.NTP.DNSCache.Enabled)
	e.duration("DNS_CACHE_MIN_TTL", &cfg.NTP.DNSCache.MinTTL)
	e.duration("DNS_CACHE_MAX_TTL", &cfg.NTP.DNSCache.MaxTTL)
	e.int("DNS_CACHE_CLEANUP_WORKERS", &cfg.NTP.DNSCache.CleanupWorkers)

	// LOGGING
	e.string("LOG_LEVEL", &cfg.Logging.Level)
	e.string("LOG_FORMAT", &cfg.Logging.Format)
	e.bool("LOG_ENABLE_FILE", &cfg.Logging.EnableFile)
	e.string("LOG_FILE_PATH", &cfg.Logging.FilePath)

	// METRICS
	e.string("METRICS_NAMESPACE", &cfg.Metrics.Namespace)
	e.string("METRICS_SUBSYSTEM", &cfg.Metrics.Subsystem)

	return errors.Join(e.errs...)
}

// parseCommaSeparated splits a comma-separated string
func parseCommaSeparated(s string) []string {
	var result []string
	for _, item := range splitByComma(s) {
		if trimmed := trim(item); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// splitByComma splits a string by comma delimiters.
// This is a utility function for parsing comma-separated values.
func splitByComma(s string) []string {
	var parts []string
	current := ""
	for _, char := range s {
		if char == ',' {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(char)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

// trim removes leading and trailing whitespace characters from a string.
// Handles spaces, tabs, and newlines.
func trim(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n') {
		end--
	}
	return s[start:end]
}

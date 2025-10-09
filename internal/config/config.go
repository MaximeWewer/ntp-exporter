// Package config provides configuration loading with explicit naming
//
// Available functions:
//
//   LoadFromEnvVarsOnly()                     - Environment variables ONLY
//                                               Use: Docker, Kubernetes (no ConfigMap)
//
//   LoadFromYamlFile(path)                    - YAML file ONLY (no env overrides)
//                                               Use: Local development, testing
//
//   LoadFromYamlWithEnvOverrides(path)        - YAML base + Environment overrides
//                                               Use: Kubernetes (ConfigMap + env vars)
//                                               Priority: Env Vars > YAML > Defaults
//
// Environment variables supported:
//
//   SERVER:
//     - NTP_EXPORTER_ADDRESS, NTP_EXPORTER_PORT
//     - SERVER_READ_TIMEOUT, SERVER_WRITE_TIMEOUT
//     - TLS_ENABLED, TLS_CERT_FILE, TLS_KEY_FILE
//     - ENABLE_CORS, ALLOWED_ORIGINS (comma-separated)
//
//   NTP:
//     - NTP_SERVERS (comma-separated), NTP_TIMEOUT, NTP_VERSION
//     - NTP_SAMPLES, NTP_MAX_CONCURRENCY, NTP_ENABLE_KERNEL
//
//   RATE_LIMIT:
//     - RATE_LIMIT_ENABLED, RATE_LIMIT_GLOBAL, RATE_LIMIT_PER_SERVER
//     - RATE_LIMIT_BURST_SIZE, RATE_LIMIT_BACKOFF_DURATION
//
//   CIRCUIT_BREAKER:
//     - CIRCUIT_BREAKER_ENABLED, CIRCUIT_BREAKER_MAX_REQUESTS
//     - CIRCUIT_BREAKER_INTERVAL, CIRCUIT_BREAKER_TIMEOUT
//     - CIRCUIT_BREAKER_FAILURE_THRESHOLD
//
//   ADAPTIVE_SAMPLING:
//     - ADAPTIVE_SAMPLING_ENABLED, ADAPTIVE_SAMPLING_DEFAULT_SAMPLES
//     - ADAPTIVE_SAMPLING_HIGH_DRIFT_SAMPLES, ADAPTIVE_SAMPLING_DRIFT_THRESHOLD
//     - ADAPTIVE_SAMPLING_MAX_DURATION
//
//   WORKER_POOL:
//     - WORKER_POOL_ENABLED, WORKER_POOL_SIZE
//
//   DNS_CACHE:
//     - DNS_CACHE_ENABLED, DNS_CACHE_MIN_TTL, DNS_CACHE_MAX_TTL
//     - DNS_CACHE_CLEANUP_WORKERS
//
//   LOGGING:
//     - LOG_LEVEL (trace|debug|info|warn|error|fatal|panic)
//     - LOG_ENABLE_FILE, LOG_FILE_PATH
//     - Note: LOG_FORMAT is NOT supported (JSON only)
//
//   METRICS:
//     - METRICS_NAMESPACE, METRICS_SUBSYSTEM
//
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/maximewewer/ntp-exporter/pkg/logger"
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
	MaxConcurrency   int                    `yaml:"max_concurrency"`
	EnableKernel     bool                   `yaml:"enable_kernel"`
	RateLimit        RateLimitConfig        `yaml:"rate_limit"`
	CircuitBreaker   CircuitBreakerConfig   `yaml:"circuit_breaker"`
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
	data, err := os.ReadFile(path)
	if err != nil {
		logger.Error("config", "Failed to read config file", err)
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		logger.Error("config", "Failed to parse config file", err)
		return nil, fmt.Errorf("failed to parse YAML config file %s: %w", path, err)
	}

	// Apply defaults
	ApplyDefaults(cfg)

	// Validate configuration
	if err := Validate(cfg); err != nil {
		logger.Error("config", "Invalid configuration", err)
		return nil, fmt.Errorf("configuration validation failed for %s: %w", path, err)
	}

	return cfg, nil
}

// LoadFromYamlWithEnvOverrides loads base config from YAML, then overrides with environment variables
// Use case: Kubernetes with ConfigMaps + env vars, Docker with config file + env vars
// Priority: Environment Variables > YAML File > Defaults
func LoadFromYamlWithEnvOverrides(path string) (*Config, error) {
	// First, try to load from YAML file
	cfg, err := LoadFromYamlFile(path)
	if err != nil {
		logger.Warn("config", "Failed to load YAML config file, falling back to env vars only")
		// If file doesn't exist, start from defaults
		cfg = &Config{}
		ApplyDefaults(cfg)
	}

	// Override with environment variables
	applyEnvOverrides(cfg)

	// Validate final configuration
	if err := Validate(cfg); err != nil {
		logger.Error("config", "Invalid configuration after env overrides", err)
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return cfg, nil
}

// applyEnvOverrides applies environment variable overrides to an existing config
func applyEnvOverrides(cfg *Config) {
	// ---------------------------------------------------------------------------
	// SERVER - HTTP Server configuration
	// ---------------------------------------------------------------------------
	if addr := os.Getenv("NTP_EXPORTER_ADDRESS"); addr != "" {
		cfg.Server.Address = addr
	}
	if port := os.Getenv("NTP_EXPORTER_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			cfg.Server.Port = p
		}
	}
	if readTimeout := os.Getenv("SERVER_READ_TIMEOUT"); readTimeout != "" {
		if t, err := time.ParseDuration(readTimeout); err == nil {
			cfg.Server.ReadTimeout = t
		}
	}
	if writeTimeout := os.Getenv("SERVER_WRITE_TIMEOUT"); writeTimeout != "" {
		if t, err := time.ParseDuration(writeTimeout); err == nil {
			cfg.Server.WriteTimeout = t
		}
	}
	if tlsEnabled := os.Getenv("TLS_ENABLED"); tlsEnabled != "" {
		if b, err := strconv.ParseBool(tlsEnabled); err == nil {
			cfg.Server.TLSEnabled = b
		}
	}
	if tlsCert := os.Getenv("TLS_CERT_FILE"); tlsCert != "" {
		cfg.Server.TLSCertFile = tlsCert
	}
	if tlsKey := os.Getenv("TLS_KEY_FILE"); tlsKey != "" {
		cfg.Server.TLSKeyFile = tlsKey
	}
	if enableCORS := os.Getenv("ENABLE_CORS"); enableCORS != "" {
		if b, err := strconv.ParseBool(enableCORS); err == nil {
			cfg.Server.EnableCORS = b
		}
	}
	if allowedOrigins := os.Getenv("ALLOWED_ORIGINS"); allowedOrigins != "" {
		cfg.Server.AllowedOrigins = parseCommaSeparated(allowedOrigins)
	}

	// ---------------------------------------------------------------------------
	// NTP - NTP client configuration
	// ---------------------------------------------------------------------------
	if servers := os.Getenv("NTP_SERVERS"); servers != "" {
		cfg.NTP.Servers = parseCommaSeparated(servers)
	}
	if enableKernel := os.Getenv("NTP_ENABLE_KERNEL"); enableKernel != "" {
		if k, err := strconv.ParseBool(enableKernel); err == nil {
			cfg.NTP.EnableKernel = k
		}
	}
	if timeout := os.Getenv("NTP_TIMEOUT"); timeout != "" {
		if t, err := time.ParseDuration(timeout); err == nil {
			cfg.NTP.Timeout = t
		}
	}
	if version := os.Getenv("NTP_VERSION"); version != "" {
		if v, err := strconv.Atoi(version); err == nil {
			cfg.NTP.Version = v
		}
	}
	if samples := os.Getenv("NTP_SAMPLES"); samples != "" {
		if s, err := strconv.Atoi(samples); err == nil {
			cfg.NTP.SamplesPerServer = s
		}
	}
	if maxConcurrency := os.Getenv("NTP_MAX_CONCURRENCY"); maxConcurrency != "" {
		if c, err := strconv.Atoi(maxConcurrency); err == nil {
			cfg.NTP.MaxConcurrency = c
		}
	}

	// ---------------------------------------------------------------------------
	// RATE LIMIT - Rate limiting configuration
	// ---------------------------------------------------------------------------
	if rateLimitEnabled := os.Getenv("RATE_LIMIT_ENABLED"); rateLimitEnabled != "" {
		if b, err := strconv.ParseBool(rateLimitEnabled); err == nil {
			cfg.NTP.RateLimit.Enabled = b
		}
	}
	if globalRate := os.Getenv("RATE_LIMIT_GLOBAL"); globalRate != "" {
		if r, err := strconv.Atoi(globalRate); err == nil {
			cfg.NTP.RateLimit.GlobalRate = r
		}
	}
	if perServerRate := os.Getenv("RATE_LIMIT_PER_SERVER"); perServerRate != "" {
		if r, err := strconv.Atoi(perServerRate); err == nil {
			cfg.NTP.RateLimit.PerServerRate = r
		}
	}
	if burstSize := os.Getenv("RATE_LIMIT_BURST_SIZE"); burstSize != "" {
		if b, err := strconv.Atoi(burstSize); err == nil {
			cfg.NTP.RateLimit.BurstSize = b
		}
	}
	if backoffDuration := os.Getenv("RATE_LIMIT_BACKOFF_DURATION"); backoffDuration != "" {
		if d, err := time.ParseDuration(backoffDuration); err == nil {
			cfg.NTP.RateLimit.BackoffDuration = d
		}
	}

	// ---------------------------------------------------------------------------
	// CIRCUIT BREAKER - Circuit breaker configuration
	// ---------------------------------------------------------------------------
	if cbEnabled := os.Getenv("CIRCUIT_BREAKER_ENABLED"); cbEnabled != "" {
		if b, err := strconv.ParseBool(cbEnabled); err == nil {
			cfg.NTP.CircuitBreaker.Enabled = b
		}
	}
	if maxRequests := os.Getenv("CIRCUIT_BREAKER_MAX_REQUESTS"); maxRequests != "" {
		if r, err := strconv.ParseUint(maxRequests, 10, 32); err == nil {
			cfg.NTP.CircuitBreaker.MaxRequests = uint32(r)
		}
	}
	if cbInterval := os.Getenv("CIRCUIT_BREAKER_INTERVAL"); cbInterval != "" {
		if i, err := time.ParseDuration(cbInterval); err == nil {
			cfg.NTP.CircuitBreaker.Interval = i
		}
	}
	if cbTimeout := os.Getenv("CIRCUIT_BREAKER_TIMEOUT"); cbTimeout != "" {
		if t, err := time.ParseDuration(cbTimeout); err == nil {
			cfg.NTP.CircuitBreaker.Timeout = t
		}
	}
	if failureThreshold := os.Getenv("CIRCUIT_BREAKER_FAILURE_THRESHOLD"); failureThreshold != "" {
		if f, err := strconv.ParseFloat(failureThreshold, 64); err == nil {
			cfg.NTP.CircuitBreaker.FailureThreshold = f
		}
	}

	// ---------------------------------------------------------------------------
	// ADAPTIVE SAMPLING - Adaptive sampling configuration
	// ---------------------------------------------------------------------------
	if asEnabled := os.Getenv("ADAPTIVE_SAMPLING_ENABLED"); asEnabled != "" {
		if b, err := strconv.ParseBool(asEnabled); err == nil {
			cfg.NTP.AdaptiveSampling.Enabled = b
		}
	}
	if defaultSamples := os.Getenv("ADAPTIVE_SAMPLING_DEFAULT_SAMPLES"); defaultSamples != "" {
		if s, err := strconv.Atoi(defaultSamples); err == nil {
			cfg.NTP.AdaptiveSampling.DefaultSamples = s
		}
	}
	if highDriftSamples := os.Getenv("ADAPTIVE_SAMPLING_HIGH_DRIFT_SAMPLES"); highDriftSamples != "" {
		if s, err := strconv.Atoi(highDriftSamples); err == nil {
			cfg.NTP.AdaptiveSampling.HighDriftSamples = s
		}
	}
	if driftThreshold := os.Getenv("ADAPTIVE_SAMPLING_DRIFT_THRESHOLD"); driftThreshold != "" {
		if d, err := time.ParseDuration(driftThreshold); err == nil {
			cfg.NTP.AdaptiveSampling.DriftThreshold = d
		}
	}
	if maxDuration := os.Getenv("ADAPTIVE_SAMPLING_MAX_DURATION"); maxDuration != "" {
		if d, err := time.ParseDuration(maxDuration); err == nil {
			cfg.NTP.AdaptiveSampling.MaxDuration = d
		}
	}

	// ---------------------------------------------------------------------------
	// WORKER POOL - Worker pool configuration
	// ---------------------------------------------------------------------------
	if wpEnabled := os.Getenv("WORKER_POOL_ENABLED"); wpEnabled != "" {
		if b, err := strconv.ParseBool(wpEnabled); err == nil {
			cfg.NTP.WorkerPool.Enabled = b
		}
	}
	if wpSize := os.Getenv("WORKER_POOL_SIZE"); wpSize != "" {
		if s, err := strconv.Atoi(wpSize); err == nil {
			cfg.NTP.WorkerPool.Size = s
		}
	}

	// ---------------------------------------------------------------------------
	// DNS CACHE - DNS cache configuration
	// ---------------------------------------------------------------------------
	if dnsCacheEnabled := os.Getenv("DNS_CACHE_ENABLED"); dnsCacheEnabled != "" {
		if b, err := strconv.ParseBool(dnsCacheEnabled); err == nil {
			cfg.NTP.DNSCache.Enabled = b
		}
	}
	if minTTL := os.Getenv("DNS_CACHE_MIN_TTL"); minTTL != "" {
		if t, err := time.ParseDuration(minTTL); err == nil {
			cfg.NTP.DNSCache.MinTTL = t
		}
	}
	if maxTTL := os.Getenv("DNS_CACHE_MAX_TTL"); maxTTL != "" {
		if t, err := time.ParseDuration(maxTTL); err == nil {
			cfg.NTP.DNSCache.MaxTTL = t
		}
	}
	if cleanupWorkers := os.Getenv("DNS_CACHE_CLEANUP_WORKERS"); cleanupWorkers != "" {
		if w, err := strconv.Atoi(cleanupWorkers); err == nil {
			cfg.NTP.DNSCache.CleanupWorkers = w
		}
	}

	// ---------------------------------------------------------------------------
	// LOGGING - Logging configuration
	// ---------------------------------------------------------------------------
	if level := os.Getenv("LOG_LEVEL"); level != "" {
		cfg.Logging.Level = level
	}
	if enableFile := os.Getenv("LOG_ENABLE_FILE"); enableFile != "" {
		if b, err := strconv.ParseBool(enableFile); err == nil {
			cfg.Logging.EnableFile = b
		}
	}
	if filePath := os.Getenv("LOG_FILE_PATH"); filePath != "" {
		cfg.Logging.FilePath = filePath
	}

	// ---------------------------------------------------------------------------
	// METRICS - Prometheus metrics configuration
	// ---------------------------------------------------------------------------
	if namespace := os.Getenv("METRICS_NAMESPACE"); namespace != "" {
		cfg.Metrics.Namespace = namespace
	}
	if subsystem := os.Getenv("METRICS_SUBSYSTEM"); subsystem != "" {
		cfg.Metrics.Subsystem = subsystem
	}
}

// LoadFromEnvVarsOnly loads configuration from environment variables only (no YAML file)
// Use case: Docker containers, Kubernetes pods without ConfigMaps
// Priority: Environment Variables > Defaults
func LoadFromEnvVarsOnly() (*Config, error) {
	cfg := &Config{}
	ApplyDefaults(cfg)

	// Apply environment variable overrides
	applyEnvOverrides(cfg)

	if err := Validate(cfg); err != nil {
		logger.Error("config", "Invalid configuration from environment", err)
		return nil, fmt.Errorf("environment configuration validation failed: %w", err)
	}

	return cfg, nil
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

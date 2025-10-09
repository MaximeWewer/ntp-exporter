package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestApplyDefaults_EmptyConfig(t *testing.T) {
	cfg := &Config{}

	ApplyDefaults(cfg)

	// Server defaults
	assert.Equal(t, "0.0.0.0", cfg.Server.Address)
	assert.Equal(t, 9559, cfg.Server.Port)
	assert.Equal(t, 10*time.Second, cfg.Server.ReadTimeout)
	assert.Equal(t, 10*time.Second, cfg.Server.WriteTimeout)

	// NTP defaults
	assert.NotEmpty(t, cfg.NTP.Servers)
	assert.Contains(t, cfg.NTP.Servers, "pool.ntp.org")
	assert.Contains(t, cfg.NTP.Servers, "time.google.com")
	assert.Equal(t, 5*time.Second, cfg.NTP.Timeout)
	assert.Equal(t, 4, cfg.NTP.Version)
	assert.Equal(t, 3, cfg.NTP.SamplesPerServer)
	assert.Equal(t, 10, cfg.NTP.MaxConcurrency)

	// Rate limiting defaults
	assert.Equal(t, 1000, cfg.NTP.RateLimit.GlobalRate)
	assert.Equal(t, 60, cfg.NTP.RateLimit.PerServerRate)
	assert.Equal(t, 10, cfg.NTP.RateLimit.BurstSize)
	assert.Equal(t, 1*time.Minute, cfg.NTP.RateLimit.BackoffDuration)

	// Logging defaults
	assert.Equal(t, "info", cfg.Logging.Level)
	assert.Equal(t, "json", cfg.Logging.Format)
	assert.Equal(t, "stdout", cfg.Logging.Output)

	// Metrics defaults
	assert.Equal(t, "ntp", cfg.Metrics.Namespace)
	assert.NotNil(t, cfg.Metrics.Labels)
}

func TestApplyDefaults_PartialConfig(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Address: "192.168.1.1",
			Port:    8080,
		},
		NTP: NTPConfig{
			Servers: []string{"time.nist.gov"},
			Timeout: 10 * time.Second,
		},
	}

	ApplyDefaults(cfg)

	// Should keep existing values
	assert.Equal(t, "192.168.1.1", cfg.Server.Address)
	assert.Equal(t, 8080, cfg.Server.Port)
	assert.Equal(t, 10*time.Second, cfg.NTP.Timeout)
	assert.Contains(t, cfg.NTP.Servers, "time.nist.gov")

	// Should apply missing defaults
	assert.Equal(t, 10*time.Second, cfg.Server.ReadTimeout)
	assert.Equal(t, 10*time.Second, cfg.Server.WriteTimeout)
	assert.Equal(t, 4, cfg.NTP.Version)
	assert.Equal(t, 3, cfg.NTP.SamplesPerServer)
	assert.Equal(t, "info", cfg.Logging.Level)
}

func TestApplyDefaults_WithPools(t *testing.T) {
	cfg := &Config{
		NTP: NTPConfig{
			Pools: []PoolConfig{
				{
					Name:       "pool.ntp.org",
					Strategy:   "best_n",
					MaxServers: 4,
				},
			},
		},
	}

	ApplyDefaults(cfg)

	// Should not add default servers when pools are configured
	assert.Empty(t, cfg.NTP.Servers)
	assert.NotEmpty(t, cfg.NTP.Pools)

	// Should still apply other defaults
	assert.Equal(t, 5*time.Second, cfg.NTP.Timeout)
	assert.Equal(t, 4, cfg.NTP.Version)
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.NotNil(t, cfg)
	assert.Equal(t, "0.0.0.0", cfg.Server.Address)
	assert.Equal(t, 9559, cfg.Server.Port)
	assert.NotEmpty(t, cfg.NTP.Servers)
	assert.Equal(t, "info", cfg.Logging.Level)
	assert.Equal(t, "ntp", cfg.Metrics.Namespace)
}

func TestApplyDefaults_ZeroTimeouts(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			ReadTimeout:  0,
			WriteTimeout: 0,
		},
		NTP: NTPConfig{
			Timeout: 0,
		},
	}

	ApplyDefaults(cfg)

	assert.Equal(t, 10*time.Second, cfg.Server.ReadTimeout)
	assert.Equal(t, 10*time.Second, cfg.Server.WriteTimeout)
	assert.Equal(t, 5*time.Second, cfg.NTP.Timeout)
}

func TestApplyDefaults_ZeroCounters(t *testing.T) {
	cfg := &Config{
		NTP: NTPConfig{
			Version:          0,
			SamplesPerServer: 0,
			MaxConcurrency:   0,
		},
	}

	ApplyDefaults(cfg)

	assert.Equal(t, 4, cfg.NTP.Version)
	assert.Equal(t, 3, cfg.NTP.SamplesPerServer)
	assert.Equal(t, 10, cfg.NTP.MaxConcurrency)
}

func TestApplyDefaults_RateLimitValues(t *testing.T) {
	cfg := &Config{
		NTP: NTPConfig{
			RateLimit: RateLimitConfig{
				GlobalRate:      0,
				PerServerRate:   0,
				BurstSize:       0,
				BackoffDuration: 0,
			},
		},
	}

	ApplyDefaults(cfg)

	assert.Equal(t, 1000, cfg.NTP.RateLimit.GlobalRate)
	assert.Equal(t, 60, cfg.NTP.RateLimit.PerServerRate)
	assert.Equal(t, 10, cfg.NTP.RateLimit.BurstSize)
	assert.Equal(t, 1*time.Minute, cfg.NTP.RateLimit.BackoffDuration)
}

func TestApplyDefaults_LoggingEmptyStrings(t *testing.T) {
	cfg := &Config{
		Logging: LoggingConfig{
			Level:  "",
			Format: "",
			Output: "",
		},
	}

	ApplyDefaults(cfg)

	assert.Equal(t, "info", cfg.Logging.Level)
	assert.Equal(t, "json", cfg.Logging.Format)
	assert.Equal(t, "stdout", cfg.Logging.Output)
}

func TestApplyDefaults_MetricsNilLabels(t *testing.T) {
	cfg := &Config{
		Metrics: MetricsConfig{
			Namespace: "",
			Labels:    nil,
		},
	}

	ApplyDefaults(cfg)

	assert.Equal(t, "ntp", cfg.Metrics.Namespace)
	assert.NotNil(t, cfg.Metrics.Labels)
	assert.Empty(t, cfg.Metrics.Labels)
}

func TestApplyDefaults_Idempotent(t *testing.T) {
	cfg := &Config{}

	ApplyDefaults(cfg)
	firstAddress := cfg.Server.Address
	firstPort := cfg.Server.Port

	ApplyDefaults(cfg)

	// Should not change values on second call
	assert.Equal(t, firstAddress, cfg.Server.Address)
	assert.Equal(t, firstPort, cfg.Server.Port)
}

func BenchmarkApplyDefaults(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cfg := &Config{}
		ApplyDefaults(cfg)
	}
}

func BenchmarkDefaultConfig(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DefaultConfig()
	}
}

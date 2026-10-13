package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

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
	assert.Equal(t, 4, cfg.NTP.SamplesPerServer)
	assert.Equal(t, "info", cfg.Logging.Level)
}

func TestApplyDefaults_WithPools(t *testing.T) {
	cfg := &Config{
		NTP: NTPConfig{
			Pools: []PoolConfig{
				{
					Name:       "pool.ntp.org",
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
	assert.Empty(t, cfg.NTP.Servers, "no implicit server")
	assert.Equal(t, "info", cfg.Logging.Level)
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
	assert.Equal(t, 4, cfg.NTP.SamplesPerServer)
	assert.Equal(t, 10, cfg.NTP.MaxConcurrency)
}

func TestApplyDefaults_LoggingEmptyStrings(t *testing.T) {
	cfg := &Config{
		Logging: LoggingConfig{
			Level:  "",
			Format: "",
		},
	}

	ApplyDefaults(cfg)

	assert.Equal(t, "info", cfg.Logging.Level)
	assert.Equal(t, "json", cfg.Logging.Format)
}

func TestApplyDefaults_MetricsNilLabels(t *testing.T) {
	cfg := &Config{
		Metrics: MetricsConfig{
			Labels: nil,
		},
	}

	ApplyDefaults(cfg)

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

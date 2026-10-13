package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFromYamlFile_Success(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	configContent := `
server:
  address: "127.0.0.1"
  port: 9559
  read_timeout: 10s
  write_timeout: 10s

ntp:
  servers:
    - "pool.ntp.org"
    - "time.google.com"
  timeout: 5s
  version: 4
  samples_per_server: 3

logging:
  level: "info"
  format: "json"

`

	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := LoadFromYamlWithEnvOverrides(configFile)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "127.0.0.1", cfg.Server.Address)
	assert.Equal(t, 9559, cfg.Server.Port)
	assert.Equal(t, 5*time.Second, cfg.NTP.Timeout)
	assert.Equal(t, 4, cfg.NTP.Version)
	assert.Equal(t, "info", cfg.Logging.Level)
}

func TestLoadFromYamlFile_FileNotFound(t *testing.T) {
	cfg, err := LoadFromYamlWithEnvOverrides("/nonexistent/config.yaml")

	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestLoadFromYamlFile_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "bad.yaml")

	// This is truly invalid YAML - unmatched bracket with indentation error
	err := os.WriteFile(configFile, []byte("server:\n  port: [\n    invalid"), 0644)
	require.NoError(t, err)

	cfg, err := LoadFromYamlWithEnvOverrides(configFile)

	assert.Error(t, err)
	assert.Nil(t, cfg)
	if err != nil {
		assert.Contains(t, err.Error(), "failed to parse")
	}
}

func TestLoadFromYamlFile_InvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "invalid.yaml")

	// Config with invalid port
	configContent := `
server:
  port: 99999
`

	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := LoadFromYamlWithEnvOverrides(configFile)

	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "configuration validation failed")
}

func TestLoadFromEnvVarsOnly_Defaults(t *testing.T) {
	// Clear environment
	os.Unsetenv("NTP_EXPORTER_ADDRESS")
	os.Unsetenv("NTP_EXPORTER_PORT")
	os.Unsetenv("NTP_EXPORTER_SERVERS")
	os.Unsetenv("NTP_EXPORTER_LOG_LEVEL")
	t.Setenv("NTP_EXPORTER_SERVERS", "time.example.org")

	cfg, err := LoadFromEnvVarsOnly()

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "0.0.0.0", cfg.Server.Address)
	assert.Equal(t, 9559, cfg.Server.Port)
	assert.Equal(t, "info", cfg.Logging.Level)
	assert.Equal(t, []string{"time.example.org"}, cfg.NTP.Servers)
}

func TestLoadFromEnvVarsOnly_WithOverrides(t *testing.T) {
	// Set environment variables
	os.Setenv("NTP_EXPORTER_ADDRESS", "192.168.1.1")
	os.Setenv("NTP_EXPORTER_PORT", "8080")
	os.Setenv("NTP_EXPORTER_SERVERS", "time.google.com,time.cloudflare.com")
	os.Setenv("NTP_EXPORTER_LOG_LEVEL", "debug")

	defer func() {
		os.Unsetenv("NTP_EXPORTER_ADDRESS")
		os.Unsetenv("NTP_EXPORTER_PORT")
		os.Unsetenv("NTP_EXPORTER_SERVERS")
		os.Unsetenv("NTP_EXPORTER_LOG_LEVEL")
	}()

	cfg, err := LoadFromEnvVarsOnly()

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "192.168.1.1", cfg.Server.Address)
	assert.Equal(t, 8080, cfg.Server.Port)
	assert.Equal(t, "debug", cfg.Logging.Level)
	assert.Contains(t, cfg.NTP.Servers, "time.google.com")
	assert.Contains(t, cfg.NTP.Servers, "time.cloudflare.com")
}

func TestLoadFromEnvVarsOnly_InvalidPort(t *testing.T) {
	os.Setenv("NTP_EXPORTER_PORT", "99999")
	defer os.Unsetenv("NTP_EXPORTER_PORT")

	cfg, err := LoadFromEnvVarsOnly()

	// Should return validation error for invalid port
	assert.Error(t, err)
	assert.Nil(t, cfg)
}

func TestParseCommaSeparated(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "single_server",
			input:    "pool.ntp.org",
			expected: []string{"pool.ntp.org"},
		},
		{
			name:     "multiple_servers",
			input:    "pool.ntp.org,time.google.com,time.cloudflare.com",
			expected: []string{"pool.ntp.org", "time.google.com", "time.cloudflare.com"},
		},
		{
			name:     "servers_with_spaces",
			input:    "pool.ntp.org , time.google.com , time.cloudflare.com",
			expected: []string{"pool.ntp.org", "time.google.com", "time.cloudflare.com"},
		},
		{
			name:     "empty_string",
			input:    "",
			expected: nil,
		},
		{
			name:     "whitespace_only",
			input:    "   ,   ,   ",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseCommaSeparated(tt.input)
			if tt.expected == nil && result == nil {
				return
			}
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLoadFromEnvVarsOnly_WithServersWithSpaces(t *testing.T) {
	os.Setenv("NTP_EXPORTER_SERVERS", " pool.ntp.org , time.google.com , time.cloudflare.com ")
	defer os.Unsetenv("NTP_EXPORTER_SERVERS")

	cfg, err := LoadFromEnvVarsOnly()

	require.NoError(t, err)
	assert.Len(t, cfg.NTP.Servers, 3)
	assert.Equal(t, "pool.ntp.org", cfg.NTP.Servers[0])
	assert.Equal(t, "time.google.com", cfg.NTP.Servers[1])
	assert.Equal(t, "time.cloudflare.com", cfg.NTP.Servers[2])
}

func TestLoadFromYamlWithEnvOverrides_Success(t *testing.T) {
	t.Setenv("NTP_EXPORTER_MODE", "agent")
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	configContent := `
server:
  address: "127.0.0.1"
  port: 9559
ntp:
  servers:
    - "pool.ntp.org"
  timeout: 5s
logging:
  level: "info"
`

	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	// Set environment overrides
	os.Setenv("NTP_EXPORTER_PORT", "8080")
	os.Setenv("NTP_EXPORTER_KERNEL_ENABLED", "true")
	os.Setenv("NTP_EXPORTER_LOG_LEVEL", "debug")

	defer func() {
		os.Unsetenv("NTP_EXPORTER_PORT")
		os.Unsetenv("NTP_EXPORTER_KERNEL_ENABLED")
		os.Unsetenv("NTP_EXPORTER_LOG_LEVEL")
	}()

	cfg, err := LoadFromYamlWithEnvOverrides(configFile)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	// YAML values
	assert.Equal(t, "127.0.0.1", cfg.Server.Address)
	assert.Contains(t, cfg.NTP.Servers, "pool.ntp.org")
	// Environment overrides
	assert.Equal(t, 8080, cfg.Server.Port)
	assert.True(t, cfg.KernelEnabled())
	assert.Equal(t, "debug", cfg.Logging.Level)
}

func BenchmarkLoadFromYamlFile(b *testing.B) {
	tmpDir := b.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	configContent := `
server:
  port: 9559
ntp:
  servers: ["pool.ntp.org"]
  timeout: 5s
  version: 4
logging:
  level: "info"
`

	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = LoadFromYamlWithEnvOverrides(configFile)
	}
}

func BenchmarkLoadFromEnvVarsOnly(b *testing.B) {
	os.Setenv("NTP_EXPORTER_SERVERS", "pool.ntp.org,time.google.com")
	defer os.Unsetenv("NTP_EXPORTER_SERVERS")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = LoadFromEnvVarsOnly()
	}
}

func TestLoadFromYamlWithEnvOverrides_MissingFileIsFatal(t *testing.T) {
	cfg, err := LoadFromYamlWithEnvOverrides(filepath.Join(t.TempDir(), "missing.yaml"))
	assert.Error(t, err)
	assert.Nil(t, cfg)
}

func TestLoadFromYamlWithEnvOverrides_InvalidYAMLIsFatal(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configFile, []byte("ntp: [unclosed"), 0o644))

	cfg, err := LoadFromYamlWithEnvOverrides(configFile)
	assert.Error(t, err)
	assert.Nil(t, cfg)
}

func TestLoadFromYamlFile_UnknownKeyRejected(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configFile, []byte("ntp:\n  servers: [time.google.com]\n  scrape_intervall: 30s\n"), 0o644))

	cfg, err := LoadFromYamlWithEnvOverrides(configFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scrape_intervall")
	assert.Nil(t, cfg)
}

func TestLoadFromEnvVarsOnly_InvalidValueIsFatal(t *testing.T) {
	t.Setenv("NTP_EXPORTER_TIMEOUT", "abc")

	cfg, err := LoadFromEnvVarsOnly()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NTP_EXPORTER_TIMEOUT")
	assert.Nil(t, cfg)
}

func TestLoadFromEnvVarsOnly_Pools(t *testing.T) {
	t.Setenv("NTP_EXPORTER_POOLS", "pool.ntp.org, europe.pool.ntp.org:3")

	cfg, err := LoadFromEnvVarsOnly()
	require.NoError(t, err)
	require.Len(t, cfg.NTP.Pools, 2)
	assert.Equal(t, PoolConfig{Name: "pool.ntp.org", MaxServers: 4}, cfg.NTP.Pools[0])
	assert.Equal(t, PoolConfig{Name: "europe.pool.ntp.org", MaxServers: 3}, cfg.NTP.Pools[1])
	assert.Empty(t, cfg.NTP.Servers, "default servers must not be added when pools are configured")
}

func TestLoadFromEnvVarsOnly_InvalidPools(t *testing.T) {
	t.Setenv("NTP_EXPORTER_POOLS", "pool.ntp.org:all:x")

	_, err := LoadFromEnvVarsOnly()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NTP_EXPORTER_POOLS")
}

func TestLoadFromYamlFile_PoolDefaults(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configFile, []byte("ntp:\n  pools:\n    - name: pool.ntp.org\n"), 0o644))

	cfg, err := LoadFromYamlWithEnvOverrides(configFile)
	require.NoError(t, err)
	assert.Equal(t, 4, cfg.NTP.Pools[0].MaxServers)
}

func TestConsensusEnabled(t *testing.T) {
	cfg := DefaultConfig()
	assert.True(t, cfg.ConsensusEnabled(), "probe mode: computed as soon as 3 targets answer")

	cfg.Mode = ModeAgent
	assert.False(t, cfg.ConsensusEnabled(), "agent mode")

	t.Setenv("NTP_EXPORTER_SERVERS", "a.example.org")
	t.Setenv("NTP_EXPORTER_CONSENSUS_ENABLED", "false")
	envCfg, err := LoadFromEnvVarsOnly()
	require.NoError(t, err)
	require.NotNil(t, envCfg.NTP.Consensus.Enabled)
	assert.False(t, envCfg.ConsensusEnabled())
}

func TestLoadFromEnvVarsOnly_MetricsLabels(t *testing.T) {
	t.Setenv("NTP_EXPORTER_SERVERS", "time.example.org")
	t.Setenv("NTP_EXPORTER_METRICS_LABELS", "site=par1, env = prod")
	cfg, err := LoadFromEnvVarsOnly()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"site": "par1", "env": "prod"}, cfg.Metrics.Labels)

	t.Setenv("NTP_EXPORTER_METRICS_LABELS", "broken")
	_, err = LoadFromEnvVarsOnly()
	assert.ErrorContains(t, err, "NTP_EXPORTER_METRICS_LABELS")
}

func TestValidate_DNSCacheTTL(t *testing.T) {
	cfg := DefaultConfig()
	cfg.NTP.DNSCache.MaxTTL = time.Minute
	cfg.NTP.DNSCache.MinTTL = time.Hour
	assert.Error(t, Validate(cfg))
}

func TestLoadFromEnvVarsOnly_IPFamily(t *testing.T) {
	t.Setenv("NTP_EXPORTER_SERVERS", "time.example.org")
	cfg, err := LoadFromEnvVarsOnly()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NTP.IPFamily != IPFamilyAuto {
		t.Errorf("default ip_family must be auto, got %q", cfg.NTP.IPFamily)
	}

	t.Setenv("NTP_EXPORTER_IP_FAMILY", "ipv6")
	cfg, err = LoadFromEnvVarsOnly()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NTP.IPFamily != IPFamilyIPv6 {
		t.Errorf("NTP_EXPORTER_IP_FAMILY not applied, got %q", cfg.NTP.IPFamily)
	}

	t.Setenv("NTP_EXPORTER_IP_FAMILY", "both")
	if _, err := LoadFromEnvVarsOnly(); err == nil {
		t.Error("invalid NTP_EXPORTER_IP_FAMILY must fail")
	}
}

func TestLoadFromEnvVarsOnly_NoImplicitServer(t *testing.T) {
	_, err := LoadFromEnvVarsOnly()
	assert.ErrorContains(t, err, "nothing to probe", "no server is queried unless configured")

	t.Setenv("NTP_EXPORTER_PROBE_ENDPOINT_ENABLED", "true")
	cfg, err := LoadFromEnvVarsOnly()
	require.NoError(t, err)
	assert.Empty(t, cfg.NTP.Servers, "only /probe requests")
}

func TestLoadFromEnvVarsOnly_Chrony(t *testing.T) {
	t.Setenv("NTP_EXPORTER_MODE", "agent")
	t.Setenv("NTP_EXPORTER_CHRONY_ENABLED", "true")
	cfg, err := LoadFromEnvVarsOnly()
	if err != nil {
		t.Fatal(err)
	}
	if enabled, auto := cfg.ChronyEnabled(); !enabled || auto || cfg.NTP.Chrony.Address != "udp://127.0.0.1:323" {
		t.Errorf("unexpected chrony config %+v", cfg.NTP.Chrony)
	}

	t.Setenv("NTP_EXPORTER_CHRONY_ADDRESS", "unix:///run/chrony/chronyd.sock")
	cfg, err = LoadFromEnvVarsOnly()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NTP.Chrony.Address != "unix:///run/chrony/chronyd.sock" {
		t.Errorf("NTP_EXPORTER_CHRONY_ADDRESS not applied, got %q", cfg.NTP.Chrony.Address)
	}
}

func TestLoad_LegacyNamesAreAcceptedWithAWarning(t *testing.T) {
	t.Setenv("NTP_SERVERS", "a.example.org")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("NTP_EXPORTER_TIMEOUT", "3s")
	t.Setenv("NTP_TIMEOUT", "9s")

	cfg, err := LoadFromEnvVarsOnly()
	require.NoError(t, err)
	assert.Equal(t, []string{"a.example.org"}, cfg.NTP.Servers)
	assert.Equal(t, "debug", cfg.Logging.Level)
	assert.Equal(t, 3*time.Second, cfg.NTP.Timeout, "the prefixed name wins")
	warnings := Warnings(cfg)
	assert.Contains(t, warnings, "NTP_SERVERS is deprecated: use NTP_EXPORTER_SERVERS")
	assert.Contains(t, warnings, "LOG_LEVEL is deprecated: use NTP_EXPORTER_LOG_LEVEL")
	assert.NotContains(t, strings.Join(warnings, "\n"), "NTP_TIMEOUT")

	_, err = func() (*Config, error) {
		t.Setenv("NTP_SAMPLES", "x")
		return LoadFromEnvVarsOnly()
	}()
	assert.ErrorContains(t, err, "NTP_SAMPLES", "errors name the variable actually set")
}

func TestLoad_LegacyScrapeInterval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("mode: probe\nntp:\n  servers: [a.example.org]\n  scrape_interval: 2m\n"), 0o600))

	cfg, err := LoadFromYamlWithEnvOverrides(path)
	require.NoError(t, err)
	assert.Equal(t, 2*time.Minute, cfg.NTP.CollectionInterval)
	assert.Contains(t, Warnings(cfg), "ntp.scrape_interval is deprecated: use ntp.collection_interval")
}

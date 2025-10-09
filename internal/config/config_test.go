package config

import (
	"os"
	"path/filepath"
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

metrics:
  namespace: "ntp"
`

	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := LoadFromYamlFile(configFile)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "127.0.0.1", cfg.Server.Address)
	assert.Equal(t, 9559, cfg.Server.Port)
	assert.Equal(t, 5*time.Second, cfg.NTP.Timeout)
	assert.Equal(t, 4, cfg.NTP.Version)
	assert.Equal(t, "info", cfg.Logging.Level)
	assert.Equal(t, "ntp", cfg.Metrics.Namespace)
}

func TestLoadFromYamlFile_FileNotFound(t *testing.T) {
	cfg, err := LoadFromYamlFile("/nonexistent/config.yaml")

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

	cfg, err := LoadFromYamlFile(configFile)

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

	cfg, err := LoadFromYamlFile(configFile)

	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "configuration validation failed")
}

func TestLoadFromEnvVarsOnly_Defaults(t *testing.T) {
	// Clear environment
	os.Unsetenv("NTP_EXPORTER_ADDRESS")
	os.Unsetenv("NTP_EXPORTER_PORT")
	os.Unsetenv("NTP_SERVERS")
	os.Unsetenv("LOG_LEVEL")

	cfg, err := LoadFromEnvVarsOnly()

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "0.0.0.0", cfg.Server.Address)
	assert.Equal(t, 9559, cfg.Server.Port)
	assert.Equal(t, "info", cfg.Logging.Level)
	assert.NotEmpty(t, cfg.NTP.Servers)
}

func TestLoadFromEnvVarsOnly_WithOverrides(t *testing.T) {
	// Set environment variables
	os.Setenv("NTP_EXPORTER_ADDRESS", "192.168.1.1")
	os.Setenv("NTP_EXPORTER_PORT", "8080")
	os.Setenv("NTP_SERVERS", "time.google.com,time.cloudflare.com")
	os.Setenv("LOG_LEVEL", "debug")

	defer func() {
		os.Unsetenv("NTP_EXPORTER_ADDRESS")
		os.Unsetenv("NTP_EXPORTER_PORT")
		os.Unsetenv("NTP_SERVERS")
		os.Unsetenv("LOG_LEVEL")
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

func TestSplitByComma(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "single_item",
			input:    "test",
			expected: []string{"test"},
		},
		{
			name:     "multiple_items",
			input:    "a,b,c",
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "empty_string",
			input:    "",
			expected: nil,
		},
		{
			name:     "trailing_comma",
			input:    "a,b,",
			expected: []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitByComma(tt.input)
			if tt.expected == nil && result == nil {
				return
			}
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTrim(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no_whitespace",
			input:    "test",
			expected: "test",
		},
		{
			name:     "leading_spaces",
			input:    "   test",
			expected: "test",
		},
		{
			name:     "trailing_spaces",
			input:    "test   ",
			expected: "test",
		},
		{
			name:     "both_sides",
			input:    "  test  ",
			expected: "test",
		},
		{
			name:     "tabs_and_newlines",
			input:    "\t\ntest\n\t",
			expected: "test",
		},
		{
			name:     "empty_string",
			input:    "",
			expected: "",
		},
		{
			name:     "only_whitespace",
			input:    "   ",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := trim(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLoadFromEnvVarsOnly_WithServersWithSpaces(t *testing.T) {
	os.Setenv("NTP_SERVERS", " pool.ntp.org , time.google.com , time.cloudflare.com ")
	defer os.Unsetenv("NTP_SERVERS")

	cfg, err := LoadFromEnvVarsOnly()

	require.NoError(t, err)
	assert.Len(t, cfg.NTP.Servers, 3)
	assert.Equal(t, "pool.ntp.org", cfg.NTP.Servers[0])
	assert.Equal(t, "time.google.com", cfg.NTP.Servers[1])
	assert.Equal(t, "time.cloudflare.com", cfg.NTP.Servers[2])
}

func TestLoadFromYamlWithEnvOverrides_Success(t *testing.T) {
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
  enable_kernel: false
logging:
  level: "info"
`

	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	// Set environment overrides
	os.Setenv("NTP_EXPORTER_PORT", "8080")
	os.Setenv("NTP_ENABLE_KERNEL", "true")
	os.Setenv("LOG_LEVEL", "debug")

	defer func() {
		os.Unsetenv("NTP_EXPORTER_PORT")
		os.Unsetenv("NTP_ENABLE_KERNEL")
		os.Unsetenv("LOG_LEVEL")
	}()

	cfg, err := LoadFromYamlWithEnvOverrides(configFile)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	// YAML values
	assert.Equal(t, "127.0.0.1", cfg.Server.Address)
	assert.Contains(t, cfg.NTP.Servers, "pool.ntp.org")
	// Environment overrides
	assert.Equal(t, 8080, cfg.Server.Port)
	assert.True(t, cfg.NTP.EnableKernel)
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
metrics:
  namespace: "ntp"
`

	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = LoadFromYamlFile(configFile)
	}
}

func BenchmarkLoadFromEnvVarsOnly(b *testing.B) {
	os.Setenv("NTP_SERVERS", "pool.ntp.org,time.google.com")
	defer os.Unsetenv("NTP_SERVERS")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = LoadFromEnvVarsOnly()
	}
}

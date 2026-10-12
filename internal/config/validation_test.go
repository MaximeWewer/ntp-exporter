package config

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestValidate_ValidConfig(t *testing.T) {
	cfg := DefaultConfig()

	err := Validate(cfg)

	assert.NoError(t, err)
}

func TestValidateServer_ValidPort(t *testing.T) {
	tests := []struct {
		name string
		port int
		want bool
	}{
		{"minimum_port", 1, true},
		{"standard_port", 9559, true},
		{"maximum_port", 65535, true},
		{"zero_port", 0, false},
		{"negative_port", -1, false},
		{"too_high_port", 65536, false},
		{"way_too_high", 99999, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ServerConfig{
				Port:         tt.port,
				ReadTimeout:  10 * time.Second,
				WriteTimeout: 10 * time.Second,
			}

			err := validateServer(cfg)

			if tt.want {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "port")
			}
		})
	}
}

func TestValidateServer_Timeouts(t *testing.T) {
	tests := []struct {
		name         string
		readTimeout  time.Duration
		writeTimeout time.Duration
		wantErr      bool
	}{
		{"valid_timeouts", 10 * time.Second, 10 * time.Second, false},
		{"minimum_timeouts", 1 * time.Second, 1 * time.Second, false},
		{"maximum_timeouts", 60 * time.Second, 60 * time.Second, false},
		{"read_too_short", 500 * time.Millisecond, 10 * time.Second, true},
		{"write_too_short", 10 * time.Second, 500 * time.Millisecond, true},
		{"read_too_long", 61 * time.Second, 10 * time.Second, true},
		{"write_too_long", 10 * time.Second, 61 * time.Second, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ServerConfig{
				Port:         9559,
				ReadTimeout:  tt.readTimeout,
				WriteTimeout: tt.writeTimeout,
			}

			err := validateServer(cfg)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateServer_TLSConfig(t *testing.T) {
	tests := []struct {
		name        string
		tlsEnabled  bool
		certFile    string
		keyFile     string
		wantErr     bool
		errContains string
	}{
		{"tls_disabled", false, "", "", false, ""},
		{"tls_with_files", true, "/path/to/cert.pem", "/path/to/key.pem", false, ""},
		{"tls_without_cert", true, "", "/path/to/key.pem", true, "tls_cert_file"},
		{"tls_without_key", true, "/path/to/cert.pem", "", true, "tls_key_file"},
		{"tls_without_both", true, "", "", true, "tls_cert_file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ServerConfig{
				Port:         9559,
				ReadTimeout:  10 * time.Second,
				WriteTimeout: 10 * time.Second,
				TLSEnabled:   tt.tlsEnabled,
				TLSCertFile:  tt.certFile,
				TLSKeyFile:   tt.keyFile,
			}

			err := validateServer(cfg)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateNTP_ServersOrPools(t *testing.T) {
	tests := []struct {
		name    string
		servers []string
		pools   []PoolConfig
		wantErr bool
	}{
		{"servers_only", []string{"pool.ntp.org"}, nil, false},
		{"pools_only", nil, []PoolConfig{{Name: "pool.ntp.org", MaxServers: 4}}, false},
		{"both", []string{"pool.ntp.org"}, []PoolConfig{{Name: "0.pool.ntp.org", MaxServers: 4}}, false},
		{"neither", nil, nil, true},
		{"empty_both", []string{}, []PoolConfig{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &NTPConfig{
				Servers:          tt.servers,
				Pools:            tt.pools,
				Timeout:          5 * time.Second,
				Version:          4,
				SamplesPerServer: 3,
				MaxConcurrency:   10,
				ScrapeInterval:   30 * time.Second,
				MaxClockOffset:   100 * time.Millisecond,
				KoD:              KoDConfig{MaxBackoff: 1024 * time.Second, SuspendDuration: time.Hour},
				DNSCache:         DNSCacheConfig{MinTTL: 5 * time.Minute, MaxTTL: time.Hour},
			}

			err := validateNTP(cfg)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "at least one NTP server or pool")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateNTP_Timeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		wantErr bool
	}{
		{"valid_5s", 5 * time.Second, false},
		{"minimum_1s", 1 * time.Second, false},
		{"maximum_60s", 60 * time.Second, false},
		{"too_short", 500 * time.Millisecond, true},
		{"too_long", 61 * time.Second, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &NTPConfig{
				Servers:          []string{"pool.ntp.org"},
				Timeout:          tt.timeout,
				Version:          4,
				SamplesPerServer: 3,
				MaxConcurrency:   10,
				ScrapeInterval:   30 * time.Second,
				MaxClockOffset:   100 * time.Millisecond,
				KoD:              KoDConfig{MaxBackoff: 1024 * time.Second, SuspendDuration: time.Hour},
				DNSCache:         DNSCacheConfig{MinTTL: 5 * time.Minute, MaxTTL: time.Hour},
			}

			err := validateNTP(cfg)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "timeout")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateNTP_Version(t *testing.T) {
	tests := []struct {
		name    string
		version int
		wantErr bool
	}{
		{"v2", 2, false},
		{"v3", 3, false},
		{"v4", 4, false},
		{"v1", 1, true},
		{"v5", 5, true},
		{"v0", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &NTPConfig{
				Servers:          []string{"pool.ntp.org"},
				Timeout:          5 * time.Second,
				Version:          tt.version,
				SamplesPerServer: 3,
				MaxConcurrency:   10,
				ScrapeInterval:   30 * time.Second,
				MaxClockOffset:   100 * time.Millisecond,
				KoD:              KoDConfig{MaxBackoff: 1024 * time.Second, SuspendDuration: time.Hour},
				DNSCache:         DNSCacheConfig{MinTTL: 5 * time.Minute, MaxTTL: time.Hour},
			}

			err := validateNTP(cfg)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "ntp version")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateNTP_SamplesPerServer(t *testing.T) {
	tests := []struct {
		name    string
		samples int
		wantErr bool
	}{
		{"minimum_1", 1, false},
		{"standard_3", 3, false},
		{"maximum_20", 20, false},
		{"zero", 0, true},
		{"too_many", 21, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &NTPConfig{
				Servers:          []string{"pool.ntp.org"},
				Timeout:          5 * time.Second,
				Version:          4,
				SamplesPerServer: tt.samples,
				MaxConcurrency:   10,
				ScrapeInterval:   30 * time.Second,
				MaxClockOffset:   100 * time.Millisecond,
				KoD:              KoDConfig{MaxBackoff: 1024 * time.Second, SuspendDuration: time.Hour},
				DNSCache:         DNSCacheConfig{MinTTL: 5 * time.Minute, MaxTTL: time.Hour},
			}

			err := validateNTP(cfg)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "samples_per_server")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateNTP_MaxConcurrency(t *testing.T) {
	tests := []struct {
		name       string
		concurrent int
		wantErr    bool
	}{
		{"minimum_1", 1, false},
		{"standard_10", 10, false},
		{"maximum_100", 100, false},
		{"zero", 0, true},
		{"too_many", 101, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &NTPConfig{
				Servers:          []string{"pool.ntp.org"},
				Timeout:          5 * time.Second,
				Version:          4,
				SamplesPerServer: 3,
				MaxConcurrency:   tt.concurrent,
				ScrapeInterval:   30 * time.Second,
				MaxClockOffset:   100 * time.Millisecond,
				KoD:              KoDConfig{MaxBackoff: 1024 * time.Second, SuspendDuration: time.Hour},
				DNSCache:         DNSCacheConfig{MinTTL: 5 * time.Minute, MaxTTL: time.Hour},
			}

			err := validateNTP(cfg)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "max_concurrency")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateNTP_Pools(t *testing.T) {
	tests := []struct {
		name    string
		pool    PoolConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid_pool",
			pool:    PoolConfig{Name: "pool.ntp.org", Strategy: "best_n", MaxServers: 4},
			wantErr: false,
		},
		{
			name:    "empty_name",
			pool:    PoolConfig{Name: "", Strategy: "best_n", MaxServers: 4},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name:    "invalid_strategy",
			pool:    PoolConfig{Name: "pool.ntp.org", Strategy: "invalid", MaxServers: 4},
			wantErr: true,
			errMsg:  "invalid strategy",
		},
		{
			name:    "max_servers_too_low",
			pool:    PoolConfig{Name: "pool.ntp.org", Strategy: "best_n", MaxServers: 0},
			wantErr: true,
			errMsg:  "max_servers",
		},
		{
			name:    "max_servers_too_high",
			pool:    PoolConfig{Name: "pool.ntp.org", Strategy: "best_n", MaxServers: 21},
			wantErr: true,
			errMsg:  "max_servers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &NTPConfig{
				Pools:            []PoolConfig{tt.pool},
				Timeout:          5 * time.Second,
				Version:          4,
				SamplesPerServer: 3,
				MaxConcurrency:   10,
				ScrapeInterval:   30 * time.Second,
				MaxClockOffset:   100 * time.Millisecond,
				KoD:              KoDConfig{MaxBackoff: 1024 * time.Second, SuspendDuration: time.Hour},
				DNSCache:         DNSCacheConfig{MinTTL: 5 * time.Minute, MaxTTL: time.Hour},
			}

			err := validateNTP(cfg)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateNTP_PoolStrategies(t *testing.T) {
	strategies := []string{"best_n", "round_robin", "all", ""}

	for _, strategy := range strategies {
		t.Run("strategy_"+strategy, func(t *testing.T) {
			pool := PoolConfig{
				Name:       "pool.ntp.org",
				Strategy:   strategy,
				MaxServers: 4,
			}

			cfg := &NTPConfig{
				Pools:            []PoolConfig{pool},
				Timeout:          5 * time.Second,
				Version:          4,
				SamplesPerServer: 3,
				MaxConcurrency:   10,
				ScrapeInterval:   30 * time.Second,
				MaxClockOffset:   100 * time.Millisecond,
				KoD:              KoDConfig{MaxBackoff: 1024 * time.Second, SuspendDuration: time.Hour},
				DNSCache:         DNSCacheConfig{MinTTL: 5 * time.Minute, MaxTTL: time.Hour},
			}

			err := validateNTP(cfg)

			if strategy == "" || strategy == "best_n" || strategy == "round_robin" || strategy == "all" {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestValidateLogging_Level(t *testing.T) {
	validLevels := []string{"trace", "debug", "info", "warn", "error", "fatal", "panic"}
	invalidLevels := []string{"invalid", "INFO", "warning", ""}

	for _, level := range validLevels {
		t.Run("valid_"+level, func(t *testing.T) {
			cfg := &LoggingConfig{
				Level:  level,
				Format: "json",
			}

			err := validateLogging(cfg)
			assert.NoError(t, err)
		})
	}

	for _, level := range invalidLevels {
		t.Run("invalid_"+level, func(t *testing.T) {
			cfg := &LoggingConfig{
				Level:  level,
				Format: "json",
			}

			err := validateLogging(cfg)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "invalid log level")
		})
	}
}

func TestValidateLogging_Format(t *testing.T) {
	tests := []struct {
		name    string
		format  string
		wantErr bool
	}{
		{"json", "json", false},
		{"console", "console", false},
		{"invalid", "xml", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &LoggingConfig{
				Level:  "info",
				Format: tt.format,
			}

			err := validateLogging(cfg)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "invalid log format")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateLogging_FileConfig(t *testing.T) {
	tests := []struct {
		name       string
		enableFile bool
		filePath   string
		wantErr    bool
	}{
		{"file_disabled", false, "", false},
		{"file_enabled_with_path", true, "/var/log/ntp.log", false},
		{"file_enabled_no_path", true, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &LoggingConfig{
				Level:      "info",
				Format:     "json",
				EnableFile: tt.enableFile,
				FilePath:   tt.filePath,
			}

			err := validateLogging(cfg)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "file_path")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateMetrics_Namespace(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		wantErr   bool
	}{
		{"valid", "ntp", false},
		{"custom", "my_metrics", false},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &MetricsConfig{
				Namespace: tt.namespace,
			}

			err := validateMetrics(cfg)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "namespace")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidate_CompleteConfig(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Address:      "0.0.0.0",
			Port:         9559,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
		},
		NTP: NTPConfig{
			Servers:          []string{"pool.ntp.org"},
			Timeout:          5 * time.Second,
			Version:          4,
			SamplesPerServer: 3,
			MaxConcurrency:   10,
			ScrapeInterval:   30 * time.Second,
			MaxClockOffset:   100 * time.Millisecond,
			KoD:              KoDConfig{MaxBackoff: 1024 * time.Second, SuspendDuration: time.Hour},
			DNSCache:         DNSCacheConfig{MinTTL: 5 * time.Minute, MaxTTL: time.Hour},
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
		},
		Metrics: MetricsConfig{
			Namespace: "ntp",
		},
	}

	err := Validate(cfg)
	assert.NoError(t, err)
}

func BenchmarkValidate(b *testing.B) {
	cfg := DefaultConfig()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Validate(cfg)
	}
}

func TestValidate_ScrapeIntervalBounds(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Second, time.Second, 2 * time.Hour} {
		cfg := DefaultConfig()
		cfg.NTP.ScrapeInterval = interval
		if err := Validate(cfg); err == nil {
			t.Errorf("scrape_interval %s should be rejected", interval)
		}
	}
	cfg := DefaultConfig()
	cfg.NTP.ScrapeInterval = 30 * time.Second
	if err := Validate(cfg); err != nil {
		t.Errorf("scrape_interval 30s should be accepted: %v", err)
	}
}

func TestValidate_MaxClockOffsetPositive(t *testing.T) {
	cfg := DefaultConfig()
	cfg.NTP.MaxClockOffset = -time.Millisecond
	if err := Validate(cfg); err == nil {
		t.Error("negative max_clock_offset should be rejected")
	}
}

func TestValidate_BurstMustFitInInterval(t *testing.T) {
	cfg := DefaultConfig()
	cfg.NTP.ScrapeInterval = 10 * time.Second
	cfg.NTP.SamplesPerServer = 5
	cfg.NTP.SampleSpacing = 2 * time.Second
	if err := Validate(cfg); err == nil {
		t.Error("5 samples x 2s must not fit in a 10s interval")
	}

	cfg.NTP.SamplesPerServer = 4
	if err := Validate(cfg); err != nil {
		t.Errorf("4 samples x 2s fit in a 10s interval: %v", err)
	}

	cfg = DefaultConfig()
	cfg.NTP.SampleSpacing = -time.Second
	if err := Validate(cfg); err == nil {
		t.Error("negative sample_spacing must be rejected")
	}
}

func TestWarnings_PublicPoolEtiquette(t *testing.T) {
	cfg := DefaultConfig()
	cfg.NTP.Servers = []string{"0.europe.pool.ntp.org", "time.google.com"}
	cfg.NTP.Pools = []PoolConfig{{Name: "pool.ntp.org"}}
	cfg.NTP.ScrapeInterval = 30 * time.Second

	warnings := Warnings(cfg)
	if len(warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %v", warnings)
	}

	cfg.NTP.ScrapeInterval = 60 * time.Second
	if w := Warnings(cfg); len(w) != 0 {
		t.Errorf("no warning expected at 60s, got %v", w)
	}
}

func TestWarnings_DeprecatedEnableKernel(t *testing.T) {
	cfg := DefaultConfig()
	cfg.NTP.EnableKernel = true
	if w := Warnings(cfg); len(w) != 1 || !strings.Contains(w[0], "deprecated") {
		t.Errorf("expected a deprecation warning, got %v", w)
	}
}

func TestValidate_IPFamily(t *testing.T) {
	for _, family := range []string{IPFamilyAuto, IPFamilyIPv4, IPFamilyIPv6} {
		cfg := DefaultConfig()
		cfg.NTP.IPFamily = family
		if err := Validate(cfg); err != nil {
			t.Errorf("ip_family %q must be accepted: %v", family, err)
		}
	}

	cfg := DefaultConfig()
	cfg.NTP.IPFamily = "inet6"
	if err := Validate(cfg); err == nil {
		t.Error("unknown ip_family must be rejected")
	}
}

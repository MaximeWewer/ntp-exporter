package config

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
				MaxRootDistance:  DefaultMaxRootDistance,
				KoD:              KoDConfig{MaxBackoff: 1024 * time.Second, SuspendDuration: time.Hour},
				DNSCache:         DNSCacheConfig{MinTTL: 5 * time.Minute, MaxTTL: time.Hour},
			}

			err := validateNTP(cfg, false)

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
				MaxRootDistance:  DefaultMaxRootDistance,
				KoD:              KoDConfig{MaxBackoff: 1024 * time.Second, SuspendDuration: time.Hour},
				DNSCache:         DNSCacheConfig{MinTTL: 5 * time.Minute, MaxTTL: time.Hour},
			}

			err := validateNTP(cfg, false)

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
				MaxRootDistance:  DefaultMaxRootDistance,
				KoD:              KoDConfig{MaxBackoff: 1024 * time.Second, SuspendDuration: time.Hour},
				DNSCache:         DNSCacheConfig{MinTTL: 5 * time.Minute, MaxTTL: time.Hour},
			}

			err := validateNTP(cfg, false)

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
				MaxRootDistance:  DefaultMaxRootDistance,
				KoD:              KoDConfig{MaxBackoff: 1024 * time.Second, SuspendDuration: time.Hour},
				DNSCache:         DNSCacheConfig{MinTTL: 5 * time.Minute, MaxTTL: time.Hour},
			}

			err := validateNTP(cfg, false)

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
				MaxRootDistance:  DefaultMaxRootDistance,
				KoD:              KoDConfig{MaxBackoff: 1024 * time.Second, SuspendDuration: time.Hour},
				DNSCache:         DNSCacheConfig{MinTTL: 5 * time.Minute, MaxTTL: time.Hour},
			}

			err := validateNTP(cfg, false)

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
				MaxRootDistance:  DefaultMaxRootDistance,
				KoD:              KoDConfig{MaxBackoff: 1024 * time.Second, SuspendDuration: time.Hour},
				DNSCache:         DNSCacheConfig{MinTTL: 5 * time.Minute, MaxTTL: time.Hour},
			}

			err := validateNTP(cfg, false)

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
				MaxRootDistance:  DefaultMaxRootDistance,
				KoD:              KoDConfig{MaxBackoff: 1024 * time.Second, SuspendDuration: time.Hour},
				DNSCache:         DNSCacheConfig{MinTTL: 5 * time.Minute, MaxTTL: time.Hour},
			}

			err := validateNTP(cfg, false)

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
			MaxRootDistance:  DefaultMaxRootDistance,
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

func TestValidate_ProbeEndpoint(t *testing.T) {
	cfg := DefaultConfig()
	assert.True(t, cfg.ProbeEndpoint.IsEnabled(), "the endpoint is served by default")

	cfg.ProbeEndpoint.Only = true
	assert.ErrorContains(t, Validate(cfg), "excludes static servers")

	cfg.NTP.Servers = nil
	cfg.NTP.Pools = nil
	assert.NoError(t, Validate(cfg), "no static target needed with probe_endpoint.only")

	disabled := false
	cfg.ProbeEndpoint.Enabled = &disabled
	assert.ErrorContains(t, Validate(cfg), "requires probe_endpoint.enabled")
}

func TestValidate_ChronyAddress(t *testing.T) {
	cfg := DefaultConfig()
	cfg.NTP.Chrony.Enabled = true
	for _, ok := range []string{"udp://127.0.0.1:323", "udp://[::1]:323", "unix:///run/chrony/chronyd.sock"} {
		cfg.NTP.Chrony.Address = ok
		assert.NoError(t, Validate(cfg), ok)
	}
	for _, bad := range []string{"127.0.0.1:323", "udp://127.0.0.1", "unix://", "tcp://127.0.0.1:323", "%zz"} {
		cfg.NTP.Chrony.Address = bad
		assert.Error(t, Validate(cfg), bad)
	}

	cfg.NTP.Chrony.Enabled = false
	assert.NoError(t, Validate(cfg), "the address is only checked when chrony is enabled")
}

func TestValidate_ServerNames(t *testing.T) {
	cfg := DefaultConfig()
	cfg.NTP.Servers = []string{"time.example.org", "192.0.2.1:123", "nts://time.cloudflare.com", "nts://nts.netnod.se:4460"}
	assert.NoError(t, Validate(cfg))

	for _, bad := range []string{"nts://", "http://time.example.org", "time example"} {
		cfg.NTP.Servers = []string{bad}
		assert.Error(t, Validate(cfg), bad)
	}

	cfg = DefaultConfig()
	cfg.NTP.Pools = []PoolConfig{{Name: "nts://pool.example.org", MaxServers: 4}}
	assert.ErrorContains(t, Validate(cfg), "NTS is only supported for servers")
}

func TestValidateNTP_DuplicateTargets(t *testing.T) {
	tests := []struct {
		name    string
		servers []string
		pools   []string
		wantErr string
	}{
		{"same server twice", []string{"a.example.org", "a.example.org"}, nil, `server "a.example.org" is listed twice`},
		{"host names are case insensitive", []string{"a.example.org", "A.example.org"}, nil, "listed twice"},
		{"default NTP port", []string{"a.example.org", "a.example.org:123"}, nil, "listed twice"},
		{"default NTS port", []string{"nts://a.example.org", "nts://a.example.org:4460"}, nil, "listed twice"},
		{"same pool twice", nil, []string{"pool.ntp.org", "pool.ntp.org"}, `pool "pool.ntp.org" is listed twice`},
		{"other port", []string{"a.example.org", "a.example.org:1123"}, nil, ""},
		{"NTP and NTS", []string{"a.example.org", "nts://a.example.org"}, nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.NTP.Servers = tt.servers
			for _, p := range tt.pools {
				cfg.NTP.Pools = append(cfg.NTP.Pools, PoolConfig{Name: p, Strategy: "all", MaxServers: 4})
			}

			err := Validate(cfg)
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}

func TestValidateNTP_MaxRootDistance(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, 1500*time.Millisecond, cfg.NTP.MaxRootDistance, "MAXDIST of ntpd by default")

	t.Setenv("NTP_MAX_ROOT_DISTANCE", "3s")
	loaded, err := LoadFromEnvVarsOnly()
	require.NoError(t, err)
	assert.Equal(t, 3*time.Second, loaded.NTP.MaxRootDistance)

	for _, d := range []time.Duration{50 * time.Millisecond, 17 * time.Second} {
		cfg := DefaultConfig()
		cfg.NTP.MaxRootDistance = d
		assert.ErrorContains(t, Validate(cfg), "max_root_distance must be between 100ms and 16s")
	}
}

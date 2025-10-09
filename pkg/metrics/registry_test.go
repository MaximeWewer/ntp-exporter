package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRegistry(t *testing.T) {
	reg := NewRegistry()

	assert.NotNil(t, reg)
	assert.NotNil(t, reg.registry)
}

func TestRegistry_Register(t *testing.T) {
	reg := NewRegistry()

	err := reg.Register()

	assert.NoError(t, err)
}

func TestRegistry_Register_Idempotent(t *testing.T) {
	reg := NewRegistry()

	// First registration should succeed
	err := reg.Register()
	assert.NoError(t, err)

	// Second registration should fail (metrics already registered)
	err = reg.Register()
	assert.Error(t, err)
}

func TestRegistry_GetRegistry(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register()
	require.NoError(t, err)

	promReg := reg.GetRegistry()

	assert.NotNil(t, promReg)
	assert.IsType(t, &prometheus.Registry{}, promReg)
}

func TestRegistry_MustRegister_Success(t *testing.T) {
	reg := NewRegistry()

	assert.NotPanics(t, func() {
		reg.MustRegister()
	})
}

func TestRegistry_MustRegister_Panic(t *testing.T) {
	reg := NewRegistry()

	// Register once successfully
	reg.MustRegister()

	// Second call should panic
	assert.Panics(t, func() {
		reg.MustRegister()
	})
}

func TestRegistry_MetricsRegistered(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register()
	require.NoError(t, err)

	// Set some metric values to ensure they appear in output
	m := reg.GetMetrics()
	m.OffsetSeconds.WithLabelValues("test.ntp.org", "2", "4").Set(0.001)
	m.ExporterBuildInfo.WithLabelValues("1.0.0", "test", "test").Set(1)

	promReg := reg.GetRegistry()

	// Gather metrics to verify they're registered
	metricFamilies, err := promReg.Gather()
	require.NoError(t, err)

	// Should have metrics registered
	assert.NotEmpty(t, metricFamilies)

	// Check for some expected metrics
	metricNames := make(map[string]bool)
	for _, mf := range metricFamilies {
		metricNames[mf.GetName()] = true
	}

	// Verify some key metrics are registered
	expectedMetrics := []string{
		"ntp_offset_seconds",
		"ntp_exporter_build_info",
	}

	for _, expected := range expectedMetrics {
		assert.True(t, metricNames[expected], "Expected metric %s to be registered", expected)
	}
}

func TestRegistry_GoMetricsRegistered(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register()
	require.NoError(t, err)

	promReg := reg.GetRegistry()
	metricFamilies, err := promReg.Gather()
	require.NoError(t, err)

	// Check for Go runtime metrics
	metricNames := make(map[string]bool)
	for _, mf := range metricFamilies {
		metricNames[mf.GetName()] = true
	}

	// Should have Go collector metrics
	goMetrics := []string{
		"go_goroutines",
		"go_info",
		"go_memstats_alloc_bytes",
	}

	foundGoMetrics := 0
	for _, metric := range goMetrics {
		if metricNames[metric] {
			foundGoMetrics++
		}
	}

	assert.Greater(t, foundGoMetrics, 0, "Should have at least one Go metric registered")
}

func TestRegistry_ProcessMetricsRegistered(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register()
	require.NoError(t, err)

	promReg := reg.GetRegistry()
	metricFamilies, err := promReg.Gather()
	require.NoError(t, err)

	// Check for process metrics
	metricNames := make(map[string]bool)
	for _, mf := range metricFamilies {
		metricNames[mf.GetName()] = true
	}

	// Should have process collector metrics
	processMetrics := []string{
		"process_cpu_seconds_total",
		"process_resident_memory_bytes",
		"process_open_fds",
	}

	foundProcessMetrics := 0
	for _, metric := range processMetrics {
		if metricNames[metric] {
			foundProcessMetrics++
		}
	}

	assert.Greater(t, foundProcessMetrics, 0, "Should have at least one process metric registered")
}

func TestMetricDefinitions_Types(t *testing.T) {
	// Verify metric types
	m := NewNTPMetrics()

	assert.IsType(t, &prometheus.GaugeVec{}, m.OffsetSeconds)
	assert.IsType(t, &prometheus.GaugeVec{}, m.RTTSeconds)
	assert.IsType(t, &prometheus.GaugeVec{}, m.ServerReachable)
	assert.IsType(t, &prometheus.GaugeVec{}, m.Stratum)

	assert.IsType(t, &prometheus.CounterVec{}, m.ServerSuspiciousTotal)
	assert.IsType(t, &prometheus.CounterVec{}, m.KissOfDeathTotal)
	assert.IsType(t, &prometheus.CounterVec{}, m.MalformedResponsesTotal)

	assert.NotNil(t, m.ExporterScrapeDuration)
	assert.NotNil(t, m.ExporterServersConfigured)
	assert.NotNil(t, m.ExporterMemoryUsageBytes)
}

func TestMetricDefinitions_LabelsUsage(t *testing.T) {
	// Test that metrics can accept labels
	m := NewNTPMetrics()

	m.OffsetSeconds.WithLabelValues("pool.ntp.org", "2", "4").Set(0.001)
	m.RTTSeconds.WithLabelValues("pool.ntp.org").Set(0.050)
	m.ServerReachable.WithLabelValues("pool.ntp.org").Set(1)
	m.Stratum.WithLabelValues("pool.ntp.org").Set(2)

	// Test counter metrics
	m.ServerSuspiciousTotal.WithLabelValues("pool.ntp.org", "high_stratum").Inc()
	m.KissOfDeathTotal.WithLabelValues("pool.ntp.org", "RATE").Inc()

	// Test pool metrics
	m.PoolServersActive.WithLabelValues("pool.ntp.org").Set(4)
	m.PoolServersTotal.WithLabelValues("pool.ntp.org").Set(5)

	// If we get here without panic, labels work correctly
	assert.True(t, true)
}

func TestRegistry_MultipleInstances(t *testing.T) {
	// Create two separate registries
	reg1 := NewRegistry()
	reg2 := NewRegistry()

	// Both should register successfully
	err1 := reg1.Register()
	err2 := reg2.Register()

	assert.NoError(t, err1)
	assert.NoError(t, err2)

	// They should be different instances
	assert.NotEqual(t, reg1.GetRegistry(), reg2.GetRegistry())
}

func TestRegistry_MetricValues(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register()
	require.NoError(t, err)

	// Get metrics instance
	m := reg.GetMetrics()

	// Set some metric values
	m.OffsetSeconds.WithLabelValues("test.ntp.org", "2", "4").Set(0.005)
	m.RTTSeconds.WithLabelValues("test.ntp.org").Set(0.025)
	m.ServerReachable.WithLabelValues("test.ntp.org").Set(1)

	// Gather and verify
	metricFamilies, err := reg.GetRegistry().Gather()
	require.NoError(t, err)

	// Find our metrics
	found := false
	for _, mf := range metricFamilies {
		if mf.GetName() == "ntp_offset_seconds" {
			found = true
			assert.NotEmpty(t, mf.GetMetric())
		}
	}

	assert.True(t, found, "Should find ntp_offset_seconds metric")
}

func BenchmarkRegistry_Register(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reg := NewRegistry()
		_ = reg.Register()
	}
}

func BenchmarkRegistry_Gather(b *testing.B) {
	reg := NewRegistry()
	err := reg.Register()
	require.NoError(b, err)

	promReg := reg.GetRegistry()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = promReg.Gather()
	}
}

func BenchmarkMetrics_SetValues(b *testing.B) {
	m := NewNTPMetrics()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.OffsetSeconds.WithLabelValues("pool.ntp.org", "2", "4").Set(0.001)
		m.RTTSeconds.WithLabelValues("pool.ntp.org").Set(0.050)
		m.ServerReachable.WithLabelValues("pool.ntp.org").Set(1)
	}
}

func TestNewRegistryWithConfig(t *testing.T) {
	// Test with custom namespace and subsystem
	reg := NewRegistryWithConfig("custom", "monitoring")

	assert.NotNil(t, reg)
	assert.NotNil(t, reg.registry)
	assert.NotNil(t, reg.ntpMetrics)
}

func TestRegistryWithConfig_MetricNames(t *testing.T) {
	// Test with custom namespace and empty subsystem
	reg1 := NewRegistryWithConfig("myapp", "")
	err := reg1.Register()
	require.NoError(t, err)

	// Set metric values
	m1 := reg1.GetMetrics()
	m1.OffsetSeconds.WithLabelValues("test.ntp.org", "2", "4").Set(0.001)
	m1.ExporterBuildInfo.WithLabelValues("1.0.0", "test", "go1.21").Set(1)
	m1.PoolServersActive.WithLabelValues("pool.ntp.org").Set(4)

	// Gather metrics
	metricFamilies1, err := reg1.GetRegistry().Gather()
	require.NoError(t, err)

	// Check metric name has custom namespace
	metricNames1 := make(map[string]bool)
	for _, mf := range metricFamilies1 {
		metricNames1[mf.GetName()] = true
	}

	// Should have metrics with custom namespace "myapp_"
	// Base metrics use the configured subsystem (empty in this case)
	assert.True(t, metricNames1["myapp_offset_seconds"], "Expected metric myapp_offset_seconds")
	// Exporter metrics always use "exporter" subsystem
	assert.True(t, metricNames1["myapp_exporter_build_info"], "Expected metric myapp_exporter_build_info")
	// Pool metrics always use "pool" subsystem
	assert.True(t, metricNames1["myapp_pool_servers_active"], "Expected metric myapp_pool_servers_active")

	// Test with custom namespace and subsystem
	reg2 := NewRegistryWithConfig("myapp", "timesync")
	err = reg2.Register()
	require.NoError(t, err)

	// Set metric values
	m2 := reg2.GetMetrics()
	m2.OffsetSeconds.WithLabelValues("test.ntp.org", "2", "4").Set(0.001)
	m2.ExporterBuildInfo.WithLabelValues("1.0.0", "test", "go1.21").Set(1)
	m2.PoolServersActive.WithLabelValues("pool.ntp.org").Set(4)

	// Gather metrics
	metricFamilies2, err := reg2.GetRegistry().Gather()
	require.NoError(t, err)

	// Check metric name has custom namespace and subsystem
	metricNames2 := make(map[string]bool)
	for _, mf := range metricFamilies2 {
		metricNames2[mf.GetName()] = true
	}

	// Should have metrics with custom namespace and subsystem "myapp_timesync_"
	assert.True(t, metricNames2["myapp_timesync_offset_seconds"], "Expected metric myapp_timesync_offset_seconds")
	// Pool metrics should always use "pool" subsystem
	assert.True(t, metricNames2["myapp_pool_servers_active"], "Expected metric myapp_pool_servers_active")
	// Exporter metrics should always use "exporter" subsystem
	assert.True(t, metricNames2["myapp_exporter_build_info"], "Expected metric myapp_exporter_build_info")
}

func TestNewNTPMetrics_BackwardCompatibility(t *testing.T) {
	// Test that NewNTPMetrics() still works with default namespace
	m := NewNTPMetrics()

	assert.NotNil(t, m)
	assert.NotNil(t, m.OffsetSeconds)
	assert.NotNil(t, m.ExporterBuildInfo)

	// Create registry and verify metric names
	reg := prometheus.NewRegistry()
	reg.MustRegister(m)

	m.OffsetSeconds.WithLabelValues("test.ntp.org", "2", "4").Set(0.001)
	m.ExporterBuildInfo.WithLabelValues("1.0.0", "test", "go1.21").Set(1)
	m.PoolServersActive.WithLabelValues("pool.ntp.org").Set(4)

	metricFamilies, err := reg.Gather()
	require.NoError(t, err)

	metricNames := make(map[string]bool)
	for _, mf := range metricFamilies {
		metricNames[mf.GetName()] = true
	}

	// Should have metrics with default namespace "ntp_"
	assert.True(t, metricNames["ntp_offset_seconds"], "Expected default metric ntp_offset_seconds")
	assert.True(t, metricNames["ntp_exporter_build_info"], "Expected default metric ntp_exporter_build_info")
	assert.True(t, metricNames["ntp_pool_servers_active"], "Expected default metric ntp_pool_servers_active")
}

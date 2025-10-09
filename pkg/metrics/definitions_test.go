package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricDefinitions_Registration(t *testing.T) {
	// Test that all metrics can be registered without conflicts
	registry := prometheus.NewRegistry()
	m := NewNTPMetrics()

	err := registry.Register(m)
	assert.NoError(t, err, "NTPMetrics should register successfully")
}

func TestMetricDefinitions_SetValues(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewNTPMetrics()
	registry.MustRegister(m)

	// Set value
	m.OffsetSeconds.WithLabelValues("pool.ntp.org", "2", "4").Set(0.010)

	// Gather metrics
	metrics, err := registry.Gather()
	require.NoError(t, err)
	require.NotEmpty(t, metrics)

	// Find our metric
	found := false
	for _, mf := range metrics {
		if mf.GetName() == "ntp_offset_seconds" {
			found = true
			assert.NotEmpty(t, mf.GetMetric())
		}
	}

	assert.True(t, found, "Metric should be present")
}

func TestMetricDefinitions_CounterIncrement(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewNTPMetrics()
	registry.MustRegister(m)

	// Increment counter
	m.ExporterScrapesTotal.WithLabelValues("success").Inc()
	m.ExporterScrapesTotal.WithLabelValues("success").Inc()
	m.ExporterScrapesTotal.WithLabelValues("failure").Inc()

	// Gather metrics
	metrics, err := registry.Gather()
	require.NoError(t, err)

	// Find counter
	found := false
	for _, mf := range metrics {
		if mf.GetName() == "ntp_exporter_scrapes_total" {
			found = true
			assert.NotEmpty(t, mf.GetMetric())
		}
	}

	assert.True(t, found, "Counter metric should be present")
}

func TestMetricDefinitions_HistogramObserve(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewNTPMetrics()
	registry.MustRegister(m)

	// Observe values
	m.ExporterScrapeDuration.Observe(0.05)
	m.ExporterScrapeDuration.Observe(0.10)
	m.ExporterScrapeDuration.Observe(0.15)

	// Gather metrics
	metrics, err := registry.Gather()
	require.NoError(t, err)

	// Find histogram
	found := false
	for _, mf := range metrics {
		if mf.GetName() == "ntp_exporter_scrape_duration_seconds" {
			found = true
			histogram := mf.GetMetric()[0].GetHistogram()
			assert.Equal(t, uint64(3), histogram.GetSampleCount())
		}
	}

	assert.True(t, found, "Histogram metric should be present")
}

func TestMetricDefinitions_Labels(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewNTPMetrics()
	registry.MustRegister(m)

	// Create metrics with different label values
	servers := []string{
		"server1.ntp.org",
		"server2.ntp.org",
		"server3.ntp.org",
	}

	for _, server := range servers {
		m.RTTSeconds.WithLabelValues(server).Set(0.050)
	}

	// Gather metrics
	metrics, err := registry.Gather()
	require.NoError(t, err)

	// Find our metric and verify labels
	for _, mf := range metrics {
		if mf.GetName() == "ntp_rtt_seconds" {
			assert.Equal(t, 3, len(mf.GetMetric()))
		}
	}
}

func TestMetricDefinitions_Reset(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewNTPMetrics()
	registry.MustRegister(m)

	// Set values
	m.RTTSeconds.WithLabelValues("pool.ntp.org").Set(0.010)

	// Reset
	m.RTTSeconds.Reset()

	// Gather metrics
	metrics, err := registry.Gather()
	require.NoError(t, err)

	// Verify metrics are cleared
	for _, mf := range metrics {
		if mf.GetName() == "ntp_rtt_seconds" {
			assert.Equal(t, 0, len(mf.GetMetric()))
		}
	}
}

func TestMetricDefinitions_PoolMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewNTPMetrics()
	registry.MustRegister(m)

	// Set pool values
	m.PoolServersActive.WithLabelValues("pool.ntp.org").Set(4)
	m.PoolServersTotal.WithLabelValues("pool.ntp.org").Set(4)
	m.PoolDNSResolutionSeconds.WithLabelValues("pool.ntp.org").Set(0.050)
	m.PoolBestOffsetSeconds.WithLabelValues("pool.ntp.org").Set(0.010)

	// Gather metrics
	metrics, err := registry.Gather()
	require.NoError(t, err)
	assert.NotEmpty(t, metrics)
}

func TestMetricDefinitions_SecurityMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewNTPMetrics()
	registry.MustRegister(m)

	// Increment counters
	m.ServerSuspiciousTotal.WithLabelValues("pool.ntp.org", "invalid_stratum").Inc()
	m.KissOfDeathTotal.WithLabelValues("pool.ntp.org", "RATE").Inc()
	m.MalformedResponsesTotal.WithLabelValues("pool.ntp.org").Inc()
	m.ServerTrustScore.WithLabelValues("pool.ntp.org").Set(0.85)

	// Gather metrics
	metrics, err := registry.Gather()
	require.NoError(t, err)
	assert.NotEmpty(t, metrics)
}

func BenchmarkMetricDefinitions_SetValue(b *testing.B) {
	registry := prometheus.NewRegistry()
	m := NewNTPMetrics()
	registry.MustRegister(m)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.OffsetSeconds.WithLabelValues("pool.ntp.org", "2", "4").Set(0.010)
	}
}

func BenchmarkMetricDefinitions_CounterInc(b *testing.B) {
	registry := prometheus.NewRegistry()
	m := NewNTPMetrics()
	registry.MustRegister(m)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.ExporterScrapesTotal.WithLabelValues("success").Inc()
	}
}

func BenchmarkMetricDefinitions_Gather(b *testing.B) {
	registry := prometheus.NewRegistry()
	m := NewNTPMetrics()
	registry.MustRegister(m)

	// Create some metrics
	for i := 0; i < 10; i++ {
		server := "server" + string(rune('0'+i)) + ".ntp.org"
		m.OffsetSeconds.WithLabelValues(server, "2", "4").Set(0.001 * float64(i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := registry.Gather()
		if err != nil {
			b.Fatal(err)
		}
	}
}

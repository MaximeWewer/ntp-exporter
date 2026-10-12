package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gatherNames(t *testing.T, g prometheus.Gatherer) map[string]bool {
	t.Helper()
	families, err := g.Gather()
	require.NoError(t, err)
	names := make(map[string]bool, len(families))
	for _, mf := range families {
		names[mf.GetName()] = true
	}
	return names
}

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register())
	assert.Error(t, r.Register(), "registering twice must fail")

	m := r.GetMetrics()
	m.ExporterBuildInfo.WithLabelValues("v1", "abc", "go1.27").Set(1)
	m.ExporterScrapesTotal.WithLabelValues("success").Inc()
	m.KissOfDeathTotal.WithLabelValues("time.example.org", "RATE").Inc()

	names := gatherNames(t, r.GetRegistry())
	for _, name := range []string{
		"ntp_exporter_build_info",
		"ntp_exporter_scrapes_total",
		"ntp_exporter_cycle_duration_seconds",
		"ntp_kiss_of_death_total",
		"go_goroutines",
		"process_cpu_seconds_total",
	} {
		assert.True(t, names[name], "missing %s", name)
	}
}

func TestRegistry_MustRegisterPanicsOnDuplicate(t *testing.T) {
	r := NewRegistry()
	r.MustRegister()
	assert.Panics(t, r.MustRegister)
}

func TestNewRegistryWithConfig_Namespace(t *testing.T) {
	r := NewRegistryWithConfig("custom", "probe")
	require.NoError(t, r.Register())
	r.GetMetrics().InvalidResponsesTotal.WithLabelValues("time.example.org", "kod").Inc()
	r.GetMetrics().ExporterScrapesTotal.WithLabelValues("success").Inc()

	names := gatherNames(t, r.GetRegistry())
	assert.True(t, names["custom_probe_invalid_responses_total"])
	assert.True(t, names["custom_exporter_scrapes_total"], "exporter metrics keep the exporter subsystem")
}

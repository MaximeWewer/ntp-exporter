package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
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
	r := NewRegistryWithConfig("ntp", "")
	require.NoError(t, r.Register())
	assert.Error(t, r.Register(), "registering twice must fail")

	m := r.GetMetrics()
	m.ExporterBuildInfo.WithLabelValues("v1", "abc", "go1.27").Set(1)
	m.ExporterCyclesTotal.WithLabelValues("success").Inc()
	m.KissOfDeathTotal.WithLabelValues("time.example.org", "RATE").Inc()

	names := gatherNames(t, r.GetRegistry())
	for _, name := range []string{
		"ntp_exporter_build_info",
		"ntp_exporter_cycles_total",
		"ntp_exporter_cycle_duration_seconds",
		"ntp_kiss_of_death_total",
		"go_goroutines",
		"process_cpu_seconds_total",
	} {
		assert.True(t, names[name], "missing %s", name)
	}
}

func TestNewRegistryWithConfig_Namespace(t *testing.T) {
	r := NewRegistryWithConfig("custom", "probe")
	require.NoError(t, r.Register())
	r.GetMetrics().InvalidResponsesTotal.WithLabelValues("time.example.org", "kod").Inc()
	r.GetMetrics().ExporterCyclesTotal.WithLabelValues("success").Inc()

	names := gatherNames(t, r.GetRegistry())
	assert.True(t, names["custom_probe_invalid_responses_total"])
	assert.True(t, names["custom_exporter_cycles_total"], "exporter metrics keep the exporter subsystem")
}

func TestRegistry_ConstLabels(t *testing.T) {
	r := NewRegistryWithConfig("ntp", "", map[string]string{"site": "par1"})
	require.NoError(t, r.Register())
	r.GetMetrics().ExporterCyclesTotal.WithLabelValues("success").Inc()
	require.NoError(t, r.RegisterCollector(prometheus.NewGauge(prometheus.GaugeOpts{Name: "ntp_extra"})))

	families, err := r.GetRegistry().Gather()
	require.NoError(t, err)
	for _, mf := range families {
		hasSite := false
		for _, lp := range mf.GetMetric()[0].GetLabel() {
			if lp.GetName() == "site" && lp.GetValue() == "par1" {
				hasSite = true
			}
		}
		isRuntime := strings.HasPrefix(mf.GetName(), "go_") || strings.HasPrefix(mf.GetName(), "process_")
		assert.Equal(t, !isRuntime, hasSite, mf.GetName())
	}
}

func TestNTPMetrics_Lint(t *testing.T) {
	m := NewNTPMetricsWithConfig("ntp", "")
	m.ExporterCyclesTotal.WithLabelValues("success").Inc()
	m.KissOfDeathTotal.WithLabelValues("time.example.org", "RATE").Inc()
	m.InvalidResponsesTotal.WithLabelValues("time.example.org", "kod").Inc()
	problems, err := testutil.CollectAndLint(m)
	require.NoError(t, err)
	assert.Empty(t, problems)
}

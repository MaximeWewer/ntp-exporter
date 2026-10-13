package collector

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// exposedMetrics returns the names of every metric the exporter can expose,
// with the suffixes of histograms
func exposedMetrics(t *testing.T) map[string]bool {
	t.Helper()
	cfg := config.DefaultConfig()
	ch := make(chan *prometheus.Desc, 256)
	NewExporter(cfg, metrics.NewNTPMetricsWithConfig("ntp", "")).Describe(ch)
	metrics.NewNTPMetricsWithConfig("ntp", "").Describe(ch)
	d := newDescs("ntp", "")
	ch <- d.probeSuccess
	ch <- d.probeDuration
	close(ch)

	fqName := regexp.MustCompile(`fqName: "([^"]+)"`)
	names := map[string]bool{}
	for desc := range ch {
		m := fqName.FindStringSubmatch(desc.String())
		require.Len(t, m, 2, desc.String())
		names[m[1]] = true
		for _, suffix := range []string{"_bucket", "_sum", "_count"} {
			names[m[1]+suffix] = true
		}
	}
	return names
}

// metricRefs returns the ntp_ metric names referenced by the files
func metricRefs(t *testing.T, pattern string) map[string][]string {
	t.Helper()
	files, err := filepath.Glob(pattern)
	require.NoError(t, err)
	require.NotEmpty(t, files, pattern)

	ref := regexp.MustCompile(`\bntp_[a-z0-9_]+`)
	refs := map[string][]string{}
	for _, file := range files {
		content, err := os.ReadFile(file) //nolint:gosec // repository files
		require.NoError(t, err)
		for _, name := range ref.FindAllString(string(content), -1) {
			refs[name] = append(refs[name], filepath.Base(file))
		}
	}
	return refs
}

// The dashboards and the alert rules only query metrics the exporter exposes
func TestAssets_ReferenceExposedMetrics(t *testing.T) {
	exposed := exposedMetrics(t)

	for _, pattern := range []string{
		"../../grafana/*.json",
		"../../deployments/prometheus/*.yml",
		"../../deployments/kubernetes/helm/ntp-exporter/templates/prometheusrule.yaml",
	} {
		refs := metricRefs(t, pattern)
		var unknown []string
		for name, files := range refs {
			if !exposed[name] {
				unknown = append(unknown, name+" ("+strings.Join(files, ", ")+")")
			}
		}
		sort.Strings(unknown)
		assert.Empty(t, unknown, "%s references metrics the exporter does not expose", pattern)
	}
}

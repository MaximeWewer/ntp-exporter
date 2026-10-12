package collector

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/ntp/ntptest"
	"github.com/maximewewer/ntp-exporter/internal/probe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const probeTarget = "192.0.2.10"

func newProbeHandler(t *testing.T) (*ProbeHandler, *ntptest.MockNTPClient, *config.Config) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.NTP.Servers = nil
	cfg.ProbeEndpoint.Only = true
	cfg.NTP.SampleSpacing = time.Millisecond
	cfg.Metrics.Labels = map[string]string{"site": "lab"}
	mock := ntptest.NewMockNTPClient()
	p := probe.New(cfg, mock)
	p.SetResolver(identityResolver{})
	return NewProbeHandler(cfg, p), mock, cfg
}

func serveProbe(t *testing.T, h http.Handler, query string, header map[string]string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/probe?"+query, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body, err := io.ReadAll(rec.Body)
	require.NoError(t, err)
	return rec.Code, string(body)
}

func TestProbeHandler_Success(t *testing.T) {
	h, mock, _ := newProbeHandler(t)
	mock.SetupSuccessfulServer(probeTarget, 3*time.Millisecond, 2)

	code, body := serveProbe(t, h, "target="+probeTarget+"&samples=2", nil)

	require.Equal(t, http.StatusOK, code, body)
	assert.Equal(t, 2, mock.GetCallCount(probeTarget), "samples parameter sets the burst size")
	assert.Contains(t, body, `ntp_probe_success{site="lab"} 1`)
	assert.Contains(t, body, `ntp_probe_duration_seconds{site="lab"}`)
	assert.Contains(t, body, `ntp_up{server="192.0.2.10",site="lab"} 1`)
	assert.Contains(t, body, `ntp_offset_seconds{server="192.0.2.10",site="lab"} 0.003`)
	assert.Contains(t, body, `ntp_server_info{address="192.0.2.10"`)
}

func TestProbeHandler_DefaultSamples(t *testing.T) {
	h, mock, cfg := newProbeHandler(t)
	mock.SetupSuccessfulServer(probeTarget, time.Millisecond, 2)

	code, _ := serveProbe(t, h, "target="+probeTarget, nil)

	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, cfg.NTP.SamplesPerServer, mock.GetCallCount(probeTarget))
}

func TestProbeHandler_UnreachableTarget(t *testing.T) {
	h, mock, _ := newProbeHandler(t)
	mock.SetupUnreachableServer(probeTarget)

	code, body := serveProbe(t, h, "target="+probeTarget, nil)

	require.Equal(t, http.StatusOK, code, "a failed probe is reported in the metrics")
	assert.Contains(t, body, `ntp_probe_success{site="lab"} 0`)
	assert.Contains(t, body, `ntp_up{server="192.0.2.10",site="lab"} 0`)
	assert.NotContains(t, body, "ntp_offset_seconds{")
}

func TestProbeHandler_TargetWithPort(t *testing.T) {
	h, mock, _ := newProbeHandler(t)
	mock.SetupSuccessfulServer(probeTarget+":1123", time.Millisecond, 2)

	code, body := serveProbe(t, h, "target="+url.QueryEscape(probeTarget+":1123"), nil)

	require.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, `ntp_probe_success{site="lab"} 1`)
}

func TestProbeHandler_BadRequests(t *testing.T) {
	h, _, _ := newProbeHandler(t)
	for _, query := range []string{
		"",
		"target=",
		"target=" + url.QueryEscape("http://time.example.org"),
		"target=" + url.QueryEscape("time.example.org:0"),
		"target=" + url.QueryEscape("time.example.org:http"),
		"target=" + url.QueryEscape(":123"),
		"target=time.example.org&samples=0",
		"target=time.example.org&samples=21",
		"target=time.example.org&samples=many",
	} {
		code, _ := serveProbe(t, h, query, nil)
		assert.Equal(t, http.StatusBadRequest, code, query)
	}
}

func TestProbeHandler_FitsScrapeTimeout(t *testing.T) {
	h, mock, cfg := newProbeHandler(t)
	cfg.NTP.SampleSpacing = 2 * time.Second
	cfg.NTP.Timeout = time.Second
	mock.SetupSuccessfulServer(probeTarget, time.Millisecond, 2)

	start := time.Now()
	code, _ := serveProbe(t, h, "target="+probeTarget+"&samples=8",
		map[string]string{"X-Prometheus-Scrape-Timeout-Seconds": "3"})

	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, 1, mock.GetCallCount(probeTarget), "2.5s budget: one query timeout, no room for a 2s spacing")
	assert.Less(t, time.Since(start), 2*time.Second)
}

func TestProbeHandler_BusyReturns503(t *testing.T) {
	h, _, _ := newProbeHandler(t)
	for range cap(h.slots) {
		h.slots <- struct{}{}
	}

	code, _ := serveProbe(t, h, "target="+probeTarget,
		map[string]string{"X-Prometheus-Scrape-Timeout-Seconds": "0.2"})
	assert.Equal(t, http.StatusServiceUnavailable, code)
}

func TestProbeTimeout(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	assert.Equal(t, defaultProbeTimeout-probeTimeoutOffset, probeTimeout(req))

	req.Header.Set("X-Prometheus-Scrape-Timeout-Seconds", "15")
	assert.Equal(t, 15*time.Second-probeTimeoutOffset, probeTimeout(req))

	req.Header.Set("X-Prometheus-Scrape-Timeout-Seconds", "0.5")
	assert.Equal(t, 500*time.Millisecond, probeTimeout(req), "short timeouts are kept whole")

	req.Header.Set("X-Prometheus-Scrape-Timeout-Seconds", "nonsense")
	assert.Equal(t, defaultProbeTimeout-probeTimeoutOffset, probeTimeout(req))
}

func TestFitSamples(t *testing.T) {
	assert.Equal(t, 4, fitSamples(4, 9500*time.Millisecond, 5*time.Second, 0), "no spacing, no limit")
	assert.Equal(t, 3, fitSamples(4, 9500*time.Millisecond, 5*time.Second, 2*time.Second))
	assert.Equal(t, 4, fitSamples(4, 14500*time.Millisecond, 5*time.Second, 2*time.Second))
	assert.Equal(t, 1, fitSamples(4, time.Second, 5*time.Second, 2*time.Second))
}

func TestValidateTarget(t *testing.T) {
	for _, ok := range []string{"time.example.org", "time.example.org:123", "192.0.2.1", "2001:db8::1", "[2001:db8::1]:123"} {
		assert.NoError(t, validateTarget(ok), ok)
	}
	assert.Error(t, validateTarget(strings.Repeat("a", 254)))
}

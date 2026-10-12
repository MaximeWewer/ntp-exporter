package collector

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/ntp"
	"github.com/maximewewer/ntp-exporter/internal/probe"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// harness wires a mock client, a prober and an exporter registered in a registry
type harness struct {
	t        *testing.T
	cfg      *config.Config
	mock     *ntp.MockNTPClient
	client   ntp.NTPQuerier
	metrics  *metrics.NTPMetrics
	exporter *Exporter
	registry *prometheus.Registry
	prober   *probe.Prober
}

func newHarness(t *testing.T, servers ...string) *harness {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.NTP.Servers = servers
	cfg.NTP.SamplesPerServer = 3
	h := &harness{t: t, cfg: cfg, mock: ntp.NewMockNTPClient()}
	h.client = h.mock
	h.metrics = metrics.NewNTPMetrics()
	h.exporter = NewExporter(cfg, h.metrics)
	h.registry = prometheus.NewRegistry()
	h.registry.MustRegister(h.metrics, h.exporter)
	return h
}

// cycle runs one probe cycle and hands it to the exporter
func (h *harness) cycle() *probe.CycleResult {
	h.t.Helper()
	if h.prober == nil {
		h.prober = probe.New(h.cfg, h.client)
		h.prober.SetResolver(identityResolver{})
	}
	result := h.prober.Run(context.Background())
	h.exporter.Update(result)
	return result
}

// value returns the value of a series, and whether it exists
func (h *harness) value(name string, labels map[string]string) (float64, bool) {
	h.t.Helper()
	families, err := h.registry.Gather()
	require.NoError(h.t, err)
	for _, mf := range families {
		if mf.GetName() != name {
			continue
		}
	metric:
		for _, m := range mf.GetMetric() {
			got := map[string]string{}
			for _, lp := range m.GetLabel() {
				got[lp.GetName()] = lp.GetValue()
			}
			for k, v := range labels {
				if got[k] != v {
					continue metric
				}
			}
			switch {
			case m.GetGauge() != nil:
				return m.GetGauge().GetValue(), true
			case m.GetCounter() != nil:
				return m.GetCounter().GetValue(), true
			}
		}
	}
	return 0, false
}

func (h *harness) mustValue(name string, labels map[string]string) float64 {
	h.t.Helper()
	v, ok := h.value(name, labels)
	require.True(h.t, ok, "series %s%v not found", name, labels)
	return v
}

func (h *harness) absent(name string, labels map[string]string) {
	h.t.Helper()
	_, ok := h.value(name, labels)
	assert.False(h.t, ok, "series %s%v must not be exposed", name, labels)
}

func srv(s string) map[string]string { return map[string]string{"server": s} }

func TestExporter_ValidResponseIsPublished(t *testing.T) {
	const server = "good.example.org"
	h := newHarness(t, server)
	h.mock.SetupSuccessfulServer(server, 5*time.Millisecond, 2)

	h.cycle()

	assert.Equal(t, 1.0, h.mustValue("ntp_up", srv(server)))
	assert.InDelta(t, 0.005, h.mustValue("ntp_offset_seconds", srv(server)), 1e-9)
	assert.Equal(t, 2.0, h.mustValue("ntp_stratum", srv(server)))
	assert.InDelta(t, 3600, h.mustValue("ntp_reference_age_seconds", srv(server)), 1, "mock reference time is 1h old")
	assert.InDelta(t, 0.015, h.mustValue("ntp_root_distance_seconds", srv(server)), 1e-9)
	h.absent("ntp_reference_timestamp_seconds", nil)
	assert.Equal(t, 0.0, h.mustValue("ntp_packet_loss_ratio", srv(server)))
	assert.Equal(t, 3.0, h.mustValue("ntp_samples_count", srv(server)))
	assert.Equal(t, 1.0, h.mustValue("ntp_response_valid", srv(server)))
	h.absent("ntp_server_trust_score", nil)
	h.absent("ntp_invalid_responses_total", srv(server))
	assert.InDelta(t, float64(time.Now().Unix()), h.mustValue("ntp_last_success_timestamp_seconds", srv(server)), 5)
	assert.Equal(t, 1.0, h.mustValue("ntp_exporter_scrapes_total", map[string]string{"status": "success"}))
}

func TestExporter_InvalidResponsesAreNotPublished(t *testing.T) {
	mutate := func(m *ntp.MockNTPClient, s string, f func(*ntp.Response)) {
		m.SetupSuccessfulServer(s, 5*time.Millisecond, 2)
		resp, _ := m.Query(context.Background(), s)
		bad := *resp
		f(&bad)
		m.SetResponse(s, &bad)
	}
	tests := []struct {
		name  string
		setup func(m *ntp.MockNTPClient, server string)
	}{
		{"kiss of death", func(m *ntp.MockNTPClient, s string) { m.SetupKoDServer(s, "RATE") }},
		{"stratum 16", func(m *ntp.MockNTPClient, s string) { m.SetupInvalidStratumServer(s) }},
		{"leap indicator 3", func(m *ntp.MockNTPClient, s string) {
			mutate(m, s, func(r *ntp.Response) { r.LeapIndicator = 3 })
		}},
		{"validation error", func(m *ntp.MockNTPClient, s string) {
			mutate(m, s, func(r *ntp.Response) { r.ValidateError = errors.New("invalid dispersion") })
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const server = "bad.example.org"
			h := newHarness(t, server)
			tt.setup(h.mock, server)

			h.cycle()

			assert.Equal(t, 1.0, h.mustValue("ntp_up", srv(server)), "server answered")
			assert.Equal(t, 1.0, h.mustValue("ntp_packet_loss_ratio", srv(server)), "invalid samples count as lost")
			assert.Equal(t, 0.0, h.mustValue("ntp_response_valid", srv(server)))
			h.absent("ntp_offset_seconds", srv(server))
			h.absent("ntp_stratum", srv(server))
			h.absent("ntp_rtt_seconds", srv(server))
			h.absent("ntp_last_success_timestamp_seconds", srv(server))
		})
	}
}

func TestExporter_UnreachableServerHasNoStaleMeasurement(t *testing.T) {
	const server = "flaky.example.org"
	h := newHarness(t, server)
	h.mock.SetupSuccessfulServer(server, 5*time.Millisecond, 2)
	h.cycle()
	lastSuccess := h.mustValue("ntp_last_success_timestamp_seconds", srv(server))

	h.mock.SetupUnreachableServer(server)
	h.cycle()

	assert.Equal(t, 0.0, h.mustValue("ntp_up", srv(server)))
	assert.Equal(t, lastSuccess, h.mustValue("ntp_last_success_timestamp_seconds", srv(server)))
	assert.Equal(t, 1.0, h.mustValue("ntp_packet_loss_ratio", srv(server)))
	assert.Equal(t, 0.0, h.mustValue("ntp_samples_count", srv(server)))
	h.absent("ntp_offset_seconds", srv(server))
	h.absent("ntp_stratum", srv(server))
	assert.Equal(t, 1.0, h.mustValue("ntp_exporter_scrapes_total", map[string]string{"status": "failure"}))
}

func TestExporter_Pool(t *testing.T) {
	const member = "192.0.2.20"
	h := newHarness(t)
	h.mock.SetupSuccessfulServer(member, -3*time.Millisecond, 2)
	h.cfg.NTP.Pools = []config.PoolConfig{{Name: member, Strategy: "all", MaxServers: 4}}

	h.cycle()

	pool := map[string]string{"pool": member}
	assert.Equal(t, 1.0, h.mustValue("ntp_pool_servers_total", pool))
	assert.Equal(t, 1.0, h.mustValue("ntp_pool_servers_active", pool))
	assert.InDelta(t, -0.003, h.mustValue("ntp_pool_offset_median_seconds", pool), 1e-9)
	h.absent("ntp_pool_best_offset_seconds", nil)
	assert.Equal(t, 1.0, h.mustValue("ntp_up", srv(member)))
}

func TestExporter_RemovedTargetsDisappear(t *testing.T) {
	h := newHarness(t)
	h.mock.SetupKoDServer("192.0.2.1", "RATE")
	h.mock.SetupSuccessfulServer("192.0.2.2", time.Millisecond, 2)

	// First cycle probes 192.0.2.1, second one probes 192.0.2.2 only, as when
	// a pool member disappears from the DNS answer
	h.cfg.NTP.Servers = []string{"192.0.2.1"}
	h.cycle()
	assert.Equal(t, 3.0, h.mustValue("ntp_kiss_of_death_total", map[string]string{"server": "192.0.2.1", "code": "RATE"}))

	h.cfg.NTP.Servers = []string{"192.0.2.2"}
	h.cycle()

	h.absent("ntp_up", srv("192.0.2.1"))
	h.absent("ntp_packet_loss_ratio", srv("192.0.2.1"))
	h.absent("ntp_kiss_of_death_total", srv("192.0.2.1"))
	h.absent("ntp_invalid_responses_total", srv("192.0.2.1"))
	assert.Equal(t, 1.0, h.mustValue("ntp_up", srv("192.0.2.2")))
}

// partialMock answers only the first `answered` queries of each QueryMultiple call
type partialMock struct {
	*ntp.MockNTPClient
	answered int
}

func (p *partialMock) QueryMultiple(ctx context.Context, server string, count int) ([]*ntp.Response, error) {
	resp, err := p.Query(ctx, server)
	if err != nil {
		return nil, err
	}
	out := make([]*ntp.Response, 0, count)
	for i := 0; i < count && i < p.answered; i++ {
		out = append(out, resp)
	}
	if len(out) == 0 {
		return nil, errors.New("all queries failed")
	}
	return out, nil
}

func TestExporter_PacketLoss(t *testing.T) {
	const server = "lossy.example.org"
	for _, adaptive := range []bool{false, true} {
		h := newHarness(t, server)
		h.mock.SetupSuccessfulServer(server, 5*time.Millisecond, 2)
		h.cfg.NTP.AdaptiveSampling.Enabled = adaptive
		h.client = &partialMock{MockNTPClient: h.mock, answered: 2}

		h.cycle()

		assert.InDelta(t, 1.0/3.0, h.mustValue("ntp_packet_loss_ratio", srv(server)), 1e-9, "adaptive=%v", adaptive)
	}
}

type fakeKernel struct {
	state *ntp.KernelTimex
	err   error
}

func (f fakeKernel) Read() (*ntp.KernelTimex, error) { return f.state, f.err }

func newKernelHarness(t *testing.T, state *ntp.KernelTimex, servers ...string) *harness {
	t.Helper()
	h := newHarness(t, servers...)
	h.cfg.NTP.Kernel.Enabled = true
	h.exporter.kernelReader = fakeKernel{state: state}
	h.exporter.nodeName = "node1"
	return h
}

func TestExporter_Kernel(t *testing.T) {
	h := newKernelHarness(t, &ntp.KernelTimex{
		MaxError: 16 * time.Millisecond, EstError: time.Millisecond, TAI: 37, State: ntp.TIME_OK,
		Status: ntp.STA_NANO, Frequency: 65536 * 10,
	}, "a.example.org")
	h.mock.SetupSuccessfulServer("a.example.org", 2*time.Millisecond, 2)

	h.cycle()

	node := map[string]string{"node": "node1"}
	assert.Equal(t, 1.0, h.mustValue("ntp_kernel_sync_status", node))
	assert.Equal(t, 1.0, h.mustValue("ntp_kernel_state", map[string]string{"node": "node1", "state": "synchronized"}))
	assert.InDelta(t, 0.016, h.mustValue("ntp_kernel_max_error_seconds", node), 1e-9)
	assert.InDelta(t, 0.001, h.mustValue("ntp_kernel_est_error_seconds", node), 1e-9)
	assert.Equal(t, 37.0, h.mustValue("ntp_kernel_tai_offset_seconds", node))
	assert.Equal(t, 10.0, h.mustValue("ntp_kernel_frequency_ppm", node))
	h.absent("ntp_kernel_divergence_seconds", nil)
	h.absent("ntp_kernel_coherence_score", nil)
	h.absent("ntp_kernel_status_code", nil)
}

func TestExporter_KernelErrorBoundViolation(t *testing.T) {
	// The mock servers have a 15ms root distance: the measurement uncertainty
	tests := []struct {
		name     string
		offset   time.Duration
		maxError time.Duration
		estError time.Duration
		violated map[string]float64
	}{
		{"bounds respected", 10 * time.Millisecond, 16 * time.Millisecond, 2 * time.Millisecond,
			map[string]float64{"maxerror": 0, "esterror": 0}},
		{"uncertainty absorbs the gap", 20 * time.Millisecond, 16 * time.Millisecond, 2 * time.Millisecond,
			map[string]float64{"maxerror": 0, "esterror": 1}},
		{"kernel underestimates its error", 80 * time.Millisecond, 16 * time.Millisecond, 2 * time.Millisecond,
			map[string]float64{"maxerror": 1, "esterror": 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newKernelHarness(t, &ntp.KernelTimex{MaxError: tt.maxError, EstError: tt.estError}, "a.example.org", "192.0.2.50")
			h.cfg.NTP.Servers = []string{"a.example.org"}
			h.cfg.NTP.Pools = []config.PoolConfig{{Name: "192.0.2.50", Strategy: "all", MaxServers: 4}}
			h.mock.SetupSuccessfulServer("a.example.org", tt.offset, 2)
			h.mock.SetupSuccessfulServer("192.0.2.50", tt.offset, 2)

			h.cycle()

			for _, server := range []string{"a.example.org", "192.0.2.50"} {
				for bound, want := range tt.violated {
					assert.Equal(t, want, h.mustValue("ntp_kernel_error_bound_violated",
						map[string]string{"node": "node1", "server": server, "bound": bound}), "%s %s", server, bound)
				}
			}
		})
	}
}

func TestExporter_KernelUnsyncHasNoBound(t *testing.T) {
	h := newKernelHarness(t, &ntp.KernelTimex{Status: ntp.STA_UNSYNC, MaxError: 16 * time.Second}, "a.example.org")
	h.mock.SetupSuccessfulServer("a.example.org", time.Millisecond, 2)

	h.cycle()

	assert.Equal(t, 0.0, h.mustValue("ntp_kernel_sync_status", map[string]string{"node": "node1"}))
	h.absent("ntp_kernel_error_bound_violated", nil)
}

func TestExporter_KernelViolationOnly(t *testing.T) {
	h := newKernelHarness(t, &ntp.KernelTimex{MaxError: time.Millisecond}, "a.example.org")
	h.cfg.NTP.Kernel.ViolationOnly = true
	h.mock.SetupSuccessfulServer("a.example.org", 50*time.Millisecond, 2)

	h.cycle()

	assert.Equal(t, 1.0, h.mustValue("ntp_kernel_sync_status", map[string]string{"node": "node1"}))
	assert.Equal(t, 1.0, h.mustValue("ntp_kernel_error_bound_violated", map[string]string{"bound": "maxerror"}))
	h.absent("ntp_kernel_max_error_seconds", nil)
	h.absent("ntp_kernel_state", nil)
}

func TestExporter_KernelReadError(t *testing.T) {
	h := newHarness(t)
	h.exporter.kernelReader = fakeKernel{err: errors.New("unsupported")}

	h.exporter.Update(&probe.CycleResult{})
	h.absent("ntp_kernel_offset_seconds", nil)
}

func TestExporter_Lint(t *testing.T) {
	h := newHarness(t, "a.example.org")
	h.mock.SetupSuccessfulServer("a.example.org", time.Millisecond, 2)
	h.cycle()

	problems, err := testutil.CollectAndLint(h.exporter)
	require.NoError(t, err)
	for _, p := range problems {
		assert.False(t, strings.Contains(p.Text, "duplicate"), p.Text)
	}
}

// identityResolver resolves every host to itself, so that the mock keyed by
// host name receives the queries
type identityResolver struct{}

func (identityResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	return []string{host}, nil
}

func TestExporter_KoDDenySuspension(t *testing.T) {
	const server = "deny.example.org"
	h := newHarness(t, server)
	h.mock.SetupKoDServer(server, "DENY")

	h.cycle()
	h.cycle()

	assert.Equal(t, 1.0, h.mustValue("ntp_target_suspended", map[string]string{"server": server, "code": "DENY"}))
	assert.Equal(t, 0.0, h.mustValue("ntp_up", srv(server)))
	assert.Equal(t, 3.0, h.mustValue("ntp_kiss_of_death_total", map[string]string{"server": server, "code": "DENY"}),
		"the suspended cycle sends nothing and counts nothing")
}

func TestExporter_KoDRateSkippedCycleIsNotCountedTwice(t *testing.T) {
	const server = "rate.example.org"
	h := newHarness(t, server)
	h.mock.SetupKoDServer(server, "RATE")

	h.cycle()
	h.cycle()

	assert.Equal(t, 3.0, h.mustValue("ntp_kiss_of_death_total", map[string]string{"server": server, "code": "RATE"}))
	assert.Equal(t, 1.0, h.mustValue("ntp_up", srv(server)), "skipped target keeps its previous state")
}

// burstMock returns a fixed sequence of samples for each burst
type burstMock struct {
	*ntp.MockNTPClient
	samples []*ntp.Response
}

func (b *burstMock) QueryMultiple(_ context.Context, server string, _ int) ([]*ntp.Response, error) {
	out := make([]*ntp.Response, len(b.samples))
	for i, s := range b.samples {
		c := *s
		c.Server = server
		out[i] = &c
	}
	return out, nil
}

func TestExporter_OffsetFromLowestDelaySample(t *testing.T) {
	const server = "filter.example.org"
	h := newHarness(t, server)
	h.cfg.NTP.SamplesPerServer = 4
	mk := func(offset, rtt time.Duration) *ntp.Response {
		return &ntp.Response{Stratum: 2, Offset: offset, RTT: rtt, ReferenceTime: time.Now()}
	}
	h.client = &burstMock{MockNTPClient: h.mock, samples: []*ntp.Response{
		mk(5*time.Millisecond, 40*time.Millisecond),
		mk(2*time.Millisecond, 12*time.Millisecond),
		mk(8*time.Millisecond, 60*time.Millisecond),
		mk(3*time.Millisecond, 15*time.Millisecond),
	}}
	h.cfg.NTP.MaxClockOffset = 2500 * time.Microsecond

	h.cycle()

	assert.InDelta(t, 0.002, h.mustValue("ntp_offset_seconds", srv(server)), 1e-9)
	assert.InDelta(t, 0.012, h.mustValue("ntp_rtt_seconds", srv(server)), 1e-9)
	assert.InDelta(t, 0.004, h.mustValue("ntp_offset_median_seconds", srv(server)), 1e-9)
	assert.Equal(t, 0.0, h.mustValue("ntp_clock_offset_exceeded", srv(server)), "threshold applies to the best sample")
	assert.InDelta(t, 0.0039158, h.mustValue("ntp_jitter_seconds", srv(server)), 1e-6)
	assert.Greater(t, h.mustValue("ntp_rtt_stddev_seconds", srv(server)), 0.02)
	assert.Greater(t, h.mustValue("ntp_offset_stddev_seconds", srv(server)), 0.0)
	h.absent("ntp_asymmetry_seconds", nil)
	h.absent("ntp_stability_seconds", nil)
}

func TestExporter_ServerInfo(t *testing.T) {
	h := newHarness(t, "time.example.org")
	h.mock.SetupSuccessfulServer("time.example.org", time.Millisecond, 1) // refid "NIST"
	h.mock.SetupSuccessfulServer("192.0.2.40", time.Millisecond, 2)
	h.cfg.NTP.Pools = []config.PoolConfig{{Name: "192.0.2.40", Strategy: "all", MaxServers: 4}}

	h.cycle()

	assert.Equal(t, 1.0, h.mustValue("ntp_server_info", map[string]string{
		"server": "time.example.org", "address": "time.example.org", "pool": "",
		"refid": "NIST", "stratum": "1", "leap": "0", "version": "4",
	}))
	assert.Equal(t, 1.0, h.mustValue("ntp_server_info", map[string]string{
		"server": "192.0.2.40", "pool": "192.0.2.40", "refid": "78.73.83.84", "stratum": "2",
	}))
}

func TestExporter_Consensus(t *testing.T) {
	servers := []string{"a.example.org", "b.example.org", "c.example.org", "bad.example.org"}
	h := newHarness(t, servers...)
	for _, s := range servers[:3] {
		h.mock.SetupSuccessfulServer(s, 2*time.Millisecond, 2) // root distance 15ms
	}
	h.mock.SetupSuccessfulServer("bad.example.org", 800*time.Millisecond, 2)

	h.cycle()

	assert.InDelta(t, 0.002, h.mustValue("ntp_consensus_offset_seconds", nil), 1e-9)
	assert.Equal(t, 4.0, h.mustValue("ntp_consensus_servers", nil))
	assert.Equal(t, 3.0, h.mustValue("ntp_consensus_truechimers", nil))
	assert.Equal(t, 1.0, h.mustValue("ntp_falseticker", srv("bad.example.org")))
	assert.Equal(t, 0.0, h.mustValue("ntp_falseticker", srv("a.example.org")))
	assert.InDelta(t, 0.798, h.mustValue("ntp_offset_from_consensus_seconds", srv("bad.example.org")), 1e-9)
}

func TestExporter_ConsensusDisabled(t *testing.T) {
	h := newHarness(t, "a.example.org", "b.example.org")
	h.mock.SetupSuccessfulServer("a.example.org", time.Millisecond, 2)
	h.mock.SetupSuccessfulServer("b.example.org", time.Millisecond, 2)

	h.cycle()
	h.absent("ntp_consensus_offset_seconds", nil)

	enabled := false
	h.cfg.NTP.Servers = append(h.cfg.NTP.Servers, "c.example.org")
	h.mock.SetupSuccessfulServer("c.example.org", time.Millisecond, 2)
	h.cfg.NTP.Consensus.Enabled = &enabled
	h.cycle()
	h.absent("ntp_consensus_offset_seconds", nil)
}

func TestExporter_InvalidResponseReasons(t *testing.T) {
	h := newHarness(t, "kod.example.org", "lossy.example.org")
	h.mock.SetupKoDServer("kod.example.org", "RATE")
	h.mock.SetupSuccessfulServer("lossy.example.org", time.Millisecond, 2)
	h.client = &partialMock{MockNTPClient: h.mock, answered: 1}

	h.cycle()

	assert.Equal(t, 1.0, h.mustValue("ntp_invalid_responses_total", map[string]string{"server": "kod.example.org", "reason": "kod"}))
	assert.Equal(t, 2.0, h.mustValue("ntp_invalid_responses_total", map[string]string{"server": "kod.example.org", "reason": "timeout"}))
	assert.Equal(t, 2.0, h.mustValue("ntp_invalid_responses_total", map[string]string{"server": "lossy.example.org", "reason": "timeout"}))
	h.absent("ntp_invalid_responses_total", map[string]string{"server": "lossy.example.org", "reason": "kod"})
}

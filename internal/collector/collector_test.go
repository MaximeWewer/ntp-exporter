package collector

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/ntp"
	"github.com/maximewewer/ntp-exporter/internal/probe"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cycle runs one probe cycle against the mock and publishes it with every publisher
func cycle(t *testing.T, cfg *config.Config, client ntp.NTPQuerier, m *metrics.NTPMetrics, pubs ...Publisher) *probe.CycleResult {
	t.Helper()
	result := probe.New(cfg, client).Run(context.Background())
	registry := NewRegistry()
	for _, p := range pubs {
		registry.Register(p)
	}
	registry.PublishAll(result)
	return result
}

func testConfig(servers ...string) *config.Config {
	cfg := config.DefaultConfig()
	cfg.NTP.Servers = servers
	cfg.NTP.SamplesPerServer = 3
	return cfg
}

func TestBaseCollector_ValidResponseIsPublished(t *testing.T) {
	const server = "good.example.org"
	mock := ntp.NewMockNTPClient()
	mock.SetupSuccessfulServer(server, 5*time.Millisecond, 2)
	cfg := testConfig(server)
	m := metrics.NewNTPMetrics()

	cycle(t, cfg, mock, m, NewBaseCollector(cfg, m))

	assert.Equal(t, 1.0, testutil.ToFloat64(m.ServerReachable.WithLabelValues(server)))
	assert.Equal(t, 1, testutil.CollectAndCount(m.OffsetSeconds))
	assert.Equal(t, 2.0, testutil.ToFloat64(m.Stratum.WithLabelValues(server)))
	assert.InDelta(t, 0.005, testutil.ToFloat64(m.OffsetSeconds.WithLabelValues(server, "2", "4")), 1e-9)
}

func TestBaseCollector_InvalidResponsesAreNotPublished(t *testing.T) {
	setValidateError := func(m *ntp.MockNTPClient, s string, mutate func(*ntp.Response)) {
		m.SetupSuccessfulServer(s, 5*time.Millisecond, 2)
		resp, _ := m.Query(context.Background(), s)
		bad := *resp
		mutate(&bad)
		m.SetResponse(s, &bad)
	}
	tests := []struct {
		name  string
		setup func(m *ntp.MockNTPClient, server string)
	}{
		{"kiss of death", func(m *ntp.MockNTPClient, s string) { m.SetupKoDServer(s, "RATE") }},
		{"stratum 16", func(m *ntp.MockNTPClient, s string) { m.SetupInvalidStratumServer(s) }},
		{"leap indicator 3", func(m *ntp.MockNTPClient, s string) {
			setValidateError(m, s, func(r *ntp.Response) { r.LeapIndicator = 3 })
		}},
		{"validation error", func(m *ntp.MockNTPClient, s string) {
			setValidateError(m, s, func(r *ntp.Response) { r.ValidateError = errors.New("invalid dispersion") })
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const server = "bad.example.org"
			mock := ntp.NewMockNTPClient()
			tt.setup(mock, server)
			cfg := testConfig(server)
			m := metrics.NewNTPMetrics()

			cycle(t, cfg, mock, m, NewBaseCollector(cfg, m))

			assert.Equal(t, 1.0, testutil.ToFloat64(m.ServerReachable.WithLabelValues(server)), "server answered")
			assert.Equal(t, 0, testutil.CollectAndCount(m.OffsetSeconds), "offset must not be published")
			assert.Equal(t, 0, testutil.CollectAndCount(m.Stratum), "stratum must not be published")
			assert.Equal(t, 0, testutil.CollectAndCount(m.RTTSeconds), "rtt must not be published")
		})
	}
}

func TestBaseCollector_UnreachableServer(t *testing.T) {
	const server = "down.example.org"
	mock := ntp.NewMockNTPClient()
	mock.SetupUnreachableServer(server)
	cfg := testConfig(server)
	m := metrics.NewNTPMetrics()

	cycle(t, cfg, mock, m, NewBaseCollector(cfg, m))

	assert.Equal(t, 0.0, testutil.ToFloat64(m.ServerReachable.WithLabelValues(server)))
	assert.Equal(t, 1.0, testutil.ToFloat64(m.ExporterScrapesTotal.WithLabelValues("failure")))
}

func TestBaseCollector_Pool(t *testing.T) {
	const member = "192.0.2.20"
	mock := ntp.NewMockNTPClient()
	mock.SetupSuccessfulServer(member, -3*time.Millisecond, 2)
	cfg := testConfig()
	cfg.NTP.Pools = []config.PoolConfig{{Name: member, Strategy: "all", MaxServers: 4}}
	m := metrics.NewNTPMetrics()

	cycle(t, cfg, mock, m, NewBaseCollector(cfg, m))

	assert.Equal(t, 1.0, testutil.ToFloat64(m.PoolServersTotal.WithLabelValues(member)))
	assert.Equal(t, 1.0, testutil.ToFloat64(m.PoolServersActive.WithLabelValues(member)))
	assert.InDelta(t, -0.003, testutil.ToFloat64(m.PoolBestOffsetSeconds.WithLabelValues(member)), 1e-9)
	assert.Equal(t, 1.0, testutil.ToFloat64(m.ServerReachable.WithLabelValues(member)))
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

func TestQualityCollector_PacketLoss(t *testing.T) {
	const server = "lossy.example.org"
	for _, adaptive := range []bool{false, true} {
		mock := ntp.NewMockNTPClient()
		mock.SetupSuccessfulServer(server, 5*time.Millisecond, 2)
		cfg := testConfig(server)
		cfg.NTP.AdaptiveSampling.Enabled = adaptive
		m := metrics.NewNTPMetrics()

		cycle(t, cfg, &partialMock{MockNTPClient: mock, answered: 2}, m, NewQualityCollector(cfg, m))

		assert.InDelta(t, 1.0/3.0, testutil.ToFloat64(m.PacketLossRatio.WithLabelValues(server)), 1e-9, "adaptive=%v", adaptive)
	}
}

func TestQualityCollector_TotalLossIsPublished(t *testing.T) {
	const server = "down.example.org"
	for _, adaptive := range []bool{false, true} {
		mock := ntp.NewMockNTPClient()
		mock.SetupSuccessfulServer(server, 5*time.Millisecond, 2)
		cfg := testConfig(server)
		cfg.NTP.AdaptiveSampling.Enabled = adaptive
		m := metrics.NewNTPMetrics()
		q := NewQualityCollector(cfg, m)

		cycle(t, cfg, mock, m, q)
		require.Equal(t, 0.0, testutil.ToFloat64(m.PacketLossRatio.WithLabelValues(server)))

		mock.SetupUnreachableServer(server)
		cycle(t, cfg, mock, m, q)
		assert.Equal(t, 1.0, testutil.ToFloat64(m.PacketLossRatio.WithLabelValues(server)), "adaptive=%v", adaptive)
		assert.Equal(t, 0.0, testutil.ToFloat64(m.SamplesCount.WithLabelValues(server)))
	}
}

func TestSecurityCollector_KissOfDeath(t *testing.T) {
	const server = "kod.example.org"
	mock := ntp.NewMockNTPClient()
	mock.SetupKoDServer(server, "RATE")
	cfg := testConfig(server)
	m := metrics.NewNTPMetrics()

	cycle(t, cfg, mock, m, NewSecurityCollector(cfg, m))

	assert.Equal(t, 3.0, testutil.ToFloat64(m.KissOfDeathTotal.WithLabelValues(server, "RATE")))
	assert.Equal(t, 3.0, testutil.ToFloat64(m.MalformedResponsesTotal.WithLabelValues(server)))
	assert.Less(t, testutil.ToFloat64(m.ServerTrustScore.WithLabelValues(server)), 1.0)
}

type fakeKernel struct {
	state *ntp.KernelTimex
	err   error
}

func (f fakeKernel) Read() (*ntp.KernelTimex, error) { return f.state, f.err }

func TestHybridCollector_CorrelatesServersAndPools(t *testing.T) {
	mock := ntp.NewMockNTPClient()
	mock.SetupSuccessfulServer("a.example.org", 2*time.Millisecond, 2)
	mock.SetupSuccessfulServer("192.0.2.30", 2*time.Millisecond, 2)
	cfg := testConfig("a.example.org")
	cfg.NTP.Pools = []config.PoolConfig{{Name: "192.0.2.30", Strategy: "all", MaxServers: 4}}
	cfg.NTP.EnableKernel = true
	m := metrics.NewNTPMetrics()
	h := NewHybridCollector(cfg, m)
	h.kernelReader = fakeKernel{state: &ntp.KernelTimex{SyncStatus: "synchronized"}}
	h.nodeName = "node1"

	cycle(t, cfg, mock, m, h)

	assert.Equal(t, 2, testutil.CollectAndCount(m.NTPKernelDivergence), "servers and pool members are correlated")
	assert.InDelta(t, 0.002, testutil.ToFloat64(m.NTPKernelDivergence.WithLabelValues("node1", "a.example.org")), 1e-9)
}

func TestHybridCollector_KernelReadError(t *testing.T) {
	cfg := testConfig("a.example.org")
	m := metrics.NewNTPMetrics()
	h := NewHybridCollector(cfg, m)
	h.kernelReader = fakeKernel{err: errors.New("unsupported")}

	h.Publish(&probe.CycleResult{})
	assert.Equal(t, 0, testutil.CollectAndCount(m.KernelOffsetSeconds))
}

func TestCalculateCoherence(t *testing.T) {
	assert.Equal(t, 1.0, calculateCoherence(0.0005, 0))
	assert.InDelta(t, 0.9, calculateCoherence(0.005, 0), 1e-9)
	assert.InDelta(t, 0.7, calculateCoherence(0.010, 0), 1e-9)
	assert.InDelta(t, 0.5, calculateCoherence(0.050, 0), 0.15)
	assert.Less(t, calculateCoherence(1, 0), 0.01)
}

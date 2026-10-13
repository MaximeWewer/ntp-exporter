package probe

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/ntp"
	"github.com/maximewewer/ntp-exporter/internal/ntp/ntptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestConfig(servers ...string) *config.Config {
	cfg := config.DefaultConfig()
	cfg.NTP.Servers = servers
	cfg.NTP.SamplesPerServer = 3
	return cfg
}

func TestProber_QueriesEachTargetOncePerCycle(t *testing.T) {
	mock := ntptest.NewMockNTPClient()
	mock.SetupSuccessfulServer("a.example.org", time.Millisecond, 2)
	mock.SetupSuccessfulServer("b.example.org", 2*time.Millisecond, 1)

	result := newProber(newTestConfig("a.example.org", "b.example.org"), mock).Run(context.Background())

	require.Len(t, result.Measurements, 2)
	// One burst of samples_per_server queries per target, shared by every metric family
	assert.Equal(t, 3, mock.GetCallCount("a.example.org"))
	assert.Equal(t, 3, mock.GetCallCount("b.example.org"))

	for _, m := range result.Measurements {
		assert.True(t, m.Reachable())
		assert.Equal(t, 3, m.Sent)
		assert.Len(t, m.Responses, 3)
		assert.NotNil(t, m.Best())
		assert.NoError(t, m.Err)
	}
}

func TestProber_UnreachableTarget(t *testing.T) {
	mock := ntptest.NewMockNTPClient()
	mock.SetupUnreachableServer("down.example.org")

	result := newProber(newTestConfig("down.example.org"), mock).Run(context.Background())

	require.Len(t, result.Measurements, 1)
	m := result.Measurements[0]
	assert.False(t, m.Reachable())
	assert.Error(t, m.Err)
	assert.Nil(t, m.Best())
	assert.Equal(t, 1.0, m.Stats().PacketLossRatio)
}

func TestProber_InvalidResponsesAreNotBest(t *testing.T) {
	mock := ntptest.NewMockNTPClient()
	mock.SetupKoDServer("kod.example.org", "RATE")

	m := newProber(newTestConfig("kod.example.org"), mock).Run(context.Background()).Measurements[0]

	assert.True(t, m.Reachable())
	assert.Nil(t, m.Best())
	assert.Equal(t, 1.0, m.Stats().PacketLossRatio, "invalid responses count as lost")
	assert.Equal(t, 1, mock.GetCallCount("kod.example.org"), "the burst stops at the Kiss-o'-Death")
	assert.Equal(t, 1, m.Sent)
}

// dualStackResolver returns an IPv6 and an IPv4 address for every host
type dualStackResolver struct{}

func (dualStackResolver) LookupHost(context.Context, string) ([]string, error) {
	return []string{"2001:db8::1", "192.0.2.1"}, nil
}

func TestProber_KissOfDeathOnFamilyProbeStopsBurst(t *testing.T) {
	mock := ntptest.NewMockNTPClient()
	mock.SetupKoDServer("2001:db8::1", "DENY")
	mock.SetupSuccessfulServer("192.0.2.1", time.Millisecond, 2)
	p := New(newTestConfig("dual.example.org"), mock)
	p.SetResolver(dualStackResolver{})
	p.families.hasRoute = func(string) bool { return true }

	m := p.Run(context.Background()).Measurements[0]

	assert.Equal(t, 1, mock.GetCallCount("2001:db8::1"), "no burst after the Kiss-o'-Death")
	assert.Equal(t, 0, mock.GetCallCount("192.0.2.1"), "the other family is not tried either")
	assert.Equal(t, 1, m.Sent)
	require.Len(t, m.Responses, 1)
	assert.Equal(t, "DENY", m.Responses[0].KissCode)
}

func TestProber_PoolMembers(t *testing.T) {
	mock := ntptest.NewMockNTPClient()
	mock.SetupSuccessfulServer("192.0.2.10", time.Millisecond, 2)

	cfg := newTestConfig()
	cfg.NTP.Pools = []config.PoolConfig{{Name: "192.0.2.10", Strategy: "best_n", MaxServers: 4}}

	result := newProber(cfg, mock).Run(context.Background())

	require.Len(t, result.Pools, 1)
	assert.Equal(t, []string{"192.0.2.10"}, result.Pools[0].Members)
	require.Len(t, result.PoolMeasurements("192.0.2.10"), 1)
	assert.Equal(t, "192.0.2.10", result.Measurements[0].Target.Pool)
}

func TestProber_StopsOnCancelledContext(t *testing.T) {
	mock := ntptest.NewMockNTPClient()
	mock.SetupSuccessfulServer("a.example.org", time.Millisecond, 2)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := newProber(newTestConfig("a.example.org"), mock).Run(ctx)
	assert.Empty(t, result.Measurements)
}

// concurrencyMock records the maximum number of bursts in flight
type concurrencyMock struct {
	*ntptest.MockNTPClient
	mu       sync.Mutex
	inFlight int
	max      int
}

func (c *concurrencyMock) QueryMultiple(ctx context.Context, server string, count int) ([]*ntp.Response, int, error) {
	c.mu.Lock()
	c.inFlight++
	if c.inFlight > c.max {
		c.max = c.inFlight
	}
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.inFlight--
		c.mu.Unlock()
	}()
	time.Sleep(20 * time.Millisecond)
	return c.MockNTPClient.QueryMultiple(ctx, server, count)
}

func TestProber_BoundedConcurrency(t *testing.T) {
	mock := &concurrencyMock{MockNTPClient: ntptest.NewMockNTPClient()}
	servers := make([]string, 30)
	for i := range servers {
		servers[i] = fmt.Sprintf("s%d.example.org", i)
		mock.SetupSuccessfulServer(servers[i], time.Millisecond, 2)
	}
	cfg := newTestConfig(servers...)
	cfg.NTP.SamplesPerServer = 1
	cfg.NTP.MaxConcurrency = 10

	result := newProber(cfg, mock).Run(context.Background())

	require.Len(t, result.Measurements, 30)
	assert.LessOrEqual(t, mock.max, 10)
	assert.Greater(t, mock.max, 1, "targets must be probed in parallel")
	for i, m := range result.Measurements {
		assert.Equal(t, servers[i], m.Target.Server, "measurements keep the target order")
		assert.True(t, m.Reachable())
	}
}

func TestProber_SlowTargetDoesNotBlockCycle(t *testing.T) {
	mock := ntptest.NewMockNTPClient()
	mock.SetupSuccessfulServer("fast.example.org", time.Millisecond, 2)
	mock.SetupSuccessfulServer("slow.example.org", time.Millisecond, 2)
	mock.SetDelay("slow.example.org", time.Hour)

	p := newProber(newTestConfig("slow.example.org", "fast.example.org"), mock)
	p.cycleTimeout = 200 * time.Millisecond

	start := time.Now()
	result := p.Run(context.Background())

	assert.Less(t, time.Since(start), time.Second)
	assert.True(t, result.Overrun)
	assert.False(t, result.Measurements[0].Reachable())
	assert.True(t, result.Measurements[1].Reachable())
}

func TestCycleTimeout(t *testing.T) {
	assert.Equal(t, 29*time.Second, CycleTimeout(30*time.Second))
	assert.Equal(t, 4500*time.Millisecond, CycleTimeout(5*time.Second))
}

// identityResolver resolves every host to itself, so that the mock keyed by
// host name receives the queries
type identityResolver struct{}

func (identityResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	return []string{host}, nil
}

func newProber(cfg *config.Config, client ntp.NTPQuerier) *Prober {
	p := New(cfg, client)
	p.SetResolver(identityResolver{})
	return p
}

// rotatingResolver returns a different address at each lookup, like pool.ntp.org
type rotatingResolver struct {
	mu    sync.Mutex
	addrs []string
	calls int
}

func (r *rotatingResolver) LookupHost(_ context.Context, _ string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	addr := r.addrs[r.calls%len(r.addrs)]
	r.calls++
	return []string{addr}, nil
}

func TestProber_BurstIsPinnedToOneAddress(t *testing.T) {
	mock := ntptest.NewMockNTPClient()
	mock.SetupSuccessfulServer("192.0.2.1", time.Millisecond, 2)
	mock.SetupSuccessfulServer("192.0.2.2", time.Millisecond, 2)
	resolver := &rotatingResolver{addrs: []string{"192.0.2.1", "192.0.2.2"}}

	p := New(newTestConfig("pool.ntp.org"), mock)
	p.SetResolver(resolver)

	first := p.Run(context.Background()).Measurements[0]
	assert.Equal(t, "192.0.2.1", first.Target.Address)
	assert.Equal(t, 3, mock.GetCallCount("192.0.2.1"), "every sample of the burst goes to the same address")
	assert.Equal(t, 0, mock.GetCallCount("192.0.2.2"))
	for _, r := range first.Responses {
		assert.Equal(t, "pool.ntp.org", r.Server, "responses keep the configured name")
	}

	second := p.Run(context.Background()).Measurements[0]
	assert.Equal(t, "192.0.2.2", second.Target.Address, "the name is resolved again at each cycle")
	assert.Equal(t, 2, resolver.calls)
}

func TestProber_ServerWithPort(t *testing.T) {
	mock := ntptest.NewMockNTPClient()
	mock.SetupSuccessfulServer("192.0.2.1:1123", time.Millisecond, 2)
	p := New(newTestConfig("ntp.example.org:1123"), mock)
	p.SetResolver(&rotatingResolver{addrs: []string{"192.0.2.1"}})

	m := p.Run(context.Background()).Measurements[0]
	assert.Equal(t, "192.0.2.1:1123", m.Target.Address)
	assert.True(t, m.Reachable())
}

type failingResolver struct{}

func (failingResolver) LookupHost(context.Context, string) ([]string, error) {
	return nil, fmt.Errorf("no such host")
}

func TestProber_ResolutionFailure(t *testing.T) {
	p := New(newTestConfig("unknown.example.org"), ntptest.NewMockNTPClient())
	p.SetResolver(failingResolver{})

	m := p.Run(context.Background()).Measurements[0]
	assert.False(t, m.Reachable())
	assert.ErrorContains(t, m.Err, "no such host")
	assert.Equal(t, 1.0, m.Stats().PacketLossRatio)
}

func sample(offset, rtt time.Duration) *ntp.Response {
	return &ntp.Response{Stratum: 2, Offset: offset, RTT: rtt}
}

func TestMeasurement_BestIsLowestDelaySample(t *testing.T) {
	m := &Measurement{Sent: 4, Responses: []*ntp.Response{
		sample(5*time.Millisecond, 40*time.Millisecond),
		sample(2*time.Millisecond, 12*time.Millisecond),
		sample(8*time.Millisecond, 60*time.Millisecond),
		sample(3*time.Millisecond, 15*time.Millisecond),
	}}

	best := m.Best()
	require.NotNil(t, best)
	assert.Equal(t, 2*time.Millisecond, best.Offset)
	assert.Equal(t, 12*time.Millisecond, best.RTT)
	assert.Equal(t, 4*time.Millisecond, m.Stats().MedianOffset)
}

func TestMeasurement_BestIgnoresInvalidSamples(t *testing.T) {
	kod := sample(0, time.Millisecond)
	kod.KissCode = "RATE"
	m := &Measurement{Sent: 2, Responses: []*ntp.Response{kod, sample(7*time.Millisecond, 30*time.Millisecond)}}

	assert.Equal(t, 7*time.Millisecond, m.Best().Offset)
}

// fakeNTS returns a prepared result for every burst
type fakeNTS struct {
	result ntp.NTSResult
	hosts  []string
}

func (f *fakeNTS) Burst(_ context.Context, host string, count int) ntp.NTSResult {
	f.hosts = append(f.hosts, host)
	r := f.result
	if r.Sent == 0 && r.Err == nil {
		r.Sent = count
	}
	return r
}

func TestProber_NTSTarget(t *testing.T) {
	expiry := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	f := &fakeNTS{result: ntp.NTSResult{
		Responses: []*ntp.Response{sample(time.Millisecond, 5*time.Millisecond), sample(2*time.Millisecond, 9*time.Millisecond)},
		State:     ntp.NTSState{KESuccess: true, CertExpiry: expiry, Address: "192.0.2.1:123"},
	}}
	mock := ntptest.NewMockNTPClient()
	p := newProber(newTestConfig("nts://nts.example.org"), mock)
	p.SetNTSClient(f)

	m := p.Run(context.Background()).Measurements[0]
	assert.Equal(t, []string{"nts.example.org"}, f.hosts)
	assert.Equal(t, "192.0.2.1:123", m.Target.Address, "the NTP server comes from the key exchange")
	require.NotNil(t, m.NTS)
	assert.Equal(t, expiry, m.NTS.CertExpiry)
	assert.Equal(t, 3, m.Sent)
	assert.Equal(t, time.Millisecond, m.Best().Offset)
	assert.Equal(t, "nts://nts.example.org", m.Responses[0].Server)
	assert.Equal(t, 0, mock.GetCallCount("nts.example.org"), "no plain NTP query to an NTS server")
}

func TestProber_NTSKeyExchangeFailure(t *testing.T) {
	f := &fakeNTS{result: ntp.NTSResult{Err: fmt.Errorf("key exchange failed"), State: ntp.NTSState{}}}
	p := newProber(newTestConfig("nts://nts.example.org"), ntptest.NewMockNTPClient())
	p.SetNTSClient(f)

	m := p.Run(context.Background()).Measurements[0]
	assert.False(t, m.Reachable())
	assert.ErrorContains(t, m.Err, "key exchange")
	assert.Equal(t, 0, m.Sent)
	assert.False(t, m.NTS.KESuccess)
}

func TestProber_ProbeStateIsApartFromConfiguredTargets(t *testing.T) {
	static := &fakeNTS{result: ntp.NTSResult{Responses: []*ntp.Response{sample(0, time.Millisecond)}}}
	onDemand := &fakeNTS{result: ntp.NTSResult{Responses: []*ntp.Response{sample(0, time.Millisecond)}}}
	p := newProber(newTestConfig("nts://static.example.org"), ntptest.NewMockNTPClient())
	p.nts = static
	p.probeNTS = onDemand

	p.Run(context.Background())
	p.Probe(context.Background(), "nts://other.example.org", 1)

	assert.Equal(t, []string{"static.example.org"}, static.hosts)
	assert.Equal(t, []string{"other.example.org"}, onDemand.hosts, "/probe targets use their own NTS sessions")
}

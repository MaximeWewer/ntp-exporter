package probe

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/ntp"
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
	mock := ntp.NewMockNTPClient()
	mock.SetupSuccessfulServer("a.example.org", time.Millisecond, 2)
	mock.SetupSuccessfulServer("b.example.org", 2*time.Millisecond, 1)

	result := New(newTestConfig("a.example.org", "b.example.org"), mock).Run(context.Background())

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
	mock := ntp.NewMockNTPClient()
	mock.SetupUnreachableServer("down.example.org")

	result := New(newTestConfig("down.example.org"), mock).Run(context.Background())

	require.Len(t, result.Measurements, 1)
	m := result.Measurements[0]
	assert.False(t, m.Reachable())
	assert.Error(t, m.Err)
	assert.Nil(t, m.Best())
	assert.Equal(t, 1.0, m.Stats().PacketLossRatio)
}

func TestProber_InvalidResponsesAreNotBest(t *testing.T) {
	mock := ntp.NewMockNTPClient()
	mock.SetupKoDServer("kod.example.org", "RATE")

	m := New(newTestConfig("kod.example.org"), mock).Run(context.Background()).Measurements[0]

	assert.True(t, m.Reachable())
	assert.Nil(t, m.Best())
	assert.Equal(t, 1.0, m.Stats().PacketLossRatio, "invalid responses count as lost")
}

func TestProber_PoolMembers(t *testing.T) {
	mock := ntp.NewMockNTPClient()
	mock.SetupSuccessfulServer("192.0.2.10", time.Millisecond, 2)

	cfg := newTestConfig()
	cfg.NTP.Pools = []config.PoolConfig{{Name: "192.0.2.10", Strategy: "best_n", MaxServers: 4}}

	result := New(cfg, mock).Run(context.Background())

	require.Len(t, result.Pools, 1)
	assert.Equal(t, []string{"192.0.2.10"}, result.Pools[0].Members)
	require.Len(t, result.PoolMeasurements("192.0.2.10"), 1)
	assert.Equal(t, "192.0.2.10", result.Measurements[0].Target.Pool)
}

func TestProber_StopsOnCancelledContext(t *testing.T) {
	mock := ntp.NewMockNTPClient()
	mock.SetupSuccessfulServer("a.example.org", time.Millisecond, 2)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := New(newTestConfig("a.example.org"), mock).Run(ctx)
	assert.Empty(t, result.Measurements)
}

// concurrencyMock records the maximum number of bursts in flight
type concurrencyMock struct {
	*ntp.MockNTPClient
	mu       sync.Mutex
	inFlight int
	max      int
}

func (c *concurrencyMock) QueryMultiple(ctx context.Context, server string, count int) ([]*ntp.Response, error) {
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
	mock := &concurrencyMock{MockNTPClient: ntp.NewMockNTPClient()}
	servers := make([]string, 30)
	for i := range servers {
		servers[i] = fmt.Sprintf("s%d.example.org", i)
		mock.SetupSuccessfulServer(servers[i], time.Millisecond, 2)
	}
	cfg := newTestConfig(servers...)
	cfg.NTP.SamplesPerServer = 1
	cfg.NTP.MaxConcurrency = 10

	result := New(cfg, mock).Run(context.Background())

	require.Len(t, result.Measurements, 30)
	assert.LessOrEqual(t, mock.max, 10)
	assert.Greater(t, mock.max, 1, "targets must be probed in parallel")
	for i, m := range result.Measurements {
		assert.Equal(t, servers[i], m.Target.Server, "measurements keep the target order")
		assert.True(t, m.Reachable())
	}
}

func TestProber_SlowTargetDoesNotBlockCycle(t *testing.T) {
	mock := ntp.NewMockNTPClient()
	mock.SetupSuccessfulServer("fast.example.org", time.Millisecond, 2)
	mock.SetupSuccessfulServer("slow.example.org", time.Millisecond, 2)
	mock.SetDelay("slow.example.org", time.Hour)

	p := New(newTestConfig("slow.example.org", "fast.example.org"), mock)
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

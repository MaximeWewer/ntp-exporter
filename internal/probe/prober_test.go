package probe

import (
	"context"
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

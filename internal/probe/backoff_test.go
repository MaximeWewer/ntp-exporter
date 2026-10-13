package probe

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/ntp"
	"github.com/maximewewer/ntp-exporter/internal/ntp/ntptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func kodMeasurement(code string) *Measurement {
	return &Measurement{Responses: []*ntp.Response{{KissCode: code}}}
}

func okMeasurement() *Measurement {
	return &Measurement{Responses: []*ntp.Response{{Stratum: 2}}}
}

func newTestBackoff() (*Backoff, *fakeClock) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	b := NewBackoff(time.Minute, 8*time.Minute, time.Hour)
	b.now = clock.now
	return b, clock
}

func TestBackoff_RateDoublesIntervalUpToMax(t *testing.T) {
	b, clock := newTestBackoff()

	expected := []time.Duration{2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 8 * time.Minute}
	for _, interval := range expected {
		require.True(t, b.Allow("s").Allowed)
		b.Record("s", kodMeasurement(KoDRate))

		clock.advance(interval - time.Minute)
		assert.False(t, b.Allow("s").Allowed, "still backing off before %s", interval)
		clock.advance(time.Minute)
	}
}

func TestBackoff_RecoversProgressively(t *testing.T) {
	b, clock := newTestBackoff()
	b.Record("s", kodMeasurement(KoDRate)) // x2
	clock.advance(2 * time.Minute)
	b.Record("s", kodMeasurement(KoDRate)) // x4
	clock.advance(4 * time.Minute)

	require.True(t, b.Allow("s").Allowed)
	b.Record("s", okMeasurement()) // back to x2
	clock.advance(time.Minute)
	assert.False(t, b.Allow("s").Allowed)
	clock.advance(time.Minute)
	require.True(t, b.Allow("s").Allowed)

	b.Record("s", okMeasurement()) // back to normal
	clock.advance(time.Minute)
	assert.True(t, b.Allow("s").Allowed)
	assert.Empty(t, b.states, "state is dropped once back to normal")
}

func TestBackoff_DenySuspendsTarget(t *testing.T) {
	for _, code := range []string{KoDDeny, KoDRstr} {
		b, clock := newTestBackoff()
		b.Record("s", kodMeasurement(code))

		d := b.Allow("s")
		assert.False(t, d.Allowed)
		assert.Equal(t, code, d.SuspendCode)

		clock.advance(59 * time.Minute)
		assert.Equal(t, code, b.Allow("s").SuspendCode)

		clock.advance(time.Minute)
		assert.True(t, b.Allow("s").Allowed, "%s suspension ends after the configured duration", code)
	}
}

func TestBackoff_ForgetKeepsActiveStates(t *testing.T) {
	b, clock := newTestBackoff()
	b.Record("a", kodMeasurement(KoDDeny))
	b.Record("b", kodMeasurement(KoDDeny))
	b.Record("c", kodMeasurement(KoDRate))

	// b and c left the cycle (a pool member gone from the DNS answer): they
	// stay suspended or backing off until their state expires
	b.Forget(map[string]bool{"a": true})
	assert.Equal(t, KoDDeny, b.Allow("b").SuspendCode, "a returning member is still suspended")
	assert.False(t, b.Allow("c").Allowed, "a returning member is still backing off")

	clock.advance(time.Hour)
	b.Forget(map[string]bool{"a": true})
	assert.Contains(t, b.states, "a")
	assert.NotContains(t, b.states, "b")
	assert.NotContains(t, b.states, "c")
}

func TestBackoff_StatesAreBounded(t *testing.T) {
	b, clock := newTestBackoff()
	for i := 0; i < maxBackoffStates; i++ {
		b.Record(fmt.Sprintf("old-%d", i), kodMeasurement(KoDRate))
	}
	require.Len(t, b.states, maxBackoffStates)

	// Full of active states: a new target is not recorded
	b.Record("new", kodMeasurement(KoDDeny))
	assert.Len(t, b.states, maxBackoffStates)
	assert.NotContains(t, b.states, "new")

	// Expired states make room
	clock.advance(3 * time.Minute)
	b.Record("new", kodMeasurement(KoDDeny))
	assert.Equal(t, KoDDeny, b.Allow("new").SuspendCode)
	assert.Len(t, b.states, 1)
}

func TestProber_ProbeHonorsKissOfDeath(t *testing.T) {
	mock := ntptest.NewMockNTPClient()
	mock.SetupKoDServer("deny.example.org", KoDDeny)
	mock.SetupKoDServer("rate.example.org", KoDRate)
	p := newProber(newTestConfig(), mock)

	for _, server := range []string{"deny.example.org", "rate.example.org"} {
		first := p.Probe(context.Background(), server, 4)
		assert.Len(t, first.Responses, 1, server)

		second := p.Probe(context.Background(), server, 4)
		assert.Equal(t, 1, mock.GetCallCount(server), "%s: no query while the Kiss-o'-Death applies", server)
		assert.False(t, second.Reachable(), server)
		assert.Equal(t, 0, second.Sent, server)
	}
	assert.Equal(t, KoDDeny, p.Probe(context.Background(), "deny.example.org", 4).SuspendCode)
	assert.True(t, p.Probe(context.Background(), "rate.example.org", 4).Skipped)

	// /probe state does not touch the configured targets
	assert.Empty(t, p.backoff.states)
}

func TestProber_KoDRateSkipsNextCycle(t *testing.T) {
	mock := ntptest.NewMockNTPClient()
	mock.SetupKoDServer("rate.example.org", KoDRate)
	p := newProber(newTestConfig("rate.example.org"), mock)

	first := p.Run(context.Background()).Measurements[0]
	assert.False(t, first.Skipped)
	assert.Equal(t, 1, mock.GetCallCount("rate.example.org"), "the burst stops at the Kiss-o'-Death")

	second := p.Run(context.Background()).Measurements[0]
	assert.True(t, second.Skipped, "target is not queried while backing off")
	assert.Equal(t, 1, mock.GetCallCount("rate.example.org"))
	assert.Len(t, second.Responses, 1, "previous measurement is carried over")
}

func TestProber_KoDDenySuspendsTarget(t *testing.T) {
	mock := ntptest.NewMockNTPClient()
	mock.SetupKoDServer("deny.example.org", KoDDeny)
	p := newProber(newTestConfig("deny.example.org"), mock)

	p.Run(context.Background())
	second := p.Run(context.Background()).Measurements[0]

	assert.Equal(t, KoDDeny, second.SuspendCode)
	assert.False(t, second.Reachable())
	assert.Equal(t, 1, mock.GetCallCount("deny.example.org"), "suspended target is not queried")
}

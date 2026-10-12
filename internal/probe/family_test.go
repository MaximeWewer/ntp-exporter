package probe

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/ntp/ntptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	v4 = "192.0.2.1"
	v6 = "2001:db8::1"
)

// staticResolver always returns the same addresses
type staticResolver []string

func (r staticResolver) LookupHost(context.Context, string) ([]string, error) {
	return r, nil
}

// routes simulates the routing table of the host
func routes(ipv4, ipv6 bool) func(string) bool {
	return func(family string) bool {
		return (family == familyIPv4 && ipv4) || (family == familyIPv6 && ipv6)
	}
}

func newDualStackProber(t *testing.T, family string, mock *ntptest.MockNTPClient, addrs ...string) *Prober {
	t.Helper()
	cfg := newTestConfig("dual.example.org")
	cfg.NTP.IPFamily = family
	cfg.NTP.SampleSpacing = time.Millisecond
	p := New(cfg, mock)
	p.SetResolver(staticResolver(addrs))
	p.families.hasRoute = routes(true, true)
	return p
}

func TestAddressFamily(t *testing.T) {
	assert.Equal(t, familyIPv4, addressFamily(v4))
	assert.Equal(t, familyIPv4, addressFamily("192.0.2.1:123"))
	assert.Equal(t, familyIPv6, addressFamily(v6))
	assert.Equal(t, familyIPv6, addressFamily("[2001:db8::1]:123"))
}

func TestFamilies_Candidates(t *testing.T) {
	f := newFamilies(config.IPFamilyAuto)
	f.hasRoute = routes(true, true)
	f.refresh()

	addrs := []string{v6, "2001:db8::2", v4, "192.0.2.2"}
	assert.Equal(t, []string{v6, v4}, f.candidates("k", addrs), "one address per family, resolver order")

	f.remember("k", v4)
	assert.Equal(t, []string{v4, v6}, f.candidates("k", addrs), "the family that answered last comes first")

	f.forget(map[string]bool{})
	assert.Equal(t, []string{v6, v4}, f.candidates("k", addrs))
}

func TestFamilies_Filter(t *testing.T) {
	addrs := []string{v6, v4}

	ipv4 := newFamilies(config.IPFamilyIPv4)
	assert.Equal(t, []string{v4}, ipv4.filter(addrs))
	assert.Empty(t, ipv4.filter([]string{v6}))

	ipv6 := newFamilies(config.IPFamilyIPv6)
	assert.Equal(t, []string{v6}, ipv6.filter(addrs))

	auto := newFamilies("")
	auto.hasRoute = routes(true, false)
	auto.refresh()
	assert.Equal(t, []string{v4}, auto.filter(addrs), "a family without route is skipped")
	assert.Equal(t, []string{v6}, auto.filter([]string{v6}), "without any routable address, keep them all")
}

func TestHasRoute(t *testing.T) {
	// The result depends on the host: only check that it does not panic
	_ = hasRoute(familyIPv4)
	_ = hasRoute(familyIPv6)
}

func TestProber_FallsBackToTheOtherFamily(t *testing.T) {
	mock := ntptest.NewMockNTPClient()
	mock.SetupUnreachableServer(v6)
	mock.SetupSuccessfulServer(v4, time.Millisecond, 2)
	p := newDualStackProber(t, config.IPFamilyAuto, mock, v6, v4)

	m := p.Run(context.Background()).Measurements[0]
	assert.Equal(t, v4, m.Target.Address)
	assert.Equal(t, 1, mock.GetCallCount(v6), "a single query is lost on the broken family")
	assert.Equal(t, 3, mock.GetCallCount(v4))
	assert.Equal(t, 4, m.Sent)
	assert.Len(t, m.Responses, 3)

	m = p.Run(context.Background()).Measurements[0]
	assert.Equal(t, v4, m.Target.Address)
	assert.Equal(t, 1, mock.GetCallCount(v6), "the family that answered is tried first next cycle")
	assert.Equal(t, 3, m.Sent)
	assert.Equal(t, 0.0, m.Stats().PacketLossRatio)
}

func TestProber_DualStackUsesFirstFamily(t *testing.T) {
	mock := ntptest.NewMockNTPClient()
	mock.SetupSuccessfulServer(v6, time.Millisecond, 2)
	mock.SetupSuccessfulServer(v4, time.Millisecond, 2)
	p := newDualStackProber(t, config.IPFamilyAuto, mock, v6, v4)

	m := p.Run(context.Background()).Measurements[0]
	assert.Equal(t, v6, m.Target.Address)
	assert.Equal(t, 3, mock.GetCallCount(v6), "the first answer is the first sample of the burst")
	assert.Equal(t, 0, mock.GetCallCount(v4))
	assert.Equal(t, 3, m.Sent)
}

func TestProber_BothFamiliesDown(t *testing.T) {
	mock := ntptest.NewMockNTPClient()
	mock.SetupUnreachableServer(v6)
	mock.SetupUnreachableServer(v4)
	p := newDualStackProber(t, config.IPFamilyAuto, mock, v6, v4)

	m := p.Run(context.Background()).Measurements[0]
	assert.False(t, m.Reachable())
	assert.Error(t, m.Err)
	assert.Equal(t, 4, m.Sent)
	assert.Equal(t, 1.0, m.Stats().PacketLossRatio)
}

func TestProber_SkipsFamilyWithoutRoute(t *testing.T) {
	mock := ntptest.NewMockNTPClient()
	mock.SetupSuccessfulServer(v4, time.Millisecond, 2)
	p := newDualStackProber(t, config.IPFamilyAuto, mock, v6, v4)
	p.families.hasRoute = routes(true, false)

	m := p.Run(context.Background()).Measurements[0]
	assert.Equal(t, v4, m.Target.Address)
	assert.Equal(t, 0, mock.GetCallCount(v6))
	assert.Equal(t, 3, m.Sent)
}

func TestProber_ForcedFamily(t *testing.T) {
	mock := ntptest.NewMockNTPClient()
	mock.SetupSuccessfulServer(v6, time.Millisecond, 2)
	mock.SetupSuccessfulServer(v4, time.Millisecond, 2)

	m := newDualStackProber(t, config.IPFamilyIPv4, mock, v6, v4).Run(context.Background()).Measurements[0]
	assert.Equal(t, v4, m.Target.Address)
	assert.Equal(t, 0, mock.GetCallCount(v6))

	m = newDualStackProber(t, config.IPFamilyIPv6, mock, v4).Run(context.Background()).Measurements[0]
	assert.False(t, m.Reachable())
	assert.ErrorContains(t, m.Err, "no ipv6 address found")
}

func TestProber_CancelledDuringSpacing(t *testing.T) {
	mock := ntptest.NewMockNTPClient()
	mock.SetupSuccessfulServer(v4, time.Millisecond, 2)
	p := newDualStackProber(t, config.IPFamilyAuto, mock, v4, v6)
	p.cfg.NTP.SampleSpacing = time.Hour

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	m := p.Run(ctx).Measurements[0]
	require.Len(t, m.Responses, 1, "the first answer is kept")
	assert.Equal(t, 1, mock.GetCallCount(v4))
	assert.True(t, errors.Is(sleep(ctx, time.Hour), context.DeadlineExceeded))
}

func TestProber_PoolMembersFilteredByFamily(t *testing.T) {
	mock := ntptest.NewMockNTPClient()
	mock.SetupSuccessfulServer(v4, time.Millisecond, 2)
	cfg := newTestConfig()
	cfg.NTP.IPFamily = config.IPFamilyIPv4
	cfg.NTP.Pools = []config.PoolConfig{{Name: "pool.example.org", Strategy: "all", MaxServers: 4}}
	p := New(cfg, mock)

	members := p.families.filter([]string{v6, v4})
	assert.Equal(t, []string{v4}, members)
}

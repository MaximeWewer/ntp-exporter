package probe

import (
	"context"
	"testing"

	"github.com/maximewewer/ntp-exporter/internal/ntp"
	"github.com/maximewewer/ntp-exporter/internal/ntp/ntptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTargetPolicy_DefaultRefusesSensitiveAddresses(t *testing.T) {
	p, err := NewTargetPolicy(nil)
	require.NoError(t, err)

	for _, addr := range []string{
		"127.0.0.1", "[::1]:123", "169.254.169.254", "fe80::1", "fd00:ec2::254",
		"0.0.0.0", "::", "224.0.1.1", "ff02::101", "::ffff:127.0.0.1",
	} {
		assert.ErrorIs(t, p.Check("host", addr), ErrTargetForbidden, addr)
	}
	for _, addr := range []string{"192.0.2.1", "203.0.113.5:123", "2001:db8::1", "10.0.0.1"} {
		assert.NoError(t, p.Check("host", addr), addr)
	}
}

func TestTargetPolicy_AllowedTargets(t *testing.T) {
	p, err := NewTargetPolicy([]string{"192.0.2.0/24", "127.0.0.1/32", `.*\.ntp\.example\.org`})
	require.NoError(t, err)

	assert.NoError(t, p.Check("192.0.2.10", "192.0.2.10"), "in an allowed range")
	assert.NoError(t, p.Check("localhost", "127.0.0.1"), "explicitly allowed loopback")
	assert.NoError(t, p.Check("a.ntp.example.org", "203.0.113.5"), "allowed name")
	assert.NoError(t, p.Check("A.NTP.example.org", "203.0.113.5"), "names are case insensitive")

	assert.ErrorIs(t, p.Check("time.example.com", "203.0.113.5"), ErrTargetForbidden, "neither range nor name")
	assert.ErrorIs(t, p.Check("evil.ntp.example.org.attacker.net", "203.0.113.5"), ErrTargetForbidden, "the whole name must match")
	assert.ErrorIs(t, p.Check("a.ntp.example.org", "169.254.169.254"), ErrTargetForbidden, "an allowed name cannot reach a sensitive address")

	_, err = NewTargetPolicy([]string{"time.(example"})
	assert.Error(t, err)
}

func TestProber_ProbeAppliesTargetPolicy(t *testing.T) {
	mock := ntptest.NewMockNTPClient()
	mock.SetupSuccessfulServer("127.0.0.1", 0, 2)
	p := newProber(newTestConfig(), mock)
	policy, err := NewTargetPolicy(nil)
	require.NoError(t, err)
	p.SetTargetPolicy(policy)

	m := p.Probe(context.Background(), "127.0.0.1", 2)
	assert.ErrorIs(t, m.Err, ErrTargetForbidden)
	assert.Equal(t, 0, mock.GetCallCount("127.0.0.1"), "nothing is sent")

	// The configured servers are not restricted
	p.cfg.NTP.Servers = []string{"127.0.0.1"}
	assert.True(t, p.Run(context.Background()).Measurements[0].Reachable())
}

func TestProber_ProbeChecksNegotiatedNTSServer(t *testing.T) {
	f := &fakeNTS{result: ntp.NTSResult{
		Responses: []*ntp.Response{sample(0, 0)},
		State:     ntp.NTSState{KESuccess: true, Address: "169.254.169.254:123"},
	}}
	p := newProber(newTestConfig(), ntptest.NewMockNTPClient())
	p.SetNTSClient(f)
	policy, err := NewTargetPolicy(nil)
	require.NoError(t, err)
	p.SetTargetPolicy(policy)

	m := p.Probe(context.Background(), "nts://192.0.2.7", 1)
	assert.ErrorIs(t, m.Err, ErrTargetForbidden, "the key exchange cannot redirect to a sensitive address")

	m = p.Probe(context.Background(), "nts://127.0.0.1", 1)
	assert.ErrorIs(t, m.Err, ErrTargetForbidden)
	assert.Len(t, f.hosts, 1, "no key exchange with a sensitive host")
}

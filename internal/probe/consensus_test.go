package probe

import (
	"testing"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/ntp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func serverMeasurement(server string, offset, distance time.Duration) *Measurement {
	return &Measurement{
		Target:    Target{Server: server},
		Sent:      1,
		Responses: []*ntp.Response{{Stratum: 2, Offset: offset, RootDistance: distance, RTT: time.Millisecond}},
	}
}

func TestComputeConsensus_DetectsFalseticker(t *testing.T) {
	ms := time.Millisecond
	c := ComputeConsensus([]*Measurement{
		serverMeasurement("a", 2*ms, 10*ms),
		serverMeasurement("b", 4*ms, 10*ms),
		serverMeasurement("c", 3*ms, 10*ms),
		serverMeasurement("bad", 500*ms, 20*ms),
	})

	require.NotNil(t, c)
	assert.True(t, c.Majority)
	assert.Equal(t, 4, c.Servers)
	assert.Equal(t, 3, c.Truechimers)
	assert.Equal(t, map[string]bool{"bad": true}, c.Falsetickers)
	assert.Equal(t, 3*ms, c.Offset, "median of the truechimers")
}

func TestComputeConsensus_IndependentOfLocalClock(t *testing.T) {
	ms := time.Millisecond
	// The local clock is 1s off: every server shows a +1s offset
	c := ComputeConsensus([]*Measurement{
		serverMeasurement("a", 1000*ms, 5*ms),
		serverMeasurement("b", 1002*ms, 5*ms),
		serverMeasurement("c", 1001*ms, 5*ms),
	})

	require.NotNil(t, c)
	assert.Empty(t, c.Falsetickers, "a wrong local clock does not make servers falsetickers")
	assert.Equal(t, 1001*ms, c.Offset)
}

func TestComputeConsensus_NoMajorityFallsBackToMedian(t *testing.T) {
	ms := time.Millisecond
	c := ComputeConsensus([]*Measurement{
		serverMeasurement("a", 0, ms),
		serverMeasurement("b", 100*ms, ms),
		serverMeasurement("c", 200*ms, ms),
	})

	require.NotNil(t, c)
	assert.False(t, c.Majority)
	assert.Empty(t, c.Falsetickers)
	assert.Equal(t, 100*ms, c.Offset)
}

func TestComputeConsensus_NeedsThreeValidServers(t *testing.T) {
	kod := &Measurement{Target: Target{Server: "kod"}, Responses: []*ntp.Response{{KissCode: "RATE"}}}
	assert.Nil(t, ComputeConsensus([]*Measurement{
		serverMeasurement("a", 0, time.Millisecond),
		serverMeasurement("b", 0, time.Millisecond),
		kod,
	}))
}

func TestComputeConsensus_SameAddressVotesOnce(t *testing.T) {
	ms := time.Millisecond
	alias := serverMeasurement("ntp.example.org", 300*ms, 10*ms)
	alias.Target.Address = "192.0.2.9"
	member := serverMeasurement("192.0.2.9", 300*ms, 10*ms)
	member.Target.Address = "192.0.2.9"

	// Counted twice, the server reached under two names would tie with the
	// two others and prevent the majority
	c := ComputeConsensus([]*Measurement{
		serverMeasurement("a", 0, 10*ms),
		serverMeasurement("b", ms, 10*ms),
		alias,
		member,
	})

	require.NotNil(t, c)
	assert.Equal(t, 3, c.Servers)
	assert.True(t, c.Majority)
	assert.Equal(t, map[string]bool{"ntp.example.org": true}, c.Falsetickers)
}

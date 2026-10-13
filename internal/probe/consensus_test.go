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

func TestComputeConsensus_NoMajority(t *testing.T) {
	ms := time.Millisecond
	c := ComputeConsensus([]*Measurement{
		serverMeasurement("a", 0, ms),
		serverMeasurement("b", 100*ms, ms),
		serverMeasurement("c", 200*ms, ms),
	})

	require.NotNil(t, c)
	assert.False(t, c.Majority)
	assert.Empty(t, c.Falsetickers, "nobody is judged without majority")
	assert.Equal(t, 1, c.Truechimers, "no two servers agree")
}

func TestComputeConsensus_SplitIsNotAgreement(t *testing.T) {
	ms := time.Millisecond
	c := ComputeConsensus([]*Measurement{
		serverMeasurement("a", 0, ms),
		serverMeasurement("b", ms, ms),
		serverMeasurement("c", 300*ms, ms),
		serverMeasurement("d", 301*ms, ms),
	})

	require.NotNil(t, c)
	assert.False(t, c.Majority, "2 against 2 is no majority")
	assert.Equal(t, 2, c.Truechimers, "not 4 of 4")
	assert.Empty(t, c.Falsetickers)
}

// rotations returns every rotation of the measurements and their reverse, to
// check that the result does not depend on the order of the servers
func rotations(ms []*Measurement) [][]*Measurement {
	out := make([][]*Measurement, 0, 2*len(ms))
	for i := range ms {
		r := append(append([]*Measurement(nil), ms[i:]...), ms[:i]...)
		out = append(out, r)
		rev := make([]*Measurement, len(r))
		for j := range r {
			rev[j] = r[len(r)-1-j]
		}
		out = append(out, rev)
	}
	return out
}

func TestComputeConsensus_Selection(t *testing.T) {
	ms := time.Millisecond
	type srv struct {
		name             string
		offset, distance time.Duration
	}
	tests := []struct {
		name         string
		servers      []srv
		majority     bool
		falsetickers []string
		truechimers  int
	}{
		{"wide server bridging two narrow ones", []srv{
			{"a", 0, 50 * ms}, {"b", 10 * ms, 2 * ms}, {"c", -10 * ms, 2 * ms},
		}, true, nil, 3},
		{"interval touching the intersection is not enough", []srv{
			{"a", 0, 5 * ms}, {"b", ms, 5 * ms}, {"c", 40 * ms, 36 * ms},
		}, true, []string{"c"}, 2},
		{"nested intervals", []srv{
			{"a", 0, 100 * ms}, {"b", ms, 10 * ms}, {"c", 2 * ms, ms},
		}, true, nil, 3},
		{"four servers, one far", []srv{
			{"a", 0, 5 * ms}, {"b", ms, 5 * ms}, {"c", 2 * ms, 5 * ms}, {"d", 500 * ms, 5 * ms},
		}, true, []string{"d"}, 3},
		{"five servers, two far", []srv{
			{"a", 0, 5 * ms}, {"b", ms, 5 * ms}, {"c", 2 * ms, 5 * ms}, {"d", 500 * ms, 5 * ms}, {"e", -400 * ms, 5 * ms},
		}, true, []string{"d", "e"}, 3},
		{"three disjoint", []srv{
			{"a", 0, ms}, {"b", 100 * ms, ms}, {"c", 200 * ms, ms},
		}, false, nil, 1},
		{"identical servers", []srv{
			{"a", 5 * ms, ms}, {"b", 5 * ms, ms}, {"c", 5 * ms, ms},
		}, true, nil, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ms []*Measurement
			for _, s := range tt.servers {
				ms = append(ms, serverMeasurement(s.name, s.offset, s.distance))
			}
			want := map[string]bool{}
			for _, f := range tt.falsetickers {
				want[f] = true
			}
			for _, order := range rotations(ms) {
				c := ComputeConsensus(order)
				require.NotNil(t, c)
				assert.Equal(t, tt.majority, c.Majority)
				assert.Equal(t, want, c.Falsetickers)
				assert.Equal(t, tt.truechimers, c.Truechimers)
			}
		})
	}
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

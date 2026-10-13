package probe

import (
	"sort"
	"time"
)

// MinConsensusServers is the minimum number of valid servers needed to
// compute a consensus
const MinConsensusServers = 3

// Consensus is the agreement between servers measured during a cycle.
// It judges servers against each other, independently of the local clock.
type Consensus struct {
	// Offset is the consensus offset of the local clock
	Offset time.Duration
	// Servers is the number of valid servers taking part in the selection
	Servers int
	// Truechimers is the number of servers whose interval contains the consensus
	Truechimers int
	// Majority is set when the truechimers are a strict majority; without it
	// the consensus falls back to the median and no falseticker is reported
	Majority bool
	// Falsetickers lists the servers whose correctness interval does not
	// intersect the majority intersection
	Falsetickers map[string]bool
	// Offsets holds the offset of each valid server
	Offsets map[string]time.Duration
}

type interval struct {
	server string
	offset time.Duration
	lo, hi time.Duration
}

// ComputeConsensus applies Marzullo's intersection algorithm (the basis of
// the RFC 5905 selection algorithm) to the correctness intervals
// [offset - root distance, offset + root distance] of the valid servers.
// It returns nil with fewer than MinConsensusServers valid servers. A server
// reached through several names (a host name and a pool member resolving to
// the same address) votes once.
func ComputeConsensus(measurements []*Measurement) *Consensus {
	var intervals []interval
	voted := make(map[string]bool, len(measurements))
	for _, m := range measurements {
		best := m.Best()
		if best == nil || m.SuspendCode != "" {
			continue
		}
		if addr := m.Target.Address; addr != "" {
			if voted[addr] {
				continue
			}
			voted[addr] = true
		}
		distance := best.RootDistance
		if distance < 0 {
			distance = 0
		}
		intervals = append(intervals, interval{
			server: m.Target.Server,
			offset: best.Offset,
			lo:     best.Offset - distance,
			hi:     best.Offset + distance,
		})
	}

	if len(intervals) < MinConsensusServers {
		return nil
	}

	c := &Consensus{
		Servers:      len(intervals),
		Falsetickers: make(map[string]bool),
		Offsets:      make(map[string]time.Duration, len(intervals)),
	}
	for _, iv := range intervals {
		c.Offsets[iv.server] = iv.offset
	}

	lo, hi, count := maxIntersection(intervals)
	c.Majority = count*2 > len(intervals)

	var chimers []time.Duration
	for _, iv := range intervals {
		if c.Majority && (iv.hi < lo || iv.lo > hi) {
			c.Falsetickers[iv.server] = true
			continue
		}
		chimers = append(chimers, iv.offset)
	}

	c.Truechimers = len(chimers)
	c.Offset = medianDuration(chimers)
	return c
}

// maxIntersection returns the region covered by the largest number of
// intervals (Marzullo's algorithm) and that number
func maxIntersection(intervals []interval) (time.Duration, time.Duration, int) {
	type edge struct {
		at    time.Duration
		delta int
	}
	edges := make([]edge, 0, 2*len(intervals))
	for _, iv := range intervals {
		edges = append(edges, edge{iv.lo, +1}, edge{iv.hi, -1})
	}
	// Starts before ends at the same point: touching intervals intersect
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].at != edges[j].at {
			return edges[i].at < edges[j].at
		}
		return edges[i].delta > edges[j].delta
	})

	best, count := 0, 0
	var lo, hi time.Duration
	for i, e := range edges {
		count += e.delta
		if count > best {
			best = count
			lo = e.at
			hi = edges[i+1].at
		}
	}
	return lo, hi, best
}

func medianDuration(values []time.Duration) time.Duration {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	n := len(sorted)
	if n%2 == 0 {
		return (sorted[n/2-1] + sorted[n/2]) / 2
	}
	return sorted[n/2]
}

package probe

import (
	"math"
	"sort"
	"time"
)

// MinConsensusServers is the minimum number of valid servers needed to
// compute a consensus
const MinConsensusServers = 3

// minWeightDistance bounds the weight of a server in the combined offset, so
// that a zero root distance does not divide by zero
const minWeightDistance = time.Microsecond

// Consensus is the agreement between servers measured during a cycle.
// It judges servers against each other, independently of the local clock.
type Consensus struct {
	// Offset is the consensus offset of the local clock, only meaningful with
	// a majority
	Offset time.Duration
	// Servers is the number of valid servers taking part in the selection
	Servers int
	// Truechimers is the number of servers whose offset lies in the selected
	// intersection; without majority, the largest number of servers whose
	// intervals overlap
	Truechimers int
	// Majority is set when more than half of the servers agree; without it no
	// consensus offset nor falseticker is reported
	Majority bool
	// Falsetickers lists the servers whose offset lies outside the majority
	// intersection
	Falsetickers map[string]bool
	// Offsets holds the offset of each valid server
	Offsets map[string]time.Duration
}

type interval struct {
	server   string
	offset   time.Duration
	distance time.Duration
	lo, hi   time.Duration
}

// ComputeConsensus applies the RFC 5905 selection algorithm (§11.2.1, derived
// from Marzullo's algorithm) to the correctness intervals
// [offset - root distance, offset + root distance] of the valid servers, then
// combines the offsets of the truechimers weighted by 1/root distance
// (§11.2.3). It returns nil with fewer than MinConsensusServers valid
// servers. A server reached through several names (a host name and a pool
// member resolving to the same address) votes once.
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
		distance := max(best.RootDistance, 0)
		intervals = append(intervals, interval{
			server:   m.Target.Server,
			offset:   best.Offset,
			distance: distance,
			lo:       best.Offset - distance,
			hi:       best.Offset + distance,
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

	lo, hi, ok := selectIntersection(intervals)
	if !ok {
		// No majority: report how many servers agree at best, judge nobody
		c.Truechimers = maxOverlap(intervals)
		return c
	}

	c.Majority = true
	var weighted, weights float64
	for _, iv := range intervals {
		if iv.offset < lo || iv.offset > hi {
			c.Falsetickers[iv.server] = true
			continue
		}
		c.Truechimers++
		w := 1 / max(iv.distance, minWeightDistance).Seconds()
		weighted += w * float64(iv.offset)
		weights += w
	}
	c.Offset = time.Duration(math.Round(weighted / weights))
	return c
}

// selectIntersection returns the intersection of the RFC 5905 selection
// algorithm: for the smallest number f of falsetickers (f < n/2), the
// interval from the lowest point covered by n - f correctness intervals to
// the highest one, provided that at most f offsets lie outside of it.
// The result does not depend on the order of the servers.
func selectIntersection(intervals []interval) (time.Duration, time.Duration, bool) {
	n := len(intervals)
	los := make([]time.Duration, 0, n)
	his := make([]time.Duration, 0, n)
	for _, iv := range intervals {
		los = append(los, iv.lo)
		his = append(his, iv.hi)
	}
	sort.Slice(los, func(i, j int) bool { return los[i] < los[j] })
	sort.Slice(his, func(i, j int) bool { return his[i] < his[j] })

	for f := 0; 2*f < n; f++ {
		need := n - f
		lo, okLo := lowestCovered(los, his, need)
		hi, okHi := highestCovered(los, his, need)
		if !okLo || !okHi || lo > hi {
			continue
		}
		outside := 0
		for _, iv := range intervals {
			if iv.offset < lo || iv.offset > hi {
				outside++
			}
		}
		if outside <= f {
			return lo, hi, true
		}
	}
	return 0, 0, false
}

// lowestCovered returns the lowest point covered by at least need intervals.
// Coverage only rises at a lower bound: the candidates are the lower bounds.
func lowestCovered(los, his []time.Duration, need int) (time.Duration, bool) {
	for _, x := range los {
		if coverage(los, his, x) >= need {
			return x, true
		}
	}
	return 0, false
}

// highestCovered returns the highest point covered by at least need
// intervals. Coverage only falls after an upper bound: the candidates are the
// upper bounds.
func highestCovered(los, his []time.Duration, need int) (time.Duration, bool) {
	for i := len(his) - 1; i >= 0; i-- {
		if coverage(los, his, his[i]) >= need {
			return his[i], true
		}
	}
	return 0, false
}

// coverage returns the number of closed intervals containing x, from their
// sorted lower and upper bounds
func coverage(los, his []time.Duration, x time.Duration) int {
	started := sort.Search(len(los), func(k int) bool { return los[k] > x })
	ended := sort.Search(len(his), func(k int) bool { return his[k] >= x })
	return started - ended
}

// maxOverlap returns the largest number of intervals sharing a point
func maxOverlap(intervals []interval) int {
	type edge struct {
		at    time.Duration
		delta int
	}
	edges := make([]edge, 0, 2*len(intervals))
	for _, iv := range intervals {
		edges = append(edges, edge{iv.lo, +1}, edge{iv.hi, -1})
	}
	// Starts before ends at the same point: touching intervals overlap
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].at != edges[j].at {
			return edges[i].at < edges[j].at
		}
		return edges[i].delta > edges[j].delta
	})
	best, count := 0, 0
	for _, e := range edges {
		count += e.delta
		best = max(best, count)
	}
	return best
}

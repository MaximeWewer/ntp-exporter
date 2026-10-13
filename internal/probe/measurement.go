// Package probe queries NTP targets once per cycle and produces measurements
// shared by every metric family.
package probe

import (
	"time"

	"github.com/maximewewer/ntp-exporter/internal/ntp"
)

// Target is a single NTP server to query
type Target struct {
	// Server is the configured server, or the resolved address of a pool member
	Server string
	// Pool is the pool name for pool members, empty for configured servers
	Pool string
	// Address is the IP address (with optional port) queried during the cycle:
	// every sample of a burst goes to the same address
	Address string
	// policy restricts the addresses of /probe targets
	policy *TargetPolicy
}

// Measurement is the result of one burst of queries to a target
type Measurement struct {
	Target Target
	// Sent is the number of queries sent during the burst
	Sent int
	// Responses holds every response received, valid or not
	Responses []*ntp.Response
	// Err is set when no response at all was received
	Err       error
	Timestamp time.Time
	Duration  time.Duration
	// Skipped is set when the target was not queried during this cycle
	// because of a RATE backoff: the measurement is the previous one
	Skipped bool
	// SuspendCode is the KoD code (DENY or RSTR) of a suspended target
	SuspendCode string
	// NTS describes the key exchange of an NTS target (nts://host)
	NTS *ntp.NTSState
}

// key identifies a target across cycles
func (t Target) key() string {
	return t.Pool + "|" + t.Server
}

// Reachable reports whether the target answered at least once
func (m *Measurement) Reachable() bool {
	return len(m.Responses) > 0
}

// Valid returns the responses usable as time measurements
func (m *Measurement) Valid() []*ntp.Response {
	return ntp.ValidResponses(m.Responses)
}

// Best returns the reference sample of the burst, or nil without valid response.
// Like the NTP clock filter (RFC 5905 §10), it is the valid sample with the
// lowest round-trip delay: the one least affected by network queuing.
func (m *Measurement) Best() *ntp.Response {
	var best *ntp.Response
	for _, r := range m.Valid() {
		if best == nil || r.RTT < best.RTT {
			best = r
		}
	}
	return best
}

// Stats computes statistics on valid responses; invalid ones count as lost
func (m *Measurement) Stats() *ntp.Statistics {
	return ntp.CalculateStatistics(m.Valid(), m.Sent)
}

// PoolResult describes the resolution of a pool during a cycle
type PoolResult struct {
	Name string
	// Members are the addresses selected for querying
	Members []string
	// Total is the number of addresses returned by the resolution
	Total       int
	DNSDuration time.Duration
	Err         error
}

// CycleResult gathers every measurement taken during one collection cycle
type CycleResult struct {
	Start        time.Time
	Duration     time.Duration
	Measurements []*Measurement
	Pools        []PoolResult
	// Overrun is set when the cycle hit its deadline before every target answered
	Overrun bool
}

// PoolMeasurements returns the measurements of the members of a pool
func (r *CycleResult) PoolMeasurements(pool string) []*Measurement {
	var out []*Measurement
	for _, m := range r.Measurements {
		if m.Target.Pool == pool {
			out = append(out, m)
		}
	}
	return out
}

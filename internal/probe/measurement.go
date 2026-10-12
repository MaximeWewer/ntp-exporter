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
}

// Reachable reports whether the target answered at least once
func (m *Measurement) Reachable() bool {
	return len(m.Responses) > 0
}

// Valid returns the responses usable as time measurements
func (m *Measurement) Valid() []*ntp.Response {
	return ntp.ValidResponses(m.Responses)
}

// Best returns the reference sample of the burst, or nil without valid response
func (m *Measurement) Best() *ntp.Response {
	valid := m.Valid()
	if len(valid) == 0 {
		return nil
	}
	return valid[0]
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

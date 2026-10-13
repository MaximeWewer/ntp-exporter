package probe

import (
	"sync"
	"time"

	"github.com/maximewewer/ntp-exporter/pkg/logger"
)

// Kiss-o'-Death codes handled by the backoff (RFC 5905 §7.4)
const (
	KoDRate = "RATE"
	KoDDeny = "DENY"
	KoDRstr = "RSTR"
)

// Backoff applies the Kiss-o'-Death rules per target:
//   - RATE: the polling interval of the target doubles (up to a maximum) and
//     goes back down progressively after successful cycles
//   - DENY / RSTR: the target is not queried at all for a suspension period
type Backoff struct {
	mu       sync.Mutex
	interval time.Duration
	max      time.Duration
	suspend  time.Duration
	now      func() time.Time
	states   map[string]*backoffState
}

type backoffState struct {
	factor         int
	nextAllowed    time.Time
	suspendedUntil time.Time
	suspendCode    string
}

// maxBackoffStates bounds the number of targets remembered: /probe targets
// come from HTTP requests
const maxBackoffStates = 4096

// NewBackoff creates a backoff for a polling interval
func NewBackoff(interval, maxInterval, suspend time.Duration) *Backoff {
	return &Backoff{
		interval: interval,
		max:      maxInterval,
		suspend:  suspend,
		now:      time.Now,
		states:   make(map[string]*backoffState),
	}
}

// Decision tells whether a target may be queried during the current cycle
type Decision struct {
	Allowed bool
	// SuspendCode is the KoD code (DENY or RSTR) when the target is suspended
	SuspendCode string
}

// Allow returns whether the target may be queried now
func (b *Backoff) Allow(server string) Decision {
	b.mu.Lock()
	defer b.mu.Unlock()

	st, ok := b.states[server]
	if !ok {
		return Decision{Allowed: true}
	}

	now := b.now()
	if now.Before(st.suspendedUntil) {
		return Decision{SuspendCode: st.suspendCode}
	}
	if st.suspendCode != "" {
		// Suspension is over: start again with a cautious interval
		st.suspendCode = ""
	}

	// Small tolerance so that a target allowed every N intervals is not
	// skipped one extra cycle because of ticker jitter
	if now.Add(b.interval / 10).Before(st.nextAllowed) {
		return Decision{}
	}
	return Decision{Allowed: true}
}

// Record updates the state of a target after a burst
func (b *Backoff) Record(server string, m *Measurement) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()
	st, ok := b.states[server]
	if !ok {
		if len(b.states) >= maxBackoffStates {
			b.purge(now)
		}
		if len(b.states) >= maxBackoffStates {
			logger.SafeWarn("probe", "Too many targets backing off, state not recorded", map[string]interface{}{
				"server": server,
			})
			return
		}
		st = &backoffState{factor: 1}
		b.states[server] = st
	}

	codes := make(map[string]bool)
	for _, resp := range m.Responses {
		if resp.KissCode != "" {
			codes[resp.KissCode] = true
		}
	}

	switch {
	case codes[KoDDeny] || codes[KoDRstr]:
		code := KoDDeny
		if !codes[KoDDeny] {
			code = KoDRstr
		}
		st.suspendCode = code
		st.suspendedUntil = now.Add(b.suspend)
		logger.SafeWarn("probe", "Kiss-o'-Death received, target suspended", map[string]interface{}{
			"server":  server,
			"code":    code,
			"resume":  st.suspendedUntil.Format(time.RFC3339),
			"suspend": b.suspend.String(),
		})
	case codes[KoDRate]:
		if time.Duration(st.factor*2)*b.interval <= b.max {
			st.factor *= 2
		}
		st.nextAllowed = now.Add(time.Duration(st.factor) * b.interval)
		logger.SafeWarn("probe", "Kiss-o'-Death RATE received, slowing down", map[string]interface{}{
			"server":   server,
			"interval": (time.Duration(st.factor) * b.interval).String(),
		})
	case m.Reachable():
		if st.factor > 1 {
			st.factor /= 2
		}
		st.nextAllowed = now.Add(time.Duration(st.factor) * b.interval)
	}

	// Back to the normal interval: nothing left to remember
	if st.factor == 1 && st.suspendCode == "" {
		delete(b.states, server)
	}
}

// Forget drops the state of targets that are no longer probed. A target still
// suspended or backing off is kept until its state expires: a pool member
// leaving the DNS answer and coming back must not be queried earlier.
func (b *Backoff) Forget(keep map[string]bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	for server, st := range b.states {
		if !keep[server] && !st.active(now) {
			delete(b.states, server)
		}
	}
}

// purge drops the states that no longer restrict their target
func (b *Backoff) purge(now time.Time) {
	for server, st := range b.states {
		if !st.active(now) {
			delete(b.states, server)
		}
	}
}

// active reports whether the state still restricts queries to its target
func (st *backoffState) active(now time.Time) bool {
	return now.Before(st.suspendedUntil) || now.Before(st.nextAllowed)
}

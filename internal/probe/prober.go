package probe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/ntp"
	"github.com/maximewewer/ntp-exporter/pkg/logger"
	"golang.org/x/sync/errgroup"
)

// Resolver resolves host names
type Resolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
}

// Prober queries every target exactly once per cycle
type Prober struct {
	cfg      *config.Config
	client   ntp.NTPQuerier
	pools    []*ntp.Pool
	resolver Resolver
	// cycleTimeout bounds a cycle so that it always ends before the next one
	cycleTimeout time.Duration
	backoff      *Backoff
	families     *families

	mu       sync.Mutex
	previous map[string]*Measurement
}

// New creates a prober. Pools are long-lived so that their DNS cache
// survives across cycles.
func New(cfg *config.Config, client ntp.NTPQuerier) *Prober {
	pools := make([]*ntp.Pool, 0, len(cfg.NTP.Pools))
	for _, pc := range cfg.NTP.Pools {
		pools = append(pools, ntp.NewPool(pc.Name, pc.Strategy, pc.MaxServers, pc.Fallback,
			ntp.DNSCacheConfig{MinTTL: cfg.NTP.DNSCache.MinTTL, MaxTTL: cfg.NTP.DNSCache.MaxTTL}))
	}

	return &Prober{
		cfg:          cfg,
		client:       client,
		pools:        pools,
		resolver:     net.DefaultResolver,
		cycleTimeout: CycleTimeout(cfg.NTP.ScrapeInterval),
		backoff:      NewBackoff(cfg.NTP.ScrapeInterval, cfg.NTP.KoD.MaxBackoff, cfg.NTP.KoD.SuspendDuration),
		families:     newFamilies(cfg.NTP.IPFamily),
		previous:     make(map[string]*Measurement),
	}
}

// SetResolver replaces the resolver used for configured servers
func (p *Prober) SetResolver(r Resolver) {
	p.resolver = r
}

// CycleTimeout returns the maximum duration of a cycle for an interval:
// the interval minus a safety margin of 10% (at most 1s)
func CycleTimeout(interval time.Duration) time.Duration {
	margin := interval / 10
	if margin > time.Second {
		margin = time.Second
	}
	return interval - margin
}

// Run executes one collection cycle: targets are probed in parallel, with at
// most max_concurrency bursts in flight, and the cycle is cut at its deadline
func (p *Prober) Run(ctx context.Context) *CycleResult {
	result := &CycleResult{Start: time.Now()}
	if ctx.Err() != nil {
		return result
	}

	cycleCtx, cancel := context.WithTimeout(ctx, p.cycleTimeout)
	defer cancel()

	p.families.refresh()
	targets := p.resolveTargets(cycleCtx, result)
	result.Measurements = make([]*Measurement, len(targets))

	var g errgroup.Group
	g.SetLimit(p.cfg.NTP.MaxConcurrency)
	for i, target := range targets {
		g.Go(func() error {
			result.Measurements[i] = p.probe(cycleCtx, target)
			return nil
		})
	}
	_ = g.Wait()
	p.forget(targets)

	result.Duration = time.Since(result.Start)
	result.Overrun = errors.Is(cycleCtx.Err(), context.DeadlineExceeded)
	if result.Overrun {
		logger.SafeWarn("probe", "Collection cycle reached its deadline", map[string]interface{}{
			"deadline": p.cycleTimeout.String(),
			"targets":  len(targets),
		})
	}

	return result
}

// resolveTargets builds the list of targets: configured servers then pool members
func (p *Prober) resolveTargets(ctx context.Context, result *CycleResult) []Target {
	targets := make([]Target, 0, len(p.cfg.NTP.Servers))
	for _, server := range p.cfg.NTP.Servers {
		targets = append(targets, Target{Server: server})
	}

	for _, pool := range p.pools {
		members, total, dnsDuration, err := pool.Members(ctx)
		result.Pools = append(result.Pools, PoolResult{
			Name:        pool.Name(),
			Members:     members,
			Total:       total,
			DNSDuration: dnsDuration,
			Err:         err,
		})
		if err != nil {
			logger.SafeWarn("probe", "Pool resolution failed", map[string]interface{}{
				"pool":  pool.Name(),
				"error": err.Error(),
			})
			continue
		}
		for _, member := range p.families.filter(members) {
			targets = append(targets, Target{Server: member, Pool: pool.Name(), Address: member})
		}
	}

	return targets
}

// resolve returns the addresses to try for a configured server, at most one
// per address family. The host is resolved once per cycle so that every
// sample of the burst reaches the same server, even behind a round-robin
// name such as pool.ntp.org.
func (p *Prober) resolve(ctx context.Context, target Target) ([]string, error) {
	server := target.Server
	host, port, err := net.SplitHostPort(server)
	if err != nil {
		host, port = server, ""
	}
	if net.ParseIP(host) != nil {
		return []string{server}, nil
	}

	addrs, err := p.resolver.LookupHost(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s: %w", host, err)
	}
	candidates := p.families.candidates(target.key(), addrs)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no %s address found for %s", p.cfg.NTP.IPFamily, host)
	}

	if port != "" {
		for i, a := range candidates {
			candidates[i] = net.JoinHostPort(a, port)
		}
	}
	return candidates, nil
}

// probe measures a target unless the Kiss-o'-Death backoff forbids it
func (p *Prober) probe(ctx context.Context, target Target) *Measurement {
	key := target.key()

	decision := p.backoff.Allow(key)
	if decision.SuspendCode != "" {
		return &Measurement{Target: target, Timestamp: time.Now(), SuspendCode: decision.SuspendCode}
	}
	if !decision.Allowed {
		p.mu.Lock()
		prev := p.previous[key]
		p.mu.Unlock()
		if prev == nil {
			return &Measurement{Target: target, Timestamp: time.Now(), Skipped: true}
		}
		skipped := *prev
		skipped.Skipped = true
		return &skipped
	}

	m := p.measure(ctx, target)
	p.backoff.Record(key, m)

	p.mu.Lock()
	p.previous[key] = m
	p.mu.Unlock()

	return m
}

// forget drops the state of targets that are no longer probed
func (p *Prober) forget(targets []Target) {
	keep := make(map[string]bool, len(targets))
	for _, t := range targets {
		keep[t.key()] = true
	}
	p.backoff.Forget(keep)
	p.families.forget(keep)

	p.mu.Lock()
	defer p.mu.Unlock()
	for key := range p.previous {
		if !keep[key] {
			delete(p.previous, key)
		}
	}
}

// measure sends one burst of queries to a target
func (p *Prober) measure(ctx context.Context, target Target) *Measurement {
	m := &Measurement{Target: target, Timestamp: time.Now()}
	defer func() { m.Duration = time.Since(m.Timestamp) }()

	candidates := []string{target.Address}
	if target.Address == "" {
		var err error
		candidates, err = p.resolve(ctx, target)
		if err != nil {
			m.Sent = p.cfg.NTP.SamplesPerServer
			m.Err = err
			logger.SafeDebug("probe", "Target resolution failed", map[string]interface{}{
				"server": target.Server,
				"error":  err.Error(),
			})
			return m
		}
	}

	// A dual-stack server is reached over the first family that answers a
	// single query; that answer is the first sample of the burst
	samples := p.cfg.NTP.SamplesPerServer
	address := candidates[len(candidates)-1]
	for _, candidate := range candidates[:len(candidates)-1] {
		m.Sent++
		resp, err := p.client.Query(ctx, candidate)
		if err == nil {
			address = candidate
			m.Responses = append(m.Responses, resp)
			samples--
			break
		}
		logger.SafeDebug("probe", "No answer, trying the other address family", map[string]interface{}{
			"server":  target.Server,
			"address": candidate,
			"error":   err.Error(),
		})
	}
	m.Target.Address = address

	var err error
	if samples > 0 {
		if len(m.Responses) > 0 {
			err = sleep(ctx, p.cfg.NTP.SampleSpacing)
		}
		if err == nil {
			var responses []*ntp.Response
			m.Sent += samples
			responses, err = p.client.QueryMultiple(ctx, address, samples)
			m.Responses = append(m.Responses, responses...)
		}
	}
	if len(m.Responses) > 0 {
		p.families.remember(target.key(), address)
	}

	// Responses are labelled with the configured server name
	for _, resp := range m.Responses {
		resp.Server = target.Server
	}

	if err != nil && len(m.Responses) == 0 {
		m.Err = err
		logger.SafeDebug("probe", "Target unreachable", map[string]interface{}{
			"server":  target.Server,
			"address": address,
			"pool":    target.Pool,
			"error":   err.Error(),
		})
	}

	return m
}

// sleep waits for d unless the context ends first
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

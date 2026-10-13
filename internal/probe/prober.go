package probe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/logger"
	"github.com/maximewewer/ntp-exporter/internal/ntp"
	"golang.org/x/sync/errgroup"
)

// Resolver resolves host names
type Resolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
}

// NTSQuerier sends bursts of authenticated queries to NTS servers
type NTSQuerier interface {
	Burst(ctx context.Context, host string, count int) ntp.NTSResult
}

// Prober queries every target exactly once per cycle
type Prober struct {
	cfg      *config.Config
	client   ntp.NTPQuerier
	nts      NTSQuerier
	pools    []*ntp.Pool
	resolver Resolver
	// cycleTimeout bounds a cycle so that it always ends before the next one
	cycleTimeout time.Duration
	backoff      *Backoff
	// probeBackoff, probeFamilies and probeNTS hold the state of /probe
	// targets, apart from the configured targets: unique /probe targets
	// cannot evict the state of the configured ones
	probeBackoff  *Backoff
	probeFamilies *families
	probeNTS      NTSQuerier
	families      *families
	// policy restricts the addresses /probe requests may reach
	policy *TargetPolicy

	mu       sync.Mutex
	previous map[string]*Measurement
}

// New creates a prober. Pools are long-lived so that their DNS cache
// survives across cycles.
func New(cfg *config.Config, client ntp.NTPQuerier) *Prober {
	pools := make([]*ntp.Pool, 0, len(cfg.NTP.Pools))
	for _, pc := range cfg.NTP.Pools {
		pools = append(pools, ntp.NewPool(pc.Name, pc.MaxServers,
			ntp.DNSCacheConfig{MinTTL: cfg.NTP.DNSCache.MinTTL, MaxTTL: cfg.NTP.DNSCache.MaxTTL}, nil))
	}

	return &Prober{
		cfg:           cfg,
		client:        client,
		nts:           ntp.NewNTSClient(cfg.NTP.Timeout, cfg.NTP.SampleSpacing),
		pools:         pools,
		resolver:      net.DefaultResolver,
		cycleTimeout:  CycleTimeout(cfg.NTP.CollectionInterval),
		backoff:       NewBackoff(cfg.NTP.CollectionInterval, cfg.NTP.KoD.MaxBackoff, cfg.NTP.KoD.SuspendDuration),
		probeBackoff:  NewBackoff(cfg.NTP.CollectionInterval, cfg.NTP.KoD.MaxBackoff, cfg.NTP.KoD.SuspendDuration),
		families:      newFamilies(cfg.NTP.IPFamily),
		probeFamilies: newFamilies(cfg.NTP.IPFamily),
		probeNTS:      ntp.NewNTSClient(cfg.NTP.Timeout, cfg.NTP.SampleSpacing),
		previous:      make(map[string]*Measurement),
	}
}

// SetNTSClient replaces the NTS client of the configured and /probe targets
func (p *Prober) SetNTSClient(c NTSQuerier) {
	p.nts = c
	p.probeNTS = c
}

// familiesFor returns the address family state of a target
func (p *Prober) familiesFor(target Target) *families {
	if target.onDemand {
		return p.probeFamilies
	}
	return p.families
}

// SetTargetPolicy sets the addresses /probe requests may reach
func (p *Prober) SetTargetPolicy(policy *TargetPolicy) {
	p.policy = policy
}

// SetResolver replaces the resolver of the configured servers and pools
func (p *Prober) SetResolver(r Resolver) {
	p.resolver = r
	for _, pool := range p.pools {
		pool.SetResolver(r)
	}
}

// CycleTimeout returns the maximum duration of a cycle for an interval
func CycleTimeout(interval time.Duration) time.Duration {
	return config.CycleTimeout(interval)
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
	p.probeFamilies.copyRoutes(p.families)
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
		logger.Warn("probe", "Collection cycle reached its deadline", "deadline", p.cycleTimeout.String(), "targets", len(targets))
	}

	return result
}

// resolveTargets builds the list of targets: configured servers then pool
// members. A pool member already listed as a server or returned by another
// pool is skipped: it is queried once per cycle and its series stay unique.
func (p *Prober) resolveTargets(ctx context.Context, result *CycleResult) []Target {
	targets := make([]Target, 0, len(p.cfg.NTP.Servers))
	seen := make(map[string]bool, len(p.cfg.NTP.Servers))
	for _, server := range p.cfg.NTP.Servers {
		targets = append(targets, Target{Server: server})
		seen[server] = true
	}

	for _, pool := range p.pools {
		members, total, err := pool.Members(ctx)
		result.Pools = append(result.Pools, PoolResult{
			Name:    pool.Name(),
			Members: members,
			Total:   total,
			Err:     err,
		})
		if err != nil {
			logger.Warn("probe", "Pool resolution failed", "pool", pool.Name(), "error", err.Error())
			continue
		}
		for _, member := range p.families.filter(members) {
			if seen[member] {
				logger.Debug("probe", "Pool member already probed, skipped", "pool", pool.Name(), "member", member)
				continue
			}
			seen[member] = true
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
	candidates := p.familiesFor(target).candidates(target.key(), addrs)
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
		skipped.BackoffFactor = p.backoff.Factor(key)
		return &skipped
	}

	m := p.measure(ctx, target, p.cfg.NTP.SamplesPerServer)
	p.backoff.Record(key, m)
	m.BackoffFactor = p.backoff.Factor(key)

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

// Probe measures a single target outside of the collection cycles, for the
// /probe endpoint. Kiss-o'-Death codes are honored across requests: a
// suspended target, or one backing off after RATE, is not queried and the
// measurement carries no response.
func (p *Prober) Probe(ctx context.Context, server string, samples int) *Measurement {
	target := Target{Server: server, policy: p.policy, onDemand: true}
	key := target.key()

	decision := p.probeBackoff.Allow(key)
	if decision.SuspendCode != "" {
		return &Measurement{Target: target, Timestamp: time.Now(), SuspendCode: decision.SuspendCode}
	}
	if !decision.Allowed {
		return &Measurement{Target: target, Timestamp: time.Now(), Skipped: true}
	}

	m := p.measure(ctx, target, samples)
	p.probeBackoff.Record(key, m)
	m.BackoffFactor = p.probeBackoff.Factor(key)
	return m
}

// measure sends one burst of queries to a target
func (p *Prober) measure(ctx context.Context, target Target, samples int) *Measurement {
	m := &Measurement{Target: target, Timestamp: time.Now()}
	defer func() { m.Duration = time.Since(m.Timestamp) }()

	if ntp.IsNTS(target.Server) {
		p.measureNTS(ctx, m, samples)
		return m
	}

	candidates := []string{target.Address}
	if target.Address == "" {
		var err error
		candidates, err = p.resolve(ctx, target)
		if err != nil {
			m.Sent = samples
			m.Err = err
			logger.Debug("probe", "Target resolution failed", "server", target.Server, "error", err.Error())
			return m
		}
	}
	if err := checkAddresses(target, candidates); err != nil {
		m.Err = err
		return m
	}

	// A dual-stack server is reached over the first family that answers a
	// single query; that answer is the first sample of the burst
	address := candidates[len(candidates)-1]
	for _, candidate := range candidates[:len(candidates)-1] {
		m.Sent++
		resp, err := p.client.Query(ctx, candidate)
		if err == nil {
			address = candidate
			m.Responses = append(m.Responses, resp)
			samples--
			// A Kiss-o'-Death ends the burst (RFC 5905 §7.4)
			if resp.IsKissOfDeath() {
				samples = 0
			}
			break
		}
		logger.Debug("probe", "No answer, trying the other address family",
			"server", target.Server,
			"address", candidate,
			"error", err.Error(),
		)
	}
	m.Target.Address = address

	var err error
	if samples > 0 {
		if len(m.Responses) > 0 {
			err = ntp.Wait(ctx, p.cfg.NTP.SampleSpacing)
		}
		if err == nil {
			var responses []*ntp.Response
			var sent int
			responses, sent, err = p.client.QueryMultiple(ctx, address, samples)
			m.Sent += sent
			m.Responses = append(m.Responses, responses...)
		}
	}
	if len(m.Responses) > 0 {
		p.familiesFor(target).remember(target.key(), address)
	}

	// Responses are labelled with the configured server name
	for _, resp := range m.Responses {
		resp.Server = target.Server
		resp.CheckRootDistance(p.cfg.NTP.MaxRootDistance)
	}

	if err != nil && len(m.Responses) == 0 {
		m.Err = err
		logger.Debug("probe", "Target unreachable",
			"server", target.Server,
			"address", address,
			"pool", target.Pool,
			"error", err.Error(),
		)
	}

	return m
}

// measureNTS sends one burst of authenticated queries to an NTS target. The
// NTP server is the one negotiated by the key exchange.
func (p *Prober) measureNTS(ctx context.Context, m *Measurement, samples int) {
	host := ntp.NTSHost(m.Target.Server)
	if m.Target.policy != nil {
		// The key exchange server, then the NTP server it negotiates
		addrs, err := p.lookup(ctx, host)
		if err == nil {
			err = checkAddresses(m.Target, addrs)
		}
		if err != nil {
			m.Err = err
			return
		}
	}

	client := p.nts
	if m.Target.onDemand {
		client = p.probeNTS
	}
	res := client.Burst(ctx, host, samples)
	if res.State.Address != "" {
		if err := checkAddresses(m.Target, []string{res.State.Address}); err != nil {
			m.Err = err
			return
		}
	}
	// No key exchange state when the request gave up waiting for the server
	if !errors.Is(res.Err, ntp.ErrNTSBusy) {
		m.NTS = &res.State
	}
	m.Target.Address = res.State.Address
	m.Sent = res.Sent
	m.Responses = res.Responses
	for _, resp := range m.Responses {
		resp.Server = m.Target.Server
		resp.CheckRootDistance(p.cfg.NTP.MaxRootDistance)
	}
	if res.Err != nil && len(m.Responses) == 0 {
		m.Err = res.Err
		logger.Debug("probe", "NTS target unreachable", "server", m.Target.Server, "error", res.Err.Error())
	}
}

// checkAddresses applies the /probe target policy to the addresses of a target
func checkAddresses(target Target, addrs []string) error {
	if target.policy == nil {
		return nil
	}
	host := strings.TrimPrefix(target.Server, ntp.NTSScheme)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	for _, addr := range addrs {
		if err := target.policy.Check(host, addr); err != nil {
			return err
		}
	}
	return nil
}

// lookup resolves the host of a host[:port] address, IP literals included
func (p *Prober) lookup(ctx context.Context, hostPort string) ([]string, error) {
	host := hostPort
	if h, _, err := net.SplitHostPort(hostPort); err == nil {
		host = h
	}
	if net.ParseIP(host) != nil {
		return []string{host}, nil
	}
	addrs, err := p.resolver.LookupHost(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s: %w", host, err)
	}
	return addrs, nil
}

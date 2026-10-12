package probe

import (
	"context"
	"errors"
	"fmt"
	"net"
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
}

// New creates a prober. Pools are long-lived so that their DNS cache
// survives across cycles.
func New(cfg *config.Config, client ntp.NTPQuerier) *Prober {
	pools := make([]*ntp.Pool, 0, len(cfg.NTP.Pools))
	for _, pc := range cfg.NTP.Pools {
		pools = append(pools, ntp.NewPoolWithDNSCache(pc.Name, pc.Strategy, pc.MaxServers, pc.Fallback, client,
			ntp.DNSCacheConfig{MinTTL: cfg.NTP.DNSCache.MinTTL, MaxTTL: cfg.NTP.DNSCache.MaxTTL}))
	}

	return &Prober{
		cfg:          cfg,
		client:       client,
		pools:        pools,
		resolver:     net.DefaultResolver,
		cycleTimeout: CycleTimeout(cfg.NTP.ScrapeInterval),
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

	targets := p.resolveTargets(cycleCtx, result)
	result.Measurements = make([]*Measurement, len(targets))

	var g errgroup.Group
	g.SetLimit(p.cfg.NTP.MaxConcurrency)
	for i, target := range targets {
		g.Go(func() error {
			result.Measurements[i] = p.measure(cycleCtx, target)
			return nil
		})
	}
	_ = g.Wait()

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
		for _, member := range members {
			targets = append(targets, Target{Server: member, Pool: pool.Name(), Address: member})
		}
	}

	return targets
}

// resolve returns the address to query for a configured server. The host is
// resolved once per cycle so that every sample of the burst reaches the same
// server, even behind a round-robin name such as pool.ntp.org.
func (p *Prober) resolve(ctx context.Context, server string) (string, error) {
	host, port, err := net.SplitHostPort(server)
	if err != nil {
		host, port = server, ""
	}
	if net.ParseIP(host) != nil {
		return server, nil
	}

	addrs, err := p.resolver.LookupHost(ctx, host)
	if err != nil {
		return "", fmt.Errorf("failed to resolve %s: %w", host, err)
	}
	if len(addrs) == 0 {
		return "", fmt.Errorf("no address found for %s", host)
	}

	if port != "" {
		return net.JoinHostPort(addrs[0], port), nil
	}
	return addrs[0], nil
}

// measure sends one burst of queries to a target
func (p *Prober) measure(ctx context.Context, target Target) *Measurement {
	m := &Measurement{Target: target, Timestamp: time.Now()}
	defer func() { m.Duration = time.Since(m.Timestamp) }()

	if m.Target.Address == "" {
		address, err := p.resolve(ctx, target.Server)
		if err != nil {
			m.Sent = p.cfg.NTP.SamplesPerServer
			m.Err = err
			logger.SafeDebug("probe", "Target resolution failed", map[string]interface{}{
				"server": target.Server,
				"error":  err.Error(),
			})
			return m
		}
		m.Target.Address = address
	}
	address := m.Target.Address

	var err error
	if p.cfg.NTP.AdaptiveSampling.Enabled {
		sampler := ntp.NewAdaptiveSampler(ntp.AdaptiveSamplingConfig{
			DefaultSamples:   p.cfg.NTP.AdaptiveSampling.DefaultSamples,
			HighDriftSamples: p.cfg.NTP.AdaptiveSampling.HighDriftSamples,
			DriftThreshold:   p.cfg.NTP.AdaptiveSampling.DriftThreshold,
			MaxDuration:      p.cfg.NTP.AdaptiveSampling.MaxDuration,
		}, p.client)
		m.Responses, m.Sent, err = sampler.SampleWithCount(ctx, address)
	} else {
		m.Sent = p.cfg.NTP.SamplesPerServer
		m.Responses, err = p.client.QueryMultiple(ctx, address, m.Sent)
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

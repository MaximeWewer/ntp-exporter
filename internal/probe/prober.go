package probe

import (
	"context"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/ntp"
	"github.com/maximewewer/ntp-exporter/pkg/logger"
)

// Prober queries every target exactly once per cycle
type Prober struct {
	cfg    *config.Config
	client ntp.NTPQuerier
	pools  []*ntp.Pool
}

// New creates a prober. Pools are long-lived so that their DNS cache
// survives across cycles.
func New(cfg *config.Config, client ntp.NTPQuerier) *Prober {
	pools := make([]*ntp.Pool, 0, len(cfg.NTP.Pools))
	for _, pc := range cfg.NTP.Pools {
		pools = append(pools, ntp.NewPool(pc.Name, pc.Strategy, pc.MaxServers, pc.Fallback, client))
	}

	return &Prober{
		cfg:    cfg,
		client: client,
		pools:  pools,
	}
}

// Run executes one collection cycle
func (p *Prober) Run(ctx context.Context) *CycleResult {
	result := &CycleResult{Start: time.Now()}

	targets := p.resolveTargets(ctx, result)
	for _, target := range targets {
		if ctx.Err() != nil {
			break
		}
		result.Measurements = append(result.Measurements, p.measure(ctx, target))
	}

	result.Duration = time.Since(result.Start)
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
			targets = append(targets, Target{Server: member, Pool: pool.Name()})
		}
	}

	return targets
}

// measure sends one burst of queries to a target
func (p *Prober) measure(ctx context.Context, target Target) *Measurement {
	m := &Measurement{Target: target, Timestamp: time.Now()}
	defer func() { m.Duration = time.Since(m.Timestamp) }()

	var err error
	if p.cfg.NTP.AdaptiveSampling.Enabled {
		sampler := ntp.NewAdaptiveSampler(ntp.AdaptiveSamplingConfig{
			DefaultSamples:   p.cfg.NTP.AdaptiveSampling.DefaultSamples,
			HighDriftSamples: p.cfg.NTP.AdaptiveSampling.HighDriftSamples,
			DriftThreshold:   p.cfg.NTP.AdaptiveSampling.DriftThreshold,
			MaxDuration:      p.cfg.NTP.AdaptiveSampling.MaxDuration,
		}, p.client)
		m.Responses, m.Sent, err = sampler.SampleWithCount(ctx, target.Server)
	} else {
		m.Sent = p.cfg.NTP.SamplesPerServer
		m.Responses, err = p.client.QueryMultiple(ctx, target.Server, m.Sent)
	}

	if err != nil && len(m.Responses) == 0 {
		m.Err = err
		logger.SafeDebug("probe", "Target unreachable", map[string]interface{}{
			"server": target.Server,
			"pool":   target.Pool,
			"error":  err.Error(),
		})
	}

	return m
}

package ntp

import (
	"context"
	"errors"
	"time"

	"github.com/maximewewer/ntp-exporter/pkg/logger"
	"github.com/maximewewer/ntp-exporter/pkg/mathutil"
)

// Pool represents an NTP pool with multiple servers
type Pool struct {
	name         string
	strategy     string
	maxServers   int
	fallback     string
	querier      NTPQuerier
	dnsCache     *DNSCache
	workerPool   *WorkerPool
	useWorkerPool bool
}

// PoolResponse represents aggregated responses from a pool
type PoolResponse struct {
	PoolName      string
	Servers       []string
	Responses     []*Response
	ActiveServers int
	TotalServers  int
	BestOffset    time.Duration
	DNSResolution time.Duration
}

// NewPool creates a new NTP pool
func NewPool(name, strategy string, maxServers int, fallback string, querier NTPQuerier) *Pool {
	if strategy == "" {
		strategy = "best_n"
	}
	if maxServers == 0 {
		maxServers = 4
	}

	return &Pool{
		name:          name,
		strategy:      strategy,
		maxServers:    maxServers,
		fallback:      fallback,
		querier:       querier,
		dnsCache:      NewDNSCache(DNSCacheConfig{}),
		workerPool:    nil, // Created on-demand
		useWorkerPool: false,
	}
}

// EnableWorkerPool enables parallel query execution using a worker pool
func (p *Pool) EnableWorkerPool(size int) {
	if size <= 0 {
		size = 5
	}
	p.workerPool = NewWorkerPool(size, p.querier)
	p.useWorkerPool = true
}

// Resolve resolves the pool DNS to get individual server IPs using cache
func (p *Pool) Resolve(ctx context.Context) ([]string, time.Duration, error) {
	start := time.Now()

	// Use DNS cache for resolution
	ips, err := p.dnsCache.Resolve(ctx, p.name)
	if err != nil {
		logger.SafeWarn("ntp", "Failed to resolve pool DNS", map[string]interface{}{
			"pool":  p.name,
			"error": err.Error(),
		})

		// Try fallback if configured
		if p.fallback != "" {
			logger.SafeInfo("ntp", "Using fallback server", map[string]interface{}{
				"pool":     p.name,
				"fallback": p.fallback,
			})
			return []string{p.fallback}, time.Since(start), nil
		}

		return nil, 0, errors.New("failed to resolve pool")
	}

	duration := time.Since(start)

	// Limit to maxServers
	if len(ips) > p.maxServers {
		ips = ips[:p.maxServers]
	}

	logger.SafeDebug("ntp", "Pool DNS resolved", map[string]interface{}{
		"pool":     p.name,
		"servers":  len(ips),
		"duration": duration.Seconds(),
		"cached":   duration < 10*time.Millisecond, // Fast response indicates cache hit
	})

	return ips, duration, nil
}

// Query queries servers in the pool based on strategy and returns aggregated results
func (p *Pool) Query(ctx context.Context, samples int) (*PoolResponse, error) {
	// Resolve pool DNS
	servers, dnsTime, err := p.Resolve(ctx)
	if err != nil {
		return nil, err
	}

	response := &PoolResponse{
		PoolName:      p.name,
		Servers:       servers,
		Responses:     make([]*Response, 0),
		TotalServers:  len(servers),
		DNSResolution: dnsTime,
	}

	// Select servers based on strategy
	selectedServers := p.selectServersByStrategy(servers)

	// Use WorkerPool for parallel queries if enabled and strategy is 'all'
	if p.useWorkerPool && p.workerPool != nil && p.strategy == "all" {
		results, err := p.workerPool.Execute(ctx, selectedServers, samples)
		if err != nil {
			logger.SafeWarn("ntp", "WorkerPool execution failed", map[string]interface{}{
				"pool":  p.name,
				"error": err.Error(),
			})
		} else {
			// Collect responses from worker pool results
			for _, result := range results {
				if result.Error == nil && len(result.Responses) > 0 {
					// Take first response from each server
					response.Responses = append(response.Responses, result.Responses[0])
					response.ActiveServers++
				}
			}
		}
	} else {
		// Sequential query (original behavior)
		for _, server := range selectedServers {
			select {
			case <-ctx.Done():
				return response, ctx.Err()
			default:
			}

			resp, err := p.querier.Query(ctx, server)
			if err != nil {
				logger.SafeDebug("ntp", "Failed to query pool server", map[string]interface{}{
					"pool":     p.name,
					"server":   server,
					"strategy": p.strategy,
					"error":    err.Error(),
				})
				continue
			}

			response.Responses = append(response.Responses, resp)
			response.ActiveServers++

			// For best_n strategy, stop after getting enough successful responses
			if p.strategy == "best_n" && response.ActiveServers >= p.maxServers {
				break
			}
		}
	}

	// Calculate best offset
	if len(response.Responses) > 0 {
		response.BestOffset = p.findBestOffset(response.Responses)
	}

	logger.SafeInfo("ntp", "Pool query completed", map[string]interface{}{
		"pool":        p.name,
		"strategy":    p.strategy,
		"active":      response.ActiveServers,
		"total":       response.TotalServers,
		"best_offset": response.BestOffset.Seconds(),
	})

	return response, nil
}

// selectServersByStrategy selects servers based on the configured strategy
func (p *Pool) selectServersByStrategy(servers []string) []string {
	switch p.strategy {
	case "all":
		// Query all servers
		return servers

	case "round_robin":
		// Round-robin: select one server per query
		// Use time-based selection to distribute across all servers over time
		if len(servers) == 0 {
			return []string{}
		}
		index := int(time.Now().Unix()) % len(servers)
		return []string{servers[index]}

	case "best_n":
		fallthrough
	default:
		// Query up to maxServers (first N available)
		return servers
	}
}

// findBestOffset finds the offset with smallest absolute value
func (p *Pool) findBestOffset(responses []*Response) time.Duration {
	if len(responses) == 0 {
		return 0
	}

	best := responses[0].Offset
	bestAbs := mathutil.AbsDuration(best)

	for _, resp := range responses[1:] {
		abs := mathutil.AbsDuration(resp.Offset)
		if abs < bestAbs {
			best = resp.Offset
			bestAbs = abs
		}
	}

	return best
}

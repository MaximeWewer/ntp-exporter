package ntp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sony/gobreaker"
)

// CircuitBreakerClient wraps an NTPQuerier with circuit breaker protection.
type CircuitBreakerClient struct {
	querier  NTPQuerier
	breakers map[string]*gobreaker.CircuitBreaker
	mu       sync.RWMutex
	config   CircuitBreakerConfig
}

// CircuitBreakerConfig holds configuration for circuit breakers.
type CircuitBreakerConfig struct {
	// MaxRequests is the maximum number of requests allowed to pass through
	// when the CircuitBreaker is half-open.
	MaxRequests uint32

	// Interval is the cyclic period of the closed state
	// for the CircuitBreaker to clear the internal Counts.
	Interval time.Duration

	// Timeout is the period of the open state,
	// after which the state becomes half-open.
	Timeout time.Duration

	// ReadyToTrip is called with a copy of Counts whenever a request fails in the closed state.
	// If ReadyToTrip returns true, the CircuitBreaker will be placed into the open state.
	// If ReadyToTrip is nil, default behavior is: consecutiveFailures > 5
	ReadyToTrip func(counts gobreaker.Counts) bool
}

// DefaultCircuitBreakerConfig returns sensible defaults for circuit breaker configuration.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		MaxRequests: 3,
		Interval:    60 * time.Second,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 3 && failureRatio >= 0.6
		},
	}
}

// NewCircuitBreakerConfigWithThreshold creates a circuit breaker config with custom failure threshold.
func NewCircuitBreakerConfigWithThreshold(maxRequests uint32, interval, timeout time.Duration, failureThreshold float64) CircuitBreakerConfig {
	return CircuitBreakerConfig{
		MaxRequests: maxRequests,
		Interval:    interval,
		Timeout:     timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 3 && failureRatio >= failureThreshold
		},
	}
}

// NewCircuitBreakerClient creates a new circuit breaker protected NTP client.
func NewCircuitBreakerClient(querier NTPQuerier, config CircuitBreakerConfig) *CircuitBreakerClient {
	if config.MaxRequests == 0 {
		config = DefaultCircuitBreakerConfig()
	}

	return &CircuitBreakerClient{
		querier:  querier,
		breakers: make(map[string]*gobreaker.CircuitBreaker),
		config:   config,
	}
}

// getBreakerForServer returns or creates a circuit breaker for the given server.
func (cb *CircuitBreakerClient) getBreakerForServer(server string) *gobreaker.CircuitBreaker {
	cb.mu.RLock()
	breaker, exists := cb.breakers[server]
	cb.mu.RUnlock()

	if exists {
		return breaker
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Double-check after acquiring write lock
	if breaker, exists := cb.breakers[server]; exists {
		return breaker
	}

	// Create new circuit breaker for this server
	breaker = gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        server,
		MaxRequests: cb.config.MaxRequests,
		Interval:    cb.config.Interval,
		Timeout:     cb.config.Timeout,
		ReadyToTrip: cb.config.ReadyToTrip,
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			// Log state change (could be integrated with Prometheus metrics)
			_ = name
			_ = from
			_ = to
		},
	})

	cb.breakers[server] = breaker
	return breaker
}

// Query performs a single NTP query with circuit breaker protection.
func (cb *CircuitBreakerClient) Query(ctx context.Context, server string) (*Response, error) {
	breaker := cb.getBreakerForServer(server)

	result, err := breaker.Execute(func() (interface{}, error) {
		return cb.querier.Query(ctx, server)
	})

	if err != nil {
		// Check if circuit breaker is open
		if errors.Is(err, gobreaker.ErrOpenState) {
			return nil, fmt.Errorf("circuit breaker open for %s: %w", server, err)
		}
		return nil, err
	}

	return result.(*Response), nil
}

// QueryMultiple performs multiple NTP queries with circuit breaker protection.
func (cb *CircuitBreakerClient) QueryMultiple(ctx context.Context, server string, samples int) ([]*Response, error) {
	breaker := cb.getBreakerForServer(server)

	result, err := breaker.Execute(func() (interface{}, error) {
		return cb.querier.QueryMultiple(ctx, server, samples)
	})

	if err != nil {
		if errors.Is(err, gobreaker.ErrOpenState) {
			return nil, fmt.Errorf("circuit breaker open for %s: %w", server, err)
		}
		return nil, err
	}

	return result.([]*Response), nil
}

// GetState returns the current state of the circuit breaker for a server.
func (cb *CircuitBreakerClient) GetState(server string) gobreaker.State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	breaker, exists := cb.breakers[server]
	if !exists {
		return gobreaker.StateClosed
	}

	return breaker.State()
}

// GetCounts returns the current counts for a server's circuit breaker.
func (cb *CircuitBreakerClient) GetCounts(server string) gobreaker.Counts {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	breaker, exists := cb.breakers[server]
	if !exists {
		return gobreaker.Counts{}
	}

	return breaker.Counts()
}

// GetAllStates returns the states of all circuit breakers.
func (cb *CircuitBreakerClient) GetAllStates() map[string]gobreaker.State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	states := make(map[string]gobreaker.State)
	for server, breaker := range cb.breakers {
		states[server] = breaker.State()
	}

	return states
}

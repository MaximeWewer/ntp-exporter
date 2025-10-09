package collector

import (
	"context"
	"fmt"

	"github.com/maximewewer/ntp-exporter/pkg/logger"
)

// Collector represents a metrics collector
type Collector interface {
	// Collect collects metrics
	Collect(ctx context.Context) error

	// Name returns the name of the collector
	Name() string

	// Enabled indicates if the collector is active
	Enabled() bool
}

// Registry manages multiple collectors
type Registry struct {
	collectors []Collector
}

// NewRegistry creates a new collector registry
func NewRegistry() *Registry {
	return &Registry{
		collectors: make([]Collector, 0),
	}
}

// Register registers a collector
func (r *Registry) Register(c Collector) {
	r.collectors = append(r.collectors, c)
}

// CollectAll collects metrics from all enabled collectors
func (r *Registry) CollectAll(ctx context.Context) error {
	var errs []error

	for _, c := range r.collectors {
		if !c.Enabled() {
			continue
		}

		if err := c.Collect(ctx); err != nil {
			logger.SafeWarn("collector", "Collection failed", map[string]interface{}{
				"collector": c.Name(),
				"error":     err.Error(),
			})
			errs = append(errs, fmt.Errorf("%s: %w", c.Name(), err))
		}
	}

	if len(errs) > 0 {
		// Return first error for simplicity
		return errs[0]
	}

	return nil
}

// List returns all registered collectors
func (r *Registry) List() []Collector {
	return r.collectors
}

// Count returns the number of registered collectors
func (r *Registry) Count() int {
	return len(r.collectors)
}

// EnabledCount returns the number of enabled collectors
func (r *Registry) EnabledCount() int {
	count := 0
	for _, c := range r.collectors {
		if c.Enabled() {
			count++
		}
	}
	return count
}

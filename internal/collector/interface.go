package collector

import (
	"github.com/maximewewer/ntp-exporter/internal/probe"
)

// Publisher turns the measurements of a cycle into metrics
type Publisher interface {
	// Publish updates metrics from the result of a collection cycle
	Publish(result *probe.CycleResult)

	// Name returns the name of the publisher
	Name() string
}

// Registry manages multiple publishers
type Registry struct {
	publishers []Publisher
}

// NewRegistry creates a new publisher registry
func NewRegistry() *Registry {
	return &Registry{}
}

// Register registers a publisher
func (r *Registry) Register(p Publisher) {
	r.publishers = append(r.publishers, p)
}

// PublishAll hands the cycle result to every publisher
func (r *Registry) PublishAll(result *probe.CycleResult) {
	for _, p := range r.publishers {
		p.Publish(result)
	}
}

// List returns all registered publishers
func (r *Registry) List() []Publisher {
	return r.publishers
}

// Count returns the number of registered publishers
func (r *Registry) Count() int {
	return len(r.publishers)
}

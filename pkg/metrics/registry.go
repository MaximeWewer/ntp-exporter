package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Registry manages Prometheus metric registration
type Registry struct {
	registry   *prometheus.Registry
	ntpMetrics *NTPMetrics
}

// NewRegistry creates a new metrics registry with NTP metrics
// Uses default namespace "ntp" and empty subsystem
func NewRegistry() *Registry {
	return NewRegistryWithConfig("ntp", "")
}

// NewRegistryWithConfig creates a new metrics registry with custom namespace and subsystem
func NewRegistryWithConfig(namespace, subsystem string) *Registry {
	return &Registry{
		registry:   prometheus.NewRegistry(),
		ntpMetrics: NewNTPMetricsWithConfig(namespace, subsystem),
	}
}

// Register registers all NTP exporter metrics
func (r *Registry) Register() error {
	// Register the NTP metrics collector
	if err := r.registry.Register(r.ntpMetrics); err != nil {
		return err
	}

	// Register Go runtime metrics
	r.registry.MustRegister(collectors.NewGoCollector())
	r.registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	return nil
}

// GetRegistry returns the underlying Prometheus registry
func (r *Registry) GetRegistry() *prometheus.Registry {
	return r.registry
}

// GetMetrics returns the NTP metrics instance
func (r *Registry) GetMetrics() *NTPMetrics {
	return r.ntpMetrics
}

// MustRegister registers all metrics and panics on error
func (r *Registry) MustRegister() {
	if err := r.Register(); err != nil {
		panic(err)
	}
}

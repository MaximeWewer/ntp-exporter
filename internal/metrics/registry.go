package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Registry manages Prometheus metric registration
type Registry struct {
	registry   *prometheus.Registry
	registerer prometheus.Registerer
	ntpMetrics *NTPMetrics
}

// NewRegistry creates the metrics registry. Optional constant labels are
// added to every exporter metric.
func NewRegistry(constLabels ...map[string]string) *Registry {
	registry := prometheus.NewRegistry()

	labels := prometheus.Labels{}
	for _, l := range constLabels {
		for k, v := range l {
			labels[k] = v
		}
	}

	var registerer prometheus.Registerer = registry
	if len(labels) > 0 {
		registerer = prometheus.WrapRegistererWith(labels, registry)
	}

	return &Registry{
		registry:   registry,
		registerer: registerer,
		ntpMetrics: NewNTPMetrics(),
	}
}

// Register registers the exporter metrics and the Go runtime collectors
func (r *Registry) Register() error {
	if err := r.registerer.Register(r.ntpMetrics); err != nil {
		return err
	}

	if err := r.registry.Register(collectors.NewGoCollector()); err != nil {
		return err
	}
	return r.registry.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
}

// RegisterCollector registers an additional exporter collector, with the
// configured constant labels
func (r *Registry) RegisterCollector(c prometheus.Collector) error {
	return r.registerer.Register(c)
}

// GetRegistry returns the underlying Prometheus registry
func (r *Registry) GetRegistry() *prometheus.Registry {
	return r.registry
}

// GetMetrics returns the NTP metrics instance
func (r *Registry) GetMetrics() *NTPMetrics {
	return r.ntpMetrics
}

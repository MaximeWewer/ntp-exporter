package server

import (
	"html/template"
	"net/http"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/logger"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ReadinessFunc returns nil when the exporter serves fresh measurements
type ReadinessFunc func() error

// Handlers contains HTTP request handlers
type Handlers struct {
	config    *config.Config
	metrics   http.Handler
	readiness ReadinessFunc
}

// NewHandlers creates the handlers. The promhttp handler is built once.
func NewHandlers(cfg *config.Config, registry *prometheus.Registry, readiness ReadinessFunc) *Handlers {
	return &Handlers{
		config: cfg,
		metrics: promhttp.HandlerFor(registry, promhttp.HandlerOpts{
			ErrorLog:          &loggerAdapter{},
			ErrorHandling:     promhttp.ContinueOnError,
			EnableOpenMetrics: true,
		}),
		readiness: readiness,
	}
}

// MetricsHandler serves Prometheus metrics
func (h *Handlers) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	h.metrics.ServeHTTP(w, r)
}

// LivenessHandler reports that the process is alive
func (h *Handlers) LivenessHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"alive"}`))
}

// ReadinessHandler reports whether the exporter serves fresh measurements
func (h *Handlers) ReadinessHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if h.readiness != nil {
		if err := h.readiness(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"not ready","reason":` + quote(err.Error()) + `}`))
			return
		}
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ready"}`))
}

var indexTemplate = template.Must(template.New("index").Parse(`<!DOCTYPE html>
<html>
<head><title>NTP Exporter</title></head>
<body>
<h1>NTP Prometheus Exporter</h1>
<ul>
<li><a href="/metrics">/metrics</a> - Prometheus metrics</li>
<li><a href="/healthz">/healthz</a> - Liveness</li>
<li><a href="/readyz">/readyz</a> - Readiness (fresh measurements available)</li>
{{- if .ProbeEndpoint.IsEnabled}}
<li><a href="/probe?target=time.cloudflare.com">/probe?target=</a> - Multi-target probe, driven by Prometheus relabeling</li>
{{- end}}
</ul>
<p>{{len .NTP.Servers}} server(s), {{len .NTP.Pools}} pool(s), {{.NTP.SamplesPerServer}} sample(s) per server every {{.NTP.ScrapeInterval}}</p>
</body>
</html>
`))

// IndexHandler serves the index page
func (h *Handlers) IndexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := indexTemplate.Execute(w, h.config); err != nil {
		logger.Error("server", "Failed to render index", err)
	}
}

// quote returns s as a JSON string literal
func quote(s string) string {
	out := make([]byte, 0, len(s)+2)
	out = append(out, '"')
	for _, c := range []byte(s) {
		switch c {
		case '"', '\\':
			out = append(out, '\\', c)
		case '\n':
			out = append(out, '\\', 'n')
		default:
			if c < 0x20 {
				continue
			}
			out = append(out, c)
		}
	}
	return string(append(out, '"'))
}

// loggerAdapter adapts internal/logger to the promhttp logger interface
type loggerAdapter struct{}

func (l *loggerAdapter) Println(v ...interface{}) {
	msg := ""
	for i, val := range v {
		if i > 0 {
			msg += " "
		}
		switch x := val.(type) {
		case string:
			msg += x
		case error:
			msg += x.Error()
		}
	}
	logger.Error("promhttp", msg, nil)
}

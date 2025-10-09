package server

import (
	"net/http"
	"strconv"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/pkg/logger"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Handlers contains HTTP request handlers
type Handlers struct {
	config   *config.Config
	registry *prometheus.Registry
}

// NewHandlers creates a new handlers instance
func NewHandlers(cfg *config.Config, registry *prometheus.Registry) *Handlers {
	return &Handlers{
		config:   cfg,
		registry: registry,
	}
}

// MetricsHandler serves Prometheus metrics
func (h *Handlers) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	handler := promhttp.HandlerFor(h.registry, promhttp.HandlerOpts{
		ErrorLog:      &loggerAdapter{},
		ErrorHandling: promhttp.ContinueOnError,
	})

	handler.ServeHTTP(w, r)
}

// HealthHandler returns health status
func (h *Handlers) HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := `{"status":"healthy","service":"ntp-exporter"}`
	w.Write([]byte(response))
}

// IndexHandler serves the index page
func (h *Handlers) IndexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)

	// Build HTML response without fmt
	html := `<!DOCTYPE html>
<html>
<head>
    <title>NTP Exporter</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 40px; }
        h1 { color: #333; }
        ul { list-style-type: none; padding: 0; }
        li { margin: 10px 0; }
        a { color: #0066cc; text-decoration: none; }
        a:hover { text-decoration: underline; }
        .info { background-color: #f0f0f0; padding: 15px; border-radius: 5px; }
    </style>
</head>
<body>
    <h1>NTP Prometheus Exporter</h1>
    <div class="info">
        <h2>Available Endpoints:</h2>
        <ul>
            <li><a href="/metrics">/metrics</a> - Prometheus metrics</li>
            <li><a href="/health">/health</a> - Health check</li>
        </ul>
        <h2>Configuration:</h2>
        <ul>
            <li>NTP Servers: ` + strconv.Itoa(len(h.config.NTP.Servers)) + ` configured</li>
            <li>NTP Pools: ` + strconv.Itoa(len(h.config.NTP.Pools)) + ` configured</li>
            <li>Samples per server: ` + strconv.Itoa(h.config.NTP.SamplesPerServer) + `</li>
            <li>Timeout: ` + h.config.NTP.Timeout.String() + `</li>
            <li>NTP Version: ` + strconv.Itoa(h.config.NTP.Version) + `</li>
        </ul>
    </div>
</body>
</html>`

	w.Write([]byte(html))
}

// loggerAdapter adapts pkg/logger to promhttp logger interface
type loggerAdapter struct{}

func (l *loggerAdapter) Println(v ...interface{}) {
	// Convert v to string without fmt
	msg := ""
	for i, val := range v {
		if i > 0 {
			msg += " "
		}
		if s, ok := val.(string); ok {
			msg += s
		} else if err, ok := val.(error); ok {
			msg += err.Error()
		}
	}
	logger.Error("promhttp", msg, nil)
}

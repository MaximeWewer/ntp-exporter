package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/collector"
	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/logger"
	"github.com/maximewewer/ntp-exporter/internal/metrics"
	"github.com/maximewewer/ntp-exporter/internal/ntp"
	"github.com/maximewewer/ntp-exporter/internal/probe"
	"github.com/maximewewer/ntp-exporter/internal/server"
	"github.com/prometheus/exporter-toolkit/web"
)

// Build information, set with -ldflags "-X main.version=... -X main.commit=... -X main.buildDate=..."
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	// Parse command-line flags
	configFile := flag.String("config", "", "Path to configuration file")
	showVersion := flag.Bool("version", false, "Show version information")
	webConfigFile := flag.String("web.config.file", "", "Path to a Prometheus web configuration file (TLS, basic auth)")
	listenAddress := flag.String("web.listen-address", "", "Address to listen on, e.g. :9559 (overrides server address and port)")
	healthcheck := flag.Bool("healthcheck", false, "Query the local /healthz endpoint and exit (for container health checks)")
	flag.Parse()

	// Show version and exit if requested
	if *showVersion {
		_, _ = os.Stdout.WriteString("ntp-exporter version " + version + " (commit " + commit + ", built " + buildDate + ", " + runtime.Version() + ")\n")
		os.Exit(0)
	}

	// Load configuration (before logger is initialized)
	cfg, err := loadConfig(*configFile)
	if err == nil {
		err = applyWebFlags(cfg, *webConfigFile, *listenAddress)
	}
	if *healthcheck {
		os.Exit(runHealthcheck(cfg, err))
	}
	if err != nil {
		// Cannot use logger yet, write to stderr
		os.Stderr.WriteString("Failed to load configuration: " + err.Error() + "\n")
		os.Exit(1)
	}

	// Initialize logger
	if err := logger.InitLogger(logger.Config{
		Level:     cfg.Logging.Level,
		Format:    cfg.Logging.Format,
		Component: "ntp-exporter",
	}); err != nil {
		os.Stderr.WriteString("Failed to initialize logger: " + err.Error() + "\n")
		os.Exit(1)
	}

	// Log startup information
	logger.Startup(version, commit, map[string]interface{}{
		"build_date": buildDate,
		"go_version": runtime.Version(),
		"config":     cfg,
	})

	for _, warning := range config.Warnings(cfg) {
		logger.Warn("config", warning)
	}

	// In agent mode every series describes this node
	if cfg.Mode == config.ModeAgent {
		cfg.Metrics.Labels["node"] = collector.NodeName()
	}

	// Create metrics registry with custom namespace and subsystem from config
	registry := metrics.NewRegistry(cfg.Metrics.Labels)
	if err := registry.Register(); err != nil {
		logger.Fatal("main", "Failed to register metrics", err)
	}

	// Get metrics instance
	m := registry.GetMetrics()

	// Set build info metric
	m.ExporterBuildInfo.WithLabelValues(version, commit, runtime.Version()).Set(1)
	m.ExporterServersConfigured.Set(float64(len(cfg.NTP.Servers) + len(cfg.NTP.Pools)))

	// A single prober queries every target once per cycle; the exporter
	// exposes the measurements of the last cycle at scrape time
	client := ntp.NewClient(cfg.NTP.Timeout, cfg.NTP.Version)
	client.SetSampleSpacing(cfg.NTP.SampleSpacing)
	prober := probe.New(cfg, client)
	exporter := collector.NewExporter(cfg, m)
	if err := registry.RegisterCollector(exporter); err != nil {
		logger.Fatal("main", "Failed to register NTP collector", err)
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start HTTP server
	srv := server.New(cfg, registry.GetRegistry(), exporter.Ready)
	if cfg.ProbeEndpoint.IsEnabled() {
		policy, err := probe.NewTargetPolicy(cfg.ProbeEndpoint.AllowedTargets)
		if err != nil {
			logger.Fatal("main", "Invalid /probe configuration", err)
		}
		prober.SetTargetPolicy(policy)
		srv.SetProbeHandler(collector.NewProbeHandler(cfg, prober))
	}
	serverErrChan := make(chan error, 1)
	go func() {
		serverErrChan <- srv.Start(ctx)
	}()

	// Start collection loop
	collectorErrChan := make(chan error, 1)
	go func() {
		collectorErrChan <- runCollectionLoop(ctx, cfg, prober, exporter)
	}()

	sigChan := make(chan os.Signal, 2)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	reason := waitAndShutdown(cancel, sigChan, serverErrChan, collectorErrChan, shutdownTimeout)
	logger.Shutdown(reason)
}

// shutdownTimeout bounds the graceful shutdown
const shutdownTimeout = 10 * time.Second

// waitAndShutdown waits for a signal or for the end of the server or of the
// collection loop, then cancels the context and waits for both to stop.
// Cancelling the context ends the requests in flight and the current cycle; a
// second signal, or the timeout, ends the wait. It returns the shutdown reason.
func waitAndShutdown(cancel context.CancelFunc, signals <-chan os.Signal, serverErr, collectorErr <-chan error,
	timeout time.Duration) string {
	serverDone, collectorDone := false, false
	select {
	case sig := <-signals:
		logger.SafeInfo("main", "Received shutdown signal", map[string]interface{}{"signal": sig.String()})
	case err := <-serverErr:
		serverDone = true
		if err != nil {
			logger.Error("main", "Server error", err)
		}
	case err := <-collectorErr:
		collectorDone = true
		if err != nil {
			logger.Error("main", "Collector error", err)
		}
	}

	cancel()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for !serverDone || !collectorDone {
		select {
		case err := <-serverErr:
			serverDone = true
			if err != nil {
				logger.Error("main", "Server shutdown error", err)
			}
		case err := <-collectorErr:
			collectorDone = true
			if err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("main", "Collector error", err)
			}
		case sig := <-signals:
			logger.SafeWarn("main", "Second signal, forcing shutdown", map[string]interface{}{"signal": sig.String()})
			return "forced"
		case <-deadline.C:
			logger.Warn("main", "Shutdown timed out")
			return "timeout"
		}
	}
	return "graceful"
}

// loadConfig loads configuration based on whether a config file is specified
func loadConfig(configFile string) (*config.Config, error) {
	if configFile != "" {
		// Load from YAML file with environment variable overrides
		// Priority: Environment Variables > YAML File > Defaults
		return config.LoadFromYamlWithEnvOverrides(configFile)
	}
	// No config file specified, use environment variables only
	// Priority: Environment Variables > Defaults
	return config.LoadFromEnvVarsOnly()
}

// runCollectionLoop runs one probe cycle per scrape interval
func runCollectionLoop(
	ctx context.Context,
	cfg *config.Config,
	prober *probe.Prober,
	exporter *collector.Exporter,
) error {
	runCycle := func() {
		result := prober.Run(ctx)
		if ctx.Err() != nil {
			return
		}
		exporter.Update(result)
		logCycle(result)
	}

	// Initial collection
	runCycle()

	ticker := time.NewTicker(cfg.NTP.ScrapeInterval)
	defer ticker.Stop()

	logger.SafeInfo("main", "Collection loop started", map[string]interface{}{
		"scrape_interval": cfg.NTP.ScrapeInterval,
	})

	for {
		select {
		case <-ctx.Done():
			logger.Info("main", "Collection loop stopped")
			return nil
		case <-ticker.C:
			runCycle()
		}
	}
}

// logCycle emits one summary line per collection cycle; details per target
// are logged at debug level by the prober
func logCycle(result *probe.CycleResult) {
	reachable, valid, skipped, suspended := 0, 0, 0, 0
	for _, m := range result.Measurements {
		switch {
		case m.SuspendCode != "":
			suspended++
		case m.Skipped:
			skipped++
		}
		if m.Reachable() {
			reachable++
		}
		if m.Best() != nil {
			valid++
		}
	}

	logger.SafeInfo("main", "Collection cycle completed", map[string]interface{}{
		"targets":   len(result.Measurements),
		"reachable": reachable,
		"valid":     valid,
		"backoff":   skipped,
		"suspended": suspended,
		"duration":  result.Duration.Round(time.Millisecond).String(),
		"overrun":   result.Overrun,
	})
}

// runHealthcheck queries the liveness endpoint of the local exporter and
// returns the process exit code. Distroless images have no shell nor wget, so
// container health checks run the exporter binary itself.
func runHealthcheck(cfg *config.Config, cfgErr error) int {
	if cfgErr != nil {
		_, _ = os.Stderr.WriteString("healthcheck: invalid configuration: " + cfgErr.Error() + "\n")
		return 1
	}

	host := cfg.Server.Address
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	hostPort := net.JoinHostPort(host, strconv.Itoa(cfg.Server.Port))

	// TLS may be enabled by the web configuration file: try HTTPS first then.
	// Local check of our own endpoint: the certificate is issued for the public name.
	client := &http.Client{
		Timeout:   3 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec // loopback health check
	}
	schemes := []string{"http"}
	if cfg.Server.WebConfigFile != "" {
		schemes = []string{"https", "http"}
	}

	var lastErr string
	for _, scheme := range schemes {
		url := scheme + "://" + hostPort + "/healthz"
		resp, err := client.Get(url)
		if err != nil {
			lastErr = err.Error()
			continue
		}
		_ = resp.Body.Close()
		// 401 means basic auth is enabled: the exporter is alive
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized {
			return 0
		}
		lastErr = url + " returned " + resp.Status
	}

	_, _ = os.Stderr.WriteString("healthcheck: " + lastErr + "\n")
	return 1
}

// applyWebFlags applies the standard Prometheus exporter web flags, which take
// precedence over the configuration file and environment variables
func applyWebFlags(cfg *config.Config, webConfigFile, listenAddress string) error {
	if webConfigFile != "" {
		cfg.Server.WebConfigFile = webConfigFile
	}
	if listenAddress != "" {
		host, port, err := net.SplitHostPort(listenAddress)
		if err != nil {
			return fmt.Errorf("invalid --web.listen-address %q: %w", listenAddress, err)
		}
		p, err := strconv.Atoi(port)
		if err != nil || p < 1 || p > 65535 {
			return fmt.Errorf("invalid port in --web.listen-address %q", listenAddress)
		}
		if host == "" {
			host = "0.0.0.0"
		}
		cfg.Server.Address = host
		cfg.Server.Port = p
	}
	if cfg.Server.WebConfigFile != "" {
		if err := web.Validate(cfg.Server.WebConfigFile); err != nil {
			return fmt.Errorf("invalid web configuration file %s: %w", cfg.Server.WebConfigFile, err)
		}
	}
	return nil
}

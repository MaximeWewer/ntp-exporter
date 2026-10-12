package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/collector"
	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/probe"
	"github.com/maximewewer/ntp-exporter/internal/server"
	"github.com/maximewewer/ntp-exporter/pkg/logger"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
)

var (
	// Build information
	version = "dev"
)

func main() {
	// Parse command-line flags
	configFile := flag.String("config", "", "Path to configuration file")
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	// Show version and exit if requested
	if *showVersion {
		// Use println for version output (user-facing, not logging)
		println("ntp-exporter version", version)
		os.Exit(0)
	}

	// Load configuration (before logger is initialized)
	cfg, err := loadConfig(*configFile)
	if err != nil {
		// Cannot use logger yet, write to stderr
		os.Stderr.WriteString("Failed to load configuration: " + err.Error() + "\n")
		os.Exit(1)
	}

	// Initialize logger
	if err := logger.InitLogger(logger.Config{
		Level:      cfg.Logging.Level,
		Format:     cfg.Logging.Format,
		Output:     cfg.Logging.Output,
		FilePath:   cfg.Logging.FilePath,
		Component:  "ntp-exporter",
		EnableFile: cfg.Logging.EnableFile,
	}); err != nil {
		os.Stderr.WriteString("Failed to initialize logger: " + err.Error() + "\n")
		os.Exit(1)
	}

	// Log startup information
	logger.Startup(version, "", map[string]interface{}{
		"go_version": runtime.Version(),
		"config":     cfg,
	})

	for _, warning := range config.Warnings(cfg) {
		logger.Warn("config", warning)
	}

	// Create metrics registry with custom namespace and subsystem from config
	registry := metrics.NewRegistryWithConfig(cfg.Metrics.Namespace, cfg.Metrics.Subsystem, cfg.Metrics.Labels)
	if err := registry.Register(); err != nil {
		logger.Fatal("main", "Failed to register metrics", err)
	}

	// Get metrics instance
	m := registry.GetMetrics()

	// Set build info metric
	m.ExporterBuildInfo.WithLabelValues(version, "", runtime.Version()).Set(1)
	m.ExporterServersConfigured.Set(float64(len(cfg.NTP.Servers) + len(cfg.NTP.Pools)))

	// A single prober queries every target once per cycle; the exporter
	// exposes the measurements of the last cycle at scrape time
	prober := probe.New(cfg, probe.NewClient(cfg))
	exporter := collector.NewExporter(cfg, m)
	if err := registry.RegisterCollector(exporter); err != nil {
		logger.Fatal("main", "Failed to register NTP collector", err)
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start HTTP server
	srv := server.New(cfg, registry.GetRegistry(), exporter.Ready)
	serverErrChan := make(chan error, 1)
	go func() {
		serverErrChan <- srv.Start(ctx)
	}()

	// Start collection loop
	collectorErrChan := make(chan error, 1)
	go func() {
		collectorErrChan <- runCollectionLoop(ctx, cfg, prober, exporter)
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	serverStopped := false
	select {
	case sig := <-sigChan:
		logger.SafeInfo("main", "Received shutdown signal", map[string]interface{}{"signal": sig.String()})
	case err := <-serverErrChan:
		serverStopped = true
		if err != nil {
			logger.Error("main", "Server error", err)
		}
	case err := <-collectorErrChan:
		if err != nil {
			logger.Error("main", "Collector error", err)
		}
	}

	// Graceful shutdown: cancelling the context stops the collection loop and
	// makes the server shut down gracefully
	cancel()
	if !serverStopped {
		select {
		case err := <-serverErrChan:
			if err != nil {
				logger.Error("main", "Server shutdown error", err)
			}
		case <-time.After(15 * time.Second):
			logger.Warn("main", "Server shutdown timed out")
		}
	}

	logger.Shutdown("graceful")
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

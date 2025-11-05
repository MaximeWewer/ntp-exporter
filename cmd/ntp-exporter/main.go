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

	// Create metrics registry with custom namespace and subsystem from config
	registry := metrics.NewRegistryWithConfig(cfg.Metrics.Namespace, cfg.Metrics.Subsystem)
	if err := registry.Register(); err != nil {
		logger.Fatal("main", "Failed to register metrics", err)
	}

	// Get metrics instance
	m := registry.GetMetrics()

	// Set build info metric
	m.ExporterBuildInfo.WithLabelValues(version, "", runtime.Version()).Set(1)
	m.ExporterServersConfigured.Set(float64(len(cfg.NTP.Servers) + len(cfg.NTP.Pools)))

	// Create collector registry and register collectors
	collectorRegistry := collector.NewRegistry()
	collectorRegistry.Register(collector.NewBaseCollector(cfg, m))
	collectorRegistry.Register(collector.NewQualityCollector(cfg, m))
	collectorRegistry.Register(collector.NewSecurityCollector(cfg, m))

	// Register hybrid collector if kernel monitoring is enabled (Agent mode)
	if cfg.NTP.EnableKernel {
		collectorRegistry.Register(collector.NewHybridCollector(cfg, m))
		logger.Info("main", "Hybrid mode enabled - kernel metrics will be collected")
	}

	logger.SafeInfo("main", "Registered collectors", map[string]interface{}{
		"total":          collectorRegistry.Count(),
		"enabled":        collectorRegistry.EnabledCount(),
		"kernel_enabled": cfg.NTP.EnableKernel,
	})

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start HTTP server
	srv := server.New(cfg, registry.GetRegistry(), m)
	serverErrChan := make(chan error, 1)
	go func() {
		serverErrChan <- srv.Start(ctx)
	}()

	// Start collection loop
	collectorErrChan := make(chan error, 1)
	go func() {
		collectorErrChan <- runCollectionLoop(ctx, cfg, collectorRegistry)
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-sigChan:
		logger.SafeInfo("main", "Received shutdown signal", map[string]interface{}{"signal": sig.String()})
		cancel()
	case err := <-serverErrChan:
		if err != nil {
			logger.Error("main", "Server error", err)
		}
		cancel()
	case err := <-collectorErrChan:
		if err != nil {
			logger.Error("main", "Collector error", err)
		}
		cancel()
	}

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("main", "Server shutdown error", err)
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

// runCollectionLoop runs the metrics collection loop
func runCollectionLoop(
	ctx context.Context,
	cfg *config.Config,
	collectorRegistry *collector.Registry,
) error {
	// Initial collection
	if err := collectorRegistry.CollectAll(ctx); err != nil {
		logger.Warn("main", "Initial collection failed")
	}

	// Collection interval using configured scrape_interval
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
			start := time.Now()
			if err := collectorRegistry.CollectAll(ctx); err != nil {
				logger.Warn("main", "Collection failed")
			}
			logger.Metric("collection", "all", time.Since(start), true)
		}
	}
}

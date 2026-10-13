// Package logger is the structured logger of the exporter, built on log/slog
// and shared with exporter-toolkit so that every line has the same format.
// The JSON and text handlers escape the values: data coming from the network
// (server names, DNS answers, /probe targets) cannot forge log lines.
package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
)

var (
	level = new(slog.LevelVar)
	log   = newLogger(os.Stdout, "json", "ntp-exporter")
)

// Config holds logger configuration
type Config struct {
	Level     string // debug, info, warn, error
	Format    string // json, console
	Component string // component name for structured logging
}

// InitLogger configures the logger: level, format, standard output
func InitLogger(cfg Config) error {
	level.Set(parseLevel(cfg.Level))
	log = newLogger(os.Stdout, cfg.Format, cfg.Component)
	return nil
}

func newLogger(w io.Writer, format, component string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if format == "console" {
		handler = slog.NewTextHandler(w, opts)
	} else {
		handler = slog.NewJSONHandler(w, opts)
	}
	return slog.New(handler).With("component", component)
}

// Slog returns the underlying logger, for libraries such as exporter-toolkit
func Slog() *slog.Logger {
	return log
}

// parseLevel converts a level name, info by default
func parseLevel(name string) slog.Level {
	switch strings.ToLower(name) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// logAt logs a message of a package with key/value pairs. A disabled level
// returns before building the record.
func logAt(l slog.Level, pkg, message string, args []any) {
	ctx := context.Background()
	if !log.Enabled(ctx, l) {
		return
	}
	log.Log(ctx, l, message, append([]any{"package", pkg}, args...)...)
}

// Debug logs a debug message with key/value pairs
func Debug(pkg, message string, args ...any) {
	logAt(slog.LevelDebug, pkg, message, args)
}

// Info logs an info message with key/value pairs
func Info(pkg, message string, args ...any) {
	logAt(slog.LevelInfo, pkg, message, args)
}

// Warn logs a warning with key/value pairs
func Warn(pkg, message string, args ...any) {
	logAt(slog.LevelWarn, pkg, message, args)
}

// Error logs an error with key/value pairs
func Error(pkg, message string, err error, args ...any) {
	logAt(slog.LevelError, pkg, message, append([]any{"error", err}, args...))
}

// Fatal logs an error and exits
func Fatal(pkg, message string, err error) {
	Error(pkg, message, err)
	os.Exit(1)
}

// HTTP logs an HTTP request, at debug level
func HTTP(method, path string, statusCode int, duration time.Duration, remoteAddr string) {
	Debug("http", "HTTP request", "method", method, "path", path, "status", statusCode,
		"duration", duration, "remote_addr", remoteAddr)
}

// Startup logs application startup information
func Startup(version, commit string, config any) {
	Info("main", "NTP Exporter starting", "version", version, "commit", commit, "config", config)
}

// Shutdown logs application shutdown
func Shutdown(reason string) {
	Info("main", "NTP Exporter shutting down", "reason", reason)
}

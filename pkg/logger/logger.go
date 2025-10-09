package logger

import (
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	// Global logger instance
	Logger zerolog.Logger

	// Pool for field maps to reduce allocations
	fieldPool = sync.Pool{
		New: func() interface{} {
			return make(map[string]interface{})
		},
	}

	// Pre-compiled regex patterns for sensitive data detection
	passwordPattern   = regexp.MustCompile(`(?i)(password|passwd|pwd|secret|token|api[_-]?key|auth)`)
	credentialPattern = regexp.MustCompile(`(?i)://([^:]+):([^@]+)@`)
	ipPattern         = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
)

// Config holds logger configuration
type Config struct {
	Level      string // debug, info, warn, error
	Format     string // json, console
	Output     string // stdout, stderr, file
	FilePath   string // path to log file if output=file
	Component  string // component name for structured logging
	EnableFile bool   // enable file output
}

// InitLogger initializes the global logger with the provided configuration
func InitLogger(cfg Config) error {
	// Set log level
	level := parseLevel(cfg.Level)
	zerolog.SetGlobalLevel(level)

	// Configure output format
	var output zerolog.ConsoleWriter
	if cfg.Format == "console" {
		output = zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
			NoColor:    false,
		}
		Logger = zerolog.New(output).With().Timestamp().Str("component", cfg.Component).Logger()
	} else {
		// JSON format
		var writer io.Writer
		switch cfg.Output {
		case "stderr":
			writer = os.Stderr
		case "file":
			if cfg.EnableFile && cfg.FilePath != "" {
				file, err := os.OpenFile(cfg.FilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
				if err != nil {
					return err
				}
				writer = file
			} else {
				writer = os.Stdout
			}
		default:
			writer = os.Stdout
		}

		Logger = zerolog.New(writer).With().Timestamp().Str("component", cfg.Component).Logger()
	}

	// Set global logger
	log.Logger = Logger

	return nil
}

// parseLevel converts string level to zerolog.Level
func parseLevel(level string) zerolog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn", "warning":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	case "fatal":
		return zerolog.FatalLevel
	case "panic":
		return zerolog.PanicLevel
	default:
		return zerolog.InfoLevel
	}
}

// getFieldMap gets a map from the pool
func getFieldMap() map[string]interface{} {
	return fieldPool.Get().(map[string]interface{})
}

// putFieldMap returns a map to the pool
func putFieldMap(m map[string]interface{}) {
	// Clear the map
	for k := range m {
		delete(m, k)
	}
	fieldPool.Put(m)
}

// sanitizeFields removes or redacts sensitive information from fields
func sanitizeFields(fields map[string]interface{}) map[string]interface{} {
	safe := getFieldMap()
	defer putFieldMap(safe)

	result := make(map[string]interface{})
	for key, value := range fields {
		// Check if key contains sensitive keywords
		if passwordPattern.MatchString(key) {
			result[key] = "***REDACTED***"
			continue
		}

		// Sanitize string values
		if strValue, ok := value.(string); ok {
			result[key] = sanitizeString(strValue)
		} else {
			result[key] = value
		}
	}

	return result
}

// sanitizeString removes sensitive information from strings
func sanitizeString(s string) string {
	// Redact credentials in URLs
	s = credentialPattern.ReplaceAllString(s, "://$1:***@")

	// Optionally mask IP addresses (configurable)
	// s = ipPattern.ReplaceAllString(s, "x.x.x.x")

	return s
}

// Debug logs a debug message
func Debug(pkg, message string) {
	Logger.Debug().
		Str("package", pkg).
		Msg(message)
}

// Debugf logs a formatted debug message
func Debugf(pkg, format string, args ...interface{}) {
	Logger.Debug().
		Str("package", pkg).
		Msgf(format, args...)
}

// Info logs an info message
func Info(pkg, message string) {
	Logger.Info().
		Str("package", pkg).
		Msg(message)
}

// Infof logs a formatted info message
func Infof(pkg, format string, args ...interface{}) {
	Logger.Info().
		Str("package", pkg).
		Msgf(format, args...)
}

// Warn logs a warning message
func Warn(pkg, message string) {
	Logger.Warn().
		Str("package", pkg).
		Msg(message)
}

// Warnf logs a formatted warning message
func Warnf(pkg, format string, args ...interface{}) {
	Logger.Warn().
		Str("package", pkg).
		Msgf(format, args...)
}

// Error logs an error message
func Error(pkg, message string, err error) {
	Logger.Error().
		Str("package", pkg).
		Err(err).
		Msg(message)
}

// Errorf logs a formatted error message
func Errorf(pkg string, err error, format string, args ...interface{}) {
	Logger.Error().
		Str("package", pkg).
		Err(err).
		Msgf(format, args...)
}

// Fatal logs a fatal message and exits
func Fatal(pkg, message string, err error) {
	Logger.Fatal().
		Str("package", pkg).
		Err(err).
		Msg(message)
}

// Fatalf logs a formatted fatal message and exits
func Fatalf(pkg string, err error, format string, args ...interface{}) {
	Logger.Fatal().
		Str("package", pkg).
		Err(err).
		Msgf(format, args...)
}

// SafeDebug logs a debug message with sanitized fields
func SafeDebug(pkg, message string, fields map[string]interface{}) {
	sanitized := sanitizeFields(fields)
	event := Logger.Debug().Str("package", pkg)
	for k, v := range sanitized {
		event = event.Interface(k, v)
	}
	event.Msg(message)
}

// SafeInfo logs an info message with sanitized fields
func SafeInfo(pkg, message string, fields map[string]interface{}) {
	sanitized := sanitizeFields(fields)
	event := Logger.Info().Str("package", pkg)
	for k, v := range sanitized {
		event = event.Interface(k, v)
	}
	event.Msg(message)
}

// SafeWarn logs a warning message with sanitized fields
func SafeWarn(pkg, message string, fields map[string]interface{}) {
	sanitized := sanitizeFields(fields)
	event := Logger.Warn().Str("package", pkg)
	for k, v := range sanitized {
		event = event.Interface(k, v)
	}
	event.Msg(message)
}

// SafeError logs an error message with sanitized fields
func SafeError(pkg, message string, err error, fields map[string]interface{}) {
	sanitized := sanitizeFields(fields)
	event := Logger.Error().Str("package", pkg).Err(err)
	for k, v := range sanitized {
		event = event.Interface(k, v)
	}
	event.Msg(message)
}

// WithFields creates a logger with predefined fields
func WithFields(pkg string, fields map[string]interface{}) zerolog.Logger {
	sanitized := sanitizeFields(fields)
	ctx := Logger.With().Str("package", pkg)
	for k, v := range sanitized {
		ctx = ctx.Interface(k, v)
	}
	return ctx.Logger()
}

// HTTP logs HTTP request information
func HTTP(method, path string, statusCode int, duration time.Duration, remoteAddr string) {
	Logger.Info().
		Str("package", "http").
		Str("method", method).
		Str("path", path).
		Int("status", statusCode).
		Dur("duration", duration).
		Str("remote_addr", sanitizeString(remoteAddr)).
		Msg("HTTP request")
}

// Metric logs metric collection information
func Metric(collector, server string, duration time.Duration, success bool) {
	event := Logger.Debug().
		Str("package", "metrics").
		Str("collector", collector).
		Str("server", server).
		Dur("duration", duration).
		Bool("success", success)

	if success {
		event.Msg("Metric collected successfully")
	} else {
		event.Msg("Metric collection failed")
	}
}

// NTP logs NTP-specific operations
func NTP(operation, server string, fields map[string]interface{}) {
	sanitized := sanitizeFields(fields)
	event := Logger.Debug().
		Str("package", "ntp").
		Str("operation", operation).
		Str("server", server)

	for k, v := range sanitized {
		event = event.Interface(k, v)
	}

	event.Msg("NTP operation")
}

// Security logs security-related events
func Security(event, reason string, fields map[string]interface{}) {
	sanitized := sanitizeFields(fields)
	logEvent := Logger.Warn().
		Str("package", "security").
		Str("event", event).
		Str("reason", reason)

	for k, v := range sanitized {
		logEvent = logEvent.Interface(k, v)
	}

	logEvent.Msg("Security event detected")
}

// Startup logs application startup information
func Startup(version, commit string, config interface{}) {
	Logger.Info().
		Str("package", "main").
		Str("version", version).
		Str("commit", commit).
		Interface("config", config).
		Msg("NTP Exporter starting")
}

// Shutdown logs application shutdown
func Shutdown(reason string) {
	Logger.Info().
		Str("package", "main").
		Str("reason", reason).
		Msg("NTP Exporter shutting down")
}

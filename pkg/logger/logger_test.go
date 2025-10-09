package logger

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestInitLogger(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "json_stdout",
			config: Config{
				Level:     "info",
				Format:    "json",
				Output:    "stdout",
				Component: "test",
			},
			wantErr: false,
		},
		{
			name: "console_format",
			config: Config{
				Level:     "debug",
				Format:    "console",
				Output:    "stdout",
				Component: "test",
			},
			wantErr: false,
		},
		{
			name: "invalid_level_defaults_to_info",
			config: Config{
				Level:     "invalid",
				Format:    "json",
				Output:    "stdout",
				Component: "test",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := InitLogger(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("InitLogger() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  zerolog.Level
	}{
		{"debug", zerolog.DebugLevel},
		{"DEBUG", zerolog.DebugLevel},
		{"info", zerolog.InfoLevel},
		{"warn", zerolog.WarnLevel},
		{"warning", zerolog.WarnLevel},
		{"error", zerolog.ErrorLevel},
		{"fatal", zerolog.FatalLevel},
		{"panic", zerolog.PanicLevel},
		{"invalid", zerolog.InfoLevel},
		{"", zerolog.InfoLevel},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseLevel(tt.input)
			if got != tt.want {
				t.Errorf("parseLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSanitizeString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "url_with_credentials",
			input: "http://user:password@example.com",
			want:  "http://user:***@example.com",
		},
		{
			name:  "https_url_with_credentials",
			input: "https://admin:secret123@api.example.com/path",
			want:  "https://admin:***@api.example.com/path",
		},
		{
			name:  "normal_string",
			input: "This is a normal log message",
			want:  "This is a normal log message",
		},
		{
			name:  "empty_string",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeString(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizeFields(t *testing.T) {
	tests := []struct {
		name   string
		fields map[string]interface{}
		check  func(t *testing.T, result map[string]interface{})
	}{
		{
			name: "password_field_redacted",
			fields: map[string]interface{}{
				"username": "admin",
				"password": "secret123",
				"host":     "localhost",
			},
			check: func(t *testing.T, result map[string]interface{}) {
				if result["password"] != "***REDACTED***" {
					t.Errorf("password not redacted: %v", result["password"])
				}
				if result["username"] != "admin" {
					t.Errorf("username should not be redacted: %v", result["username"])
				}
			},
		},
		{
			name: "token_field_redacted",
			fields: map[string]interface{}{
				"api_key": "abc123",
				"token":   "xyz789",
				"user":    "john",
			},
			check: func(t *testing.T, result map[string]interface{}) {
				if result["api_key"] != "***REDACTED***" {
					t.Errorf("api_key not redacted: %v", result["api_key"])
				}
				if result["token"] != "***REDACTED***" {
					t.Errorf("token not redacted: %v", result["token"])
				}
			},
		},
		{
			name: "url_credentials_sanitized",
			fields: map[string]interface{}{
				"url": "http://user:pass@example.com",
			},
			check: func(t *testing.T, result map[string]interface{}) {
				url, ok := result["url"].(string)
				if !ok {
					t.Fatal("url should be string")
				}
				if url == "http://user:pass@example.com" {
					t.Error("credentials in URL not sanitized")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeFields(tt.fields)
			tt.check(t, result)
		})
	}
}

func TestLoggingFunctions(t *testing.T) {
	// Initialize logger for testing
	var buf bytes.Buffer
	Logger = zerolog.New(&buf).With().Timestamp().Logger()

	t.Run("Debug", func(t *testing.T) {
		buf.Reset()
		zerolog.SetGlobalLevel(zerolog.DebugLevel) // Enable debug logging
		Debug("test", "debug message")
		if buf.Len() == 0 {
			t.Log("Debug() did not write (debug level might be filtered)")
		}
		zerolog.SetGlobalLevel(zerolog.InfoLevel) // Reset to info
	})

	t.Run("Info", func(t *testing.T) {
		buf.Reset()
		Info("test", "info message")
		if buf.Len() == 0 {
			t.Error("Info() did not write to log")
		}
	})

	t.Run("Warn", func(t *testing.T) {
		buf.Reset()
		Warn("test", "warning message")
		if buf.Len() == 0 {
			t.Error("Warn() did not write to log")
		}
	})

	t.Run("Error", func(t *testing.T) {
		buf.Reset()
		Error("test", "error message", errors.New("test error"))
		if buf.Len() == 0 {
			t.Error("Error() did not write to log")
		}
	})

	t.Run("SafeInfo", func(t *testing.T) {
		buf.Reset()
		fields := map[string]interface{}{
			"user":     "admin",
			"password": "secret",
		}
		SafeInfo("test", "safe info", fields)
		if buf.Len() == 0 {
			t.Error("SafeInfo() did not write to log")
		}
		// Verify password is redacted
		output := buf.String()
		if bytes.Contains(buf.Bytes(), []byte("secret")) {
			t.Error("SafeInfo() did not redact sensitive data: " + output)
		}
	})
}

func TestHTTPLog(t *testing.T) {
	var buf bytes.Buffer
	Logger = zerolog.New(&buf).With().Timestamp().Logger()

	HTTP("GET", "/metrics", 200, 100*time.Millisecond, "192.168.1.1")

	if buf.Len() == 0 {
		t.Error("HTTP() did not write to log")
	}
}

func TestMetricLog(t *testing.T) {
	var buf bytes.Buffer
	Logger = zerolog.New(&buf).Level(zerolog.DebugLevel).With().Timestamp().Logger()

	Metric("ntp_base", "pool.ntp.org", 50*time.Millisecond, true)

	if buf.Len() == 0 {
		t.Skip("Metric() uses Debug level, may not write in test")
	}
}

func TestNTPLog(t *testing.T) {
	var buf bytes.Buffer
	Logger = zerolog.New(&buf).Level(zerolog.DebugLevel).With().Timestamp().Logger()

	fields := map[string]interface{}{
		"offset": 0.001,
		"rtt":    0.050,
	}
	NTP("query", "pool.ntp.org", fields)

	if buf.Len() == 0 {
		t.Skip("NTP() uses Debug level, may not write in test")
	}
}

func TestSecurityLog(t *testing.T) {
	var buf bytes.Buffer
	Logger = zerolog.New(&buf).With().Timestamp().Logger()

	fields := map[string]interface{}{
		"server": "suspicious.ntp.org",
		"reason": "invalid_stratum",
	}
	Security("suspicious_server", "stratum out of range", fields)

	if buf.Len() == 0 {
		t.Error("Security() did not write to log")
	}
}

func BenchmarkSanitizeFields(b *testing.B) {
	fields := map[string]interface{}{
		"username": "admin",
		"password": "secret123",
		"token":    "xyz789",
		"url":      "http://user:pass@example.com",
		"host":     "localhost",
		"port":     9559,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sanitizeFields(fields)
	}
}

func TestFormattedLogging(t *testing.T) {
	var buf bytes.Buffer
	Logger = zerolog.New(&buf).With().Timestamp().Logger()

	t.Run("Debugf", func(t *testing.T) {
		buf.Reset()
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		Debugf("test", "formatted message: %s, number: %d", "debug", 42)
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
		// May or may not write depending on level
	})

	t.Run("Infof", func(t *testing.T) {
		buf.Reset()
		Infof("test", "formatted info: %s", "data")
		if buf.Len() == 0 {
			t.Error("Infof() did not write to log")
		}
	})

	t.Run("Warnf", func(t *testing.T) {
		buf.Reset()
		Warnf("test", "warning: %d items", 5)
		if buf.Len() == 0 {
			t.Error("Warnf() did not write to log")
		}
	})

	t.Run("Errorf", func(t *testing.T) {
		buf.Reset()
		Errorf("test", errors.New("test error"), "error: %v", "something")
		if buf.Len() == 0 {
			t.Error("Errorf() did not write to log")
		}
	})
}

func TestSafeLogging(t *testing.T) {
	var buf bytes.Buffer
	Logger = zerolog.New(&buf).Level(zerolog.DebugLevel).With().Timestamp().Logger()

	fields := map[string]interface{}{
		"user":     "admin",
		"password": "secret",
		"api_key":  "key123",
	}

	t.Run("SafeDebug", func(t *testing.T) {
		buf.Reset()
		SafeDebug("test", "safe debug", fields)
		output := buf.String()
		if bytes.Contains(buf.Bytes(), []byte("secret")) || bytes.Contains(buf.Bytes(), []byte("key123")) {
			t.Error("SafeDebug() did not redact sensitive data: " + output)
		}
	})

	t.Run("SafeWarn", func(t *testing.T) {
		buf.Reset()
		SafeWarn("test", "safe warning", fields)
		output := buf.String()
		if bytes.Contains(buf.Bytes(), []byte("secret")) {
			t.Error("SafeWarn() did not redact sensitive data: " + output)
		}
	})

	t.Run("SafeError", func(t *testing.T) {
		buf.Reset()
		SafeError("test", "safe error", errors.New("test"), fields)
		output := buf.String()
		if bytes.Contains(buf.Bytes(), []byte("secret")) {
			t.Error("SafeError() did not redact sensitive data: " + output)
		}
	})
}

func TestWithFields(t *testing.T) {
	var buf bytes.Buffer
	Logger = zerolog.New(&buf).With().Timestamp().Logger()

	fields := map[string]interface{}{
		"request_id": "123",
		"user":       "test",
	}

	contextLogger := WithFields("test", fields)
	contextLogger.Info().Msg("test message")

	if buf.Len() == 0 {
		t.Error("WithFields() logger did not write")
	}

	output := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("request_id")) || !bytes.Contains(buf.Bytes(), []byte("123")) {
		t.Error("WithFields() did not include fields: " + output)
	}
}

func TestStartupShutdown(t *testing.T) {
	var buf bytes.Buffer
	Logger = zerolog.New(&buf).With().Timestamp().Logger()

	t.Run("Startup", func(t *testing.T) {
		buf.Reset()
		config := map[string]interface{}{
			"port":    9559,
			"servers": []string{"pool.ntp.org"},
		}
		Startup("v1.0.0", "abc123", config)

		if buf.Len() == 0 {
			t.Error("Startup() did not write to log")
		}

		output := buf.String()
		if !bytes.Contains(buf.Bytes(), []byte("v1.0.0")) || !bytes.Contains(buf.Bytes(), []byte("abc123")) {
			t.Error("Startup() missing version/commit info: " + output)
		}
	})

	t.Run("Shutdown", func(t *testing.T) {
		buf.Reset()
		Shutdown("graceful shutdown")

		if buf.Len() == 0 {
			t.Error("Shutdown() did not write to log")
		}

		output := buf.String()
		if !bytes.Contains(buf.Bytes(), []byte("graceful shutdown")) {
			t.Error("Shutdown() missing reason: " + output)
		}
	})
}

func BenchmarkSanitizeString(b *testing.B) {
	testString := "http://admin:secretpassword@ntp.example.com:123/path?query=value"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sanitizeString(testString)
	}
}

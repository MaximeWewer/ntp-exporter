package config

import (
	"os"
	"testing"
)

// The mode is required: tests load configurations in probe mode unless they
// set it themselves
func TestMain(m *testing.M) {
	if os.Getenv("NTP_EXPORTER_MODE") == "" {
		_ = os.Setenv("NTP_EXPORTER_MODE", "probe")
	}
	os.Exit(m.Run())
}

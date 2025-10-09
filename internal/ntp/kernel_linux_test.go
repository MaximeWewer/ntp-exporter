//go:build linux
// +build linux

package ntp

import (
	"testing"
)

// These tests run ONLY on Linux where adjtimex syscall is available

func TestGetStatusString(t *testing.T) {
	tests := []struct {
		name     string
		status   int32
		expected string
	}{
		{"synchronized", TIME_OK, "synchronized"},
		{"unsync", STA_UNSYNC, "unsynchronized"},
		{"clock_error", STA_CLOCKERR, "clock_error"},
		{"leap_insert", TIME_INS, "leap_insert_pending"},
		{"leap_delete", TIME_DEL, "leap_delete_pending"},
		{"leap_in_progress", TIME_OOP, "leap_in_progress"},
		{"leap_occurred", TIME_WAIT, "leap_occurred"},
		{"error", TIME_ERROR, "error"},
		{"unknown", 99, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getStatusString(tt.status)
			if result != tt.expected {
				t.Errorf("getStatusString(0x%04x) = %q, want %q", tt.status, result, tt.expected)
			}
		})
	}
}

func TestKernelTimexIsSynchronizedLinux(t *testing.T) {
	tests := []struct {
		name     string
		status   int32
		expected bool
	}{
		{"synchronized", 0, true},
		{"unsynchronized", STA_UNSYNC, false},
		{"with_pll", STA_PLL, true},
		{"mixed", STA_PLL | STA_UNSYNC, false}, // UNSYNC takes precedence
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kt := &KernelTimex{Status: tt.status}
			result := kt.IsSynchronized()
			if result != tt.expected {
				t.Errorf("IsSynchronized() = %v, want %v (status=0x%04x)",
					result, tt.expected, tt.status)
			}
		})
	}
}

func TestKernelTimexHasLeapSecondLinux(t *testing.T) {
	tests := []struct {
		name     string
		status   int32
		expected bool
	}{
		{"no_leap", 0, false},
		{"leap_insert", STA_INS, true},
		{"leap_delete", STA_DEL, true},
		{"both_flags", STA_INS | STA_DEL, true},
		{"other_flags", STA_PLL, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kt := &KernelTimex{Status: tt.status}
			result := kt.HasLeapSecond()
			if result != tt.expected {
				t.Errorf("HasLeapSecond() = %v, want %v (status=0x%04x)",
					result, tt.expected, tt.status)
			}
		})
	}
}

func TestKernelTimexIsPPSActiveLinux(t *testing.T) {
	tests := []struct {
		name     string
		status   int32
		expected bool
	}{
		{"no_pps", 0, false},
		{"pps_signal", STA_PPSSIGNAL, true},
		{"other_flags", STA_PLL, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kt := &KernelTimex{Status: tt.status}
			result := kt.IsPPSActive()
			if result != tt.expected {
				t.Errorf("IsPPSActive() = %v, want %v (status=0x%04x)",
					result, tt.expected, tt.status)
			}
		})
	}
}

func TestKernelReaderReadLinux(t *testing.T) {
	kr := NewKernelReader(true)
	state, err := kr.Read()

	// May fail if not running as root or without CAP_SYS_TIME
	if err != nil {
		t.Skipf("Kernel timex read failed (needs CAP_SYS_TIME or root): %v", err)
		return
	}

	if state == nil {
		t.Fatal("Read returned nil state without error")
	}

	// Validate fields are populated
	t.Logf("Kernel state:")
	t.Logf("  Offset: %v (%v seconds)", state.Offset, state.GetOffsetSeconds())
	t.Logf("  Frequency: %d (%v PPM)", state.Frequency, state.GetFrequencyPPM())
	t.Logf("  Status: %s (0x%04x)", state.SyncStatus, state.StatusCode)
	t.Logf("  Synchronized: %v", state.IsSynchronized())
	t.Logf("  MaxError: %v", state.MaxError)
	t.Logf("  EstError: %v", state.EstError)
	t.Logf("  Precision: %v", state.Precision)

	// Sanity checks
	if state.StatusCode < 0 {
		t.Error("Status code should not be negative")
	}

	if state.SyncStatus == "" {
		t.Error("SyncStatus should not be empty")
	}

	// Frequency should be within reasonable bounds (-500 to +500 PPM)
	freqPPM := state.GetFrequencyPPM()
	if freqPPM < -500 || freqPPM > 500 {
		t.Errorf("Frequency %v PPM out of reasonable range", freqPPM)
	}
}

func BenchmarkKernelReaderReadLinux(b *testing.B) {
	kr := NewKernelReader(true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := kr.Read()
		if err != nil {
			b.Skip("Kernel read requires CAP_SYS_TIME or root")
		}
	}
}

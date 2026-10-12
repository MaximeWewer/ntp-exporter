//go:build linux
// +build linux

package ntp

import (
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// These tests run ONLY on Linux where adjtimex syscall is available

func TestGetStatusString(t *testing.T) {
	tests := []struct {
		name     string
		state    int
		status   int32
		expected string
	}{
		// ntpd typically sets STA_PLL|STA_NANO: must not be read as a leap state
		{"ntpd synchronized", TIME_OK, STA_PLL | STA_NANO, "synchronized"},
		// chrony sets STA_NANO (and STA_MODE) without using the kernel PLL
		{"chrony synchronized", TIME_OK, STA_NANO, "synchronized"},
		{"chrony mode", TIME_OK, STA_NANO | STA_MODE, "synchronized"},
		{"unsync flag", TIME_ERROR, STA_UNSYNC, "unsynchronized"},
		{"unsync wins over state", TIME_OK, STA_PLL | STA_UNSYNC | STA_NANO, "unsynchronized"},
		{"clock_error", TIME_OK, STA_CLOCKERR, "clock_error"},
		{"leap_insert", TIME_INS, STA_PLL | STA_INS, "leap_insert_pending"},
		{"leap_delete", TIME_DEL, STA_PLL | STA_DEL, "leap_delete_pending"},
		{"leap_in_progress", TIME_OOP, STA_PLL, "leap_in_progress"},
		{"leap_occurred", TIME_WAIT, STA_PLL, "leap_occurred"},
		{"error state", TIME_ERROR, 0, "error"},
		{"unknown state", 99, 0, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getStatusString(tt.state, tt.status)
			if result != tt.expected {
				t.Errorf("getStatusString(%d, 0x%04x) = %q, want %q", tt.state, tt.status, result, tt.expected)
			}
		})
	}
}

func TestFromTimexOffsetUnits(t *testing.T) {
	micro := fromTimex(&unix.Timex{Offset: 1500, Status: STA_PLL}, TIME_OK)
	if micro.Offset != 1500*time.Microsecond {
		t.Errorf("microsecond offset = %v, want 1.5ms", micro.Offset)
	}

	nano := fromTimex(&unix.Timex{Offset: 1500, Status: STA_PLL | STA_NANO}, TIME_OK)
	if nano.Offset != 1500*time.Nanosecond {
		t.Errorf("nanosecond offset = %v, want 1.5us", nano.Offset)
	}

	errs := fromTimex(&unix.Timex{Maxerror: 16000, Esterror: 250, Status: STA_NANO, Tai: 37}, TIME_OK)
	if errs.MaxError != 16*time.Millisecond || errs.EstError != 250*time.Microsecond {
		t.Errorf("error bounds must stay in microseconds: max=%v est=%v", errs.MaxError, errs.EstError)
	}
	if errs.TAI != 37 || errs.State != TIME_OK {
		t.Errorf("TAI=%d State=%d, want 37 and TIME_OK", errs.TAI, errs.State)
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

	// Read-only adjtimex (modes=0) must work without any privilege
	if err != nil {
		t.Fatalf("Kernel timex read failed: %v", err)
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

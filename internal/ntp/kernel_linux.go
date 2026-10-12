package ntp

import (
	"fmt"
	"time"

	"github.com/maximewewer/ntp-exporter/pkg/logger"
	"golang.org/x/sys/unix"
)

// Linux kernel time status constants
const (
	TIME_OK       = 0 // Clock synchronized
	TIME_INS      = 1 // Insert leap second
	TIME_DEL      = 2 // Delete leap second
	TIME_OOP      = 3 // Leap second in progress
	TIME_WAIT     = 4 // Leap second has occurred
	TIME_ERROR    = 5 // Clock not synchronized
	STA_PLL       = 0x0001
	STA_PPSFREQ   = 0x0002
	STA_PPSTIME   = 0x0004
	STA_FLL       = 0x0008
	STA_INS       = 0x0010
	STA_DEL       = 0x0020
	STA_UNSYNC    = 0x0040
	STA_FREQHOLD  = 0x0080
	STA_PPSSIGNAL = 0x0100
	STA_PPSJITTER = 0x0200
	STA_PPSWANDER = 0x0400
	STA_PPSERROR  = 0x0800
	STA_CLOCKERR  = 0x1000
	STA_NANO      = 0x2000
	STA_MODE      = 0x4000
	STA_CLK       = 0x8000
)

// Read reads the current kernel NTP state.
// adjtimex with Modes=0 is read-only and requires no privilege nor capability.
func (k *KernelReader) Read() (*KernelTimex, error) {
	if !k.enabled {
		return nil, fmt.Errorf("kernel reader is disabled")
	}

	var tx unix.Timex
	state, err := unix.Adjtimex(&tx)
	if err != nil {
		logger.SafeWarn("ntp", "Failed to read kernel timex", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, fmt.Errorf("adjtimex syscall failed: %w", err)
	}

	result := fromTimex(&tx, state)

	logger.SafeDebug("ntp", "Kernel timex state read", map[string]interface{}{
		"offset_ns":   result.Offset.Nanoseconds(),
		"frequency":   result.Frequency,
		"status":      result.SyncStatus,
		"status_code": result.StatusCode,
		"state":       result.State,
	})

	return result, nil
}

// fromTimex converts the raw kernel structure. Field widths differ between
// 32 and 64-bit architectures, hence the explicit int64 conversions.
func fromTimex(tx *unix.Timex, state int) *KernelTimex {
	status := int32(tx.Status)

	// The offset unit depends on STA_NANO: nanoseconds when set, microseconds otherwise.
	// maxerror, esterror and precision are always expressed in microseconds.
	offsetUnit := time.Microsecond
	if status&STA_NANO != 0 {
		offsetUnit = time.Nanosecond
	}

	return &KernelTimex{
		Offset:     time.Duration(int64(tx.Offset)) * offsetUnit,
		Frequency:  int64(tx.Freq),
		MaxError:   time.Duration(int64(tx.Maxerror)) * time.Microsecond,
		EstError:   time.Duration(int64(tx.Esterror)) * time.Microsecond,
		Status:     status,
		Constant:   int64(tx.Constant),
		Precision:  time.Duration(int64(tx.Precision)) * time.Microsecond,
		Tolerance:  int64(tx.Tolerance),
		Tick:       int64(tx.Tick),
		PpsFreq:    int64(tx.Ppsfreq),
		PpsJitter:  int64(tx.Jitter),
		Shift:      int32(tx.Shift),
		Stabil:     int64(tx.Stabil),
		JitCnt:     int64(tx.Jitcnt),
		CalCnt:     int64(tx.Calcnt),
		ErrCnt:     int64(tx.Errcnt),
		StbCnt:     int64(tx.Stbcnt),
		TAI:        int64(tx.Tai),
		State:      state,
		SyncStatus: getStatusString(state, status),
		StatusCode: status,
	}
}

// IsSynchronized returns true if the kernel clock is synchronized
func (k *KernelTimex) IsSynchronized() bool {
	return (k.Status & STA_UNSYNC) == 0
}

// HasLeapSecond returns true if a leap second is pending
func (k *KernelTimex) HasLeapSecond() bool {
	return (k.Status&STA_INS) != 0 || (k.Status&STA_DEL) != 0
}

// IsPPSActive returns true if PPS signal is active
func (k *KernelTimex) IsPPSActive() bool {
	return (k.Status & STA_PPSSIGNAL) != 0
}

// getStatusString converts the clock state returned by adjtimex (TIME_*)
// and the status flags into a human-readable string
func getStatusString(state int, status int32) string {
	if (status & STA_UNSYNC) != 0 {
		return "unsynchronized"
	}

	if (status & STA_CLOCKERR) != 0 {
		return "clock_error"
	}

	return stateString(state)
}

// stateString returns the name of a TIME_* clock state
func stateString(state int) string {
	switch state {
	case TIME_OK:
		return "synchronized"
	case TIME_INS:
		return "leap_insert_pending"
	case TIME_DEL:
		return "leap_delete_pending"
	case TIME_OOP:
		return "leap_in_progress"
	case TIME_WAIT:
		return "leap_occurred"
	case TIME_ERROR:
		return "error"
	default:
		return "unknown"
	}
}

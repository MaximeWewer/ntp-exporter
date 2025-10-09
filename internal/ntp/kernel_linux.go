package ntp

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"github.com/maximewewer/ntp-exporter/pkg/logger"
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

// timex structure matching Linux kernel struct timex
type timex struct {
	Modes     uint32
	Offset    int64
	Freq      int64
	Maxerror  int64
	Esterror  int64
	Status    int32
	Constant  int64
	Precision int64
	Tolerance int64
	Time      syscall.Timeval
	Tick      int64
	Ppsfreq   int64
	Jitter    int64
	Shift     int32
	Stabil    int64
	Jitcnt    int64
	Calcnt    int64
	Errcnt    int64
	Stbcnt    int64
	Tai       int32
	_         [44]byte // Padding
}

// Read reads the current kernel NTP state
func (k *KernelReader) Read() (*KernelTimex, error) {
	if !k.enabled {
		return nil, fmt.Errorf("kernel reader is disabled")
	}

	var tx timex

	// Call adjtimex syscall (read-only mode with Modes=0)
	_, _, errno := syscall.Syscall(syscall.SYS_ADJTIMEX, uintptr(unsafe.Pointer(&tx)), 0, 0)
	if errno != 0 {
		logger.SafeWarn("ntp", "Failed to read kernel timex", map[string]interface{}{
			"error": errno.Error(),
		})
		return nil, fmt.Errorf("adjtimex syscall failed: %v", errno)
	}

	// Convert to our structure
	result := &KernelTimex{
		Offset:     time.Duration(tx.Offset) * time.Microsecond,
		Frequency:  tx.Freq,
		MaxError:   time.Duration(tx.Maxerror) * time.Microsecond,
		EstError:   time.Duration(tx.Esterror) * time.Microsecond,
		Status:     tx.Status,
		Constant:   tx.Constant,
		Precision:  time.Duration(tx.Precision) * time.Microsecond,
		Tolerance:  tx.Tolerance,
		Tick:       tx.Tick,
		PpsFreq:    tx.Ppsfreq,
		PpsJitter:  tx.Jitter,
		Shift:      tx.Shift,
		Stabil:     tx.Stabil,
		JitCnt:     tx.Jitcnt,
		CalCnt:     tx.Calcnt,
		ErrCnt:     tx.Errcnt,
		StbCnt:     tx.Stbcnt,
		SyncStatus: getStatusString(tx.Status),
		StatusCode: tx.Status,
	}

	logger.SafeDebug("ntp", "Kernel timex state read", map[string]interface{}{
		"offset_us":   result.Offset.Microseconds(),
		"frequency":   result.Frequency,
		"status":      result.SyncStatus,
		"status_code": result.StatusCode,
	})

	return result, nil
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

// getStatusString converts numeric status to human-readable string
func getStatusString(status int32) string {
	if (status & STA_UNSYNC) != 0 {
		return "unsynchronized"
	}

	if (status & STA_CLOCKERR) != 0 {
		return "clock_error"
	}

	switch status & 0x7 {
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


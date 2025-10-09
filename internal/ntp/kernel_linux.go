package ntp

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"github.com/maximewewer/ntp-exporter/pkg/logger"
)

// KernelTimex represents the kernel NTP state from adjtimex syscall
type KernelTimex struct {
	Offset     time.Duration // Time offset in microseconds
	Frequency  int64         // Frequency offset (scaled ppm)
	MaxError   time.Duration // Maximum error
	EstError   time.Duration // Estimated error
	Status     int32         // Clock status
	Constant   int64         // PLL time constant
	Precision  time.Duration // Clock precision
	Tolerance  int64         // Clock frequency tolerance
	Tick       int64         // Microseconds per tick
	PpsFreq    int64         // PPS frequency
	PpsJitter  int64         // PPS jitter
	Shift      int32         // PPS interval duration (seconds)
	Stabil     int64         // PPS stability
	JitCnt     int64         // PPS jitter limit exceeded count
	CalCnt     int64         // PPS calibration intervals
	ErrCnt     int64         // PPS calibration errors
	StbCnt     int64         // PPS stability limit exceeded count
	SyncStatus string        // Human-readable sync status
	StatusCode int32         // Numeric status code
}

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

// KernelReader reads kernel NTP state via adjtimex syscall
type KernelReader struct {
	enabled bool
}

// NewKernelReader creates a new kernel reader
func NewKernelReader(enabled bool) *KernelReader {
	return &KernelReader{
		enabled: enabled,
	}
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

// GetOffsetSeconds returns the offset in seconds (for metrics)
func (k *KernelTimex) GetOffsetSeconds() float64 {
	return k.Offset.Seconds()
}

// GetFrequencyPPM returns the frequency offset in PPM
func (k *KernelTimex) GetFrequencyPPM() float64 {
	// Kernel frequency is in scaled PPM (65536 = 1 ppm)
	return float64(k.Frequency) / 65536.0
}

// GetMaxErrorSeconds returns max error in seconds
func (k *KernelTimex) GetMaxErrorSeconds() float64 {
	return k.MaxError.Seconds()
}

// GetEstErrorSeconds returns estimated error in seconds
func (k *KernelTimex) GetEstErrorSeconds() float64 {
	return k.EstError.Seconds()
}

// GetPrecisionSeconds returns precision in seconds
func (k *KernelTimex) GetPrecisionSeconds() float64 {
	return k.Precision.Seconds()
}

package ntp

import (
	"fmt"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/logger"
	"golang.org/x/sys/unix"
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
//
//nolint:unconvert // conversions are required on 32-bit architectures
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

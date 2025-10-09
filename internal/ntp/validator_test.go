package ntp

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewValidator(t *testing.T) {
	v := NewValidator()

	assert.NotNil(t, v)
	assert.Equal(t, 1*time.Hour, v.maxClockSkew)
	assert.Equal(t, uint8(1), v.minStratum)
	assert.Equal(t, uint8(15), v.maxStratum)
	assert.True(t, v.validateRefID)
	assert.True(t, v.checkKissOfDeath)
}

func TestValidator_Validate_ValidResponse(t *testing.T) {
	v := NewValidator()

	resp := &Response{
		Server:         "pool.ntp.org",
		Offset:         10 * time.Millisecond,
		RTT:            50 * time.Millisecond,
		Stratum:        2,
		LeapIndicator:  0,
		ReferenceTime:  time.Now(),
		RootDelay:      10 * time.Millisecond,
		RootDispersion: 5 * time.Millisecond,
	}

	result := v.Validate(resp)

	assert.True(t, result.Valid)
	assert.Empty(t, result.Errors)
	assert.Equal(t, 1.0, result.TrustScore)
}

func TestValidator_Validate_LowStratum(t *testing.T) {
	v := NewValidator()

	resp := &Response{
		Stratum:        0,
		Offset:         10 * time.Millisecond,
		RTT:            50 * time.Millisecond,
		LeapIndicator:  0,
		ReferenceTime:  time.Now(),
		RootDelay:      10 * time.Millisecond,
		RootDispersion: 5 * time.Millisecond,
	}

	result := v.Validate(resp)

	assert.False(t, result.Valid)
	assert.NotEmpty(t, result.Errors)
	assert.Contains(t, result.Errors[0], "stratum")
	assert.Less(t, result.TrustScore, 1.0)
}

func TestValidator_Validate_HighStratum(t *testing.T) {
	v := NewValidator()

	resp := &Response{
		Stratum:        16,
		Offset:         10 * time.Millisecond,
		RTT:            50 * time.Millisecond,
		LeapIndicator:  0,
		ReferenceTime:  time.Now(),
		RootDelay:      10 * time.Millisecond,
		RootDispersion: 5 * time.Millisecond,
	}

	result := v.Validate(resp)

	assert.False(t, result.Valid)
	assert.NotEmpty(t, result.Errors)
	assert.Contains(t, result.Errors[0], "stratum")
	assert.Contains(t, result.Errors[0], "above maximum")
}

func TestValidator_Validate_KissOfDeath(t *testing.T) {
	v := NewValidator()

	resp := &Response{
		Stratum:        0,
		KissCode:       "RATE",
		Offset:         10 * time.Millisecond,
		RTT:            50 * time.Millisecond,
		LeapIndicator:  0,
		ReferenceTime:  time.Now(),
		RootDelay:      10 * time.Millisecond,
		RootDispersion: 5 * time.Millisecond,
	}

	result := v.Validate(resp)

	assert.False(t, result.Valid)
	assert.NotEmpty(t, result.Errors)
	// KoD responses have stratum 0, validator catches this
	hasKoDOrStratum := false
	for _, err := range result.Errors {
		if strings.Contains(err, "Kiss-of-Death") || strings.Contains(err, "stratum") {
			hasKoDOrStratum = true
			break
		}
	}
	assert.True(t, hasKoDOrStratum, "Should detect Kiss-of-Death or invalid stratum")
}

func TestValidator_Validate_LargeClockSkew(t *testing.T) {
	v := NewValidator()

	resp := &Response{
		Stratum:        2,
		Offset:         2 * time.Hour, // Exceeds maxClockSkew
		RTT:            50 * time.Millisecond,
		LeapIndicator:  0,
		ReferenceTime:  time.Now(),
		RootDelay:      10 * time.Millisecond,
		RootDispersion: 5 * time.Millisecond,
	}

	result := v.Validate(resp)

	assert.True(t, result.Valid) // Still valid, but warning
	assert.NotEmpty(t, result.Warnings)
	assert.Contains(t, result.Warnings[0], "large clock offset")
	assert.Less(t, result.TrustScore, 1.0)
}

func TestValidator_Validate_HighRTT(t *testing.T) {
	v := NewValidator()

	resp := &Response{
		Stratum:        2,
		Offset:         10 * time.Millisecond,
		RTT:            15 * time.Second, // > 10s
		LeapIndicator:  0,
		ReferenceTime:  time.Now(),
		RootDelay:      10 * time.Millisecond,
		RootDispersion: 5 * time.Millisecond,
	}

	result := v.Validate(resp)

	assert.True(t, result.Valid) // Still valid, but warning
	assert.NotEmpty(t, result.Warnings)
	assert.Contains(t, result.Warnings[0], "high RTT")
	assert.Less(t, result.TrustScore, 1.0)
}

func TestValidator_Validate_NegativeRTT(t *testing.T) {
	v := NewValidator()

	resp := &Response{
		Stratum:        2,
		Offset:         10 * time.Millisecond,
		RTT:            -50 * time.Millisecond,
		LeapIndicator:  0,
		ReferenceTime:  time.Now(),
		RootDelay:      10 * time.Millisecond,
		RootDispersion: 5 * time.Millisecond,
	}

	result := v.Validate(resp)

	assert.False(t, result.Valid)
	assert.NotEmpty(t, result.Errors)
	assert.Contains(t, result.Errors[0], "negative RTT")
}

func TestValidator_Validate_LeapIndicator(t *testing.T) {
	v := NewValidator()

	resp := &Response{
		Stratum:        2,
		Offset:         10 * time.Millisecond,
		RTT:            50 * time.Millisecond,
		LeapIndicator:  3, // Clock not synchronized
		ReferenceTime:  time.Now(),
		RootDelay:      10 * time.Millisecond,
		RootDispersion: 5 * time.Millisecond,
	}

	result := v.Validate(resp)

	assert.True(t, result.Valid) // Still valid, but warning
	assert.NotEmpty(t, result.Warnings)
	assert.Contains(t, result.Warnings[0], "clock not synchronized")
}

func TestValidator_Validate_NegativeRootDelay(t *testing.T) {
	v := NewValidator()

	resp := &Response{
		Stratum:        2,
		Offset:         10 * time.Millisecond,
		RTT:            50 * time.Millisecond,
		LeapIndicator:  0,
		ReferenceTime:  time.Now(),
		RootDelay:      -10 * time.Millisecond,
		RootDispersion: 5 * time.Millisecond,
	}

	result := v.Validate(resp)

	assert.False(t, result.Valid)
	assert.NotEmpty(t, result.Errors)
	assert.Contains(t, result.Errors[0], "negative root delay")
}

func TestValidator_Validate_NegativeRootDispersion(t *testing.T) {
	v := NewValidator()

	resp := &Response{
		Stratum:        2,
		Offset:         10 * time.Millisecond,
		RTT:            50 * time.Millisecond,
		LeapIndicator:  0,
		ReferenceTime:  time.Now(),
		RootDelay:      10 * time.Millisecond,
		RootDispersion: -5 * time.Millisecond,
	}

	result := v.Validate(resp)

	assert.False(t, result.Valid)
	assert.NotEmpty(t, result.Errors)
	assert.Contains(t, result.Errors[0], "negative root dispersion")
}

func TestValidator_Validate_ZeroReferenceTime(t *testing.T) {
	v := NewValidator()

	resp := &Response{
		Stratum:        2,
		Offset:         10 * time.Millisecond,
		RTT:            50 * time.Millisecond,
		LeapIndicator:  0,
		ReferenceTime:  time.Time{},
		RootDelay:      10 * time.Millisecond,
		RootDispersion: 5 * time.Millisecond,
	}

	result := v.Validate(resp)

	assert.True(t, result.Valid) // Still valid, but warning
	assert.NotEmpty(t, result.Warnings)
	assert.Contains(t, result.Warnings[0], "zero reference time")
}

func TestValidator_Validate_MultipleErrors(t *testing.T) {
	v := NewValidator()

	resp := &Response{
		Stratum:        16,
		Offset:         10 * time.Millisecond,
		RTT:            -50 * time.Millisecond,
		LeapIndicator:  0,
		ReferenceTime:  time.Now(),
		RootDelay:      -10 * time.Millisecond,
		RootDispersion: 5 * time.Millisecond,
	}

	result := v.Validate(resp)

	assert.False(t, result.Valid)
	assert.GreaterOrEqual(t, len(result.Errors), 2) // Multiple errors
	assert.LessOrEqual(t, result.TrustScore, 0.5)   // Significantly reduced
}

func TestValidator_Validate_TrustScoreClamp(t *testing.T) {
	v := NewValidator()

	resp := &Response{
		Stratum:        16,
		KissCode:       "RATE",
		Offset:         2 * time.Hour,
		RTT:            -50 * time.Millisecond,
		LeapIndicator:  3,
		ReferenceTime:  time.Time{},
		RootDelay:      -10 * time.Millisecond,
		RootDispersion: -5 * time.Millisecond,
	}

	result := v.Validate(resp)

	assert.False(t, result.Valid)
	assert.GreaterOrEqual(t, result.TrustScore, 0.0) // Clamped at 0
	assert.LessOrEqual(t, result.TrustScore, 1.0)    // Clamped at 1
}

func TestValidator_GetSuspicionReason(t *testing.T) {
	v := NewValidator()

	tests := []struct {
		name     string
		resp     *Response
		expected string
	}{
		{
			name:     "invalid_stratum_zero",
			resp:     &Response{Stratum: 0},
			expected: "invalid_stratum",
		},
		{
			name:     "stratum_too_high",
			resp:     &Response{Stratum: 16},
			expected: "stratum_too_high",
		},
		{
			name:     "kod_received",
			resp:     &Response{Stratum: 2, KissCode: "RATE"},
			expected: "kod_received",
		},
		{
			name:     "time_mismatch",
			resp:     &Response{Stratum: 2, Offset: 2 * time.Hour},
			expected: "time_mismatch",
		},
		{
			name:     "high_rtt",
			resp:     &Response{Stratum: 2, RTT: 15 * time.Second},
			expected: "high_rtt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := v.GetSuspicionReason(tt.resp)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidator_GetSuspicionReason_Unknown(t *testing.T) {
	v := NewValidator()

	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("pool.ntp.org", 10*time.Millisecond, 2)

	resp := &Response{
		Server:         "pool.ntp.org",
		Offset:         10 * time.Millisecond,
		RTT:            50 * time.Millisecond,
		Stratum:        2,
		LeapIndicator:  0,
		ReferenceTime:  time.Now(),
		RootDelay:      10 * time.Millisecond,
		RootDispersion: 5 * time.Millisecond,
	}

	reason := v.GetSuspicionReason(resp)
	assert.Equal(t, "unknown", reason)
}

func TestValidationResult_Structure(t *testing.T) {
	result := &ValidationResult{
		Valid:      true,
		Errors:     []string{},
		Warnings:   []string{"test warning"},
		TrustScore: 0.9,
	}

	assert.True(t, result.Valid)
	assert.Empty(t, result.Errors)
	assert.Len(t, result.Warnings, 1)
	assert.Equal(t, 0.9, result.TrustScore)
}

func BenchmarkValidator_Validate(b *testing.B) {
	v := NewValidator()

	resp := &Response{
		Server:         "pool.ntp.org",
		Offset:         10 * time.Millisecond,
		RTT:            50 * time.Millisecond,
		Stratum:        2,
		LeapIndicator:  0,
		ReferenceTime:  time.Now(),
		RootDelay:      10 * time.Millisecond,
		RootDispersion: 5 * time.Millisecond,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.Validate(resp)
	}
}

func BenchmarkValidator_GetSuspicionReason(b *testing.B) {
	v := NewValidator()

	resp := &Response{
		Stratum: 16,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.GetSuspicionReason(resp)
	}
}

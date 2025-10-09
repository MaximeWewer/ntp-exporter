package ntp

import (
	"errors"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/maximewewer/ntp-exporter/pkg/logger"
	"github.com/maximewewer/ntp-exporter/pkg/mathutil"
)

var (
	// Regular expressions for validation and sanitization
	ipv4Pattern      = regexp.MustCompile(`^(?:\d{1,3}\.){3}\d{1,3}$`)
	ipv6Pattern      = regexp.MustCompile(`^([0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}$|^::$|^::1$|^([0-9a-fA-F]{1,4}:){1,7}:$`)
	hostnamePattern  = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?)*$`)
	maliciousPattern = regexp.MustCompile(`[;&|<>$` + "`" + `\x00]`)
	emailPattern     = regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	ipAddressPattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	passwordPattern  = regexp.MustCompile(`(?i)(password|passwd|pwd|secret|token|api[_-]?key)=\S+`)
)

// Validator validates NTP responses for security and correctness
type Validator struct {
	maxClockSkew     time.Duration
	minStratum       uint8
	maxStratum       uint8
	validateRefID    bool
	checkKissOfDeath bool
}

// ValidationResult contains the result of validation
type ValidationResult struct {
	Valid      bool
	Errors     []string
	Warnings   []string
	TrustScore float64
}

// NewValidator creates a new NTP response validator
func NewValidator() *Validator {
	return &Validator{
		maxClockSkew:     1 * time.Hour,
		minStratum:       1,
		maxStratum:       15,
		validateRefID:    true,
		checkKissOfDeath: true,
	}
}

// Validate validates an NTP response
func (v *Validator) Validate(resp *Response) *ValidationResult {
	result := &ValidationResult{
		Valid:      true,
		Errors:     make([]string, 0),
		Warnings:   make([]string, 0),
		TrustScore: 1.0,
	}

	// Check stratum
	if resp.Stratum < v.minStratum {
		result.Errors = append(result.Errors, "stratum "+strconv.Itoa(int(resp.Stratum))+" below minimum "+strconv.Itoa(int(v.minStratum)))
		result.Valid = false
		result.TrustScore -= 0.3
	}

	if resp.Stratum > v.maxStratum {
		result.Errors = append(result.Errors, "stratum "+strconv.Itoa(int(resp.Stratum))+" above maximum "+strconv.Itoa(int(v.maxStratum)))
		result.Valid = false
		result.TrustScore -= 0.3
	}

	// Check for Kiss-of-Death
	if v.checkKissOfDeath && resp.IsKissOfDeath() {
		result.Errors = append(result.Errors, "Kiss-of-Death received: "+resp.KissCode)
		result.Valid = false
		result.TrustScore -= 0.5
	}

	// Check clock skew
	if mathutil.AbsDuration(resp.Offset) > v.maxClockSkew {
		result.Warnings = append(result.Warnings, "large clock offset: "+resp.Offset.String())
		result.TrustScore -= 0.2
	}

	// Check for unreasonable RTT
	if resp.RTT > 10*time.Second {
		result.Warnings = append(result.Warnings, "high RTT: "+resp.RTT.String())
		result.TrustScore -= 0.1
	}

	if resp.RTT < 0 {
		result.Errors = append(result.Errors, "negative RTT")
		result.Valid = false
		result.TrustScore -= 0.3
	}

	// Check leap indicator
	if resp.LeapIndicator == 3 {
		result.Warnings = append(result.Warnings, "clock not synchronized (leap indicator = 3)")
		result.TrustScore -= 0.2
	}

	// Check root delay and dispersion
	if resp.RootDelay < 0 {
		result.Errors = append(result.Errors, "negative root delay")
		result.Valid = false
		result.TrustScore -= 0.2
	}

	if resp.RootDispersion < 0 {
		result.Errors = append(result.Errors, "negative root dispersion")
		result.Valid = false
		result.TrustScore -= 0.2
	}

	// Check reference time
	if resp.ReferenceTime.IsZero() {
		result.Warnings = append(result.Warnings, "zero reference time")
		result.TrustScore -= 0.1
	}

	// Ensure trust score is in valid range [0, 1]
	if result.TrustScore < 0 {
		result.TrustScore = 0
	}
	if result.TrustScore > 1 {
		result.TrustScore = 1
	}

	return result
}

// GetSuspicionReason returns the reason why a response is suspicious
func (v *Validator) GetSuspicionReason(resp *Response) string {
	if resp.Stratum == 0 {
		return "invalid_stratum"
	}
	if resp.Stratum > v.maxStratum {
		return "stratum_too_high"
	}
	if resp.IsKissOfDeath() {
		return "kod_received"
	}
	if mathutil.AbsDuration(resp.Offset) > v.maxClockSkew {
		return "time_mismatch"
	}
	if resp.RTT > 10*time.Second {
		return "high_rtt"
	}
	if !resp.IsValid() {
		return "validation_failed"
	}

	return "unknown"
}

// ValidateServerAddress validates an NTP server address
func ValidateServerAddress(address string) error {
	// Check for empty address
	if address == "" {
		return errors.New("server address is empty")
	}

	// Check for maximum length
	if len(address) > 255 {
		return errors.New("server address is too long")
	}

	// Check for null bytes
	if strings.Contains(address, "\x00") {
		return errors.New("server address contains null byte")
	}

	// Check for malicious patterns
	if maliciousPattern.MatchString(address) {
		return errors.New("server address contains invalid characters")
	}

	// Parse address (may contain port)
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		// No port, use address as host
		host = address
	}

	// Check for localhost
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return errors.New("localhost not allowed")
	}

	// Parse IP address
	ip := net.ParseIP(host)
	if ip != nil {
		// Check for private IP ranges
		if ip.IsPrivate() {
			return errors.New("private IP addresses not allowed")
		}
		return nil
	}

	// Validate as hostname
	if !hostnamePattern.MatchString(host) {
		return errors.New("invalid server address format")
	}

	return nil
}

// ValidateNTPResponse validates an NTP response for security and correctness
func ValidateNTPResponse(resp *Response) error {
	if resp == nil {
		return errors.New("nil response")
	}

	// Check stratum
	if resp.Stratum == 0 && resp.KissCode == "" {
		return errors.New("invalid stratum 0 without Kiss-of-Death code")
	}

	if resp.Stratum == 16 {
		return errors.New("server is unsynchronized (stratum 16)")
	}

	// Check for excessive time drift (>10 seconds)
	if mathutil.AbsDuration(resp.Offset) > 10*time.Second {
		return errors.New("excessive time drift detected")
	}

	// Check for future reference time
	if resp.ReferenceTime.After(time.Now().Add(1 * time.Minute)) {
		return errors.New("future reference time detected")
	}

	// Check for negative root delay
	if resp.RootDelay < 0 {
		return errors.New("invalid negative root delay")
	}

	// Check for excessive root dispersion
	if resp.RootDispersion > 5*time.Second {
		return errors.New("excessive root dispersion")
	}

	return nil
}

// ValidateTimeout validates timeout duration
func ValidateTimeout(timeout time.Duration) error {
	if timeout <= 0 {
		return errors.New("timeout must be positive")
	}

	if timeout < 500*time.Millisecond {
		return errors.New("timeout too short (minimum 500ms)")
	}

	if timeout > 60*time.Second {
		return errors.New("timeout too long (maximum 60s)")
	}

	return nil
}

// ValidateStratum validates NTP stratum value
func ValidateStratum(stratum uint8, kissCode string) error {
	// Stratum 0 is valid with Kiss-of-Death
	if stratum == 0 && kissCode != "" {
		return nil
	}

	if stratum == 0 {
		return errors.New("invalid stratum 0 without Kiss-of-Death")
	}

	if stratum > 15 {
		return errors.New("invalid stratum (must be 1-15)")
	}

	return nil
}

// CalculateTrustScore calculates a trust score for an NTP response
func CalculateTrustScore(resp *Response) float64 {
	if resp == nil {
		return 0.0
	}

	score := 1.0

	// Penalize invalid stratum
	if resp.Stratum == 0 || resp.Stratum > 15 {
		return 0.0
	}

	// Penalize high stratum
	if resp.Stratum > 5 {
		score -= 0.2
	}

	// Penalize large offset
	offsetAbs := mathutil.AbsDuration(resp.Offset)
	if offsetAbs > 100*time.Millisecond {
		score -= 0.3
	} else if offsetAbs > 50*time.Millisecond {
		score -= 0.1
	}

	// Penalize Kiss-of-Death
	if resp.KissCode != "" {
		score -= 0.5
	}

	// Penalize validation errors
	if resp.ValidateError != nil {
		score -= 0.3
	}

	// Ensure score is in [0, 1]
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}

	return score
}

// IsSuspiciousResponse checks if a response has suspicious characteristics
func IsSuspiciousResponse(resp *Response) bool {
	if resp == nil {
		return true
	}

	// Check stratum
	if resp.Stratum == 0 && resp.KissCode == "" {
		return true
	}

	if resp.Stratum > 15 {
		return true
	}

	// Check for excessive offset
	if mathutil.AbsDuration(resp.Offset) > 10*time.Second {
		return true
	}

	// Check for Kiss-of-Death
	if resp.KissCode != "" {
		return true
	}

	// Check validation error
	if resp.ValidateError != nil {
		return true
	}

	return false
}

// DetectTimeAnomaly detects time anomalies in NTP responses
func DetectTimeAnomaly(resp *Response) (bool, string) {
	if resp == nil {
		return false, ""
	}

	offsetAbs := mathutil.AbsDuration(resp.Offset)

	if offsetAbs > 5*time.Second {
		return true, "critical"
	}

	if offsetAbs > 1*time.Second {
		return true, "major"
	}

	if offsetAbs > 500*time.Millisecond {
		return true, "moderate"
	}

	if offsetAbs > 100*time.Millisecond {
		return true, "minor"
	}

	return false, ""
}

// SanitizeLogOutput sanitizes log output to remove sensitive information
func SanitizeLogOutput(input string) string {
	// Redact IP addresses
	output := ipAddressPattern.ReplaceAllString(input, "[REDACTED_IP]")

	// Redact email addresses
	output = emailPattern.ReplaceAllString(output, "[REDACTED_EMAIL]")

	// Redact passwords and tokens
	output = passwordPattern.ReplaceAllString(output, "$1=[REDACTED]")

	return output
}

// LogSecurityEvent logs a security event with proper sanitization
func LogSecurityEvent(event, reason string, fields map[string]interface{}) {
	logger.Security(event, reason, fields)
}

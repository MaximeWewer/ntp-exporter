package collector

import (
	"context"
	"fmt"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/ntp"
	"github.com/maximewewer/ntp-exporter/pkg/logger"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
)

// SecurityCollector collects security-related NTP metrics
type SecurityCollector struct {
	*CommonCollector
	validator *ntp.Validator
}

// NewSecurityCollector creates a new security metrics collector
func NewSecurityCollector(cfg *config.Config, m *metrics.NTPMetrics) *SecurityCollector {
	return &SecurityCollector{
		CommonCollector: NewCommonCollector(cfg, m, "security"),
		validator:       ntp.NewValidator(),
	}
}

// Collect collects security metrics from all configured servers
func (c *SecurityCollector) Collect(ctx context.Context) error {
	return c.IterateServers(ctx, c.collectFromServer, "security")
}

// collectFromServer collects security metrics from a single server
func (c *SecurityCollector) collectFromServer(ctx context.Context, server string) error {
	client := c.GetClient()
	m := c.GetMetrics()

	// Query NTP server
	resp, err := client.Query(ctx, server)
	if err != nil {
		logger.Error("collector", "Query failed", err)
		return fmt.Errorf("failed to query NTP server %s for security metrics: %w", server, err)
	}

	// Validate response
	validation := c.validator.Validate(resp)

	// Update trust score
	m.ServerTrustScore.WithLabelValues(server).Set(validation.TrustScore)

	// Check for Kiss-of-Death
	if resp.IsKissOfDeath() {
		m.KissOfDeathTotal.WithLabelValues(server, resp.KissCode).Inc()
		logger.SafeWarn("collector", "Kiss-of-Death received", map[string]interface{}{
			"server":   server,
			"kod_code": resp.KissCode,
		})
	}

	// Check for suspicious behavior
	if resp.IsSuspicious() {
		reason := c.validator.GetSuspicionReason(resp)
		m.ServerSuspiciousTotal.WithLabelValues(server, reason).Inc()
		logger.SafeWarn("collector", "Suspicious NTP server detected", map[string]interface{}{
			"server": server,
			"reason": reason,
		})
	}

	// Check for malformed responses
	if !resp.IsValid() {
		m.MalformedResponsesTotal.WithLabelValues(server).Inc()
		logger.SafeWarn("collector", "Malformed NTP response", map[string]interface{}{
			"server": server,
			"error":  resp.ValidateError.Error(),
		})
	}

	logger.SafeDebug("collector", "Security metrics updated", map[string]interface{}{
		"server":      server,
		"trust_score": validation.TrustScore,
		"valid":       validation.Valid,
	})

	return nil
}

package collector

import (
	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/ntp"
	"github.com/maximewewer/ntp-exporter/internal/probe"
	"github.com/maximewewer/ntp-exporter/pkg/logger"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
)

// SecurityCollector publishes security-related NTP metrics
type SecurityCollector struct {
	*CommonCollector
	validator *ntp.Validator
}

// NewSecurityCollector creates a new security metrics publisher
func NewSecurityCollector(cfg *config.Config, m *metrics.NTPMetrics) *SecurityCollector {
	return &SecurityCollector{
		CommonCollector: NewCommonCollector(cfg, m, "security"),
		validator:       ntp.NewValidator(),
	}
}

// Publish updates security metrics from every response received during the cycle
func (c *SecurityCollector) Publish(result *probe.CycleResult) {
	m := c.GetMetrics()

	for _, meas := range result.Measurements {
		if !meas.Reachable() {
			continue
		}
		server := meas.Target.Server

		for _, resp := range meas.Responses {
			if resp.IsKissOfDeath() {
				m.KissOfDeathTotal.WithLabelValues(server, resp.KissCode).Inc()
				logger.SafeWarn("collector", "Kiss-of-Death received", map[string]interface{}{
					"server":   server,
					"kod_code": resp.KissCode,
				})
			}

			if resp.IsSuspicious() {
				m.ServerSuspiciousTotal.WithLabelValues(server, c.validator.GetSuspicionReason(resp)).Inc()
			}

			if !resp.IsValid() {
				m.MalformedResponsesTotal.WithLabelValues(server).Inc()
			}
		}

		// Trust score of the reference sample, or of the first response if none is valid
		ref := meas.Best()
		if ref == nil {
			ref = meas.Responses[0]
		}
		m.ServerTrustScore.WithLabelValues(server).Set(c.validator.Validate(ref).TrustScore)
	}
}

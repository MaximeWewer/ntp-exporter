package collector

import (
	"fmt"
	"testing"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/metrics"
	"github.com/maximewewer/ntp-exporter/internal/ntp"
	"github.com/maximewewer/ntp-exporter/internal/probe"
	"github.com/prometheus/client_golang/prometheus"
)

// BenchmarkCollect measures one scrape of 500 targets with 4 samples each
func BenchmarkCollect(b *testing.B) {
	cfg := config.DefaultConfig()
	now := time.Now()
	result := &probe.CycleResult{Start: now}
	for i := range 500 {
		server := fmt.Sprintf("time%d.example.org", i)
		cfg.NTP.Servers = append(cfg.NTP.Servers, server)
		m := &probe.Measurement{
			Target: probe.Target{Server: server, Address: fmt.Sprintf("192.0.2.%d", i%250+1)},
			Sent:   4, Timestamp: now, BackoffFactor: 1,
		}
		for s := range 4 {
			m.Responses = append(m.Responses, &ntp.Response{
				Server: server, Time: now, Offset: time.Duration(s+i) * time.Microsecond,
				RTT: time.Duration(10+s) * time.Millisecond, Stratum: 2, ReferenceTime: now.Add(-time.Minute),
				RootDelay: time.Millisecond, RootDispersion: time.Millisecond, RootDistance: 10 * time.Millisecond,
				Version: 4,
			})
		}
		result.Measurements = append(result.Measurements, m)
	}
	e := NewExporter(cfg, metrics.NewNTPMetrics())
	e.Update(result)

	reg := prometheus.NewRegistry()
	if err := reg.Register(e); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := reg.Gather(); err != nil {
			b.Fatal(err)
		}
	}
}

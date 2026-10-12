# Migration guide: v1 to v2

Version 2 reworks how the exporter measures and exposes NTP data. Most
changes are breaking: read this guide before upgrading, then update your
dashboards (new ones are provided in [`grafana/`](grafana/)) and alerts
(see [`deployments/prometheus/ntp-alerts.yml`](deployments/prometheus/ntp-alerts.yml)).

## What changes in behavior

| Topic | v1 | v2 |
|---|---|---|
| Queries per cycle | Each collector (base, quality, security, hybrid) queried every server on its own: up to 6 packets per server and per cycle | One burst of `samples_per_server` queries per server and per cycle, shared by every metric |
| Spacing between samples | 100 ms (triggers rate limiting on ntpd/chrony servers) | `sample_spacing`, 2 s by default |
| Concurrency | Servers queried one after the other | Up to `max_concurrency` servers in parallel; a cycle is cut 10% (at most 1 s) before the next one |
| Offset | First sample | Sample with the lowest round-trip delay (RFC 5905 clock filter) |
| Invalid responses | Kiss-o'-Death and unsynchronized responses were published as measurements | Never published as measurements; counted in `ntp_invalid_responses_total` |
| Kiss-o'-Death | Only counted | `RATE` doubles the polling interval of the server; `DENY`/`RSTR` suspend it (`ntp_target_suspended`) |
| Failed servers | Last values stayed exposed forever | Only `ntp_up=0`, `ntp_packet_loss_ratio` and `ntp_last_success_timestamp_seconds` remain |
| Pool members gone from DNS | Their series stayed forever | Their series disappear |
| Hostnames | Resolved for every packet (a burst to `pool.ntp.org` could hit several servers) | Resolved once per cycle; the burst targets a single address (IPv4 preferred) |
| Invalid configuration | Silently replaced by the defaults | Startup fails with an explicit error; unknown YAML keys are rejected |
| Default interval / samples | 30 s / 3 | 60 s / 4 |

## Deployment modes

There are now **two modes**: `probe` and `agent`. The `hybrid` mode is
replaced by the **kernel option of the agent mode**.

| v1 | v2 |
|---|---|
| Helm `mode: hybrid` | `mode: agent` + `kernel.enabled: true` (`mode: hybrid` still works, with a deprecation notice) |
| Helm `config.enableKernel: true` | `kernel.enabled: true` |
| YAML `ntp.enable_kernel: true` | `ntp.kernel.enabled: true` (old key accepted with a warning) |
| `NTP_ENABLE_KERNEL=true` | `NTP_KERNEL_ENABLED=true` (old variable accepted with a warning) |
| `values-hybrid.yaml`, `docker-compose-hybrid.yml` | `values-agent.yaml`, `docker-compose-agent.yml` with the kernel option |

The kernel state is read with a read-only `adjtimex` call, which needs **no
privilege**: remove `privileged: true`, `runAsUser: 0`, the `SYS_TIME`
capability and `AmbientCapabilities=CAP_SYS_TIME` from your deployments.
`hostNetwork` is optional (the pod shares the node clock).

## Metrics

### Renamed or replaced

| v1 | v2 | Notes |
|---|---|---|
| `ntp_server_reachable` | `ntp_up` | 1 when the server answered during the last cycle |
| `ntp_offset_seconds{server,stratum,version}` | `ntp_offset_seconds{server}` | `stratum` and `version` moved to `ntp_server_info`; the value comes from the lowest-delay sample |
| `ntp_reference_timestamp_seconds` | `ntp_reference_age_seconds` | `time() - reference_timestamp` becomes `ntp_reference_age_seconds` |
| `ntp_stability_seconds` | `ntp_offset_stddev_seconds` | Same computation |
| `ntp_jitter_seconds` (stddev of the RTT) | `ntp_rtt_stddev_seconds` | `ntp_jitter_seconds` now follows RFC 5905 (offset jitter) |
| `ntp_malformed_responses_total{server}` | `ntp_invalid_responses_total{server,reason}` | `sum without (reason) (...)` gives the old value, plus timeouts |
| `ntp_server_suspicious_total{server,reason}` | `ntp_invalid_responses_total{server,reason}` | Closed set of reasons: `kod`, `unsync`, `stratum`, `root_distance`, `stale_reference`, `invalid_time`, `other`, `timeout` |
| `ntp_pool_best_offset_seconds` | `ntp_pool_offset_median_seconds` | The smallest absolute offset was a biased statistic |
| `ntp_kernel_sync_status{node,status}` | `ntp_kernel_sync_status{node}` | 1 or 0, no more `status` label |
| `ntp_kernel_status_code` | `ntp_kernel_state{node,state}` | The state now comes from the adjtimex return value (TIME_OK, TIME_INS...) |

### Removed

| Metric | Replacement |
|---|---|
| `ntp_asymmetry_seconds` | None: it was `max(RTT) - min(RTT)`, not an asymmetry. Use `ntp_rtt_stddev_seconds` |
| `ntp_server_trust_score` | `ntp_response_valid`, `ntp_invalid_responses_total`, `ntp_falseticker` |
| `ntp_kernel_divergence_seconds`, `ntp_kernel_coherence_score` | `ntp_kernel_error_bound_violated{node,server,bound}` |
| `ntp_kernel_precision_seconds` | None (`node_timex_*` from node_exporter if needed) |
| `ntp_exporter_memory_usage_bytes`, `ntp_exporter_goroutines_count`, `ntp_exporter_memory_*_bytes`, `ntp_exporter_gc_*` | `go_*` and `process_*` metrics |
| `ntp_exporter_dns_resolution_duration_seconds`, `ntp_query_duration_seconds`, `ntp_exporter_collector_duration_seconds` | Were never populated. Use `ntp_exporter_cycle_duration_seconds` |

### New

| Metric | Description |
|---|---|
| `ntp_server_info{server,address,pool,refid,stratum,leap,version}` | Identity of the server (always 1) |
| `ntp_last_success_timestamp_seconds{server}` | Last cycle with a valid response |
| `ntp_response_valid{server}` | 1 when at least one valid response was received |
| `ntp_offset_median_seconds{server}` | Median offset of the burst |
| `ntp_rtt_stddev_seconds{server}` | Standard deviation of the round-trip delays |
| `ntp_target_suspended{server,code}` | Server no longer queried after `DENY`/`RSTR` |
| `ntp_consensus_offset_seconds`, `ntp_consensus_servers`, `ntp_consensus_truechimers` | Consensus of the servers (3+ servers) |
| `ntp_offset_from_consensus_seconds{server}`, `ntp_falseticker{server}` | Agreement of each server with the consensus |
| `ntp_kernel_state{node,state}`, `ntp_kernel_tai_offset_seconds{node}` | Kernel clock state |
| `ntp_kernel_error_bound_violated{node,server,bound}` | The measured offset exceeds the error bound announced by the kernel |
| `ntp_exporter_cycle_duration_seconds`, `ntp_exporter_cycle_overrun_total` | Collection cycle health |

### PromQL equivalents

```promql
# v1: ntp_server_reachable == 0
ntp_up == 0

# v1: time() - ntp_reference_timestamp_seconds
ntp_reference_age_seconds

# v1: increase(ntp_malformed_responses_total[1h])
sum without (reason) (increase(ntp_invalid_responses_total{reason!="timeout"}[1h]))

# v1: ntp_offset_seconds{stratum="1"}
ntp_offset_seconds * on (server) group_left (stratum) ntp_server_info{stratum="1"}

# v1: ntp_kernel_sync_status{status="synchronized"} == 1
ntp_kernel_sync_status == 1
```

## Configuration

### Removed options

These options had no effect or covered removed features. With strict YAML
parsing, **remove them from your configuration files**, or startup fails.

| YAML | Environment variable |
|---|---|
| `server.enable_cors`, `server.allowed_origins` | `ENABLE_CORS`, `ALLOWED_ORIGINS` |
| `ntp.rate_limit.*` | `RATE_LIMIT_*` |
| `ntp.circuit_breaker.*` | `CIRCUIT_BREAKER_*` |
| `ntp.worker_pool.*` | `WORKER_POOL_*` |
| `ntp.dns_cache.enabled`, `ntp.dns_cache.cleanup_workers` | `DNS_CACHE_ENABLED`, `DNS_CACHE_CLEANUP_WORKERS` |
| `ntp.adaptive_sampling.high_drift_samples` | `ADAPTIVE_SAMPLING_HIGH_DRIFT_SAMPLES` → `max_samples` / `ADAPTIVE_SAMPLING_MAX_SAMPLES` |
| `ntp.adaptive_sampling.drift_threshold` | `ADAPTIVE_SAMPLING_DRIFT_THRESHOLD` → `dispersion_threshold` / `ADAPTIVE_SAMPLING_DISPERSION_THRESHOLD` |

Adaptive sampling now takes more samples when the initial burst is **noisy**
(offset standard deviation above `dispersion_threshold`, or packet loss),
instead of when the offset is large: a large but stable offset is a real
offset.

### New options

| YAML | Environment variable | Default |
|---|---|---|
| `ntp.sample_spacing` | `NTP_SAMPLE_SPACING` | `2s` |
| `ntp.kernel.enabled`, `ntp.kernel.violation_only` | `NTP_KERNEL_ENABLED`, `NTP_KERNEL_VIOLATION_ONLY` | `false` |
| `ntp.kod.max_backoff`, `ntp.kod.suspend_duration` | `NTP_KOD_MAX_BACKOFF`, `NTP_KOD_SUSPEND_DURATION` | `1024s`, `1h` |
| `ntp.consensus.enabled` | `NTP_CONSENSUS_ENABLED` | enabled with 3+ servers or pools |
| `ntp.pools` | `NTP_POOLS` (`name[:strategy[:max_servers]]`) | none |
| `metrics.labels` (now applied) | `METRICS_LABELS` (`key=value,...`) | none |
| `server.web_config_file` | `WEB_CONFIG_FILE`, `--web.config.file` | none |
| | `--web.listen-address` | |

`tls_enabled`, `tls_cert_file` and `tls_key_file` still work but are
deprecated: use a [Prometheus web configuration file](https://prometheus.io/docs/prometheus/latest/configuration/https/)
(TLS, basic auth).

## HTTP endpoints

| v1 | v2 |
|---|---|
| `/health` (always healthy) | `/healthz` (liveness; `/health` kept as an alias) |
| | `/readyz`: 503 until a cycle completed, or when the last one is older than 3 intervals |

The Helm chart now uses `/healthz` for the liveness probe and `/readyz` for
the readiness probe. Container images check their health with
`/ntp-exporter --healthcheck` (distroless images have no shell nor `wget`).

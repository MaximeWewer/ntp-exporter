# NTP Prometheus Exporter

A Prometheus exporter that measures time synchronization: the health and
accuracy of NTP servers, and the clock accuracy of the machines it runs on.

> Upgrading from v1? Read the [migration guide](MIGRATION.md): metrics,
> configuration options and deployment modes changed.

## Table of contents

- [What it measures](#what-it-measures)
- [Deployment modes](#deployment-modes)
- [Quick start](#quick-start)
- [How a measurement works](#how-a-measurement-works)
- [Multi-target probing](#multi-target-probing)
- [Network Time Security](#network-time-security)
- [Configuration](#configuration)
- [Metrics reference](#metrics-reference)
- [Alerting](#alerting)
- [Grafana dashboards](#grafana-dashboards)
- [Kernel option](#kernel-option)
- [chrony option](#chrony-option)
- [Security](#security)
- [Troubleshooting](#troubleshooting)
- [Development](#development)

## What it measures

- **Offset** of the local clock against each server, from the sample with the
  lowest round-trip delay (RFC 5905 clock filter), plus the median offset.
- **Server health**: reachability, stratum, leap indicator, reference ID,
  root distance (upper bound of the server error), age of the server reference.
- **Measurement quality**: RFC 5905 jitter, delay variability, packet loss.
- **Consensus** of the servers (Marzullo intersection): which servers disagree
  with the others (*falsetickers*), independently of the local clock.
- **Network Time Security** (RFC 8915): `nts://` servers are queried with
  authenticated packets; the key exchange and its TLS certificate are monitored.
- **Kiss-o'-Death** handling: `RATE` slows the polling of a server down,
  `DENY`/`RSTR` suspend it.
- **Kernel clock state** (agent mode, Linux, optional): synchronization status,
  error bounds announced by the time daemon, and whether the offset measured
  from the outside violates those bounds.

## Deployment modes

Two questions, two modes:

| Question | Mode | Deployment | Key metrics |
|---|---|---|---|
| Are my time servers reliable? | **probe** | Deployment, Docker, systemd on a monitoring host | `ntp_up`, `ntp_response_valid`, `ntp_falseticker`, `ntp_offset_from_consensus_seconds`, `ntp_root_distance_seconds`, `ntp_target_suspended` |
| Is the clock of each machine right? | **agent** | DaemonSet, one container or service per host | `ntp_consensus_offset_seconds`, `ntp_offset_seconds`, and with the kernel option `ntp_kernel_sync_status`, `ntp_kernel_error_bound_violated` |

The mode is only a deployment concern: the binary is the same. In agent mode,
the pod shares the node clock, so the measured offset is the node offset.
Neither mode needs privileges or host networking.

In probe mode, `ntp_offset_seconds` mixes the error of the probe host clock and
the error of the server: use `ntp_offset_from_consensus_seconds` and
`ntp_falseticker` to judge the servers themselves (3 servers or more).

## Quick start

### Binary

```bash
curl -LO https://github.com/MaximeWewer/ntp-exporter/releases/latest/download/ntp-exporter-linux-amd64
chmod +x ntp-exporter-linux-amd64
sudo mv ntp-exporter-linux-amd64 /usr/local/bin/ntp-exporter

NTP_SERVERS=time.cloudflare.com,time.google.com,ptbtime1.ptb.de ntp-exporter
curl -s localhost:9559/metrics | grep '^ntp_'
```

### Docker

```bash
docker run -d --name ntp-exporter -p 9559:9559 \
  -e NTP_SERVERS=time.cloudflare.com,time.google.com,ptbtime1.ptb.de \
  ghcr.io/maximewewer/ntp-exporter:latest
```

Ready-made compose files are in [`deployments/docker/`](deployments/docker/):
`docker-compose-probe.yml`, `docker-compose-agent.yml` (set
`NTP_KERNEL_ENABLED=true` for the kernel option) and
`docker-compose-with-yaml.yml`.

### Kubernetes (Helm)

```bash
# Agent mode: one pod per node, with the kernel option
helm install ntp-exporter oci://ghcr.io/maximewewer/charts/ntp-exporter \
  --namespace monitoring --create-namespace \
  -f deployments/kubernetes/helm/ntp-exporter/values-agent.yaml \
  --set kernel.enabled=true \
  --set serviceMonitor.enabled=true --set prometheusRule.enabled=true

# Probe mode: central monitoring of NTP servers
helm install ntp-probe oci://ghcr.io/maximewewer/charts/ntp-exporter \
  --namespace monitoring \
  -f deployments/kubernetes/helm/ntp-exporter/values-probe.yaml
```

Every option of the chart is documented in
[`values-with-all-options.yaml`](deployments/kubernetes/helm/ntp-exporter/values-with-all-options.yaml).

### systemd

```ini
# /etc/systemd/system/ntp-exporter.service
[Unit]
Description=NTP Prometheus Exporter
After=network-online.target
Wants=network-online.target

[Service]
User=ntp-exporter
ExecStart=/usr/local/bin/ntp-exporter --config /etc/ntp-exporter/config.yaml
Restart=on-failure
# No capability is needed, even for the kernel option
CapabilityBoundingSet=
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

## How a measurement works

Every `scrape_interval` (60 s by default), the exporter runs one **cycle**:

1. **Targets**: configured servers are resolved once per cycle, so that every
   sample of a burst reaches the same server even behind a round-robin name
   such as `pool.ntp.org`. Pools are resolved through a DNS cache and their
   members become targets. With `ip_family: auto`, address families without a
   route on the host are skipped, and a dual-stack server is queried over the
   family that answered last: when it does not answer, the other family is
   tried in the same cycle.
2. **Burst**: each target receives `samples_per_server` queries spaced by
   `sample_spacing` (2 s: NTP servers rate-limit clients sending packets closer
   than that). Up to `max_concurrency` targets are probed in parallel.
3. **Filtering**: responses that are not valid time measurements (Kiss-o'-Death,
   leap indicator 3, invalid stratum, failed RFC 5905 checks) are counted in
   `ntp_invalid_responses_total` and never published as measurements. The
   offset comes from the valid sample with the lowest delay.
4. **Kiss-o'-Death**: `RATE` doubles the polling interval of the target (up to
   `kod.max_backoff`), `DENY` and `RSTR` stop queries for `kod.suspend_duration`.
5. **Consensus**: with 3 servers or more, correctness intervals
   `[offset - root distance, offset + root distance]` are intersected (Marzullo);
   servers outside the majority intersection are falsetickers.
6. **Snapshot**: metrics are built from the last cycle at scrape time. A server
   that stops answering exposes `ntp_up 0` and no stale offset; a pool member
   gone from DNS disappears.

A cycle is cut 10% (at most 1 s) before the next one
(`ntp_exporter_cycle_overrun_total`).

## Network Time Security

Servers written `nts://host[:port]` (port 4460 by default) are queried with
NTS (RFC 8915): a key exchange over TLS 1.3 (NTS-KE) gives keys and cookies,
then every NTP query is authenticated. The NTP server may differ from the key
exchange host: `ntp_server_info{address}` shows the one negotiated.

```yaml
ntp:
  servers:
    - nts://time.cloudflare.com
    - nts://nts.netnod.se
    - nts://ptbtime1.ptb.de
```

The session is kept across cycles; the key exchange runs again every hour,
after an authentication failure or a `NTSN` Kiss-o'-Death, so that the key
exchange server and its certificate are monitored too
(`ntp_nts_ke_success`, `ntp_nts_cert_expiry_timestamp_seconds`). When the key
exchange fails, no NTP query is sent and `ntp_up` is 0. `/probe` accepts NTS
targets as well (`/probe?target=nts://time.cloudflare.com`).

Outgoing TCP 4460 must be open towards the key exchange servers (the Helm
`values-probe.yaml` network policy opens it).

## Multi-target probing

Like blackbox_exporter, `/probe?target=host[:port]` measures one target on
demand, so that Prometheus drives the targets through relabeling and probe
replicas hold no target list:

```yaml
scrape_configs:
  - job_name: ntp-probe
    metrics_path: /probe
    params:
      samples: ["4"]          # optional, defaults to samples_per_server
    scrape_interval: 60s      # keep 60s or more for public servers
    scrape_timeout: 10s
    static_configs:
      - targets: [time.cloudflare.com, ptbtime1.ptb.de, 192.0.2.10:123]
    relabel_configs:
      - source_labels: [__address__]
        target_label: __param_target
      - source_labels: [__param_target]
        target_label: instance
      - target_label: __address__
        replacement: ntp-exporter:9559
```

The response holds the per-server metrics of the target (`ntp_up`,
`ntp_offset_seconds`, `ntp_server_info`...) plus `ntp_probe_success` and
`ntp_probe_duration_seconds`. The burst is shortened to fit in the scrape
timeout announced by Prometheus (minus 0.5 s): `(samples − 1) × sample_spacing`
plus one query timeout. No Kiss-o'-Death state is kept between requests: the
scrape interval sets the polling rate.

With the Prometheus Operator, the Helm chart creates the `Probe` resource
(`probeEndpoint.probe.enabled`, `probeEndpoint.probe.targets`). Set
`probe_endpoint.only: true` (`PROBE_ENDPOINT_ONLY=true`, Helm
`probeEndpoint.only`) to stop probing static servers in the background.
Consensus and pool metrics need the static targets: they are not available
through `/probe`.

## Configuration

The exporter reads, by increasing priority: defaults, a YAML file
(`--config`), environment variables, then command-line flags. Any invalid
value or unknown YAML key stops the startup with an explicit error.

A fully commented file is available in
[`deployments/docker/config-example.yaml`](deployments/docker/config-example.yaml).
Minimal example:

```yaml
ntp:
  servers: [time.cloudflare.com, time.google.com, ptbtime1.ptb.de]
  pools:
    - name: europe.pool.ntp.org
      strategy: best_n   # best_n, all or round_robin
      max_servers: 4
  scrape_interval: 60s
  samples_per_server: 4
  kernel:
    enabled: false
```

### Flags

| Flag | Description |
|---|---|
| `--config` | YAML configuration file |
| `--web.config.file` | [Prometheus web configuration](https://prometheus.io/docs/prometheus/latest/configuration/https/) (TLS, basic auth) |
| `--web.listen-address` | Listen address, e.g. `:9559` (overrides `server.address` and `server.port`) |
| `--healthcheck` | Query the local `/healthz` endpoint and exit 0/1 (container health checks) |
| `--version` | Print version, commit and build date |

### Environment variables

| Variable | Default | Description |
|---|---|---|
| `PROBE_ENDPOINT_ENABLED` | `true` | Serve `/probe?target=` |
| `PROBE_ENDPOINT_ONLY` | `false` | Only measure `/probe` targets (no static servers) |
| `NTP_SERVERS` | `pool.ntp.org,time.google.com` (when no pool) | Comma-separated servers (`host`, `host:port` or `nts://host[:port]`) |
| `NTP_POOLS` | | Comma-separated pools, `name[:strategy[:max_servers]]` |
| `NTP_SCRAPE_INTERVAL` | `60s` | Interval between two cycles (5 s to 1 h) |
| `NTP_SAMPLES` | `4` | Queries per server and per cycle (1 to 20) |
| `NTP_SAMPLE_SPACING` | `2s` | Delay between two queries to the same server |
| `NTP_TIMEOUT` | `5s` | Timeout of one query |
| `NTP_VERSION` | `4` | NTP protocol version (2 to 4) |
| `NTP_MAX_CONCURRENCY` | `10` | Servers probed in parallel |
| `NTP_IP_FAMILY` | `auto` | `auto`, `ipv4` or `ipv6` |
| `NTP_MAX_CLOCK_OFFSET` | `100ms` | Threshold of `ntp_clock_offset_exceeded` |
| `NTP_CONSENSUS_ENABLED` | enabled with 3+ servers or pools | Consensus and falseticker detection |
| `NTP_KOD_MAX_BACKOFF` | `1024s` | Longest polling interval after repeated `RATE` |
| `NTP_KOD_SUSPEND_DURATION` | `1h` | Suspension after `DENY` or `RSTR` |
| `NTP_KERNEL_ENABLED` | `false` | Kernel option (agent mode, Linux) |
| `NTP_KERNEL_VIOLATION_ONLY` | `false` | Only expose the bound violation and sync status |
| `NTP_CHRONY_ENABLED` | `false` | chrony option (agent mode) |
| `NTP_CHRONY_ADDRESS` | `udp://127.0.0.1:323` | chronyd command address, or `unix:///run/chrony/chronyd.sock` |
| `DNS_CACHE_MIN_TTL` / `DNS_CACHE_MAX_TTL` | `5m` / `60m` | Cache of pool DNS answers |
| `NTP_EXPORTER_ADDRESS` / `NTP_EXPORTER_PORT` | `0.0.0.0` / `9559` | Listen address |
| `SERVER_READ_TIMEOUT` / `SERVER_WRITE_TIMEOUT` | `10s` | HTTP timeouts |
| `WEB_CONFIG_FILE` | | Prometheus web configuration file |
| `LOG_LEVEL` | `info` | `trace`, `debug`, `info`, `warn`, `error` |
| `LOG_FORMAT` | `json` | `json` or `console` |
| `LOG_ENABLE_FILE` / `LOG_FILE_PATH` | `false` | Also log to a file |
| `METRICS_NAMESPACE` / `METRICS_SUBSYSTEM` | `ntp` / | Metric name prefix |
| `METRICS_LABELS` | | Constant labels, `key=value,...` |

Deprecated but still accepted: `NTP_ENABLE_KERNEL`, `TLS_ENABLED`,
`TLS_CERT_FILE`, `TLS_KEY_FILE` (a warning is logged at startup).

When querying the **public NTP pool**, keep `scrape_interval` at 60 s or more:
the exporter logs a warning otherwise.

## Metrics reference

Names below use the default `ntp` namespace and no subsystem.

### Per server (`server` label)

| Metric | Description |
|---|---|
| `ntp_up` | 1 when the server answered during the last cycle |
| `ntp_response_valid` | 1 when at least one response was a valid time measurement |
| `ntp_last_success_timestamp_seconds` | Unix time of the last cycle with a valid response |
| `ntp_server_info{address,pool,refid,stratum,leap,version}` | Identity of the server (value 1). `refid` is an ASCII code (`GPS`, `PPS`...) for stratum 1, the upstream IPv4 address otherwise |
| `ntp_offset_seconds` | Offset of the local clock, from the sample with the lowest delay |
| `ntp_offset_median_seconds` | Median offset of the valid samples |
| `ntp_clock_offset_exceeded` | 1 when `abs(offset)` exceeds `max_clock_offset` |
| `ntp_rtt_seconds` | Round-trip delay of the selected sample |
| `ntp_stratum`, `ntp_leap_indicator`, `ntp_precision_seconds` | Fields of the server response |
| `ntp_root_delay_seconds`, `ntp_root_dispersion_seconds` | Delay and dispersion to the stratum 1 source |
| `ntp_root_distance_seconds` | Synchronization distance: `(root delay + rtt) / 2 + root dispersion`, an upper bound of the error of the server time |
| `ntp_reference_age_seconds` | Time since the server clock was last set or corrected |
| `ntp_jitter_seconds` | RFC 5905 jitter: `sqrt(Σ(θ₀ − θᵢ)² / (n − 1))`, θ₀ being the offset of the lowest-delay sample |
| `ntp_offset_stddev_seconds`, `ntp_rtt_stddev_seconds` | Standard deviations of the offsets and delays of the burst |
| `ntp_samples_count` | Valid samples in the last burst |
| `ntp_packet_loss_ratio` | Queries without valid response / queries sent |
| `ntp_target_suspended{code}` | 1 while the server is not queried after `DENY` or `RSTR` |
| `ntp_kiss_of_death_total{code}` | Kiss-o'-Death packets received |
| `ntp_invalid_responses_total{reason}` | Queries without valid measurement: `kod`, `unsync`, `stratum`, `root_distance`, `stale_reference`, `invalid_time`, `other`, `timeout` |
| `ntp_nts_ke_success` | NTS servers: 1 when the last key exchange succeeded |
| `ntp_nts_cert_expiry_timestamp_seconds` | NTS servers: expiry of the key exchange server certificate |

### Consensus (3 valid servers or more)

| Metric | Description |
|---|---|
| `ntp_consensus_offset_seconds` | Offset of the local clock against the consensus (median of the truechimers) |
| `ntp_consensus_servers` / `ntp_consensus_truechimers` | Servers taking part / agreeing with the consensus |
| `ntp_offset_from_consensus_seconds{server}` | Offset of a server against the consensus, independent of the local clock |
| `ntp_falseticker{server}` | 1 when the server interval misses the majority intersection |

### Pools (`pool` label)

| Metric | Description |
|---|---|
| `ntp_pool_servers_total` | Addresses returned by the last resolution |
| `ntp_pool_servers_active` | Members with a valid response |
| `ntp_pool_offset_median_seconds` | Median offset of the valid members |
| `ntp_pool_dns_resolution_seconds` | Duration of the last real (non cached) resolution |

### Kernel option (`node` label)

| Metric | Description |
|---|---|
| `ntp_kernel_sync_status` | 1 when the kernel clock is synchronized (`STA_UNSYNC` not set) |
| `ntp_kernel_error_bound_violated{server,bound}` | 1 when `abs(offset) − root distance` exceeds the `maxerror` or `esterror` bound announced by the kernel |
| `ntp_kernel_state{state}` | Clock state returned by `adjtimex` (`synchronized`, `leap_insert_pending`...) |
| `ntp_kernel_max_error_seconds`, `ntp_kernel_est_error_seconds` | Error bounds maintained by the time daemon |
| `ntp_kernel_frequency_ppm`, `ntp_kernel_offset_seconds`, `ntp_kernel_tai_offset_seconds` | Frequency correction, residual PLL offset, TAI−UTC |

### chrony option (`node` label)

| Metric | Description |
|---|---|
| `ntp_chrony_up` | 1 when the tracking report could be read |
| `ntp_chrony_tracking_info{refid,source}` | Source selected by chronyd (address, or reference clock name) |
| `ntp_chrony_stratum`, `ntp_chrony_leap_status` | Stratum of the local clock, leap status (3 = not synchronised) |
| `ntp_chrony_system_time_offset_seconds` | Offset still being slewed out of the system clock (positive: clock slow) |
| `ntp_chrony_last_offset_seconds`, `ntp_chrony_rms_offset_seconds` | Offset at the last update, long-term average |
| `ntp_chrony_frequency_ppm`, `ntp_chrony_residual_frequency_ppm`, `ntp_chrony_skew_ppm` | Frequency error (negative: slow), residual frequency, frequency error bound |
| `ntp_chrony_root_delay_seconds`, `ntp_chrony_root_dispersion_seconds` | Distance to the stratum 1 source |
| `ntp_chrony_reference_timestamp_seconds`, `ntp_chrony_update_interval_seconds` | Last clock update, interval between updates |

### Exporter

| Metric | Description |
|---|---|
| `ntp_exporter_build_info{version,commit,go_version}` | Build information |
| `ntp_exporter_cycle_duration_seconds`, `ntp_exporter_scrape_duration_seconds` | Duration of the last cycle / histogram of cycle durations |
| `ntp_exporter_cycle_overrun_total` | Cycles cut at their deadline |
| `ntp_exporter_scrapes_total{status}` | Cycles with at least one answering server (`success`) or none (`failure`) |
| `ntp_exporter_servers_configured` | Servers and pools configured |

Go runtime and process metrics (`go_*`, `process_*`) are also exposed.

### Endpoints

| Path | Description |
|---|---|
| `/metrics` | Prometheus metrics |
| `/healthz` | Liveness (`/health` is kept as an alias) |
| `/readyz` | 503 until a cycle completed, or when the last cycle is older than 3 intervals |
| `/probe?target=host[:port][&samples=N]` | Measures one target on demand (see [Multi-target probing](#multi-target-probing)) |

## Alerting

Recommended rules are provided:

- plain Prometheus: [`deployments/prometheus/ntp-alerts.yml`](deployments/prometheus/ntp-alerts.yml) (`rule_files`)
- Prometheus Operator: `prometheusRule.enabled=true` in the Helm chart

| Alert | Fires when |
|---|---|
| `NTPServerDown` | A server does not answer for 5 minutes |
| `NTPServerNoValidResponse` | A server answers but only with invalid responses for 10 minutes |
| `NTPServerUnsynchronized` | A server reports leap indicator 3 |
| `NTPOffsetTooHigh` | `abs(ntp_offset_seconds)` exceeds 100 ms for 5 minutes |
| `NTPHighPacketLoss` | More than half of the queries fail over 15 minutes |
| `NTPKissOfDeathSuspended` | A server sent `DENY` or `RSTR` |
| `NTPFalseticker` | A server disagrees with the consensus for 10 minutes |
| `NTPNTSKeyExchangeFailed` | The NTS key exchange of a server fails for 15 minutes |
| `NTPNTSCertificateExpiringSoon` | The NTS key exchange certificate expires within 14 days |
| `NTPKernelUnsynchronized` | The kernel clock of a node is unsynchronized for 10 minutes |
| `NTPKernelErrorBoundViolated` | The measured offset exceeds the kernel `maxerror` for 10 minutes |
| `NTPExporterCycleOverrun` | Cycles hit their deadline |

## Grafana dashboards

Import the dashboards from [`grafana/`](grafana/):

- `ntp-probe-mode.json`: server health, consensus and falsetickers, quality
- `ntp-agent-mode.json`: clock offset per node, kernel state and error bounds,
  server health

Both dashboards use a Prometheus data source variable and `server` (and
`node`) filters.

## Kernel option

With `kernel.enabled`, the exporter also reads the kernel clock state with a
read-only `adjtimex` call (Linux only). **No privilege is required**: any
process may call `adjtimex` without modifying the clock, and the real-time
clock is not namespaced, so a regular container sees the node clock.

The key signal is `ntp_kernel_error_bound_violated`: the time daemon (chrony,
ntpd, systemd-timesyncd) announces a maximum error (`maxerror`) and an
estimated error (`esterror`) through the kernel. When the offset measured from
the outside, minus the measurement uncertainty (root distance), exceeds that
bound, the daemon underestimates its error: it may be locked on a wrong source.
Alert on `maxerror` (guaranteed bound, few false positives) and keep `esterror`
for dashboards (earlier detection, noisier).

The other kernel metrics are equivalent to the `timex` collector of
node_exporter (`node_timex_*`). If node_exporter already runs on your nodes,
set `kernel.violation_only` to only expose `ntp_kernel_error_bound_violated`
and `ntp_kernel_sync_status`.

Notes per daemon:

- **chrony** does not use the kernel PLL (see also the [chrony option](#chrony-option)): `ntp_kernel_offset_seconds` stays
  close to 0, which is expected. Old versions (< 1.31) do not maintain
  `STA_UNSYNC` reliably.
- **ntpd** and **systemd-timesyncd** maintain the kernel state.
- **OpenNTPD** < 5.9p1 does not update the kernel flags.

## chrony option

With `chrony.enabled` (agent mode), the exporter reads the `tracking` report
of the local chronyd once per cycle, like `chronyc tracking`: the source the
daemon selected, its stratum, the offset it still has to correct, the last
offset, the frequency and its skew. Comparing them with `ntp_offset_seconds`
correlates the measurement from the outside with the decision of the daemon.

chronyd answers read-only commands from localhost on UDP port 323 by default
(`udp://127.0.0.1:323`, the default address): the exporter must share the
network namespace of the host (`hostNetwork: true`, Docker `--network host`,
systemd). Without host networking, use the unix socket
(`unix:///run/chrony/chronyd.sock`, Helm `chrony.socketDir: /run/chrony`): the
exporter must belong to the group owning the socket (usually `chrony` or
`_chrony`, Helm `podSecurityContext.supplementalGroups`).

For the full chronyd state (every source, server statistics), run
[chrony_exporter](https://github.com/SuperQ/chrony_exporter) next to it.

## Security

- Containers run as non-root on a distroless image, with all capabilities
  dropped, a read-only root filesystem and no host network, in both modes.
- TLS and basic authentication use the standard
  [Prometheus web configuration file](https://prometheus.io/docs/prometheus/latest/configuration/https/)
  (`--web.config.file`, Helm `webConfig.existingSecret`).
- The exporter only sends NTP client requests (UDP 123) and honors
  Kiss-o'-Death codes.
- `govulncheck` runs in CI on every change. It may report
  [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932) at module level:
  `golang.org/x/crypto` is required (bcrypt, for basic auth) but its
  deprecated `openpgp` package is not imported nor compiled in the binary.
- `/probe` sends NTP requests to any host given by the caller, at most
  `max_concurrency` bursts at a time. Restrict who can reach it (network
  policy, basic auth) or disable it with `PROBE_ENDPOINT_ENABLED=false`.

## Troubleshooting

**The exporter does not start.** The error names the faulty key, variable or
flag. Unknown YAML keys are rejected: compare with
[`config-example.yaml`](deployments/docker/config-example.yaml) and the
[migration guide](MIGRATION.md).

**`ntp_up` is 1 but no offset is exposed.** The server answers with invalid
responses: check `ntp_invalid_responses_total` by `reason` (for instance
`kod`, or `unsync` for a server that lost its source).

**`ntp_packet_loss_ratio` is high on a public server.** Check that
`sample_spacing` is at least 2 s and that `ntp_kiss_of_death_total` stays at 0.

**`ntp_exporter_cycle_overrun_total` increases.** Unreachable servers consume
`samples_per_server × timeout`. Lower the timeout or the samples, raise
`max_concurrency` or `scrape_interval`.

**A dual-stack server is reached over the wrong family.** In `auto` mode the
exporter uses the family that answered last; force one with
`NTP_IP_FAMILY=ipv4` or `ipv6`. `ntp_server_info{address}` shows the address
used.

**No kernel metrics.** The kernel option is Linux only and must be enabled
(`NTP_KERNEL_ENABLED=true`, Helm `kernel.enabled=true`); check the logs for
`adjtimex` errors.

**`/readyz` returns 503.** No cycle completed yet (the first one takes
`(samples − 1) × sample_spacing` plus the network delays), or the collection
loop is stuck.

## Development

```bash
go test -race ./...                 # unit tests, no network access needed
golangci-lint run ./...             # lint (.golangci.yml, golangci-lint v2)
helm lint deployments/kubernetes/helm/ntp-exporter
docker build -t ntp-exporter:dev .
```

Package layout:

| Package | Role |
|---|---|
| `cmd/ntp-exporter` | Entry point, flags, collection loop |
| `internal/config` | Loading (YAML, environment), defaults, validation |
| `internal/probe` | Cycle: target resolution, parallel bursts, Kiss-o'-Death backoff, consensus |
| `internal/ntp` | NTP client, response validation, statistics, pools and DNS cache, kernel reader |
| `internal/collector` | `prometheus.Collector` exposing the snapshot of the last cycle |
| `internal/server` | HTTP server, health endpoints, web configuration |
| `pkg/metrics`, `pkg/logger` | Exporter counters and registry, logging |

Contributions are welcome: open an issue or a pull request with tests.

Built with [beevik/ntp](https://github.com/beevik/ntp),
[client_golang](https://github.com/prometheus/client_golang) and
[exporter-toolkit](https://github.com/prometheus/exporter-toolkit).

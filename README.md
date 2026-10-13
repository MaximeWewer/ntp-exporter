# NTP Prometheus Exporter

A Prometheus exporter that measures time synchronization: the health and
accuracy of NTP servers, and the clock accuracy of the machines it runs on.

## Table of contents

- [What it measures](#what-it-measures)
- [Deployment modes](#deployment-modes)
- [Quick start](#quick-start)
- [How a measurement works](#how-a-measurement-works)
- [Network Time Security](#network-time-security)
- [Multi-target probing](#multi-target-probing)
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
- **chronyd tracking report** (agent mode, optional): source selected by the
  local chronyd, offset it is correcting, frequency and skew, to compare with
  the offset measured from the outside.
- **On-demand targets**: `/probe?target=` measures one server per scrape, the
  targets being driven by Prometheus relabeling (like blackbox_exporter).

## Deployment modes

Two questions, two modes:

| Question | Mode | Deployment | Key metrics |
|---|---|---|---|
| Are my time servers reliable? | **probe** | Deployment, Docker, systemd on a monitoring host | `ntp_up`, `ntp_response_valid`, `ntp_falseticker`, `ntp_offset_from_consensus_seconds`, `ntp_root_distance_seconds`, `ntp_target_suspended`, `ntp_nts_ke_success`, `ntp_probe_success` |
| Is the clock of each machine right? | **agent** | DaemonSet, one container or service per host | `ntp_kernel_sync_status`, `ntp_kernel_error_bound_violated`, `ntp_chrony_*`, `ntp_offset_seconds` against 1 or 2 reference servers, all with a `node` label |

The mode is required: `mode: probe` or `mode: agent` in the YAML file, or
`NTP_EXPORTER_MODE`. It applies its guards wherever the exporter runs: the
kernel and chrony options describe the host clock and are refused in probe
mode. Neither mode needs privileges or host networking (except the chrony
option over UDP, see [chrony option](#chrony-option)).

The agent measures the clock of its host, not the servers:

- the kernel option (Linux) and the chrony option are enabled by default:
  the kernel state is always read, chronyd when it answers;
- at most 2 reference servers, preferably inside the site, and no pool: on
  a fleet every node queries them, and a public server must not receive a
  burst from each node. Without server, the agent reads the kernel and
  chronyd only;
- no consensus nor server health metrics: monitor the servers once, with a
  probe;
- every series carries a `node` label (`NODE_NAME`, or the hostname).

The pod shares the node clock, so the offset it measures is the node offset;
through the pod network it is only accurate to about half the round-trip
delay, while chronyd knows the node offset much better.

In probe mode, `ntp_offset_seconds` mixes the error of the probe host clock and
the error of the server: use `ntp_offset_from_consensus_seconds` and
`ntp_falseticker` to judge the servers themselves (3 servers or more).

## Quick start

### Binary

```bash
curl -LO https://github.com/MaximeWewer/ntp-exporter/releases/latest/download/ntp-exporter-linux-amd64
chmod +x ntp-exporter-linux-amd64
sudo mv ntp-exporter-linux-amd64 /usr/local/bin/ntp-exporter

NTP_EXPORTER_MODE=probe NTP_EXPORTER_SERVERS=ptbtime1.ptb.de,ptbtime2.ptb.de,ptbtime3.ptb.de ntp-exporter
curl -s localhost:9559/metrics | grep '^ntp_'
```

### Docker

```bash
docker run -d --name ntp-exporter -p 9559:9559 \
  -e NTP_EXPORTER_MODE=probe \
  -e NTP_EXPORTER_SERVERS=ptbtime1.ptb.de,ptbtime2.ptb.de,ptbtime3.ptb.de \
  ghcr.io/maximewewer/ntp-exporter:latest
```

Ready-made compose files are in [`deployments/docker/`](deployments/docker/):
`docker-compose-probe.yml`, `docker-compose-agent.yml` (host network, kernel
option enabled) and `docker-compose-with-yaml.yml` (mounts
`config-example.yaml`). Override the defaults in a `.env` file, see
`.env.example`.

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

Every `collection_interval` (60 s by default), the exporter runs one **cycle**:

1. **Targets**: configured servers are resolved once per cycle, so that every
   sample of a burst reaches the same server even behind a round-robin name
   such as `pool.ntp.org`. Pools are resolved through a DNS cache and their
   members become targets. With `ip_family: auto`, address families without a
   route on the host are skipped, and a dual-stack server is queried over the
   family that answered last: when it does not answer, the other family is
   tried in the same cycle. A pool member already listed as a server, or
   returned by another pool, is probed once, under its first name.
2. **Burst**: each target receives `samples_per_server` queries spaced by
   `sample_spacing` (2 s: NTP servers rate-limit clients sending packets closer
   than that). Up to `max_concurrency` targets are probed in parallel.
3. **Filtering**: responses that are not valid time measurements (Kiss-o'-Death,
   leap indicator 3, invalid stratum, failed RFC 5905 checks, root distance
   above `max_root_distance`) are counted in `ntp_invalid_responses_total` and
   never published as measurements nor used by the consensus. The offset comes
   from the valid sample with the lowest delay.
4. **Kiss-o'-Death**: the burst stops at the first Kiss-o'-Death (RFC 5905
   §7.4). `RATE` doubles the polling interval of the target (up to
   `kod.max_backoff`), `DENY` and `RSTR` stop queries for `kod.suspend_duration`.
5. **Consensus**: with 3 servers or more, the RFC 5905 selection algorithm
   intersects the correctness intervals
   `[offset - root distance, offset + root distance]`: it looks for the
   smallest number `f` of falsetickers (`f < n/2`) such that `n - f` intervals
   overlap and at most `f` offsets lie outside that intersection. Servers whose
   offset lies outside it are falsetickers; the result does not depend on the
   order of the servers. The consensus offset combines the offsets of the
   truechimers weighted by 1/root distance (RFC 5905 §11.2.3): an imprecise
   server barely moves it.
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
    - nts://nts.netnod.se
    - nts://ptbtime1.ptb.de
    - nts://ptbtime2.ptb.de
```

The session is kept across cycles; the key exchange runs again every hour,
after an authentication failure (including a `NTSN` Kiss-o'-Death), so that the key
exchange server and its certificate are monitored too
(`ntp_nts_ke_success`, `ntp_nts_cert_expiry_timestamp_seconds`). When the key
exchange fails, no NTP query is sent and `ntp_up` is 0. `/probe` accepts NTS
targets as well (`/probe?target=nts://time.cloudflare.com`). Other
Kiss-o'-Death codes are not authenticated in NTS and are ignored (RFC 8915
§5.7): NTS targets get no `RATE` backoff.

Outgoing TCP 4460 must be open towards the key exchange servers (the Helm
network policy opens it).

## Multi-target probing

Like blackbox_exporter, `/probe?target=host[:port]` measures one target on
demand, so that Prometheus drives the targets through relabeling and probe
replicas hold no target list. The endpoint is disabled by default: enable it
with `probe_endpoint.enabled: true` (`NTP_EXPORTER_PROBE_ENDPOINT_ENABLED=true`, Helm
`probeEndpoint.enabled`) and restrict its targets with
`probe_endpoint.allowed_targets` (see [Security](#security)).

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
timeout announced by Prometheus, capped by `server.write_timeout` (minus 0.5 s):
`(samples − 1) × sample_spacing`
plus one query timeout. Kiss-o'-Death codes are honored across requests: while
a target is suspended (`DENY`, `RSTR`) or backing off (`RATE`), `/probe` sends
nothing and answers `ntp_probe_success 0` (with `ntp_target_suspended` for a
suspension).

With the Prometheus Operator, the Helm chart creates the `Probe` resource
(`probeEndpoint.probe.enabled`, `probeEndpoint.probe.targets`). Without
`ntp.servers` nor `ntp.pools`, the exporter only serves `/probe` and probes
nothing in the background. Consensus and pool metrics need the static
targets: they are not available through `/probe`.

## Configuration

The exporter reads, by increasing priority: defaults, a YAML file
(`--config`), environment variables, then command-line flags. Any invalid
value or unknown YAML key stops the startup with an explicit error.

A fully commented file is available in
[`deployments/docker/config-example.yaml`](deployments/docker/config-example.yaml).
Minimal example:

```yaml
mode: probe
ntp:
  servers: [ptbtime1.ptb.de, ptbtime2.ptb.de, ptbtime3.ptb.de]
  pools:
    - name: europe.pool.ntp.org
      max_servers: 4
  collection_interval: 60s
  samples_per_server: 4
  ip_family: auto      # auto, ipv4 or ipv6
  kernel:
    enabled: false
  chrony:
    enabled: false
    address: udp://127.0.0.1:323
probe_endpoint:
  enabled: true        # serve /probe?target=
  only: false          # true: no static servers nor pools
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

Every option of the YAML file has an environment variable, prefixed with
`NTP_EXPORTER_`. The names used before this prefix (`NTP_SERVERS`,
`LOG_LEVEL`, `SERVER_READ_TIMEOUT`...) are still accepted for one release and
logged as deprecated.

| Variable | Default | Description |
|---|---|---|
| `NTP_EXPORTER_MODE` | (required) | `probe` or `agent` |
| `NTP_EXPORTER_PROBE_ENDPOINT_ENABLED` | `false` | Serve `/probe?target=` |
| `NTP_EXPORTER_PROBE_ENDPOINT_ALLOWED_TARGETS` | | Comma-separated CIDR ranges and host name regular expressions `/probe` may reach |
| `NTP_EXPORTER_SERVERS` | (none) | Comma-separated servers (`host`, `host:port` or `nts://host[:port]`); nothing is queried unless configured |
| `NTP_EXPORTER_POOLS` | | Comma-separated pools, `name[:max_servers]`: the first `max_servers` (4) addresses of each DNS answer are queried |
| `NTP_EXPORTER_COLLECTION_INTERVAL` | `60s` | Interval between two cycles (5 s to 1 h) |
| `NTP_EXPORTER_SAMPLES_PER_SERVER` | `4` | Queries per server and per cycle (1 to 20) |
| `NTP_EXPORTER_SAMPLE_SPACING` | `2s` | Delay between two queries to the same server |
| `NTP_EXPORTER_TIMEOUT` | `5s` | Timeout of one query |
| `NTP_EXPORTER_NTP_VERSION` | `4` | NTP protocol version (2 to 4) |
| `NTP_EXPORTER_MAX_CONCURRENCY` | `10` | Servers probed in parallel |
| `NTP_EXPORTER_IP_FAMILY` | `auto` | `auto`, `ipv4` or `ipv6` |
| `NTP_EXPORTER_MAX_ROOT_DISTANCE` | `1.5s` | Responses with a larger root distance are invalid (`reason="root_distance"`), like ntpd's MAXDIST (chrony uses 3 s); 100 ms to 16 s |
| `NTP_EXPORTER_CONSENSUS_ENABLED` | `true` in probe mode | Consensus and falseticker detection, computed as soon as 3 targets (pool members included) answer |
| `NTP_EXPORTER_KOD_MAX_BACKOFF` | `1024s` | Longest polling interval after repeated `RATE` |
| `NTP_EXPORTER_KOD_SUSPEND_DURATION` | `1h` | Suspension after `DENY` or `RSTR` |
| `NTP_EXPORTER_KERNEL_ENABLED` | agent on Linux | Kernel option (agent mode only) |
| `NTP_EXPORTER_CHRONY_ENABLED` | agent when chronyd answers | chrony option (agent mode only) |
| `NTP_EXPORTER_CHRONY_ADDRESS` | `udp://127.0.0.1:323` | chronyd command address, or `unix:///run/chrony/chronyd.sock` |
| `NTP_EXPORTER_DNS_CACHE_MIN_TTL` / `NTP_EXPORTER_DNS_CACHE_MAX_TTL` | `5m` / `60m` | Cache of pool DNS answers |
| `NTP_EXPORTER_ADDRESS` / `NTP_EXPORTER_PORT` | `0.0.0.0` / `9559` | Listen address |
| `NTP_EXPORTER_READ_TIMEOUT` / `NTP_EXPORTER_WRITE_TIMEOUT` | `10s` | HTTP timeouts |
| `NTP_EXPORTER_WEB_CONFIG_FILE` | | Prometheus web configuration file |
| `NTP_EXPORTER_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `NTP_EXPORTER_LOG_FORMAT` | `json` | `json` or `console`, on the standard output |
| `NTP_EXPORTER_METRICS_LABELS` | | Constant labels, `key=value,...` |
| `NODE_NAME` | hostname | `node` label of every series in agent mode (set by the Helm DaemonSet) |

When querying the **public NTP pool**, keep `collection_interval` at 60 s or more:
the exporter logs a warning otherwise.

The exporter only queries the servers it is given. Do not mix servers that
smear leap seconds (`time.google.com`, `time.cloudflare.com`, `time.apple.com`
and other cloud providers) with servers that step them (PTB, NIST, Netnod, the
public pool): around a leap second they differ by up to 0.5 s, and the
consensus would flag one group as falsetickers.

## Metrics reference

Every metric is prefixed with `ntp_`.

### Per server (`server` label)

| Metric | Description |
|---|---|
| `ntp_up` | 1 when the server answered during the last cycle |
| `ntp_response_valid` | 1 when at least one response was a valid time measurement |
| `ntp_last_success_timestamp_seconds` | Unix time of the last cycle with a valid response |
| `ntp_server_info{address,pool,refid,version}` | Identity of the server (value 1): queried address, pool, reference ID (an ASCII code such as `GPS` or `PPS` for stratum 1, the upstream IPv4 address otherwise), NTP version of the response |
| `ntp_offset_seconds` | Offset of the local clock, from the sample with the lowest delay: positive when the local clock is behind the server |
| `ntp_rtt_seconds` | Round-trip delay of the selected sample |
| `ntp_stratum`, `ntp_leap_indicator`, `ntp_precision_seconds` | Fields of the server response |
| `ntp_root_delay_seconds`, `ntp_root_dispersion_seconds` | Delay and dispersion to the stratum 1 source |
| `ntp_root_distance_seconds` | Synchronization distance: `(root delay + rtt) / 2 + root dispersion`, an upper bound of the error of the server time |
| `ntp_reference_age_seconds` | Time since the server clock was last set or corrected |
| `ntp_jitter_seconds` | RFC 5905 jitter: `sqrt(Σ(θ₀ − θᵢ)² / (n − 1))`, θ₀ being the offset of the lowest-delay sample |
| `ntp_samples_count` | Valid samples in the last burst (jitter and standard deviations need 2 or more) |
| `ntp_packet_loss_ratio` | Queries left without any answer / queries sent (invalid answers are counted in `ntp_invalid_responses_total`) |
| `ntp_target_suspended{code}` | 1 while the server is not queried after `DENY` or `RSTR` |
| `ntp_kiss_of_death_total{code}` | Kiss-o'-Death packets received |
| `ntp_invalid_responses_total{reason}` | Queries without valid measurement: `kod`, `unsync`, `stratum`, `root_distance`, `stale_reference`, `invalid_time` (bad transmit time, clock ticking backwards, origin timestamp mismatch), `other`, `timeout` (no answer) |
| `ntp_nts_ke_success` | NTS servers: 1 when the last key exchange succeeded |
| `ntp_nts_cert_expiry_timestamp_seconds` | NTS servers: expiry of the key exchange server certificate |

### Consensus (3 valid servers or more)

| Metric | Description |
|---|---|
| `ntp_consensus_offset_seconds` | Offset of the local clock against the consensus (truechimers weighted by 1/root distance) |
| `ntp_consensus_majority` | 1 when more than half of the servers agree. Without majority, `ntp_consensus_offset_seconds`, `ntp_offset_from_consensus_seconds` and `ntp_falseticker` are not exposed |
| `ntp_consensus_servers` / `ntp_consensus_truechimers` | Servers taking part / agreeing with the consensus (without majority: the largest group of servers agreeing with each other) |
| `ntp_offset_from_consensus_seconds{server}` | Offset of a server against the consensus, independent of the local clock |
| `ntp_falseticker{server}` | 1 when the server interval misses the majority intersection |

### Pools (`pool` label)

| Metric | Description |
|---|---|
| `ntp_pool_servers_total` | Addresses returned by the last resolution |
| `ntp_pool_servers_active` | Members with a valid response |

### Kernel option (`node` label)

| Metric | Description |
|---|---|
| `ntp_kernel_sync_status` | 1 when the kernel clock is synchronized (`STA_UNSYNC` not set) |
| `ntp_kernel_error_bound_violated` | 1 when `abs(offset) − root distance` against the reference server (lowest root distance) exceeds the maximum error announced by the kernel |

### chrony option (`node` label)

| Metric | Description |
|---|---|
| `ntp_chrony_up` | 1 when the tracking report could be read |
| `ntp_chrony_tracking_info{refid,source}` | Source selected by chronyd (address, or reference clock name) |
| `ntp_chrony_stratum`, `ntp_chrony_leap_status` | Stratum of the local clock, leap status (3 = not synchronised) |
| `ntp_chrony_system_time_offset_seconds` | Offset still being slewed out of the system clock (positive: clock slow) |
| `ntp_chrony_root_dispersion_seconds` | Dispersion accumulated since the stratum 1 source |
| `ntp_chrony_reference_timestamp_seconds` | Last clock update |

### Exporter

| Metric | Description |
|---|---|
| `ntp_exporter_build_info{version,commit,go_version}` | Build information |
| `ntp_exporter_cycle_duration_seconds` | Histogram of the collection cycle durations |
| `ntp_exporter_cycle_overrun_total` | Cycles cut at their deadline |
| `ntp_exporter_cycles_total{status}` | Cycles with at least one answering server (`success`) or none (`failure`) |
| `ntp_exporter_servers_configured` | Servers and pools configured |

Go runtime and process metrics (`go_*`, `process_*`) are also exposed.

### Endpoints

| Path | Description |
|---|---|
| `/metrics` | Prometheus metrics |
| `/healthz` | Liveness |
| `/readyz` | 503 until a cycle completed, or when the last cycle is older than 3 intervals |
| `/probe?target=[nts://]host[:port][&samples=N]` | Measures one target on demand (see [Multi-target probing](#multi-target-probing)) |

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
| `NTPKernelErrorBoundViolated` | The offset against the reference server exceeds the kernel maximum error for 10 minutes |
| `NTPExporterCycleOverrun` | Cycles hit their deadline |

## Grafana dashboards

Import the dashboards from [`grafana/`](grafana/):

- `ntp-probe-mode.json`: server health, consensus and falsetickers, quality,
  NTS key exchange and certificate expiry, `/probe` targets
- `ntp-agent-mode.json`: clock offset per node, kernel state and error bounds,
  chronyd tracking report against the measured offset, server health, NTS

Both dashboards use a Prometheus data source variable and `server` (and
`node`) filters.

## Kernel option

In agent mode on Linux (unless `kernel.enabled: false`), the exporter reads
the kernel clock state with a read-only `adjtimex` call. **No privilege is required**: any
process may call `adjtimex` without modifying the clock, and the real-time
clock is not namespaced, so a regular container sees the node clock.

The key signal is `ntp_kernel_error_bound_violated`: the time daemon (chrony,
ntpd, systemd-timesyncd) announces a maximum error (`maxerror`) through the
kernel, a guaranteed bound. The offset is measured against the reference
server with the lowest root distance; when it exceeds that bound beyond the
measurement uncertainty (the root distance of the server), the daemon
underestimates its error: it may be locked on a wrong source. Without any
answering reference server, the violation is not exposed.

Only `ntp_kernel_sync_status` and the violation are exposed: the raw kernel
values (frequency, error bounds, TAI offset) are the `timex` collector of
node_exporter (`node_timex_*`).

Notes per daemon:

- **chrony** does not use the kernel PLL (see also the [chrony option](#chrony-option)).
  Old versions (< 1.31) do not maintain `STA_UNSYNC` reliably.
- **ntpd** and **systemd-timesyncd** maintain the kernel state.
- **OpenNTPD** < 5.9p1 does not update the kernel flags.

## chrony option

In agent mode, when chronyd answers at startup (or with `chrony.enabled:
true`), the exporter reads the `tracking` report of the local chronyd once per
cycle, like `chronyc tracking`: the source the daemon selected, its stratum
and leap status, the offset it still has to correct, its root dispersion and
the time of its last clock update. Comparing them with `ntp_offset_seconds`
correlates the measurement from the outside with the decision of the daemon.

chronyd answers read-only commands from localhost on UDP port 323 by default
(`udp://127.0.0.1:323`, the default address): the exporter must share the
network namespace of the host (`hostNetwork: true`, Docker `--network host`,
systemd). Without host networking, use the unix socket
(`unix:///run/chrony/chronyd.sock`, Helm `chrony.socketDir: /run/chrony`).
Like `chronyc`, the exporter creates its reply socket next to the chronyd
socket: chronyd resolves that path in its own mount namespace and cannot see
the `/tmp` of the exporter (systemd `PrivateTmp`, a container). The directory
is usually writable by chronyd only (`/run/chrony` is `0700 _chrony` on
Debian and Ubuntu): run the exporter as the chronyd user (systemd
`User=_chrony`, Helm `chrony.runAsUser` set to `id -u _chrony` of the nodes).

For the full chronyd state (every source, server statistics), run
[chrony_exporter](https://github.com/SuperQ/chrony_exporter) next to it.

## Security

- Containers run as non-root on a distroless image, with all capabilities
  dropped, a read-only root filesystem and no host network, in both modes
  (the chrony option over UDP is the only case needing host networking).
- TLS and basic authentication use the standard
  [Prometheus web configuration file](https://prometheus.io/docs/prometheus/latest/configuration/https/)
  (`--web.config.file`, Helm `webConfig.existingSecret`).
- Outgoing traffic: NTP client requests (UDP 123), NTS key exchanges
  (TLS 1.3, TCP 4460) for `nts://` servers, and read-only `tracking` requests
  to the local chronyd with the chrony option. Kiss-o'-Death codes are honored.
- `govulncheck` runs in CI on every change. It may report
  [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932) at module level:
  `golang.org/x/crypto` is required (bcrypt, for basic auth) but its
  deprecated `openpgp` package is not imported nor compiled in the binary.
- `/probe` is disabled by default: it sends NTP requests (and NTS key
  exchanges) to hosts chosen by the caller, at most `max_concurrency` bursts at
  a time. When enabled, it refuses loopback, link-local (including the cloud
  metadata address 169.254.169.254), multicast and unspecified addresses,
  checked on the resolved address and on the NTP server negotiated by NTS.
  Restrict it further with `probe_endpoint.allowed_targets` (CIDR ranges and
  name expressions) and restrict who can reach it (network policy, basic
  auth): a refused target answers 403.

## Troubleshooting

**The exporter does not start.** The error names the faulty key, variable or
flag. A server or pool listed twice is rejected (host names are case
insensitive, `:123` and NTS `:4460` are implied). Unknown YAML keys are rejected: compare with
[`config-example.yaml`](deployments/docker/config-example.yaml).

**`ntp_up` is 1 but no offset is exposed.** The server answers with invalid
responses: check `ntp_invalid_responses_total` by `reason` (for instance
`kod`, or `unsync` for a server that lost its source).

**`ntp_packet_loss_ratio` is high on a public server.** Check that
`sample_spacing` is at least 2 s and that `ntp_kiss_of_death_total` stays at 0.

**`ntp_exporter_cycle_overrun_total` increases.** Unreachable servers consume
`samples_per_server × timeout`. The exporter refuses to start when such a
burst cannot fit in a cycle (`collection_interval` minus 10%, at most 1 s). Lower the timeout or the samples, raise
`max_concurrency` or `collection_interval`.

**A dual-stack server is reached over the wrong family.** In `auto` mode the
exporter uses the family that answered last; force one with
`NTP_EXPORTER_IP_FAMILY=ipv4` or `ipv6`. `ntp_server_info{address}` shows the address
used.

**`ntp_nts_ke_success` is 0.** The key exchange failed: check that outgoing
TCP 4460 is open and that the server supports NTS (not every NTP server does,
for instance `time.google.com`). The `NTS key exchange failed` log gives the
TLS or protocol error.

**`ntp_chrony_up` is 0.** Over UDP, chronyd only answers from localhost: the
exporter needs host networking, and chronyd must allow commands (`cmdport` not
0). Over the unix socket, the exporter must be able to create its reply socket
in the chronyd socket directory: the log names the directory.

**No kernel metrics.** The kernel option is Linux only and must be enabled
(`NTP_EXPORTER_KERNEL_ENABLED=true`, Helm `kernel.enabled=true`); check the logs for
`adjtimex` errors.

**`/readyz` returns 503.** No cycle completed yet (the first one takes
`(samples − 1) × sample_spacing` plus the network delays), or the collection
loop is stuck.

## Development

```bash
go test -race ./...                 # unit tests, no network access needed
golangci-lint run ./...             # lint (.golangci.yml, golangci-lint v2)
govulncheck ./...                   # known vulnerabilities
.github/scripts/check-deployments.sh   # Helm + kubeconform, promtool, compose
docker build -t ntp-exporter:dev .
act -W .github/workflows/quality-assurance.yml   # run the CI locally (nektos/act)
```

Under act, steps uploading artifacts and SARIF reports are skipped. The
alerting rules in `deployments/prometheus/ntp-alerts.yml` are generated from
the Helm template: after changing it, run
`.github/scripts/check-deployments.sh --update-rules`. A Go test checks that
the dashboards and the rules only query metrics the exporter exposes.

Package layout:

| Package | Role |
|---|---|
| `cmd/ntp-exporter` | Entry point, flags, collection loop |
| `internal/config` | Loading (YAML, environment), defaults, validation |
| `internal/probe` | Cycle: target resolution, parallel bursts, Kiss-o'-Death backoff, consensus |
| `internal/ntp` | NTP and NTS clients, response validation, statistics, pools and DNS cache, kernel reader, chronyd client |
| `internal/collector` | `prometheus.Collector` exposing the snapshot of the last cycle, `/probe` handler |
| `internal/server` | HTTP server, health endpoints, web configuration |
| `internal/metrics`, `internal/logger` | Exporter counters and registry, logging |

Releases follow `YEAR.MONTH_INDEX.RELEASE_INDEX` (for instance `2026.6.3`,
tag `v2026.6.3`, no leading zero): push a `v*.*.*` tag, or run the *Build and
Release* workflow by hand, which takes the next index of the current month
(or `force_version`). A suffix such as `-rc.1` marks a pre-release.

Contributions are welcome: open an issue or a pull request with tests.

Built with [beevik/ntp](https://github.com/beevik/ntp),
[beevik/nts](https://github.com/beevik/nts),
[client_golang](https://github.com/prometheus/client_golang) and
[exporter-toolkit](https://github.com/prometheus/exporter-toolkit).

# NTP Prometheus Exporter

A production-grade Prometheus exporter for monitoring Network Time Protocol (NTP) servers with advanced metrics, flexible deployment modes, and comprehensive observability.

---

## Table of contents

- [Overview](#overview)
- [Features](#features)
- [Quick start](#quick-start)
  - [Binary](#binary)
  - [Docker compose](#docker-compose)
  - [Kubernetes with Helm](#kubernetes-with-helm)
- [Deployment modes](#deployment-modes)
  - [Probe mode](#1-probe-mode-centralized-monitoring)
  - [Agent mode](#2-agent-mode-per-hostnode-monitoring)
  - [Hybrid mode](#3-hybrid-mode-ntp--kernel-correlation)
  - [Mode comparison](#mode-comparison-table)
- [Systemd deployment](#systemd-deployment)
  - [Create systemd service](#create-systemd-service)
  - [Service configuration options](#systemd-service-configuration-options)
  - [Health monitoring](#health-monitoring-with-systemd)
  - [Multi-Instance deployment](#multi-instance-deployment)
- [Metrics reference](#metrics-reference)
  - [Metrics by deployment mode](#metrics-by-deployment-mode)
  - [NTP server metrics](#ntp-server-metrics)
  - [Kernel metrics (Hybrid/Agent mode)](#kernel-metrics-hybridagent-mode-only)
  - [Exporter internal metrics](#exporter-internal-metrics)
- [Configuration](#configuration)
  - [Environment variables](#environment-variables)
  - [Metrics namespace and subsystem](#metrics-namespace-and-subsystem)
- [Prometheus integration](#prometheus-integration)
  - [Alerting rules](#alerting-rules)
  - [Example promQLqQueries](#example-promql-queries)
- [Grafana Dashboard](#grafana-dashboard)
- [Use cases and examples](#use-cases-and-examples)
- [Kernel monitoring compatibility](#kernel-monitoring-compatibility)
- [Troubleshooting](#troubleshooting)
- [Contributing](#contributing)

---

## Overview

NTP Exporter is a production-ready Prometheus exporter that provides deep visibility into Network Time Protocol (NTP) server health and time synchronization quality. It supports three distinct deployment modes to meet different monitoring requirements, from centralized infrastructure monitoring to per-node clock drift detection.

**Key highlights:**

- **Multi-mode deployment**: Probe (centralized), Agent (per-node), or Hybrid (NTP + kernel correlation)
- **Advanced metrics**: Jitter, stability, root distance, network asymmetry, and trust scores
- **Production-ready**: Rate limiting, circuit breakers, health checks, and comprehensive error handling
- **Flexible configuration**: Environment variables or YAML config with customizable metric namespaces
- **Performance optimized**: Connection pooling, buffered channels, and efficient statistical calculations

---

## Features

### Core capabilities

- **Multiple Collection Modes**: Base metrics, quality metrics, security metrics, and kernel state
- **Flexible Deployment**: Probe mode (centralized), Agent mode (DaemonSet), or Hybrid mode (kernel correlation)
- **Advanced Statistics**: Jitter, stability, root distance, network asymmetry analysis
- **Rate Limiting**: Global and per-server rate limiting to prevent server blacklisting
- **Pool Support**: Dynamic NTP pool resolution with fallback strategies
- **Context-Aware**: Proper context cancellation and timeout handling

### Performance & reliability

- **Buffered Channels**: Prevents goroutine leaks
- **Connection Pooling**: Efficient NTP query handling
- **Error Wrapping**: Full error chain preservation for debugging
- **Health Checks**: Built-in health endpoint for monitoring
- **Circuit Breakers**: Automatic protection against failing servers
- **DNS Caching**: Reduces DNS lookup overhead

---

## Quick start

### Binary

Ideal for VMs, bare-metal servers, or systemd deployments

**1. Download and install the binary:**

```bash
# Download the latest release for your platform
curl -LO https://github.com/MaximeWewer/ntp-exporter/releases/latest/download/ntp-exporter-linux-amd64
chmod +x ntp-exporter-linux-amd64
sudo mv ntp-exporter-linux-amd64 /usr/local/bin/ntp-exporter

# Verify installation
ntp-exporter --version
```

**2. Create a configuration file:**

```bash
# Create config directory
sudo mkdir -p /etc/ntp-exporter

# Download example config or create your own
sudo curl -o /etc/ntp-exporter/config.yaml \
  https://raw.githubusercontent.com/MaximeWewer/ntp-exporter/main/config.example.yaml

# Or create a minimal config
cat <<EOF | sudo tee /etc/ntp-exporter/config.yaml
server:
  address: "0.0.0.0"
  port: 9559

ntp:
  servers:
    - "pool.ntp.org"
    - "time.google.com"
  timeout: 5s
  version: 4
  samples_per_server: 3
  scrape_interval: 30s
  max_clock_offset: 100ms
  enable_kernel: true

logging:
  level: "info"

metrics:
  namespace: "ntp"
  subsystem: ""
EOF
```

**3. Run manually or create a systemd service:**

```bash
# Run manually (foreground)
ntp-exporter --config /etc/ntp-exporter/config.yaml

# Or create systemd service (see Deployment > Systemd section below)
```

**4. Verify metrics:**

```bash
curl http://localhost:9559/metrics | grep ntp_offset_seconds
```

### Docker compose

Get started in 3 steps:

**1. Choose your deployment mode:**

```bash
# Probe Mode: Monitor external NTP servers from a central location
cd deployments/docker
docker compose -f docker-compose-probe.yml up -d

# Agent Mode: Monitor time sync on each host/node
docker compose -f docker-compose-agent.yml up -d

# Hybrid Mode: Agent mode + kernel monitoring
docker compose -f docker-compose-hybrid.yml up -d
```

**2. Verify the exporter is running:**

```bash
curl http://localhost:9559/health
# Response: {"status":"healthy","service":"ntp-exporter"}
```

**3. View metrics:**

```bash
curl http://localhost:9559/metrics
```

### Kubernetes with Helm

**1. Add the Helm repository :**

```bash
helm install ntp-exporter oci://ghcr.io/maximewewer/charts/ntp-exporter \
  --version 2025.11.1 -f values.yaml --namespace monitoring --create-namespace
```

**2. Install using pre-configured values:**

```bash
# Probe Mode: Centralized deployment
helm install ntp-exporter oci://ghcr.io/maximewewer/charts/ntp-exporter \
  -f deployments/kubernetes/helm/ntp-exporter/values-probe.yaml \
  --namespace monitoring --create-namespace

# Agent Mode: DaemonSet on every node
helm install ntp-exporter oci://ghcr.io/maximewewer/charts/ntp-exporter \
  -f deployments/kubernetes/helm/ntp-exporter/values-agent.yaml \
  --namespace monitoring --create-namespace

# Hybrid Mode: DaemonSet with kernel monitoring
helm install ntp-exporter oci://ghcr.io/maximewewer/charts/ntp-exporter \
  -f deployments/kubernetes/helm/ntp-exporter/values-hybrid.yaml \
  --namespace monitoring --create-namespace
```

**3. Verify deployment:**

```bash
# Check pods
kubectl get pods -n monitoring -l app.kubernetes.io/name=ntp-exporter

# Check metrics endpoint
kubectl port-forward -n monitoring svc/ntp-exporter 9559:9559
curl http://localhost:9559/metrics
```

---

## Deployment modes

The NTP exporter supports three deployment modes, each optimized for different use cases.

### 1. Probe mode (Centralized monitoring)

**Deployment type:** Kubernetes Deployment with 2+ replicas

**When to use:**

- Monitoring public NTP infrastructure (time.google.com, pool.ntp.org)
- Centralized NTP server health monitoring
- Infrastructure-level time service monitoring

**Architecture:**

```text
┌─────────────────────────────────┐
│         Prometheus Server       │
│                                 │
│   Scrapes: /metrics every 60s   │
└──────────────┬──────────────────┘
               │
               v
┌─────────────────────────────────┐
│   NTP Exporter (Deployment)     │
│   - 2+ replicas                 │
│   - ClusterIP service           │
│   - Pod anti-affinity           │
└──────────────┬──────────────────┘
               │
               v
┌─────────────────────────────────┐
│   External NTP Servers          │
│   - time.google.com             │
│   - time.cloudflare.com         │
│   - pool.ntp.org                │
└─────────────────────────────────┘
```

**Configuration example:**

```yaml
# values-probe.yaml
mode: probe
replicaCount: 2
config:
  ntpServers:
    - time.google.com
    - time.cloudflare.com
    - time.apple.com
  enableKernel: false
  metrics:
    namespace: ntp
    subsystem: probe  # Results in ntp_probe_* metrics
```

**Key features:**

- High availability with multiple replicas
- Cluster network (hostNetwork: false)
- More samples per server (10+)
- Rate limiting enabled

### 2. Agent mode (Per-host/node monitoring)

**Deployment type:**

- **Kubernetes:** DaemonSet (one pod per node)
- **Docker:** One container per VM/host
- **Systemd:** One service per machine

**When to use:**

- Monitoring local time synchronization per host/node/VM
- Detecting host-specific clock drift
- Per-host NTP health monitoring
- Validating chronyd/ntpd on every machine
- Monitoring Docker hosts and VMs for time drift

**Architecture:**

```text
┌──────────────────────────────────────┐
│         Prometheus Server            │
│                                      │
│   Scrapes: /metrics every 30s        │
└──────────────┬───────────────────────┘
               │
               v
┌──────────────────────────────────────┐
│   NTP Exporter (Agent per host)      │
│   ┌────────┐ ┌────────┐ ┌────────┐   │
│   │ Host 1 │ │ Host 2 │ │ VM 3   │   │
│   │(K8s/VM)│ │(Docker)│ │(Docker)│   │
│   └────┬───┘ └────┬───┘ └────┬───┘   │
└────────┼──────────┼──────────┼───────┘
         │          │          │
         v          v          v
    NTP Pools  NTP Pools  NTP Pools
    (0-3.pool.ntp.org)
```

**Configuration example:**

```yaml
# values-agent.yaml
mode: agent
config:
  ntpServers:
    - 0.pool.ntp.org
    - 1.pool.ntp.org
    - 2.pool.ntp.org
    - 3.pool.ntp.org
  enableKernel: false  # Can be enabled
  metrics:
    namespace: ntp
    subsystem: ""  # Results in ntp_* metrics (no subsystem)
hostNetwork: true
tolerations:
  - key: node-role.kubernetes.io/master
    operator: Exists
```

**Key features:**

- Runs on every Kubernetes node
- Host network access for accurate measurements
- Fewer samples per server (1-3)
- Tolerates master/control-plane nodes

### 3. Hybrid mode (NTP + Kernel correlation)

**Deployment type:**

- **Kubernetes:** DaemonSet with kernel monitoring enabled (requires CAP_SYS_TIME)
- **Docker:** One container per Linux host (requires --cap-add=SYS_TIME)
- **Systemd:** One service per Linux machine

**When to use:**

- Advanced time synchronization monitoring
- Detecting discrepancies between NTP daemon and kernel
- Monitoring kernel PLL (Phase-Locked Loop) state
- Validating chronyd/ntpd effectiveness
- Forensic analysis of time synchronization issues

**Architecture:**

```text
┌──────────────────────────────────────┐
│         Prometheus Server            │
│                                      │
│   Scrapes: /metrics every 30s        │
└──────────────┬───────────────────────┘
               │
               v
┌──────────────────────────────────────┐
│   NTP Exporter (DaemonSet)           │
│   ┌──────────────────────────────┐   │
│   │ Per Node:                    │   │
│   │  - Query NTP servers         │   │
│   │  - Read kernel timex state   │   │
│   │  - Calculate divergence      │   │
│   │  - Compute coherence score   │   │
│   └──────────────────────────────┘   │
└──────────────┬───────────────────────┘
               │
               v
    ┌──────────┴──────────┐
    │                     │
    v                     v
NTP Servers      Kernel (adjtimex)
pool.ntp.org     - Offset
                 - Frequency
                 - Sync status
```

**Configuration example:**

```yaml
# values-hybrid.yaml
mode: hybrid
config:
  ntpServers:
    - 0.pool.ntp.org
    - 1.pool.ntp.org
  enableKernel: true  # CRITICAL
  metrics:
    namespace: ntp
    subsystem: ""  # Results in ntp_* metrics
hostNetwork: true
securityContext:
  capabilities:
    add:
      - SYS_TIME  # Required for adjtimex syscall
```

**Key features:**

- Correlates external NTP offset with kernel clock offset
- Detects divergence between NTP and local system clock
- Provides coherence score (0-1) for NTP/kernel agreement
- Requires Linux kernel and CAP_SYS_TIME capability
- Exposes kernel synchronization status

**Coherence score interpretation:**

| Score | Quality | Divergence | Description |
|-------|---------|------------|-------------|
| 1.0 | Perfect | < 1ms | Perfect agreement |
| 0.9-1.0 | Excellent | 1-5ms | Excellent sync |
| 0.7-0.9 | Good | 5-10ms | Good sync |
| 0.5-0.7 | Acceptable | 10-50ms | Acceptable |
| < 0.5 | Poor | > 50ms | Investigation needed |

### Mode comparison table

| Feature | Probe Mode | Agent Mode | Hybrid Mode |
|---------|------------|------------|-------------|
| **Deployment Type** | Deployment | DaemonSet | DaemonSet |
| **Replicas** | 2+ | 1 per node | 1 per node |
| **Host Network** | No | Yes | Yes |
| **Kernel Metrics** | No | Optional | Yes |
| **NTP Servers** | External/Public | Local/Pools | Local/Pools |
| **Scrape Interval** | 60s | 30s | 30s |
| **Samples per Server** | 10+ | 1-3 | 3-5 |
| **Use Case** | Infrastructure monitoring | Node-level monitoring | Advanced diagnostics |
| **Metric Prefix** | `ntp_probe_*` | `ntp_*` | `ntp_*` |

---

## Systemd deployment

Deploy NTP Exporter as a systemd service for production-grade reliability on Linux VMs and bare-metal servers.

### Create systemd service

**1. Create the service file:**

```bash
sudo tee /etc/systemd/system/ntp-exporter.service > /dev/null <<'EOF'
[Unit]
Description=NTP Prometheus Exporter
Documentation=https://github.com/MaximeWewer/ntp-exporter
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=ntp-exporter
Group=ntp-exporter

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/ntp-exporter

# For kernel monitoring (hybrid/agent mode), add capability
AmbientCapabilities=CAP_SYS_TIME

# Binary and config
ExecStart=/usr/local/bin/ntp-exporter --config /etc/ntp-exporter/config.yaml

# Restart configuration
Restart=on-failure
RestartSec=5s

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=ntp-exporter

[Install]
WantedBy=multi-user.target
EOF
```

**2. Create dedicated user:**

```bash
# Create system user and group
sudo useradd --system --no-create-home --shell /bin/false ntp-exporter

# Create working directory
sudo mkdir -p /var/lib/ntp-exporter
sudo chown ntp-exporter:ntp-exporter /var/lib/ntp-exporter

# Set permissions on config
sudo chown -R ntp-exporter:ntp-exporter /etc/ntp-exporter
```

**3. Enable and start the service:**

```bash
# Reload systemd
sudo systemctl daemon-reload

# Enable on boot
sudo systemctl enable ntp-exporter

# Start the service
sudo systemctl start ntp-exporter

# Check status
sudo systemctl status ntp-exporter
```

**4. View logs:**

```bash
# Follow logs in real-time
sudo journalctl -u ntp-exporter -f

# View recent logs
sudo journalctl -u ntp-exporter -n 100
```

### Systemd service configuration options

**Without kernel monitoring (Probe mode):**

```ini
# Remove CAP_SYS_TIME capability
# AmbientCapabilities=CAP_SYS_TIME
```

**With environment variables override:**

```ini
[Service]
Environment="NTP_SERVERS=pool.ntp.org,time.google.com"
Environment="LOG_LEVEL=debug"
Environment="NTP_ENABLE_KERNEL=true"
ExecStart=/usr/local/bin/ntp-exporter --config /etc/ntp-exporter/config.yaml
```

**With custom user and higher privileges:**

```ini
[Service]
User=root
Group=root
# Full privileges for kernel monitoring
AmbientCapabilities=CAP_SYS_TIME CAP_NET_RAW
```

### Health monitoring with systemd

Add a health check watchdog:

```ini
[Service]
# Enable watchdog (30 seconds)
WatchdogSec=30s

# Notify systemd when ready
Type=notify
NotifyAccess=main
```

### Multi-instance deployment

Deploy multiple instances with different configs:

```bash
# Create instance-specific configs
sudo cp /etc/ntp-exporter/config.yaml /etc/ntp-exporter/config-pool1.yaml
sudo cp /etc/ntp-exporter/config.yaml /etc/ntp-exporter/config-pool2.yaml

# Edit ports in each config (9559, 9560, etc.)

# Create service instances
sudo cp /etc/systemd/system/ntp-exporter.service \
     /etc/systemd/system/ntp-exporter@.service

# Modify ExecStart to use instance config
# ExecStart=/usr/local/bin/ntp-exporter --config /etc/ntp-exporter/config-%i.yaml

# Start instances
sudo systemctl start ntp-exporter@pool1
sudo systemctl start ntp-exporter@pool2
```

---

## Metrics reference

### Metrics by Deployment Mode

**IMPORTANT:** The metric naming convention differs by mode:

| Mode | METRICS_NAMESPACE | METRICS_SUBSYSTEM | Metric Prefix | Kernel Metrics |
|------|-------------------|-------------------|---------------|----------------|
| **Probe** | `ntp` | `probe` | `ntp_probe_*` | No |
| **Agent** | `ntp` | `""` (empty) | `ntp_*` | Optional |
| **Hybrid** | `ntp` | `""` (empty) | `ntp_*` | Yes |

**Examples:**

```prometheus
# Probe Mode (METRICS_SUBSYSTEM="probe")
ntp_probe_offset_seconds{server="time.google.com"} -0.000328
ntp_probe_rtt_seconds{server="time.google.com"} 0.015234
ntp_probe_jitter_seconds{server="time.google.com"} 0.000086

# Agent/Hybrid Mode (METRICS_SUBSYSTEM="")
ntp_offset_seconds{server="pool.ntp.org"} -0.000328
ntp_rtt_seconds{server="pool.ntp.org"} 0.015234
ntp_kernel_offset_seconds{node="node1"} -0.000001  # Only in Hybrid/Agent with kernel enabled
```

### NTP server metrics

Available in **all modes** (prefix varies by mode):

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `{prefix}_offset_seconds` | Gauge | server, stratum, version | Time offset between local clock and NTP server |
| `{prefix}_clock_offset_exceeded` | Gauge | server | Whether the clock offset exceeds the configured threshold (1=exceeded, 0=within limits) |
| `{prefix}_rtt_seconds` | Gauge | server | Round-trip time to NTP server |
| `{prefix}_jitter_seconds` | Gauge | server | Jitter calculated from multiple samples |
| `{prefix}_stability_seconds` | Gauge | server | Stability of time offset (standard deviation) |
| `{prefix}_asymmetry_seconds` | Gauge | server | Network asymmetry detected |
| `{prefix}_server_reachable` | Gauge | server | Whether the server is reachable (1=yes, 0=no) |
| `{prefix}_stratum` | Gauge | server | NTP server stratum level (0-16) |
| `{prefix}_leap_indicator` | Gauge | server | Leap second indicator (0-3) |
| `{prefix}_root_delay_seconds` | Gauge | server | Root delay of the NTP server |
| `{prefix}_root_dispersion_seconds` | Gauge | server | Root dispersion of the NTP server |
| `{prefix}_root_distance_seconds` | Gauge | server | Calculated root distance (quality metric) |
| `{prefix}_reference_timestamp_seconds` | Gauge | server | Reference timestamp of the NTP server |
| `{prefix}_precision_seconds` | Gauge | server | Precision of the NTP server |
| `{prefix}_packet_loss_ratio` | Gauge | server | Packet loss ratio during measurements (0-1) |
| `{prefix}_samples_count` | Gauge | server | Number of samples used for calculation |
| `{prefix}_server_trust_score` | Gauge | server | Trust score for the server (0-1) |

> **Note:** Replace `{prefix}` with `ntp` for Agent/Hybrid mode or `ntp_probe` for Probe mode.

### Kernel metrics (Hybrid/Agent Mode Only)

Available **only when `NTP_ENABLE_KERNEL=true`** (Linux only):

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `ntp_kernel_offset_seconds` | Gauge | node | Kernel time offset (from adjtimex syscall) |
| `ntp_kernel_frequency_ppm` | Gauge | node | Kernel frequency adjustment in PPM |
| `ntp_kernel_max_error_seconds` | Gauge | node | Kernel maximum time error |
| `ntp_kernel_est_error_seconds` | Gauge | node | Kernel estimated time error |
| `ntp_kernel_sync_status` | Gauge | node | Kernel synchronization status (1=synced, 0=unsynced) |
| `ntp_kernel_divergence_seconds` | Gauge | node | Absolute difference between NTP and kernel offsets |
| `ntp_kernel_coherence_score` | Gauge | node | Coherence score (0-1, higher is better) |

**Example:**

```prometheus
# Hybrid mode metrics
ntp_offset_seconds{server="pool.ntp.org"} -0.000328
ntp_kernel_offset_seconds{node="k8s-node-1"} -0.000001
ntp_kernel_divergence_seconds{node="k8s-node-1"} 0.000327
ntp_kernel_coherence_score{node="k8s-node-1"} 0.99
```

### Exporter internal metrics

Available in **all modes**:

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `ntp_exporter_build_info` | Gauge | version, commit, go_version | Build information |
| `ntp_exporter_servers_configured` | Gauge | - | Number of NTP servers configured |
| `ntp_exporter_scrapes_total` | Counter | status | Total number of scrapes (success/failure) |
| `ntp_exporter_scrape_duration_seconds` | Histogram | - | Duration of NTP scrape operations |
| `ntp_exporter_collector_duration_seconds` | Histogram | collector | Collector execution duration |
| `ntp_query_duration_seconds` | Histogram | server, status | NTP query duration distribution |
| `ntp_exporter_memory_allocated_bytes` | Gauge | - | Memory allocated by Go runtime |
| `ntp_exporter_memory_heap_bytes` | Gauge | - | Heap memory in use |
| `ntp_exporter_goroutines_count` | Gauge | - | Number of active goroutines |

---

## Configuration

### Environment variables

Complete list of environment variables (Docker/Docker Compose):

#### Server configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `NTP_EXPORTER_ADDRESS` | HTTP server bind address | `0.0.0.0` |
| `NTP_EXPORTER_PORT` | HTTP server port | `9559` |
| `SERVER_READ_TIMEOUT` | HTTP read timeout | `10s` |
| `SERVER_WRITE_TIMEOUT` | HTTP write timeout | `10s` |
| `TLS_ENABLED` | Enable HTTPS/TLS | `false` |
| `TLS_CERT_FILE` | Path to TLS certificate | `""` |
| `TLS_KEY_FILE` | Path to TLS private key | `""` |
| `ENABLE_CORS` | Enable CORS headers | `false` |
| `ALLOWED_ORIGINS` | Allowed CORS origins (comma-separated) | `""` |

#### NTP configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `NTP_SERVERS` | Comma-separated list of NTP servers | `""` |
| `NTP_TIMEOUT` | NTP query timeout | `5s` |
| `NTP_VERSION` | NTP protocol version (2, 3, 4) | `4` |
| `NTP_SAMPLES` | Samples per server for statistics | `3` |
| `NTP_MAX_CONCURRENCY` | Maximum concurrent queries | `10` |
| `NTP_SCRAPE_INTERVAL` | Interval between NTP collections | `30s` |
| `NTP_MAX_CLOCK_OFFSET` | Maximum acceptable clock offset threshold | `100ms` |
| `NTP_ENABLE_KERNEL` | Enable kernel monitoring (Linux only) | `false` |

#### Rate limiting

| Variable | Description | Default |
|----------|-------------|---------|
| `RATE_LIMIT_ENABLED` | Enable rate limiting | `false` |
| `RATE_LIMIT_GLOBAL` | Global rate limit (req/s) | `1000` |
| `RATE_LIMIT_PER_SERVER` | Per-server rate limit (req/s) | `60` |
| `RATE_LIMIT_BURST_SIZE` | Burst size for rate limiter | `10` |
| `RATE_LIMIT_BACKOFF_DURATION` | Backoff duration after rate limit | `1m` |

#### Circuit breaker

| Variable | Description | Default |
|----------|-------------|---------|
| `CIRCUIT_BREAKER_ENABLED` | Enable circuit breaker | `true` |
| `CIRCUIT_BREAKER_MAX_REQUESTS` | Max requests in half-open state | `3` |
| `CIRCUIT_BREAKER_INTERVAL` | Interval to clear counters | `60s` |
| `CIRCUIT_BREAKER_TIMEOUT` | Timeout in open state | `30s` |
| `CIRCUIT_BREAKER_FAILURE_THRESHOLD` | Failure ratio threshold (0-1) | `0.6` |

#### Adaptive sampling

| Variable | Description | Default |
|----------|-------------|---------|
| `ADAPTIVE_SAMPLING_ENABLED` | Enable adaptive sampling | `false` |
| `ADAPTIVE_SAMPLING_DEFAULT_SAMPLES` | Default number of samples | `3` |
| `ADAPTIVE_SAMPLING_HIGH_DRIFT_SAMPLES` | Samples for high drift | `10` |
| `ADAPTIVE_SAMPLING_DRIFT_THRESHOLD` | Drift threshold to increase samples | `50ms` |
| `ADAPTIVE_SAMPLING_MAX_DURATION` | Max duration for sampling | `30s` |

#### Worker pool

| Variable | Description | Default |
|----------|-------------|---------|
| `WORKER_POOL_ENABLED` | Enable worker pool | `false` |
| `WORKER_POOL_SIZE` | Number of workers | `5` |

#### DNS cache

| Variable | Description | Default |
|----------|-------------|---------|
| `DNS_CACHE_ENABLED` | Enable DNS caching | `true` |
| `DNS_CACHE_MIN_TTL` | Minimum DNS cache TTL | `5m` |
| `DNS_CACHE_MAX_TTL` | Maximum DNS cache TTL | `60m` |
| `DNS_CACHE_CLEANUP_WORKERS` | Number of cleanup workers | `1` |

#### Logging

| Variable | Description | Default |
|----------|-------------|---------|
| `LOG_LEVEL` | Log level (trace, debug, info, warn, error, fatal) | `info` |
| `LOG_FORMAT` | Log format (json, console) | `json` |
| `LOG_ENABLE_FILE` | Enable logging to file | `false` |
| `LOG_FILE_PATH` | Path to log file | `""` |

#### Metrics

| Variable | Description | Default |
|----------|-------------|---------|
| `METRICS_NAMESPACE` | Prometheus metrics namespace | `ntp` |
| `METRICS_SUBSYSTEM` | Prometheus metrics subsystem | `""` |

### Metrics namespace and subsystem

The exporter allows customization of metric names through namespace and subsystem:

**Formula:** `{namespace}_{subsystem}_{metric_name}`

**Examples:**

```bash
# Default Agent/Hybrid mode
METRICS_NAMESPACE=ntp
METRICS_SUBSYSTEM=""
# Result: ntp_offset_seconds, ntp_rtt_seconds

# Default Probe mode
METRICS_NAMESPACE=ntp
METRICS_SUBSYSTEM=probe
# Result: ntp_probe_offset_seconds, ntp_probe_rtt_seconds

# Custom namespace
METRICS_NAMESPACE=monitoring
METRICS_SUBSYSTEM=timesync
# Result: monitoring_timesync_offset_seconds
```

**Important:** Kernel metrics always use the `ntp_kernel_*` prefix regardless of subsystem configuration.

---

## Prometheus integration

### Alerting rules

Example alerting rules for critical NTP issues:

```yaml
groups:
  - name: ntp_critical
    interval: 30s
    rules:
      # High Time Drift (>100ms)
      - alert: NTPHighTimeDrift
        expr: abs(ntp_offset_seconds) > 0.1
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "Critical time drift detected on {{ $labels.server }}"
          description: "Time offset is {{ $value }}s, exceeding 100ms threshold"

      # Clock Offset Exceeded Configured Threshold
      - alert: NTPClockOffsetExceeded
        expr: ntp_clock_offset_exceeded == 1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "NTP clock offset exceeded threshold on {{ $labels.server }}"
          description: "The clock offset on {{ $labels.server }} has exceeded the configured max_clock_offset threshold"

      # Server Unreachable
      - alert: NTPServerUnreachable
        expr: ntp_server_reachable == 0
        for: 3m
        labels:
          severity: critical
        annotations:
          summary: "NTP server {{ $labels.server }} is unreachable"

      # High Jitter (>50ms)
      - alert: NTPHighJitter
        expr: ntp_jitter_seconds > 0.050
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "High jitter detected on {{ $labels.server }}: {{ $value }}s"

      # Clock Unsynchronized
      - alert: NTPClockUnsynchronized
        expr: ntp_leap_indicator == 3
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "NTP server {{ $labels.server }} reports unsynchronized clock"

      # Insufficient Redundancy
      - alert: NTPInsufficientRedundancy
        expr: sum(ntp_server_reachable) < 2
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "Less than 2 NTP servers reachable"
          description: "Only {{ $value }} server(s) reachable. Minimum 3 recommended."

  - name: ntp_hybrid_mode
    interval: 30s
    rules:
      # NTP/Kernel Divergence (Hybrid mode)
      - alert: NTPKernelDivergence
        expr: ntp_kernel_divergence_seconds > 0.010
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "NTP and kernel offsets diverging on {{ $labels.node }}"
          description: "Divergence is {{ $value }}s (>10ms) between NTP and kernel"

      # Low Coherence Score (Hybrid mode)
      - alert: NTPKernelIncoherence
        expr: ntp_kernel_coherence_score < 0.7
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "Low coherence between NTP and kernel on {{ $labels.node }}"
          description: "Coherence score is {{ $value }} (should be >0.7)"

      # Kernel Not Synchronized
      - alert: KernelNotSynchronized
        expr: ntp_kernel_sync_status == 0
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Kernel clock not synchronized on {{ $labels.node }}"
```

### Example promQL queries

**Basic time monitoring:**

```promql
# Current time offset for all servers
ntp_offset_seconds

# Average RTT by server (5m window)
avg_over_time(ntp_rtt_seconds[5m])

# Maximum jitter across all servers
max(ntp_jitter_seconds)

# Server availability percentage (last 1h)
avg_over_time(ntp_server_reachable[1h]) * 100
```

**Advanced queries:**

```promql
# 95th percentile query duration
histogram_quantile(0.95, rate(ntp_query_duration_seconds_bucket[5m]))

# Servers with high packet loss
ntp_packet_loss_ratio > 0.1

# Root distance (quality metric)
ntp_root_distance_seconds

# Trust score threshold
ntp_server_trust_score < 0.5

# Memory usage rate
rate(ntp_exporter_memory_allocated_bytes[5m])
```

**Hybrid mode queries:**

```promql
# Kernel offset vs NTP offset
ntp_kernel_offset_seconds
ntp_offset_seconds

# Divergence between kernel and NTP
ntp_kernel_divergence_seconds

# Nodes with poor coherence
ntp_kernel_coherence_score < 0.7

# Kernel frequency adjustment (drift rate)
ntp_kernel_frequency_ppm
```

**Probe mode queries (note the `ntp_probe_` prefix):**

```promql
# Probe mode offset
ntp_probe_offset_seconds

# Probe mode RTT
ntp_probe_rtt_seconds

# Probe mode server reachability
ntp_probe_server_reachable
```

---

## Grafana dashboard

Import the following dashboard JSON or create panels manually:

**Key panels:**

1. **Time Offset Trends**: Line graph of `ntp_offset_seconds` by server
2. **Round-Trip Time Distribution**: Line graph of `ntp_rtt_seconds`
3. **Jitter Analysis**: Line graph of `ntp_jitter_seconds`
4. **Server Reachability Heatmap**: Heatmap of `ntp_server_reachable`
5. **Stratum Distribution**: Bar chart of `ntp_stratum`
6. **Query Duration Histograms**: Histogram of `ntp_query_duration_seconds`
7. **Memory Usage Trends**: Line graph of `ntp_exporter_memory_*`
8. **Kernel Metrics (Hybrid)**: Graphs for `ntp_kernel_*` metrics

**Example panel queries:**

```promql
# Panel: Current Time Offset
ntp_offset_seconds

# Panel: Server Availability (1h)
avg_over_time(ntp_server_reachable[1h]) * 100

# Panel: Network RTT
ntp_rtt_seconds

# Panel: Kernel Divergence (Hybrid mode)
ntp_kernel_divergence_seconds

# Panel: Coherence Score (Hybrid mode)
ntp_kernel_coherence_score
```

---

## Use cases and examples

### Use case 1: Monitoring public NTP infrastructure

**Scenario:** You want to monitor the health and availability of public NTP servers used by your organization.

**Solution:** Deploy in **Probe mode**

```bash
# Docker Compose
cd deployments/docker
docker-compose -f docker-compose-probe.yml up -d

# Kubernetes
helm install ntp-exporter ./deployments/kubernetes/helm/ntp-exporter \
  -f deployments/kubernetes/helm/ntp-exporter/values-probe.yaml
```

**Configuration:**

```yaml
config:
  ntpServers:
    - time.google.com
    - time.cloudflare.com
    - time.apple.com
    - pool.ntp.org
  metrics:
    subsystem: probe  # Metrics: ntp_probe_*
```

### Use case 2: Per-Node clock drift detection

**Scenario:** You need to detect clock drift on individual Kubernetes nodes.

**Solution:** Deploy in **Agent mode**

```bash
helm install ntp-exporter ./deployments/kubernetes/helm/ntp-exporter \
  -f deployments/kubernetes/helm/ntp-exporter/values-agent.yaml
```

**Configuration:**

```yaml
mode: agent
config:
  ntpServers:
    - 0.pool.ntp.org
    - 1.pool.ntp.org
    - 2.pool.ntp.org
  metrics:
    subsystem: ""  # Metrics: ntp_*
hostNetwork: true
```

**Alert:**

```yaml
- alert: NodeClockDrift
  expr: abs(ntp_offset_seconds{node=~".*"}) > 0.050
  for: 5m
  annotations:
    summary: "Clock drift >50ms on node {{ $labels.node }}"
```

### Use case 3: Validating chronyd effectiveness

**Scenario:** You want to validate that chronyd is properly synchronizing the kernel clock.

**Solution:** Deploy in **Hybrid mode**

```bash
helm install ntp-exporter ./deployments/kubernetes/helm/ntp-exporter \
  -f deployments/kubernetes/helm/ntp-exporter/values-hybrid.yaml
```

**Configuration:**

```yaml
mode: hybrid
config:
  ntpServers:
    - 0.pool.ntp.org
    - 1.pool.ntp.org
  enableKernel: true
  metrics:
    subsystem: ""  # Metrics: ntp_* and ntp_kernel_*
hostNetwork: true
securityContext:
  capabilities:
    add: [SYS_TIME]
```

**Validation query:**

```promql
# Low divergence = chronyd is working
ntp_kernel_divergence_seconds < 0.001

# High coherence = good sync
ntp_kernel_coherence_score > 0.9
```

---

## Kernel monitoring compatibility

The hybrid mode (`NTP_ENABLE_KERNEL=true`) reads kernel NTP state via the `adjtimex` syscall on Linux. However, compatibility varies by NTP daemon implementation.

### Chrony

**Version < 1.31** (Debian Jessie):

- The `STA_UNSYNC` flag may not be properly maintained
- **Impact**: `ntp_kernel_sync_status` may be inaccurate
- **Workaround**: Monitor `ntp_kernel_divergence_seconds` instead:

```promql
# Daemon is working if divergence is low
ntp_kernel_divergence_seconds < 0.001  # < 1ms
```

**Modern versions (≥ 1.31)**:

- Full support
- Ensure `rtcsync` option is enabled in `/etc/chrony/chrony.conf`
- Verify with: `chronyc tracking`

### systemd-timesyncd

- **Full support** for all versions
- Monitor both metrics as recommended by Prometheus node_exporter:

```promql
ntp_kernel_sync_status == 1 AND
abs(ntp_kernel_offset_seconds) < 0.050  # < 50ms
```

### OpenNTPD

**Version < 5.9p1**:

- Does not update kernel flags
- **Impact**: Kernel metrics will be stale or incorrect
- **Workaround**: Disable kernel monitoring:

```yaml
ntp:
  enableKernel: false  # Rely on NTP metrics only
```

**Version ≥ 5.9p1**:

- Full support

### ntpd (Classic)

- **Full support** for all modern versions
- Kernel state is updated regularly via `ntp_adjtime()`

### Detection queries

Detect if your NTP daemon is not updating the kernel properly:

```promql
# NTP measurements show sync, but kernel diverges
(abs(ntp_offset_seconds) < 0.001) AND
(ntp_kernel_divergence_seconds > 0.010)
```

Alert on low coherence (daemon not working):

```promql
# Coherence score < 0.7 = problem
ntp_kernel_coherence_score < 0.7
```

### Why hybrid mode?

The hybrid mode provides superior monitoring compared to kernel-only approaches (like Prometheus `node_exporter` timex collector) because:

1. **Direct NTP measurements** bypass daemon issues
2. **Divergence detection** catches misconfigured daemons
3. **Coherence score** quantifies NTP/kernel agreement
4. **Works with all daemons** (even those with kernel bugs)

**Reference:** Prometheus node_exporter [timex collector documentation](https://github.com/prometheus/node_exporter/blob/master/docs/TIME.md#timex-collector) acknowledges similar daemon compatibility issues.

---

## Troubleshooting

### Exporter not starting

```bash
# Check logs
docker logs ntp-exporter

# Common issues:
# - Port 9559 already in use → Change NTP_EXPORTER_PORT
# - Invalid server address → Check NTP_SERVERS format
# - Config file not found → Verify mount path
```

### No metrics for a server

```bash
# Check if server is reachable
ntp_server_reachable{server="pool.ntp.org"}

# Check query duration (should be < timeout)
ntp_query_duration_seconds_bucket

# Verify NTP port (UDP 123) is open
nc -u -v -z pool.ntp.org 123

# Check DNS resolution
nslookup pool.ntp.org
```

### Metrics have wrong prefix

**Problem:** Expected `ntp_offset_seconds` but seeing `ntp_probe_offset_seconds`

**Solution:** Check `METRICS_SUBSYSTEM` environment variable:

```bash
# Agent/Hybrid mode should use empty subsystem
METRICS_SUBSYSTEM=""

# Probe mode uses "probe" subsystem
METRICS_SUBSYSTEM="probe"
```

### Kernel metrics not appearing

**Problem:** `ntp_kernel_*` metrics are missing in Hybrid/Agent mode

**Checklist:**

1. Verify `NTP_ENABLE_KERNEL=true` is set
2. Check that you're running on Linux (not supported on Windows/macOS)
3. Verify CAP_SYS_TIME capability:

```yaml
securityContext:
  capabilities:
    add:
      - SYS_TIME
```

1. Check logs for kernel access errors:

```bash
kubectl logs -n monitoring ntp-exporter-xxx | grep kernel
```

### High memory usage

```bash
# Check memory metrics
ntp_exporter_memory_allocated_bytes
ntp_exporter_goroutines_count

# Reduce concurrent queries
NTP_MAX_CONCURRENCY=5

# Reduce samples per server
NTP_SAMPLES=1

# Disable adaptive sampling
ADAPTIVE_SAMPLING_ENABLED=false
```

### Rate limiting issues

**Symptom:** Frequent "rate limit exceeded" errors

**Solution:**

```bash
# Increase rate limits
RATE_LIMIT_GLOBAL=2000
RATE_LIMIT_PER_SERVER=120

# Or disable (not recommended for production)
RATE_LIMIT_ENABLED=false
```

### Circuit breaker opening frequently

**Symptom:** Servers marked as unavailable even though they're reachable

**Solution:**

```bash
# Increase failure threshold
CIRCUIT_BREAKER_FAILURE_THRESHOLD=0.8

# Increase timeout
CIRCUIT_BREAKER_TIMEOUT=60s

# Or disable
CIRCUIT_BREAKER_ENABLED=false
```

---

## Contributing

Contributions are welcome! Please:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit changes (`git commit -m 'Add amazing feature'`)
4. Push to branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

**Development Guidelines:**

- Write tests for new features
- Follow Go best practices and conventions
- Update documentation
- Add Godoc comments for public APIs
- Run `go fmt`, `go vet`, and `golangci-lint`

---

## Acknowledgments

- Built with [beevik/ntp](https://github.com/beevik/ntp) for NTP protocol implementation
- Uses [Prometheus client_golang](https://github.com/prometheus/client_golang) for metrics

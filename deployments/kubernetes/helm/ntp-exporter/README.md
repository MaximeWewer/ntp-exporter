# NTP Exporter Helm Chart

Helm chart for deploying the NTP Exporter to Kubernetes. The NTP Exporter is a Prometheus exporter that provides Network Time Protocol metrics.

## Modes

- `mode: agent` (default): DaemonSet measuring the clock of every node. Set
  `kernel.enabled=true` to also read the kernel clock state (unprivileged).
- `mode: probe`: Deployment monitoring NTP servers from a central place.

`mode: hybrid` is deprecated and behaves as `mode: agent` with `kernel.enabled=true`.

## Main values

| Value | Default | Description |
|---|---|---|
| `mode` | `agent` | `agent` or `probe` |
| `kernel.enabled` / `kernel.violationOnly` | `false` | Kernel option of the agent mode |
| `config.ntpServers` | `pool.ntp.org`, `time.google.com`, `time.cloudflare.com` | Servers to query |
| `config.scrapeInterval` | `60s` | Interval between two collection cycles |
| `configYaml.enabled` / `configYaml.content` | `false` | Provide the full exporter YAML configuration |
| `webConfig.existingSecret` | | Secret with a Prometheus web configuration (TLS, basic auth) |
| `serviceMonitor.enabled` | `false` | Create a ServiceMonitor |
| `prometheusRule.enabled` | `false` | Create a PrometheusRule with the recommended alerts |

Every value is documented in `values-with-all-options.yaml`. See the
[project README](../../../../README.md) for the metrics and the
[migration guide](../../../../MIGRATION.md) when upgrading from v1.

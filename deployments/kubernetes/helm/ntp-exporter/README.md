# NTP Exporter Helm Chart

Helm chart for deploying the NTP Exporter to Kubernetes. The NTP Exporter is a Prometheus exporter that provides Network Time Protocol metrics.

## Modes

The mode is required (`values-agent.yaml` or `values-probe.yaml`):

- `mode: agent`: DaemonSet measuring the clock of every node: kernel clock
  state (unprivileged) and chronyd when available, at most 2 reference
  servers, every series labelled with `node`.
- `mode: probe`: Deployment monitoring NTP servers from a central place.

The former `mode: hybrid` is `mode: agent`, whose kernel option is enabled by
default.

## Installing

Install the published chart, which pulls the image of the same release:

```bash
helm install ntp-exporter oci://ghcr.io/maximewewer/charts/ntp-exporter \
  -n monitoring -f values-probe.yaml
```

The chart in the repository may be ahead of the last release: `appVersion` is
the last released image, and the release workflow sets it at each release. To
install from the sources, build the image of the same commit:

```bash
docker build -t ntp-exporter:dev .
helm install ntp-exporter deployments/kubernetes/helm/ntp-exporter \
  --set image.repository=ntp-exporter --set image.tag=dev
```

## Main values

| Value | Default | Description |
|---|---|---|
| `mode` | (required) | `agent` or `probe` |
| `kernel.enabled` / `chrony.enabled` | unset | Kernel and chrony options of the agent mode: unset, enabled when available |
| `config` | | The YAML configuration of the exporter, rendered as is (same keys as `deployments/docker/config-example.yaml`): `config.ntp.servers`, `config.ntp.collection_interval`... The chart sets the mode, `/probe` and the kernel and chrony options from the values above |
| `config.ntp.servers` | none | Servers to query (agent: at most 2 reference servers) |
| `webConfig.existingSecret` | | Secret with a Prometheus web configuration (TLS, basic auth), mounted in `/etc/ntp-exporter/web` |
| `webConfig.scheme` / `webConfig.basicAuth` | `http` / `false` | Set `https` when the web configuration enables TLS (probes and ServiceMonitor use HTTPS); set `basicAuth` when it enables basic auth (probes then only check the port) |
| `serviceMonitor.scheme` / `tlsConfig` / `basicAuth` | | Scrape settings of the ServiceMonitor endpoint |
| `serviceMonitor.enabled` | `false` | Create a ServiceMonitor |
| `scrapeAnnotations` | `true` | Add the `prometheus.io/*` scrape annotations, only when the ServiceMonitor is disabled |
| `prometheusRule.enabled` | `false` | Create a PrometheusRule with the recommended alerts |
| `image.repository` | `ghcr.io/maximewewer/ntp-exporter` | Image, tagged with the chart appVersion by default |
| `networkPolicy.enabled` | `false` | Create a NetworkPolicy (enabled in `values-agent.yaml` and `values-probe.yaml`) |
| `networkPolicy.prometheusNamespace` | `monitoring` | Namespace allowed to scrape, matched on `kubernetes.io/metadata.name` |
| `networkPolicy.ntpCIDRs` / `exceptCIDRs` | `0.0.0.0/0`, `::/0` / none | Destinations of NTP (UDP 123) and NTS key exchange (TCP 4460) traffic |

The network policy allows DNS, NTP and NTS traffic towards `ntpCIDRs` through
`ipBlock` rules: a `namespaceSelector` only matches pods of the cluster and
would block every external server. Set `exceptCIDRs` to the pod and service
ranges of the cluster to keep the exporter away from them, or set
`networkPolicy.ingress` / `networkPolicy.egress` to replace the generated
rules with your own.

Every value is documented in `values-with-all-options.yaml`. See the
[project README](../../../../README.md) for the metrics.

The pod has no Kubernetes API access: no RBAC role is created and the service
account token is not mounted.

## Pod Security Standards

With the default values the pods meet the `restricted` profile. Two options
of the agent mode need the `privileged` profile on their namespace, as they
are not allowed by `baseline`:

- `hostNetwork: true` (chrony option over UDP 127.0.0.1:323);
- `chrony.socketDir`, a `hostPath` volume on the chronyd socket directory.

On the host network the DNS policy becomes `ClusterFirstWithHostNet`, so that
server names still resolve through the cluster DNS.

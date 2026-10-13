{{/*
Expand the name of the chart.
*/}}
{{- define "ntp-exporter.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "ntp-exporter.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "ntp-exporter.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "ntp-exporter.labels" -}}
helm.sh/chart: {{ include "ntp-exporter.chart" . }}
{{ include "ntp-exporter.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "ntp-exporter.selectorLabels" -}}
app.kubernetes.io/name: {{ include "ntp-exporter.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "ntp-exporter.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "ntp-exporter.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the image path
*/}}
{{- define "ntp-exporter.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion }}
{{- printf "%s:%s" .Values.image.repository $tag }}
{{- end }}

{{/*
Create the ConfigMap name
*/}}
{{- define "ntp-exporter.configMapName" -}}
{{- printf "%s-config" (include "ntp-exporter.fullname" .) }}
{{- end }}

{{/*
Create the ServiceMonitor namespace
*/}}
{{- define "ntp-exporter.serviceMonitor.namespace" -}}
{{- if .Values.serviceMonitor.namespace }}
{{- .Values.serviceMonitor.namespace }}
{{- else }}
{{- .Release.Namespace }}
{{- end }}
{{- end }}

{{/*
Liveness or readiness probe. The web configuration applies to every path:
with TLS the probe uses HTTPS, with basic auth (the kubelet has no password)
it only checks that the port accepts connections.
*/}}
{{- define "ntp-exporter.probe" -}}
{{- $probe := deepCopy .probe -}}
{{- $web := .root.Values.webConfig -}}
{{- if $probe.httpGet -}}
{{- if and $web.existingSecret $web.basicAuth -}}
{{- $_ := unset $probe "httpGet" -}}
{{- $_ := set $probe "tcpSocket" (dict "port" "metrics") -}}
{{- else -}}
{{- $_ := set $probe.httpGet "port" "metrics" -}}
{{- if and $web.existingSecret (eq ($web.scheme | default "http") "https") -}}
{{- $_ := set $probe.httpGet "scheme" "HTTPS" -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- toYaml $probe -}}
{{- end -}}

{{/*
Effective deployment mode: the deprecated "hybrid" mode is "agent" with the kernel option
*/}}
{{- define "ntp-exporter.mode" -}}
{{- if eq .Values.mode "hybrid" -}}agent{{- else -}}{{ .Values.mode }}{{- end -}}
{{- end -}}

{{/*
Whether the kernel clock state option is enabled (kernel.enabled, deprecated
config.enableKernel or deprecated hybrid mode)
*/}}
{{- define "ntp-exporter.kernelEnabled" -}}
{{- if or (and .Values.kernel .Values.kernel.enabled) .Values.config.enableKernel (eq .Values.mode "hybrid") -}}true{{- end -}}
{{- end -}}

{{/*
Whether the /probe endpoint is served (enabled unless explicitly disabled)
*/}}
{{- define "ntp-exporter.probeEndpointEnabled" -}}
{{- if and .Values.probeEndpoint (or .Values.probeEndpoint.enabled .Values.probeEndpoint.only) -}}true{{- end -}}
{{- end -}}

{{/*
Whether only /probe targets are measured (no static servers)
*/}}
{{- define "ntp-exporter.probeEndpointOnly" -}}
{{- if and .Values.probeEndpoint .Values.probeEndpoint.only -}}true{{- end -}}
{{- end -}}

{{/*
chronyd address
*/}}
{{- define "ntp-exporter.chronyAddress" -}}
{{- if .Values.chrony.socketDir -}}
unix://{{ trimSuffix "/" .Values.chrony.socketDir }}/chronyd.sock
{{- else -}}
{{ .Values.chrony.address | default "udp://127.0.0.1:323" }}
{{- end -}}
{{- end -}}

{{/*
Validate mode value
*/}}
{{- define "ntp-exporter.validateMode" -}}
{{- if and (ne .Values.mode "agent") (ne .Values.mode "probe") (ne .Values.mode "hybrid") -}}
{{- fail "mode must be 'agent' or 'probe' ('hybrid' is deprecated: use mode=agent with kernel.enabled=true)" -}}
{{- end -}}
{{- if and (eq .Values.mode "probe") (include "ntp-exporter.kernelEnabled" .) -}}
{{- fail "kernel.enabled only makes sense in agent mode: the kernel state describes the node the agent runs on" -}}
{{- end -}}
{{- if and .Values.chrony .Values.chrony.enabled -}}
{{- if eq (include "ntp-exporter.mode" .) "probe" -}}
{{- fail "chrony.enabled only makes sense in agent mode: chronyd runs on the node" -}}
{{- end -}}
{{- if and (not .Values.chrony.socketDir) (not .Values.hostNetwork) (hasPrefix "udp://127." (.Values.chrony.address | default "udp://127.0.0.1:323")) -}}
{{- fail "chrony.enabled needs hostNetwork: true to reach udp://127.0.0.1:323 on the node, or chrony.socketDir" -}}
{{- end -}}
{{- if and .Values.chrony.socketDir (kindIs "invalid" .Values.chrony.runAsUser) -}}
{{- fail "chrony.socketDir needs chrony.runAsUser: the uid of chronyd on the nodes (e.g. id -u _chrony), the only user allowed to write in its socket directory" -}}
{{- end -}}
{{- end -}}
{{- if and (include "ntp-exporter.probeEndpointEnabled" .) (eq (include "ntp-exporter.mode" .) "agent") .Values.hostNetwork -}}
{{- fail "probeEndpoint.enabled with hostNetwork exposes /probe on every node IP: disable one of them in agent mode" -}}
{{- end -}}
{{- end -}}

{{/*
Pod annotations: the prometheus.io scrape annotations, unless a ServiceMonitor
already scrapes the pods (both would collect every series twice)
*/}}
{{- define "ntp-exporter.podAnnotations" -}}
{{- $annotations := dict -}}
{{- if and .Values.scrapeAnnotations (not .Values.serviceMonitor.enabled) -}}
{{- $_ := set $annotations "prometheus.io/scrape" "true" -}}
{{- $_ := set $annotations "prometheus.io/port" (toString (.Values.config.metricsPort | default 9559)) -}}
{{- $_ := set $annotations "prometheus.io/path" "/metrics" -}}
{{- end -}}
{{- with .Values.podAnnotations -}}
{{- $annotations = merge (deepCopy .) $annotations -}}
{{- end -}}
{{- with $annotations -}}
{{- toYaml . -}}
{{- end -}}
{{- end -}}

{{/*
DNS policy: a pod on the host network needs ClusterFirstWithHostNet to keep
resolving through the cluster DNS
*/}}
{{- define "ntp-exporter.dnsPolicy" -}}
{{- if and .Values.hostNetwork (eq .Values.dnsPolicy "ClusterFirst") -}}
ClusterFirstWithHostNet
{{- else -}}
{{ .Values.dnsPolicy }}
{{- end -}}
{{- end -}}

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
Return the appropriate apiVersion for NetworkPolicy
*/}}
{{- define "ntp-exporter.networkPolicy.apiVersion" -}}
{{- if semverCompare ">=1.7-0" .Capabilities.KubeVersion.GitVersion -}}
networking.k8s.io/v1
{{- else -}}
extensions/v1beta1
{{- end -}}
{{- end -}}

{{/*
Return the appropriate apiVersion for RBAC
*/}}
{{- define "ntp-exporter.rbac.apiVersion" -}}
{{- if .Capabilities.APIVersions.Has "rbac.authorization.k8s.io/v1" -}}
rbac.authorization.k8s.io/v1
{{- else -}}
rbac.authorization.k8s.io/v1beta1
{{- end -}}
{{- end -}}

{{/*
Validate mode value
*/}}
{{- define "ntp-exporter.validateMode" -}}
{{- if and (ne .Values.mode "agent") (ne .Values.mode "probe") (ne .Values.mode "hybrid") -}}
{{- fail "mode must be 'agent', 'probe', or 'hybrid'" -}}
{{- end -}}
{{- end -}}

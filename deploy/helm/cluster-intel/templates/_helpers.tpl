{{/*
Expand the name of the chart.
*/}}
{{- define "cluster-intel.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
Truncated to 63 chars per the DNS naming spec.
*/}}
{{- define "cluster-intel.fullname" -}}
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
Chart name + version for the chart label.
*/}}
{{- define "cluster-intel.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels applied to every resource.
*/}}
{{- define "cluster-intel.labels" -}}
helm.sh/chart: {{ include "cluster-intel.chart" . }}
{{ include "cluster-intel.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: cluster-intel
{{- end }}

{{/*
Selector labels (used in both Deployment selectors and Service selectors).
*/}}
{{- define "cluster-intel.selectorLabels" -}}
app.kubernetes.io/name: {{ include "cluster-intel.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/* ===================================================================
     Store / bus endpoint helpers — resolve bundled vs external
     =================================================================== */}}

{{/*
Postgres host: bundled chart service or external host.
*/}}
{{- define "cluster-intel.postgres.host" -}}
{{- if or .Values.stores.postgres.bundled .Values.stores.postgres.standalone -}}
{{ include "cluster-intel.fullname" . }}-postgresql.{{ .Release.Namespace }}.svc.cluster.local
{{- else -}}
{{ .Values.stores.postgres.external.host }}
{{- end -}}
{{- end }}

{{/*
Postgres port.
*/}}
{{- define "cluster-intel.postgres.port" -}}
{{- if or .Values.stores.postgres.bundled .Values.stores.postgres.standalone -}}
5432
{{- else -}}
{{ .Values.stores.postgres.external.port }}
{{- end -}}
{{- end }}

{{/*
Redis address: bundled chart, standalone StatefulSet, or external addr.
*/}}
{{- define "cluster-intel.redis.addr" -}}
{{- if or .Values.stores.redis.bundled .Values.stores.redis.standalone -}}
{{ include "cluster-intel.fullname" . }}-redis.{{ .Release.Namespace }}.svc.cluster.local:6379
{{- else -}}
{{ .Values.stores.redis.external.addr }}
{{- end -}}
{{- end }}

{{/*
NATS URL: bundled chart service or external URL.
*/}}
{{- define "cluster-intel.nats.url" -}}
{{- if .Values.bus.nats.bundled -}}
nats://{{ .Release.Name }}-nats.{{ .Release.Namespace }}.svc.cluster.local:4222
{{- else -}}
{{ .Values.bus.nats.external.url }}
{{- end -}}
{{- end }}

{{/* ===================================================================
     Pod/container helpers shared across deployments
     =================================================================== */}}

{{/*
Merged container-level securityContext (spec.containers[].securityContext).
fsGroup is NOT valid here; use cluster-intel.podSecurityContext instead.
*/}}
{{- define "cluster-intel.securityContext" -}}
{{- $merged := mustMergeOverwrite (deepCopy .top) .override -}}
{{- toYaml $merged -}}
{{- end }}

{{/*
Merged pod-level securityContext (spec.securityContext).
Contains runAsNonRoot, runAsUser, runAsGroup, fsGroup, seccompProfile.
*/}}
{{- define "cluster-intel.podSecurityContext" -}}
{{- $merged := mustMergeOverwrite (deepCopy .top) .override -}}
{{- toYaml $merged -}}
{{- end }}

{{/*
Affinity block — uses component override if non-empty, otherwise the
top-level affinity.
*/}}
{{- define "cluster-intel.affinity" -}}
{{- if . -}}
{{- toYaml . -}}
{{- end -}}
{{- end }}

{{/*
Namespace the monitoring stack deploys into. Defaults to
`monitoring.namespace` (from values.yaml), falls back to the release
namespace.
*/}}
{{- define "cluster-intel.monitoringNamespace" -}}
{{- default .Release.Namespace .Values.monitoring.namespace -}}
{{- end }}

{{/*
Component selector labels — selectorLabels plus component tag. Used by
NetworkPolicy peer selectors.
*/}}
{{- define "cluster-intel.componentSelectorLabels" -}}
{{ include "cluster-intel.selectorLabels" . }}
app.kubernetes.io/component: {{ .component }}
{{- end }}

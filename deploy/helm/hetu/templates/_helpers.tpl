{{/*
Required-value validation. Called from configmap.yaml so it runs on every
helm install / upgrade / template. Uses fail/required so the command exits
with a clear message before any resources are applied.
*/}}
{{- define "hetu.validate" -}}

{{- /* cluster.id ------------------------------------------------------- */}}
{{- if not .Values.cluster.id -}}
{{- fail "\n\n[hetu] cluster.id is required.\n  --set cluster.id=<unique-name>   (e.g. prod-eu-1, homelab)\n" -}}
{{- end -}}

{{- /* llm.model --------------------------------------------------------- */}}
{{- if not .Values.llm.model -}}
{{- fail "\n\n[hetu] llm.model is required.\n  --set llm.model=<model>   (e.g. llama3.2, Qwen2.5-Coder:14B-Instruct, claude-sonnet-4-6)\n" -}}
{{- end -}}

{{- /* llm.endpoint — required for self-hosted providers --------------- */}}
{{- if and (not (eq .Values.llm.provider "anthropic")) (not .Values.llm.endpoint) -}}
{{- fail (printf "\n\n[hetu] llm.endpoint is required when provider=%q.\n  --set llm.endpoint=http://<host>:<port>\n" .Values.llm.provider) -}}
{{- end -}}

{{- /* dashboard NodePort value ----------------------------------------- */}}
{{- if and (eq (.Values.dashboard.service.type | default "ClusterIP") "NodePort") (not .Values.dashboard.service.nodePort) -}}
{{- fail "\n\n[hetu] dashboard.service.nodePort is required when dashboard.service.type=NodePort.\n  --set dashboard.service.nodePort=<30000-32767>\n" -}}
{{- end -}}

{{- end -}}

{{/*
Cluster display name — falls back to cluster.id when displayName is empty.
*/}}
{{- define "hetu.displayName" -}}
{{- .Values.cluster.displayName | default .Values.cluster.id -}}
{{- end -}}

{{/*
Expand the name of the chart.
*/}}
{{- define "hetu.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
Truncated to 63 chars per the DNS naming spec.
*/}}
{{- define "hetu.fullname" -}}
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
{{- define "hetu.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels applied to every resource.
*/}}
{{- define "hetu.labels" -}}
helm.sh/chart: {{ include "hetu.chart" . }}
{{ include "hetu.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: hetu
{{- end }}

{{/*
Selector labels (used in both Deployment selectors and Service selectors).
*/}}
{{- define "hetu.selectorLabels" -}}
app.kubernetes.io/name: {{ include "hetu.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/* ===================================================================
     Store / bus endpoint helpers — resolve bundled vs external
     =================================================================== */}}

{{/*
Postgres host: bundled chart service or external host.
*/}}
{{- define "hetu.postgres.host" -}}
{{- if or .Values.stores.postgres.bundled .Values.stores.postgres.standalone -}}
{{ include "hetu.fullname" . }}-postgresql.{{ .Release.Namespace }}.svc.cluster.local
{{- else -}}
{{ .Values.stores.postgres.external.host }}
{{- end -}}
{{- end }}

{{/*
Postgres port.
*/}}
{{- define "hetu.postgres.port" -}}
{{- if or .Values.stores.postgres.bundled .Values.stores.postgres.standalone -}}
5432
{{- else -}}
{{ .Values.stores.postgres.external.port }}
{{- end -}}
{{- end }}

{{/*
Redis address: bundled chart, standalone StatefulSet, or external addr.
*/}}
{{- define "hetu.redis.addr" -}}
{{- if or .Values.stores.redis.bundled .Values.stores.redis.standalone -}}
{{ include "hetu.fullname" . }}-redis.{{ .Release.Namespace }}.svc.cluster.local:6379
{{- else -}}
{{ .Values.stores.redis.external.addr }}
{{- end -}}
{{- end }}

{{/*
NATS URL: bundled chart service or external URL.
*/}}
{{- define "hetu.nats.url" -}}
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
fsGroup is NOT valid here; use hetu.podSecurityContext instead.
*/}}
{{- define "hetu.securityContext" -}}
{{- $merged := mustMergeOverwrite (deepCopy .top) .override -}}
{{- toYaml $merged -}}
{{- end }}

{{/*
Merged pod-level securityContext (spec.securityContext).
Contains runAsNonRoot, runAsUser, runAsGroup, fsGroup, seccompProfile.
*/}}
{{- define "hetu.podSecurityContext" -}}
{{- $merged := mustMergeOverwrite (deepCopy .top) .override -}}
{{- toYaml $merged -}}
{{- end }}

{{/*
Affinity block — uses component override if non-empty, otherwise the
top-level affinity.
*/}}
{{- define "hetu.affinity" -}}
{{- if . -}}
{{- toYaml . -}}
{{- end -}}
{{- end }}

{{/*
Namespace the monitoring stack deploys into. Defaults to
`monitoring.namespace` (from values.yaml), falls back to the release
namespace.
*/}}
{{- define "hetu.monitoringNamespace" -}}
{{- default .Release.Namespace .Values.monitoring.namespace -}}
{{- end }}

{{/*
Component selector labels — selectorLabels plus component tag. Used by
NetworkPolicy peer selectors.
*/}}
{{- define "hetu.componentSelectorLabels" -}}
{{ include "hetu.selectorLabels" . }}
app.kubernetes.io/component: {{ .component }}
{{- end }}

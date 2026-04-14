# cluster-intel Helm chart

Kubernetes Cluster Intelligence Engine — Go collector, Go analyzer,
Next.js dashboard, with optional Tempo / OTEL / Grafana observability.

Chart version: `0.2.0` · appVersion: `7.0.0`

## Install

```bash
helm dependency build .
helm upgrade --install cluster-intel . \
  --namespace cluster-intel --create-namespace
```

With a production override:

```bash
helm upgrade --install cluster-intel . \
  --namespace cluster-intel --create-namespace \
  -f ../../../values-deploy.yaml
```

See [../../../docs/MIGRATION.md](../../../docs/MIGRATION.md) if you're
migrating from the pre-0.2.0 kustomize manifests.

## Values reference

### Global

| Key | Default | Description |
|---|---|---|
| `global.namespace` | `cluster-intel` | Namespace the chart installs into (also used by `global.createNamespace`) |
| `global.createNamespace` | `true` | Render a `Namespace` resource |
| `global.imagePullSecrets` | `[]` | Pull secrets for private registries |

### Identity

| Key | Default | Description |
|---|---|---|
| `cluster.id` | `default` | Cluster identifier embedded in telemetry |
| `cluster.displayName` | `default` | Human-readable cluster name |

### Per-component (`collector` / `analyzer` / `dashboard`)

Each component block takes the same shape:

| Key | Default (collector) | Default (analyzer) | Default (dashboard) |
|---|---|---|---|
| `image.repository` | `cluster-intel-collector` | `cluster-intel-analyzer` | `cluster-intel-dashboard` |
| `image.tag` | `latest` | `latest` | `latest` |
| `image.pullPolicy` | `IfNotPresent` | `IfNotPresent` | `IfNotPresent` |
| `replicas` | `1` | `1` | `1` |
| `resources.requests` | 100m / 128Mi | 200m / 256Mi | 100m / 128Mi |
| `resources.limits` | 500m / 256Mi | 1 / 512Mi | 500m / 256Mi |
| `env` | `[]` | `[]` | `[]` |
| `securityContext` | `{}` (inherits top) | `{}` | `{readOnlyRootFilesystem: false, runAsUser: 1001, runAsGroup: 1001, fsGroup: 1001}` |
| `affinity` | `{}` (inherits top) | `{}` | `{}` |
| `topologySpreadConstraints` | `[]` (inherits top) | `[]` | `[]` |
| `pdb.enabled` | `false` | `false` | `false` |
| `pdb.minAvailable` | `1` | `1` | `1` |

Collector-only:

| Key | Default | Description |
|---|---|---|
| `collector.prometheusUrl` | `http://prometheus-server.monitoring.svc.cluster.local:9090` | Prometheus the collector scrapes |

### Top-level pod security + scheduling

| Key | Default | Description |
|---|---|---|
| `securityContext.runAsNonRoot` | `true` | |
| `securityContext.runAsUser` | `65534` | nobody |
| `securityContext.runAsGroup` | `65534` | |
| `securityContext.fsGroup` | `65534` | |
| `securityContext.seccompProfile.type` | `RuntimeDefault` | |
| `securityContext.allowPrivilegeEscalation` | `false` | |
| `securityContext.readOnlyRootFilesystem` | `true` | overridden to `false` for dashboard |
| `securityContext.capabilities.drop` | `["ALL"]` | |
| `affinity.podAntiAffinity…` | prefer different nodes | |
| `topologySpreadConstraints` | zone maxSkew 1 | |

### Stores

| Key | Default | Description |
|---|---|---|
| `stores.postgres.bundled` | `true` | Use bundled Bitnami Postgres |
| `stores.postgres.external.*` | (unset) | host/port/db/user/existingSecret when `bundled: false` |
| `stores.postgres.sslMode` | `disable` | |
| `stores.redis.bundled` | `true` | Use bundled Bitnami Redis |
| `stores.redis.external.*` | (unset) | addr/existingSecret when `bundled: false` |
| `bus.nats.bundled` | `true` | Use bundled NATS chart |
| `bus.nats.external.*` | (unset) | url/existingSecret when `bundled: false` |

(ClickHouse was removed in 0.2.0 — no template referenced it. Re-add when a CH-backed feature ships.)

### LLM

| Key | Default | Description |
|---|---|---|
| `llm.provider` | `anthropic` | `anthropic` / `openai` / `ollama` / `vllm` / `llamacpp` / `azure` / `bedrock` / `none` |
| `llm.endpoint` | `""` | Required for non-default providers |
| `llm.model` | `claude-sonnet-4-6` | |
| `llm.apiKeySecret.name` | `""` | Existing K8s secret with the API key |
| `llm.apiKeySecret.key` | `api-key` | Key inside the secret |
| `llm.maxTokens` | `4096` | |
| `llm.temperature` | `0.2` | |
| `llm.dailyTokenBudget` | `1000000` | |

### Safety rails

| Key | Default | Description |
|---|---|---|
| `writeActions.enabled` | `false` | Allows analyzer to mutate K8s (scale / restart / delete) |
| `exec.enabled` | `false` | Allows `kubectl exec`-style streams from dashboard |
| `protectedNamespaces` | `[kube-system, kube-public, kube-node-lease]` | Analyzer never mutates these |

### Ingress

| Key | Default | Description |
|---|---|---|
| `ingress.enabled` | `false` | |
| `ingress.className` | `""` | e.g. `nginx` |
| `ingress.host` | `cluster-intel.example.com` | |
| `ingress.annotations` | `{}` | |
| `ingress.tls` | `[]` | List of `{secretName, hosts}` |

### Prometheus ServiceMonitor (for the app's own metrics)

| Key | Default | Description |
|---|---|---|
| `serviceMonitor.enabled` | `false` | Renders ServiceMonitors for collector + analyzer |

### NetworkPolicy (zero-trust, opt-in)

| Key | Default | Description |
|---|---|---|
| `networkPolicy.enabled` | **`false`** | Install 7 NetworkPolicies (default-deny + per-component) |
| `networkPolicy.monitoringNamespace` | `monitoring` | Peer ns for Prometheus scraping |
| `networkPolicy.ingressNamespace` | `ingress-nginx` | Peer ns for Ingress controller |
| `networkPolicy.llmEgressCIDR` | `""` | e.g. `10.0.0.50/32` for self-hosted LLM |
| `networkPolicy.llmEgressPort` | `11434` | |

### Logging

| Key | Default | Description |
|---|---|---|
| `logging.level` | `info` | |
| `logging.format` | `json` | |

### Observability stack (opt-in)

Gated by `monitoring.enabled: false` at top. Each sub-block has its own toggle.

| Key | Default | Description |
|---|---|---|
| `monitoring.enabled` | **`false`** | Master switch for every `monitoring.*` template |
| `monitoring.namespace` | `monitoring` | Namespace to deploy Tempo / OTEL / alerts into |

**Tempo**

| Key | Default |
|---|---|
| `monitoring.tempo.enabled` | `true` (when `monitoring.enabled`) |
| `monitoring.tempo.image.repository` | `grafana/tempo` |
| `monitoring.tempo.image.tag` | `2.3.1` |
| `monitoring.tempo.retention` | `48h` |
| `monitoring.tempo.persistence.enabled` | `false` |
| `monitoring.tempo.persistence.size` | `10Gi` |
| `monitoring.tempo.resources.*` | 100m / 256Mi req, 500m / 1Gi lim |

**OpenTelemetry Collector**

| Key | Default |
|---|---|
| `monitoring.otel.enabled` | `true` |
| `monitoring.otel.image.repository` | `otel/opentelemetry-collector-contrib` |
| `monitoring.otel.image.tag` | `0.92.0` |
| `monitoring.otel.memoryLimit` | `512Mi` |

**Ollama ServiceMonitor + external endpoint shim**

| Key | Default | Notes |
|---|---|---|
| `monitoring.ollama.enabled` | `true` | Template fails fast if `externalIP` is empty |
| `monitoring.ollama.externalIP` | `""` | **required** when enabled |
| `monitoring.ollama.port` | `11434` | |
| `monitoring.ollama.namespace` | `ai-system` | Created if absent |

**PrometheusRule alerts**

| Key | Default | Notes |
|---|---|---|
| `monitoring.alerts.enabled` | `true` | 14 Ollama + 4 LLM application alerts |
| `monitoring.alerts.extraRules` | `[]` | Appended as a third group `cluster-intel-extra.rules` |

**AlertManager Slack routing**

| Key | Default | Notes |
|---|---|---|
| `monitoring.alertmanager.enabled` | `false` | Separate toggle because it needs a Slack webhook |
| `monitoring.alertmanager.slackWebhookSecret.name` | `""` | **required** when enabled; pre-create this secret |
| `monitoring.alertmanager.slackWebhookSecret.key` | `slack-webhook-url` | |
| `monitoring.alertmanager.channels.critical` | `#k8s-critical` | |
| `monitoring.alertmanager.channels.warning` | `#k8s-alerts` | |
| `monitoring.alertmanager.channels.ollama` | `#ollama-alerts` | |
| `monitoring.alertmanager.channels.llm` | `#llm-alerts` | |

**Grafana**

| Key | Default | Notes |
|---|---|---|
| `monitoring.grafana.dashboards.clusterIntel` | `true` | Health scores, pod health, security, resources |
| `monitoring.grafana.dashboards.ollama` | `true` | |
| `monitoring.grafana.dashboards.llmTraces` | `true` | Requires Tempo |
| `monitoring.grafana.tempoDatasourceUid` | `tempo` | |
| `monitoring.grafana.prometheusDatasourceUid` | `prometheus` | |

## Kubernetes kinds rendered

| Resource | When |
|---|---|
| `Namespace` | `global.createNamespace: true` |
| `ServiceAccount` | always |
| `ClusterRole`/`ClusterRoleBinding` (reader) | always |
| `ClusterRole`/`ClusterRoleBinding` (writer) | `writeActions.enabled: true` |
| `ConfigMap` (app config) | always |
| `Deployment` × 3 | always — collector, analyzer, dashboard |
| `Service` × 3 | always |
| `Ingress` | `ingress.enabled: true` |
| `ServiceMonitor` × 2 | `serviceMonitor.enabled: true` |
| `PodDisruptionBudget` × N | per-component `pdb.enabled: true` |
| `NetworkPolicy` × 7 | `networkPolicy.enabled: true` |
| Tempo `ConfigMap` / `Deployment` / `Service` / (optional) `PVC` | `monitoring.enabled && monitoring.tempo.enabled` |
| OTEL `ConfigMap` / `Deployment` / `Service` | `monitoring.enabled && monitoring.otel.enabled` |
| Ollama `Namespace` / `ServiceMonitor` / `Endpoints` / `Service` | `monitoring.enabled && monitoring.ollama.enabled` |
| `PrometheusRule` (Ollama + LLM) | `monitoring.enabled && monitoring.alerts.enabled` |
| AlertManager `ConfigMap` | `monitoring.enabled && monitoring.alertmanager.enabled` |
| Grafana Tempo datasource `ConfigMap` | `monitoring.enabled && monitoring.tempo.enabled` |
| Grafana dashboard `ConfigMap` × up to 3 | `monitoring.enabled && monitoring.grafana.dashboards.*: true` |

## Templates

```
templates/
├── _helpers.tpl                 # naming, labels, store endpoint resolvers, securityContext merge
├── namespace.yaml               # gated by global.createNamespace
├── rbac.yaml                    # SA + ClusterRole(s) + ClusterRoleBinding(s)
├── configmap.yaml               # main app config
├── collector.yaml               # Deployment
├── analyzer.yaml                # Deployment
├── dashboard.yaml               # Deployment
├── services.yaml                # 3 ClusterIP services
├── ingress.yaml                 # gated by ingress.enabled
├── servicemonitor.yaml          # gated by serviceMonitor.enabled
├── poddisruptionbudget.yaml     # gated per-component
├── networkpolicy.yaml           # gated by networkPolicy.enabled (7 policies)
└── monitoring/
    ├── tempo-deployment.yaml
    ├── otel-collector.yaml
    ├── ollama-servicemonitor.yaml
    ├── prometheus-rules.yaml
    ├── alertmanager-config.yaml
    ├── grafana-datasource.yaml
    └── grafana-dashboards.yaml

dashboards/
├── cluster-intel.json
├── ollama.json
└── llm-traces.json
```

## Fail-fast guards

The chart refuses to render in a broken state:

| Condition | Error |
|---|---|
| `monitoring.ollama.enabled=true` with empty `externalIP` | `monitoring.ollama.enabled=true requires monitoring.ollama.externalIP to be set` |
| `monitoring.alertmanager.enabled=true` with empty `slackWebhookSecret.name` | `monitoring.alertmanager.enabled=true requires monitoring.alertmanager.slackWebhookSecret.name …` |

## Verification

```bash
helm lint .
helm dependency build .
helm template test . > /dev/null         # render everything at defaults
helm template test . --set monitoring.enabled=true \
                     --set monitoring.ollama.externalIP=10.0.0.1 > /dev/null
```

If any of those print an error, the chart is broken.

## Uninstall

```bash
helm uninstall cluster-intel -n cluster-intel
# If you also want to drop the data volumes from bundled Postgres/Redis/NATS:
kubectl delete pvc -n cluster-intel -l app.kubernetes.io/instance=cluster-intel
```

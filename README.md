# K8s Cluster Intelligence Engine v6.0

A production-ready Kubernetes security, compliance, and health monitoring platform with AI-powered insights.

![Version](https://img.shields.io/badge/version-6.0.0-blue)
![License](https://img.shields.io/badge/license-MIT-green)
![Kubernetes](https://img.shields.io/badge/kubernetes-1.25+-blue)

## Features

### Security & Compliance
- **Vulnerability Scanning**: Trivy Operator integration for CVE detection
- **CIS Kubernetes Benchmark**: 25+ security controls validated
- **Pod Security Standards**: Privileged, Baseline, Restricted validation
- **RBAC Analysis**: Over-privileged role detection
- **Secret Scanning**: Detect exposed secrets in ConfigMaps/env vars
- **Network Policy Audit**: Identify unprotected namespaces

### Observability
- **Health Scoring**: Overall, Reliability, Security, Cost, Architecture scores
- **Prometheus Integration**: Real-time CPU/Memory metrics
- **Historical Trends**: Score and vulnerability tracking over time
- **Resource Utilization**: Right-sizing recommendations

### Operations
- **Real-time Dashboard**: Modern web UI with dark/light themes
- **Alert Webhooks**: Slack, Discord, Teams, PagerDuty integration
- **Export Reports**: JSON, CSV, PDF compliance reports
- **Multi-cluster Ready**: Aggregate views across clusters

### Pod Health Management (Coming Soon)
- **Non-Running Pod Detection**: Evicted, Failed, Pending, CrashLoop, Stuck
- **Root Cause Analysis**: Automatic diagnosis with recommendations
- **One-Click Remediation**: Delete evicted, restart deployments, force delete
- **Auto-Cleanup**: Configurable automated cleanup of safe-to-delete pods
- **Action Logging**: Full audit trail of all remediation actions

See [Pod Health Management Design Doc](docs/POD_HEALTH_MANAGEMENT.md) for details.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                     K8s Cluster Intelligence                     │
├─────────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐          │
│  │   Collector  │  │   Analyzer   │  │   Dashboard  │          │
│  │              │  │              │  │              │          │
│  │ • K8s API    │  │ • Scoring    │  │ • Web UI     │          │
│  │ • Trivy CRDs │  │ • CIS Checks │  │ • REST API   │          │
│  │ • Prometheus │  │ • Alerts     │  │ • WebSocket  │          │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘          │
│         │                 │                 │                   │
│         └─────────────────┴─────────────────┘                   │
│                           │                                     │
│                    ┌──────┴──────┐                              │
│                    │   Storage   │                              │
│                    │  (SQLite)   │                              │
│                    └─────────────┘                              │
└─────────────────────────────────────────────────────────────────┘
         │                    │                    │
         ▼                    ▼                    ▼
┌─────────────┐      ┌─────────────┐      ┌─────────────┐
│ Kubernetes  │      │   Trivy     │      │ Prometheus  │
│    API      │      │  Operator   │      │   Server    │
└─────────────┘      └─────────────┘      └─────────────┘
```

## Quick Start

### Prerequisites
- Kubernetes cluster (1.25+)
- `kubectl` configured
- `helm` 3.x
- ~512MB available memory (more with bundled Postgres / Redis / NATS)

### Install (Helm only)

All deployments go through the Helm chart at
[`deploy/helm/cluster-intel/`](deploy/helm/cluster-intel/). There is no
`deploy.sh` or `kubectl apply` flow anymore — see
[docs/MIGRATION.md](docs/MIGRATION.md) if you're coming from a pre-0.2.0
install.

```bash
git clone https://github.com/hellodk/k8s-cluster-health.git
cd k8s-cluster-health
make helm-deps      # downloads Postgres / Redis / NATS sub-charts
make helm-deploy    # = helm upgrade --install cluster-intel …
```

Or, directly with Helm:

```bash
helm dependency build deploy/helm/cluster-intel/
helm upgrade --install cluster-intel deploy/helm/cluster-intel/ \
  --namespace cluster-intel --create-namespace
```

### Production override

```bash
helm upgrade --install cluster-intel deploy/helm/cluster-intel/ \
  --namespace cluster-intel --create-namespace \
  -f values-deploy.yaml
```

### Environments (dev / uat / prod)

This repo supports environment-specific Helm values:

- `values-dev.yaml`
- `values-uat.yaml`
- `values-prod.yaml` (create locally from `values-prod.yaml.example`)

Deploy with:

```bash
make helm-deploy ENV=dev
make helm-deploy ENV=uat
make helm-deploy ENV=prod   # expects values-prod.yaml
```

You can also override the namespace:

```bash
make helm-deploy ENV=uat NAMESPACE=cluster-intel-uat
```

### Optional hardening / observability

```bash
--set networkPolicy.enabled=true            # zero-trust network policies
--set collector.pdb.enabled=true \
--set analyzer.pdb.enabled=true \
--set dashboard.pdb.enabled=true            # pod disruption budgets
--set monitoring.enabled=true \
--set monitoring.ollama.externalIP=10.0.0.50   # Tempo + OTEL + alerts + dashboards
```

Full values reference:
[`deploy/helm/cluster-intel/README.md`](deploy/helm/cluster-intel/README.md).

### Access the dashboard

```bash
kubectl port-forward svc/cluster-intel-dashboard 3000:3000 \
  -n cluster-intel
# → http://localhost:3000
```

## Environments (dev / uat / prod)

`scripts/run-local.sh` is the operator-facing tool for **everything
local**. It loads `env/<ENVIRONMENT>.env`, **prompts for every
configurable value** (showing the env-file default in `[brackets]`),
**validates each one** (port collisions, URL syntax, kubeconfig
reachability, Go duration syntax, LLM provider whitelist), and only
then starts the services.

### Sub-commands

```bash
scripts/run-local.sh                       # interactive menu
scripts/run-local.sh start                 # interactive: prompt + validate + start
scripts/run-local.sh start --yes -e dev    # accept env-file defaults silently
scripts/run-local.sh start --non-interactive --yes -e prod   # CI: zero prompts
scripts/run-local.sh stop                  # kill via pidfile
scripts/run-local.sh status                # tabular health
scripts/run-local.sh restart -e uat
scripts/run-local.sh logs [analyzer|dashboard|collector|all]
scripts/run-local.sh build                 # rebuild the analyzer binary
scripts/run-local.sh setup -e dev          # wizard: write env/dev.env
scripts/run-local.sh doctor                # pre-flight (tools, ports, kubeconfig) — never mutates
scripts/run-local.sh lint                  # validate the env file only
scripts/run-local.sh version
```

### Convenience targets

```bash
make doctor                                # delegate
make run    ENV=dev                        # = scripts/run-local.sh start --yes -e dev
make stop
make status
```

### Flags

| Flag | Purpose |
|---|---|
| `-e ENV`, `--env ENV` | Pick `env/<ENV>.env` (defaults to `dev`) |
| `-y`, `--yes` | Accept all defaults silently — no prompts |
| `--non-interactive` | Hard-fail on any required value missing — for CI |
| `--env-file PATH` | Override env-file path |
| `--log-dir PATH` | Override log directory |

### Validation policy

| Severity | What it catches |
|---|---|
| **Hard-fail** | Port already bound, missing kubeconfig (live mode), bad LLM provider, port not in 1..65535, missing required value in `--non-interactive` |
| **Warn** | URL not reachable (network may be transient), empty `COLLECTOR_URL` in live mode |

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `CLUSTER_ID` | `default` | Cluster identifier |
| `NAMESPACE` | `cluster-intel` | Deployment namespace |
| `ANALYSIS_INTERVAL` | `60` | Seconds between scans |
| `PROMETHEUS_URL` | `` | Prometheus server URL |
| `ALERT_WEBHOOK_URL` | `` | Webhook for alerts |
| `ALERT_THRESHOLD` | `50` | Score threshold for alerts |
| `HISTORY_RETENTION_DAYS` | `30` | Days to keep history |
| `THEME` | `light` | Default theme (light/dark) |

### Alert Configuration

```yaml
# ConfigMap: cluster-intel-config
alerts:
  enabled: true
  webhook_url: "https://hooks.slack.com/services/..."
  threshold: 50
  channels:
    - type: slack
      url: "https://hooks.slack.com/..."
    - type: discord
      url: "https://discord.com/api/webhooks/..."
    - type: pagerduty
      routing_key: "your-routing-key"
```

### Prometheus Integration

If Prometheus is available, metrics are automatically collected:

```yaml
env:
  - name: PROMETHEUS_URL
    value: "http://prometheus-server.monitoring:80"
```

Collected metrics:
- `container_cpu_usage_seconds_total`
- `container_memory_usage_bytes`
- `kube_pod_container_resource_requests`
- `kube_pod_container_resource_limits`

## CIS Kubernetes Benchmark

Checks implemented (v1.8.0):

| ID | Title | Severity |
|----|-------|----------|
| 1.2.1 | Ensure anonymous-auth is disabled | Critical |
| 1.2.6 | Ensure kubelet-certificate-authority is set | High |
| 2.1 | Ensure etcd encryption is enabled | Critical |
| 4.2.1 | Ensure kubelet anonymous-auth is disabled | High |
| 5.1.1 | Minimize cluster-admin role usage | High |
| 5.1.3 | Minimize wildcard use in Roles | Medium |
| 5.1.6 | Ensure ServiceAccount tokens are not auto-mounted | Medium |
| 5.2.1 | Minimize privileged containers | Critical |
| 5.2.2 | Minimize allowPrivilegeEscalation | High |
| 5.2.3 | Minimize root containers | High |
| 5.2.4 | Minimize hostNetwork containers | High |
| 5.2.5 | Minimize hostPID containers | High |
| 5.2.6 | Minimize hostIPC containers | Medium |
| 5.2.7 | Minimize hostPath volumes | High |
| 5.2.8 | Minimize hostPort usage | Medium |
| 5.2.9 | Ensure AppArmor is set | Low |
| 5.2.10 | Ensure Seccomp is set | Medium |
| 5.2.11 | Minimize capabilities | Medium |
| 5.2.12 | Ensure readOnlyRootFilesystem | Low |
| 5.3.1 | Ensure NetworkPolicies are defined | Medium |
| 5.4.1 | Prefer secrets as files over env vars | Medium |
| 5.4.2 | Consider external secret storage | Low |
| 5.7.1 | Create namespace boundaries | Medium |
| 5.7.2 | Ensure securityContext is set | Medium |
| 5.7.3 | Apply default deny NetworkPolicy | Medium |

## API Reference

### Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/` | Dashboard UI |
| GET | `/healthz` | Health check |
| GET | `/readyz` | Readiness check |
| GET | `/api/v1/health` | Full cluster health report |
| GET | `/api/v1/scores` | Current scores only |
| GET | `/api/v1/vulns` | Vulnerability list |
| GET | `/api/v1/cis` | CIS benchmark results |
| GET | `/api/v1/pods` | All pods with status |
| GET | `/api/v1/history` | Historical score data |
| GET | `/api/v1/export?format=json` | Export report |
| POST | `/api/v1/scan` | Trigger manual scan |
| POST | `/api/v1/alerts/test` | Test alert webhook |

### Response Format

```json
{
  "clusterId": "production",
  "timestamp": "2026-02-14T12:00:00Z",
  "scores": {
    "overall": 75,
    "reliability": 85,
    "security": 60,
    "cost": 80,
    "architecture": 70
  },
  "summary": {
    "totalPods": 45,
    "healthyPods": 42,
    "totalVulns": 23,
    "cisPassed": 18,
    "cisFailed": 7
  },
  "vulnerabilities": {...},
  "cisBenchmark": {...},
  "topIssues": [...]
}
```

## Scoring Methodology

### Overall Score (0-100)
overall = reliability × 0.35 + security × 0.30 + cost × 0.20 + architecture × 0.15

### Component Scores

**Reliability**
- Base: 100
- Deductions:
  - CrashLoopBackOff: -10 per pod
  - High restarts (>5): -3 per pod
  - Pending pods: -5 per pod
  - Warning events: -1 per event

**Security**
- Base: 100
- Deductions:
  - Critical CVE: -5 each
  - High CVE: -2 each
  - CIS FAIL: -8 each
  - CIS WARN: -3 each
  - Privileged container: -10 each

**Cost**
- Base: 70 (default, requires Prometheus for accuracy)
- Adjustments based on:
  - CPU utilization efficiency
  - Memory utilization efficiency
  - Idle workloads
  - Oversized requests

**Architecture**
- Base: 85
- Deductions:
  - Missing resource limits: -2 per container
  - Missing probes: -1 per container
  - Single replica deployments: -3 each
  - Missing PodDisruptionBudget: -2 each

## Troubleshooting

### Dashboard not loading
```bash
# Check pod status
kubectl get pods -n cluster-intel

# Check logs
kubectl logs -n cluster-intel -l app=cluster-intel

# Verify service
kubectl get svc -n cluster-intel
```

### No vulnerability data
```bash
# Check if Trivy Operator is installed
kubectl get pods -n trivy-system

# Check for VulnerabilityReports
kubectl get vulnerabilityreports -A

# Fallback scanning is used if Trivy is not present
```

### Prometheus not connected
```bash
# Verify Prometheus URL is accessible
kubectl exec -n cluster-intel deploy/cluster-intel -- \
  curl -s http://prometheus-server.monitoring:80/api/v1/status/config
```

### Alerts not firing
```bash
# Test webhook manually
kubectl exec -n cluster-intel deploy/cluster-intel -- \
  python -c "import urllib.request; urllib.request.urlopen('YOUR_WEBHOOK_URL', b'{\"test\":true}')"
```

## Development

### Local Development
```bash
# Run locally with kubeconfig
export KUBECONFIG=~/.kube/config
python app.py

# Run with Docker
docker build -t cluster-intel:dev .
docker run -p 8080:8080 -v ~/.kube:/root/.kube cluster-intel:dev
```

### Running Tests
```bash
# Unit tests
python -m pytest tests/

# Integration tests (requires cluster)
./tests/integration.sh
```

## Security Considerations

- **RBAC**: Uses minimal ClusterRole with read-only access
- **Network**: Supports NetworkPolicy for pod isolation
- **Secrets**: No secrets stored; uses ServiceAccount tokens
- **TLS**: Supports TLS termination via Ingress

## Contributing

1. Fork the repository
2. Create feature branch (`git checkout -b feature/amazing`)
3. Commit changes (`git commit -m 'Add amazing feature'`)
4. Push branch (`git push origin feature/amazing`)
5. Open Pull Request

## License

MIT License - see [LICENSE](LICENSE) for details.

## Acknowledgments

- [Trivy](https://github.com/aquasecurity/trivy) - Vulnerability scanning
- [CIS Kubernetes Benchmark](https://www.cisecurity.org/benchmark/kubernetes)
- [Kubernetes Python Client](https://github.com/kubernetes-client/python)

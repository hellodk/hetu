# LLM Monitoring Stack for Ollama

This monitoring stack provides comprehensive observability for Ollama LLM deployments, including metrics collection, distributed tracing, alerting, and Grafana dashboards.

## Table of Contents

- [Overview](#overview)
- [Architecture](#architecture)
- [Components](#components)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Configuration](#configuration)
- [Dashboards](#dashboards)
- [Alerts](#alerts)
- [Tracing](#tracing)
- [Application Integration](#application-integration)
- [Troubleshooting](#troubleshooting)

---

## Overview

This stack monitors:
- **Ollama server metrics** - Request rates, latency, VRAM usage, queue depth
- **LLM application metrics** - Token usage, time-to-first-token (TTFT), success rates
- **Distributed traces** - End-to-end request tracing through OpenTelemetry
- **Alerts** - Proactive notifications for performance degradation

### Key Features

| Feature | Description |
|---------|-------------|
| Request Metrics | Track request count, latency percentiles (P50/P95/P99) |
| Token Tracking | Input/output token counts per model and task |
| VRAM Monitoring | GPU memory utilization and alerts |
| Distributed Tracing | Full request traces via OpenTelemetry + Tempo |
| Slack Alerts | Real-time notifications for critical issues |
| Grafana Dashboards | Pre-built dashboards in dedicated "llm" folder |

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Kubernetes Cluster                              │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐       │
│  │   Your App      │     │   Your App      │     │   Ollama        │       │
│  │ (Go/Python)     │     │ (Go/Python)     │     │   Server        │       │
│  │                 │     │                 │     │                 │       │
│  │ ┌─────────────┐ │     │ ┌─────────────┐ │     │  192.168.1.10   │       │
│  │ │ LLM Metrics │ │     │ │ LLM Metrics │ │     │    :11434       │       │
│  │ │  Wrapper    │ │     │ │  Wrapper    │ │     └────────┬────────┘       │
│  │ └──────┬──────┘ │     │ └──────┬──────┘ │              │                │
│  └────────┼────────┘     └────────┼────────┘              │                │
│           │                       │                       │                │
│           │ OTLP                  │ OTLP                  │ /metrics       │
│           │ traces                │ traces                │                │
│           ▼                       ▼                       ▼                │
│  ┌────────────────────────────────────────────────────────────────────┐    │
│  │                     monitoring namespace                            │    │
│  │                                                                     │    │
│  │  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐          │    │
│  │  │    OTEL      │    │    Tempo     │    │  Prometheus  │          │    │
│  │  │  Collector   │───▶│   (traces)   │    │  (metrics)   │◀─────────│    │
│  │  │   :4317      │    │   :3200      │    │   :9090      │          │    │
│  │  └──────────────┘    └──────────────┘    └──────┬───────┘          │    │
│  │                                                  │                  │    │
│  │                      ┌──────────────┐           │                  │    │
│  │                      │   Grafana    │◀──────────┘                  │    │
│  │                      │   :3000      │                              │    │
│  │                      │              │                              │    │
│  │                      │  📁 llm/     │                              │    │
│  │                      │  ├─ Ollama   │                              │    │
│  │                      │  └─ Traces   │                              │    │
│  │                      └──────────────┘                              │    │
│  │                                                                     │    │
│  │  ┌──────────────┐    ┌──────────────┐                              │    │
│  │  │ AlertManager │───▶│    Slack     │                              │    │
│  │  │   :9093      │    │  #alerts     │                              │    │
│  │  └──────────────┘    └──────────────┘                              │    │
│  │                                                                     │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Components

### Directory Structure

```
manifests/monitoring/
├── grafana/
│   ├── folder-provisioning.yaml    # Creates 'llm' folder in Grafana
│   └── llm-traces-dashboard.yaml   # LLM request traces dashboard
├── ollama/
│   ├── servicemonitor.yaml         # Prometheus ServiceMonitor for Ollama
│   └── grafana-dashboard.yaml      # Ollama metrics dashboard
├── alerts/
│   ├── ollama-alerts.yaml          # PrometheusRules (14 alert rules)
│   └── alertmanager-config.yaml    # Slack integration configuration
├── tempo/
│   ├── tempo-deployment.yaml       # Tempo server for trace storage
│   ├── grafana-datasource.yaml     # Tempo datasource for Grafana
│   └── otel-collector.yaml         # OpenTelemetry Collector
├── deploy-monitoring.sh            # Deployment script
├── kustomization.yaml              # Kustomize bundle
└── README.md                       # This documentation
```

### Component Details

| Component | Image | Purpose |
|-----------|-------|---------|
| Tempo | `grafana/tempo:2.3.1` | Distributed trace storage and querying |
| OTEL Collector | `otel/opentelemetry-collector-contrib:0.92.0` | Trace aggregation and forwarding |
| ServiceMonitor | CRD | Prometheus scrape configuration for Ollama |
| PrometheusRule | CRD | Alert rule definitions |

---

## Prerequisites

1. **Kubernetes cluster** (v1.24+)
2. **Prometheus Operator** installed (for ServiceMonitor/PrometheusRule CRDs)
3. **Grafana** with sidecar for dashboard provisioning
4. **kubectl** configured with cluster access
5. **Ollama** server accessible from the cluster

### Check Prerequisites

```bash
# Verify Prometheus Operator CRDs
kubectl get crd servicemonitors.monitoring.coreos.com
kubectl get crd prometheusrules.monitoring.coreos.com

# Verify Grafana is running
kubectl get pods -n monitoring -l app.kubernetes.io/name=grafana
```

---

## Installation

### Quick Start

```bash
cd manifests/monitoring

# Deploy with default settings (Ollama at 192.168.1.10)
./deploy-monitoring.sh deploy --ollama-ip 192.168.1.10

# Or with Slack alerts
./deploy-monitoring.sh deploy \
  --ollama-ip 192.168.1.10 \
  --slack-webhook "https://hooks.slack.com/services/YOUR/WEBHOOK/URL"
```

### Using Kustomize

```bash
# Deploy all components
kubectl apply -k manifests/monitoring/

# Or selectively
kubectl apply -f manifests/monitoring/ollama/
kubectl apply -f manifests/monitoring/tempo/
kubectl apply -f manifests/monitoring/alerts/
kubectl apply -f manifests/monitoring/grafana/
```

### Verify Installation

```bash
# Check deployment status
./deploy-monitoring.sh status

# Or manually
kubectl get pods -n monitoring -l 'app in (tempo,otel-collector)'
kubectl get servicemonitors -n monitoring
kubectl get prometheusrules -n monitoring
```

---

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `MONITORING_NAMESPACE` | Namespace for monitoring components | `monitoring` |
| `OLLAMA_NAMESPACE` | Namespace for Ollama service | `ai-system` |
| `OLLAMA_IP` | IP address of Ollama server | `192.168.1.10` |
| `SLACK_WEBHOOK_URL` | Slack webhook for alerts | *(none)* |

### Customizing Ollama Endpoint

If your Ollama is at a different IP, update before deployment:

```bash
# Option 1: Via deploy script
./deploy-monitoring.sh deploy --ollama-ip 10.0.0.50

# Option 2: Edit directly
sed -i 's/192.168.1.10/YOUR_IP/g' ollama/servicemonitor.yaml
```

### Slack Webhook Setup

1. Go to https://api.slack.com/apps
2. Create new app → "From scratch"
3. Enable "Incoming Webhooks"
4. Add webhook to your channel
5. Copy webhook URL and deploy:

```bash
./deploy-monitoring.sh deploy --slack-webhook "https://hooks.slack.com/..."
```

---

## Dashboards

All dashboards are provisioned in the **`llm`** folder in Grafana.

### Ollama LLM Monitoring (`ollama-monitoring`)

| Panel | Description |
|-------|-------------|
| Ollama Status | Up/down indicator |
| Models Loaded | Number of loaded models |
| Active Requests | Currently processing requests |
| Queued Requests | Requests waiting in queue |
| GPU Memory Used | VRAM consumption |
| VRAM Utilization | Percentage gauge |
| Request Rate by Model | Requests/second by model |
| Request Latency Percentiles | P50/P95/P99 latency |
| Token Generation Rate | Tokens/second |
| Time to First Token | TTFT percentiles |
| LLM Requests by Task | Application-level metrics |
| Token Usage (Hourly) | Input/output token counts |
| LLM Success Rate | Success percentage |

### LLM Request Traces (`llm-traces`)

| Panel | Description |
|-------|-------------|
| Recent LLM Traces | Searchable trace list |
| Service Dependency Graph | Node graph of services |
| LLM Request Latency | Duration distribution |
| Requests by Task | Success/error breakdown |
| Errors by Type | Pie chart of error types |
| Error Rate Over Time | Error trend analysis |

### Accessing Dashboards

```bash
# Port-forward Grafana
kubectl port-forward svc/grafana 3000:80 -n monitoring

# Open browser
open http://localhost:3000/dashboards/f/llm-folder/llm
```

---

## Alerts

### Alert Rules

| Alert | Condition | Severity | Description |
|-------|-----------|----------|-------------|
| `OllamaDown` | `up == 0` for 2m | Critical | Ollama service unreachable |
| `OllamaHighLatency` | P95 > 30s for 5m | Warning | Response times degraded |
| `OllamaCriticalLatency` | P95 > 60s for 5m | Critical | Severe latency issues |
| `OllamaQueueBacklog` | Queue > 10 for 5m | Warning | Requests backing up |
| `OllamaVRAMWarning` | VRAM > 80% for 5m | Warning | GPU memory pressure |
| `OllamaVRAMCritical` | VRAM > 95% for 2m | Critical | OOM imminent |
| `OllamaNoModelsLoaded` | Models == 0 for 5m | Warning | Cold start expected |
| `OllamaHighErrorRate` | Errors > 5% for 5m | Warning | Elevated failures |
| `OllamaErrorRateCritical` | Errors > 20% for 3m | Critical | Service degraded |
| `LLMRequestFailureRate` | App errors > 10% for 5m | Warning | Application issues |
| `LLMSlowResponses` | App P95 > 45s for 10m | Warning | Slow analysis |
| `LLMTokenUsageSpike` | Tokens > 10k/5m for 10m | Info | Unusual usage |
| `LLMNoRequests` | No requests for 30m | Warning | System may be down |

### Slack Alert Channels

Configure these channels in Slack for alert routing:

| Channel | Alert Types |
|---------|-------------|
| `#k8s-alerts` | General warnings |
| `#k8s-critical` | Critical alerts |
| `#ollama-alerts` | Ollama-specific alerts |
| `#llm-alerts` | Application LLM alerts |

---

## Tracing

### How Tracing Works

1. Application uses instrumented LLM client
2. Client creates spans for each LLM request
3. Spans sent to OTEL Collector via OTLP (port 4317)
4. Collector forwards traces to Tempo
5. Grafana queries Tempo for visualization

### Trace Attributes

Each LLM trace includes:

| Attribute | Description |
|-----------|-------------|
| `llm.model` | Model name (e.g., `llama3:70b`) |
| `llm.provider` | Provider (e.g., `ollama`) |
| `llm.task` | Task type (e.g., `rca`, `security`) |
| `llm.input_tokens` | Input token count |
| `llm.output_tokens` | Output token count |
| `llm.duration_seconds` | Request duration |
| `llm.ttft_seconds` | Time to first token |

### Viewing Traces

1. Open Grafana → Explore
2. Select "Tempo" datasource
3. Search by service: `cluster-intel`
4. Filter by span name: `llm.complete`

---

## Application Integration

### Go Integration

```go
import (
    // Add to your imports
    "your-module/analyzer"
)

// Create instrumented client
metrics := analyzer.NewLLMMetrics("cluster_intel")
client := analyzer.NewLLMClient(analyzer.LLMClientConfig{
    Endpoint:    "http://ollama:11434",
    Model:       "llama3:70b",
    Provider:    "ollama",
    MaxTokens:   4096,
    Temperature: 0.3,
}, metrics)

// Use the client
result, err := client.Complete(ctx, "rca", messages)
if err != nil {
    // Error automatically recorded in metrics
    return err
}
// Token usage automatically tracked
```

### Python Integration

```python
from llm_metrics import create_instrumented_llm_client

# Create client
client = create_instrumented_llm_client(
    endpoint="http://ollama:11434",
    model="llama3:70b"
)

# Use the client
result = client.complete(
    messages=[
        {"role": "system", "content": "You are a K8s expert."},
        {"role": "user", "content": prompt}
    ],
    task="rca"  # Task type for metrics grouping
)

# Access results
print(f"Response: {result.content}")
print(f"Tokens: {result.input_tokens} in, {result.output_tokens} out")
print(f"TTFT: {result.time_to_first_token:.2f}s")
```

### Environment Variables for Applications

```yaml
env:
  - name: LLM_ENDPOINT
    value: "http://ollama.ai-system.svc:11434"
  - name: LLM_MODEL
    value: "llama3:70b"
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: "http://otel-collector.monitoring.svc:4317"
  - name: OTEL_SERVICE_NAME
    value: "cluster-intel"
```

---

## Troubleshooting

### Common Issues

#### Ollama metrics not appearing

```bash
# Check if ServiceMonitor is created
kubectl get servicemonitor ollama -n monitoring

# Check Prometheus targets
kubectl port-forward svc/prometheus 9090:9090 -n monitoring
# Visit http://localhost:9090/targets - look for 'ollama' job

# Verify Ollama endpoint is reachable
kubectl run curl --rm -it --image=curlimages/curl -- \
  curl -s http://ollama.ai-system.svc:11434/api/tags
```

#### Traces not appearing in Tempo

```bash
# Check OTEL Collector logs
kubectl logs -n monitoring -l app=otel-collector

# Verify OTEL Collector is receiving traces
kubectl port-forward svc/otel-collector 55679:55679 -n monitoring
# Visit http://localhost:55679/debug/tracez

# Check Tempo is healthy
kubectl logs -n monitoring -l app=tempo
```

#### Dashboards not appearing in Grafana

```bash
# Check ConfigMaps have correct labels
kubectl get configmaps -n monitoring -l grafana_dashboard=1

# Restart Grafana sidecar
kubectl rollout restart deployment grafana -n monitoring

# Check sidecar logs
kubectl logs -n monitoring -l app.kubernetes.io/name=grafana -c grafana-sc-dashboard
```

#### Alerts not firing

```bash
# Check PrometheusRule is loaded
kubectl get prometheusrules ollama-alerts -n monitoring

# Check rule evaluation in Prometheus
kubectl port-forward svc/prometheus 9090:9090 -n monitoring
# Visit http://localhost:9090/rules

# Test alert manually
# Artificially trigger by stopping Ollama briefly
```

### Useful Commands

```bash
# Check overall status
./deploy-monitoring.sh status

# View component logs
kubectl logs -n monitoring -l app=tempo --tail=100
kubectl logs -n monitoring -l app=otel-collector --tail=100

# Restart components
kubectl rollout restart deployment tempo -n monitoring
kubectl rollout restart deployment otel-collector -n monitoring

# Uninstall everything
./deploy-monitoring.sh uninstall
```

---

## Uninstalling

```bash
# Remove all monitoring components
./deploy-monitoring.sh uninstall

# Or manually with kustomize
kubectl delete -k manifests/monitoring/
```

---

## References

- [Ollama API Documentation](https://github.com/ollama/ollama/blob/main/docs/api.md)
- [Prometheus Operator](https://prometheus-operator.dev/)
- [Grafana Tempo](https://grafana.com/docs/tempo/latest/)
- [OpenTelemetry Collector](https://opentelemetry.io/docs/collector/)
- [Slack Incoming Webhooks](https://api.slack.com/messaging/webhooks)

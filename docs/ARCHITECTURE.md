# K8s AI Cluster Intelligence Engine - Architecture (v7)

## System Overview

```mermaid
flowchart TB
    subgraph External["External Systems"]
        K8sAPI["K8s API"]
        KPS["kube-prometheus-stack<br/>(existing)"]
        S3["S3<br/>(LB logs)"]
        LLM["LLM Provider"]
        TrivyOp["Trivy Operator"]
    end

    subgraph Frontend["Frontend"]
        Dashboard["Dashboard<br/>(Next.js)"]
    end

    subgraph Bus["Message Bus"]
        NATS["NATS JetStream"]
    end

    subgraph Backend["Backend Services"]
        Collector["Collector<br/>(Go, existing)"]
        Analyzer["Analyzer<br/>(Go, existing)<br/>RCA / Optimizers /<br/>Anomaly / Workload Browser"]
        CollectorPodlogs["collector-podlogs<br/>(Go, NEW)"]
        CollectorLblogs["collector-lblogs<br/>(Go, NEW)"]
        SecurityScanner["Security Scanner"]
    end

    subgraph Storage["Storage"]
        Postgres["Postgres"]
        ClickHouse["ClickHouse<br/>(optional)"]
        Redis["Redis"]
    end

    %% Data flows
    K8sAPI -->|watches & events| Collector
    KPS -->|metrics| Collector
    K8sAPI -->|pod log streams| CollectorPodlogs
    S3 -->|LB access logs| CollectorLblogs
    TrivyOp -->|vuln reports| SecurityScanner

    Collector -->|publish| NATS
    CollectorPodlogs -->|publish| NATS
    CollectorLblogs -->|publish| NATS
    SecurityScanner -->|publish| NATS

    NATS -->|subscribe| Analyzer
    Analyzer -->|query| LLM
    LLM -->|response| Analyzer

    Analyzer -->|write| Postgres
    Analyzer -->|write| ClickHouse
    Analyzer -->|cache| Redis
    SecurityScanner -->|write| Postgres

    Dashboard -->|read| Postgres
    Dashboard -->|read| Redis
```

## Component Responsibilities

### 1. Data Collection Layer

| Component | Responsibility | Data Sources |
|-----------|---------------|--------------|
| Collector (Go, existing) | Watch cluster state changes and scrape metrics | Pods, Deployments, Events, Nodes, Services, PVCs, RBAC, NetworkPolicies via K8s API; CPU, Memory, Network, Disk via kube-prometheus-stack |
| collector-podlogs (Go, NEW) | Stream and forward pod logs | K8s API pod log streams |
| collector-lblogs (Go, NEW) | Ingest load-balancer access logs | S3 buckets |
| Security Scanner | Aggregate vulnerability and misconfiguration reports | Trivy Operator CRDs |

### 2. Message Bus

| Component | Responsibility | Notes |
|-----------|---------------|-------|
| NATS JetStream | Durable, ordered delivery between collectors and analyzers | Subjects per data type; at-least-once semantics |

### 3. Analyzer (Go, existing + v7 extensions)

| Sub-component | Responsibility | Output |
|---------------|---------------|--------|
| Correlator | Correlate events, metrics, and logs into incidents | Incident records |
| RCA Engine | Root-cause analysis via LLM | RCA reports |
| Optimizers | Resource, cost, and reliability optimization suggestions | Scored recommendations |
| Anomaly Detector | Detect anomalous patterns in time-series and logs | Anomaly alerts |
| Workload Browser | Queryable inventory of workloads and their status | Workload index |

### 4. Security Scanner

| Component | Responsibility | Output |
|-----------|---------------|--------|
| Vuln Aggregator | Collect Trivy Operator VulnerabilityReports | Vulnerability summaries |
| Policy Checker | Evaluate workloads against security baselines | Policy violations |

### 5. Frontend

| Component | Responsibility | Output |
|-----------|---------------|--------|
| Dashboard (Next.js) | Visualize health, incidents, RCA reports, recommendations | Interactive UI |

## Data Flow

End-to-end root-cause analysis path:

```mermaid
sequenceDiagram
    participant K8sAPI as K8s API
    participant Collector as Collector
    participant NATS as NATS JetStream
    participant Correlator as Correlator
    participant Incident as Incident Store
    participant RCA as RCA Engine
    participant LLM as LLM Provider
    participant Postgres as Postgres
    participant Dashboard as Dashboard

    K8sAPI->>Collector: events & metrics
    Collector->>NATS: publish normalized data
    NATS->>Correlator: subscribe
    Correlator->>Incident: create incident
    Incident->>RCA: trigger analysis
    RCA->>LLM: send context + prompt
    LLM->>RCA: structured diagnosis
    RCA->>Postgres: persist RCA report
    Postgres->>Dashboard: query & display
```

## Resource Footprint

### Minimum Requirements (Small Cluster <100 nodes)

| Component | CPU | Memory | Storage |
|-----------|-----|--------|---------|
| Collector | 100m | 256Mi | - |
| collector-podlogs | 100m | 128Mi | - |
| collector-lblogs | 50m | 128Mi | - |
| Analyzer | 500m | 1Gi | - |
| Security Scanner | 100m | 256Mi | - |
| Dashboard | 50m | 128Mi | - |
| NATS JetStream | 100m | 256Mi | 2Gi |
| Redis | 100m | 256Mi | 1Gi |
| PostgreSQL | 200m | 512Mi | 10Gi |
| **Total** | **~1.3 cores** | **~2.9Gi** | **~13Gi** |

### Recommended (Medium Cluster 100-1000 nodes)

| Component | CPU | Memory | Storage |
|-----------|-----|--------|---------|
| Collector | 500m | 1Gi | - |
| collector-podlogs | 250m | 512Mi | - |
| collector-lblogs | 100m | 256Mi | - |
| Analyzer | 2000m | 4Gi | - |
| Security Scanner | 250m | 512Mi | - |
| Dashboard | 100m | 256Mi | - |
| NATS JetStream | 500m | 1Gi | 10Gi |
| Redis | 500m | 1Gi | 5Gi |
| PostgreSQL | 1000m | 2Gi | 50Gi |
| ClickHouse (optional) | 1000m | 2Gi | 100Gi |
| **Total** | **~6.2 cores** | **~12.5Gi** | **~165Gi** |

### Large Scale (1000-10000+ nodes)

- Deploy collector as DaemonSet with node-level aggregation
- Shard analyzer across multiple pods
- Use ClickHouse for high-volume log and metric retention
- Implement hierarchical aggregation
- Consider multi-cluster federation

## Security Model

### RBAC Requirements

```yaml
# cluster-intel-reader (existing) - read-only access for collection
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: cluster-intel-reader
rules:
  # Read-only access to most resources
  - apiGroups: [""]
    resources: ["pods", "pods/log", "services", "endpoints", "nodes", "events",
                "namespaces", "configmaps", "secrets", "persistentvolumeclaims"]
    verbs: ["get", "list", "watch"]

  # Metrics and status
  - apiGroups: ["metrics.k8s.io"]
    resources: ["pods", "nodes"]
    verbs: ["get", "list"]

  # Workloads
  - apiGroups: ["apps"]
    resources: ["deployments", "replicasets", "statefulsets", "daemonsets"]
    verbs: ["get", "list", "watch"]

  # Autoscaling
  - apiGroups: ["autoscaling"]
    resources: ["horizontalpodautoscalers"]
    verbs: ["get", "list", "watch"]

  # Networking
  - apiGroups: ["networking.k8s.io"]
    resources: ["networkpolicies", "ingresses"]
    verbs: ["get", "list", "watch"]

  # RBAC (for security analysis)
  - apiGroups: ["rbac.authorization.k8s.io"]
    resources: ["roles", "rolebindings", "clusterroles", "clusterrolebindings"]
    verbs: ["get", "list", "watch"]

  # Policy
  - apiGroups: ["policy"]
    resources: ["poddisruptionbudgets", "podsecuritypolicies"]
    verbs: ["get", "list", "watch"]

  # Trivy Operator CRDs
  - apiGroups: ["aquasecurity.github.io"]
    resources: ["vulnerabilityreports", "configauditreports"]
    verbs: ["get", "list", "watch"]
```

```yaml
# cluster-intel-writer (new, opt-in) - write access for remediation actions
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: cluster-intel-writer
rules:
  # Patch deployments for resource right-sizing
  - apiGroups: ["apps"]
    resources: ["deployments", "statefulsets"]
    verbs: ["patch", "update"]

  # Manage HPA recommendations
  - apiGroups: ["autoscaling"]
    resources: ["horizontalpodautoscalers"]
    verbs: ["create", "patch", "update"]

  # Create events for audit trail
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["create"]
```

### Network Policies

```
┌──────────────────────────────────────────────────────────────┐
│                    NETWORK SEGMENTATION                       │
├──────────────────────────────────────────────────────────────┤
│                                                              │
│  ┌─────────────┐        ┌─────────────┐                     │
│  │ Collector   │◄──────►│ K8s API     │  (Allowed)          │
│  └─────────────┘        └─────────────┘                     │
│         │                                                    │
│         ▼                                                    │
│  ┌─────────────┐        ┌─────────────┐                     │
│  │ Processor   │◄──────►│ Redis       │  (Internal only)    │
│  └─────────────┘        └─────────────┘                     │
│         │                                                    │
│         ▼                                                    │
│  ┌─────────────┐        ┌─────────────┐                     │
│  │ Analyzer    │───────►│ LLM Backend │  (Egress allowed)   │
│  └─────────────┘        └─────────────┘                     │
│         │                                                    │
│         ▼                                                    │
│  ┌─────────────┐        ┌─────────────┐                     │
│  │ API Server  │◄──────►│ Dashboard   │  (Ingress allowed)  │
│  └─────────────┘        └─────────────┘                     │
│                                                              │
└──────────────────────────────────────────────────────────────┘
```

### Secrets Management

- LLM API keys stored in Kubernetes Secrets (or external secrets operator)
- Database credentials via Secret references
- TLS certificates for internal communication
- Service account tokens auto-rotated
- Support for Vault/AWS Secrets Manager/GCP Secret Manager

## Configuration Model

All external endpoints (Postgres, ClickHouse, Redis, NATS, Prometheus, LLM providers, S3 buckets) are configurable via `pkg/config` -- see `docs/PLAN_V7.md` section 4.5 for the complete specification, config schema, env-var conventions, and Helm chart values.

## Multi-Cluster Support

```
┌─────────────────────────────────────────────────────────────────┐
│                     MULTI-CLUSTER TOPOLOGY                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌──────────────────┐   ┌──────────────────┐                    │
│  │  Cluster A       │   │  Cluster B       │                    │
│  │  ┌────────────┐  │   │  ┌────────────┐  │                    │
│  │  │ Collector  │  │   │  │ Collector  │  │                    │
│  │  │ Agent      │──┼───┼──│ Agent      │  │                    │
│  │  └────────────┘  │   │  └────────────┘  │                    │
│  └──────────────────┘   └──────────────────┘                    │
│            │                     │                               │
│            └──────────┬──────────┘                               │
│                       ▼                                          │
│            ┌──────────────────────┐                              │
│            │  Central Hub         │                              │
│            │  ┌────────────────┐  │                              │
│            │  │ Federation     │  │                              │
│            │  │ Controller     │  │                              │
│            │  └────────────────┘  │                              │
│            │  ┌────────────────┐  │                              │
│            │  │ Global         │  │                              │
│            │  │ Dashboard      │  │                              │
│            │  └────────────────┘  │                              │
│            └──────────────────────┘                              │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

## Performance Considerations

### Rate Limiting

| Operation | Default Rate | Configurable |
|-----------|-------------|--------------|
| K8s API calls | 50 QPS | Yes |
| LLM requests | 10 RPM | Yes |
| Metric scrapes | 15s interval | Yes |
| Event processing | 1000/s | Yes |

### Caching Strategy

- **L1 Cache (In-memory)**: Hot data, 5-minute TTL
- **L2 Cache (Redis)**: Warm data, 1-hour TTL
- **L3 Cache (DB)**: Cold data, queryable history

### Backpressure Handling

```
┌─────────────────────────────────────────────────────┐
│               BACKPRESSURE MECHANISM                 │
├─────────────────────────────────────────────────────┤
│                                                     │
│  Input Rate > Processing Rate?                      │
│         │                                           │
│         ▼                                           │
│  ┌─────────────────────────────────────────────┐   │
│  │ 1. Drop low-priority events (INFO level)    │   │
│  │ 2. Increase aggregation window              │   │
│  │ 3. Sample high-frequency metrics            │   │
│  │ 4. Queue overflow → disk spill              │   │
│  │ 5. Alert on sustained pressure              │   │
│  └─────────────────────────────────────────────┘   │
│                                                     │
└─────────────────────────────────────────────────────┘
```

## Air-Gap Deployment

For air-gapped environments:

1. **Local LLM**: Deploy Ollama/vLLM with open models (Llama, Mistral)
2. **Image Registry**: Use internal registry mirror
3. **No External Dependencies**: All telemetry stays internal
4. **Offline Updates**: Support for manual update packages

```yaml
# Air-gap configuration
llm:
  backend: "ollama"
  endpoint: "http://ollama.ai-system.svc:11434"
  model: "llama3:70b"
  
telemetry:
  externalExport: false
  
updates:
  mode: "offline"
  packagePath: "/mnt/updates"
```

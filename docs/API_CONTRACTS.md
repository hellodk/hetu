# API Contracts & Data Structures

## Core Data Models

### ClusterHealthReport

```json
{
  "clusterId": "prod-us-east-1",
  "timestamp": "2026-02-14T10:30:00Z",
  "scores": {
    "overall": 82,
    "reliability": 91,
    "security": 72,
    "cost": 65,
    "architecture": 88
  },
  "summary": {
    "totalNodes": 47,
    "totalPods": 1284,
    "totalNamespaces": 23,
    "healthyPods": 1256,
    "unhealthyPods": 28,
    "pendingPods": 12,
    "evictedPods": 3
  },
  "resourceUtilization": {
    "cpu": {
      "requested": 45.2,
      "used": 32.1,
      "capacity": 94.0,
      "unit": "cores"
    },
    "memory": {
      "requested": 186.5,
      "used": 142.3,
      "capacity": 376.0,
      "unit": "Gi"
    },
    "storage": {
      "requested": 2.4,
      "used": 1.8,
      "capacity": 10.0,
      "unit": "Ti"
    }
  },
  "topIssues": [
    {
      "id": "issue-001",
      "severity": "critical",
      "category": "reliability",
      "title": "CrashLoopBackOff in production",
      "affectedResources": ["prod/api-gateway-7d8f9c6b5-x2k9m"],
      "confidence": 0.94
    }
  ],
  "estimatedMonthlySavings": 4250.00,
  "currency": "USD"
}
```

### TelemetryEvent

```json
{
  "id": "evt-2026021410300001",
  "timestamp": "2026-02-14T10:30:00.123Z",
  "cluster": "prod-us-east-1",
  "source": "kubernetes",
  "type": "Warning",
  "reason": "BackOff",
  "involvedObject": {
    "kind": "Pod",
    "namespace": "prod",
    "name": "api-gateway-7d8f9c6b5-x2k9m",
    "uid": "abc123-def456-ghi789"
  },
  "message": "Back-off restarting failed container",
  "count": 15,
  "firstTimestamp": "2026-02-14T10:15:00Z",
  "lastTimestamp": "2026-02-14T10:30:00Z",
  "metadata": {
    "nodeName": "node-pool-1-abc123",
    "ownerRef": {
      "kind": "ReplicaSet",
      "name": "api-gateway-7d8f9c6b5"
    },
    "labels": {
      "app": "api-gateway",
      "env": "prod"
    }
  }
}
```

### ResourceMetrics

```json
{
  "timestamp": "2026-02-14T10:30:00Z",
  "cluster": "prod-us-east-1",
  "resourceType": "pod",
  "resource": {
    "namespace": "prod",
    "name": "api-gateway-7d8f9c6b5-x2k9m"
  },
  "metrics": {
    "cpu": {
      "usage": 0.245,
      "request": 0.5,
      "limit": 1.0,
      "throttled_seconds": 12.5
    },
    "memory": {
      "usage_bytes": 268435456,
      "request_bytes": 536870912,
      "limit_bytes": 1073741824,
      "working_set_bytes": 234881024,
      "oom_killed": false
    },
    "network": {
      "rx_bytes": 1048576,
      "tx_bytes": 524288,
      "rx_errors": 0,
      "tx_errors": 0
    },
    "restarts": 15
  },
  "trends": {
    "cpu_7d_avg": 0.22,
    "cpu_7d_p95": 0.41,
    "memory_7d_avg": 251658240,
    "memory_7d_p95": 285212672
  }
}
```

### Recommendation

```json
{
  "id": "rec-2026021410300001",
  "timestamp": "2026-02-14T10:30:00Z",
  "cluster": "prod-us-east-1",
  "category": "cost",
  "subcategory": "right-sizing",
  "severity": "medium",
  "confidence": 0.87,
  "title": "Reduce CPU request for api-gateway",
  "description": "The api-gateway deployment is consistently using only 45% of its requested CPU over the past 7 days. Right-sizing could save resources.",
  "affectedResources": [
    {
      "kind": "Deployment",
      "namespace": "prod",
      "name": "api-gateway"
    }
  ],
  "impact": {
    "costSavings": {
      "monthly": 125.50,
      "currency": "USD"
    },
    "resourceSavings": {
      "cpu": 0.25,
      "unit": "cores"
    },
    "riskLevel": "low",
    "blastRadius": "single-service"
  },
  "evidence": [
    {
      "type": "metric",
      "description": "7-day CPU utilization average: 45%",
      "data": {
        "metric": "container_cpu_usage_seconds_total",
        "value": 0.225,
        "request": 0.5
      }
    }
  ],
  "remediation": {
    "type": "patch",
    "automated": true,
    "patch": {
      "apiVersion": "apps/v1",
      "kind": "Deployment",
      "metadata": {
        "name": "api-gateway",
        "namespace": "prod"
      },
      "spec": {
        "template": {
          "spec": {
            "containers": [
              {
                "name": "api-gateway",
                "resources": {
                  "requests": {
                    "cpu": "250m"
                  }
                }
              }
            ]
          }
        }
      }
    }
  },
  "status": "pending",
  "aiReasoning": "Based on 7-day metrics analysis, this pod consistently operates at 45% of requested CPU with P95 at 82%. A 50% reduction in CPU request maintains headroom while reducing waste.",
  "relatedIssues": ["rec-2026021310150003"],
  "tags": ["quick-win", "automated", "low-risk"]
}
```

### SecurityFinding

```json
{
  "id": "sec-2026021410300001",
  "timestamp": "2026-02-14T10:30:00Z",
  "cluster": "prod-us-east-1",
  "category": "container-security",
  "subcategory": "image-vulnerability",
  "severity": "high",
  "cveId": "CVE-2026-1234",
  "title": "Critical vulnerability in base image",
  "description": "The nginx:1.21 base image contains a critical RCE vulnerability",
  "affectedResources": [
    {
      "kind": "Pod",
      "namespace": "prod",
      "name": "web-frontend-abc123",
      "image": "nginx:1.21",
      "imageDigest": "sha256:abc123..."
    }
  ],
  "cisControl": "5.1.1",
  "compliance": ["PCI-DSS", "SOC2"],
  "remediation": {
    "type": "image-update",
    "targetImage": "nginx:1.25.4",
    "automated": false,
    "instructions": "Update the base image to nginx:1.25.4 or later"
  },
  "evidence": {
    "scanner": "trivy",
    "scanTimestamp": "2026-02-14T10:00:00Z",
    "vulnerabilityDetails": {
      "cvss": 9.8,
      "exploitAvailable": true,
      "fixedIn": "1.25.0"
    }
  },
  "status": "open",
  "assignee": null,
  "sla": {
    "dueDate": "2026-02-21T10:30:00Z",
    "severity": "P1"
  }
}
```

### AnalysisRequest (LLM Pipeline)

```json
{
  "requestId": "analysis-2026021410300001",
  "timestamp": "2026-02-14T10:30:00Z",
  "cluster": "prod-us-east-1",
  "analysisType": "incident-root-cause",
  "context": {
    "timeWindow": {
      "start": "2026-02-14T10:00:00Z",
      "end": "2026-02-14T10:30:00Z"
    },
    "focusResource": {
      "kind": "Pod",
      "namespace": "prod",
      "name": "api-gateway-7d8f9c6b5-x2k9m"
    },
    "relatedEvents": [
      {
        "type": "Warning",
        "reason": "BackOff",
        "message": "Back-off restarting failed container",
        "count": 15
      },
      {
        "type": "Warning", 
        "reason": "Unhealthy",
        "message": "Liveness probe failed: connection refused",
        "count": 8
      }
    ],
    "metrics": {
      "cpu_usage": 0.95,
      "memory_usage": 0.87,
      "restarts": 15
    },
    "logs": [
      {
        "timestamp": "2026-02-14T10:29:55Z",
        "level": "ERROR",
        "message": "Failed to connect to database: connection timeout"
      }
    ],
    "topology": {
      "dependencies": ["postgres-primary", "redis-cache"],
      "dependents": ["web-frontend", "mobile-api"]
    }
  },
  "constraints": {
    "maxTokens": 2000,
    "temperature": 0.3,
    "responseFormat": "structured"
  }
}
```

### AnalysisResponse (LLM Output)

```json
{
  "requestId": "analysis-2026021410300001",
  "timestamp": "2026-02-14T10:30:05Z",
  "status": "completed",
  "analysis": {
    "rootCause": {
      "primary": "database-connectivity",
      "confidence": 0.92,
      "description": "The api-gateway pod is experiencing CrashLoopBackOff due to failed database connections. The postgres-primary service appears to be under load, causing connection timeouts during the pod's startup health checks."
    },
    "contributingFactors": [
      {
        "factor": "aggressive-liveness-probe",
        "confidence": 0.78,
        "description": "Liveness probe timeout of 1s is too aggressive for database-dependent startup"
      },
      {
        "factor": "missing-connection-retry",
        "confidence": 0.85,
        "description": "Application lacks exponential backoff for database connections"
      }
    ],
    "blastRadius": {
      "directImpact": ["api-gateway"],
      "indirectImpact": ["web-frontend", "mobile-api"],
      "severity": "high",
      "userFacing": true
    },
    "recommendations": [
      {
        "action": "increase-probe-timeout",
        "priority": 1,
        "effort": "low",
        "description": "Increase liveness probe initialDelaySeconds to 30 and timeout to 5s"
      },
      {
        "action": "add-startup-probe",
        "priority": 2,
        "effort": "low",
        "description": "Add startupProbe to handle slow database connections during initialization"
      },
      {
        "action": "investigate-database",
        "priority": 3,
        "effort": "medium",
        "description": "Review postgres-primary resource utilization and connection pool settings"
      }
    ],
    "preventiveMeasures": [
      "Implement circuit breaker pattern for database connections",
      "Add PodDisruptionBudget to prevent cascading restarts",
      "Configure connection pool warmup in application"
    ]
  },
  "metadata": {
    "modelUsed": "gpt-4-turbo",
    "tokensUsed": 1847,
    "latencyMs": 4523
  }
}
```

## REST API Endpoints

### Health & Scores

```
GET /api/v1/clusters/{clusterId}/health
GET /api/v1/clusters/{clusterId}/scores
GET /api/v1/clusters/{clusterId}/scores/history?from={timestamp}&to={timestamp}
```

### Namespaces

```
GET /api/v1/clusters/{clusterId}/namespaces
GET /api/v1/clusters/{clusterId}/namespaces/{namespace}/health
GET /api/v1/clusters/{clusterId}/namespaces/{namespace}/pods
GET /api/v1/clusters/{clusterId}/namespaces/{namespace}/recommendations
```

### Recommendations

```
GET /api/v1/clusters/{clusterId}/recommendations
GET /api/v1/clusters/{clusterId}/recommendations/{id}
POST /api/v1/clusters/{clusterId}/recommendations/{id}/apply
POST /api/v1/clusters/{clusterId}/recommendations/{id}/dismiss
GET /api/v1/clusters/{clusterId}/recommendations/summary
```

### Security

```
GET /api/v1/clusters/{clusterId}/security/findings
GET /api/v1/clusters/{clusterId}/security/findings/{id}
GET /api/v1/clusters/{clusterId}/security/compliance
GET /api/v1/clusters/{clusterId}/security/rbac-analysis
```

### Cost

```
GET /api/v1/clusters/{clusterId}/cost/summary
GET /api/v1/clusters/{clusterId}/cost/breakdown
GET /api/v1/clusters/{clusterId}/cost/forecast
POST /api/v1/clusters/{clusterId}/cost/what-if
```

### Analysis

```
POST /api/v1/clusters/{clusterId}/analysis/trigger
GET /api/v1/clusters/{clusterId}/analysis/{analysisId}
GET /api/v1/clusters/{clusterId}/analysis/history
```

### Events & Timeline

```
GET /api/v1/clusters/{clusterId}/events
GET /api/v1/clusters/{clusterId}/timeline
GET /api/v1/clusters/{clusterId}/incidents
```

## WebSocket Subscriptions

```
WS /api/v1/ws/clusters/{clusterId}/stream

// Subscribe to specific channels
{
  "action": "subscribe",
  "channels": ["health", "events", "recommendations", "alerts"]
}

// Message format
{
  "channel": "events",
  "type": "event.new",
  "timestamp": "2026-02-14T10:30:00Z",
  "data": { /* TelemetryEvent */ }
}
```

## GraphQL Schema (Excerpt)

```graphql
type Query {
  cluster(id: ID!): Cluster
  clusters: [Cluster!]!
  recommendations(
    clusterId: ID!
    category: RecommendationCategory
    severity: Severity
    limit: Int
    offset: Int
  ): RecommendationConnection!
  securityFindings(
    clusterId: ID!
    severity: Severity
    status: FindingStatus
  ): [SecurityFinding!]!
}

type Cluster {
  id: ID!
  name: String!
  health: ClusterHealth!
  namespaces: [Namespace!]!
  nodes: [Node!]!
  recommendations: [Recommendation!]!
  costSummary: CostSummary!
}

type ClusterHealth {
  overall: Int!
  reliability: Int!
  security: Int!
  cost: Int!
  architecture: Int!
  lastUpdated: DateTime!
}

type Recommendation {
  id: ID!
  category: RecommendationCategory!
  severity: Severity!
  confidence: Float!
  title: String!
  description: String!
  affectedResources: [Resource!]!
  impact: Impact!
  remediation: Remediation
  status: RecommendationStatus!
  aiReasoning: String
}

enum RecommendationCategory {
  RELIABILITY
  COST
  SECURITY
  ARCHITECTURE
}

enum Severity {
  CRITICAL
  HIGH
  MEDIUM
  LOW
  INFO
}

type Mutation {
  applyRecommendation(id: ID!): ApplyResult!
  dismissRecommendation(id: ID!, reason: String): Recommendation!
  triggerAnalysis(clusterId: ID!, type: AnalysisType!): Analysis!
}

type Subscription {
  clusterHealth(clusterId: ID!): ClusterHealth!
  newEvents(clusterId: ID!, types: [String!]): TelemetryEvent!
  recommendations(clusterId: ID!): Recommendation!
}
```

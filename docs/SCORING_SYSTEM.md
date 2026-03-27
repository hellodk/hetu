# Recommendation Scoring System

## Overview

The scoring system evaluates cluster health across four dimensions and generates prioritized recommendations. Each dimension contributes to an overall health score (0-100).

## Score Dimensions

### 1. Reliability Score (0-100)

Measures cluster stability and availability.

```
┌────────────────────────────────────────────────────────────────────────┐
│                      RELIABILITY SCORE CALCULATION                      │
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│  Base Score: 100                                                       │
│                                                                        │
│  Deductions:                                                           │
│  ┌──────────────────────────────────────────────────────────────────┐ │
│  │ CrashLoopBackOff pods      : -10 per pod (max -30)               │ │
│  │ OOMKilled events (24h)     : -5 per event (max -20)              │ │
│  │ Pending pods > 5min        : -3 per pod (max -15)                │ │
│  │ Node NotReady              : -15 per node (max -30)              │ │
│  │ Failed probes (1h)         : -2 per 10 failures (max -10)        │ │
│  │ Evictions (24h)            : -3 per eviction (max -15)           │ │
│  │ Missing PDB for critical   : -5 per deployment (max -15)         │ │
│  │ Single replica deployments : -2 per deployment (max -10)         │ │
│  │ No resource limits         : -1 per pod (max -10)                │ │
│  └──────────────────────────────────────────────────────────────────┘ │
│                                                                        │
│  Bonuses:                                                              │
│  ┌──────────────────────────────────────────────────────────────────┐ │
│  │ All pods healthy 7d        : +5                                  │ │
│  │ Zero evictions 30d         : +3                                  │ │
│  │ PDB coverage > 80%         : +2                                  │ │
│  └──────────────────────────────────────────────────────────────────┘ │
│                                                                        │
│  Final Score = max(0, min(100, base + bonuses - deductions))          │
│                                                                        │
└────────────────────────────────────────────────────────────────────────┘
```

### 2. Security Score (0-100)

Measures security posture and compliance.

```
┌────────────────────────────────────────────────────────────────────────┐
│                       SECURITY SCORE CALCULATION                        │
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│  Base Score: 100                                                       │
│                                                                        │
│  Critical Deductions:                                                  │
│  ┌──────────────────────────────────────────────────────────────────┐ │
│  │ Privileged containers      : -15 per container (max -30)         │ │
│  │ Root containers            : -10 per container (max -30)         │ │
│  │ Host network/PID/IPC       : -10 per pod (max -20)               │ │
│  │ Cluster-admin bindings     : -10 per binding (max -20)           │ │
│  │ Secrets in env vars        : -5 per secret (max -15)             │ │
│  │ No network policies        : -5 per namespace (max -20)          │ │
│  │ Critical CVEs              : -10 per image (max -30)             │ │
│  │ High CVEs                  : -3 per image (max -15)              │ │
│  └──────────────────────────────────────────────────────────────────┘ │
│                                                                        │
│  Moderate Deductions:                                                  │
│  ┌──────────────────────────────────────────────────────────────────┐ │
│  │ Writable root filesystem   : -2 per container (max -10)          │ │
│  │ Missing securityContext    : -2 per pod (max -10)                │ │
│  │ Wildcard RBAC rules        : -3 per rule (max -10)               │ │
│  │ Default service account    : -1 per pod (max -5)                 │ │
│  └──────────────────────────────────────────────────────────────────┘ │
│                                                                        │
│  Bonuses:                                                              │
│  ┌──────────────────────────────────────────────────────────────────┐ │
│  │ Pod Security Standards     : +5 (restricted), +3 (baseline)      │ │
│  │ All images scanned         : +5                                  │ │
│  │ Network policies 100%      : +5                                  │ │
│  │ No critical CVEs           : +5                                  │ │
│  └──────────────────────────────────────────────────────────────────┘ │
│                                                                        │
└────────────────────────────────────────────────────────────────────────┘
```

### 3. Cost Efficiency Score (0-100)

Measures resource utilization and waste.

```
┌────────────────────────────────────────────────────────────────────────┐
│                    COST EFFICIENCY SCORE CALCULATION                    │
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│  Components:                                                           │
│                                                                        │
│  1. CPU Efficiency (40% weight):                                       │
│  ┌──────────────────────────────────────────────────────────────────┐ │
│  │ Optimal: Used/Requested 60-80% = 100 points                      │ │
│  │ Under-utilized: < 30% = (used/requested) * 100                   │ │
│  │ Over-utilized: > 90% = 100 - ((utilization - 90) * 2)            │ │
│  └──────────────────────────────────────────────────────────────────┘ │
│                                                                        │
│  2. Memory Efficiency (40% weight):                                    │
│  ┌──────────────────────────────────────────────────────────────────┐ │
│  │ Same formula as CPU                                              │ │
│  └──────────────────────────────────────────────────────────────────┘ │
│                                                                        │
│  3. Storage Efficiency (20% weight):                                   │
│  ┌──────────────────────────────────────────────────────────────────┐ │
│  │ Unused PVCs (no pods)      : -20 per PVC (max -40)               │ │
│  │ PVC utilization < 20%      : -5 per PVC (max -20)                │ │
│  │ Unbound PVCs > 24h         : -10 per PVC (max -20)               │ │
│  └──────────────────────────────────────────────────────────────────┘ │
│                                                                        │
│  Deductions:                                                           │
│  ┌──────────────────────────────────────────────────────────────────┐ │
│  │ Idle deployments (< 1% CPU): -5 per deployment (max -20)         │ │
│  │ Zombie pods (no owner)     : -3 per pod (max -15)                │ │
│  │ On-demand in non-prod      : -2 per node (max -10)               │ │
│  └──────────────────────────────────────────────────────────────────┘ │
│                                                                        │
│  Final = (CPU * 0.4) + (Memory * 0.4) + (Storage * 0.2) - Deductions  │
│                                                                        │
└────────────────────────────────────────────────────────────────────────┘
```

### 4. Architecture Score (0-100)

Measures architectural best practices.

```
┌────────────────────────────────────────────────────────────────────────┐
│                    ARCHITECTURE SCORE CALCULATION                       │
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│  Base Score: 100                                                       │
│                                                                        │
│  Deductions:                                                           │
│  ┌──────────────────────────────────────────────────────────────────┐ │
│  │ Single zone deployment     : -20                                 │ │
│  │ No pod anti-affinity       : -3 per deployment (max -15)         │ │
│  │ Monolith pods (> 4 containers): -5 per pod (max -15)             │ │
│  │ Missing topology spread    : -3 per deployment (max -10)         │ │
│  │ Tight coupling (shared PVC): -5 per shared PVC (max -15)         │ │
│  │ No HPA for variable load   : -3 per deployment (max -10)         │ │
│  │ Circular dependencies      : -10 per cycle (max -20)             │ │
│  │ Resource quota missing     : -5 per namespace (max -15)          │ │
│  │ No LimitRange              : -3 per namespace (max -10)          │ │
│  └──────────────────────────────────────────────────────────────────┘ │
│                                                                        │
│  Bonuses:                                                              │
│  ┌──────────────────────────────────────────────────────────────────┐ │
│  │ Multi-zone spread          : +5                                  │ │
│  │ Multi-region               : +5                                  │ │
│  │ Service mesh enabled       : +3                                  │ │
│  │ GitOps deployed            : +2                                  │ │
│  └──────────────────────────────────────────────────────────────────┘ │
│                                                                        │
└────────────────────────────────────────────────────────────────────────┘
```

## Overall Health Score

```python
def calculate_overall_health(reliability, security, cost, architecture):
    """
    Calculate weighted overall health score.
    
    Weights reflect production priorities:
    - Reliability: 35% (uptime is critical)
    - Security: 30% (compliance and safety)
    - Cost: 20% (efficiency matters)
    - Architecture: 15% (long-term health)
    """
    weights = {
        'reliability': 0.35,
        'security': 0.30,
        'cost': 0.20,
        'architecture': 0.15
    }
    
    weighted_sum = (
        reliability * weights['reliability'] +
        security * weights['security'] +
        cost * weights['cost'] +
        architecture * weights['architecture']
    )
    
    # Apply floor effect for critical issues
    if security < 50:
        weighted_sum = min(weighted_sum, 60)  # Cap at 60 if security is critical
    if reliability < 50:
        weighted_sum = min(weighted_sum, 50)  # Cap at 50 if reliability is critical
    
    return round(weighted_sum)
```

## Recommendation Prioritization

### Priority Matrix

```
┌─────────────────────────────────────────────────────────────────────────┐
│                      RECOMMENDATION PRIORITY MATRIX                      │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                         │
│                           IMPACT                                        │
│                    Low      Medium      High                            │
│               ┌─────────┬──────────┬──────────┐                        │
│          Low  │    P4   │    P3    │    P2    │                        │
│   EFFORT      ├─────────┼──────────┼──────────┤                        │
│        Medium │    P4   │    P3    │    P2    │                        │
│               ├─────────┼──────────┼──────────┤                        │
│          High │    P5   │    P4    │    P3    │                        │
│               └─────────┴──────────┴──────────┘                        │
│                                                                         │
│  Risk Modifier:                                                         │
│  - Low risk: No change                                                  │
│  - Medium risk: +1 priority level                                       │
│  - High risk: +2 priority levels                                        │
│                                                                         │
│  Final Priority = base_priority + risk_modifier                         │
│                                                                         │
│  Priority Levels:                                                       │
│  P1: Critical - Immediate action required                               │
│  P2: High - Address within 24 hours                                     │
│  P3: Medium - Address within 1 week                                     │
│  P4: Low - Address within 1 month                                       │
│  P5: Informational - Nice to have                                       │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
```

### Quick Win Detection

```python
def is_quick_win(recommendation):
    """
    Identify quick wins - high impact, low effort, low risk.
    """
    return (
        recommendation.impact.level in ['high', 'medium'] and
        recommendation.effort == 'low' and
        recommendation.risk == 'low' and
        recommendation.confidence >= 0.8
    )
```

### Cost Savings Calculation

```python
def calculate_monthly_savings(recommendation):
    """
    Calculate potential monthly cost savings.
    
    Factors:
    - Resource reduction (CPU/Memory)
    - Instance type optimization
    - Storage cleanup
    - License consolidation
    """
    savings = 0.0
    
    # CPU savings
    if recommendation.type == 'cpu-right-sizing':
        cpu_reduction = recommendation.current_cpu - recommendation.target_cpu
        savings += cpu_reduction * COST_PER_VCPU_HOUR * 730  # hours/month
    
    # Memory savings
    if recommendation.type == 'memory-right-sizing':
        mem_reduction = recommendation.current_memory - recommendation.target_memory
        savings += mem_reduction * COST_PER_GB_HOUR * 730
    
    # Storage savings
    if recommendation.type == 'pvc-cleanup':
        storage_freed = recommendation.storage_gb
        savings += storage_freed * COST_PER_GB_MONTH
    
    # Spot/Preemptible opportunity
    if recommendation.type == 'spot-opportunity':
        on_demand_cost = recommendation.current_cost
        savings += on_demand_cost * 0.70  # Typical 70% savings
    
    return round(savings, 2)
```

## Confidence Scoring

### Evidence-Based Confidence

```python
def calculate_confidence(finding):
    """
    Calculate confidence score based on evidence quality.
    """
    score = 0.0
    
    # Metric evidence (most reliable)
    if finding.has_metric_evidence:
        score += 0.40
        if finding.metric_timespan_days >= 7:
            score += 0.10
    
    # Event correlation
    if finding.has_correlated_events:
        score += 0.20
    
    # Log evidence
    if finding.has_log_evidence:
        score += 0.15
    
    # Historical accuracy for this type
    historical_accuracy = get_historical_accuracy(finding.type)
    score += historical_accuracy * 0.15
    
    return min(score, 1.0)
```

### Confidence Levels

| Range | Level | Interpretation |
|-------|-------|----------------|
| 0.90 - 1.00 | Very High | Almost certain, multiple evidence sources |
| 0.75 - 0.89 | High | Confident, strong evidence |
| 0.60 - 0.74 | Medium | Likely, reasonable evidence |
| 0.40 - 0.59 | Low | Possible, limited evidence |
| 0.00 - 0.39 | Very Low | Speculative, needs investigation |

## Blast Radius Assessment

```python
def assess_blast_radius(affected_resources):
    """
    Assess the potential impact scope of an issue or change.
    """
    # Count affected components
    pods = len([r for r in affected_resources if r.kind == 'Pod'])
    deployments = len([r for r in affected_resources if r.kind == 'Deployment'])
    services = len([r for r in affected_resources if r.kind == 'Service'])
    
    # Check for critical namespaces
    critical_ns = ['kube-system', 'istio-system', 'monitoring', 'production']
    affects_critical = any(r.namespace in critical_ns for r in affected_resources)
    
    # Determine blast radius
    if affects_critical or deployments > 5 or services > 3:
        return 'cluster-wide'
    elif deployments > 2 or pods > 10:
        return 'multi-service'
    elif pods > 3:
        return 'single-service'
    else:
        return 'isolated'
```

## Score History & Trends

```sql
-- Track score changes over time
CREATE TABLE score_history (
    id SERIAL PRIMARY KEY,
    cluster_id VARCHAR(255) NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    overall_score INTEGER NOT NULL,
    reliability_score INTEGER NOT NULL,
    security_score INTEGER NOT NULL,
    cost_score INTEGER NOT NULL,
    architecture_score INTEGER NOT NULL,
    CONSTRAINT score_range CHECK (
        overall_score BETWEEN 0 AND 100 AND
        reliability_score BETWEEN 0 AND 100 AND
        security_score BETWEEN 0 AND 100 AND
        cost_score BETWEEN 0 AND 100 AND
        architecture_score BETWEEN 0 AND 100
    )
);

-- Index for efficient time-series queries
CREATE INDEX idx_score_history_cluster_time 
ON score_history(cluster_id, timestamp DESC);

-- Calculate trend
SELECT 
    cluster_id,
    overall_score - LAG(overall_score) OVER (
        PARTITION BY cluster_id ORDER BY timestamp
    ) as score_delta,
    CASE 
        WHEN overall_score > LAG(overall_score) OVER (
            PARTITION BY cluster_id ORDER BY timestamp
        ) THEN 'improving'
        WHEN overall_score < LAG(overall_score) OVER (
            PARTITION BY cluster_id ORDER BY timestamp
        ) THEN 'degrading'
        ELSE 'stable'
    END as trend
FROM score_history
WHERE timestamp > NOW() - INTERVAL '7 days'
ORDER BY cluster_id, timestamp DESC;
```

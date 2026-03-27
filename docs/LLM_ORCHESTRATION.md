# LLM Prompt Orchestration Strategy

## Overview

The LLM orchestration layer converts raw telemetry into actionable insights through structured prompts. This document defines the prompt engineering strategy, context management, and response parsing.

## Architecture

```
┌────────────────────────────────────────────────────────────────────────┐
│                      LLM ORCHESTRATION PIPELINE                         │
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐            │
│  │ Telemetry    │───►│ Context      │───►│ Prompt       │            │
│  │ Buffer       │    │ Builder      │    │ Template     │            │
│  └──────────────┘    └──────────────┘    └──────────────┘            │
│                                                 │                     │
│                                                 ▼                     │
│                           ┌──────────────────────────────────┐       │
│                           │        PROMPT ASSEMBLY           │       │
│                           │  ┌────────────────────────────┐  │       │
│                           │  │ System Prompt              │  │       │
│                           │  │ + Domain Context           │  │       │
│                           │  │ + Telemetry Data           │  │       │
│                           │  │ + Output Schema            │  │       │
│                           │  └────────────────────────────┘  │       │
│                           └──────────────────────────────────┘       │
│                                          │                           │
│                                          ▼                           │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐           │
│  │ Response     │◄───│ LLM          │◄───│ Rate         │           │
│  │ Parser       │    │ Gateway      │    │ Limiter      │           │
│  └──────────────┘    └──────────────┘    └──────────────┘           │
│         │                                                            │
│         ▼                                                            │
│  ┌──────────────────────────────────────────────────────────┐       │
│  │                  ANALYSIS DISPATCH                        │       │
│  │  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────────────┐│       │
│  │  │Reliab.  │ │Security │ │Cost     │ │Architecture     ││       │
│  │  │Engine   │ │Engine   │ │Engine   │ │Engine           ││       │
│  │  └─────────┘ └─────────┘ └─────────┘ └─────────────────┘│       │
│  └──────────────────────────────────────────────────────────┘       │
│                                                                      │
└────────────────────────────────────────────────────────────────────────┘
```

## Prompt Templates

### 1. Root Cause Analysis

```
SYSTEM:
You are a Kubernetes SRE expert analyzing cluster incidents. You have deep knowledge of:
- Kubernetes internals (scheduler, kubelet, controllers)
- Container runtime behavior
- Network policies and service mesh
- Resource management and QoS classes
- Common failure patterns and anti-patterns

Analyze incidents systematically:
1. Identify the primary symptom
2. Trace the causal chain
3. Consider contributing factors
4. Assess blast radius
5. Prioritize recommendations

Always provide confidence scores (0.0-1.0) for your assessments.

USER:
## Incident Context

**Cluster:** {cluster_id}
**Time Window:** {start_time} to {end_time}
**Focus Resource:** {resource_kind}/{resource_namespace}/{resource_name}

## Events (Last 30 minutes)
```yaml
{events_yaml}
```

## Resource Metrics
```json
{metrics_json}
```

## Recent Logs
```
{logs_excerpt}
```

## Service Topology
- Upstream dependencies: {dependencies}
- Downstream dependents: {dependents}

## Current State
- Pod Status: {pod_status}
- Container States: {container_states}
- Node Conditions: {node_conditions}

---

Analyze this incident and provide:
1. Root cause identification with confidence score
2. Contributing factors
3. Blast radius assessment
4. Prioritized remediation steps
5. Preventive measures

Output as JSON matching this schema:
```json
{
  "rootCause": {
    "primary": "string",
    "confidence": 0.0-1.0,
    "description": "string"
  },
  "contributingFactors": [...],
  "blastRadius": {...},
  "recommendations": [...],
  "preventiveMeasures": [...]
}
```
```

### 2. Resource Optimization Analysis

```
SYSTEM:
You are a Kubernetes resource optimization specialist. Analyze resource utilization patterns to identify:
- Over-provisioned workloads
- Under-provisioned workloads at risk
- Memory leak indicators
- CPU throttling patterns
- HPA/VPA tuning opportunities

Base recommendations on statistical analysis (P50, P95, P99) over meaningful time windows.
Consider business criticality and risk tolerance.

USER:
## Resource Analysis Request

**Cluster:** {cluster_id}
**Analysis Period:** {days} days
**Scope:** {namespace} / {workload_type}

## Current Resource Configuration
```yaml
{resource_config}
```

## Utilization Statistics (7-day)
| Metric | P50 | P95 | P99 | Max | Request | Limit |
|--------|-----|-----|-----|-----|---------|-------|
{metrics_table}

## HPA Configuration (if exists)
```yaml
{hpa_config}
```

## Observed Patterns
- Restart count: {restart_count}
- OOMKilled events: {oom_events}
- CPU throttle ratio: {throttle_ratio}
- Eviction events: {eviction_count}

## Cluster Context
- Node pool type: {node_pool}
- Cost per core/hour: ${cost_per_core}
- Cost per GB/hour: ${cost_per_gb}

---

Provide optimization recommendations:
1. Right-sizing suggestions with new values
2. Confidence level and risk assessment
3. Estimated cost impact
4. Implementation priority

Output as JSON:
```json
{
  "currentState": {
    "efficiency": 0.0-1.0,
    "risk": "low|medium|high"
  },
  "recommendations": [
    {
      "type": "cpu-request|memory-limit|hpa-config",
      "currentValue": "string",
      "recommendedValue": "string",
      "confidence": 0.0-1.0,
      "estimatedSavings": {
        "monthly": 0.0,
        "currency": "USD"
      },
      "risk": "low|medium|high",
      "reasoning": "string"
    }
  ]
}
```
```

### 3. Security Risk Assessment

```
SYSTEM:
You are a Kubernetes security expert conducting security assessments. Evaluate:
- RBAC configurations for least-privilege violations
- Network policy coverage and gaps
- Pod security standards compliance
- Container image vulnerabilities
- Secrets management practices
- CIS Kubernetes Benchmark alignment

Map findings to compliance frameworks (SOC2, PCI-DSS, HIPAA) where applicable.

USER:
## Security Assessment Request

**Cluster:** {cluster_id}
**Namespace:** {namespace}
**Assessment Type:** {assessment_type}

## RBAC Configuration
### ServiceAccounts
```yaml
{service_accounts}
```

### RoleBindings
```yaml
{role_bindings}
```

### ClusterRoleBindings
```yaml
{cluster_role_bindings}
```

## Network Policies
```yaml
{network_policies}
```

## Pod Security Context
```yaml
{pod_security}
```

## Container Images
| Image | Digest | Vulnerabilities | SBOM Available |
|-------|--------|-----------------|----------------|
{image_table}

## Secrets Usage
- Mounted secrets: {mounted_secrets}
- Environment secrets: {env_secrets}
- External secrets: {external_secrets}

---

Assess security posture and provide:
1. Risk findings with severity
2. Compliance gaps
3. Prioritized remediation
4. CIS benchmark mapping

Output as JSON:
```json
{
  "overallRisk": "critical|high|medium|low",
  "complianceScore": 0-100,
  "findings": [
    {
      "id": "string",
      "severity": "critical|high|medium|low",
      "category": "rbac|network|pod-security|image|secrets",
      "title": "string",
      "description": "string",
      "affectedResources": [...],
      "cisControl": "string",
      "compliance": ["SOC2", "PCI-DSS"],
      "remediation": "string"
    }
  ]
}
```
```

### 4. Cost Optimization Analysis

```
SYSTEM:
You are a cloud cost optimization specialist for Kubernetes. Analyze spending patterns to identify:
- Idle and abandoned resources
- Over-provisioned workloads
- Spot/preemptible instance candidates
- Storage optimization opportunities
- Right-sizing recommendations

Quantify savings in monthly USD. Consider reliability trade-offs.

USER:
## Cost Analysis Request

**Cluster:** {cluster_id}
**Cloud Provider:** {cloud_provider}
**Analysis Period:** {days} days

## Resource Utilization Summary
| Namespace | CPU Req | CPU Used | Mem Req | Mem Used | Est. Monthly |
|-----------|---------|----------|---------|----------|--------------|
{namespace_table}

## Idle Resources Detected
```yaml
{idle_resources}
```

## Storage Analysis
| PVC | Size | Used | Growth Rate | Last Access |
|-----|------|------|-------------|-------------|
{storage_table}

## Node Pool Configuration
```yaml
{node_pools}
```

## Current Spending
- Compute: ${compute_cost}/month
- Storage: ${storage_cost}/month
- Network: ${network_cost}/month
- Total: ${total_cost}/month

---

Provide cost optimization roadmap:
1. Quick wins (< 1 week, low risk)
2. Medium-term optimizations
3. Strategic recommendations
4. What-if scenarios

Output as JSON:
```json
{
  "currentSpend": {
    "monthly": 0.0,
    "breakdown": {...}
  },
  "potentialSavings": {
    "total": 0.0,
    "byCategory": {...}
  },
  "recommendations": [
    {
      "category": "right-sizing|idle|spot|storage",
      "title": "string",
      "description": "string",
      "affectedResources": [...],
      "monthlySavings": 0.0,
      "effort": "low|medium|high",
      "risk": "low|medium|high",
      "implementation": "string"
    }
  ],
  "whatIfScenarios": [...]
}
```
```

### 5. Architecture Quality Analysis

```
SYSTEM:
You are a Kubernetes architecture specialist. Evaluate cluster architecture for:
- Anti-patterns and design smells
- Resilience and fault tolerance gaps
- Scalability bottlenecks
- Service dependency risks
- Best practice violations

Consider cloud-native principles and 12-factor app methodology.

USER:
## Architecture Analysis Request

**Cluster:** {cluster_id}
**Scope:** {scope}

## Workload Topology
```yaml
{workload_topology}
```

## Affinity/Anti-affinity Rules
```yaml
{affinity_rules}
```

## Pod Disruption Budgets
```yaml
{pdbs}
```

## Service Dependencies
```mermaid
{dependency_graph}
```

## Scaling Configuration
```yaml
{scaling_config}
```

## Observed Issues
- Single points of failure: {spof_list}
- Noisy neighbors: {noisy_neighbors}
- Resource contention: {contention_issues}

---

Analyze architecture and provide:
1. Anti-pattern identification
2. Resilience gaps
3. Scalability concerns
4. Improvement recommendations

Output as JSON:
```json
{
  "architectureScore": 0-100,
  "antiPatterns": [
    {
      "pattern": "string",
      "severity": "high|medium|low",
      "description": "string",
      "affectedResources": [...],
      "recommendation": "string"
    }
  ],
  "resilienceGaps": [...],
  "scalabilityIssues": [...],
  "recommendations": [...]
}
```
```

## Context Management Strategy

### Token Budget Allocation

```
┌────────────────────────────────────────────────────────┐
│              TOKEN BUDGET (8K Context)                  │
├────────────────────────────────────────────────────────┤
│                                                        │
│  System Prompt:     800 tokens (10%)                   │
│  ├── Role definition                                   │
│  ├── Domain expertise                                  │
│  └── Output format                                     │
│                                                        │
│  Context Data:      5600 tokens (70%)                  │
│  ├── Events:        1500 tokens                        │
│  ├── Metrics:       1500 tokens                        │
│  ├── Logs:          1000 tokens                        │
│  ├── Config:        1000 tokens                        │
│  └── Topology:      600 tokens                         │
│                                                        │
│  Query:             400 tokens (5%)                    │
│                                                        │
│  Response Buffer:   1200 tokens (15%)                  │
│                                                        │
└────────────────────────────────────────────────────────┘
```

### Context Prioritization

When context exceeds budget, apply this priority:

1. **Critical** (always include):
   - Error events
   - Resource state
   - Direct metrics
   
2. **High** (include if space):
   - Warning events
   - Related pod metrics
   - Recent logs
   
3. **Medium** (summarize):
   - Info events
   - Historical metrics
   - Topology context
   
4. **Low** (omit if needed):
   - Labels/annotations
   - Extended history
   - Verbose logs

### Chunking Strategy

For large analysis tasks, use hierarchical chunking:

```
┌─────────────────────────────────────────────────────────┐
│                   CHUNKING STRATEGY                      │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  Level 1: Namespace Summary                             │
│  ┌─────────────────────────────────────────────────┐   │
│  │ Analyze each namespace independently            │   │
│  │ Output: Namespace-level findings                │   │
│  └─────────────────────────────────────────────────┘   │
│                        │                                │
│                        ▼                                │
│  Level 2: Cross-Namespace Correlation                   │
│  ┌─────────────────────────────────────────────────┐   │
│  │ Combine namespace findings                      │   │
│  │ Identify cross-cutting issues                   │   │
│  │ Output: Cluster-level insights                  │   │
│  └─────────────────────────────────────────────────┘   │
│                        │                                │
│                        ▼                                │
│  Level 3: Executive Summary                             │
│  ┌─────────────────────────────────────────────────┐   │
│  │ Synthesize all findings                         │   │
│  │ Prioritize recommendations                      │   │
│  │ Output: Action plan                             │   │
│  └─────────────────────────────────────────────────┘   │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

## Response Parsing

### JSON Extraction

```python
def parse_llm_response(raw_response: str) -> dict:
    """
    Extract structured JSON from LLM response.
    Handles markdown code blocks and partial JSON.
    """
    # Try direct JSON parse
    try:
        return json.loads(raw_response)
    except json.JSONDecodeError:
        pass
    
    # Extract from markdown code block
    json_match = re.search(r'```(?:json)?\s*([\s\S]*?)\s*```', raw_response)
    if json_match:
        try:
            return json.loads(json_match.group(1))
        except json.JSONDecodeError:
            pass
    
    # Try to find JSON object in text
    brace_match = re.search(r'\{[\s\S]*\}', raw_response)
    if brace_match:
        try:
            return json.loads(brace_match.group(0))
        except json.JSONDecodeError:
            pass
    
    # Return error structure
    return {
        "error": "Failed to parse response",
        "raw": raw_response[:500]
    }
```

### Validation Schema

```python
from pydantic import BaseModel, Field, validator
from typing import List, Optional
from enum import Enum

class Severity(str, Enum):
    CRITICAL = "critical"
    HIGH = "high"
    MEDIUM = "medium"
    LOW = "low"
    INFO = "info"

class RootCauseAnalysis(BaseModel):
    primary: str
    confidence: float = Field(ge=0.0, le=1.0)
    description: str
    
    @validator('confidence')
    def round_confidence(cls, v):
        return round(v, 2)

class Recommendation(BaseModel):
    action: str
    priority: int = Field(ge=1, le=10)
    effort: str
    description: str
    estimated_savings: Optional[float] = None

class IncidentAnalysis(BaseModel):
    root_cause: RootCauseAnalysis
    contributing_factors: List[dict]
    blast_radius: dict
    recommendations: List[Recommendation]
    preventive_measures: List[str]
```

## LLM Backend Configuration

### Supported Backends

```yaml
llm:
  # OpenAI API
  openai:
    enabled: true
    endpoint: "https://api.openai.com/v1"
    model: "gpt-4-turbo"
    apiKeySecret: "llm-credentials"
    apiKeyField: "openai-api-key"
    maxTokens: 4096
    temperature: 0.3
    
  # Azure OpenAI
  azure:
    enabled: false
    endpoint: "https://{resource}.openai.azure.com"
    deployment: "gpt-4"
    apiVersion: "2024-02-15-preview"
    apiKeySecret: "llm-credentials"
    apiKeyField: "azure-api-key"
    
  # Anthropic Claude
  anthropic:
    enabled: false
    endpoint: "https://api.anthropic.com"
    model: "claude-3-opus-20240229"
    apiKeySecret: "llm-credentials"
    apiKeyField: "anthropic-api-key"
    
  # Local Ollama
  ollama:
    enabled: false
    endpoint: "http://ollama.ai-system.svc:11434"
    model: "llama3:70b"
    
  # vLLM (self-hosted)
  vllm:
    enabled: false
    endpoint: "http://vllm.ai-system.svc:8000"
    model: "meta-llama/Llama-3-70b-chat-hf"
    
  # Generic OpenAI-compatible
  custom:
    enabled: false
    endpoint: "${LLM_ENDPOINT}"
    model: "${LLM_MODEL}"
    apiKeySecret: "llm-credentials"
    apiKeyField: "custom-api-key"

  # Fallback chain
  fallbackOrder:
    - openai
    - azure
    - ollama
```

### Rate Limiting

```yaml
rateLimits:
  # Per-backend limits
  openai:
    requestsPerMinute: 60
    tokensPerMinute: 150000
    
  azure:
    requestsPerMinute: 120
    tokensPerMinute: 300000
    
  ollama:
    requestsPerMinute: 30  # Local, conservative
    tokensPerMinute: 50000
    
  # Global limits
  global:
    maxConcurrentRequests: 10
    requestTimeoutSeconds: 60
    maxRetries: 3
    retryBackoffMs: [1000, 2000, 4000]
```

## Analysis Scheduling

```yaml
analysisSchedule:
  # Continuous analysis (event-driven)
  eventDriven:
    triggers:
      - type: "Warning"
        reasons: ["BackOff", "Unhealthy", "FailedScheduling"]
        debounceSeconds: 60
      - type: "Normal"
        reasons: ["Killing", "OOMKilled"]
        debounceSeconds: 30
        
  # Scheduled analysis
  scheduled:
    - name: "full-cluster-health"
      cron: "0 */6 * * *"  # Every 6 hours
      analysisTypes: ["reliability", "cost", "security"]
      
    - name: "resource-optimization"
      cron: "0 2 * * *"    # Daily at 2 AM
      analysisTypes: ["resource-optimization"]
      
    - name: "security-audit"
      cron: "0 3 * * 0"    # Weekly Sunday 3 AM
      analysisTypes: ["security-deep-scan"]
      
  # On-demand analysis
  onDemand:
    maxConcurrent: 3
    queueSize: 100
    timeoutMinutes: 30
```

## Confidence Scoring

### Score Calculation

```python
def calculate_confidence(
    llm_confidence: float,
    evidence_strength: float,
    historical_accuracy: float,
    data_completeness: float
) -> float:
    """
    Calculate final confidence score combining multiple factors.
    
    Args:
        llm_confidence: LLM's self-reported confidence (0-1)
        evidence_strength: Quality of supporting evidence (0-1)
        historical_accuracy: Past accuracy for similar analyses (0-1)
        data_completeness: Completeness of input data (0-1)
    
    Returns:
        Weighted confidence score (0-1)
    """
    weights = {
        'llm': 0.30,
        'evidence': 0.35,
        'historical': 0.20,
        'completeness': 0.15
    }
    
    score = (
        weights['llm'] * llm_confidence +
        weights['evidence'] * evidence_strength +
        weights['historical'] * historical_accuracy +
        weights['completeness'] * data_completeness
    )
    
    # Apply penalty for low data completeness
    if data_completeness < 0.5:
        score *= 0.8
    
    return round(min(score, 1.0), 2)
```

### Evidence Classification

| Evidence Type | Weight | Description |
|--------------|--------|-------------|
| Direct metric correlation | 1.0 | Metric directly shows the issue |
| Event correlation | 0.9 | Events align with analysis |
| Log evidence | 0.8 | Logs confirm the finding |
| Historical pattern | 0.7 | Similar past incidents |
| Inference | 0.5 | Logical deduction |
| Speculation | 0.3 | Possible but unconfirmed |

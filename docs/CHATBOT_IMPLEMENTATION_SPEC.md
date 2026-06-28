# AI Chatbot Implementation Specification

**Status:** Design Phase  
**Author:** hellodk  
**Last Updated:** 2026-06-20  
**Visual Design:** See `AI_CHATBOT_DESIGN.html`

---

## Table of Contents

1. [System Overview](#system-overview)
2. [API Contract](#api-contract)
3. [Tool Definitions](#tool-definitions)
4. [Embedding Strategy](#embedding-strategy)
5. [Request Lifecycle](#request-lifecycle)
6. [Safety & Guardrails](#safety--guardrails)
7. [Database Schema](#database-schema)
8. [Deployment](#deployment)

---

## System Overview

### Architecture
```
User → API Gateway → LLM Orchestrator → Tool Router → [Tools, Embeddings, Data Sources]
                                ↓
                        Conversation Store
                        (PostgreSQL)
```

### Key Components

| Component | Purpose | Tech |
|-----------|---------|------|
| **API Gateway** | Auth, rate limit, logging | FastAPI + middleware |
| **LLM Orchestrator** | Conversation management, tool routing | Claude API |
| **Tool Router** | Parse function calls, execute, error handling | Custom Python |
| **Embedding Engine** | Encode queries, vector search | all-MiniLM-L6-v2 |
| **Vector DB** | Store node embeddings | Qdrant |
| **Cache** | Hot embeddings, recent searches | Redis |
| **Conversation Store** | Thread history, user feedback | PostgreSQL |

---

## API Contract

### Endpoint: POST /api/v1/chat

**Authentication:** Bearer token or API key

**Request Body:**
```json
{
  "message": "What's causing the high error rate in the collector?",
  "conversation_id": "conv_abc123",  // optional; auto-create if missing
  "stream": true,
  "context": {
    "namespace": "default",          // optional; inferred from conversation if exists
    "service": "collector",          // optional
    "incident_id": "INC-1234"        // optional; links to existing incident
  }
}
```

**Response (Streaming JSON Lines):**
```jsonl
{"type":"thinking","content":"Analyzing the query...","timestamp":"2026-06-20T12:00:00Z"}
{"type":"tool_call","tool":"get_pod_status","params":{"namespace":"default","pod_name":"collector-*"},"call_id":"call_1"}
{"type":"tool_result","call_id":"call_1","result":{"pods":[...],"summary":"5 pods, 3 restarting"},"duration_ms":245}
{"type":"tool_call","tool":"search_knowledge_graph","params":{"query":"collector error handling restart","limit":5},"call_id":"call_2"}
{"type":"tool_result","call_id":"call_2","result":{"nodes":[...],"scores":[0.92,0.87,0.81]},"duration_ms":52}
{"type":"response","content":"The collector is experiencing memory pressure...","sources":[{"node_id":"pkg_collector","label":"Collector","relevance":0.92}],"suggested_actions":[{"action":"increase_memory_limit","description":"Raise to 1GB","requires_confirmation":true}]}
{"type":"done","conversation_id":"conv_abc123","message_id":"msg_xyz789"}
```

**Streaming Event Types:**
- `thinking` — LLM is reasoning
- `tool_call` — Function call issued with params
- `tool_result` — Result from tool execution
- `response` — Final LLM response (not streamed character-by-character; one event)
- `done` — End of stream
- `error` — Tool or LLM error

---

## Tool Definitions

### Category A: Information Retrieval

#### 1. search_knowledge_graph
```python
def search_knowledge_graph(
    query: str,                    # User query or expanded query
    limit: int = 5,               # Top-K results (1-10)
    score_threshold: float = 0.7, # Minimum similarity score
    filter_communities: Optional[List[int]] = None  # Filter by community IDs
) -> SearchResult:
    """
    Semantic search across knowledge graph nodes using embeddings.
    
    Returns:
    {
      "nodes": [
        {
          "node_id": "pkg_collector",
          "label": "Collector",
          "similarity_score": 0.92,
          "community_id": 3,
          "snippet": "Collects telemetry from cluster...",
          "source_file": "docs/ARCHITECTURE.md"
        },
        ...
      ],
      "search_time_ms": 45,
      "reranked": true  // Used BM25 secondary ranking
    }
    """
```

**Safety:** Read-only. No side effects.

**Latency:** 50-100ms (with caching).

---

#### 2. get_pod_status
```python
def get_pod_status(
    namespace: str,
    pod_name: str,  # glob pattern: "collector-*"
    include_logs: bool = False
) -> PodStatus:
    """
    Fetch current pod status, resource usage, recent events.
    
    Returns:
    {
      "pods": [
        {
          "name": "collector-xyz123",
          "phase": "Running" | "Pending" | "Failed" | "Unknown",
          "status_conditions": [
            {"type": "Ready", "status": "True", "reason": "PodReady", "message": "..."}
          ],
          "containers": [
            {
              "name": "collector",
              "state": "Running",
              "restart_count": 5,
              "last_state": {"terminated": {"exit_code": 1, "reason": "OOMKilled"}},
              "resources": {
                "cpu_usage": "250m",
                "memory_usage": "512Mi",
                "limits": {"cpu": "500m", "memory": "512Mi"}
              }
            }
          ],
          "recent_events": [
            {"timestamp": "2026-06-20T11:50:00Z", "type": "Warning", "reason": "BackOff", "message": "Back-off restarting failed container..."},
          ]
        }
      ],
      "query_time_ms": 180
    }
    """
```

**Safety:** Read-only. Kubectl access required (read-only role).

**Latency:** 200-500ms.

---

#### 3. query_prometheus
```python
def query_prometheus(
    query: str,           # PromQL query (e.g., "rate(errors[5m])")
    range_duration: str = "1h",  # "1h", "24h", "7d"
    step: str = "1m"     # Resolution
) -> MetricsResult:
    """
    Execute PromQL query against Prometheus.
    
    Returns:
    {
      "query": "rate(http_requests_total[5m])",
      "results": [
        {
          "metric": {"job": "collector", "handler": "/health"},
          "values": [
            [1718876400, "123.45"],
            [1718876460, "125.67"],
            ...
          ]
        }
      ],
      "query_time_ms": 320,
      "data_points": 60
    }
    """
```

**Safety:** Read-only. Prometheus API auth via service account.

**Latency:** 300-600ms (includes range queries).

---

#### 4. get_logs
```python
def get_logs(
    service: str,
    namespace: str = "default",
    since: str = "1h",        # "5m", "1h", "24h"
    limit: int = 100,         # Max lines returned
    level: Optional[str] = None  # "ERROR", "WARN", "INFO"
) -> LogsResult:
    """
    Fetch logs from Loki with optional filtering.
    
    Returns:
    {
      "logs": [
        {
          "timestamp": "2026-06-20T11:50:23.456Z",
          "level": "ERROR",
          "message": "Failed to publish event to NATS",
          "trace_id": "trace_abc123",
          "service": "collector",
          "pod": "collector-xyz123"
        },
        ...
      ],
      "total_lines": 2450,
      "truncated": true,
      "query_time_ms": 240
    }
    """
```

**Safety:** Read-only. Loki auth via service account.

**Latency:** 200-400ms.

---

#### 5. list_incidents
```python
def list_incidents(
    status: Literal["open", "resolved", "all"] = "open",
    sort_by: str = "severity",
    limit: int = 20
) -> IncidentsResult:
    """
    Fetch recent incidents from dashboard API.
    
    Returns:
    {
      "incidents": [
        {
          "id": "INC-2024-001",
          "title": "Collector OOMKilled - memory leak in NATS",
          "severity": "high",
          "component": "collector",
          "status": "open",
          "first_seen": "2026-06-20T11:00:00Z",
          "last_seen": "2026-06-20T12:15:00Z",
          "error_rate_spike": 0.45,
          "duration_seconds": 4500
        },
        ...
      ],
      "total_open": 3,
      "query_time_ms": 85
    }
    """
```

**Safety:** Read-only. Dashboard API auth.

**Latency:** 100-200ms.

---

### Category B: Analysis

#### 6. analyze_pod_health
```python
def analyze_pod_health(
    pod_id: str,                    # "collector-xyz123"
    symptom: Literal["crashed", "slow", "memory-leak", "cpu-spike"],
    details: Optional[str] = None   # Additional context
) -> AnalysisResult:
    """
    Call RCA analyzer microservice to diagnose pod issue.
    
    Returns:
    {
      "pod": "collector-xyz123",
      "root_causes": [
        {
          "cause": "NATS client not closing connections",
          "confidence": 0.92,
          "evidence": [
            "Connection count increasing linearly",
            "Memory usage correlates with uptime",
            "No connection reset events logged"
          ],
          "affected_component": "pkg/collector/nats.go",
          "suggested_fixes": [
            "Add defer conn.Close() in main loop",
            "Implement connection pooling with max_idle_time"
          ]
        }
      ],
      "timeline": [
        {"time": "T-30m", "event": "Pod started"},
        {"time": "T-5m", "event": "Memory usage > 80%"},
        {"time": "T0", "event": "OOMKilled"}
      ],
      "severity": "critical"
    }
    """
```

**Safety:** Read-only analysis. No changes made.

**Latency:** 800-2000ms (may involve LLM calls).

---

#### 7. correlate_metrics
```python
def correlate_metrics(
    start_time: str,    # ISO8601
    end_time: str,      # ISO8601
    service: str
) -> CorrelationResult:
    """
    Find correlated metrics during incident window.
    
    Returns:
    {
      "window": {"start": "2026-06-20T11:00:00Z", "end": "2026-06-20T12:00:00Z"},
      "correlations": [
        {
          "metric": "process_resident_memory_bytes",
          "correlation_score": 0.98,
          "spike_details": {
            "baseline": "512MB",
            "peak": "1024MB",
            "spike_start": "2026-06-20T11:30:00Z"
          },
          "likely_cause": "Memory leak in NATS client"
        },
        {
          "metric": "rate(nats_connection_count)",
          "correlation_score": 0.95,
          ...
        }
      ]
    }
    """
```

**Safety:** Read-only.

**Latency:** 1000-3000ms (multi-metric correlation).

---

### Category C: Actions

#### 8. restart_pod (Requires Confirmation)
```python
def restart_pod(
    namespace: str,
    pod_name: str,
    confirmation_token: str  # Must match user confirmation
) -> ActionResult:
    """
    Gracefully restart pod via kubectl delete.
    
    Returns:
    {
      "action": "restart_pod",
      "pod": "collector-xyz123",
      "status": "initiated",
      "new_pod_name": "collector-abc789",
      "grace_period": 30,
      "message": "Pod deletion initiated. New pod starting."
    }
    """
```

**Safety:** ⚠️ **Medium** — Restarts pod, triggers brief service interruption (30s).

**Confirmation Required:** User must acknowledge summary.

**Audit:** Logged with user, timestamp, reason.

---

#### 9. scale_deployment (Requires Confirmation)
```python
def scale_deployment(
    namespace: str,
    deployment: str,
    new_replicas: int,
    confirmation_token: str
) -> ActionResult:
    """
    Change replica count for deployment.
    
    Returns:
    {
      "action": "scale_deployment",
      "deployment": "collector",
      "old_replicas": 1,
      "new_replicas": 3,
      "status": "scaled",
      "affected_pods": ["collector-xyz", "collector-abc", "collector-def"],
      "estimated_readiness_time_seconds": 45
    }
    """
```

**Safety:** ⚠️ **Medium** — Affects pod count, may increase resource usage.

**Confirmation Required:** User must confirm old/new replica counts.

---

#### 10. drain_node (Requires Strong Confirmation)
```python
def drain_node(
    node_name: str,
    confirmation_token: str  # User must type "DRAIN"
) -> ActionResult:
    """
    Drain a node for maintenance.
    
    Returns:
    {
      "action": "drain_node",
      "node": "node-1",
      "status": "draining",
      "pods_to_evict": ["pod-1", "pod-2", "pod-3"],
      "expected_duration_seconds": 120
    }
    """
```

**Safety:** 🔴 **High** — Evicts all workloads. Service disruption risk.

**Confirmation Required:** User must type "DRAIN" (case-sensitive).

**Audit:** Logged with explicit user authorization.

---

#### 11. create_incident_ticket
```python
def create_incident_ticket(
    title: str,
    description: str,
    severity: Literal["critical", "high", "medium", "low"],
    component: str,
    related_incident_ids: Optional[List[str]] = None
) -> TicketResult:
    """
    Create GitHub issue for tracking.
    
    Returns:
    {
      "ticket_id": "INC-2024-042",
      "url": "https://github.com/hellodk/k8s-cluster-health/issues/1234",
      "status": "created",
      "message": "Incident tracked as issue #1234"
    }
    """
```

**Safety:** 🟢 **Low** — Creates read-only ticket.

**Confirmation Required:** Auto-approved. Summary shown to user.

---

## Embedding Strategy

### Model Choice
**Primary:** `all-MiniLM-L6-v2`
- 384 dimensions
- 10ms latency per query
- Self-hosted (no API calls)
- Production-ready

**Fallback:** OpenAI `text-embedding-3-small` (higher quality, slower)

### Indexing Pipeline

1. **Pre-compute Phase** (triggered on `/graphify update`)
   ```python
   for node in graph.nodes:
       embedding = model.encode(node.label + " " + node.description)
       qdrant.upsert(node_id, embedding, metadata=node)
   ```

2. **Cache Phase**
   ```python
   redis.set(f"embedding:{query_hash}", embedding, ttl=86400)
   redis.set(f"search:{query_hash}", results, ttl=3600)
   ```

3. **Query Phase**
   ```python
   # Check cache first
   if cached := redis.get(f"embedding:{query_hash}"):
       vector = cached
   else:
       vector = model.encode(query)
   
   # Vector search
   results = qdrant.search(vector, limit=10)
   
   # Rerank with BM25
   reranked = bm25.rank(results, query)
   ```

### Query Expansion
```python
def expand_query(user_query: str) -> str:
    """
    LLM generates synonyms and related terms.
    
    Input: "Pod keeps crashing"
    Output: "Pod CrashLoopBackOff restart failure container error"
    """
    expansion = llm(f"Generate related terms: {user_query}")
    return user_query + " " + expansion
```

---

## Request Lifecycle

### Complete Flow: Incident Diagnosis

```
1. User: "Collector pod keeps restarting"
   ↓
2. API Gateway validates request, logs trace_id
   ↓
3. Load balancer routes to API pod
   ↓
4. Orchestrator:
   - Loads/creates conversation
   - Builds system prompt with user context
   - Calls Claude with conversation history + tools
   ↓
5. Claude responds with tool calls (parallel):
   a) get_pod_status(namespace="default", pod_name="collector-*")
   b) search_knowledge_graph(query="collector error handling")
   c) get_logs(service="collector", since="30m")
   ↓
6. Tool Router executes in parallel:
   a) Kubectl API → returns { status: Running, restarts: 5, last_error: "OOMKilled" }
   b) Qdrant search → returns [ {node: "collector", score: 0.92}, ... ]
   c) Loki query → returns [ {timestamp, level, message}, ... ]
   ↓
7. Orchestrator streams results to client as they complete
   ↓
8. Claude analyzes results, formulates response:
   "Root cause: Memory leak in NATS (per docs). Recommended: increase limit to 1GB."
   ↓
9. LLM streams response to client
   ↓
10. User reads response, decides to act or investigate further
    ↓
11. If action needed:
    - User: "Go ahead and patch it"
    - LLM: "I'll increase memory limit to 1GB. Confirm? Y/N"
    - User: "Y"
    - LLM calls: scale_pod_memory(pod="collector-*", limit="1Gi")
    - Tool Router executes, confirms success
    ↓
12. Conversation stored in PostgreSQL
    - message_id, user_id, timestamp, content
    - tool_calls and results
    - user_feedback (thumbs up/down)
    ↓
13. Feedback loop (async):
    - If user gives negative feedback, re-embed related nodes
    - Update conversation rating in vector DB
    - Log for fine-tuning dataset
```

---

## Safety & Guardrails

### Request-Level
- **Rate Limit:** 10 requests/min per user
- **Timeout:** 30s per request
- **Token Budget:** Max 2000 tokens per LLM call
- **Tool Timeout:** 5s per tool execution

### Tool-Level
- **Read vs. Write:** All tools return read-only by default
- **Destructive Confirmation:** `restart_pod`, `scale_deployment` require explicit user confirmation
- **High-Risk Confirmation:** `drain_node` requires user to type "DRAIN"
- **Permission Checks:** Tool router validates user role before execution

### LLM-Level
- **Tool Schema Validation:** LLM must follow exact function signature
- **Output Validation:** Tool results validated before returning to LLM
- **Prompt Injection Prevention:** Tool parameters sanitized; LLM cannot construct arbitrary commands
- **Hallucination Check:** If tool call fails, LLM is told explicitly (not inventing results)

### Audit Trail
```
All actions logged to PostgreSQL audit_log table:
- user_id, timestamp
- action (tool_name or conversation_update)
- input parameters
- result (success/error)
- duration_ms
- user_feedback (if provided)
```

---

## Database Schema

### PostgreSQL: Conversations
```sql
CREATE TABLE conversations (
    id UUID PRIMARY KEY,
    user_id VARCHAR(255) NOT NULL,
    created_at TIMESTAMP DEFAULT NOW(),
    last_message_at TIMESTAMP,
    title VARCHAR(255),
    context JSONB  -- {"namespace": "default", "service": "collector"}
);

CREATE TABLE messages (
    id UUID PRIMARY KEY,
    conversation_id UUID REFERENCES conversations(id),
    role ENUM('user', 'assistant'),
    content TEXT,
    created_at TIMESTAMP DEFAULT NOW(),
    tool_calls JSONB,  -- [{tool: "...", params: {...}}]
    tool_results JSONB,
    user_feedback ENUM('positive', 'negative', null)
);

CREATE TABLE audit_log (
    id UUID PRIMARY KEY,
    user_id VARCHAR(255),
    action VARCHAR(255),
    input JSONB,
    result JSONB,
    duration_ms INT,
    timestamp TIMESTAMP DEFAULT NOW()
);
```

### Qdrant: Vector Collections
```
Collection: graph_nodes
- Vector: 384-dim embedding
- Metadata: {node_id, label, community_id, source_file}
- Index: HNSW (approximate nearest neighbor)

Collection: query_cache (optional)
- Vector: embedding of recent queries
- Metadata: {query_hash, results, timestamp}
```

---

## Deployment

### Development Setup
```bash
# 1. Clone & install dependencies
git clone ...
cd k8s-cluster-health
pip install -r requirements-chatbot.txt

# 2. Start services
docker-compose up -d  # Redis, Qdrant, PostgreSQL

# 3. Pre-compute embeddings
python scripts/index_knowledge_graph.py

# 4. Start API server
python -m uvicorn app.chatbot.api:app --reload --port 8000

# 5. Test endpoint
curl -X POST http://localhost:8000/api/v1/chat \
  -H "Authorization: Bearer dev-token" \
  -H "Content-Type: application/json" \
  -d '{"message":"What is the collector?"}'
```

### Production Deployment (Kubernetes)
See `AI_CHATBOT_DESIGN.html` Tab 6 for full architecture.

Key resources:
```yaml
# Deploy API replicas
kubectl apply -f deploy/chatbot/api-deployment.yaml

# Deploy embedding service
kubectl apply -f deploy/chatbot/embedding-service.yaml

# Deploy Redis + Qdrant (or use external)
kubectl apply -f deploy/chatbot/data-services.yaml

# Expose via Ingress
kubectl apply -f deploy/chatbot/ingress.yaml

# Check status
kubectl get pods -n chatbot
kubectl logs -f deployment/chatbot-api -n chatbot
```

---

## Next Steps

1. **Week 1-2:** Implement API Gateway + basic LLM orchestrator
2. **Week 3:** Integrate embeddings + knowledge graph search
3. **Week 4:** Add tools (kubectl, Prometheus, logs)
4. **Week 5:** Add destructive tools with confirmation
5. **Week 6+:** Testing, monitoring, iteration

---

**Questions?** Open an issue or ping @hellodk in Slack.

# Operator Chatbot Framework — Reusable Pattern

This document defines a **domain-agnostic chatbot framework** that both `k8s-cluster-health` and `kri` projects can use. The core pattern remains identical; only the **Tools** and **System Prompt** change per domain.

---

## Framework Architecture

### Core Components (Domain-Agnostic)

```
┌─────────────────────────────────────────────────────────────┐
│  LLM Orchestrator (FastAPI + async/await)                   │
│  - Conversation memory (in-memory or Redis)                 │
│  - Tool executor (parallel execution)                       │
│  - Streaming JSON-lines response handler                    │
└─────────────────────────────────────────────────────────────┘
         ↓
┌─────────────────────────────────────────────────────────────┐
│  Tool Router (Configuration-driven)                          │
│  - Load tools from YAML/env                                 │
│  - Execute tools in parallel                                │
│  - Return structured results                                │
└─────────────────────────────────────────────────────────────┘
         ↓
┌─────────────────────────────────────────────────────────────┐
│  Model Router (Configuration-driven)                         │
│  - Local endpoint (192.168.1.19:8080)                       │
│  - Cloud fallback (Claude, GPT-4, etc.)                     │
│  - Health checks & failover                                 │
└─────────────────────────────────────────────────────────────┘
```

### Configuration-Driven Domain Adaptation

Each project provides:
1. **System Prompt** — domain-specific role (Kubernetes operator vs. Fleet operations engineer)
2. **Tools** — domain-specific tools (kubectl vs. SSH commands)
3. **Knowledge Base** — domain-specific docs (k8s architecture vs. fleet baseline)
4. **Model Config** — LLM/embedding endpoints (same local server for both)

---

## Implementation Pattern

### Step 1: Create Domain-Specific Config

```yaml
# config/chatbot-models.yaml
domain: "kubernetes"  # or "fleet"
model_server: "192.168.1.19:8080"

system_prompt: |
  You are an expert Kubernetes operator. Answer questions about
  pod health, metrics, logs, and cluster incidents.
  
tools:
  - name: "get_pod_status"
    description: "List pods in a namespace"
  - name: "get_logs"
    description: "Fetch logs from a pod"
  # ... domain-specific tools

knowledge_base:
  docs_path: "./docs/architecture/"
  embedding_model: "all-MiniLM-L6-v2"
```

### Step 2: Implement Domain-Specific Tools

```python
# tools/kubernetes_tools.py OR tools/fleet_tools.py
class DomainTools:
    async def tool_1(self, **args):
        """Implementation specific to domain"""
        pass
    
    async def tool_2(self, **args):
        """Another domain tool"""
        pass
```

### Step 3: Compose with Framework

```python
# main.py (same for both projects)
from chatbot_framework import LLMOrchestrator, ModelRouter

# Load domain config
config = load_config("config/chatbot-models.yaml")

# Instantiate domain tools
tools = DomainTools(config)

# Initialize orchestrator (framework)
orchestrator = LLMOrchestrator(
    model_router=ModelRouter(config),
    tools=tools,
    config=config
)

# FastAPI routes (identical for both projects)
@app.post("/api/v1/chat")
async def chat(request: ChatRequest):
    async for chunk in orchestrator.stream_response(request):
        yield chunk
```

---

## Reusable Components

### 1. `chatbot_framework/orchestrator.py`
- Conversation state management
- Tool execution & result aggregation
- LLM communication (streaming)
- Response formatting (JSON-lines)
- **No domain-specific logic**

### 2. `chatbot_framework/model_router.py`
- Local endpoint health checks
- Cloud fallback (Claude/GPT-4)
- Request routing (which model to use)
- **No domain-specific logic**

### 3. `chatbot_framework/base_tools.py`
- Abstract base class for domain tools
- Tool result schema
- Parallel execution harness
- **No domain-specific logic**

### 4. `chatbot_framework/api.py`
- FastAPI endpoints (POST /api/v1/chat, GET /conversations, etc.)
- Bearer token auth
- Streaming response handlers
- Error handling
- **No domain-specific logic**

### 5. `chatbot_framework/cli.py`
- Interactive operator CLI
- Streaming response parsing
- Command history
- **No domain-specific logic**

---

## Project-Specific Structure

### For `k8s-cluster-health`
```
src/chatbot/
├── config/
│   └── chatbot-models.yaml
├── tools/
│   └── kubernetes_tools.py       ← Domain-specific
├── main.py                        ← Uses framework
├── cli.py                         ← Uses framework
├── requirements.txt
└── Dockerfile
```

### For `kri`
```
src/chatbot/
├── config/
│   └── chatbot-models.yaml       ← Different config
├── tools/
│   └── fleet_tools.py            ← Domain-specific
├── main.py                        ← Identical to k8s-cluster-health
├── cli.py                         ← Identical to k8s-cluster-health
├── requirements.txt
└── Dockerfile
```

---

## Deployment Parity

### Both Projects Deploy Identically

```bash
# Local (docker-compose)
./scripts/chatbot-start.sh local

# Kubernetes
./scripts/chatbot-start.sh k8s
```

### Config Differences Only

| Aspect | k8s-cluster-health | kri |
|--------|-------------------|-----|
| **Domain** | Kubernetes | Fleet |
| **Tools** | kubectl, Prometheus, Loki | SSH, Salt, Ansible |
| **Knowledge Base** | k8s architecture docs | Fleet baseline, playbooks |
| **System Prompt** | Kubernetes operator | Fleet operations engineer |
| **Startup Script** | Identical | Identical |
| **API Endpoints** | Identical | Identical |
| **CLI** | Identical | Identical |

---

## Benefits

✅ **Code Reuse**: Framework lives in a shared library or monorepo  
✅ **Consistency**: Both projects have identical API contracts  
✅ **Maintenance**: One framework, many domains  
✅ **Onboarding**: New projects use the same chatbot pattern  
✅ **Testing**: Framework tests apply to all projects  

---

## Rollout Plan

### Phase 1: Extract Framework (Week 1)
- Move `orchestrator.py`, `model_router.py`, `base_tools.py`, `api.py`, `cli.py` to a shared location
- Publish as `chatbot_framework` pip package (internal)
- Keep k8s-cluster-health's implementation as the reference

### Phase 2: Adapt kri (Week 2)
- Create `src/chatbot/tools/fleet_tools.py` (SSH, Salt, Ansible)
- Write `config/chatbot-models.yaml` for kri domain
- Use framework; update `main.py` to import from `chatbot_framework`
- Test both projects with framework

### Phase 3: Generalize (Week 3+)
- Document the framework
- Create starter template for new projects
- Share with team as the "operator chatbot" standard

---

## Example: Two Operators, Same Framework

### k8s-cluster-health Operator
```
User: "Why is the collector restarting?"
→ Tool: get_pod_status(pod=collector, ns=default)
→ Tool: get_logs(pod=collector, ns=default)
→ LLM: "Your collector is restarting because..."
```

### kri Fleet Operator
```
User: "Which Mac Minis are off baseline?"
→ Tool: check_node_baseline(node=mac-1)
→ Tool: check_node_baseline(node=mac-2)
→ Tool: search_fleet_docs(query="baseline compliance")
→ LLM: "Nodes mac-1 and mac-2 are drifted because..."
```

**Same framework, different tools, same operator experience.**

---

## Next Steps

1. Confirm both projects adopt this pattern
2. Extract framework components into shared module
3. Implement kri chatbot using the framework
4. Document tool-writing guide for new domains

# k8s-cluster-health Chatbot

**AI Assistant for operators to query and troubleshoot the Kubernetes cluster**

Powered by local LLM (Mistral/Llama) + knowledge graph + real-time cluster data.

---

## Quick Start

### Prerequisites

- Local model server running at `192.168.1.19:8080` (Ollama, vLLM, or Text Generation WebUI)
- Kubernetes cluster access with kubeconfig
- Python 3.9+
- Docker (for containerized deployment)

### Option 1: Local Development (Docker Compose)

```bash
# From project root
docker-compose -f docker-compose.chatbot.yml up -d

# Test the server
curl http://localhost:8000/health

# Run CLI client
export TOKEN="your-token"
python src/chatbot/cli.py --token $TOKEN
```

### Option 2: Kubernetes Deployment

```bash
# Deploy to cluster
kubectl apply -f deploy/k8s/chatbot-deployment.yaml

# Verify
kubectl get pods -n chatbot

# Port-forward for local testing
kubectl port-forward -n chatbot svc/chatbot-api 8000:8000

# Use CLI client
python src/chatbot/cli.py --server http://localhost:8000 --token $TOKEN
```

---

## Usage

### Interactive CLI

```bash
python src/chatbot/cli.py --token $TOKEN

# Or with specific server
python src/chatbot/cli.py \
  --server http://chatbot-api.chatbot.svc.cluster.local:8000 \
  --token $TOKEN \
  --namespace default
```

**Commands:**
- Type your question and press Enter
- `help` — Show available commands
- `history` — Show conversation history
- `namespace <ns>` — Switch namespace
- `exit` — Exit the chat

### Example Queries

#### Pod & Status Queries
```
You: What pods are failing?
You: Show me the status of all pods in kube-system
You: Why is the collector pod restarting?
You: How many restarts has the analyzer had?
```

#### Metrics & Performance
```
You: What's using the most CPU?
You: Show me memory usage for the past hour
You: Are there any memory leaks?
You: What's the error rate in the API?
```

#### Incident & Troubleshooting
```
You: What are the recent incidents?
You: Why did the dashboard go down?
You: Analyze the logs for the collector service
You: What changes happened in the last 30 minutes?
```

#### Architecture & Knowledge
```
You: How does the scoring system work?
You: Tell me about error handling in the collector
You: What's the architecture of the dashboard?
You: How do the LLM orchestration components interact?
```

#### Combined Analysis
```
You: The collector is down. What's the root cause?
You: Performance degradation - investigate

# The chatbot will:
# 1. Check pod status
# 2. Query logs
# 3. Search metrics
# 4. Check incidents
# 5. Synthesize findings from knowledge graph
# 6. Recommend solutions
```

---

## Example Conversation

```
You: What services are failing?

🤖 Assistant:
I'll check the current status of all services in your cluster.

Let me examine:
1. Pod status across namespaces
2. Recent incidents
3. Error metrics

Based on the data:
- **collector pod (default)**: CrashLoopBackOff - restarted 12 times in 2 hours
- **analyzer pod (default)**: Running but high CPU (95%)
- **dashboard pod (default)**: Running, healthy

**Root Cause Analysis:**
The collector is experiencing a memory leak. Logs show:
  "memory exceeded 512MB limit"
  
Per the architecture docs, this is a known issue in the NATS client connection handling.

**Recommended Actions:**
1. Increase memory limit to 1GB: kubectl set resources deployment/collector -n default --limits=memory=1Gi
2. Review pkg/collector/nats.go line 142 for the connection cleanup bug
3. Redeploy after patching

Would you like me to check anything else?
---

You: What's the overall health score?

🤖 Assistant:
Based on current metrics:
- Reliability: 65% (pod restarts, errors)
- Security: 92% (RBAC policies intact)
- Cost Efficiency: 78% (resource usage reasonable)
- Architecture: 85% (system design sound)

**Overall Cluster Health: 80/100 (Fair)**

The collector issue is the primary concern. Fixing that will bring you to ~88/100.
```

---

## API Reference

### Chat Endpoint

**POST** `/api/v1/chat`

Stream a response from the chatbot.

**Request:**
```bash
curl -X POST http://localhost:8000/api/v1/chat \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "message": "What pods are failing?",
    "conversation_id": "conv_abc123",
    "namespace": "default"
  }'
```

**Response:** (Server-Sent Events / JSON Lines)
```json
{"type":"chunk","content":"I'll check"}
{"type":"chunk","content":" the status"}
...
{"type":"done"}
```

### Get Conversation

**GET** `/api/v1/conversations/{conversation_id}`

Retrieve conversation history.

```bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8000/api/v1/conversations/conv_abc123
```

### Health Check

**GET** `/health`

```bash
curl http://localhost:8000/health
```

Response:
```json
{
  "status": "healthy",
  "llm_endpoint": "http://192.168.1.19:8080/v1/chat/completions",
  "embedding_endpoint": "http://192.168.1.19:8080/embeddings"
}
```

---

## Available Tools

The chatbot can call these tools to gather data:

| Tool | Purpose | Example |
|------|---------|---------|
| `get_pod_status` | Check pod health, restarts, conditions | "What pods are failing?" |
| `get_logs` | Fetch service logs with filtering | "Show me error logs for the collector" |
| `query_metrics` | Query Prometheus for metrics | "What's the error rate?" |
| `get_incidents` | List recent incidents from dashboard | "What are the recent incidents?" |
| `search_knowledge_graph` | Search architecture docs by semantic similarity | "How does scoring work?" |

---

## Configuration

### Model Configuration

Edit `config/chatbot-models.yaml`:

```yaml
embedding:
  provider: "local"
  local:
    endpoint: "http://192.168.1.19:8080/embeddings"
    model_name: "all-MiniLM-L6-v2"

llm:
  provider: "local"
  local:
    endpoint: "http://192.168.1.19:8080/v1/chat/completions"
    model_name: "mistral-7b"  # Change to llama2, neural-chat, etc.
```

### Using Different LLM Models

The local server (192.168.1.19:8080) should expose:
- `Mistral 7B` (fast, good reasoning)
- `Llama 2 13B` (accurate, slower)
- `Neural Chat 7B` (optimized for conversations)

Switch by updating `model_name` in config:
```yaml
llm:
  local:
    model_name: "llama2-13b"  # More accurate but slower
```

### Cloud Fallback (Optional)

If local model is down, automatically fall back to Claude:

```yaml
llm:
  cloud:
    enabled: true
    provider: "anthropic"
    api_key: "${ANTHROPIC_API_KEY}"
    model_name: "claude-3-sonnet"
```

---

## Troubleshooting

### "Connection refused" error

**Problem:** Chatbot can't reach the model server

**Solution:**
```bash
# Check if model server is running
curl http://192.168.1.19:8080/models

# If not, start it (Ollama example)
docker run -d -p 8080:11434 ollama/ollama
ollama pull mistral
```

### "Pod not found" error in logs

**Problem:** Chatbot can't access Kubernetes API

**Solution:**
```bash
# Verify kubeconfig is mounted (if in Docker/K8s)
kubectl config view  # Local

# Or check RBAC permissions
kubectl auth can-i list pods --as=system:serviceaccount:chatbot:chatbot-sa
```

### Slow response time

**Problem:** LLM inference is slow

**Solution:**
1. Check model server capacity: `curl http://192.168.1.19:8080/health`
2. Try a faster model: `mistral-7b` instead of `llama2-13b`
3. Increase GPU allocation if using vLLM

---

## Metrics & Monitoring

The chatbot exposes Prometheus metrics (if integrated):

```
chatbot_requests_total          # Total API requests
chatbot_response_latency_ms     # Response time distribution
chatbot_tool_calls_total        # Tool usage breakdown
chatbot_fallback_invocations    # Times cloud fallback was used
```

---

## Security

- **Authentication:** Bearer token required for all API calls
- **Authorization:** Chatbot uses a service account with read-only RBAC
- **Privacy:** Conversations stored locally (in-memory for dev, PostgreSQL for prod)
- **Tool Safety:** All tools are read-only by default

---

## Architecture

```
┌─────────────────┐
│   Operator      │
│  (CLI Client)   │
└────────┬────────┘
         │
    HTTP/1.1
    Bearer Token
         │
┌────────▼────────────────────┐
│   FastAPI Server            │
│  (chatbot-api:8000)         │
├─────────────────────────────┤
│ LLM Orchestrator            │
│ ├─ Tool Router              │
│ ├─ Conversation Manager     │
│ └─ Stream Handler           │
└────────┬─────────────────────┘
         │
    ┌────┴──────────┬──────────┬──────────┬──────────┐
    │               │          │          │          │
    ▼               ▼          ▼          ▼          ▼
Local LLM      Kubernetes   Prometheus   Loki    Qdrant
192.168.1.19  API Server    Metrics    Logs    Graph DB
:8080         (localhost)   (9090)     (3100)  (6333)
```

---

## Development

### Run tests

```bash
pytest tests/chatbot/ -v
```

### Run with hot-reload

```bash
python -m uvicorn src.chatbot.main:app --reload
```

### Build Docker image

```bash
docker build -t k8s-cluster-health/chatbot:latest src/chatbot/
```

---

## Support

For issues or feature requests, open a GitHub issue with:
- The query that failed
- The error message
- Relevant logs (`kubectl logs -n chatbot deployment/chatbot-api`)

---

**Happy troubleshooting! 🚀**

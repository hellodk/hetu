# 🤖 k8s-cluster-health Chatbot - Quick Start Guide

**AI-powered assistant for operators to query and troubleshoot your Kubernetes cluster.**

---

## 🚀 TL;DR - Start in 30 seconds

### Local (Docker Compose)
```bash
cd /home/dk/Documents/git/k8s-cluster-health

# Ensure model server is running at 192.168.1.19:8080
# Then start chatbot:
./scripts/chatbot-start.sh local

# In another terminal:
export TOKEN="your-token"
python src/chatbot/cli.py --token $TOKEN
```

### Kubernetes
```bash
./scripts/chatbot-start.sh k8s

# Port-forward to localhost
kubectl port-forward -n chatbot svc/chatbot-api 8000:8000 &

# Use the CLI
export TOKEN="your-token"
python src/chatbot/cli.py --token $TOKEN
```

---

## 📋 What You Get

The chatbot can answer operator questions like:

### Pod & Status Queries
- ✅ "What pods are failing?"
- ✅ "Why is the collector pod restarting?"
- ✅ "Show me all running pods in kube-system"

### Metrics & Performance
- ✅ "What's using the most CPU?"
- ✅ "Show me memory usage trends"
- ✅ "Are there any memory leaks?"

### Incident Investigation
- ✅ "What's the root cause of the API slowdown?"
- ✅ "Show me recent incidents"
- ✅ "Analyze the logs for errors"

### Architecture Questions
- ✅ "How does the scoring system work?"
- ✅ "Tell me about error handling in the collector"
- ✅ "What's the architecture of the dashboard?"

---

## 🏗️ Architecture

```
Operator Terminal
       ↓
CLI Client (Python)
       ↓ HTTP + Bearer Token
┌──────────────────────────┐
│  FastAPI Server          │
│  (chatbot-api:8000)      │
├──────────────────────────┤
│ • Tool Router            │
│ • Conversation Manager   │
│ • LLM Orchestrator       │
└──────────────────────────┘
       ↓
    7 Tools:
  • kubectl (pods/logs)
  • Prometheus (metrics)
  • Loki (logs)
  • Dashboard API (incidents)
  • Knowledge Graph (search)
  • Local LLM (reasoning)
  • Embeddings (semantic search)
```

---

## 📦 What's Included

| File | Purpose |
|------|---------|
| `src/chatbot/main.py` | FastAPI server with tools & LLM integration |
| `src/chatbot/cli.py` | Interactive CLI for operators |
| `docker-compose.chatbot.yml` | Local dev setup (API + Redis) |
| `deploy/k8s/chatbot-deployment.yaml` | Kubernetes manifests (production) |
| `scripts/chatbot-start.sh` | One-command startup script |
| `src/chatbot/config/chatbot-models.yaml` | Model configuration |
| `src/chatbot/README.md` | Detailed documentation |

---

## 🔧 Prerequisites

✅ **Local model server** running at `192.168.1.19:8080`:
  - Ollama: `docker run -d -p 8080:11434 ollama/ollama && ollama pull mistral`
  - vLLM: `python -m vllm.entrypoints.openai.api_server --model mistral --port 8080`
  - Text Generation WebUI: Uses built-in API server

✅ **Python 3.9+** (local CLI)

✅ **Docker** (for local mode) OR **kubectl** (for K8s mode)

---

## 💬 Example Conversation

```
You: What's the status of the system?

🤖 Assistant:
Analyzing cluster health...

Based on current data:
• collector pod: CRASHING (restarted 12 times)
• analyzer pod: RUNNING (95% CPU)
• dashboard pod: RUNNING (healthy)

**Findings:**
The collector is experiencing memory pressure. Logs show:
  "memory exceeded 512MB limit"

Per the architecture docs, this is a known NATS client issue.

**Recommended fix:**
1. Increase memory limit to 1GB
2. Patch the connection cleanup bug in pkg/collector/nats.go
3. Redeploy

Would you like me to get more details?

---

You: How do I fix the collector?

🤖 Assistant:
Two options:

**Quick fix (temporary):**
kubectl set resources deployment/collector -n default --limits=memory=1Gi

**Permanent fix:**
1. Clone the repo
2. Fix the NATS connection leak (line 142 in pkg/collector/nats.go)
3. Add defer conn.Close() in the main loop
4. Rebuild and redeploy

Need help with either approach?
```

---

## 🎯 Common Use Cases

### Incident Response
```bash
operator@terminal:~$ python src/chatbot/cli.py --token $TOKEN

You: The API is slow. What's happening?

# Chatbot:
# 1. Checks pod metrics
# 2. Queries error rates
# 3. Analyzes logs
# 4. Correlates with architecture docs
# 5. Recommends action
```

### Pod Troubleshooting
```bash
You: Why is the collector pod crashing?

# Chatbot will:
# - Get pod status (restarts, events)
# - Fetch recent logs
# - Query memory/CPU usage
# - Search knowledge graph for similar issues
# - Suggest root cause and fixes
```

### Architecture Questions
```bash
You: How does the scoring system work?

# Chatbot will:
# - Search knowledge graph
# - Find architecture docs
# - Explain the four dimensions (Reliability, Security, Cost, Architecture)
# - Show the weighted formula
# - Explain deductions/bonuses
```

---

## 🚨 Troubleshooting

### "Connection refused" error
```bash
# Check if model server is running
curl http://192.168.1.19:8080/models

# If not, start it:
docker run -d -p 8080:11434 ollama/ollama
ollama pull mistral
```

### "Pod not found" in logs
```bash
# Check kubeconfig
kubectl config view

# Or check RBAC permissions
kubectl auth can-i list pods --as=system:serviceaccount:chatbot:chatbot-sa -n chatbot
```

### Slow responses
```bash
# Check model server health
curl http://192.168.1.19:8080/health

# Try faster model
# Edit config/chatbot-models.yaml:
# model_name: "mistral-7b"  # Fast
# model_name: "llama2-13b"  # Accurate but slower
```

---

## 📊 Monitoring

### View server health
```bash
curl http://localhost:8000/health
```

### Check logs
```bash
# Local mode
docker-compose -f docker-compose.chatbot.yml logs -f chatbot-api

# Kubernetes mode
kubectl logs -n chatbot deployment/chatbot-api -f
```

### Check conversation history
```bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8000/api/v1/conversations/conv_abc123
```

---

## 🔐 Security Notes

- **Authentication:** All API calls require Bearer token
- **Authorization:** Chatbot uses read-only service account in K8s
- **Privacy:** Conversations stored locally (in-memory for dev)
- **Tools:** All operations are read-only (no destructive actions)

---

## 📚 Full Documentation

See `src/chatbot/README.md` for:
- Complete API reference
- All available tools
- Configuration options
- Development setup
- Production deployment

---

## 💡 Tips for Operators

### Use context switching
```bash
You: Switch to kube-system namespace
namespace kube-system

You: What's the status of coredns?
# Now queries kube-system by default
```

### Get full conversation history
```bash
# Show history within the chat
history

# Or view via API
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8000/api/v1/conversations/conv_xyz
```

### Debug with different models
```bash
# Start with fast model for quick diagnosis
model_name: "mistral-7b"

# Switch to accurate model for detailed analysis
model_name: "llama2-13b"
```

---

## 🤝 Support

- **Issues?** Check `src/chatbot/README.md` → Troubleshooting section
- **Questions?** Use the `help` command in the CLI
- **Feedback?** Open a GitHub issue

---

**Happy clustering! 🚀**

# AI Chatbot - Local & Configurable Model Architecture

**Status:** Updated Design  
**Date:** 2026-06-20  
**Model Server:** Running at `192.168.1.19:8080`

---

## Overview

The chatbot now supports **fully configurable models**:
- **Embedding Model:** Configure which embedding service to use (local or cloud)
- **LLM Model:** Configure which LLM backend to use (local, Claude API, OpenAI, etc.)
- **Model Registry:** Discover available models and their capabilities
- **Fallback Strategy:** Automatic downgrade if primary model is unavailable

---

## Architecture: Configurable Models

```
┌─────────────────────────────────────────────────────────────┐
│                   Chatbot API Server                         │
│              (FastAPI + Tool Router)                         │
└─────────────────────────────────────────────────────────────┘
              ↓              ↓              ↓
    ┌─────────────┐  ┌──────────────┐  ┌──────────────┐
    │ LLM Router  │  │ Embedding    │  │ Config       │
    │             │  │ Router       │  │ Manager      │
    └─────────────┘  └──────────────┘  └──────────────┘
         ↓                  ↓                 ↓
    ┌─────────────────────────────────────────────────────────┐
    │            Model Configuration Store                     │
    │  (YAML / JSON - persisted in PostgreSQL / ConfigMap)    │
    └─────────────────────────────────────────────────────────┘
         ↓                  ↓                 ↓
    Local Models (192.168.1.19:8080)
    ├─ Embedding: all-MiniLM-L6-v2
    ├─ LLM: Mistral, Llama2, etc.
    └─ Model Discovery API

    + Cloud Fallback (if configured)
    ├─ Anthropic Claude API
    ├─ OpenAI GPT-4
    └─ Other providers
```

---

## Configuration Schema

### File: `config/chatbot-models.yaml`

```yaml
# Embedding Model Configuration
embedding:
  provider: "local"  # Options: local, openai, huggingface
  
  # Local endpoint
  local:
    endpoint: "http://192.168.1.19:8080/embeddings"
    model_name: "all-MiniLM-L6-v2"
    dimensions: 384
    timeout_seconds: 10
    retry_max: 3
  
  # Cloud fallback (optional)
  cloud:
    enabled: true
    provider: "openai"
    api_key: "${OPENAI_API_KEY}"
    model_name: "text-embedding-3-small"
  
  # Caching strategy
  cache:
    enabled: true
    ttl_seconds: 86400
    backend: "redis"  # redis or memory
    host: "redis.chatbot.svc.cluster.local"
    port: 6379

# LLM Configuration
llm:
  provider: "local"  # Options: local, anthropic, openai, custom
  
  # Local LLM endpoint
  local:
    endpoint: "http://192.168.1.19:8080/v1/chat/completions"
    model_name: "mistral-7b"  # or llama2, neural-chat, etc.
    timeout_seconds: 30
    max_tokens: 2048
    temperature: 0.7
    top_p: 0.95
    retry_max: 2
    stream: true
  
  # Primary cloud provider (with fallback chain)
  cloud:
    enabled: true
    provider: "anthropic"
    api_key: "${ANTHROPIC_API_KEY}"
    model_name: "claude-3-sonnet-20240229"
    timeout_seconds: 30
    max_tokens: 2048
  
  # Fallback chain (if primary unavailable)
  fallback_chain:
    - provider: "openai"
      api_key: "${OPENAI_API_KEY}"
      model_name: "gpt-4-turbo"
    - provider: "anthropic"
      api_key: "${ANTHROPIC_API_KEY}"
      model_name: "claude-3-haiku-20240307"

# Model Discovery
discovery:
  enabled: true
  registry_endpoint: "http://192.168.1.19:8080/models"
  refresh_interval_seconds: 300
  cache_local_models: true

# Tool Execution Config
tools:
  timeout_per_tool: 5
  parallel_execution: true
  max_retries: 2

# Feature Flags
features:
  enable_local_models: true
  enable_cloud_fallback: true
  enable_model_routing: true
  enable_usage_tracking: true
```

### File: `config/chatbot-models-dev.yaml` (Development Override)

```yaml
embedding:
  provider: "local"
  local:
    endpoint: "http://localhost:8080/embeddings"
    model_name: "all-MiniLM-L6-v2"
  cache:
    backend: "memory"  # Use in-memory cache for dev

llm:
  provider: "local"
  local:
    endpoint: "http://localhost:8080/v1/chat/completions"
    model_name: "mistral-7b"
  cloud:
    enabled: false  # Don't fallback in dev
```

---

## Dynamic Model Configuration API

### Endpoint: GET /api/v1/config/models

**Description:** Discover available models on the local server

**Request:**
```bash
curl -H "Authorization: Bearer $TOKEN" \
  http://chatbot-api:8000/api/v1/config/models
```

**Response:**
```json
{
  "embedding_models": [
    {
      "id": "all-MiniLM-L6-v2",
      "name": "MiniLM (Fast)",
      "dimensions": 384,
      "latency_ms": 10,
      "quality_score": 0.93,
      "status": "healthy",
      "endpoint": "http://192.168.1.19:8080/embeddings"
    },
    {
      "id": "all-mpnet-base-v2",
      "name": "MPNet (Accurate)",
      "dimensions": 768,
      "latency_ms": 25,
      "quality_score": 0.97,
      "status": "healthy",
      "endpoint": "http://192.168.1.19:8080/embeddings"
    }
  ],
  "llm_models": [
    {
      "id": "mistral-7b",
      "name": "Mistral 7B",
      "context_window": 8000,
      "latency_ms": 500,
      "quality_score": 0.88,
      "status": "healthy",
      "endpoint": "http://192.168.1.19:8080/v1/chat/completions"
    },
    {
      "id": "llama2-13b",
      "name": "Llama 2 13B",
      "context_window": 4096,
      "latency_ms": 750,
      "quality_score": 0.85,
      "status": "healthy",
      "endpoint": "http://192.168.1.19:8080/v1/chat/completions"
    }
  ],
  "cloud_providers": [
    {
      "provider": "anthropic",
      "status": "available",
      "models": ["claude-3-sonnet", "claude-3-haiku"]
    },
    {
      "provider": "openai",
      "status": "unavailable",
      "reason": "API key not configured"
    }
  ]
}
```

---

### Endpoint: POST /api/v1/config/models/select

**Description:** Change the active embedding or LLM model

**Request:**
```bash
curl -X POST http://chatbot-api:8000/api/v1/config/models/select \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "embedding_model": "all-mpnet-base-v2",
    "llm_model": "mistral-7b",
    "provider": "local",
    "apply_for_conversation": "conv_abc123"  # optional; apply only to this conversation
  }'
```

**Response:**
```json
{
  "status": "success",
  "message": "Models updated",
  "active_config": {
    "embedding": {
      "model_id": "all-mpnet-base-v2",
      "provider": "local",
      "quality_score": 0.97
    },
    "llm": {
      "model_id": "mistral-7b",
      "provider": "local",
      "quality_score": 0.88
    }
  },
  "estimated_latency_ms": 535,
  "effective_after": "immediate"
}
```

---

## Model Router Implementation

### Python: Smart Model Selection

```python
from typing import Literal
from enum import Enum
from dataclasses import dataclass
import httpx
import yaml

class ModelProvider(Enum):
    LOCAL = "local"
    ANTHROPIC = "anthropic"
    OPENAI = "openai"
    HUGGINGFACE = "huggingface"

@dataclass
class ModelConfig:
    provider: ModelProvider
    model_name: str
    endpoint: str
    timeout_seconds: int
    max_retries: int = 2

class EmbeddingRouter:
    """Route embedding requests to configured model with fallback."""
    
    def __init__(self, config_path: str):
        self.config = yaml.safe_load(open(config_path))
        self.cache = {}
        self.health_check_interval = 300  # 5 min
    
    async def embed(self, text: str) -> list[float]:
        """
        Embed text using configured model with fallback.
        
        Flow:
        1. Check cache (Redis/Memory)
        2. Try primary provider (local)
        3. Fall back to cloud if primary fails
        4. Store result in cache
        """
        # Cache check
        cache_key = f"embedding:{hash(text)}"
        if cached := self.cache.get(cache_key):
            return cached
        
        # Primary: Local
        if self.config["embedding"]["provider"] == "local":
            try:
                return await self._embed_local(text)
            except Exception as e:
                print(f"Local embedding failed: {e}")
                if self.config["embedding"]["cloud"]["enabled"]:
                    return await self._embed_cloud(text)
                raise
        
        # Primary: Cloud
        else:
            return await self._embed_cloud(text)
    
    async def _embed_local(self, text: str) -> list[float]:
        """Call local embedding endpoint (192.168.1.19:8080)."""
        cfg = self.config["embedding"]["local"]
        async with httpx.AsyncClient(timeout=cfg["timeout_seconds"]) as client:
            response = await client.post(
                cfg["endpoint"],
                json={"input": text, "model": cfg["model_name"]}
            )
            response.raise_for_status()
            return response.json()["data"][0]["embedding"]
    
    async def _embed_cloud(self, text: str) -> list[float]:
        """Fall back to OpenAI or other cloud provider."""
        cfg = self.config["embedding"]["cloud"]
        if cfg["provider"] == "openai":
            from openai import OpenAI
            client = OpenAI(api_key=cfg["api_key"])
            response = client.embeddings.create(
                model=cfg["model_name"],
                input=text
            )
            return response.data[0].embedding
    
    async def health_check(self) -> dict:
        """Check if primary model is reachable."""
        cfg = self.config["embedding"]["local"]
        try:
            async with httpx.AsyncClient(timeout=5) as client:
                response = await client.get(f"{cfg['endpoint']}/health")
                return {"status": "healthy", "latency_ms": response.elapsed.total_seconds() * 1000}
        except Exception as e:
            return {"status": "unhealthy", "error": str(e)}

class LLMRouter:
    """Route LLM requests across multiple providers with intelligent fallback."""
    
    def __init__(self, config_path: str):
        self.config = yaml.safe_load(open(config_path))
        self.fallback_chain = self._build_fallback_chain()
    
    def _build_fallback_chain(self) -> list[ModelConfig]:
        """Build ordered list of models to try."""
        chain = []
        
        # Primary
        if self.config["llm"]["provider"] == "local":
            chain.append(ModelConfig(
                provider=ModelProvider.LOCAL,
                model_name=self.config["llm"]["local"]["model_name"],
                endpoint=self.config["llm"]["local"]["endpoint"],
                timeout_seconds=self.config["llm"]["local"]["timeout_seconds"]
            ))
        
        # Fallback chain
        for fallback in self.config["llm"].get("fallback_chain", []):
            chain.append(ModelConfig(
                provider=ModelProvider[fallback["provider"].upper()],
                model_name=fallback["model_name"],
                endpoint=fallback.get("endpoint"),
                timeout_seconds=fallback.get("timeout_seconds", 30)
            ))
        
        return chain
    
    async def chat(self, messages: list[dict], **kwargs) -> str:
        """
        Send chat request with automatic provider fallback.
        
        Tries models in order until one succeeds.
        """
        for i, model_config in enumerate(self.fallback_chain):
            try:
                print(f"Trying LLM #{i+1}: {model_config.provider.value}/{model_config.model_name}")
                
                if model_config.provider == ModelProvider.LOCAL:
                    return await self._chat_local(messages, model_config, **kwargs)
                elif model_config.provider == ModelProvider.ANTHROPIC:
                    return await self._chat_anthropic(messages, model_config, **kwargs)
                elif model_config.provider == ModelProvider.OPENAI:
                    return await self._chat_openai(messages, model_config, **kwargs)
            
            except Exception as e:
                print(f"LLM #{i+1} failed: {e}")
                if i == len(self.fallback_chain) - 1:
                    raise RuntimeError(f"All LLM providers failed. Last error: {e}")
                continue
    
    async def _chat_local(self, messages: list[dict], model: ModelConfig, **kwargs) -> str:
        """Call local LLM endpoint (OpenAI-compatible API)."""
        async with httpx.AsyncClient(timeout=model.timeout_seconds) as client:
            response = await client.post(
                model.endpoint,
                json={
                    "model": model.model_name,
                    "messages": messages,
                    **kwargs
                }
            )
            response.raise_for_status()
            return response.json()["choices"][0]["message"]["content"]
    
    async def _chat_anthropic(self, messages: list[dict], model: ModelConfig, **kwargs) -> str:
        """Call Anthropic Claude API."""
        import anthropic
        client = anthropic.Anthropic(api_key=self.config["llm"]["cloud"]["api_key"])
        response = client.messages.create(
            model=model.model_name,
            messages=messages,
            **kwargs
        )
        return response.content[0].text
    
    async def _chat_openai(self, messages: list[dict], model: ModelConfig, **kwargs) -> str:
        """Call OpenAI API."""
        from openai import OpenAI
        client = OpenAI(api_key=self.config["llm"]["cloud"]["api_key"])
        response = client.chat.completions.create(
            model=model.model_name,
            messages=messages,
            **kwargs
        )
        return response.choices[0].message.content
```

---

## Per-Conversation Model Selection

Users can choose different models **per conversation** for testing/optimization:

### API: Start Conversation with Model Config

```bash
curl -X POST http://chatbot-api:8000/api/v1/chat \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "message": "What services are failing?",
    "conversation_id": "conv_new",
    "models": {
      "embedding": "all-mpnet-base-v2",  # Use high-quality embeddings
      "llm": "llama2-13b"                # Use faster LLM
    }
  }'
```

**Per-conversation config is stored in PostgreSQL:**
```sql
CREATE TABLE conversation_model_config (
    conversation_id UUID PRIMARY KEY,
    embedding_model VARCHAR(255),
    embedding_provider VARCHAR(255),
    llm_model VARCHAR(255),
    llm_provider VARCHAR(255),
    created_at TIMESTAMP DEFAULT NOW()
);
```

---

## Health & Metrics Dashboard

### Endpoint: GET /api/v1/health

```json
{
  "status": "healthy",
  "timestamp": "2026-06-20T12:00:00Z",
  "services": {
    "api_server": { "status": "up", "latency_ms": 2 },
    "embedding_local": { 
      "status": "healthy", 
      "latency_ms": 11,
      "model": "all-MiniLM-L6-v2",
      "endpoint": "http://192.168.1.19:8080/embeddings"
    },
    "llm_local": { 
      "status": "healthy", 
      "latency_ms": 520,
      "model": "mistral-7b",
      "endpoint": "http://192.168.1.19:8080/v1/chat/completions"
    },
    "llm_fallback_anthropic": { 
      "status": "healthy",
      "model": "claude-3-sonnet"
    },
    "redis_cache": { "status": "up", "latency_ms": 1 },
    "postgres": { "status": "up", "latency_ms": 5 },
    "qdrant": { "status": "up", "latency_ms": 8 }
  },
  "metrics": {
    "total_conversations": 1234,
    "active_conversations": 12,
    "avg_response_latency_ms": 850,
    "cache_hit_ratio": 0.73,
    "successful_requests_today": 5432,
    "failed_requests_today": 12,
    "fallback_invocations_today": 3  # Times we fell back from local to cloud
  }
}
```

---

## Example: Testing Different Models

### Scenario: Compare embedding quality

```bash
# Conversation 1: Fast embedding
curl -X POST http://chatbot-api:8000/api/v1/chat \
  -d '{
    "message": "Pod memory leak?",
    "models": {"embedding": "all-MiniLM-L6-v2"}
  }' > response1.json

# Conversation 2: Accurate embedding
curl -X POST http://chatbot-api:8000/api/v1/chat \
  -d '{
    "message": "Pod memory leak?",
    "models": {"embedding": "all-mpnet-base-v2"}
  }' > response2.json

# Compare: same LLM, different embeddings
# response1.json shows faster search (10ms) but lower relevance
# response2.json shows slower search (25ms) but higher relevance (0.97 vs 0.93)
```

---

## Deployment: Local Model Server at 192.168.1.19:8080

### Using Ollama or vLLM

```bash
# Option 1: Ollama (simplest)
docker run -d \
  -p 8080:11434 \
  --name ollama \
  ollama/ollama

# Pull models
ollama pull mistral
ollama pull neural-chat
ollama pull all-minilm-l6-v2

# Option 2: vLLM (for inference optimization)
python -m vllm.entrypoints.openai.api_server \
  --model mistral-community/Mistral-7B-Instruct-v0.1 \
  --port 8080 \
  --gpu-memory-utilization 0.9

# Option 3: Text Generation WebUI (vLLM + embeddings)
# Supports multiple models, easy UI, embedding endpoints
```

### API Compatibility

The server at **192.168.1.19:8080** should expose:

```
GET /models
  → List available models

POST /v1/chat/completions (OpenAI-compatible)
  → LLM inference

POST /embeddings
  → Embedding generation

GET /health
  → Server health status
```

---

## Configuration in Kubernetes

### ConfigMap: Model Config

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: chatbot-models
  namespace: chatbot
data:
  models.yaml: |
    embedding:
      provider: "local"
      local:
        endpoint: "http://192.168.1.19:8080/embeddings"
        model_name: "all-MiniLM-L6-v2"
    llm:
      provider: "local"
      local:
        endpoint: "http://192.168.1.19:8080/v1/chat/completions"
        model_name: "mistral-7b"
      cloud:
        enabled: true
        provider: "anthropic"
```

### Pod: Mount Config

```yaml
spec:
  containers:
  - name: chatbot-api
    volumeMounts:
    - name: model-config
      mountPath: /app/config
  volumes:
  - name: model-config
    configMap:
      name: chatbot-models
```

---

## Benefits of This Architecture

| Aspect | Benefit |
|--------|---------|
| **Cost** | Use free local models; cloud fallback only if needed |
| **Latency** | Local models are instant (no API round-trip) |
| **Privacy** | Sensitive queries stay local |
| **Flexibility** | Swap models without redeploying |
| **Testing** | A/B test different models per-conversation |
| **Resilience** | Auto-fallback if local model goes down |
| **Control** | Full custody of model selection |

---

## Summary

✅ **Primary:** Local models at 192.168.1.19:8080 (fast, free, private)  
✅ **Fallback:** Cloud providers (Claude, GPT-4) configured as backups  
✅ **Per-Conversation:** Users can select different models for testing  
✅ **Discovery:** Auto-detect available models on the local server  
✅ **Health Monitoring:** Track latency, availability, fallback events  
✅ **Configuration:** YAML-based, easy to update without code changes  

---

**Next Steps:**
1. Ensure local model server (192.168.1.19:8080) exposes `/models` + `/v1/chat/completions` + `/embeddings`
2. Implement `EmbeddingRouter` and `LLMRouter` classes
3. Add model discovery API endpoint
4. Deploy chatbot with ConfigMap pointing to your server
5. Test fallback chain (disable local models, verify cloud fallback works)

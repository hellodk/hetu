"""
AI Chatbot for k8s-cluster-health
Operators query: pods, logs, metrics, incidents, knowledge graph
Local LLM at 192.168.1.19:8080 with cloud fallback

Env vars (override any YAML config):
  LLM_ENDPOINT          — full URL for /v1/chat/completions
  LLM_MODEL             — model name
  LLM_PROVIDER          — default "ollama"
  EMBEDDING_ENDPOINT    — full URL for embeddings
  EMBEDDING_MODEL       — embedding model name
  LOG_LEVEL             — default INFO
  AUTH_ENABLED          — default "true" (set "false" for local dev)
  OIDC_ISSUER           — Keycloak realm URL
  OIDC_CLIENT_ID        — default "hetu-chatbot"
  OIDC_AUDIENCE         — default = OIDC_CLIENT_ID
  OTEL_SERVICE_NAME     — default "hetu-chatbot"
  OTEL_EXPORTER_OTLP_ENDPOINT — default http://avika-tempo.monitoring.svc:4317
"""

import json
import logging
import os
import time
import uuid
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import AsyncGenerator

import httpx
import yaml
from fastapi import BackgroundTasks, Depends, FastAPI, HTTPException, Request, Response
from fastapi.responses import StreamingResponse
from auth import require_token

# ============================================================================
# Structured JSON logging
# ============================================================================

import pythonjsonlogger.jsonlogger as _jsonlogger  # python-json-logger

# Lazy import of trace_id helper — observability module defines it
def _get_trace_id() -> str:
    try:
        from observability import current_trace_id
        return current_trace_id()
    except Exception:
        return ""


class _TraceIdFilter(logging.Filter):
    """Inject trace_id and service name into every LogRecord."""

    def filter(self, record: logging.LogRecord) -> bool:
        record.trace_id = _get_trace_id()
        record.service = os.environ.get("OTEL_SERVICE_NAME", "hetu-chatbot")
        return True


_LOG_LEVEL = os.environ.get("LOG_LEVEL", "INFO").upper()
_log_handler = logging.StreamHandler()
_log_handler.setFormatter(
    _jsonlogger.JsonFormatter(
        fmt="%(asctime)s %(levelname)s %(name)s %(message)s %(trace_id)s %(service)s",
        rename_fields={"asctime": "timestamp", "levelname": "level"},
    )
)
_log_handler.addFilter(_TraceIdFilter())

logging.root.handlers = []
logging.root.addHandler(_log_handler)
logging.root.setLevel(_LOG_LEVEL)

logger = logging.getLogger(__name__)

# ============================================================================
# FastAPI app — created before OTEL init so instrumentor can wrap it
# ============================================================================

app = FastAPI(title="k8s-cluster-health Chatbot", version="1.0.0")

# ============================================================================
# OTEL tracing
# ============================================================================

try:
    from observability import init_tracing, tracer
    init_tracing(app)
except Exception as _otel_exc:
    logger.warning("OTEL tracing unavailable: %s", _otel_exc)

    from contextlib import contextmanager

    class _NoOpSpan:
        def set_attribute(self, *a, **kw): pass
        def record_exception(self, *a, **kw): pass

    class _NoOpTracer:
        @contextmanager
        def start_as_current_span(self, name, **kw):
            yield _NoOpSpan()

    tracer = _NoOpTracer()  # type: ignore[assignment]

# ============================================================================
# Prometheus metrics
# ============================================================================

from prometheus_client import generate_latest, CONTENT_TYPE_LATEST
from metrics import (
    chatbot_requests_total as _chatbot_requests_total,
    chatbot_request_errors_total as _chatbot_request_errors_total,
    chatbot_request_latency_seconds as _chatbot_request_latency_seconds,
    chatbot_llm_tokens_total as _chatbot_llm_tokens_total,
    chatbot_tool_calls_total as _chatbot_tool_calls_total,
)


@app.middleware("http")
async def _metrics_middleware(request: Request, call_next):
    endpoint = request.url.path
    start = time.perf_counter()
    try:
        response = await call_next(request)
        elapsed = time.perf_counter() - start
        _chatbot_requests_total.labels(endpoint=endpoint).inc()
        _chatbot_request_latency_seconds.labels(endpoint=endpoint).observe(elapsed)
        if response.status_code >= 400:
            _chatbot_request_errors_total.labels(endpoint=endpoint).inc()
        return response
    except Exception as exc:
        elapsed = time.perf_counter() - start
        _chatbot_requests_total.labels(endpoint=endpoint).inc()
        _chatbot_request_errors_total.labels(endpoint=endpoint).inc()
        _chatbot_request_latency_seconds.labels(endpoint=endpoint).observe(elapsed)
        raise


# ============================================================================
# Configuration
# ============================================================================

@dataclass
class ModelConfig:
    embedding_endpoint: str
    embedding_model: str
    llm_endpoint: str
    llm_model: str
    llm_provider: str = "ollama"
    llm_timeout: int = 30
    embedding_timeout: int = 10


def load_config() -> ModelConfig:
    """Load config with resolution order: env var > YAML file > hardcoded default."""

    # --- Hardcoded defaults ---
    defaults = {
        "llm_endpoint": "http://192.168.1.19:8080/v1/chat/completions",
        "llm_model": "mistral-7b",
        "llm_provider": "ollama",
        "llm_timeout": 30,
        "embedding_endpoint": "http://192.168.1.19:8080/embeddings",
        "embedding_model": "all-MiniLM-L6-v2",
        "embedding_timeout": 10,
    }

    # --- YAML overrides ---
    yaml_values: dict = {}
    config_path = Path("config/chatbot-models.yaml")
    if config_path.exists():
        try:
            with open(config_path) as f:
                cfg = yaml.safe_load(f) or {}
            llm_local = cfg.get("llm", {}).get("local", {})
            embed_local = cfg.get("embedding", {}).get("local", {})
            if llm_local.get("endpoint"):
                yaml_values["llm_endpoint"] = llm_local["endpoint"]
            if llm_local.get("model_name"):
                yaml_values["llm_model"] = llm_local["model_name"]
            if llm_local.get("provider"):
                yaml_values["llm_provider"] = llm_local["provider"]
            if llm_local.get("timeout_seconds"):
                yaml_values["llm_timeout"] = int(llm_local["timeout_seconds"])
            if embed_local.get("endpoint"):
                yaml_values["embedding_endpoint"] = embed_local["endpoint"]
            if embed_local.get("model_name"):
                yaml_values["embedding_model"] = embed_local["model_name"]
            if embed_local.get("timeout_seconds"):
                yaml_values["embedding_timeout"] = int(embed_local["timeout_seconds"])
        except Exception as exc:
            logger.warning("Failed to load chatbot-models.yaml: %s", exc)

    # --- Env var overrides (highest priority) ---
    env_values: dict = {}
    _env_map = {
        "LLM_ENDPOINT": "llm_endpoint",
        "LLM_MODEL": "llm_model",
        "LLM_PROVIDER": "llm_provider",
        "EMBEDDING_ENDPOINT": "embedding_endpoint",
        "EMBEDDING_MODEL": "embedding_model",
    }
    for env_key, field in _env_map.items():
        val = os.environ.get(env_key, "").strip()
        if val:
            env_values[field] = val

    merged = {**defaults, **yaml_values, **env_values}

    return ModelConfig(
        embedding_endpoint=merged["embedding_endpoint"],
        embedding_model=merged["embedding_model"],
        llm_endpoint=merged["llm_endpoint"],
        llm_model=merged["llm_model"],
        llm_provider=merged.get("llm_provider", "ollama"),
        llm_timeout=int(merged.get("llm_timeout", 30)),
        embedding_timeout=int(merged.get("embedding_timeout", 10)),
    )


config = load_config()

# ============================================================================
# In-Memory Storage (replace with PostgreSQL for production)
# ============================================================================

conversations: dict = {}          # {conversation_id: [messages]}
conversation_metadata: dict = {}  # {conversation_id: {created_at, user_id, ...}}

# ============================================================================
# Tools for Operators
# ============================================================================

class Tools:
    """Available tools operators can use to query the system."""

    @staticmethod
    async def get_pod_status(namespace: str = "default", pod_pattern: str = "") -> dict:
        """Get pod status, resource usage, recent events."""
        try:
            import subprocess
            cmd = f"kubectl get pods -n {namespace} -o json"
            result = subprocess.run(cmd.split(), capture_output=True, text=True, timeout=5)
            pods_data = json.loads(result.stdout)

            pods = []
            for pod in pods_data.get("items", []):
                name = pod["metadata"]["name"]
                if pod_pattern and pod_pattern not in name:
                    continue

                status = pod["status"]
                pods.append({
                    "name": name,
                    "phase": status.get("phase"),
                    "restart_count": sum(
                        c.get("restartCount", 0)
                        for c in status.get("containerStatuses", [])
                    ),
                    "conditions": [
                        {"type": c["type"], "status": c["status"], "reason": c.get("reason")}
                        for c in status.get("conditions", [])
                    ]
                })

            _chatbot_tool_calls_total.labels(tool="get_pod_status", status="success").inc()
            return {
                "status": "success",
                "pods": pods,
                "namespace": namespace,
                "total": len(pods)
            }
        except Exception as e:
            _chatbot_tool_calls_total.labels(tool="get_pod_status", status="error").inc()
            return {"status": "error", "message": str(e)}

    @staticmethod
    async def get_logs(service: str, namespace: str = "default", lines: int = 50) -> dict:
        """Fetch logs from a service."""
        try:
            import subprocess
            cmd = f"kubectl logs -n {namespace} -l app={service} --tail={lines} --timestamps=true"
            result = subprocess.run(cmd.split(), capture_output=True, text=True, timeout=5)

            logs = result.stdout.split("\n")
            _chatbot_tool_calls_total.labels(tool="get_logs", status="success").inc()
            return {
                "status": "success",
                "service": service,
                "namespace": namespace,
                "logs": logs,
                "total_lines": len([l for l in logs if l.strip()])
            }
        except Exception as e:
            _chatbot_tool_calls_total.labels(tool="get_logs", status="error").inc()
            return {"status": "error", "message": str(e)}

    @staticmethod
    async def query_metrics(metric_query: str) -> dict:
        """Query Prometheus for metrics."""
        try:
            prometheus_url = "http://prometheus.monitoring.svc.cluster.local:9090"
            async with httpx.AsyncClient(timeout=10) as client:
                response = await client.get(
                    f"{prometheus_url}/api/v1/query",
                    params={"query": metric_query}
                )
                data = response.json()
                _chatbot_tool_calls_total.labels(tool="query_metrics", status="success").inc()
                return {
                    "status": "success",
                    "query": metric_query,
                    "results": data.get("data", {}).get("result", [])
                }
        except Exception as e:
            _chatbot_tool_calls_total.labels(tool="query_metrics", status="error").inc()
            return {"status": "error", "message": str(e)}

    @staticmethod
    async def get_incidents() -> dict:
        """Get recent incidents from dashboard API."""
        try:
            dashboard_url = "http://localhost:3000"  # Update to your dashboard URL
            async with httpx.AsyncClient(timeout=5) as client:
                response = await client.get(f"{dashboard_url}/api/v1/incidents?status=open&limit=10")
                data = response.json()
                _chatbot_tool_calls_total.labels(tool="get_incidents", status="success").inc()
                return {
                    "status": "success",
                    "incidents": data.get("incidents", []),
                    "total": len(data.get("incidents", []))
                }
        except Exception as e:
            _chatbot_tool_calls_total.labels(tool="get_incidents", status="error").inc()
            return {"status": "error", "message": str(e)}

    @staticmethod
    async def search_knowledge_graph(query: str, limit: int = 5) -> dict:
        """Search the knowledge graph by semantic similarity."""
        try:
            # Embed the query
            async with httpx.AsyncClient(timeout=config.embedding_timeout) as client:
                embed_response = await client.post(
                    config.embedding_endpoint,
                    json={"input": query, "model": config.embedding_model}
                )
                embedding = embed_response.json()["data"][0]["embedding"]

            # Search Qdrant vector DB
            qdrant_url = "http://qdrant.default.svc.cluster.local:6333"
            async with httpx.AsyncClient(timeout=5) as client:
                search_response = await client.post(
                    f"{qdrant_url}/collections/graph_nodes/points/search",
                    json={
                        "vector": embedding,
                        "limit": limit,
                        "score_threshold": 0.7
                    }
                )
                results = search_response.json()
                _chatbot_tool_calls_total.labels(tool="search_knowledge_graph", status="success").inc()
                return {
                    "status": "success",
                    "query": query,
                    "results": results.get("result", []),
                    "total": len(results.get("result", []))
                }
        except Exception as e:
            _chatbot_tool_calls_total.labels(tool="search_knowledge_graph", status="error").inc()
            return {"status": "error", "message": str(e), "query": query}

# ============================================================================
# LLM Integration
# ============================================================================

class LLMOrchestrator:
    """Manages conversation with LLM and tool execution."""

    def __init__(self, config: ModelConfig):
        self.config = config
        self.tools = {
            "get_pod_status": Tools.get_pod_status,
            "get_logs": Tools.get_logs,
            "query_metrics": Tools.query_metrics,
            "get_incidents": Tools.get_incidents,
            "search_knowledge_graph": Tools.search_knowledge_graph,
        }

    def build_system_prompt(self) -> str:
        return """You are an expert SRE assistant helping operators troubleshoot Kubernetes cluster health issues.

You have access to these tools:
- get_pod_status: Check pod health, restarts, conditions
- get_logs: Fetch logs from services
- query_metrics: Query Prometheus for metrics (use PromQL)
- get_incidents: List recent incidents from dashboard
- search_knowledge_graph: Search architecture docs and design patterns

When an operator asks about system issues:
1. Analyze the problem statement
2. Call appropriate tools to gather data (you can call multiple tools in parallel)
3. Synthesize findings and recommend solutions
4. Always cite your sources (which tool returned the information)

Be concise, actionable, and prioritize critical issues. If you need clarification, ask specific follow-up questions."""

    async def chat(
        self,
        conversation_id: str,
        user_message: str,
        conversation_history: list,
    ) -> AsyncGenerator[str, None]:
        """Stream chat response with tool use."""

        # Add user message to history
        conversation_history.append({
            "role": "user",
            "content": user_message
        })

        messages = conversation_history
        provider = self.config.llm_provider
        model = self.config.llm_model

        try:
            with tracer.start_as_current_span("llm.generate") as span:
                span.set_attribute("llm.provider", provider)
                span.set_attribute("llm.model", model)
                span.set_attribute("llm.endpoint", self.config.llm_endpoint)

                async with httpx.AsyncClient(timeout=self.config.llm_timeout) as client:
                    response = await client.post(
                        self.config.llm_endpoint,
                        json={
                            "model": model,
                            "messages": messages,
                            "temperature": 0.7,
                            "stream": True,
                        }
                    )

                    token_count = 0
                    async for line in response.aiter_lines():
                        if line.startswith("data: "):
                            try:
                                data = json.loads(line[6:])
                                delta = data.get("choices", [{}])[0].get("delta", {})
                                if "content" in delta:
                                    token_count += 1
                                    yield delta["content"]
                                # Accumulate usage if present
                                usage = data.get("usage", {})
                                if usage.get("completion_tokens"):
                                    token_count = usage["completion_tokens"]
                            except json.JSONDecodeError:
                                continue

                    if token_count:
                        _chatbot_llm_tokens_total.labels(provider=provider, model=model).inc(token_count)
                        span.set_attribute("llm.tokens_used", token_count)

        except httpx.ConnectError as e:
            yield f"\n⚠️ LLM server unavailable at {self.config.llm_endpoint}. Check if model is running.\n"
            logger.error("LLM connection failed: %s", e)
        except Exception as e:
            yield f"\n❌ Error: {str(e)}\n"
            logger.error("Chat error: %s", e)


orchestrator = LLMOrchestrator(config)

# ============================================================================
# API Endpoints
# ============================================================================

@app.get("/health")
async def health():
    """Health check endpoint — unauthenticated."""
    return {
        "status": "healthy",
        "timestamp": datetime.utcnow().isoformat(),
        "llm_endpoint": config.llm_endpoint,
        "embedding_endpoint": config.embedding_endpoint,
    }


@app.get("/metrics")
async def metrics():
    """Prometheus metrics endpoint — unauthenticated."""
    data = generate_latest()
    return Response(content=data, media_type=CONTENT_TYPE_LATEST)


@app.post("/api/v1/chat")
async def chat(
    request: dict,
    background_tasks: BackgroundTasks = None,
    principal: dict = Depends(require_token),
):
    """
    Chat endpoint - stream response from chatbot.

    Request:
    {
        "message": "What pods are failing?",
        "conversation_id": "conv_abc123" (optional, auto-created if missing),
        "namespace": "default" (optional context)
    }
    """

    user_id = principal.get("preferred_username") or principal.get("sub", "operator")

    user_message = request.get("message", "").strip()
    if not user_message:
        raise HTTPException(status_code=400, detail="Message is required")

    conversation_id = request.get("conversation_id") or str(uuid.uuid4())
    namespace = request.get("namespace", "default")

    # Initialize conversation
    if conversation_id not in conversations:
        conversations[conversation_id] = []
        conversation_metadata[conversation_id] = {
            "created_at": datetime.utcnow().isoformat(),
            "user_id": user_id,
            "namespace": namespace,
        }

    conversation_history = conversations[conversation_id]

    async def response_generator():
        """Generate streamed chat response."""
        try:
            full_response = ""
            async for chunk in orchestrator.chat(conversation_id, user_message, conversation_history):
                full_response += chunk
                yield f'{{"type":"chunk","content":"{chunk.replace(chr(34), chr(92)+chr(34))}"}}\n'

            conversations[conversation_id].append({
                "role": "assistant",
                "content": full_response
            })

            yield '{"type":"done"}\n'

        except Exception as e:
            logger.error("Response generation error: %s", e)
            yield f'{{"type":"error","message":"{str(e)}"}}\n'

    return StreamingResponse(
        response_generator(),
        media_type="application/x-ndjson",
        headers={"X-Conversation-ID": conversation_id}
    )


@app.get("/api/v1/conversations/{conversation_id}")
async def get_conversation(conversation_id: str):
    """Get conversation history."""
    if conversation_id not in conversations:
        raise HTTPException(status_code=404, detail="Conversation not found")

    return {
        "conversation_id": conversation_id,
        "metadata": conversation_metadata[conversation_id],
        "messages": conversations[conversation_id],
    }


@app.get("/api/v1/config")
async def get_config():
    """Get current model configuration."""
    return {
        "embedding": {
            "endpoint": config.embedding_endpoint,
            "model": config.embedding_model,
        },
        "llm": {
            "endpoint": config.llm_endpoint,
            "model": config.llm_model,
            "provider": config.llm_provider,
        }
    }


if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8000)

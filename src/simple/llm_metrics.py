#!/usr/bin/env python3
"""
LLM Metrics Module for K8s Cluster Intelligence Engine
Provides comprehensive monitoring for Ollama and other LLM backends
"""

import json
import time
import threading
import urllib.request
import ssl
from datetime import datetime, timezone
from typing import Dict, Optional, Any, List
from dataclasses import dataclass, field
from contextlib import contextmanager
import os

# Try to import prometheus_client, fall back to mock if not available
try:
    from prometheus_client import Counter, Histogram, Gauge, Info, CollectorRegistry, generate_latest
    PROMETHEUS_AVAILABLE = True
except ImportError:
    PROMETHEUS_AVAILABLE = False
    print("[LLM Metrics] prometheus_client not installed, metrics will be logged only")

# Try to import opentelemetry for tracing
try:
    from opentelemetry import trace
    from opentelemetry.trace import Status, StatusCode
    from opentelemetry.sdk.trace import TracerProvider
    from opentelemetry.sdk.trace.export import BatchSpanProcessor
    from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
    from opentelemetry.sdk.resources import Resource
    from opentelemetry.semconv.resource import ResourceAttributes
    OTEL_AVAILABLE = True
except ImportError:
    OTEL_AVAILABLE = False
    print("[LLM Metrics] opentelemetry not installed, tracing disabled")


@dataclass
class LLMCompletionResult:
    """Result from an LLM completion request"""
    content: str
    input_tokens: int = 0
    output_tokens: int = 0
    total_tokens: int = 0
    time_to_first_token: float = 0.0
    model_loaded: bool = False
    finish_reason: str = "stop"
    total_duration_ns: int = 0
    load_duration_ns: int = 0
    prompt_eval_duration_ns: int = 0
    eval_duration_ns: int = 0


class LLMMetrics:
    """Prometheus metrics for LLM monitoring"""
    
    def __init__(self, namespace: str = "cluster_intel", registry: Optional[Any] = None):
        self.namespace = namespace
        self._lock = threading.Lock()
        self._metrics_data: Dict[str, Any] = {
            "requests_total": {},
            "requests_in_flight": 0,
            "errors_total": {},
            "duration_seconds": [],
            "tokens_input": 0,
            "tokens_output": 0,
        }
        
        if PROMETHEUS_AVAILABLE:
            self._init_prometheus_metrics(registry)
        
    def _init_prometheus_metrics(self, registry):
        """Initialize Prometheus metrics"""
        # Request metrics
        self.request_total = Counter(
            f"{self.namespace}_llm_request_total",
            "Total number of LLM requests",
            ["model", "task", "status", "provider"],
            registry=registry
        )
        
        self.request_duration = Histogram(
            f"{self.namespace}_llm_request_duration_seconds",
            "LLM request duration in seconds",
            ["model", "task", "provider"],
            buckets=[0.5, 1, 2, 5, 10, 20, 30, 45, 60, 90, 120],
            registry=registry
        )
        
        self.request_in_flight = Gauge(
            f"{self.namespace}_llm_request_in_flight",
            "Number of LLM requests currently in flight",
            registry=registry
        )
        
        # Token metrics
        self.tokens_input_total = Counter(
            f"{self.namespace}_llm_tokens_input_total",
            "Total input tokens sent to LLM",
            ["model", "task", "provider"],
            registry=registry
        )
        
        self.tokens_output_total = Counter(
            f"{self.namespace}_llm_tokens_output_total",
            "Total output tokens received from LLM",
            ["model", "task", "provider"],
            registry=registry
        )
        
        # Performance metrics
        self.time_to_first_token = Histogram(
            f"{self.namespace}_llm_time_to_first_token_seconds",
            "Time to first token in seconds",
            ["model", "provider"],
            buckets=[0.1, 0.25, 0.5, 1, 2, 5, 10, 20],
            registry=registry
        )
        
        self.tokens_per_second = Gauge(
            f"{self.namespace}_llm_tokens_per_second",
            "Token generation rate",
            ["model", "provider"],
            registry=registry
        )
        
        # Model loading metrics
        self.model_load_duration = Histogram(
            f"{self.namespace}_llm_model_load_duration_seconds",
            "Model loading duration in seconds",
            ["model", "provider"],
            buckets=[0.5, 1, 2, 5, 10, 30, 60, 120],
            registry=registry
        )
        
        # Error metrics
        self.errors_total = Counter(
            f"{self.namespace}_llm_errors_total",
            "Total number of LLM errors",
            ["model", "task", "error_type", "provider"],
            registry=registry
        )
        
        # Ollama-specific metrics
        self.ollama_queue_depth = Gauge(
            f"{self.namespace}_ollama_queue_depth",
            "Estimated Ollama queue depth",
            registry=registry
        )
    
    def record_request(self, model: str, task: str, provider: str, 
                      status: str, duration: float, result: Optional[LLMCompletionResult] = None):
        """Record metrics for a completed LLM request"""
        if PROMETHEUS_AVAILABLE:
            self.request_total.labels(model=model, task=task, status=status, provider=provider).inc()
            self.request_duration.labels(model=model, task=task, provider=provider).observe(duration)
            
            if result:
                self.tokens_input_total.labels(model=model, task=task, provider=provider).inc(result.input_tokens)
                self.tokens_output_total.labels(model=model, task=task, provider=provider).inc(result.output_tokens)
                
                if result.time_to_first_token > 0:
                    self.time_to_first_token.labels(model=model, provider=provider).observe(result.time_to_first_token)
                
                if duration > 0 and result.output_tokens > 0:
                    tps = result.output_tokens / duration
                    self.tokens_per_second.labels(model=model, provider=provider).set(tps)
                
                if result.load_duration_ns > 0:
                    load_secs = result.load_duration_ns / 1e9
                    self.model_load_duration.labels(model=model, provider=provider).observe(load_secs)
        
        # Also log for debugging
        print(f"[LLM] model={model} task={task} provider={provider} status={status} "
              f"duration={duration:.2f}s tokens_in={result.input_tokens if result else 0} "
              f"tokens_out={result.output_tokens if result else 0}")
    
    def record_error(self, model: str, task: str, provider: str, error_type: str):
        """Record an LLM error"""
        if PROMETHEUS_AVAILABLE:
            self.errors_total.labels(
                model=model, task=task, error_type=error_type, provider=provider
            ).inc()
        
        print(f"[LLM Error] model={model} task={task} provider={provider} error={error_type}")
    
    @contextmanager
    def track_request(self):
        """Context manager to track in-flight requests"""
        if PROMETHEUS_AVAILABLE:
            self.request_in_flight.inc()
        with self._lock:
            self._metrics_data["requests_in_flight"] += 1
        try:
            yield
        finally:
            if PROMETHEUS_AVAILABLE:
                self.request_in_flight.dec()
            with self._lock:
                self._metrics_data["requests_in_flight"] -= 1


class LLMTracer:
    """OpenTelemetry tracing for LLM requests"""
    
    def __init__(self, service_name: str = "cluster-intel", endpoint: Optional[str] = None):
        self.tracer = None
        
        if OTEL_AVAILABLE:
            endpoint = endpoint or os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://tempo:4317")
            
            resource = Resource.create({
                ResourceAttributes.SERVICE_NAME: service_name,
                ResourceAttributes.SERVICE_VERSION: "1.0.0",
            })
            
            provider = TracerProvider(resource=resource)
            
            try:
                exporter = OTLPSpanExporter(endpoint=endpoint, insecure=True)
                provider.add_span_processor(BatchSpanProcessor(exporter))
                trace.set_tracer_provider(provider)
                self.tracer = trace.get_tracer(__name__)
                print(f"[LLM Tracer] Initialized with endpoint: {endpoint}")
            except Exception as e:
                print(f"[LLM Tracer] Failed to initialize: {e}")
    
    @contextmanager
    def trace_llm_request(self, model: str, task: str, provider: str):
        """Context manager to trace an LLM request"""
        if not self.tracer:
            yield None
            return
        
        with self.tracer.start_as_current_span("llm.complete") as span:
            span.set_attribute("llm.model", model)
            span.set_attribute("llm.task", task)
            span.set_attribute("llm.provider", provider)
            
            try:
                yield span
            except Exception as e:
                span.record_exception(e)
                span.set_status(Status(StatusCode.ERROR, str(e)))
                raise


class OllamaClient:
    """Instrumented Ollama client with metrics and tracing"""
    
    def __init__(self, 
                 endpoint: str = "http://localhost:11434",
                 model: str = "llama3:70b",
                 metrics: Optional[LLMMetrics] = None,
                 tracer: Optional[LLMTracer] = None):
        self.endpoint = endpoint.rstrip("/")
        self.model = model
        self.metrics = metrics or LLMMetrics()
        self.tracer = tracer or LLMTracer()
        self.provider = "ollama"
        self.timeout = int(os.getenv("LLM_TIMEOUT", "120"))
    
    def complete(self, 
                 messages: List[Dict[str, str]], 
                 task: str = "default",
                 model: Optional[str] = None,
                 temperature: float = 0.3,
                 max_tokens: int = 4096) -> LLMCompletionResult:
        """
        Send a completion request to Ollama with full instrumentation
        
        Args:
            messages: List of message dicts with 'role' and 'content'
            task: Task type for metrics (e.g., 'rca', 'security', 'cost')
            model: Override model for this request
            temperature: Sampling temperature
            max_tokens: Maximum tokens to generate
            
        Returns:
            LLMCompletionResult with response and metrics
        """
        model = model or self.model
        start_time = time.time()
        
        with self.metrics.track_request():
            with self.tracer.trace_llm_request(model, task, self.provider) as span:
                try:
                    result = self._send_request(messages, model, temperature, max_tokens)
                    duration = time.time() - start_time
                    
                    self.metrics.record_request(
                        model=model,
                        task=task,
                        provider=self.provider,
                        status="success",
                        duration=duration,
                        result=result
                    )
                    
                    if span:
                        span.set_attribute("llm.input_tokens", result.input_tokens)
                        span.set_attribute("llm.output_tokens", result.output_tokens)
                        span.set_attribute("llm.duration_seconds", duration)
                        if result.time_to_first_token > 0:
                            span.set_attribute("llm.ttft_seconds", result.time_to_first_token)
                    
                    return result
                    
                except Exception as e:
                    duration = time.time() - start_time
                    error_type = self._classify_error(e)
                    
                    self.metrics.record_error(model, task, self.provider, error_type)
                    self.metrics.record_request(
                        model=model,
                        task=task,
                        provider=self.provider,
                        status="error",
                        duration=duration,
                        result=None
                    )
                    
                    raise
    
    def _send_request(self, messages: List[Dict[str, str]], model: str,
                      temperature: float, max_tokens: int) -> LLMCompletionResult:
        """Send request to Ollama's native API"""
        request_data = {
            "model": model,
            "messages": messages,
            "stream": False,
            "options": {
                "num_predict": max_tokens,
                "temperature": temperature,
            }
        }
        
        url = f"{self.endpoint}/api/chat"
        
        req = urllib.request.Request(
            url,
            data=json.dumps(request_data).encode("utf-8"),
            headers={"Content-Type": "application/json"},
            method="POST"
        )
        
        ctx = ssl.create_default_context()
        ctx.check_hostname = False
        ctx.verify_mode = ssl.CERT_NONE
        
        print(f"[LLM] Sending request to {url} model={model}")
        
        with urllib.request.urlopen(req, timeout=self.timeout, context=ctx) as response:
            data = json.loads(response.read().decode("utf-8"))
        
        # Parse Ollama response with detailed metrics
        content = data.get("message", {}).get("content", "")
        
        return LLMCompletionResult(
            content=content,
            input_tokens=data.get("prompt_eval_count", 0),
            output_tokens=data.get("eval_count", 0),
            total_tokens=data.get("prompt_eval_count", 0) + data.get("eval_count", 0),
            time_to_first_token=data.get("prompt_eval_duration", 0) / 1e9,  # ns to seconds
            model_loaded=data.get("load_duration", 0) > 0,
            finish_reason="stop" if data.get("done", False) else "incomplete",
            total_duration_ns=data.get("total_duration", 0),
            load_duration_ns=data.get("load_duration", 0),
            prompt_eval_duration_ns=data.get("prompt_eval_duration", 0),
            eval_duration_ns=data.get("eval_duration", 0),
        )
    
    def _classify_error(self, error: Exception) -> str:
        """Classify error for metrics"""
        err_str = str(error).lower()
        
        if "timeout" in err_str or "timed out" in err_str:
            return "timeout"
        elif "connection refused" in err_str:
            return "connection_refused"
        elif "429" in err_str or "rate limit" in err_str:
            return "rate_limited"
        elif "500" in err_str or "502" in err_str or "503" in err_str:
            return "server_error"
        elif "404" in err_str:
            return "not_found"
        elif "model" in err_str and "not found" in err_str:
            return "model_not_found"
        else:
            return "unknown"
    
    def health_check(self) -> Dict[str, Any]:
        """Check Ollama health and return status"""
        try:
            url = f"{self.endpoint}/api/tags"
            req = urllib.request.Request(url, method="GET")
            
            ctx = ssl.create_default_context()
            ctx.check_hostname = False
            ctx.verify_mode = ssl.CERT_NONE
            
            with urllib.request.urlopen(req, timeout=5, context=ctx) as response:
                data = json.loads(response.read().decode("utf-8"))
                models = data.get("models", [])
                return {
                    "status": "healthy",
                    "endpoint": self.endpoint,
                    "models_available": len(models),
                    "model_names": [m.get("name") for m in models],
                }
        except Exception as e:
            return {
                "status": "unhealthy",
                "endpoint": self.endpoint,
                "error": str(e),
            }


# Convenience function for the existing codebase
def create_instrumented_llm_client(
    endpoint: Optional[str] = None,
    model: Optional[str] = None,
    otel_endpoint: Optional[str] = None
) -> OllamaClient:
    """
    Factory function to create an instrumented LLM client
    
    Uses environment variables for configuration:
    - LLM_ENDPOINT: Ollama endpoint (default: http://localhost:11434)
    - LLM_MODEL: Model to use (default: llama3:70b)
    - OTEL_EXPORTER_OTLP_ENDPOINT: OpenTelemetry endpoint
    """
    endpoint = endpoint or os.getenv("LLM_ENDPOINT", "http://localhost:11434")
    model = model or os.getenv("LLM_MODEL", "llama3:70b")
    otel_endpoint = otel_endpoint or os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
    
    metrics = LLMMetrics(namespace="cluster_intel")
    tracer = LLMTracer(endpoint=otel_endpoint) if otel_endpoint else None
    
    return OllamaClient(
        endpoint=endpoint,
        model=model,
        metrics=metrics,
        tracer=tracer
    )


# Example usage and testing
if __name__ == "__main__":
    print("Testing LLM Metrics Module...")
    
    # Create client
    client = create_instrumented_llm_client(
        endpoint=os.getenv("LLM_ENDPOINT", "http://localhost:11434"),
        model=os.getenv("LLM_MODEL", "qwen2.5:7b-instruct")
    )
    
    # Health check
    health = client.health_check()
    print(f"Health check: {json.dumps(health, indent=2)}")
    
    if health["status"] == "healthy":
        # Test completion
        try:
            result = client.complete(
                messages=[
                    {"role": "system", "content": "You are a helpful assistant."},
                    {"role": "user", "content": "Say hello in exactly 3 words."}
                ],
                task="test"
            )
            print(f"\nCompletion result:")
            print(f"  Content: {result.content}")
            print(f"  Input tokens: {result.input_tokens}")
            print(f"  Output tokens: {result.output_tokens}")
            print(f"  TTFT: {result.time_to_first_token:.3f}s")
        except Exception as e:
            print(f"Completion failed: {e}")

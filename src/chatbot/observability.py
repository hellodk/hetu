"""
OpenTelemetry tracing initialisation for the chatbot.

Env vars:
  OTEL_EXPORTER_OTLP_ENDPOINT  — default: http://avika-tempo.monitoring.svc:4317
  OTEL_SERVICE_NAME            — default: hetu-chatbot

Usage:
    from observability import init_tracing, tracer
    init_tracing(app)   # call once at startup
    with tracer.start_as_current_span("llm.generate") as span:
        span.set_attribute("llm.provider", "ollama")
        ...
"""

import logging
import os
from typing import Optional

from fastapi import FastAPI

logger = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Lazy tracer — replaced by a real one when init_tracing() succeeds, otherwise
# falls back to the no-op tracer from opentelemetry-api so callers never crash.
# ---------------------------------------------------------------------------

try:
    from opentelemetry import trace as _otel_trace
    from opentelemetry.sdk.trace import TracerProvider
    from opentelemetry.sdk.trace.export import BatchSpanProcessor
    from opentelemetry.sdk.resources import Resource, SERVICE_NAME
    from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
    from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor
    from opentelemetry.instrumentation.httpx import HTTPXClientInstrumentor

    _OTEL_AVAILABLE = True
except ImportError:
    _OTEL_AVAILABLE = False
    logger.warning("opentelemetry packages not installed — tracing disabled")


def init_tracing(app: FastAPI) -> None:
    """Configure the TracerProvider and instrument FastAPI + HTTPX.

    If OTLP endpoint is unreachable, the BatchSpanProcessor silently drops spans
    (non-blocking background thread). The app continues to function normally.
    """
    if not _OTEL_AVAILABLE:
        return

    service_name = os.environ.get("OTEL_SERVICE_NAME", "hetu-chatbot")
    otlp_endpoint = os.environ.get(
        "OTEL_EXPORTER_OTLP_ENDPOINT", "http://avika-tempo.monitoring.svc:4317"
    )

    try:
        resource = Resource(attributes={SERVICE_NAME: service_name})
        provider = TracerProvider(resource=resource)

        exporter = OTLPSpanExporter(endpoint=otlp_endpoint, insecure=True)
        provider.add_span_processor(BatchSpanProcessor(exporter))

        _otel_trace.set_tracer_provider(provider)

        FastAPIInstrumentor.instrument_app(app, tracer_provider=provider)
        HTTPXClientInstrumentor().instrument(tracer_provider=provider)

        logger.info(
            "OTEL tracing initialised: service=%s endpoint=%s",
            service_name,
            otlp_endpoint,
        )
    except Exception as exc:
        logger.warning("OTEL tracing init failed (non-fatal): %s", exc)


def get_tracer():
    """Return a tracer.  Falls back to no-op tracer if OTEL is unavailable."""
    if _OTEL_AVAILABLE:
        return _otel_trace.get_tracer(__name__)
    # Minimal no-op stand-in so callers can always do `with tracer.start_as_current_span(...)`
    from contextlib import contextmanager

    class _NoOpSpan:
        def set_attribute(self, *a, **kw): pass
        def record_exception(self, *a, **kw): pass
        def set_status(self, *a, **kw): pass

    class _NoOpTracer:
        @contextmanager
        def start_as_current_span(self, name, **kw):
            yield _NoOpSpan()

    return _NoOpTracer()


def current_trace_id() -> str:
    """Return the current OTEL trace_id as a 32-char hex string, or '' if none."""
    if not _OTEL_AVAILABLE:
        return ""
    try:
        span = _otel_trace.get_current_span()
        ctx = span.get_span_context()
        if ctx and ctx.is_valid:
            return format(ctx.trace_id, "032x")
    except Exception:
        pass
    return ""


# Module-level tracer — callers import this directly
tracer = get_tracer()

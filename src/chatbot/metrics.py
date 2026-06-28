"""Prometheus metric singletons.

These are defined in their own module so the metric objects are registered
exactly once against the default CollectorRegistry. Tests reload ``main``,
``auth`` and ``observability`` (deleting them from ``sys.modules``) but never
this module — so re-importing ``main`` does not re-register these collectors
and never raises ``ValueError: Duplicated timeseries in CollectorRegistry``.
"""

from prometheus_client import Counter, Histogram

chatbot_requests_total = Counter(
    "chatbot_requests_total",
    "Total chatbot HTTP requests",
    ["endpoint"],
)
chatbot_request_errors_total = Counter(
    "chatbot_request_errors_total",
    "Total chatbot HTTP request errors",
    ["endpoint"],
)
chatbot_request_latency_seconds = Histogram(
    "chatbot_request_latency_seconds",
    "Chatbot HTTP request latency",
    ["endpoint"],
    buckets=(0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0),
)
chatbot_llm_tokens_total = Counter(
    "chatbot_llm_tokens_total",
    "Total LLM tokens consumed",
    ["provider", "model"],
)
chatbot_tool_calls_total = Counter(
    "chatbot_tool_calls_total",
    "Total tool invocations",
    ["tool", "status"],
)

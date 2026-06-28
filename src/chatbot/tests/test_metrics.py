"""
Tests: /metrics endpoint.

Verifies that the endpoint exists, returns 200, and contains the expected metric names.
Uses TestClient against the FastAPI app with AUTH_ENABLED=false.
"""

import os
import sys

import pytest


@pytest.fixture(scope="module", autouse=True)
def _set_env_for_metrics(tmp_path_factory):
    """Configure env and cwd before main is imported for this test module."""
    tmp_path = tmp_path_factory.mktemp("metrics_cfg")
    original_cwd = os.getcwd()
    os.chdir(str(tmp_path))
    os.environ["AUTH_ENABLED"] = "false"
    # Suppress OTEL so no background thread tries to connect
    os.environ.setdefault("OTEL_EXPORTER_OTLP_ENDPOINT", "")
    yield
    os.chdir(original_cwd)


@pytest.fixture(scope="module")
def client():
    # Remove cached modules so fresh import picks up env
    for mod in list(sys.modules.keys()):
        if mod in ("main", "auth", "observability"):
            del sys.modules[mod]
    import main as m
    from fastapi.testclient import TestClient
    return TestClient(m.app)


class TestMetricsEndpoint:
    def test_metrics_returns_200(self, client):
        response = client.get("/metrics")
        assert response.status_code == 200

    def test_metrics_content_type(self, client):
        response = client.get("/metrics")
        assert "text/plain" in response.headers.get("content-type", "")

    def test_chatbot_requests_total_present(self, client):
        response = client.get("/metrics")
        assert "chatbot_requests_total" in response.text

    def test_chatbot_request_errors_total_present(self, client):
        response = client.get("/metrics")
        assert "chatbot_request_errors_total" in response.text

    def test_chatbot_request_latency_seconds_present(self, client):
        response = client.get("/metrics")
        assert "chatbot_request_latency_seconds" in response.text

    def test_chatbot_llm_tokens_total_present(self, client):
        response = client.get("/metrics")
        assert "chatbot_llm_tokens_total" in response.text

    def test_chatbot_tool_calls_total_present(self, client):
        response = client.get("/metrics")
        assert "chatbot_tool_calls_total" in response.text

    def test_health_endpoint_still_works(self, client):
        response = client.get("/health")
        assert response.status_code == 200

    def test_metrics_not_auth_protected(self, client):
        """GET /metrics must return 200 without any Authorization header."""
        response = client.get("/metrics")
        assert response.status_code == 200

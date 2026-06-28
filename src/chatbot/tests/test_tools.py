"""
Tests: Tools class methods.

subprocess.run and httpx are monkeypatched — no live cluster or LLM needed.
"""

import asyncio
import json
from unittest.mock import MagicMock, AsyncMock, patch

import pytest


# ---------------------------------------------------------------------------
# Import Tools from main without triggering side-effects that need env vars.
# We import main once here; load_config uses defaults which is fine for Tools tests.
# ---------------------------------------------------------------------------

import sys, os

def _get_tools():
    for mod in list(sys.modules.keys()):
        if mod in ("main", "auth", "observability"):
            del sys.modules[mod]
    os.environ.setdefault("AUTH_ENABLED", "false")
    import main as m
    return m.Tools


class TestGetPodStatus:
    def _make_kubectl_result(self, pods: list) -> MagicMock:
        result = MagicMock()
        result.stdout = json.dumps({"items": pods})
        result.returncode = 0
        return result

    def _make_pod(self, name: str, phase: str = "Running", restart_count: int = 0) -> dict:
        return {
            "metadata": {"name": name},
            "status": {
                "phase": phase,
                "containerStatuses": [{"restartCount": restart_count}],
                "conditions": [{"type": "Ready", "status": "True", "reason": None}],
            },
        }

    def test_success_returns_status_success(self):
        Tools = _get_tools()
        pods = [self._make_pod("pod-abc"), self._make_pod("pod-xyz")]
        mock_result = self._make_kubectl_result(pods)

        with patch("subprocess.run", return_value=mock_result):
            result = asyncio.get_event_loop().run_until_complete(
                Tools.get_pod_status(namespace="default")
            )

        assert result["status"] == "success"
        assert result["total"] == 2
        assert result["namespace"] == "default"

    def test_pod_pattern_filters(self):
        Tools = _get_tools()
        pods = [self._make_pod("hetu-api"), self._make_pod("prometheus-xyz")]
        mock_result = self._make_kubectl_result(pods)

        with patch("subprocess.run", return_value=mock_result):
            result = asyncio.get_event_loop().run_until_complete(
                Tools.get_pod_status(namespace="default", pod_pattern="hetu")
            )

        assert result["status"] == "success"
        assert result["total"] == 1
        assert result["pods"][0]["name"] == "hetu-api"

    def test_exception_returns_status_error(self):
        Tools = _get_tools()
        with patch("subprocess.run", side_effect=Exception("kubectl not found")):
            result = asyncio.get_event_loop().run_until_complete(
                Tools.get_pod_status(namespace="default")
            )

        assert result["status"] == "error"
        assert "kubectl not found" in result["message"]

    def test_timeout_returns_error(self):
        Tools = _get_tools()
        import subprocess
        with patch("subprocess.run", side_effect=subprocess.TimeoutExpired(cmd="kubectl", timeout=5)):
            result = asyncio.get_event_loop().run_until_complete(
                Tools.get_pod_status(namespace="default")
            )
        assert result["status"] == "error"


class TestGetLogs:
    def test_success(self):
        Tools = _get_tools()
        mock_result = MagicMock()
        mock_result.stdout = "line1\nline2\nline3\n"

        with patch("subprocess.run", return_value=mock_result):
            result = asyncio.get_event_loop().run_until_complete(
                Tools.get_logs(service="hetu-api", namespace="default", lines=10)
            )

        assert result["status"] == "success"
        assert result["service"] == "hetu-api"

    def test_exception_returns_error(self):
        Tools = _get_tools()
        with patch("subprocess.run", side_effect=Exception("no kubectl")):
            result = asyncio.get_event_loop().run_until_complete(
                Tools.get_logs(service="hetu-api")
            )

        assert result["status"] == "error"


class TestQueryMetrics:
    def test_success(self):
        Tools = _get_tools()

        mock_response = MagicMock()
        mock_response.json.return_value = {
            "data": {"result": [{"metric": {}, "value": [1234567890, "1"]}]}
        }

        async def _mock_get(*args, **kwargs):
            return mock_response

        mock_client = AsyncMock()
        mock_client.__aenter__ = AsyncMock(return_value=mock_client)
        mock_client.__aexit__ = AsyncMock(return_value=False)
        mock_client.get = AsyncMock(return_value=mock_response)

        with patch("httpx.AsyncClient", return_value=mock_client):
            result = asyncio.get_event_loop().run_until_complete(
                Tools.query_metrics("up")
            )

        assert result["status"] == "success"
        assert len(result["results"]) == 1

    def test_exception_returns_error(self):
        Tools = _get_tools()
        import httpx

        mock_client = AsyncMock()
        mock_client.__aenter__ = AsyncMock(side_effect=httpx.ConnectError("refused"))
        mock_client.__aexit__ = AsyncMock(return_value=False)

        with patch("httpx.AsyncClient", return_value=mock_client):
            result = asyncio.get_event_loop().run_until_complete(
                Tools.query_metrics("up")
            )

        assert result["status"] == "error"


class TestGetIncidents:
    def test_success(self):
        Tools = _get_tools()

        mock_response = MagicMock()
        mock_response.json.return_value = {
            "incidents": [{"id": "inc-1", "title": "Pod crash loop"}]
        }

        mock_client = AsyncMock()
        mock_client.__aenter__ = AsyncMock(return_value=mock_client)
        mock_client.__aexit__ = AsyncMock(return_value=False)
        mock_client.get = AsyncMock(return_value=mock_response)

        with patch("httpx.AsyncClient", return_value=mock_client):
            result = asyncio.get_event_loop().run_until_complete(
                Tools.get_incidents()
            )

        assert result["status"] == "success"
        assert result["total"] == 1

    def test_exception_returns_error(self):
        Tools = _get_tools()

        mock_client = AsyncMock()
        mock_client.__aenter__ = AsyncMock(side_effect=Exception("connection refused"))
        mock_client.__aexit__ = AsyncMock(return_value=False)

        with patch("httpx.AsyncClient", return_value=mock_client):
            result = asyncio.get_event_loop().run_until_complete(
                Tools.get_incidents()
            )

        assert result["status"] == "error"

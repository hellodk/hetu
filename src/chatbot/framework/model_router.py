"""Model selection and routing logic for LLM calls."""

import asyncio
import httpx
from typing import Optional, Dict, Any
from dataclasses import dataclass
import logging

logger = logging.getLogger(__name__)


@dataclass
class ModelEndpoint:
    """Configuration for a model endpoint."""
    name: str
    url: str
    model: str
    api_key: Optional[str] = None
    headers: Dict[str, str] = None

    def __post_init__(self):
        if self.headers is None:
            self.headers = {}


class ModelRouter:
    """
    Route LLM requests to local or cloud endpoints.

    Implements health checks and automatic failover to cloud fallbacks.
    """

    def __init__(self, config: Dict[str, Any]):
        """
        Initialize router with config.

        Config schema:
        {
            "local_endpoint": {
                "url": "http://192.168.1.19:8080",
                "model": "mistral-7b"
            },
            "fallback_endpoints": [
                {
                    "name": "claude",
                    "url": "https://api.anthropic.com/...",
                    "model": "claude-3-sonnet",
                    "api_key": "sk-..."
                }
            ]
        }
        """
        self.config = config
        self.local = self._init_endpoint(config.get("local_endpoint"))
        self.fallbacks = [
            self._init_endpoint(fb)
            for fb in config.get("fallback_endpoints", [])
        ]
        self._health_cache = {}

    def _init_endpoint(self, endpoint_config: Dict[str, Any]) -> ModelEndpoint:
        """Initialize a ModelEndpoint from config dict."""
        if not endpoint_config:
            return None
        return ModelEndpoint(
            name=endpoint_config.get("name", "local"),
            url=endpoint_config["url"],
            model=endpoint_config["model"],
            api_key=endpoint_config.get("api_key"),
        )

    async def health_check(self, endpoint: ModelEndpoint) -> bool:
        """Check if an endpoint is healthy."""
        if endpoint.url in self._health_cache:
            return self._health_cache[endpoint.url]

        try:
            async with httpx.AsyncClient(timeout=2.0) as client:
                response = await client.get(f"{endpoint.url}/health")
                healthy = response.status_code == 200
        except (httpx.RequestError, asyncio.TimeoutError):
            healthy = False

        self._health_cache[endpoint.url] = healthy
        return healthy

    async def select_endpoint(self) -> ModelEndpoint:
        """
        Select the best available endpoint.

        Strategy: try local first, then fallback chain.
        """
        if self.local and await self.health_check(self.local):
            return self.local

        for fallback in self.fallbacks:
            if await self.health_check(fallback):
                logger.warning(f"Local endpoint down, using {fallback.name}")
                return fallback

        raise RuntimeError("All endpoints are down")

    async def call_llm(
        self,
        prompt: str,
        system: Optional[str] = None,
        temperature: float = 0.7,
        max_tokens: int = 2048,
        stream: bool = False,
    ):
        """
        Call the selected LLM endpoint.

        Yields JSON chunks if stream=True, otherwise returns full response.
        """
        endpoint = await self.select_endpoint()
        logger.info(f"Using endpoint: {endpoint.name}")

        headers = endpoint.headers.copy()
        if endpoint.api_key:
            headers["Authorization"] = f"Bearer {endpoint.api_key}"

        # Normalize to Ollama/Anthropic API format
        request_body = self._format_request(
            endpoint,
            prompt=prompt,
            system=system,
            temperature=temperature,
            max_tokens=max_tokens,
            stream=stream,
        )

        async with httpx.AsyncClient(timeout=30.0) as client:
            async with client.stream(
                "POST",
                f"{endpoint.url}/api/generate",
                json=request_body,
                headers=headers,
            ) as response:
                if stream:
                    async for line in response.aiter_lines():
                        yield line
                else:
                    return await response.json()

    def _format_request(
        self,
        endpoint: ModelEndpoint,
        prompt: str,
        system: Optional[str],
        temperature: float,
        max_tokens: int,
        stream: bool,
    ) -> Dict[str, Any]:
        """Format request for the endpoint's API."""
        # Ollama API format
        return {
            "model": endpoint.model,
            "prompt": f"{system or ''}\n{prompt}",
            "temperature": temperature,
            "num_predict": max_tokens,
            "stream": stream,
        }

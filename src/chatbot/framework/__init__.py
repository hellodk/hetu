"""
Operator Chatbot Framework — Domain-agnostic reusable components.

Usage:
  from chatbot_framework import LLMOrchestrator, ModelRouter, BaseTool

This framework provides the core chatbot functionality for any domain.
Domain-specific tools and system prompts are injected at initialization.
"""

from .orchestrator import LLMOrchestrator
from .model_router import ModelRouter
from .base_tools import BaseTool
from .schemas import ChatRequest, ChatResponse, ToolResult

__all__ = [
    "LLMOrchestrator",
    "ModelRouter",
    "BaseTool",
    "ChatRequest",
    "ChatResponse",
    "ToolResult",
]

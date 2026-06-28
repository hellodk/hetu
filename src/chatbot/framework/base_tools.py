"""Base tool class for domain-specific tool implementations."""

from abc import ABC, abstractmethod
from typing import Any, Dict, List, Optional
from dataclasses import dataclass
import json


@dataclass
class ToolResult:
    """Structured result from tool execution."""
    tool_name: str
    success: bool
    data: Dict[str, Any]
    error: Optional[str] = None
    execution_time_ms: float = 0.0

    def to_dict(self) -> Dict[str, Any]:
        return {
            "tool": self.tool_name,
            "success": self.success,
            "data": self.data,
            "error": self.error,
            "execution_time_ms": self.execution_time_ms,
        }


class BaseTool(ABC):
    """
    Abstract base class for domain-specific tools.

    Subclasses must implement `execute()` and provide a tool inventory via `get_tools()`.
    """

    def __init__(self, config: Dict[str, Any]):
        """Initialize tool with domain config."""
        self.config = config

    @abstractmethod
    def get_tools(self) -> List[Dict[str, str]]:
        """
        Return list of available tools.

        Returns:
            [{
                "name": "tool_name",
                "description": "What this tool does",
                "parameters": {
                    "arg1": "description of arg1",
                    "arg2": "description of arg2",
                }
            }, ...]
        """
        pass

    @abstractmethod
    async def execute(self, tool_name: str, **kwargs) -> ToolResult:
        """
        Execute a named tool with the given arguments.

        Args:
            tool_name: Name of the tool to execute
            **kwargs: Tool-specific arguments

        Returns:
            ToolResult with success status and data or error
        """
        pass

    async def execute_parallel(
        self, tools: List[tuple[str, Dict[str, Any]]]
    ) -> List[ToolResult]:
        """
        Execute multiple tools in parallel.

        Args:
            tools: List of (tool_name, kwargs) tuples

        Returns:
            List of ToolResults
        """
        import asyncio

        tasks = [self.execute(name, **kwargs) for name, kwargs in tools]
        return await asyncio.gather(*tasks, return_exceptions=False)

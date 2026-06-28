"""LLM orchestrator - core conversation engine."""

import json
import uuid
from typing import AsyncGenerator, Dict, Any, Optional
from datetime import datetime
import logging

from .schemas import ChatRequest, ChatResponseChunk, Conversation
from .model_router import ModelRouter
from .base_tools import BaseTool

logger = logging.getLogger(__name__)


class LLMOrchestrator:
    """
    Orchestrates conversations between operator and LLM.

    Handles:
    - Conversation state management
    - Tool invocation and result aggregation
    - LLM communication with streaming
    - Response formatting
    """

    def __init__(
        self,
        model_router: ModelRouter,
        tools: BaseTool,
        config: Dict[str, Any],
    ):
        """
        Initialize orchestrator.

        Args:
            model_router: ModelRouter instance for LLM selection
            tools: BaseTool subclass with domain-specific tools
            config: Configuration dict with system_prompt, etc.
        """
        self.model_router = model_router
        self.tools = tools
        self.config = config
        self.system_prompt = config.get(
            "system_prompt",
            "You are a helpful assistant.",
        )

        # Conversation storage (in-memory; use Redis for production)
        self.conversations: Dict[str, Conversation] = {}

    async def stream_response(
        self, request: ChatRequest
    ) -> AsyncGenerator[str, None]:
        """
        Stream a response to the operator's message.

        Yields JSON-lines format chunks:
        {"type": "thinking", "content": {...}}
        {"type": "tool_call", "content": {...}}
        {"type": "tool_result", "content": {...}}
        {"type": "text", "content": {...}}
        {"type": "complete", "content": {...}}
        """
        # Ensure conversation exists
        if request.conversation_id not in self.conversations:
            self.conversations[request.conversation_id] = Conversation(
                conversation_id=request.conversation_id,
                created_at=datetime.utcnow(),
                updated_at=datetime.utcnow(),
                context=request.context or {},
            )

        conv = self.conversations[request.conversation_id]
        conv.add_message("user", request.message)

        # Build context for LLM
        messages = "\n".join([
            f"{msg['role'].upper()}: {msg['content']}"
            for msg in conv.messages[-10:]  # Last 10 messages
        ])

        prompt = f"""
{self.system_prompt}

Available tools:
{json.dumps(self.tools.get_tools(), indent=2)}

Conversation history:
{messages}

Respond with:
1. Your thinking about which tools to call
2. Tool calls in JSON format
3. Based on results, provide the final answer

Format tool calls as:
TOOL: tool_name
ARGS: {{"arg1": "value1", "arg2": "value2"}}
"""

        # Call LLM with streaming
        accumulated_response = ""
        async for chunk in self.model_router.call_llm(
            prompt=prompt,
            system=self.system_prompt,
            stream=True,
        ):
            if isinstance(chunk, str):
                accumulated_response += chunk

            # Stream text chunks to operator
            yield ChatResponseChunk(
                type="text",
                content={"text": chunk},
            ).to_json_line()

        # Parse tool calls from response
        tool_calls = self._parse_tool_calls(accumulated_response)

        if tool_calls:
            # Yield tool call notifications
            for tool_name, args in tool_calls:
                yield ChatResponseChunk(
                    type="tool_call",
                    content={"tool": tool_name, "args": args},
                ).to_json_line()

            # Execute tools in parallel
            results = await self.tools.execute_parallel(tool_calls)

            # Yield tool results
            for result in results:
                yield ChatResponseChunk(
                    type="tool_result",
                    content=result.to_dict(),
                ).to_json_line()

        # Final response
        yield ChatResponseChunk(
            type="complete",
            content={"message": "Response complete"},
        ).to_json_line()

        # Save to conversation history
        conv.add_message("assistant", accumulated_response)

    def _parse_tool_calls(self, response: str) -> list[tuple[str, Dict[str, Any]]]:
        """
        Parse tool calls from LLM response.

        Expected format:
        TOOL: tool_name
        ARGS: {"arg1": "value1"}
        """
        tool_calls = []
        lines = response.split("\n")

        i = 0
        while i < len(lines):
            line = lines[i].strip()
            if line.startswith("TOOL:"):
                tool_name = line.replace("TOOL:", "").strip()
                # Next line should be ARGS
                if i + 1 < len(lines):
                    args_line = lines[i + 1].strip()
                    if args_line.startswith("ARGS:"):
                        try:
                            args_json = args_line.replace("ARGS:", "").strip()
                            args = json.loads(args_json)
                            tool_calls.append((tool_name, args))
                            i += 2
                            continue
                        except json.JSONDecodeError:
                            logger.warning(f"Failed to parse args: {args_json}")
            i += 1

        return tool_calls

    def get_conversation(self, conversation_id: str) -> Optional[Conversation]:
        """Retrieve conversation history."""
        return self.conversations.get(conversation_id)

    def list_conversations(self) -> list[str]:
        """List all active conversation IDs."""
        return list(self.conversations.keys())

"""Request/response schemas for the chatbot API."""

from typing import Optional, List, Dict, Any
from dataclasses import dataclass, field
from datetime import datetime


@dataclass
class ChatRequest:
    """Incoming chat request from operator."""
    conversation_id: str
    message: str
    context: Optional[Dict[str, str]] = None  # domain-specific context


@dataclass
class ChatResponseChunk:
    """Individual chunk in streaming response."""
    type: str  # "thinking" | "tool_call" | "tool_result" | "text" | "complete"
    content: Dict[str, Any]
    timestamp: float = field(default_factory=lambda: __import__('time').time())

    def to_json_line(self) -> str:
        """Convert to JSON-lines format for streaming."""
        import json
        return json.dumps({
            "type": self.type,
            "content": self.content,
            "timestamp": self.timestamp,
        })


@dataclass
class ChatResponse:
    """Complete chat response."""
    conversation_id: str
    response_text: str
    tool_calls: List[Dict[str, Any]] = field(default_factory=list)
    tool_results: List[Dict[str, Any]] = field(default_factory=list)
    metadata: Dict[str, Any] = field(default_factory=dict)
    created_at: datetime = field(default_factory=datetime.utcnow)


@dataclass
class Conversation:
    """Conversation metadata and history."""
    conversation_id: str
    created_at: datetime
    updated_at: datetime
    messages: List[Dict[str, str]] = field(default_factory=list)
    context: Dict[str, Any] = field(default_factory=dict)

    def add_message(self, role: str, content: str):
        """Add message to conversation history."""
        self.messages.append({
            "role": role,
            "content": content,
            "timestamp": datetime.utcnow().isoformat(),
        })
        self.updated_at = datetime.utcnow()

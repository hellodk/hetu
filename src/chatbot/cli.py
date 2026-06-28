#!/usr/bin/env python3
"""
CLI Client for k8s-cluster-health Chatbot
Operators use this to query the system via the chatbot.

Usage:
    python cli.py --server http://localhost:8000 --token $TOKEN
"""

import argparse
import httpx
import json
import sys
from datetime import datetime
from typing import AsyncGenerator
import asyncio
from pathlib import Path

class ChatbotClient:
    """Client for interacting with the chatbot."""

    def __init__(self, server_url: str, token: str, conversation_id: str = None):
        self.server_url = server_url.rstrip("/")
        self.token = token
        self.conversation_id = conversation_id
        self.headers = {"Authorization": f"Bearer {token}", "Content-Type": "application/json"}
        self.history_file = Path.home() / ".chatbot_history"

    async def chat(self, message: str, namespace: str = "default"):
        """Send message and stream response."""
        try:
            async with httpx.AsyncClient(timeout=60) as client:
                response = await client.post(
                    f"{self.server_url}/api/v1/chat",
                    headers=self.headers,
                    json={
                        "message": message,
                        "conversation_id": self.conversation_id,
                        "namespace": namespace,
                    }
                )

                if response.status_code != 200:
                    print(f"❌ Error: {response.text}", file=sys.stderr)
                    return

                # Extract conversation ID from response header
                if not self.conversation_id:
                    self.conversation_id = response.headers.get("X-Conversation-ID")

                # Stream response as JSON lines
                print("\n🤖 Assistant:\n")
                async for line in response.aiter_lines():
                    if line.strip():
                        try:
                            data = json.loads(line)
                            if data.get("type") == "chunk":
                                content = data.get("content", "").replace('\\"', '"')
                                print(content, end="", flush=True)
                            elif data.get("type") == "error":
                                print(f"\n⚠️ {data.get('message')}", file=sys.stderr)
                        except json.JSONDecodeError:
                            pass

                print("\n")

        except Exception as e:
            print(f"❌ Connection error: {e}", file=sys.stderr)
            sys.exit(1)

    async def interactive_mode(self, namespace: str = "default"):
        """Start interactive chat session."""
        print("🤖 k8s-cluster-health Chatbot")
        print(f"📍 Server: {self.server_url}")
        print(f"📦 Namespace: {namespace}")
        if self.conversation_id:
            print(f"💬 Conversation ID: {self.conversation_id}")
        print("\nType 'help' for commands, 'exit' to quit.\n")

        while True:
            try:
                user_input = input("You: ").strip()

                if not user_input:
                    continue

                if user_input.lower() == "exit":
                    print("👋 Goodbye!")
                    break

                if user_input.lower() == "help":
                    print_help()
                    continue

                if user_input.lower() == "history":
                    await self.show_history()
                    continue

                if user_input.lower().startswith("namespace "):
                    namespace = user_input.split(" ", 1)[1]
                    print(f"📦 Namespace switched to: {namespace}")
                    continue

                # Send message to chatbot
                await self.chat(user_input, namespace=namespace)

            except KeyboardInterrupt:
                print("\n👋 Goodbye!")
                break
            except Exception as e:
                print(f"❌ Error: {e}", file=sys.stderr)

    async def show_history(self):
        """Show conversation history."""
        if not self.conversation_id:
            print("No active conversation")
            return

        try:
            async with httpx.AsyncClient() as client:
                response = await client.get(
                    f"{self.server_url}/api/v1/conversations/{self.conversation_id}",
                    headers=self.headers
                )
                data = response.json()

                print(f"\n📜 Conversation: {self.conversation_id}")
                print(f"Created: {data['metadata']['created_at']}\n")

                for msg in data["messages"]:
                    role = "You" if msg["role"] == "user" else "🤖"
                    content = msg["content"][:100] + "..." if len(msg["content"]) > 100 else msg["content"]
                    print(f"{role}: {content}")

                print()

        except Exception as e:
            print(f"❌ Error fetching history: {e}", file=sys.stderr)

def print_help():
    """Print help message."""
    print("""
Commands:
  help           - Show this message
  exit           - Exit chatbot
  history        - Show conversation history
  namespace <ns> - Switch namespace (e.g., 'namespace kube-system')

Example queries:
  - What pods are failing?
  - Show me recent incidents
  - What's using the most CPU?
  - Why is the collector pod restarting?
  - How does the scoring system work?
  - Check the status of all services

Type your question or command and press Enter.
""")

async def main():
    parser = argparse.ArgumentParser(description="k8s-cluster-health Chatbot CLI")
    parser.add_argument("--server", default="http://localhost:8000", help="Chatbot server URL")
    parser.add_argument("--token", required=True, help="API token for authentication")
    parser.add_argument("--namespace", default="default", help="Kubernetes namespace")
    parser.add_argument("--message", help="Single message (non-interactive mode)")
    parser.add_argument("--conversation-id", help="Conversation ID (for continuing a chat)")

    args = parser.parse_args()

    client = ChatbotClient(args.server, args.token, conversation_id=args.conversation_id)

    if args.message:
        # Single message mode
        await client.chat(args.message, namespace=args.namespace)
    else:
        # Interactive mode
        await client.interactive_mode(namespace=args.namespace)

if __name__ == "__main__":
    asyncio.run(main())

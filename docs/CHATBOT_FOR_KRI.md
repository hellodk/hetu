# Using the Chatbot Framework for KRI

This guide shows how to use the reusable **Chatbot Framework** to build a chatbot for the KRI fleet operations platform.

---

## Quick Start: 5-Minute Adaptation

The framework does the heavy lifting. You only need to:

1. **Define fleet-specific tools** (SSH, Salt, Ansible)
2. **Write a system prompt** (fleet operations expert)
3. **Create a config file** (endpoint URLs, models)

---

## Step 1: Create Fleet Tools

```python
# kri/src/chatbot/tools/fleet_tools.py

from chatbot_framework import BaseTool, ToolResult
import asyncio
import json

class FleetTools(BaseTool):
    """Fleet-specific tools for KRI chatbot."""

    def get_tools(self):
        """Available tools for fleet operators."""
        return [
            {
                "name": "list_nodes",
                "description": "List all fleet nodes with health status",
                "parameters": {"filter": "optional filter (healthy|unhealthy|all)"}
            },
            {
                "name": "check_node_baseline",
                "description": "Check if a node matches its declared baseline",
                "parameters": {"node": "node name or IP"}
            },
            {
                "name": "get_node_logs",
                "description": "Get recent logs from a node via SSH",
                "parameters": {
                    "node": "node name",
                    "lines": "number of lines to fetch (default 50)"
                }
            },
            {
                "name": "run_playbook",
                "description": "Run an Ansible playbook on nodes",
                "parameters": {
                    "playbook": "playbook name",
                    "nodes": "comma-separated node names or 'all'"
                }
            },
            {
                "name": "check_salt_state",
                "description": "Check Salt state status on a node",
                "parameters": {"node": "node name"}
            },
            {
                "name": "search_fleet_docs",
                "description": "Search fleet docs (baselines, playbooks, runbooks)",
                "parameters": {"query": "search query"}
            }
        ]

    async def execute(self, tool_name: str, **kwargs) -> ToolResult:
        """Execute a fleet tool."""
        
        if tool_name == "list_nodes":
            return await self._list_nodes(kwargs.get("filter", "all"))
        elif tool_name == "check_node_baseline":
            return await self._check_node_baseline(kwargs["node"])
        elif tool_name == "get_node_logs":
            return await self._get_node_logs(
                kwargs["node"],
                kwargs.get("lines", 50)
            )
        elif tool_name == "run_playbook":
            return await self._run_playbook(
                kwargs["playbook"],
                kwargs["nodes"]
            )
        elif tool_name == "check_salt_state":
            return await self._check_salt_state(kwargs["node"])
        elif tool_name == "search_fleet_docs":
            return await self._search_fleet_docs(kwargs["query"])
        else:
            return ToolResult(
                tool_name=tool_name,
                success=False,
                data={},
                error=f"Unknown tool: {tool_name}"
            )

    async def _list_nodes(self, filter_type: str) -> ToolResult:
        """List fleet nodes."""
        # Fetch from KRI API or database
        nodes = [
            {"name": "mac-mini-1", "status": "healthy", "os": "macOS 14.1"},
            {"name": "mac-mini-2", "status": "unhealthy", "os": "macOS 13.5"},
            {"name": "linux-1", "status": "healthy", "os": "Ubuntu 22.04"},
        ]
        
        if filter_type != "all":
            nodes = [n for n in nodes if n["status"] == filter_type]
        
        return ToolResult(
            tool_name="list_nodes",
            success=True,
            data={"nodes": nodes, "count": len(nodes)}
        )

    async def _check_node_baseline(self, node: str) -> ToolResult:
        """Check node against baseline."""
        # Run 'salt <node> state.show_lowstate' or KRI API call
        status = {
            "node": node,
            "baseline_id": "baseline-v2.1",
            "matches": True,
            "drift": []
        }
        return ToolResult(
            tool_name="check_node_baseline",
            success=True,
            data=status
        )

    async def _get_node_logs(self, node: str, lines: int) -> ToolResult:
        """Fetch logs from node."""
        # SSH into node and run 'tail -n <lines> /var/log/syslog'
        logs = f"[Sample logs from {node} - last {lines} lines]"
        return ToolResult(
            tool_name="get_node_logs",
            success=True,
            data={"node": node, "logs": logs, "line_count": lines}
        )

    async def _run_playbook(self, playbook: str, nodes: str) -> ToolResult:
        """Run Ansible playbook."""
        # Run 'ansible-playbook <playbook> -i inventory <nodes>'
        return ToolResult(
            tool_name="run_playbook",
            success=True,
            data={
                "playbook": playbook,
                "nodes": nodes.split(","),
                "status": "started",
                "job_id": "job-12345"
            }
        )

    async def _check_salt_state(self, node: str) -> ToolResult:
        """Check Salt state."""
        # Run 'salt <node> state.sls <state>'
        return ToolResult(
            tool_name="check_salt_state",
            success=True,
            data={"node": node, "state": "running", "status": "ok"}
        )

    async def _search_fleet_docs(self, query: str) -> ToolResult:
        """Search fleet documentation."""
        # Use embeddings to search docs
        return ToolResult(
            tool_name="search_fleet_docs",
            success=True,
            data={
                "query": query,
                "results": [
                    {"title": "Baseline Compliance", "snippet": "..."},
                    {"title": "Mac Mini Setup", "snippet": "..."}
                ]
            }
        )
```

---

## Step 2: Create KRI Config

```yaml
# kri/src/chatbot/config/chatbot-models.yaml

domain: "fleet"
description: "KRI Fleet Operations Chatbot"

model_server: "192.168.1.19:8080"

system_prompt: |
  You are an expert fleet operations engineer managing Apple Silicon build agents,
  Mac Minis, and Linux servers. You answer questions about node health, baseline
  compliance, and help operators manage their fleet.
  
  When asked about node status, always check the current baseline.
  When drift is detected, suggest remediation via Ansible or Salt.
  Always search docs before recommending changes.

local_endpoint:
  name: "local"
  url: "http://192.168.1.19:8080"
  model: "mistral-7b"

fallback_endpoints:
  - name: "claude"
    url: "https://api.anthropic.com/v1/messages"
    model: "claude-3-sonnet"
    api_key: "${ANTHROPIC_API_KEY}"

knowledge_base:
  docs_path: "./docs/fleet/"
  embedding_model: "all-MiniLM-L6-v2"
  sources:
    - "baselines/"
    - "playbooks/"
    - "runbooks/"
    - "architecture/"

fleet:
  inventory_file: "/etc/kri/inventory.yaml"
  baseline_store: "postgresql"  # or "etcd"
  ssh_config: "/home/ops/.ssh/config"
  ansible_vault: "/etc/kri/.vault"
```

---

## Step 3: Create KRI Main Entry Point

```python
# kri/src/chatbot/main.py

from fastapi import FastAPI, HTTPException, Header
from fastapi.responses import StreamingResponse
import os
import yaml
from typing import Optional

from chatbot_framework import LLMOrchestrator, ModelRouter
from tools.fleet_tools import FleetTools

# Load config
with open("config/chatbot-models.yaml") as f:
    config = yaml.safe_load(f)

# Initialize router and tools
model_router = ModelRouter(config)
tools = FleetTools(config)

# Initialize orchestrator (framework handles the rest)
orchestrator = LLMOrchestrator(
    model_router=model_router,
    tools=tools,
    config=config
)

app = FastAPI(title="KRI Fleet Chatbot", version="1.0.0")

@app.post("/api/v1/chat")
async def chat(
    request: dict,
    authorization: Optional[str] = Header(None)
):
    """Streaming chat endpoint."""
    if not authorization or not authorization.startswith("Bearer "):
        raise HTTPException(status_code=401, detail="Unauthorized")
    
    from chatbot_framework import ChatRequest
    
    chat_request = ChatRequest(
        conversation_id=request.get("conversation_id", "default"),
        message=request["message"],
        context=request.get("context", {})
    )
    
    async def event_stream():
        async for chunk in orchestrator.stream_response(chat_request):
            yield chunk + "\n"
    
    return StreamingResponse(event_stream(), media_type="application/x-ndjson")

@app.get("/api/v1/conversations/{conversation_id}")
async def get_conversation(conversation_id: str):
    """Get conversation history."""
    conv = orchestrator.get_conversation(conversation_id)
    if not conv:
        raise HTTPException(status_code=404, detail="Conversation not found")
    return {"messages": conv.messages, "context": conv.context}

@app.get("/health")
async def health():
    """Health check."""
    return {"status": "ok"}
```

---

## Step 4: Docker Compose for KRI

```yaml
# docker-compose.chatbot.yml

version: "3.9"
services:
  kri-chatbot:
    build: ./src/chatbot
    ports:
      - "8001:8000"
    environment:
      LLM_ENDPOINT: "http://192.168.1.19:8080"
      CONFIG_PATH: "/app/config/chatbot-models.yaml"
      ANTHROPIC_API_KEY: "${ANTHROPIC_API_KEY}"
    volumes:
      - ./src/chatbot/config:/app/config
      - ~/.ssh:/home/ops/.ssh:ro
      - /etc/kri:/etc/kri:ro
    depends_on:
      - redis
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8000/health"]
      interval: 10s
      timeout: 5s

  redis:
    image: redis:7.2.4-alpine
    ports:
      - "6379:6379"
```

---

## Step 5: Use the Framework in KRI CLI

```python
# kri/src/chatbot/cli.py

import asyncio
import httpx
import json

class KRIFleetChat:
    """Interactive CLI for fleet operators."""
    
    def __init__(self, token: str, base_url: str = "http://localhost:8001"):
        self.token = token
        self.base_url = base_url
    
    async def chat(self, message: str, conversation_id: str = "default"):
        """Send message and stream response."""
        headers = {"Authorization": f"Bearer {self.token}"}
        
        async with httpx.AsyncClient() as client:
            async with client.stream(
                "POST",
                f"{self.base_url}/api/v1/chat",
                json={
                    "conversation_id": conversation_id,
                    "message": message
                },
                headers=headers
            ) as response:
                async for line in response.aiter_lines():
                    chunk = json.loads(line)
                    self._handle_chunk(chunk)
    
    def _handle_chunk(self, chunk):
        """Handle incoming chunk."""
        chunk_type = chunk["type"]
        content = chunk["content"]
        
        if chunk_type == "text":
            print(content["text"], end="", flush=True)
        elif chunk_type == "tool_call":
            print(f"\n→ Calling: {content['tool']} {content['args']}\n")
        elif chunk_type == "tool_result":
            print(f"  Result: {content['data']}\n")
        elif chunk_type == "complete":
            print("\n")
    
    async def interactive_mode(self):
        """Interactive operator session."""
        print("KRI Fleet Chatbot - Type 'help' for commands")
        
        while True:
            try:
                message = input("fleet> ").strip()
                if not message:
                    continue
                if message == "exit":
                    break
                if message == "help":
                    self._print_help()
                    continue
                
                await self.chat(message)
            except KeyboardInterrupt:
                break
    
    def _print_help(self):
        """Print help."""
        print("""
Available commands:
  • "Which nodes are unhealthy?" - Check fleet status
  • "Show mac-mini-1 baseline" - Check node compliance
  • "Run baseline playbook on all" - Remediate drift
  • "What's causing failures on linux-1?" - Troubleshoot
  • "history" - Show conversation history
  • "exit" - Exit
        """)

if __name__ == "__main__":
    import os
    token = os.getenv("KRI_CHATBOT_TOKEN", "dev-token")
    cli = KRIFleetChat(token)
    asyncio.run(cli.interactive_mode())
```

---

## What You Get

✅ **Same framework** as k8s-cluster-health (code reuse)  
✅ **Fleet-specific tools** (SSH, Salt, Ansible, baselines)  
✅ **Same API contracts** (both projects have identical endpoints)  
✅ **Local LLM first** (192.168.1.19:8080)  
✅ **Streaming responses** (real-time feedback for operators)  
✅ **Tool parallelization** (query multiple nodes concurrently)  

---

## Deploy KRI Chatbot

```bash
# Local development
docker-compose -f docker-compose.chatbot.yml up -d

# Interactive operator CLI
export KRI_CHATBOT_TOKEN="your-token"
python src/chatbot/cli.py

# Example queries
# "Status of all Mac Minis?"
# "Which nodes are drifted?"
# "Run baseline check on all unhealthy nodes"
```

---

## Kubernetes Deployment

Same pattern as k8s-cluster-health — deploy manifests in `deploy/k8s/chatbot/` with KRI-specific ServiceAccount (RBAC over Kubernetes API) replaced with SSH access config.

---

## Next: Framework Reuse Across Projects

Once both k8s-cluster-health and kri use this framework, any new project just needs:
1. Domain-specific tools class
2. System prompt + config
3. API wiring (copy from existing project)

The framework does the rest.

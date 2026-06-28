# Hetu AI Chat + RAG — Architecture & Implementation Plan

Status: **in progress** (Phase 1 landing in this change set)
Owner: platform/AI
Last updated: 2026-06-28

---

## 1. Goal

Ship a "full blown" RAG + AI chat assistant for hetu that meets or beats
HolmesGPT / K8sGPT / kagent / kubectl-ai for the Kubernetes troubleshooting
use case — but grounded in hetu's unique asset: its **own continuous analytical
state** (calibrated RCA, health scoring, anomaly detection, optimizer findings,
security scan, grouped errors with embeddings).

The assistant must:

- Answer operator questions in natural language, streamed into the dashboard.
- Retrieve from a real knowledge base (repo docs, runbooks, past incidents/RCA).
- Call tools to read **live** cluster + hetu state (pods, logs, metrics,
  incidents, error groups, health, recommendations).
- Cite its sources (doc chunks, incident IDs, tool results).
- Run fully on-prem against the operator-configured LLM + embedding endpoints.

---

## 2. Where hetu stands (competitive analysis)

| Capability | K8sGPT | HolmesGPT | kagent | kubectl-ai | hetu today | hetu target |
|---|---|---|---|---|---|---|
| Agentic tool loop | no | yes | yes | limited | partial | **yes** |
| RAG knowledge base | no | runbooks/history | per-agent | no | stubbed | **yes** |
| Continuous analysis + scoring | scan | health checks | no | no | **strong** | strong |
| Embedded chat UI | SaaS | SaaS | web UI | no | no | **yes** |
| Confidence calibration | no | no | no | no | **yes** | yes |
| Local/self-host | yes | yes | yes | yes | yes | yes |

hetu's analytics already lead K8sGPT/kubectl-ai. The missing piece is the
interactive agentic chat + a working RAG layer. HolmesGPT is the bar; hetu's
differentiator is grounding answers in its own scored/correlated state.

---

## 3. Current-state findings (gaps fixed by this plan)

1. The Python `src/chatbot/` chat path streams the user message straight to the
   LLM — **tools are never called and retrieval is never injected**. RAG is a
   stub; the queried Qdrant collection (`graph_nodes`) is never populated.
2. A better orchestrator (`framework/orchestrator.py`) exists but is **dead
   code**, and uses a different LLM API shape than `main.py`.
3. Four different LLM client implementations across the repo (fragmentation).
4. Conversations are in-memory; fragile `TOOL:/ARGS:` text protocol.
5. No embedding configuration in the UI (only LLM).
6. No chat surface in the dashboard.

**Decision:** consolidate the assistant into the Go `analyzer`, reuse the
unified `pkg/llm` client + `EmbeddingScorer` + `VectorStore` + `pkg/kube`, and
deprecate the Python service.

---

## 4. Target architecture

```
Dashboard (Next.js)  ──/api/v1/chat (SSE)──►  Analyzer (Go)
  ChatPanel.tsx                                 │
   - streams tokens                             ├─ ChatEngine (chat.go)
   - tool-call chips                            │    1. plan: pick tools/queries
   - citations                                  │    2. retrieve: RAG + tools
                                                │    3. synthesize: stream answer
                                                │
                                                ├─ KB index (chat_kb.go)
                                                │    docs/*.md + incidents → Qdrant (hetu_kb)
                                                │
                                                ├─ Tools (chat_tools.go) — in-process:
                                                │    health, scores, incidents, error groups,
                                                │    recommendations, pods (pkg/kube), metrics (prom)
                                                │
                                                └─ Embedding + LLM config (embedconfig.go,
                                                     llmconfig.go) — operator-set endpoints
```

### Retrieval-augmented, planner-driven loop

Native function-calling is unreliable on small local models, so the loop is:

1. **Plan** — one cheap LLM call (or heuristic fallback) returns a JSON list of
   tool calls + a KB search query, given the user message + conversation.
2. **Execute** — run selected tools against in-process state and run a Qdrant
   semantic search over `hetu_kb` (+ `rca_incidents`). Bounded, parallel.
3. **Synthesize** — stream the final grounded answer with citations via SSE.

This degrades gracefully: if planning fails, default to KB search +
health/incident summary tools.

---

## 5. Knowledge base

- Collection `hetu_kb` in Qdrant. Vector dim auto-detected from the embedder.
- Sources (Phase 1): `docs/**/*.md`, top-level `*.md` (README, CHANGELOG,
  CHATBOT_QUICKSTART), `prompts`. Chunked ~800 tokens with overlap; payload =
  {source, title, heading, text, indexedAt}.
- Sources (Phase 2): live cluster snapshot (pods/events), Prometheus rule docs,
  external K8s docs / CVE descriptions (optional, behind a flag).
- Incidents/RCA already flow into `rca_incidents`; chat retrieval queries both.
- Re-index: on startup (best-effort, async) + `POST /api/v1/chat/reindex`.

---

## 6. API surface (analyzer)

| Method | Path | Purpose |
|---|---|---|
| POST | `/api/v1/chat` | Send message, SSE stream (events: `token`, `tool`, `citation`, `done`, `error`) |
| GET | `/api/v1/chat/conversations/{id}` | Fetch conversation history |
| POST | `/api/v1/chat/reindex` | Rebuild the KB index |
| GET | `/api/v1/chat/status` | KB size, embedding/LLM readiness |
| GET/PUT | `/api/v1/embedding/config` | Embedding provider/endpoint/model |
| POST | `/api/v1/embedding/discover-models` | Probe embedding endpoint |

SSE flows through the dashboard proxy unchanged (it passes the body through).

---

## 7. Conversation persistence

- Interface `ChatStore` with an in-memory default (works with zero deps).
- Optional Postgres-backed implementation (table `chat_conversations`,
  `chat_messages`) when `DATABASE_URL` is set — reuse `pkg/store/postgres`.

---

## 8. Safety

- All tools are **read-only**. No `kubectl exec`, no writes from chat.
- Pod/log reads go through `pkg/kube` (typed client), not shell subprocess —
  removes the command-injection surface in the Python version.
- Respect the namespace boundary rules; never surface secrets values.
- Token budget shared with the existing daily budget accounting.

---

## 9. Phasing

- **Phase 1 (this change set):** embedding config (API+UI); KB indexer over
  docs + incidents; agentic RAG chat engine + SSE; in-process tools;
  dashboard ChatPanel with streaming, tool chips, citations; deprecate Python.
- **Phase 2:** Postgres conversation persistence; live-cluster snapshot source;
  reranking; per-tool tracing spans; eval harness (golden Q→expected-citation).
- **Phase 3:** background "operator mode" (proactive findings → chat digests);
  MCP tool support; multi-cluster; feedback thumbs → retrieval tuning.

---

## 10. Eval (Phase 2)

Golden set of operator questions with expected tool(s) + expected citation
source(s). Score retrieval hit-rate, tool-selection precision, answer
groundedness (cited vs. uncited claims), latency, and token cost per query —
mirroring HolmesGPT's evals.

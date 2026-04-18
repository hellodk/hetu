# LLM Context Enrichment — Implementation Report

**Branch:** `deep-analysis-improvement-plan`
**Status:** Phases 1–3 shipped; Phase 4 (Vector DB) in progress.

---

## What the LLM receives today (before this branch)

| Field | Content | Truncation |
|---|---|---|
| Incident metadata | ID, severity, detected-at, affected services | — |
| Signals | timestamp, source, namespace/service, kind, title | details → 300 chars |
| System prompt | "You are a Kubernetes SRE expert. Answer concisely." | — |

**Gaps identified:**
- No cluster health state (overall score, resource pressure)
- No K8s Warning events (what was Kubernetes itself upset about?)
- No pod logs (most RCA-relevant signal is in stderr)
- No Prometheus metrics (CPU throttle, OOM rate, error rates)
- No prior incident context (same failure mode recurs weekly, LLM doesn't know)
- LLM output was unformatted plain text — JSON blobs rendered as a wall of text

---

## Phase 1 — Cluster health enrichment ✅

**File:** `src/analyzer/rca.go` → `buildPrompt()`

Added `## Cluster Health at Analysis Time` section:

```
Health Scores — Overall:72  Reliability:68  Security:90  Cost:85  Architecture:77
Resource Utilization — CPU:84.3% used (6.7/8.0cores)  Memory:91.2% used (14.6/16.0Gi)
Cluster State — Nodes:3  Pods:47 (4 unhealthy, 2 pending)  Events:12 warning / 3 critical
```

**Signal detail truncation** raised 300 → 500 chars (more log context per signal).

**LLM system prompt** (`handleAsk`) upgraded:
- Full K8s domain list (scheduler, kubelet, CNI, Prometheus, autoscaler)
- Explicit formatting rules: JSON → ` ```json ` blocks, kubectl → ` ```bash ` blocks
- Confidence percentage included when prior RCA exists

**Token budget impact:** ~150 additional tokens per prompt. Well within budget for 8K+ context models.

---

## Phase 2 — Live data enrichment ✅

**File:** `src/analyzer/rca.go` → `buildPrompt()` enrichment goroutines

Three optional sections fetched in parallel with a **5-second hard deadline** each:

### K8s Warning Events
```go
cs.CoreV1().Events(ns).List(ctx, metav1.ListOptions{
    FieldSelector: "type=Warning", Limit: 15,
})
```
Sorted newest-first, formatted as:
```
[15:04:05] Pod/api-server-7f9b: BackOff — Back-off restarting failed container (×47)
```

### Pod Logs (last 30 lines)
```go
cs.CoreV1().Pods(ns).GetLogs(pod, &corev1.PodLogOptions{
    Container: firstNonInitContainer, TailLines: &30,
}).DoRaw(ctx)
```
Multi-container pods: resolves container name via `Pods.Get()` first, picks `Spec.Containers[0]`.
Truncated to 1000 chars per pod. Up to 3 unique pods per incident.

### Prometheus Metrics
```go
// CPU throttle rate per pod
rate(container_cpu_cfs_throttled_seconds_total{namespace="..."}[5m])

// Memory working set per pod (MiB)
container_memory_working_set_bytes{namespace="...", container!=""} / 1024 / 1024
```
Only runs if `PROMETHEUS_URL` env var is set. Response body capped at 512 KB.

**Wiring:** `SetEnrichmentSources(clientset, prometheusURL)` called from `main.go` after both
`workloadHandler` and `rcaEngine` are initialized. Uses existing `workloadHandler.Clientset()` — no new K8s client needed.

---

## Phase 3 — Similar incident search (keyword overlap) ✅

**File:** `src/analyzer/rca.go` — `buildFingerprint`, `findSimilarIncidents`

**How it works:**
1. When an RCA completes, extract tokens from incident signals (namespace, service, pod, kind, title, severity) into a `rcaFingerprint`.
2. At next RCA, compute same token set for the current incident.
3. Score all stored fingerprints by overlap count; threshold ≥ 2 matches, return top-2.
4. Inject as `## Similar Past Incidents` section — LLM references proven root causes.

**Why keyword works well for K8s:** The vocabulary is small and structured. `{namespace="payments", kind="oom", severity="critical"}` is a nearly unambiguous fingerprint.

**Lifecycle:** Fingerprints evicted alongside their parent `RCAReport` (orphan, TTL, and size-cap eviction all prune both maps atomically under the same mutex).

---

## Phase 4 — Semantic vector search (in progress)

Keyword overlap misses synonyms: `"connection refused"` ≠ `"connection failed"`, `"OOM killed"` ≠ `"out of memory"`. A vector DB solves this via embedding similarity.

**Architecture:**
- **Qdrant** (Rust, ~60MB Docker image) as the vector store
- **Embedding model:** `nomic-embed-text` via the existing Ollama server — zero new infrastructure
- **Both searches run:** vector results preferred (semantic quality); keyword fills gaps when Qdrant is unreachable
- **Docker Compose** bundles Qdrant alongside analyzer + dashboard

See `docker-compose.yml` at repo root and `src/analyzer/vectorstore.go`.

---

## Frontend changes ✅

**File:** `src/dashboard/app/incidents/[id]/page.tsx`

| Feature | Implementation |
|---|---|
| Code block copy button | `MarkdownPre` with `useRef<HTMLPreElement>`, clipboard API, Check→Copy icon after 1.5s |
| Language-based syntax colors | `bash/sh` → yellow · `json` → cyan · `yaml` → orange · `go` → blue · default → green |
| Chat history persistence | `localStorage` keyed `incident-chat-{id}`, load on mount, save on change, cap 50 messages |
| Clear history | Also calls `localStorage.removeItem(key)` |

---

## Audit fixes ✅

| Issue | Fix |
|---|---|
| No timeout on `handleRegenerate` | `context.WithTimeout(2 * time.Minute)` |
| No timeout on `handleAsk` LLM call | `context.WithTimeout(60 * time.Second)` |
| No question length validation | Reject empty or > 2000-char questions with 400 |
| Unbounded history sent to LLM | Cap `body.History` to last 20 messages before building message list |
| Prometheus `io.ReadAll` unbounded | `io.LimitReader(resp.Body, 512*1024)` |
| Pod logs silent fail on multi-container | Resolve `Spec.Containers[0].Name` via `Pods.Get()` before `GetLogs` |

---

## Pending items

| # | Item | Priority |
|---|---|---|
| 1 | **Phase 4 Vector DB** — `vectorstore.go` + docker-compose Qdrant | High |
| 2 | **Playwright tests for incidents page** — chat history, copy button, RCA render | Medium |
| 3 | **`QDRANT_URL` + `QDRANT_EMBED_MODEL` in `run-local.sh`** — add to doctor checks | Low |
| 4 | **Token budget telemetry** — expose `tokensUsed` / `dailyBudget` in `/api/v1/status` | Low |
| 5 | **Signal-time enrichment** — capture pod logs + Prometheus snapshot *when the signal arrives*, not at RCA time (logs gone if pod restarted) | Future |
| 6 | **Streaming RCA** — SSE stream from `/incidents/{id}/rca/regenerate` so UI can show progress | Future |

---

## Context window budget (for reference)

| Section | Approx tokens |
|---|---|
| Incident metadata + signals (10 signals × 500 char details) | ~800 |
| Cluster health snapshot | ~80 |
| K8s Warning events (15 events) | ~300 |
| Pod logs (3 pods × 30 lines) | ~600 |
| Prometheus metrics (2 namespaces) | ~80 |
| Similar past incidents (2 × summary) | ~100 |
| System prompt | ~350 |
| **Total** | **~2,300** |

Leaves ~5,700 tokens for the LLM response on an 8K model; ~13,700 on a 16K model.

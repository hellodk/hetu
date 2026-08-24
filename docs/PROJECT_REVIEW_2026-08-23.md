# hetu — Full-Stack Project Review & Agentic Workload Enhancement Plan

Date: 2026-08-23
Scope: whole repo (Go collector/analyzer, Next.js dashboard, deploy artifacts)
Method: four parallel principal-engineer reviews (Architect+Performance,
SRE+Observability, DevOps+CI/QA, UI/UX+Frontend) plus a dedicated deep-dive on
the agentic RAG chat workload. Every finding below cites file:line that was
actually read this session. Cross-checked claims were independently confirmed
by ≥2 reviewers unless noted.

---

## 0. Remediation status

Tracked under GitHub issue #12. Branch `fix/phase-1-criticals`.

### Phase 1 — same-day criticals (implemented this session)

| Item | Fix | Tests |
|------|-----|-------|
| C1 mock-data fallback | `fetchEvents` live-mode DNS fallback removed (`src/analyzer/main.go`); failure now surfaces via existing diagnostic-report path | `fetch_events_live_test.go` — red first ("nil error with 3 events" reproduced the bug) |
| C2 WS concurrent writes | `wsWriter` mutex wrapper; used by pod-log heartbeat/scanner/error paths and pod-exec stream adapter | `workload_ws_test.go` — 8-goroutine interleaved writes, passes `-race` |
| trace_id/service logging | `buildLogger`, `InitWithService`, `TraceHook`; both binaries stamped `service=hetu-analyzer`/`hetu-collector`; `WithRequestID` bakes trace fields when a span ctx exists | `logger_internal_test.go` (5 tests) |
| Readiness semantics | Analyzer `ready` atomic set on first published report; `/readyz` 503→200 monotonic; chart readinessProbe → `/readyz` | `readyz_test.go` (3 tests) |
| Chat citations dropped | ChatWidget registers `onCitation`, renders grounding chips (`data-testid="chat-citation-chip"`); corepack pinned pnpm in dashboard package.json | `chat-widget.spec.ts` citation test (6/6 pass) |

Verified locally: all module suites green incl. `-race` (pkg/logger,
pkg/config, pkg/bus, src/analyzer, src/collector), go vet clean across all 10
modules, tsc clean, chat-widget spec 6/6.

Deliberately deferred within Phase 1: collector `/readyz` is still an
unconditional-200 stub (chart still points collector at /healthz until that
has real state — Phase 2); OTEL SDK initialization (Phase 2, needs exporter
dep decision).

### Phase 2 — this week (next)
CI pipeline (lint + race + e2e + helm lint) · route runLLMAnalysis +
error-group LLM calls through pkg/llm.Client · snapshot fixes for the three
races in C4 · theme-token migration (workloads/incidents/errors pages) ·
collector real /readyz (informer-synced) · OTEL OTLP init in both binaries.

### Phase 3 — next sprint
Chat Tier-1: Postgres conversation persistence (+ migrations Job finally
calling MigratePostgres), streamed-token budget accounting, cluster_intel_chat_
* metrics · ingestion dedup key at ErrorAggregator boundary · single-flight
RCA · compose auth for qdrant + remove dead redis service · WS zombie-reconnect
fix + EventSource/ResizeObserver leaks · aria-live/focus-trap a11y batch.

### Phase 4 — backlog
Agentic loop Tier-2 (observe→re-plan, ≤5 hops, abort propagation) · hybrid
retrieval + rerank + golden-set eval harness · react-query adoption · git
history rewrite (227MB) · version-bump automation (VERSION vs tags vs
appVersion drift) · HPA/PDB enablement · Bedrock removal-or-implement ·
OBSERVABILITY_ARCHITECTURE.md rewrite against reality.

---

## 1. Executive summary

hetu has genuinely strong bones: disciplined eviction design
(`eviction.go`), a well-instrumented LLM client (`pkg/llm/client.go`),
hardened pod securityContext defaults, hermetic Playwright tests, XSS-safe
markdown, and an honest degraded-state story in the UI. The problems are
concentrated in five areas:

| # | Theme | Worst offender |
|---|-------|----------------|
| 1 | **Trust**: live mode silently serves fabricated telemetry | `main.go:1288` |
| 2 | **Concurrency**: data races and lock-scope bugs in hot paths | `workload_ws.go:106`, `errors_neardup.go:224` |
| 3 | **Observability is aspirational**: OTEL SDK never initialized; docs describe a Python system that doesn't exist | `pkg/logger/logger.go:16` |
| 4 | **No enforcement layer**: no CI at all; gates live only in bypassable local hooks | no `.github/` |
| 5 | **Agentic loop is single-shot**: plan→retrieve→synthesize once; no iteration, no evals, citations not enforced or rendered | `chat.go:213-244` |

---

## 2. Critical issues (fix first)

### C1. Live profile fabricates telemetry on DNS failure — VERIFIED
`src/analyzer/main.go:1283-1289`. If the collector hostname doesn't resolve,
`fetchEvents` returns `mockTelemetryEvents()` **in live mode**. Synthetic
CrashLoopBackOff events get scored, correlated into incidents, and displayed as
real cluster state. This contradicts the documented invariant at `main.go:73-76`.
Two reviewers found it independently.
**Fix:** delete the fallback; return an error and surface a diagnostic report.
Mock data belongs to `PROFILE=mock` only.

### C2. Concurrent WebSocket writes → panic under load
`src/analyzer/workload_ws.go:106-136`. Heartbeat goroutine (`WriteJSON`) and
scanner loop (`WriteMessage`) write one gorilla websocket concurrently with no
mutex. Gorilla forbids concurrent writers.
**Fix:** per-connection write mutex or single writer goroutine.

### C3. Near-duplicate scan holds the aggregator write lock across embedding HTTP calls
`src/analyzer/errors_neardup.go:224-318` + `errors_embeddings.go:166-186`.
With embeddings enabled, cache misses do a 15s-timeout HTTP call inside
`ea.mu.Lock()` held across an O(n²) loop. First scan after enabling can block
all ingest + all `/api/v1/errors/*` handlers for minutes.
**Fix:** compute vectors/signatures outside the lock; score against a snapshot.

### C4. Data races on shared state (verified patterns)
- `rca.go:182-261` / `correlator.go:98-115`: `GetIncident` returns the live
  `*Incident`; RCA prompt-builder iterates `Signals` unlocked while
  `IngestSignal` appends. Fix: value-copy snapshot from `GetIncident`.
- `errors_llm.go:55,250-273`: async analysis goroutine reads `*ErrorGroup`
  fields while ingest mutates them during the 60s LLM call. Fix: pass an
  immutable snapshot struct.
- `main.go:1469-1481`, `errors_llm.go:57-80`: runtime-mutable LLM config read
  bare (writes happen under `configMu`). Fix: locked accessors like the
  existing `getLLMEndpoint()` (`main.go:205-209`).
Run `go test -race ./...` as the gate.

### C5. Zero application-level authentication
`grep` confirms no token validation anywhere in `src/analyzer/*.go`;
`pkg/middleware/middleware.go` exports only CORS + rate-limit. Keycloak +
oauth2-proxy exist (`deploy/keycloak/`, `templates/ingress-sso.yaml`) but are
opt-in edge-only for the dashboard path. The analyzer API includes mutating
routes (`POST /api/v1/pods/health/scan`, `PUT /api/v1/lb/config`) plus a pod
exec terminal. In compose mode port 18081 is host-published unauthenticated;
CORS defaults to `*` (`main.go:2005-2007`).
**Fix:** wire OIDC token validation on every non-`/health` `/metrics` route;
default CORS deny.

### C6. No CI system
No `.github/`, no GitLab CI, no Jenkinsfile. gofmt/vet/golangci-lint/gosec/
secret-scan run only in `.githooks/pre-commit`, bypassable via
`BYPASS_HOOKS=1`. No `go test`, no race detector, no `next build`, no
`helm lint`, no image publish automation, no scanning/SBOM/signing.
**Fix:** minimal GitHub Actions: lint + `go test -race` + playwright + helm
lint/template on PR.

### C7. Observability doc describes a different system
`docs/OBSERVABILITY_ARCHITECTURE.md:3-6` documents "hetu-chatbot, FastAPI,
port 8000, python-json-logger, chatbot_* metrics". The actual system is Go on
8080/8081 with zerolog and `cluster_intel_*` metrics. The Watchdog alert it
claims does not exist anywhere in the chart.
**Fix:** rewrite against reality (see §4).

### C8. Distributed tracing does not exist despite shipping Tempo + OTel Collector
`otel.Tracer()` is called (`pkg/llm/client.go:164`) but no SDK/exporter is
ever initialized → global no-op tracer → zero spans ever reach the shipped
Tempo pipeline. No trace_id in logs either (`pkg/logger/logger.go:16-47`
carries only HTTP request_id). The `llm-traces.json` Grafana dashboard is
permanently empty.
**Fix:** initialize OTLP tracer provider in both binaries; add zerolog hook
for trace_id/span_id; propagate context collector←analyzer.

---

## 3. Discipline findings

### 3a. Architecture (Principal Architect)

| Sev | Finding | Where |
|-----|---------|-------|
| HIGH | Four parallel LLM client implementations; hottest paths (health analysis `runLLMAnalysis`, error groups, chat synthesis) bypass the instrumented `pkg/llm.Client` → no metrics/budget/tracing on them | `main.go:1452-1538`, `chat.go:513-630` |
| HIGH | Retry/backoff/circuit-breaker fields exist but are dead code — no retry loop anywhere; transient 429/503 fails the tick | `pkg/llm/client.go:157-174` |
| HIGH | Duplicate ingestion: collector re-emits on resync, analyzer re-ingests full event list every tick, pod-health bridge re-ingests every 2 min → phantom counts + spurious paid LLM analyses | `collector/main.go:241-250`, `main.go:995-1014`, `888-917` |
| MED | No single-flight on RCA: N browser tabs = N concurrent full analyses, last-write-wins | `rca.go:532-607` |
| MED | Unbounded goroutine fan-out from `triggerAnalysis` on error storms (only per-fingerprint throttle) | `errors.go:564-575` |
| MED | Layering violation: `BuildScoreInput` locks six other components' mutexes and reads their private fields; no interfaces → subsystem unit-testing impossible | `scoring.go:441-592` |
| MED | JSON encoding runs under exclusive locks → slow dashboard client backpressures ingestion | `errors.go:1393-1439`, `correlator.go:304-325` |
| LOW | Bedrock provider cannot work (no SigV4 signing) — dead feature advertised | `pkg/llm/client.go:719-728` |
| LOW | Divergent env helpers: collector's `getEnvIntOrDefault("BUFFER_SIZE")` parse-error → 0 → division-by-zero panic in ring buffer Push | `collector/main.go:770-777` |

### 3b. Performance (Principal Performance)

| Sev | Finding | Where |
|-----|---------|-------|
| HIGH | Metrics ring sized 10K *events* but filled per-pod-per-scrape → torn scrape mix ≥10K pods; ceiling not nodes but pods | `collector/main.go:170-171,470-509` |
| HIGH | Full-buffer copy + multi-MB JSON encode per poll; timeline endpoint refetches collector with zero caching on every dashboard hit | `collector/main.go:613-667`, `main.go:2743-2754` |
| HIGH | O(n²) near-dup scan under write lock (see C3); correlator linearly scans all open incidents × signals per ingested signal, incidents persist 48h | `correlator.go:102-116` |
| MED | SSE fan-out marshals full report once per subscriber instead of once per tick | `main.go:2084-2096` |
| MED | Anomaly detector: duplicate anomaly IDs per scan while z>3 persists (~20 dupes/h each); stats cap 1000 constantly evicted at 10K pods → rolling history <10 samples → detection silently disabled at scale | `anomaly.go:110-184` |
| MED | RCA enrichment: goroutine per namespace (unbounded), two sequential Prom queries per namespace inside one 5s budget | `rca.go:318-360,1027-1034` |
| LOW | OpenAI path drops split token counts → per-direction token metrics always 0 there | `pkg/llm/client.go:336-339` |
| LOW | HealthyPods KPI = TotalPods − len(TopIssues) → meaningless number | `main.go:1823` |

### 3c. SRE (Principal SRE)

| Sev | Finding | Where |
|-----|---------|-------|
| CRIT | Mock-data fallback (C1 above) | `main.go:1283-1289` |
| HIGH | Readiness probes hit `/healthz` which unconditionally 200s → wedged-but-up pods stay in endpoints; collector's real `/readyz` unused by chart | `analyzer.yaml:89-94`, `main.go:1883-1886` |
| HIGH | No alert exists for hetu's own components down; analysis-loop death produces zero alerts and a frozen dashboard | `monitoring/prometheus-rules.yaml` |
| HIGH | ServiceMonitor/PrometheusRule lack the conventional `release:` selector label → kube-prometheus-stack likely ignores both; everything default-off too | `servicemonitor.yaml:7-8` |
| MED | Qdrant failure = silent empty KB: `Search` swallows errors without logging; chat answers tools-only with no signal | `vectorstore.go:124-168` |
| MED | Alertmanager config renders a ConfigMap nothing consumes | `templates/monitoring/alertmanager-config.yaml:12-16` |
| MED | docker-compose mode has zero observability wiring (no Prometheus/Grafana/scrape) | `docker-compose.yml` |
| LOW | Chart README drifts from values.yaml (image tags, store bundling, LLM default) — 2am hazard | `deploy/helm/hetu/README.md:50-102` |

### 3d. Observability (Principal Observability)

| Sev | Finding | Where |
|-----|---------|-------|
| CRIT | OTEL SDK never initialized; Tempo + OTel Collector shipped but permanently empty; no trace_id in logs | repo-wide grep |
| HIGH | Primary LLM path bypasses all instrumentation → headline `OllamaProviderUnreachable` alert cannot fire from the analysis path | `main.go:1452-1538` vs `prometheus-rules.yaml:29-35` |
| HIGH | Chat path: zero metrics (requests/latency/tool outcomes); streamed tokens invisible to daily-token-budget accounting; logs lose request_id exactly where debugging is hardest | `chat.go:171-244,233` |
| MED | Dead duplicate LLM instrumentation file registers on wrong registry; unreferenced | `src/analyzer/llm_metrics.go:100-177` |
| MED | Prometheus-dependency failures quiet: optimizers silently skip; no query-error counter | `optimizer_cluster.go:16` |
| LOW | Logs lack static `service` field; streams indistinguishable when aggregated | `pkg/logger/logger.go` |

### 3e. DevOps / CI-QA (Principal DevOps)

| Sev | Finding | Evidence |
|-----|---------|----------|
| HIGH | ~100MB of venv/node_modules/.next webpack caches permanently in git history (initial commit) → `.git` = 227MB. Binaries are NOT tracked (corrected assumption) | `git log --all -- venv` |
| HIGH | docker-compose publishes qdrant (6333/6334) + redis (6379) to host with no auth; redis service referenced by nothing (dead) | `docker-compose.yml:12-14,98` |
| MED | Migrations exist (`migrations/postgres/`) but nothing applies them — `MigratePostgres` has zero callers; no chart migration Job → schema drift guaranteed on fresh installs | `pkg/store/migrate.go:19` |
| MED | No backup/DR story for qdrant volume | grep: empty |
| MED | Version drift three ways: VERSION=7.0.0, git tags up to v7.4.0, chart appVersion=7.0.0 → deployed images don't match tagged source | `VERSION`, `Chart.yaml` |
| MED | Go test coverage ~20% (13 `_test.go` vs 63 source files); **zero tests for chat/RCA/scoring-critical paths beyond scoring** | find output |
| MED | Base images tag-pinned not digest-pinned; `RUN GOWORK=off go mod tidy` inside Dockerfiles mutates deps at build time | Dockerfiles |
| LOW | Dashboard image builds with `npm ci` — violates pnpm-only convention | `src/dashboard/Dockerfile:10` |
| GOOD | Secrets handling solid: existingSecret pattern, randAlphaNum persisted via lookup, no secrets in values examples | `standalone-stores.yaml:109-129` |
| GOOD | securityContext hardened: non-root, RO rootfs, drop ALL caps, seccomp | `values.yaml:37-51` |

### 3f. UI/UX (Principal UI/UX)

| Sev | Finding | Where |
|-----|---------|-------|
| CRIT | Static dark-theme utilities (`text-white`, `gray-8xx`) on light-default theme → white-on-cream headings, charcoal panels; 91+ occurrences across workloads/incidents/errors pages + WorkloadActions + PodExecTerminal | `…/[name]/page.tsx` (62×), `errors/page.tsx` (29×) |
| CRIT | Streaming chat invisible to screen readers — messages container lacks `aria-live`/`role="log"` (incident chat does it correctly, so the pattern exists in-repo) | `ChatWidget.tsx:145` |
| HIGH | GlobalSearch modal: no focus trap/dialog semantics; Tab escapes behind overlay | `GlobalSearch.tsx:349-513` |
| HIGH | Second, worse modal implementation for destructive actions (scale/restart/delete): no trap, no Escape, no role | `WorkloadActions.tsx:170-185` |
| HIGH | Workload actions leave stale UI — `onAction` never passed so scale/restart/delete don't refresh anything | `…/[name]/page.tsx:1013-1019` |
| MED | Fake "Data Refresh Mode" setting — uncontrolled select that changes nothing | `SettingsModal.tsx:272-280` |
| MED | Auto-fires a paid LLM RCA stream merely by visiting an incident page | `incidents/[id]/page.tsx:194-201` |
| MED | Timestamps: 43 `toLocale*()` calls, browser-local TZ, inconsistent formats, no absolute+relative pairing | various |
| LOW | Incident detail loading/error states bare vs overview's exemplary states | `incidents/[id]/page.tsx:261-262` |
| GOOD | Delete requires typing resource name; Modal.tsx traps focus properly; skip-link present; markdown XSS-safe (no rehype-raw) | verified |

### 3g. Frontend engineering

| Sev | Finding | Where |
|-----|---------|-------|
| HIGH | Zombie WebSocket after leaving pod logs — reconnect timer re-fires on unmounted component (up to 30s later) | `…/[name]/page.tsx:398-442` |
| MED | EventSource leak on manual RCA regeneration (no abort ref kept) | `incidents/[id]/page.tsx:110-216` |
| MED | PodExecTerminal leaks ResizeObserver (cleanup returned by async handler, discarded) | `PodExecTerminal.tsx:100-113` |
| HIGH | Render-per-token O(n²) chat streaming: array copy + fresh ReactMarkdown parse + smooth scrollIntoView per token | `ChatWidget.tsx:81-176` |
| MED | Citation events silently dropped — backend emits `citation` frames, lib defines `onCitation`, widget never registers it → **grounding chips never render** | `lib/chat.ts:86-88`, `ChatWidget.tsx:75-87` |
| MED | Hand-rolled fetch everywhere; no react-query/SWR; ad-hoc polling (60s/30s) with no cancellation → out-of-order response races | multiple pages |
| MED | AIInsightFeed awaits 6 APIs sequentially before first paint | `AIInsightFeed.tsx:44-128` |
| LOW | Chat conversation dies on reload (incident chat persists to localStorage, widget doesn't) | `ChatWidget.tsx:23-27` |
| MED | Triplicated theme system (Navigation, SettingsModal, FOUC script); triplicated kind→route maps; two modal implementations; two log viewers | listed files |
| MED | ~48 TypeScript `any` leaks incl. entire K8s object typed `any` | `…/[name]/page.tsx:811` |

---

## 4. Agentic workload — how it works today

Source of truth: `src/analyzer/chat.go`, `chat_tools.go`, `chat_kb.go`,
`docs/AI_CHAT_RAG_PLAN.md`.

```
 Operator asks question (dashboard ChatWidget)
        │  POST /api/v1/chat  (SSE)
        ▼
 ┌─────────────────────────── ANALYZER (Go) ───────────────────────────┐
 │                                                                      │
 │  1 PLAN      one LLM call → JSON {kb_query, tools[≤3]}              │
 │              ├─ LLM unreachable → keyword heuristicPlan()           │
 │              └─ invalid tool names dropped; namespace injected      │
 │                                                                      │
 │  2 RETRIEVE  (concurrent, one-shot)                                 │
 │    ├── Tools (read-only, in-process):                               │
 │    │     get_cluster_health · list_incidents · list_error_groups   │
 │    │     list_recommendations · list_security_findings             │
 │    │     get_pods (typed k8s client, ≤200 pods, ≤40 shown)         │
 │    │     query_prometheus (instant PromQL)                          │
 │    └── KB: Qdrant semantic search over hetu_kb (+ incidents)       │
 │          docs/**/*.md chunked ~800 tok · EmbeddingScorer            │
 │                                                                      │
 │  3 SYNTHESIZE  grounded prompt (system rules + last 8 turns +       │
 │                retrieved blocks) → streamed tokens via SSE          │
 │                openai-compat / ollama native streaming;             │
 │                anthropic/bedrock/azure = blocking                   │
 │                LLM fails → deterministic fallback text              │
 │                                                                      │
 │  ✗ citation frames emitted but ChatWidget drops them                │
 │  ✗ streamed tokens bypass pkg/llm → no metrics, no budget accounting│
 │  ✗ conversations in-memory only (40-turn cap, lost on restart)      │
 └──────────────────────────────────────────────────────────────────────┘
```

### Honest assessment vs the "agentic" label

What exists is a **single-shot retrieval pipeline**, not an agent:

| Agentic capability | HolmesGPT/kagent bar | hetu today |
|---|---|---|
| Iterative tool loop (observe → re-plan) | yes | **no — one plan, one retrieve, done** |
| Tool results feeding next tool call | yes | no (tools can't chain, e.g. incident → its pods → their logs) |
| Multi-step reasoning traces shown to user | partial | tool chips emitted upfront, results not surfaced |
| Citation verification | no | planned but broken end-to-end (dropped in UI, unvalidated in text) |
| Conversation persistence | varies | memory-only |
| Eval harness for answer quality | no (industry gap) | none |
| Read-only safety | yes | **yes — genuinely good design** |

The planner is capped at 3 tools, executed in parallel, then synthesized. If
the model needs one more hop ("which node is that pod on?"), it cannot ask.

### What is already strong (keep)

- Read-only tool catalogue with typed clients — no shell injection surface
- Heuristic fallback keeps chat useful with no LLM
- Deterministic fallback answer if synthesis fails
- Graceful degradation ladder: Qdrant off → tools-only; LLM off → summary
- Namespace context injection + tool-name validation on plan output

## 5. Making the agentic workload awesome + RAG compliant

### Target architecture

```
                    ┌────────────────────────────┐
                    │  Operator (ChatWidget)     │
                    │  tokens · tool cards ·     │
                    │  citations · plan trace    │
                    └─────────────┬──────────────┘
                                  │ SSE (token|tool_result|citation|plan|done)
                    ┌─────────────▼──────────────┐
                    │      ChatEngine v2 (Go)     │
                    │                             │
                    │  ┌─── agentic loop ─────┐  │
                    │  │ PLAN → ACT → OBSERVE │◄─┼── max N iterations (e.g. 5)
                    │  │   ▲        │         │  │   budget-guarded (tokens,
                    │  │   └── need more?      │  │   wall-clock, tool count)
                    │  └───────────┬───────────┘  │
                    │              ▼               │
                    │  SYNTHESIZE + self-check     │
                    │   • every claim must map     │
                    │     to a citation id         │
                    │   • verifier pass flags      │
                    │     uncited claims           │
                    └──┬──────┬──────┬──────┬─────┘
                       │      │      │      │
          ┌────────────▼┐ ┌───▼────┐ ┌▼─────┐ ┌▼──────────────┐
          │ TOOL LAYER  │ │ VECTOR │ │ BM25 │ │ MEMORY        │
          │ (typed, RO) │ │ Qdrant │ │ kw   │ │ Postgres conv │
          │ + NEW:      │ │ hetu_kb│ │ index│ │ + summaries   │
          │  describe_  │ │ rca_   │ │(hybrid│ │ + entity refs │
          │  pod_logs   │ │ incids │ │ fusion)│ └───────────────┘
          │  get_events │ └───┬────┘ └───┬───┘
          │  get_workload│    │          │
          │  kubectl_explain-like│    │
          └──────┬──────┘     │          │
                 │            ▼          ▼
                 │      ┌──────────────────┐
                 │      │ INGESTION PIPELINE│
                 │      │ docs+incidents+  │
                 │      │ runbooks+K8s docs│
                 │      │ chunk→embed→upsert│
                 │      │ hash-diff reindex│
                 │      └──────────────────┘
                 ▼
        ┌──────────────────┐
        │ EVAL HARNESS     │
        │ golden Q/A set   │
        │ retrieval recall │
        │ citation F1      │
        │ nightly in CI    │
        └──────────────────┘
```

### Enhancements, prioritized

**Tier 1 — correctness of what already ships**
1. **Render citations** (`ChatWidget.onCitation`) and validate synthesis:
   post-process the answer, flag any `[doc:...]`/`[incident #N]` reference that
   wasn't actually retrieved; append a grounding disclaimer if >0 unmatched.
2. **Route chat synthesis through `pkg/llm.Client`** + count streamed usage
   (Ollama `done` chunk carries eval counts) so daily budget is honest.
3. **Persist conversations** (the planned `ChatStore` Postgres impl — table
   DDL is already in `docs/AI_CHAT_RAG_PLAN.md`; `MigratePostgres` finally
   gets a caller).
4. Fix chat-path logging (`log.Ctx(ctx)`), add `cluster_intel_chat_*` metrics
   (requests, stream duration, tool_calls_total{tool,status}).

**Tier 2 — true agentic loop**
5. **Iterate**: after retrieve, let the planner see tool results and decide
   "enough" or name follow-up tools (bounded: ≤5 hops, global semaphore,
   per-conversation token meter). This converts the one-shot pipeline into an
   observe→re-plan agent without native function calling (keeps small-model
   compatibility — same rationale as today's JSON-plan design).
6. **Tool chaining data**: `list_incidents` should return affected pod names in
   machine-readable form so hop 2 (`describe_pod`, `get_pod_logs`) can consume
   them; add `get_events(namespace,involvedObject)` and
   `get_pod_logs(pod,tail)` tools — the single highest-value additions for
   troubleshooting realism.
7. **Single-flight per conversation** + queue position event; abort propagation
   from SSE disconnect → cancel downstream LLM calls (today a closed tab keeps
   burning tokens until the 5-min client timeout).

**Tier 3 — RAG compliance (retrieval quality discipline)**
8. **Hybrid retrieval**: dense (Qdrant) + sparse (BM25) fused via reciprocal
   rank fusion; then a cross-encoder rerank of top-20 → top-5. Pure dense
   recall on ops vocabulary ("CrashLoopBackOff", "oomkilled") is measurably
   worse than hybrid.
9. **Chunking hygiene**: keep heading breadcrumb in every chunk payload
   (already stored — start returning it as part of the citation), respect code
   fences as atomic chunks, dedupe near-identical chunks by hash before embed.
10. **Freshness**: hash-diff re-index (skip unchanged files) instead of full
    10-minute rebuild; stamp `indexedAt` and prefer fresher chunks on score
    ties; include incident/RCA embeddings within seconds of creation via hook,
    not next reindex.
11. **Grounded-answer contract**: system prompt already forbids invention —
    make it verifiable: require the model to emit citation markers inline
    (it tries), then mechanically verify coverage; expose "groundedness" as a
    metric (`cluster_intel_chat_citations_total` / uncited-claim ratio).
12. **Eval harness** (the actual compliance gate): golden set of ~30 Q/A pairs
    built from this repo's own incidents/docs; measure retrieval recall@5 and
    citation precision; run nightly in CI against the configured local models.
    Without this, every prompt tweak is unfalsifiable.

**Tier 4 — experience polish**
13. Buffer tokens (rAF flush), memoize markdown per turn, smart auto-scroll.
14. Persist widget conversation to localStorage like the incident page does.
15. Surface the plan trace ("I checked incidents, then pulled pods in ns X")
    — the tool chips exist but arrive before results; pair result cards with
    them.

---

## 6. Suggested fix order (cross-discipline)

1. **Same-day**: C1 mock-data fallback · C2 WS write mutex · C8 OTLP init +
   trace_id hook · chat citation rendering · readiness probe semantics
2. **This week**: CI pipeline (lint + race + e2e + helm lint) · route all LLM
   paths through `pkg/llm.Client` · snapshot fixes for the three races (C4) ·
   theme-token migration on the three worst pages
3. **Next sprint**: Tier-1 chat items (persistence, budget honesty, metrics) ·
   ingestion dedup key · single-flight RCA · qdrant/redis compose auth +
   remove dead redis · migrations Job
4. **Backlog**: Tier-2/3 agentic loop + hybrid retrieval + eval harness ·
   react-query adoption · git history rewrite (227MB) · version-bump
   automation · HPA/PDB enablement · Bedrock removal or implementation

# Errors feature — improvement plan

**Status:** Phase 1.1 shipping first; remainder tracked as follow-up work.

## Why this exists

The errors page today is a working Sentry-style MVP — deterministic fingerprint,
in-memory ring buffers, one manual LLM button — but it has rough edges that
make it hard to *read*, hard to *trust the grouping*, and underused as a source
of LLM-backed insight. This doc is the concrete plan to fix all three.

## Starting point (audit)

| Area | Today | File / line |
|---|---|---|
| Grouping key | `SHA1(service \| exceptionType \| top-5 stack frames)` — stack path; `SHA1(service \| level \| templated-msg)` fallback | `src/collector-podlogs/fingerprint.go:13-131` |
| Storage | in-memory map, 200 group cap, 50 occurrences/group ring buffer, 7-day TTL | `src/analyzer/errors.go:50-66`, `:152-186` |
| Sort | `last_seen desc` only | `errors.go:298-305` |
| Summary | hard-coded `topGroups[:10]`, no pagination | `errors.go:230-233` |
| LLM touch | single button, top 5 open groups, markdown blob into `AISummary` | `src/analyzer/main.go:2311-2472` |
| Exemplar | "first 3 messages win" | `errors.go:121-126` |
| Correlation | none — errors float alone, no link to incidents/pod events/recs | — |
| Metrics | none — no `errors_ingest_total`, `errors_evict_total`, etc. | — |

## The plan — 3 phases

### Phase 1 — Analysis & readability (low risk, visible)

| # | Change | Files |
|---|---|---|
| 1.1 | **Rate buckets** — compute `count1m / count5m / count1h / count24h` from the occurrence ring buffer; expose on `/errors/groups`; surface sparkline + "spike" badge in list. Count-since-forever hides "50 in the last 5 minutes" inside a 12 000 all-time group. | `errors.go`, `page.tsx` |
| 1.2 | **Severity column** — surface `level` as its own sortable, coloured chip (fatal > error > warn). Today it drives icon colour only. | `page.tsx` |
| 1.3 | **Remove hard-coded 10-cap** — add `?limit=&offset=` on `/errors/summary` and `/errors/groups`; frontend pagination. | `errors.go`, `page.tsx` |
| 1.4 | **Detail-page filters** — time range, pod, container, message search; "evicted" watermark when `count > 50`. | `errors/[id]/page.tsx` |
| 1.5 | **Correlated context panel** — `GET /errors/groups/{id}/context` fanning out to correlator incidents, pod events (CrashLoop/OOM), optimizer recs for same ns/service. Errors currently float alone. | new handler in `errors.go` |
| 1.6 | **Prometheus metrics** — `errors_ingest_total`, `errors_fingerprint_miss`, `errors_evict_total{reason=ttl\|cap}`, `errors_llm_latency_seconds`. | instrumentation pass |

### Phase 2 — Intelligent de-duplication (medium risk)

Today's fingerprint is simultaneously **too strict** (same root cause, different
service → different hash because `service` is in the key) and **too loose**
(aggressive `\b\d{2,}\b → :n` can fuse distinct faults). Four concrete moves:

| # | Change | Rationale |
|---|---|---|
| 2.1 | **`faultKey`** — `SHA1(exceptionType \| top-3 frames)` without `service`. Stored alongside `fingerprint`. UI adds "cross-service roll-up" toggle so "DNS timeout in 7 services" becomes one row. | Same root cause, 7 services, today = 7 groups. |
| 2.2 | **Embedding near-dup (GATED)** — cheap local `bge-small` on `sampleMessage`; cosine ≥ 0.92 against last 50 groups merges into variant. Catches "connection refused" ↔ "connect: connection refused" that today's regex misses. Batch every 60s; bail fast when `len(groups) > 200`. | Real infrastructure — needs explicit approval. Risk: silent bad merges. Mitigation: keep raw fingerprint; merge is display-layer rollup. |
| 2.3 | **Manual merge/split UI** — `PATCH /errors/groups/{id}/merge-into/{target}`, `POST /errors/groups/{id}/split`. `MergedFrom []string` audit trail. | No fingerprint algo is 100% correct; operators need an escape hatch. |
| 2.4 | **Scored exemplar** — replace "first 3 wins" with: stack-trace > URL > longest unique > random. | Today an early bad sample dominates forever. |

### Phase 3 — LLM-based findings (medium risk)

Today's LLM is a one-shot summariser. It should be a triggered, **structured**,
budget-aware analyser. Replace `AISummary string` with a typed struct.

```go
type ErrorAnalysis struct {
    RootCause  string     `json:"rootCause"`
    Impact     string     `json:"impact"`
    Fix        string     `json:"fix"`
    Severity   string     `json:"severity"` // critical|high|medium|low
    Confidence float64    `json:"confidence"`
    Evidence   []Evidence `json:"evidence,omitempty"`
    Model      string     `json:"model,omitempty"`
    GeneratedAt time.Time `json:"generatedAt"`
}
type Evidence struct {
    Kind string `json:"kind"`   // incident|podEvent|optimizer|log
    Ref  string `json:"ref"`    // e.g. incident id, pod name, rec id
    Note string `json:"note,omitempty"`
}
```

| # | Change | Rationale |
|---|---|---|
| 3.1 | **Typed `Analysis *ErrorAnalysis`** replaces markdown `AISummary`. Frontend renders each field with its own affordance (severity → chip, evidence → clickable). | Markdown blobs rot fast; the UI can't do anything smart with them. |
| 3.2 | **Async triggers + token budget** — auto-analyse (a) on new group, (b) on rate spike (`count5m/count1h > 3×`), (c) on umbrella fault (≥3 groups share `faultKey`). Reuse `LLMTokenBudget.TryReserve`; skip when budget < 10%. | Today LLM runs only on click, and only on top 5 — 95% of groups never get an analysis. |
| 3.3 | **Signal-based confidence** — `confidence = 0.2×hasStackTrace + 0.1×multiPod + 0.2×correlatedIncident + 0.5×llmSelfReport` (clamped 0–1). Honest per `CONFIDENCE_SCORES.md`. | Today the LLM self-reports a number we print unchanged. |

## Rollout order

**1.1 → 1.2 → 1.3 → 2.1 → 2.3 → 3.1 → 3.3 → 1.5 → 2.4 → 2.2 → 1.4 → 3.2 → 1.6**

Rationale: cheapest / most-visible wins first. Embeddings (2.2) only after the
typed `Analysis` struct (3.1) exists so we have somewhere clean to put
similarity scores. Metrics (1.6) late because they measure the other work.

## Out of scope

- Persisting errors to Postgres (tracked in `ROADMAP.md` — independent effort).
- Auto-resolve on deployment (needs deployment-event source first).
- Cross-cluster error rollup (multi-cluster support is a different milestone).

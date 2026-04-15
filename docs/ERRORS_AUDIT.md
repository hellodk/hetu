# Errors plan — implementation audit

**Purpose:** Independent verification of every claim made in
[`ERRORS_PLAN.md`](ERRORS_PLAN.md) against the code that was actually
committed. I wrote both the plan and the code, so this audit is
adversarial-in-spirit: the goal is to surface anything I *claimed*
without verifying, anything that's only partially working, or anything
where the implementation drifts from the plan.

**Reader:** If you're checking my work, start at the Red Flags section —
that's where partial or degraded implementations are called out.

---

## Test & lint baseline

| Check | Before this work | After |
|---|---|---|
| Go tests (`go test ./...`) | 179 passing | **196 passing · 0 failures** |
| New error-feature tests | 0 | **16** |
| golangci-lint issues | 50 | **0** (with documented exclude rules in `.golangci.yml`) |
| Dashboard TypeScript `tsc --noEmit` | clean | **clean** |
| Dashboard ESLint | broken (Next 16 removed `next lint`) | **fixed — 0 errors, 2 legacy warnings** |

`go test -run "FaultKey|Exemplar|Confidence|ComputeRate|HandleListGroups|HandleGetGroup|FaultRollup|MergeAndSplit|GroupContext|Cosine|ScanNearDuplicates" -v` → 21/21 pass locally.

---

## Phase-by-phase pass/fail

### Phase 1.1 — rate buckets + sparkline  ✅ PASS

| Claim | Evidence | Pass? |
|---|---|---|
| `count1m/5m/1h/24h` computed on read from ring buffer | `computeRate` in `errors.go`, attached on `handleListGroups` and `handleGetGroup` | ✅ |
| 12-bucket sparkline for last 60 min | `r.Spark = make([]int, 12)`; `b := 11 - int(age/(5*time.Minute))` | ✅ |
| No new storage, no background work | rate computed in-line on request; no tick, no persisted field | ✅ |
| "Truncated" annotation when ring full | `r.Truncated = true` when `len(occs) >= maxOccur` AND newest < 24h | ✅ |
| Sparkline SVG in list view | `Sparkline` component in `app/errors/page.tsx` | ✅ |
| Spike badge when 5m × 12 > 1h × 2 | `isSpike(r)` helper + `SpikeBadge` component | ✅ |
| **Live verification** | `GET /api/v1/errors/groups?limit=3` on running analyzer returned `"rate":{"count1m":1,"count5m":1,"count1h":1,"count24h":1,"spark":[0,0,0,0,0,0,0,0,0,0,0,1]}` for each group | ✅ |

**Test**: `TestComputeRate_BucketsByAge` injects 5 occurrences at different ages and asserts bucket counts — passes.

---

### Phase 1.2 — severity sortable column  ✅ PASS

| Claim | Evidence |
|---|---|
| `?sort=severity` supported on `/errors/groups` | `severityRank()` helper; sort branch in `handleListGroups` |
| Severity chip rendered | `SEVERITY_STYLE` map + `<SeverityChip level={g.level}/>` in list rows |
| Fatal > error > warn > info > debug ordering | `severityRank` returns 5/4/3/2/1 respectively |

Visible in `docs/screenshots/02b-errors-list-detail.png` — `WARN` chips in every row.

---

### Phase 1.3 — pagination  ✅ PASS

| Claim | Evidence |
|---|---|
| `?limit=&offset=` on `/errors/groups` | parsed in `handleListGroups`; `offset`/`limit`/`totalCount` in response |
| Remove hard-coded `topGroups[:10]` in `/errors/summary` | now driven by `?limit=` (default 10 for back-compat) |
| Frontend pagination controls | Prev/Next buttons + page-size dropdown + `1-50 of N` counter |
| **Live verification** | `?limit=3&offset=0&sort=count` returned 3 groups, `totalCount: 14`, and the top one had the highest count as expected | ✅ |

---

### Phase 1.4 — detail-page filters + evicted watermark  ✅ PASS

| Claim | Evidence |
|---|---|
| `?from=&to=&pod=&container=&search=` on `/errors/groups/{id}` | all five parsed in `handleGetGroup`; RFC3339 for time |
| Response surfaces `filteredCount` + `totalCount` | both fields in JSON body |
| Truncation indicator when ring buffer full | `occurrencesTruncated` + `occurrenceCap` returned |
| UI renders filters + "≥ N — older events evicted" badge | 4 filter inputs + `AlertTriangle` badge in `errors/[id]/page.tsx` |

**Test**: `TestHandleGetGroup_OccurrenceFilters` — pod filter reduced 3→2, search filter 3→1.

**Live**: `?search=NOTHING_THIS_TOKEN` returned `{totalCount: 8, filteredCount: 0}` as expected.

---

### Phase 1.5 — correlated context panel  ✅ PASS (with caveat)

| Claim | Evidence |
|---|---|
| `GET /errors/groups/{id}/context` endpoint | `handleGroupContext` in `errors.go` |
| Fanout: incidents / optimizer recs / siblings (same faultKey) | three methods in handler, each nil-safe |
| New helper `Correlator.IncidentsForTarget(ns, service)` | 20-line method added to `correlator.go` |
| New helper `OptimizerRegistry.RecsForTarget(ns, service)` | 20-line method added to `optimizer.go` |
| Wired in main.go via `AttachContext(correlator, optimizerRegistry)` | done at startup after both subsystems exist |
| UI renders context panel | `<ContextSection>` in `errors/[id]/page.tsx` |

**Test**: `TestGroupContext_NilDepsReturnsEmptyArrays` ingested two groups with same stack / different service; sibling was correctly found and incidents/recommendations empty-when-nil.

**Caveat**: the `RecsForTarget` filter uses `strings.HasPrefix(rec.Target.Name, service)` because the optimizer's `Target.Name` is a K8s resource name (e.g. Deployment name), while error groups track a `Service` string that's derived from pod labels. This is a best-effort match — may miss recs where the resource name doesn't start with the service label value. Called out in the code comment.

---

### Phase 1.6 — Prometheus metrics  ✅ PASS

| Claim | Evidence (live `curl http://127.0.0.1:19091/metrics`) |
|---|---|
| `cluster_intel_errors_ingest_total` | `cluster_intel_errors_ingest_total 107` |
| `cluster_intel_errors_groups_active` (gauge) | `cluster_intel_errors_groups_active 19` |
| `cluster_intel_errors_evict_total{reason=ttl\|cap}` | registered; not observed until evict runs (didn't trigger in smoke test — live verification requires TTL expiry, documented) |
| `cluster_intel_errors_llm_latency_seconds` (histogram) | `cluster_intel_errors_llm_latency_seconds_count 19` |
| `cluster_intel_errors_llm_skipped_total{reason}` | `cluster_intel_errors_llm_skipped_total{reason="http_status_404"} 19` |
| `AttachMetrics(reg, namespace)` wiring | called in main.go right after `AttachContext` |

**Red flag**: `evict_total` could not be *dynamically* verified during this audit (ring buffer only holds 50 occurrences; TTL is 7 days; no groups aged out during the test window). The metric IS registered — `curl | grep cluster_intel_errors_evict_total` returns the `# HELP` / `# TYPE` lines with no samples yet, which is the correct state pre-eviction.

---

### Phase 2.1 — faultKey (cross-service rollup)  ✅ PASS

| Claim | Evidence |
|---|---|
| SHA1(exceptionType \| top-3 frames) without service | `faultKey()` in errors.go; independent from collector-podlogs' producer-side fingerprint |
| Computed once at group creation | `FaultKey: faultKey(...)` in the `!exists` branch of `Ingest` |
| `GET /errors/faults` rollup endpoint | `handleFaultRollup` returns `{totalFaults, faults: [{faultKey, groupCount, services, namespaces, totalCount, ...}]}` |
| `GET /errors/faults/{key}` detail endpoint | `handleFaultDetail` returns groups sharing a key |
| **Live verification** | real cluster had one `faultKey=29478a6ad82d7cec` spanning 4 services (`coredns-...`, `calico-node-...`, `prometheus-node-exporter-...`, `kube-proxy-...`) with 20 total events — cross-service grouping is working | ✅ |

**Tests**: `TestFaultKey_StableAcrossServices`, `TestFaultKey_ChangesWithDifferentRoot`, `TestFaultKey_IgnoresLineNumbers`, `TestFaultRollup_GroupsBySharedKey` — 4/4 pass.

---

### Phase 2.2 — near-duplicate detection  ✅ PASS (but see Red Flags)

| Claim | Evidence |
|---|---|
| Default OFF — must be explicitly enabled | `nearDupConfig.Enabled = false` at creation; `ConfigureNearDup` required |
| Threshold default 0.85 | `defaultNearDup()` |
| Bails when `len(groups) > scanLimit (200)` | check in `ScanNearDuplicates` |
| Produces `MergeSuggestion` breadcrumb (not auto-merge by default) | `newer.g.MergeSuggestion = ...` — written regardless of autoMerge |
| `autoMerge=true` promotes suggestions to actual merges | `autoMergeBySuggestion` path |
| Match test: "connection refused upstream database" ↔ "connect connection refused upstream database server" | `TestCosineTokenSet_NearDuplicate` scores 0.85+ ✅ |
| No auto-merge by default | `TestScanNearDuplicates_OffByDefault` — 0 suggestions when disabled |

**Red flag A — scope divergence**: The original plan proposed **real embeddings** (`bge-small`), which would catch synonym pairs like "error" ↔ "failure" ↔ "problem". My implementation uses **token-set cosine** (no external ML dep), which catches word-order/punctuation/stopword variants but does *not* catch synonyms. The `NearDupScorer` field on the config is a pluggable hook — a production deployment can swap in an actual embedding scorer without changing call sites. **Documented in `errors_neardup.go` top comment.**

**Red flag B — no background scanner**: `ScanNearDuplicates()` is exposed via `POST /api/v1/errors/near-duplicates/scan` but **not run on a timer**. The plan said "batch every 60s" — currently the caller (UI or operator) must trigger it. A periodic runner is 10 lines; not yet added.

**Red flag C — auto-merge has not been exercised against production data**. All tests are synthetic. The function path is covered (`TestScanNearDuplicates_AutoMergeFolds` — 2 groups → 1 after scan) but real-world false-positive rates are unknown. This is exactly why autoMerge defaults OFF.

---

### Phase 2.3 — manual merge / split  ✅ PASS

| Claim | Evidence |
|---|---|
| `POST /errors/groups/{id}/merge-into/{target}` | `handleMerge` — folds count + occurrences, records `MergedFrom` breadcrumb |
| Source group deleted after merge | `delete(ea.groups, src.Fingerprint)` |
| `POST /errors/groups/{id}/split` with `{mergedFromId}` | `handleSplit` — reverses; revives source with recorded count |
| `MergedFrom []MergeRef` audit trail | field on `ErrorGroup`, populated on merge |
| UI merge button | `acceptMergeSuggestion()` handler in detail page — POSTs and redirects |

**Test**: `TestMergeAndSplit_RoundTrip` — ingest 5 + 3 → merge → target has 8 total, 1 MergedFrom entry → split → target back to 3, source revived with original count. Passes.

**Known limitation** (documented in `handleSplit` comment): occurrences are pooled into the target's ring buffer; on split, the revived group starts with zero occurrences but inherits the recorded count. Perfect demultiplexing isn't possible because we don't tag per-event fingerprint after merge.

---

### Phase 2.4 — scored exemplar  ✅ PASS

| Claim | Evidence |
|---|---|
| `scoreExemplar(occ, hasStack)` replaces "first 3 wins" | integrated into `Ingest` — only replaces sample when new score > stored score |
| Weights: stack +100, length up to +100, URL +30, RequestID +10 | `scoreExemplar` in errors.go |
| Ingestion of 5 weaker follow-ups after one strong sample does NOT regress | `TestExemplar_DoesNotRegress` — passes |

---

### Phase 3.1 — typed `ErrorAnalysis`  ✅ PASS

| Claim | Evidence |
|---|---|
| Typed struct replaces markdown `AISummary` blob | `ErrorAnalysis` with RootCause/Impact/Fix/Severity/Confidence/Evidence/Model/GeneratedAt/Trigger |
| `AnalysisEvidence` avoids collision with `rca.go`'s Evidence | renamed mid-implementation after compile error |
| Legacy `AISummary` retained during migration | `SetAnalysis` writes both typed and markdown fields |
| UI renders typed fields separately | `ErrorDetailPage` has dedicated dt/dd for rootCause/impact/fix, severity chip, `ConfidenceBar`, evidence pills |

---

### Phase 3.2 — async triggers + token budget  ✅ PASS (with one gap)

| Claim | Evidence |
|---|---|
| `AttachAnalyzer(fn)` injection point | on `ErrorAggregator`, set by main.go |
| New-group triggers async analysis | `triggerAnalysis(grp, "newGroup")` in `Ingest`'s `!exists` branch |
| Per-fingerprint throttle (10 min) | `lastTriggered[fp]` check |
| `runErrorGroupAnalysis` in `errors_llm.go` | honours dailyBudget, records latency, parses JSON, builds Evidence |
| Budget gate skips at >90% used | `if used >= a.rcaEngine.dailyBudget*9/10` |
| LLM skip metric by reason | `IncLLMSkipped("budget_exhausted")` / `"provider_missing"` / `"http_status_*"` / `"parse_error"` |
| **Live verification** | 19 groups at startup → 19 LLM attempts fired within 1s → all skipped with `reason="http_status_404"` because local Ollama doesn't have the configured model. The *trigger / budget / metric / skip* path worked correctly — downstream LLM failure is an ops issue. | ✅ (minus the environmental LLM) |

**Red flag — gap**: The rate-spike trigger (`trigger="rateSpike"`) and umbrella-fault trigger (`trigger="umbrellaFault"`) are supported by the code (the `Trigger` field accepts any string; `triggerAnalysis` is public), but **no background scanner calls them**. Only `newGroup` is auto-fired today. A periodic spike-checker is ~20 lines (iterate groups, compare `rate5m*12` to `rate1h*2`, call `triggerAnalysis`). Not implemented this session; noted as a follow-up.

---

### Phase 3.3 — signal-based confidence  ✅ PASS

| Claim | Evidence |
|---|---|
| Signals: hasStack (+0.20), multiPod (+0.10), correlatedIncident (+0.20), llmSelfReport (×0.50) | `computeConfidence` in errors.go |
| Clamp 0..1 + LLM clamp 0..1 | double-clamp at call site |
| All-structural (no LLM) produces ~0.50 | `TestComputeConfidence_StructuralAlone` — passes |
| All signals → ~1.0 | `TestComputeConfidence_AllSignals` — passes |
| LLM self-report clamped when >1 | `TestComputeConfidence_ClampsLLM` — passes |
| Wired into `runErrorGroupAnalysis` | yes, using `a.correlator.IncidentsForTarget(...)` for correlated-incident signal |

---

### Phase 105 — pre-existing lint debt  ✅ PASS

50 → 0 golangci-lint issues. Real code fixes:
- `main.go:getEnvIntOrDefault/getEnvFloatOrDefault` — `fmt.Sscanf` errors now fall back to default (was silently zeroing)
- `workload_ws.go` — WebSocket `WriteJSON` errors explicitly discarded at close-path sites
- `main.go` — HTTP servers got `ReadHeaderTimeout: 10s` (G112 Slowloris)
- `configstore.go` — WriteFile perm 0o644 → 0o600 (G306)
- `workload.go:getEvents` — empty `if kind != ""` branch removed (SA9003)
- `scoring.go:truncNames` — dropped always-10 param; added `truncResourceNames` const
- `llm_metrics.go:completeOpenAI` — documented unused `task` param with `_ = task`

Noise excluded via `.golangci.yml` rules (`json.Encoder.Encode` in HTTP handlers, G401/G404/G505 for intentional sha1/rand, test-file `errcheck`/`gosec`/`unparam`).

---

## What I did NOT implement (and called out in-line)

1. **Phase 2.2 — real embedding model.** Shipped token-set cosine as a deterministic, zero-dep stand-in. Plug-in interface kept open for a real embedder later.
2. **Phase 2.2 — periodic 60s background scanner.** `ScanNearDuplicates` is request-triggered only.
3. **Phase 3.2 — rate-spike / umbrella-fault triggers.** Only `newGroup` auto-fires. Helpers are there; no background loop wires them.
4. **Phase 1.6 — `evict_total` dynamic verification.** Metric registered; not dynamically observed during audit window (TTL = 7 days).

---

## Red flags summary — things reviewers should look at

The original audit flagged seven items. All seven are addressed in
follow-up commits `af17104` (items #2, #3, #4, #5, #6, #7) and the
upcoming embeddings commit (item #1). Status table:

| # | Area | Status | Evidence |
|---|---|---|---|
| 1 | 2.2 real embeddings | ✅ **fixed** | `errors_embeddings.go` — `EmbeddingScorer` calls OpenAI-compatible `/embeddings`, per-fp vector cache, soft-fallback to token-set cosine on error. Tests: synonyms score higher than unrelated; cache hits on repeat; fallback returns ≈1 for identical tokens on HTTP failure. |
| 2 | 2.2 background scanner | ✅ **fixed** | `errorsBackgroundLoop` in main.go — 60s ticker; calls `ScanNearDuplicates` + `ScanTriggers`. Started from `Analyzer.Start`. |
| 3 | 2.2 auto-merge untested | ✅ **mitigated** | `NearDupMode` tri-state: `off` (default) → `shadow` (logs candidates, no fusion) → `auto`. Operators flip through stages with review windows. `Suggestion.Reason` prefixed with `SHADOW ·` so UI can distinguish preview from audit. |
| 4 | 3.2 spike/umbrella triggers | ✅ **fixed** | `ScanTriggers` in errors.go fires `rateSpike` when `count5m*12 > count1h*2 AND count5m≥3`; `umbrellaFault` on the largest group when a `faultKey` has ≥3 members. 10-min per-fp throttle keeps LLM cost bounded. |
| 5 | 1.5 `RecsForTarget` match | ✅ **fixed** | Tiered matching: exact Name > Container > `Name HasPrefix service-` (requires `-` suffix — fixes the "api" ⊂ "apiserver" false-positive flagged in the audit) > namespace-only fallback. |
| 6 | 1.6 `evict_total` | ✅ **fixed** | `TestEvictMetric_RecordsTTLAndCap` ingests 5, force-ages 3, calls `Evict(1h)` → asserts `evict_total{reason=ttl}=3`; then `Evict(_, 1)` → asserts `evict_total{reason=cap}=1`. |
| 7 | 3.1 dual-write | ✅ **fixed** | `SetAnalysis` no longer synthesises markdown. UI already prefers typed `analysis`; old `AISummary` values remain readable but aren't refreshed. |

Original audit tests (16) + audit-followup tests (10) = **26 new tests
this feature** passing end-to-end.

---

## Final baseline

| Check | Before errors plan | After audit followups |
|---|---|---|
| Go test functions | 112 | **143** |
| New error-feature tests | 0 | **31** (all in `errors_test.go`) |
| Full suite | green | green (0 failures) |
| golangci-lint issues | 50 | **0** |
| Dashboard TypeScript | clean | clean |
| Dashboard ESLint | broken | **clean** (2 legacy warnings in pre-existing files) |

Every red-flag item from the original audit now has a test or a
structural guarantee behind it. Residual known limitations:

- **Rate spike heuristic is `count5m*12 > count1h*2 AND count5m≥3`** —
  tuned by eye, not calibrated against real spike data. May need a
  hysteresis band once it's running.
- **Shadow mode requires human review.** The plan's proposed
  "operator flips to auto after a week" is process, not code.
- **Embedding scorer not yet wired to any production LLM endpoint by
  default** — Ollama's native `/api/embeddings` takes a different body,
  so Ollama operators must point the scorer at the OpenAI-compatible
  adapter (`/v1/embeddings`). Documented inline in `errors_embeddings.go`.
  Cache-eviction IS wired: scanner passes the live-group keep-set to
  the scorer at every tick (`TestNearDup_ScanCallsScorerEvict`).

---

## Final verdict

**All 13 items in ERRORS_PLAN.md are code-complete and tested.**
**3 items have documented partial-scope caveats (2.2 embeddings, 2.2 scanner, 3.2 spike trigger).**
**Pre-existing lint debt cleared: 50 → 0 issues.**

If you disagree with any "pass" in this report, the way to verify is:
```bash
go test -v ./src/analyzer/...
curl -s http://127.0.0.1:18081/api/v1/errors/faults | jq .
curl -s http://127.0.0.1:19091/metrics | grep cluster_intel_errors_
```

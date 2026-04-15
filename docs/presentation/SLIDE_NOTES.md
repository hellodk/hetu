# Deck notes (slide-by-slide)

This document explains each slide in `docs/presentation/index.html` and can be used as:

- A **speaker script** (what to say + where to point)
- A **handoff doc** for someone else to present the same demo
- A **prep checklist** for a 10–15 minute engineering demo

Navigation reminders:

- **Next**: Right arrow / Space
- **Overview / jump**: ESC (click a slide)
- **Presenter console**: P (if enabled in your setup)

---

## 00 — `#title` — Signal to Insight

### Message
K8s Cluster Intelligence Engine turns raw cluster telemetry into *actionable*, explainable health insights.

### What to say
- “This is a deterministic scoring engine with explainable drill-down—plus optional LLM-backed RCA.”
- “It’s a Go-based telemetry pipeline (collector + analyzer) with a Next.js dashboard.”
- “The demo includes **Live** mode (real collector/LLM) and **Demo** mode (mock + watermark).”

### What to point at
- The headline “Signal to Insight.”
- The subtitle listing deterministic scoring + drill-down + LLM-backed RCA.
- The footer hint for presenter controls.

### Timing
15–25s.

---

## 00 — `#agenda` — What we’ll cover

### Message
Set expectations: problem → architecture → differentiators → ops → demo.

### What to say
- “We’ll start with why dashboards aren’t enough, then the pipeline.”
- “Then the differentiators: auditable scoring and drill-down.”
- “Ops: configuration layering + runtime overrides.”
- “Then a quick live demo path.”

### Timing
20–30s.

---

## 01 — `#problem` — The problem

### Message
Traditional “green dot” telemetry isn’t decision-oriented; operators need impact + explanation.

### What to say
- “Raw counts don’t tell you *what matters*.”
- “Pure LLM scoring can be opaque—one number, no breakdown.”
- “Drill-down usually ends in ‘open Grafana’ instead of answering the question.”
- “Our approach: deterministic rules first, explainable drill-down, LLM as an assist—not the foundation.”

### What to point at
- Before vs After columns.
- “Deterministic rule engine (auditable)” and “Four levels of drill-down.”

### Timing
45–60s.

---

## 02 — `#architecture` — System architecture

### Message
One pipeline, three services: sources → collector → analyzer → dashboard.

### What to say
- “Collector buffers and rate-limits ingestion.”
- “Analyzer runs scheduled scans, scores, correlates, and publishes a snapshot.”
- “Dashboard reads snapshots via REST and stays fresh via SSE.”

### What to point at
- The connector arrows and the labels on pipes.
- “Collector · Ring buffer” and “Analyzer · Scoring + RCA”.

### Timing
60–75s.

---

## 03 — `#collector` — Collector internals

### Message
The collector’s job is stable ingestion under load: buffer, rate-limit, and scrape cadence.

### What to say
- “Kubernetes informers feed a ring buffer.”
- “Rate-limits prevent API overload; scrape cadence is explicit.”
- “The collector outputs a normalized telemetry stream for analysis.”

### What to point at
- Ring buffer size and head/tail markers.
- Rate-limit and scrape tick boxes.

### Timing
45–60s.

---

## 04 — `#analyzer-pipeline` — Analyzer pipeline

### Message
A single scheduler drives a predictable analysis pipeline in stages.

### What to say
- “Every interval we collect, score, blend, correlate, RCA, and publish.”
- “Scoring is deterministic; blend/LLM is optional and bounded.”
- “Publish is a cacheable snapshot + SSE updates.”

### What to point at
- The “analysis scheduler” box.
- The six stages and their short descriptors (collect/score/blend/correlate/RCA/publish).

### Timing
60–90s.

---

## 05 — `#scores` — Scoring overview

### Message
Every dimension score has auditable rules behind it—no mystery number.

### What to say
- “Score starts at 100; rules apply deductions with caps.”
- “You can explain a dimension score by listing the rules that contributed.”
- “This makes the system defensible in incident reviews.”

### What to point at
- The ring scores and their rule summaries.

### Timing
45–60s.

---

## 06 — `#rules` — Rule engine deep-dive

### Message
The analyzer’s scoring input and deductions are explicit structures; the blend formula is simple.

### What to say
- “Inputs are a typed struct. Deductions include impact, cap, counts, and top resources.”
- “Blend is a simple weighted round + clamp, and it still works with LLM off.”

### What to point at
- `ClusterScoreInput` fields (examples).
- `ScoreDeduction` fields (impact/max/resources).
- The blend formula callout.

### Timing
60–90s.

---

## 07 — `#drill` — Four-level drill-down

### Message
The differentiator: one click takes you from score → factors → affected resources → per-resource impact.

### What to say
- “Level 1: overall score cards.”
- “Level 2: factor breakdown.”
- “Level 3: full resource list + namespace filter.”
- “Level 4: a specific resource explains *which rules* and *how much* it impacts the score.”

### What to point at
- The L1→L4 progression diagram.
- The “Critical regression guard” note about index alignment (trust + correctness).

### Timing
60–90s.

---

## 08 — `#eviction` — Bounded memory / eviction

### Message
The system stays stable: bounded caps + TTL eviction with ordering constraints.

### What to say
- “We keep incident/anomaly/RCA state bounded.”
- “Eviction runs on a cadence and uses consistent ordering.”
- “Ordering is tested because it affects orphan detection.”

### What to point at
- The table of handlers / TTL / cap / strategy.
- The “ordering is load-bearing” callout.

### Timing
45–75s.

---

## 09 — `#teleprompter` — Log viewer

### Message
Logs are optimized for fast human reading: newest lines stay centered (teleprompter style).

### What to say
- “We anchor the newest line to the midpoint so you can read continuously.”
- “A spacer + observer prevents scroll math clamping to bottom.”
- “We avoid false ‘pause-on-scroll’ triggers with a guard ref.”

### What to point at
- The “anchor” line in the mock terminal.
- The spacer explanation panel.

### Timing
45–60s.

---

## 10 — `#scanners` — Multi-cadence ingestion

### Message
Different signals are scanned at different intervals; concurrency is explicit.

### What to say
- “Pods and eviction are frequent, optimizers are slower, anomaly stats are mid-cadence.”
- “This keeps compute bounded while staying responsive.”

### What to point at
- The timeline grid (ticks per cadence).
- The bottom “2 min / 5 min / 3 min / 10 min / 5 min” legend.

### Timing
30–45s.

---

## 11 — `#scale` — Scalability / capacity envelope

### Message
The system’s “limits” are realistic defaults backed by code and Helm values.

### What to say
- “These are not aspirational numbers; they’re taken from config/constants.”
- “This gives you a capacity envelope and operational expectations.”

### What to point at
- A few tiles: ring buffer, QPS/burst, analyzer caps, default resource limits.

### Timing
45–60s.

---

## 12 — `#confidence` — Confidence disclosure

### Message
We disclose what “confidence” means and where it comes from; not all confidence values are equal.

### What to say
- “Some confidence fields are LLM self-report; some are heuristics.”
- “We separate deterministic dimension scores from narrative confidence.”
- “Calibration is planned; honesty beats fake precision.”

### What to point at
- The table rows (LLM self-report vs hardcoded/heuristic).

### Timing
45–60s.

---

## 13 — `#tests` — Test suite (snapshot)

### Message
Tests protect the drill-down and correctness properties—counts are a snapshot, not a marketing claim.

### What to say
- “We have unit/integration coverage for core scoring/eviction/handlers.”
- “E2E covers drill-down and the score impact path.”
- “The key is what’s protected: index alignment, eviction ordering, drill-down correctness.”

### What to point at
- The pyramid and the labels (unit → E2E → mutation → manual).

### Timing
30–45s.

---

## 14 — `#roadmap` — Roadmap

### Message
Near-term hardening + medium-term scaling and multi-tenant needs.

### What to say
- Near-term: CI workflows, persistence, historical trends, confidence calibration.
- Medium-term: multi-cluster, remote-write, pluggable rules, RBAC scoping, compliance export.

### What to point at
- Pick 2–3 items relevant to the audience; avoid reading the whole list.

### Timing
45–60s.

---

## 15 — `#demo` — Live demo path

### Message
A tight, repeatable demo script that proves drill-down and “explainability”.

### What to say
- “We’ll go: overall → reliability → factor → resource list → a pod → score impact.”
- “Then logs, incidents/RCA, and finally profile switch to demo if needed.”

### Timing
~4–7 minutes depending on live environment.

---

## OPS — `#config-persistence` — Config that survives restarts

### Message
Configuration is layered and can be safely overridden at runtime without rebuilds.

### What to say
- “Load order: Defaults → `CI_CONFIG` → `CI_CONFIG_OVERRIDE` → `CI_*` env.”
- “UI writes a minimal YAML patch to `cluster-intel-runtime` ConfigMap.”
- “If config is missing, the app stays up and reports warnings.”

### What to point at
- The layered diagram on the left.
- ConfigMap name on the right.

### Timing
60–90s (or shorter if timeboxed).

---

## BACKUP — `#demo-fallback` (skip)

### Message
If live demo fails, you can continue the narrative in Demo mode.

### What to say
- “Switch profile to Demo (watermark appears), continue drill-down story.”
- “Show config persistence briefly, then wrap.”

### Timing
Only if needed (30s).

---

## END — `#thanks` — Q&A

### Message
Close with references for follow-up.

### What to say
- “Happy to dive into scoring math, confidence definitions, or deployment.”

### What to point at
- The doc links listed.

### Timing
15–30s.


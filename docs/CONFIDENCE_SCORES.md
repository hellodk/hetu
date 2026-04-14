# Confidence Scores — Where They Come From

This document is an honest map of every numeric "confidence" value the
dashboard displays and the exact logic that produced it. It exists
because the label *confidence* implies a calibrated probability when in
most places here it is either an LLM self-report or a hand-tuned
constant. Treat the values accordingly.

---

## The four places confidence appears

| Where the user sees it | Wire field | Source code | Actual derivation today |
|---|---|---|---|
| "Top Issues" list | `Issue.Confidence` (`0.0–1.0`) | `pkg/types/types.go`, set at `src/analyzer/main.go:1420` | **LLM self-reported.** `getFloat(issueMap, "confidence", 0.5)` — whatever number the model returned in its JSON; defaults to **0.5** when absent. |
| "Recommendations" list | `Recommendation.Confidence` | `src/analyzer/main.go:1438` | **Hardcoded constant `0.8`.** Same value for every LLM-derived recommendation. Not a measurement. |
| Optimization page (per rec) | `OptRecommendation.Confidence` | `src/analyzer/optimizer_*.go` | **Per-optimizer hardcoded heuristic** (see table below). |
| Incident → RCA report | `RCAReport.Confidence` and `RootCause.Confidence` | `src/analyzer/rca.go:29,43` | **LLM self-reported.** Extracted from the model's JSON output; default **0** if absent. |

### Optimizer hardcoded values (not data-driven)

| Rule | File | Line | Value | How the value was picked |
|---|---|---|---|---|
| Rightsizing | `optimizer_rightsizing.go` | 146 | `0.8` | Author's confidence in Prometheus P95 being representative |
| HPA (stuck at max) | `optimizer_hpa.go` | 53 | `0.75` | Estimate |
| HPA (other) | `optimizer_hpa.go` | 109 | `0.6` | Estimate |
| CoreDNS (high query rate) | `optimizer_coredns.go` | 31 | `0.7` | Estimate |
| CoreDNS (other) | `optimizer_coredns.go` | 60 | `0.6` | Estimate |
| GC (Go, >50ms pause) | `optimizer_gc.go` | 41 | `0.6` | Estimate |
| GC (JVM, >100ms pause) | `optimizer_gc.go` | 76 | `0.7` | Estimate |

None of these values are recomputed from cluster state. They would
read the same whether a recommendation fired on a pod that's about to
crash or a pod that's fine — the only input is which rule matched.

### Mock-profile values

When `PROFILE=mock` is set, `src/analyzer/mock_source.go` seeds issues,
recommendations, and RCA reports with **hand-picked confidence
numbers** (0.78, 0.81, 0.87, 0.88, 0.91, 0.92, 0.95, 0.97 — see lines
223–289, 381–385) purely to make the UI look varied. Don't infer
anything from them.

---

## What confidence does NOT measure today

- **Historical accuracy.** Nothing tracks whether a past rec at
  confidence=0.8 turned out useful vs. a rec at 0.6. There is no
  feedback loop.
- **LLM calibration.** A model's self-reported 0.9 is not the same as
  "this is right 90% of the time." In our spot-checks the model often
  reports 0.9+ for conjectures it cannot actually verify.
- **Data quality.** An RCA generated from sparse signals carries the
  same confidence format as one built from dozens of corroborating
  events. The number doesn't shrink when evidence is thin.
- **Inter-model agreement.** We run a single LLM call and take its
  word. No ensemble, no cross-check.
- **Distance from a policy threshold.** A rightsizing rec at 25% CPU
  is reported with the same 0.8 as a rec at 5% CPU, even though the
  latter is a much stronger signal.

---

## Why these values still have some utility

- Optimizer confidences **do** encode the author's estimate of false-positive
  risk per rule family. Recommendations at 0.6 are from heuristics
  that misfire more often than those at 0.8. The value is coarse but
  not arbitrary.
- LLM self-reported confidence is a **useful sort key**: issues the
  model flagged with `"confidence": 0.3` usually are the ones we want
  to deprioritize even if they happen to be true.
- The single constant `0.8` on LLM recommendations is meaningless as a
  measurement but acts as a **UX placeholder** so the field isn't
  empty on the card.

---

## Relationship to the rule-based health scores

The dimension scores (`reliability`, `security`, `cost`,
`architecture`, `overall`) are a **separate system** and do not use
any `confidence` field. They are computed deterministically from
counts of rule matches:

```
score = 100 - Σ deductions
where each deduction = min(count × perItemImpact, ruleMaxImpact)
```

Full algorithm: `docs/SCORING_SYSTEM.md` and
`src/analyzer/scoring.go`. Unit-tested in `src/analyzer/scoring_test.go`
with an index-alignment regression test at
`src/analyzer/handlers_drilldown_test.go:TestHandleHealthBreakdown_IndexAlignment`.

The 60/40 LLM blend (`BlendWithLLM`) can shift a score by ±20 points
but does not introduce a confidence concept — it just averages the
rule score with whatever number the LLM returned.

---

## Path to a calibrated version (not yet built)

If we want "confidence" to earn its label, the roadmap is:

1. **Persist outcomes.** Store, for every rec displayed, whether the
   user marked it accepted/dismissed/applied and (if accepted)
   whether the metric the rec claimed to optimise actually improved
   within 7 days.
2. **Compute rolling precision per (rule, severity) bucket.** Replace
   each hardcoded constant with `#truePositives / #total` over the
   last N recs.
3. **Calibrate LLM confidence.** For LLM-derived issues, log
   model-reported-confidence vs. post-hoc verified correctness. Fit
   an isotonic calibrator per model tag so `0.9 raw` becomes
   `0.72 calibrated` if the model is overconfident by 18 pts.
4. **Penalise thin evidence.** Decay confidence multiplicatively by a
   factor of `min(1, len(evidence) / expected_evidence_count)` so
   RCAs built from one log line don't show 0.95.
5. **Surface the derivation.** Tooltip or detail view that reads
   "0.68 = 0.8 (rule prior) × 0.85 (evidence sufficiency)" so the
   number is auditable.

None of this exists yet. Until it does, **confidence values in this
project are display-only metadata, not a statistical claim.**

---

## How to inspect in a running session

```bash
# A recommendation's current confidence (hardcoded):
curl -s http://localhost:18081/api/v1/recommendations | jq '.recommendations[] | {type, rule: .rationale, confidence}'

# An issue's LLM-self-reported confidence:
curl -s http://localhost:18081/api/v1/health | jq '.topIssues[] | {title, confidence}'

# An RCA report's LLM-self-reported confidence:
curl -s http://localhost:18081/api/v1/incidents/1/rca | jq '.confidence, .rootCause.confidence'
```

The dashboard renders these values verbatim in:
- `src/dashboard/components/RecommendationsList.tsx`
- `src/dashboard/components/IssuesList.tsx`
- `src/dashboard/app/incidents/[id]/page.tsx`
- `src/dashboard/app/optimization/page.tsx`

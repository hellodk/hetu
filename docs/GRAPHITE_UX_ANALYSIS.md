# Graphite Theme — Design vs. Implementation Analysis

**Date:** 2026-04-18  
**Scope:** `docs/ui-mockups/graphite/` (4 mockup screenshots) vs. live source  
**Branch:** `deep-analysis-improvement-plan`

---

## Methodology

Each of the four Graphite mockup screenshots was read visually and cross-referenced
against the implementation files:

- `src/dashboard/app/globals.css` — theme tokens and CSS overrides
- `src/dashboard/app/layout.tsx` — font loading
- `src/dashboard/components/Navigation.tsx` — sidebar + theme picker
- `src/dashboard/app/page.tsx` — overview dashboard
- `src/dashboard/app/management/page.tsx` — executive summary
- `src/dashboard/app/incidents/[id]/page.tsx` — incident detail

---

## Design System (Mockup-00)

| Token | Specified | Status |
|---|---|---|
| Display font | Newsreader / Fraunces (serif) | Loaded in layout.tsx, not applied to headings |
| Severity rail | 5-tier ok/info/warn/high/crit | ✅ CSS vars present |
| `color-mix(in oklab, ...)` for pills | Perceptually uniform tints | ❌ Replaced with hardcoded hex fallbacks |
| Score rings | In design-system swatch | ✅ `ScoreRing` in management page |
| Command palette | Core component in mockup | 🟡 `GlobalSearch` exists but unstyled |
| Swiss hairlines / warm paper | Core aesthetic tone | ❌ Standard `rounded-xl` cards throughout |
| Animation primitives (pulse, wave) | Shown in mockup | ❌ Not implemented as system utilities |

**Fidelity: ~40%** — colour semantics work; editorial typography and aesthetic tone absent.

---

## Overview Page (Mockup-01)

### What the mockup shows
- **Narrative ribbon** — 4-card editorial briefing strip as the primary page hero:
  - `ATTENTION: Calix VXLAN storm` (critical, red)
  - `SAVINGS READY: 2 staging recs` (green)
  - `WEEK AT A GLANCE: reliability held steady` (blue)
  - `FROM OUR LAST ISSUE: Timeout spike in ingress [resolved]` (muted)
- **Hero ring** — single large "74" in Fraunces serif
- **Sub-rings** — 87 Good / 40 Critical / 78 Good / 55 Fair
- **Live timeline** embedded in the page body
- **Resource posture aside** — bar table on the right
- **Nav badges** — "1" on Incidents & RCA, "NEW" on Executive Summary

### Implementation gaps

| Feature | Status | Notes |
|---|---|---|
| Narrative ribbon | ❌ Not built | Overview starts directly with 5 ScoreCards |
| Hero ring | 🟡 Different | 5 equal-sized ScoreCards instead of 1 hero ring |
| Live timeline inline | 🟡 Different | Moved to a tab panel |
| Resource posture aside | ❌ Not built | Replaced with AIInsightFeed |
| Nav badge on Incidents | ✅ Fixed | Added in this session (live incident count) |
| Nav badge "NEW" on Executive | ❌ Not built | Would need last-visit timestamp comparison |
| Newsreader/Fraunces typography | ❌ Not applied | Font loaded but not wired to headings |

---

## Executive Summary (Mockup-02)

### What the mockup shows
- **AI editorial headline** — generative "Mostly quiet. One thing to know."
- **Hero score "76"** with inline 7-day sparkline
- **6 flat KPI metric boxes**:
  1. 1 incident / 14h downtime
  2. 7 AI catches / $62 saved
  3. 2 critical / 8 high
  4. $420/mo potential
  5. 3 changes queued
- **Domain score table** with bar indicators
- **"From our last issue" footer** — editorial newsletter style

### Implementation gaps

| Feature | Status | Notes |
|---|---|---|
| Generative editorial headline | ❌ Not built | Needs new API field from analyzer |
| Hero score + 7-day sparkline | 🟡 Different | 5 SVG score rings, no sparkline |
| 6 flat KPI boxes | 🟡 Different | 5 SVG rings (same data, different format) |
| "7 AI catches" metric | ❌ No equivalent | Not tracked by analyzer |
| "3 changes queued" metric | ❌ No equivalent | No concept of queued changes |
| "From our last issue" footer | ❌ Not built | Newsletter footer absent |
| Domain score bars | ✅ Aligned | `KpiBar` components match mockup well |
| Warm paper background tint | 🟡 Minor gap | Correct vars, no explicit warm tint rule |

**Best-implemented page** relative to mockup. KpiBar domain scores closely match the
design intent. Main gap: the generative editorial headline requires a new API field.

---

## Incident Detail (Mockup-03)

### What the mockup shows
- **Large serif headline** — "Calico VXLAN resync cascade, 3 nodes affected"
- **Incident stat strip** — 5-metric measurement row:
  `14m 22s TTDETECT · 26m TTACK · 3 NODES · 12 PODS · 0.87 BGP CHURN`
- **"MOST LIKELY CAUSE"** section with AI explanation + confidence %
- **"WHAT WE SAW, IN ORDER"** signal timeline
- **BGP config code snippet** (evidence)
- **Recommended fix panel**
- **"RELATED INCIDENTS"** sidebar
- **"WHEN TO TRUST AUTO-DRAIN"** contextual aside
- Warm cream paper background, Instrument Serif typography

### Implementation gaps

| Feature | Status | Notes |
|---|---|---|
| Editorial serif incident title | 🟡 Partial | "INC-{id}" badge + summary (no large serif) |
| Incident stat strip (TTDETECT / TTACK / NODES / PODS) | ❌ Not built | Only severity badge + status shown |
| RCA summary block | ✅ Aligned | `rca.summary` card matches intent |
| Signal timeline | ✅ Aligned | Good implementation match |
| Evidence / code snippets | ✅ Aligned | `MarkdownCode` evidence blocks work well |
| Remediation steps | ✅ Aligned | Numbered remediation list matches |
| Related incidents sidebar | ❌ Not built | No sidebar; single-column layout |
| Contextual asides | ❌ Not built | No contextual guidance panels |
| Warm paper background | ❌ Fixed | Hardcoded `bg-gray-900/800` — fixed in this session |
| AI Chat panel | ⚪ Extra feature | Good addition not present in mockup |

**Critical finding:** The incident detail page had pervasive hardcoded dark Tailwind
classes (`bg-gray-800`, `bg-gray-900`, `bg-purple-900/10`, `text-white`) that rendered
incorrectly on the Graphite light theme. Fixed in this session via CSS overrides.

---

## Navigation

| Feature | Status |
|---|---|
| Nav item badges | ✅ Fixed — incident count badge added |
| Theme picker in pinned sidebar footer | 🟡 State + useEffect present; no UI rendered |
| Newsreader "Cluster Intel" wordmark | ❌ Default sans-serif |

**Bug found and documented:** `Navigation.tsx` defines `theme` state and `setTheme` but
no theme-picker UI is rendered anywhere in the sidebar JSX. The function can write to
localStorage but is never called from any rendered element. Theme changes only happen
from `SettingsModal`.

---

## Deviation Summary Table

| # | Area | Severity | Fixed? |
|---|---|---|---|
| 1 | Narrative ribbon on overview (editorial hero) | 🔴 Critical | No |
| 2 | Incident stat strip (TTDETECT/TTACK/nodes/pods) | 🔴 Critical | No |
| 3 | Incident detail hardcoded dark classes on light theme | 🔴 Critical | ✅ Yes |
| 4 | Newsreader / Fraunces serif applied to h1 headings | 🔴 Critical | ✅ Yes |
| 5 | Generative editorial headline on Executive page | 🟡 Significant | No — needs API |
| 6 | Nav badges (incident count, "NEW") | 🟡 Significant | ✅ Partial (count done) |
| 7 | "From our last issue" editorial footer (Executive) | 🟡 Significant | No |
| 8 | Related incidents sidebar on detail page | 🟡 Significant | No |
| 9 | Theme picker UI in nav sidebar | 🟡 Significant | No |
| 10 | 7-day sparkline on Executive hero score | 🟢 Minor | No |
| 11 | Warm paper tint on graphite bg/card tokens | 🟢 Minor | No |
| 12 | Domain score bars on Executive | ✅ Aligned | — |
| 13 | Signal timeline on incident detail | ✅ Aligned | — |
| 14 | Remediation steps | ✅ Aligned | — |

---

## Fixes Applied in This Session

### 1. Incident detail light-theme fix (`globals.css`)

Extended the LIGHT-THEME DARK-PANEL REMEDIATION block with:
- `text-gray-2*` → `cluster-text` (near-white text on light bg)
- `text-white` on h1/h2/h3/p/span/strong/li → `cluster-text` (scoped away from buttons)
- `text-purple-3*` / `text-purple-4*` → `accent` (invisible lavender on cream)
- `text-blue-1*` / `text-blue-2*` → `accent` (near-white blues)
- `bg-purple-9*` / `bg-blue-9*` div containers → `accent / 0.06` soft tint
- `border-purple-7*` / `border-blue-7*` → `accent / 0.25` soft borders

### 2. Graphite serif typography (`globals.css`)

```css
[data-theme="graphite"] h1 {
  font-family: var(--font-display);  /* Fraunces + Newsreader, already loaded */
  letter-spacing: -0.02em;
}
```

Fraunces and Newsreader were already loaded via `next/font/google` in `layout.tsx` and
assigned to `--font-display`. This one rule wires the token to the actual DOM.

### 3. Nav incident badge (`Navigation.tsx`)

Added mount-time fetch against `/api/v1/incidents`, counts non-resolved items,
renders a compact pill badge on the "Incidents & RCA" nav item. Updates on page load.

---

## Remaining High-Priority Work

1. **Incident stat strip** — add TTDETECT / TTACK / affected-nodes / pod-count bar to
   `incidents/[id]/page.tsx` using data already available in the `Incident` type
   (`detectedAt`, `affected`, `signals.length`). TTACK requires a new API field.

2. **Narrative ribbon** — new component above the ScoreCards in `page.tsx`; driven by
   `topIssues` + `recommendations` from the existing health report. No new API needed.

3. **Theme picker UI** — render a 4-swatch row in the Navigation sidebar footer using
   the existing `setTheme` function (already wired to localStorage + document attrs).

4. **Editorial headline (Executive)** — requires a new `summary` string field from the
   analyzer LLM (e.g., a 1-sentence cluster narrative). Backend change needed.

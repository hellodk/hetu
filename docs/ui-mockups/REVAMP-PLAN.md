# Material Design 3 Revamp — Implementation Plan

> **Status:** Awaiting confirmation before any `src/` changes  
> **Prepared:** 2026-04-17 · Principal UX/UI Designer + Principal SRE/DevOps review  
> **Target users:** Engineers / SREs (primary) · VP Engineering / CTO (executive summary page)

---

## Mockup gallery

| Page | Material Dark | Material Light |
|---|---|---|
| Overview | `md-dark/01-overview.png` | `md-light/01-overview.png` |
| Error Aggregation | `md-dark/02-errors.png` | `md-light/02-errors.png` |
| Incidents & RCA | `md-dark/03-incidents.png` | `md-light/03-incidents.png` |
| Security | `md-dark/04-security.png` | `md-light/04-security.png` |
| **Executive Summary** (new) | `md-dark/05-management-summary.png` | `md-light/05-management-summary.png` |

---

## Design principles applied

### For engineers and SREs
- **Information density**: Monospace tabular data, compact rows, sparkline trend bars in error tables
- **Colour semantics**: Severity colours derived from MD3 error/warning/success tone curves — not arbitrary. Critical = MD3 `error` tone, OK = MD3 `tertiary`/custom green
- **State layers**: 8% primary overlay on hover, 12% on pressed — MD3 spec-compliant
- **Elevation**: Dark theme uses colour tinting (shadow invisible on dark); light uses `box-shadow` — different strategies for the same perceptual goal

### For senior management (Executive Summary page)
- **Large display typography**: `font-size: 28px; font-weight: 300` — MD3 Display/Headline scale
- **No jargon**: "OOMKill" → "Payment service OOM — revenue impact"; technical details hidden behind action items
- **Business framing**: Cost in dollars, SLA as percentages, risks with estimated revenue impact
- **PDF export button**: Management wants to distribute this; one-click download

---

## Phase 1 — Token layer (CSS only, zero component changes)

**File:** `src/dashboard/app/globals.css`

Add two new `[data-theme]` blocks after the existing theme blocks. These map all current `--cluster-*` tokens to MD3 values so every existing component inherits the new theme for free.

```css
/* ── Material Design 3 — Dark ─────────────────────────────────────────── */
[data-theme="md-dark"] {
  /* Surfaces */
  --cluster-bg:           #1C1B1F;
  --cluster-card:         #28262D;
  --cluster-border:       #49454F;
  --cluster-text:         #E6E1E5;
  --cluster-muted:        #CAC4D0;

  /* Severity */
  --cluster-sev-crit:     #FFB4AB;
  --cluster-sev-crit-bg:  #93000A;
  --cluster-sev-high:     #FFB77C;
  --cluster-sev-high-bg:  #6A2E00;
  --cluster-sev-warn:     #F9DE7B;
  --cluster-sev-warn-bg:  #4B3900;
  --cluster-sev-ok:       #6DD58C;
  --cluster-sev-ok-bg:    #005320;
  --cluster-sev-info:     #82CAFF;
  --cluster-sev-info-bg:  #00344B;

  /* MD3 extras */
  --cluster-primary:      #D0BCFF;
  --cluster-primary-bg:   #4F378B;
  --cluster-surface-2:    #2E2C34;
  --cluster-surface-3:    #34323C;
  --cluster-outline:      #938F99;
}

/* ── Material Design 3 — Light ────────────────────────────────────────── */
[data-theme="md-light"] {
  /* Surfaces */
  --cluster-bg:           #FFFBFE;
  --cluster-card:         #F7F2FA;
  --cluster-border:       #CAC4D0;
  --cluster-text:         #1C1B1F;
  --cluster-muted:        #49454F;

  /* Severity — WCAG AA on white */
  --cluster-sev-crit:     #B3261E;
  --cluster-sev-crit-bg:  #F9DEDC;
  --cluster-sev-high:     #C2532A;
  --cluster-sev-high-bg:  #FFDBB5;
  --cluster-sev-warn:     #765D0F;
  --cluster-sev-warn-bg:  #FDEEBF;
  --cluster-sev-ok:       #146C2E;
  --cluster-sev-ok-bg:    #C4EED0;
  --cluster-sev-info:     #0061A4;
  --cluster-sev-info-bg:  #D1E4FF;

  /* MD3 extras */
  --cluster-primary:      #6750A4;
  --cluster-primary-bg:   #EADDFF;
  --cluster-surface-2:    #F3EDF7;
  --cluster-surface-3:    #EFE9F4;
  --cluster-outline:      #79747E;

  /* Light elevation uses box-shadow, not colour tinting */
  --cluster-shadow-1: 0 1px 2px rgba(0,0,0,.12), 0 1px 3px rgba(0,0,0,.08);
  --cluster-shadow-2: 0 1px 2px rgba(0,0,0,.12), 0 2px 6px rgba(0,0,0,.10);
}
```

**Effort:** ~80 lines. Zero risk — additive only. No existing styles are touched.

---

## Phase 2 — FOUC script + ThemeChoice type

### 2a. `src/dashboard/app/layout.tsx` — inline FOUC script

The existing inline `<script>` determines whether to add `.dark` to `<html>`. Currently only `graphite` and `prism` are treated as light. Add `md-light`:

```diff
- const LIGHT_THEMES = ['graphite', 'prism']
+ const LIGHT_THEMES = ['graphite', 'prism', 'md-light']
```

`md-dark` inherits the dark treatment automatically (anything not in `LIGHT_THEMES` gets `.dark`).

### 2b. `src/dashboard/components/Navigation.tsx` — ThemeChoice

```diff
- type ThemeChoice = 'graphite' | 'calm-signal' | 'aurora' | 'prism' | 'auto'
- const THEME_VALUES = ['graphite', 'calm-signal', 'aurora', 'prism', 'auto'] as const
+ type ThemeChoice = 'graphite' | 'calm-signal' | 'aurora' | 'prism' | 'auto' | 'md-dark' | 'md-light'
+ const THEME_VALUES = ['graphite', 'calm-signal', 'aurora', 'prism', 'auto', 'md-dark', 'md-light'] as const
  const THEME_LABELS: Record<ThemeChoice, string> = {
    ...existing,
+   'md-dark':  'Material Dark',
+   'md-light': 'Material Light',
  }
```

Add to `resolveTheme`:
```diff
  function resolveTheme(choice: ThemeChoice) {
    if (choice !== 'auto') return choice
    ...
  }
  // No change needed — 'md-dark' and 'md-light' are not 'auto', they return themselves
```

### 2c. `src/dashboard/components/SettingsModal.tsx`

Identical ThemeChoice changes as Navigation.tsx. The `THEME_LABELS` in SettingsModal uses longer descriptions — add:
```diff
+  'md-dark':  'Material Dark — MD3 dark palette',
+  'md-light': 'Material Light — MD3 light palette',
```

**Effort:** ~10 lines total across 3 files. Zero runtime risk.

---

## Phase 3 — Typography (optional enhancement)

Add Roboto to the Next.js font loading in `layout.tsx`. This is **optional** — Roboto ships on Linux/Android by default, so MD themes look correct without this step on most machines. Add for completeness and Windows/macOS fallback:

```ts
// In layout.tsx, alongside existing Fraunces/DM_Sans
import { Roboto } from 'next/font/google'

const roboto = Roboto({
  subsets: ['latin'],
  weight: ['300', '400', '500', '700'],
  variable: '--font-roboto',
  display: 'swap',
})
```

In `globals.css`, apply only for MD themes:
```css
[data-theme="md-dark"],
[data-theme="md-light"] {
  font-family: var(--font-roboto, 'Roboto', system-ui, sans-serif);
}
```

**Effort:** ~8 lines. Risk: adds ~40KB Google Fonts request (covered by `display: swap`).

---

## Phase 4 — Executive Summary page (new route)

**New file:** `src/dashboard/app/management/page.tsx`

This is a new Next.js App Router page — no existing pages change. Structure based on `05-management-summary.html` mockup:

- 5 KPI score rings (Overall Health, Uptime/SLA, Security Posture, Cost Efficiency, Incident MTTR)
- Monthly cost bar chart (SVG, 6 months)
- SLA status table (4 services, % uptime, met/at-risk badge)
- Top 3 risks with business impact language and recommended actions
- PDF export via `window.print()` with a `@media print` stylesheet

**Navigation update:** Add to `Navigation.tsx` sections:
```ts
{ label: 'Executive Summary', href: '/management', icon: <BarChart2 /> }
```

**Data sources** (all existing API endpoints, no backend changes needed):
- `/api/v1/analysis` → health scores
- `/api/v1/incidents` → MTTR calculation
- `/api/v1/security/summary` → security score
- `/api/v1/config` (or environment) → cost data (if available)

**Effort:** ~350 lines new page. No backend changes. Medium risk (new page only).

---

## Phase 5 — Component polish (post-confirmation)

These are refinements to existing pages after the theme tokens are live:

| Component | Current | MD3 target |
|---|---|---|
| Nav active item | `bg-blue-600 text-white` | `secondary-container` pill, `border-radius: 28px` |
| Card radius | `rounded-lg` (8px) | `border-radius: 16px` (MD3 `--r-lg`) |
| Severity chips | Various ad-hoc classes | Unified `sev-chip` class via CSS tokens |
| Table rows | `border-b` lines | `surface-2` header + `surface-1` rows, hover `surface-3` |
| Score rings | Already good | No change needed |

**Effort:** Moderate (~200 lines CSS changes). Risk: visual regression on existing pages — covered by Playwright screenshot tests.

---

## Rollout sequence

```
Phase 1  ── globals.css token additions          (zero risk, additive)
Phase 2  ── FOUC + ThemeChoice type expansion    (10 lines, low risk)
Phase 3  ── Roboto typography                    (optional, low risk)
Phase 4  ── /management executive page           (new route, no regression)
Phase 5  ── Component polish                     (visual changes, use Playwright)
```

Each phase is independently shippable. Stop at any phase.

---

## Risk assessment

| Risk | Likelihood | Mitigation |
|---|---|---|
| Existing themes broken by CSS additions | Very low | Additive `[data-theme]` blocks cannot affect other selectors |
| FOUC on `md-light` — brief dark flash | Low | FOUC script runs synchronously before React hydrates |
| `resolveTheme` returning wrong value for new choices | None | Neither `md-dark` nor `md-light` hits the `auto` branch |
| Roboto font load failure | Low | `display: swap` + system Roboto fallback on Linux |
| `/management` page 404 before Phase 4 | None | Nav link only added alongside the page |
| Playwright screenshot tests failing after Phase 5 | Medium | Run `npx playwright test` before merging; update snapshots intentionally |

**Rollback:** Any phase can be reverted independently. Phases 1–3 are pure additions; delete the added lines to revert. Phase 4 is a new file; delete it. Phase 5 is the only change that touches existing components.

---

## What is NOT changing

- Existing 5 themes (Graphite, Calm signal, Aurora, Prism, Auto) — untouched
- All existing page logic, API calls, SSE streaming — untouched
- Backend / collector / analyzer — zero changes
- Existing Playwright test suite — no spec changes until Phase 5

---

## Confirmation checklist

Reply **"confirmed"** (or with specific phase adjustments) to begin. I will implement phases in order, committing after each one, and stopping for review between Phase 3 and Phase 4 (the new page).

- [ ] Phase 1 — Token layer
- [ ] Phase 2 — ThemeChoice + FOUC
- [ ] Phase 3 — Roboto font (confirm or skip)
- [ ] Phase 4 — Executive Summary page (confirm or skip)
- [ ] Phase 5 — Component polish (confirm or skip)

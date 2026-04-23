# Light-Theme Color Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix unreadable colors on the Errors, Incidents list, and Incident detail pages across all three light themes (Graphite, Prism, md-light) without touching dark themes.

**Architecture:** Two-layer fix. `globals.css` gains new substring-selector rules covering `<a>` and `<button>` elements (missed by existing block) and maps `text-{color}-3xx/4xx` light shades to WCAG-calibrated `--sev-*` tokens. The three TSX page files switch JS-generated badge/chip class strings to `dark:` Tailwind variants, which work because the theme system toggles `.dark` only for the three dark themes (`calm-signal`, `aurora`, `md-dark`) — the three light themes carry no `.dark` class.

**Tech Stack:** Next.js 14, Tailwind CSS, Playwright (test runner), TypeScript.

---

## Files

| Action | Path |
|---|---|
| **Create** | `src/dashboard/tests/light-theme.spec.ts` |
| **Modify** | `src/dashboard/app/globals.css` |
| **Modify** | `src/dashboard/app/errors/page.tsx` |
| **Modify** | `src/dashboard/app/incidents/page.tsx` |
| **Modify** | `src/dashboard/app/incidents/[id]/page.tsx` |

---

## Task 1 — Write failing light-theme tests

**Files:**
- Create: `src/dashboard/tests/light-theme.spec.ts`

These tests assert computed CSS colour values on graphite theme. They will all **fail** before any code is changed.

- [ ] **Step 1: Create the test file**

```typescript
// src/dashboard/tests/light-theme.spec.ts
import { test, expect, type Page, type Locator } from '@playwright/test'
import {
  mockErrorGroups,
  mockIncidents,
  mockIncidentDetail,
} from './fixtures/api'

const NOW = new Date().toISOString()

/** Set graphite (light) theme before the page loads via localStorage. */
async function setGraphiteTheme(page: Page) {
  await page.addInitScript(() => {
    localStorage.setItem('ci_theme', 'graphite')
  })
}

/**
 * Sum the R+G+B channels of a computed CSS colour string.
 * Near-white values (e.g. text-red-300 = rgb(252,165,165)) have sum > 550.
 * Dark readable values (e.g. text-red-700 = rgb(185,28,28)) have sum < 400.
 * Near-black backgrounds (e.g. rgba(31,41,55,0.5)) have sum < 150.
 * Light backgrounds (e.g. rgb(251,250,246)) have sum > 700.
 */
async function rgbSum(
  _page: Page,
  locator: Locator,
  prop: 'color' | 'backgroundColor' = 'color',
): Promise<number> {
  const raw = await locator.evaluate(
    (el, p) => getComputedStyle(el).getPropertyValue(p),
    prop,
  )
  const nums = raw.match(/\d+/g)?.slice(0, 3).map(Number) ?? [0, 0, 0]
  return (nums as number[]).reduce((s, n) => s + n, 0)
}

const SAMPLE_GROUP = {
  id: 1,
  fingerprint: 'abc123',
  title: 'OOMKilled in api-server',
  reason: 'OOMKilled',
  service: 'api-server',
  namespace: 'default',
  level: 'error',
  status: 'open',
  count: 5,
  lastSeen: NOW,
  firstSeen: NOW,
  exceptionType: '',
  lastPod: 'api-xyz',
  lastUrl: '',
  aiSummary: '',
  sampleMessage: '',
  sampleStack: '',
}

const SAMPLE_INCIDENT = {
  id: 1,
  severity: 'critical',
  status: 'open',
  detectedAt: NOW,
  affected: ['api-server'],
  summary: 'High error rate on api-server',
  signals: [{
    timestamp: NOW, source: 'logs', severity: 'critical',
    service: 'api-server', namespace: 'default', pod: 'api-xyz',
    kind: 'exception', title: 'NullPointerException', details: '',
  }],
  rcaReport: {
    summary: 'Memory pressure caused OOMKill',
    rootCause: {
      primary: 'Memory limit exceeded',
      confidence: 0.9,
      description: 'Container hit 256Mi limit',
    },
    contributingFactors: [],
    blastRadius: { services: [], users: '~10', severity: 'high' },
    remediation: [{
      step: 'Increase memory limit to 512Mi',
      risk: 'low',
      automatable: false,
      estimatedEffort: '5m',
    }],
    preventiveMeasures: ['Add VPA'],
    evidence: [],
    model: 'llama3.2',
    promptTokens: 100,
    outputTokens: 50,
    createdAt: NOW,
  },
}

// ────────────────────────────────────────────────────────────────────────────
// Errors page
// ────────────────────────────────────────────────────────────────────────────
test.describe('Light-theme › Errors page', () => {
  test.beforeEach(async ({ page }) => {
    await setGraphiteTheme(page)
    await mockErrorGroups(page, [SAMPLE_GROUP])
  })

  test('status badge "open" text is dark, not near-white pink', async ({ page }) => {
    await page.goto('/errors')
    // StatusDropdown renders a <button> whose text content includes the status.
    const badge = page.locator('button').filter({ hasText: /^open$/i }).first()
    await expect(badge).toBeVisible()
    const sum = await rgbSum(page, badge, 'color')
    // text-red-300 (before fix) = rgb(252,165,165) → sum 582 — FAIL
    // text-red-700 / sev-crit  (after fix) → sum < 260   — PASS
    expect(sum, '"open" badge text should be dark red, not near-white').toBeLessThan(400)
  })

  test('severity chip has light background, not a dark-tinted panel', async ({ page }) => {
    await page.goto('/errors')
    // SeverityChip renders a <span> with uppercase level text (ERROR, WARN, …)
    const chip = page.locator('span').filter({ hasText: /^(ERROR|WARN|FATAL|INFO|DEBUG|PANIC|TRACE)$/ }).first()
    await expect(chip).toBeVisible()
    const sum = await rgbSum(page, chip, 'backgroundColor')
    // bg-red-900/40 (before fix) = rgba(127,29,29,0.4) → first-3 sum 185 — FAIL
    // bg-red-100    (after fix)  = rgb(254,226,226)    → sum 706          — PASS
    expect(sum, 'severity chip bg should be light, not dark-tinted').toBeGreaterThan(400)
  })
})

// ────────────────────────────────────────────────────────────────────────────
// Incidents list
// ────────────────────────────────────────────────────────────────────────────
test.describe('Light-theme › Incidents list', () => {
  test.beforeEach(async ({ page }) => {
    await setGraphiteTheme(page)
    await mockIncidents(page, [SAMPLE_INCIDENT])
  })

  test('incident card link has light background, not near-black gray', async ({ page }) => {
    await page.goto('/incidents')
    const card = page.locator('a[href="/incidents/1"]')
    await expect(card).toBeVisible()
    const sum = await rgbSum(page, card, 'backgroundColor')
    // bg-gray-800/50 (before fix) = rgba(31,41,55,0.5) → first-3 sum 127  — FAIL
    // cluster-card   (after fix)  = rgb(251,250,246)   → sum 747           — PASS
    expect(sum, 'incident card bg should be light cream, not dark gray').toBeGreaterThan(500)
  })

  test('status badge text is readable dark colour, not near-white', async ({ page }) => {
    await page.goto('/incidents')
    // statusBadge() renders a <span> with the exact status string (lowercase)
    const badge = page.getByText('open', { exact: true }).first()
    await expect(badge).toBeVisible()
    const sum = await rgbSum(page, badge, 'color')
    // text-red-300 (before fix) → sum 582 — FAIL
    // text-red-700 (after fix)  → sum 241 — PASS
    expect(sum, 'status badge text should be dark, not near-white').toBeLessThan(400)
  })
})

// ────────────────────────────────────────────────────────────────────────────
// Incident detail
// ────────────────────────────────────────────────────────────────────────────
test.describe('Light-theme › Incident detail', () => {
  test.beforeEach(async ({ page }) => {
    await setGraphiteTheme(page)
    await mockIncidentDetail(page, 1, SAMPLE_INCIDENT)
  })

  test('severity badge has light background, not dark-tinted', async ({ page }) => {
    await page.goto('/incidents/1')
    await page.waitForSelector('h1')
    // Severity badge is a <span> whose text is the exact severity word
    const sevBadge = page.locator('span').filter({ hasText: /^(critical|high|medium|low)$/ }).first()
    await expect(sevBadge).toBeVisible()
    const sum = await rgbSum(page, sevBadge, 'backgroundColor')
    // bg-red-900/30 (before fix) = rgba(127,29,29,0.3) → first-3 sum 185  — FAIL
    // bg-red-100    (after fix)  = rgb(254,226,226)    → sum 706           — PASS
    expect(sum, 'severity badge bg should be light, not dark-tinted').toBeGreaterThan(400)
  })

  test('incident heading text is dark (not white)', async ({ page }) => {
    await page.goto('/incidents/1')
    const h1 = page.getByRole('heading', { name: /INC-1/ })
    await expect(h1).toBeVisible()
    const sum = await rgbSum(page, h1, 'color')
    // text-white (before fix) = rgb(255,255,255) → sum 765 — FAIL
    // cluster-text via globals = rgb(20,21,22)   → sum 63  — PASS
    // (heading text-white is already covered by globals.css, so this should
    //  pass BEFORE our code changes — it acts as a regression guard.)
    expect(sum, 'INC-1 heading should not be white').toBeLessThan(600)
  })
})
```

- [ ] **Step 2: Run tests — verify they fail as expected**

```bash
cd src/dashboard && npx playwright test tests/light-theme.spec.ts --reporter=line
```

Expected: 5 tests, 4 fail (all except "incident heading text is dark" which is already covered by globals.css).

---

## Task 2 — Extend `globals.css` with missing element types and semantic colour remaps

**Files:**
- Modify: `src/dashboard/app/globals.css`

These additions go at the **end** of the file (after the `[data-theme="graphite"] h1` rule on the last line).

- [ ] **Step 1: Append new rules to globals.css**

Open `src/dashboard/app/globals.css` and add the following block at the very end:

```css
/* ============================================================================
   LIGHT-THEME REMEDIATION: anchor/button elements + semantic palette text
   Extends the LIGHT-THEME DARK-PANEL REMEDIATION block above to cover three
   gaps left by the original selector set:

   1. <a>/<Link> elements used as cards (e.g. incident list rows) — the original
      block only targeted div / section / aside / thead / tr.
   2. <button> elements with a static hardcoded dark bg (e.g. pagination).
   3. Semantic palette text classes that render near-invisible on cream/white:
        text-{red,amber,yellow,orange,green,blue,cyan}-3xx / -4xx
      Mapped to the --sev-* tokens, which are pre-calibrated per light theme
      for WCAG AA contrast against --cluster-bg.
   ============================================================================ */

/* Anchor / Link card containers -------------------------------------------- */
:is([data-theme="graphite"], [data-theme="prism"], [data-theme="md-light"]) a[class*="bg-gray-9"] {
  background-color: rgb(var(--cluster-bg)) !important;
}
:is([data-theme="graphite"], [data-theme="prism"], [data-theme="md-light"]) a[class*="bg-gray-8"] {
  background-color: rgb(var(--cluster-card)) !important;
}
:is([data-theme="graphite"], [data-theme="prism"], [data-theme="md-light"]) a[class*="bg-gray-7"] {
  background-color: rgb(var(--cluster-border) / 0.4) !important;
}
:is([data-theme="graphite"], [data-theme="prism"], [data-theme="md-light"]) a[class*="hover:bg-gray-"]:hover {
  background-color: rgb(var(--cluster-border) / 0.3) !important;
}
/* Gray text inside <a> cards remaps the same as in divs -------------------- */
:is([data-theme="graphite"], [data-theme="prism"], [data-theme="md-light"]) a [class*="text-gray-3"],
:is([data-theme="graphite"], [data-theme="prism"], [data-theme="md-light"]) a [class*="text-gray-4"] {
  color: rgb(var(--cluster-text)) !important;
}
:is([data-theme="graphite"], [data-theme="prism"], [data-theme="md-light"]) a [class*="text-gray-5"],
:is([data-theme="graphite"], [data-theme="prism"], [data-theme="md-light"]) a [class*="text-gray-6"] {
  color: rgb(var(--cluster-muted)) !important;
}

/* Button elements with a static dark bg (not just :hover) ------------------ */
:is([data-theme="graphite"], [data-theme="prism"], [data-theme="md-light"])
button[class*="bg-gray-8"]:not([class*="bg-blue"]):not([class*="bg-red"]):not([class*="bg-green"]):not([class*="bg-purple"]) {
  background-color: rgb(var(--cluster-card)) !important;
  color:            rgb(var(--cluster-text)) !important;
  border-color:     rgb(var(--cluster-border)) !important;
}

/* Semantic palette text: light shades → sev tokens ------------------------- */
:is([data-theme="graphite"], [data-theme="prism"], [data-theme="md-light"]) [class*="text-red-3"],
:is([data-theme="graphite"], [data-theme="prism"], [data-theme="md-light"]) [class*="text-red-4"] {
  color: rgb(var(--sev-crit)) !important;
}
:is([data-theme="graphite"], [data-theme="prism"], [data-theme="md-light"]) [class*="text-amber-3"],
:is([data-theme="graphite"], [data-theme="prism"], [data-theme="md-light"]) [class*="text-amber-4"],
:is([data-theme="graphite"], [data-theme="prism"], [data-theme="md-light"]) [class*="text-yellow-3"],
:is([data-theme="graphite"], [data-theme="prism"], [data-theme="md-light"]) [class*="text-yellow-4"] {
  color: rgb(var(--sev-warn)) !important;
}
:is([data-theme="graphite"], [data-theme="prism"], [data-theme="md-light"]) [class*="text-orange-3"],
:is([data-theme="graphite"], [data-theme="prism"], [data-theme="md-light"]) [class*="text-orange-4"] {
  color: rgb(var(--sev-high)) !important;
}
:is([data-theme="graphite"], [data-theme="prism"], [data-theme="md-light"]) [class*="text-green-3"],
:is([data-theme="graphite"], [data-theme="prism"], [data-theme="md-light"]) [class*="text-green-4"] {
  color: rgb(var(--sev-ok)) !important;
}
:is([data-theme="graphite"], [data-theme="prism"], [data-theme="md-light"]) [class*="text-blue-3"],
:is([data-theme="graphite"], [data-theme="prism"], [data-theme="md-light"]) [class*="text-blue-4"],
:is([data-theme="graphite"], [data-theme="prism"], [data-theme="md-light"]) [class*="text-cyan-3"],
:is([data-theme="graphite"], [data-theme="prism"], [data-theme="md-light"]) [class*="text-cyan-4"] {
  color: rgb(var(--sev-info)) !important;
}
```

- [ ] **Step 2: Run light-theme tests — verify partial progress**

```bash
cd src/dashboard && npx playwright test tests/light-theme.spec.ts --reporter=line
```

Expected after this step:
- `incident card link has light background` → **PASS** (a[class*="bg-gray-8"] rule)
- `status badge text errors page` → **PASS** (text-red-3 → sev-crit)
- `status badge text incidents list` → **PASS** (text-red-3 → sev-crit)
- `incident heading text is dark` → **PASS** (was already passing)
- `severity chip has light background` → still **FAIL** (span[class*="bg-red-9"] not covered by CSS — fixed in Task 3)
- `severity badge detail has light bg` → still **FAIL** (span[class*="bg-red-9"] — fixed in Task 5)

- [ ] **Step 3: Commit globals.css + test file**

```bash
git add src/dashboard/app/globals.css src/dashboard/tests/light-theme.spec.ts
git commit -m "$(cat <<'EOF'
fix(theme): extend light-theme CSS remediation to cover <a>/<button> elements and semantic palette text

Closes gap where incident list Link cards (a[class*="bg-gray-8"]) and
pagination buttons retained dark backgrounds on graphite/prism/md-light.
Adds text-{colour}-3xx/4xx → --sev-* token remaps for near-invisible
light-palette text on cream/white backgrounds.

Also adds light-theme colour visibility Playwright tests.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3 — Fix `errors/page.tsx`

**Files:**
- Modify: `src/dashboard/app/errors/page.tsx`

All changes use `dark:` Tailwind variants. The `.dark` class is absent on graphite/prism/md-light, so the base (light) classes apply on those themes; dark themes get the `dark:` variant.

- [ ] **Step 1: Replace `SEVERITY_STYLE` — collapse `bg`+`text` into a single `cls` field**

Find this block (lines ~134–143):
```typescript
const SEVERITY_STYLE: Record<string, { bg: string; text: string; label: string; rank: number }> = {
  fatal: { bg: 'bg-red-900/40',    text: 'text-red-300',    label: 'FATAL', rank: 5 },
  panic: { bg: 'bg-red-900/40',    text: 'text-red-300',    label: 'PANIC', rank: 5 },
  error: { bg: 'bg-red-800/30',    text: 'text-red-200',    label: 'ERROR', rank: 4 },
  warn:  { bg: 'bg-amber-900/30',  text: 'text-amber-300',  label: 'WARN',  rank: 3 },
  warning: { bg: 'bg-amber-900/30', text: 'text-amber-300', label: 'WARN',  rank: 3 },
  info:  { bg: 'bg-blue-900/30',   text: 'text-blue-300',   label: 'INFO',  rank: 2 },
  debug: { bg: 'bg-gray-700/40',   text: 'text-gray-300',   label: 'DEBUG', rank: 1 },
  trace: { bg: 'bg-gray-700/40',   text: 'text-gray-400',   label: 'TRACE', rank: 1 },
}
```

Replace with:
```typescript
const SEVERITY_STYLE: Record<string, { cls: string; label: string; rank: number }> = {
  fatal:   { cls: 'bg-red-100    dark:bg-red-900/40    text-red-700    dark:text-red-300',    label: 'FATAL', rank: 5 },
  panic:   { cls: 'bg-red-100    dark:bg-red-900/40    text-red-700    dark:text-red-300',    label: 'PANIC', rank: 5 },
  error:   { cls: 'bg-red-50     dark:bg-red-800/30    text-red-600    dark:text-red-200',    label: 'ERROR', rank: 4 },
  warn:    { cls: 'bg-amber-100  dark:bg-amber-900/30  text-amber-700  dark:text-amber-300',  label: 'WARN',  rank: 3 },
  warning: { cls: 'bg-amber-100  dark:bg-amber-900/30  text-amber-700  dark:text-amber-300',  label: 'WARN',  rank: 3 },
  info:    { cls: 'bg-blue-100   dark:bg-blue-900/30   text-blue-700   dark:text-blue-300',   label: 'INFO',  rank: 2 },
  debug:   { cls: 'bg-gray-100   dark:bg-gray-700/40   text-gray-600   dark:text-gray-300',   label: 'DEBUG', rank: 1 },
  trace:   { cls: 'bg-gray-100   dark:bg-gray-700/40   text-gray-500   dark:text-gray-400',   label: 'TRACE', rank: 1 },
}
```

- [ ] **Step 2: Update `SeverityChip` to use the new `cls` field**

Find (lines ~145–155):
```typescript
function SeverityChip({ level }: { level: string }) {
  const s = SEVERITY_STYLE[level?.toLowerCase()] || { bg: 'bg-gray-700/40', text: 'text-gray-400', label: (level || '—').toUpperCase(), rank: 0 }
  return (
    <span
      className={`inline-flex items-center justify-center px-1.5 py-0.5 rounded text-[10px] font-mono font-semibold tabular-nums ${s.bg} ${s.text} min-w-[56px]`}
```

Replace with:
```typescript
function SeverityChip({ level }: { level: string }) {
  const s = SEVERITY_STYLE[level?.toLowerCase()] || { cls: 'bg-gray-100 dark:bg-gray-700/40 text-gray-500 dark:text-gray-400', label: (level || '—').toUpperCase(), rank: 0 }
  return (
    <span
      className={`inline-flex items-center justify-center px-1.5 py-0.5 rounded text-[10px] font-mono font-semibold tabular-nums ${s.cls} min-w-[56px]`}
```

- [ ] **Step 3: Replace `statusColor()` — add `dark:` variants**

Find (lines ~127–130):
```typescript
function statusColor(status: string) {
  if (status === 'open') return 'bg-red-900/30 text-red-300 border-red-700/50'
  if (status === 'resolved') return 'bg-green-900/30 text-green-300 border-green-700/50'
  return 'bg-gray-700/50 text-gray-400 border-gray-600/50'
}
```

Replace with:
```typescript
function statusColor(status: string) {
  if (status === 'open')
    return 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300 border-red-200 dark:border-red-700/50'
  if (status === 'resolved')
    return 'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300 border-green-200 dark:border-green-700/50'
  return 'bg-gray-100 dark:bg-gray-700/50 text-gray-600 dark:text-gray-400 border-gray-200 dark:border-gray-600/50'
}
```

- [ ] **Step 4: Update `SummaryCard` accent colour maps — add `dark:` variants**

Find (lines ~915–926) inside the `SummaryCard` function:
```typescript
  const accentMap = {
    blue: 'border-blue-700/30 bg-blue-900/10',
    amber: 'border-amber-700/30 bg-amber-900/10',
    red: 'border-red-700/30 bg-red-900/10',
    green: 'border-green-700/30 bg-green-900/10',
  }
  const textMap = {
    blue: 'text-blue-300',
    amber: 'text-amber-300',
    red: 'text-red-300',
    green: 'text-green-300',
  }
```

Replace with:
```typescript
  const accentMap = {
    blue:  'border-blue-200  dark:border-blue-700/30  bg-blue-50   dark:bg-blue-900/10',
    amber: 'border-amber-200 dark:border-amber-700/30 bg-amber-50  dark:bg-amber-900/10',
    red:   'border-red-200   dark:border-red-700/30   bg-red-50    dark:bg-red-900/10',
    green: 'border-green-200 dark:border-green-700/30 bg-green-50  dark:bg-green-900/10',
  }
  const textMap = {
    blue:  'text-blue-700  dark:text-blue-300',
    amber: 'text-amber-700 dark:text-amber-300',
    red:   'text-red-700   dark:text-red-300',
    green: 'text-green-700 dark:text-green-300',
  }
```

- [ ] **Step 5: Also fix the label text colour inside `SummaryCard`**

Find inside `SummaryCard` return (lines ~928–930):
```typescript
      <div className="flex items-center gap-1.5 text-xs text-gray-400 mb-1">
```

Replace with:
```typescript
      <div className="flex items-center gap-1.5 text-xs text-cluster-muted mb-1">
```

- [ ] **Step 6: Fix the "Most Common Reason" summary card (4th card, inline in `ErrorsPage`)**

Find (lines ~453–462):
```typescript
          <div className="p-4 bg-gray-800/50 border border-gray-700/50 rounded-lg">
            <div className="flex items-center gap-1.5 text-xs text-gray-400 mb-1">
              <Activity className="w-4 h-4 text-purple-400" />
              Most Common Reason
            </div>
            <div className="text-lg font-bold text-white font-mono truncate" title={topReason}>
              {topReason}
            </div>
          </div>
```

Replace with:
```typescript
          <div className="p-4 bg-cluster-card border border-cluster-border rounded-lg">
            <div className="flex items-center gap-1.5 text-xs text-cluster-muted mb-1">
              <Activity className="w-4 h-4 text-purple-400" />
              Most Common Reason
            </div>
            <div className="text-lg font-bold text-cluster-text font-mono truncate" title={topReason}>
              {topReason}
            </div>
          </div>
```

- [ ] **Step 7: Fix `SpikeBadge` — add `dark:` variants**

Find (lines ~207–213):
```typescript
    <span
      className="inline-flex items-center gap-1 px-1.5 py-0.5 text-[10px] font-semibold font-mono bg-amber-500/20 text-amber-300 border border-amber-500/40 rounded"
      title="Recent rate >2× hourly average"
    >
```

Replace with:
```typescript
    <span
      className="inline-flex items-center gap-1 px-1.5 py-0.5 text-[10px] font-semibold font-mono bg-amber-100 dark:bg-amber-500/20 text-amber-700 dark:text-amber-300 border border-amber-300 dark:border-amber-500/40 rounded"
      title="Recent rate >2× hourly average"
    >
```

- [ ] **Step 8: Fix the service badge in error group rows**

Find (lines ~662–664):
```typescript
                        <span className="px-1.5 py-0.5 bg-blue-900/30 text-blue-300 rounded border border-blue-700/30">
                          {g.service}
                        </span>
```

Replace with:
```typescript
                        <span className="px-1.5 py-0.5 bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 rounded border border-blue-200 dark:border-blue-700/30">
                          {g.service}
                        </span>
```

- [ ] **Step 9: Fix the occurrence count (amber rate) and count badge**

Find the rate display block (lines ~684–696):
```typescript
                            <span className="text-amber-300">{g.rate.count5m}</span>
                            <span className="text-gray-600">/5m</span>
                            <span className="mx-1 text-gray-600">·</span>
                            <span className="text-amber-200">{g.rate.count1h}</span>
                            <span className="text-gray-600">/1h</span>
```

Replace with:
```typescript
                            <span className="text-amber-600 dark:text-amber-300">{g.rate.count5m}</span>
                            <span className="text-cluster-muted">/5m</span>
                            <span className="mx-1 text-cluster-muted">·</span>
                            <span className="text-amber-500 dark:text-amber-200">{g.rate.count1h}</span>
                            <span className="text-cluster-muted">/1h</span>
```

Find the count badge (lines ~710–712):
```typescript
                      <span className="px-2.5 py-1 text-sm font-bold bg-amber-500/20 text-amber-300 border border-amber-500/30 rounded-lg tabular-nums">
                        {g.count.toLocaleString()}
                      </span>
```

Replace with:
```typescript
                      <span className="px-2.5 py-1 text-sm font-bold bg-amber-100 dark:bg-amber-500/20 text-amber-700 dark:text-amber-300 border border-amber-200 dark:border-amber-500/30 rounded-lg tabular-nums">
                        {g.count.toLocaleString()}
                      </span>
```

- [ ] **Step 10: Fix "View full detail" link colour**

Find (lines ~851–855):
```typescript
                      <Link
                        href={`/errors/${g.id}`}
                        className="inline-block mt-2 text-xs text-blue-400 hover:text-blue-300 hover:underline"
                        onClick={e => e.stopPropagation()}
                      >
```

Replace with:
```typescript
                      <Link
                        href={`/errors/${g.id}`}
                        className="inline-block mt-2 text-xs text-blue-600 dark:text-blue-400 hover:text-blue-500 dark:hover:text-blue-300 hover:underline"
                        onClick={e => e.stopPropagation()}
                      >
```

- [ ] **Step 11: Fix Recharts axis tick colours**

Find both `<XAxis>` and `<YAxis>` elements (lines ~475–483):
```typescript
                <XAxis
                  dataKey="reason"
                  tick={{ fill: '#9ca3af', fontSize: 11 }}
                  ...
                />
                <YAxis
                  tick={{ fill: '#9ca3af', fontSize: 11 }}
                  allowDecimals={false}
                />
```

Replace with:
```typescript
                <XAxis
                  dataKey="reason"
                  tick={{ fill: 'rgb(var(--cluster-muted))', fontSize: 11 }}
                  ...
                />
                <YAxis
                  tick={{ fill: 'rgb(var(--cluster-muted))', fontSize: 11 }}
                  allowDecimals={false}
                />
```

Note: Recharts applies `tick.fill` via `style="fill: ..."` on SVG `<text>` elements, so CSS custom properties work here.

- [ ] **Step 12: Fix pagination buttons**

Find the "Previous" and "Next" pagination buttons (lines ~872–888):
```typescript
          <button
            onClick={() => setOffset(Math.max(0, offset - pageSize))}
            disabled={offset === 0}
            className="px-3 py-1.5 text-sm bg-gray-800 border border-gray-700 rounded-lg text-gray-300 hover:bg-gray-700 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
          >
            ← Previous
          </button>
          <span className="text-sm text-gray-500 tabular-nums font-mono">
            Page {Math.floor(offset / pageSize) + 1} of {Math.max(1, Math.ceil(totalCount / pageSize))}
          </span>
          <button
            onClick={() => setOffset(offset + pageSize)}
            disabled={offset + pageSize >= totalCount}
            className="px-3 py-1.5 text-sm bg-gray-800 border border-gray-700 rounded-lg text-gray-300 hover:bg-gray-700 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
          >
            Next →
          </button>
```

Replace with:
```typescript
          <button
            onClick={() => setOffset(Math.max(0, offset - pageSize))}
            disabled={offset === 0}
            className="px-3 py-1.5 text-sm bg-cluster-card border border-cluster-border rounded-lg text-cluster-text hover:bg-cluster-border/50 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
          >
            ← Previous
          </button>
          <span className="text-sm text-cluster-muted tabular-nums font-mono">
            Page {Math.floor(offset / pageSize) + 1} of {Math.max(1, Math.ceil(totalCount / pageSize))}
          </span>
          <button
            onClick={() => setOffset(offset + pageSize)}
            disabled={offset + pageSize >= totalCount}
            className="px-3 py-1.5 text-sm bg-cluster-card border border-cluster-border rounded-lg text-cluster-text hover:bg-cluster-border/50 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
          >
            Next →
          </button>
```

- [ ] **Step 13: Run light-theme tests — verify errors tests pass**

```bash
cd src/dashboard && npx playwright test tests/light-theme.spec.ts --reporter=line
```

Expected: "severity chip has light background" now **PASS**. All 3 errors-page tests pass. Incidents and detail tests remain as before.

- [ ] **Step 14: Also run existing errors tests to verify no regression**

```bash
cd src/dashboard && npx playwright test tests/errors.spec.ts --reporter=line
```

Expected: all pass.

- [ ] **Step 15: Commit**

```bash
git add src/dashboard/app/errors/page.tsx
git commit -m "$(cat <<'EOF'
fix(theme): fix light-theme colour visibility on Errors page

Replace dark-only JS colour classes (SEVERITY_STYLE, statusColor,
SummaryCard, SpikeBadge, badges) with dark: Tailwind variants.
Fix Recharts tick fill to use rgb(var(--cluster-muted)) so chart
axes are readable on cream/white backgrounds.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4 — Fix `incidents/page.tsx`

**Files:**
- Modify: `src/dashboard/app/incidents/page.tsx`

- [ ] **Step 1: Fix `statusBadge()` — add `dark:` variants**

Find (lines ~43–50):
```typescript
  const statusBadge = (s: string) => {
    const styles: Record<string, string> = {
      open: 'bg-red-900/30 text-red-300',
      investigating: 'bg-blue-900/30 text-blue-300',
      resolved: 'bg-green-900/30 text-green-300',
      dismissed: 'bg-gray-700 text-gray-400',
    }
    return <span className={`px-1.5 py-0.5 text-xs rounded ${styles[s] || styles.open}`}>{s}</span>
  }
```

Replace with:
```typescript
  const statusBadge = (s: string) => {
    const styles: Record<string, string> = {
      open:          'bg-red-100   dark:bg-red-900/30   text-red-700   dark:text-red-300',
      investigating: 'bg-blue-100  dark:bg-blue-900/30  text-blue-700  dark:text-blue-300',
      resolved:      'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300',
      dismissed:     'bg-gray-100  dark:bg-gray-700     text-gray-600  dark:text-gray-400',
    }
    return <span className={`px-1.5 py-0.5 text-xs rounded ${styles[s] ?? styles.open}`}>{s}</span>
  }
```

- [ ] **Step 2: Fix incident Link card classes**

Find (lines ~90–92):
```typescript
            <Link key={inc.id} href={`/incidents/${inc.id}`}
              className="block p-4 bg-gray-800/50 border border-gray-700/50 rounded-lg hover:bg-gray-800 transition-colors">
```

Replace with:
```typescript
            <Link key={inc.id} href={`/incidents/${inc.id}`}
              className="block p-4 bg-cluster-card border border-cluster-border rounded-lg hover:bg-cluster-border/30 transition-colors">
```

- [ ] **Step 3: Run light-theme tests**

```bash
cd src/dashboard && npx playwright test tests/light-theme.spec.ts --reporter=line
```

Expected: the two incidents-list tests now **PASS**. All 5 light-theme tests should pass except the incident detail severity badge test.

- [ ] **Step 4: Run existing incidents test for regression check**

```bash
cd src/dashboard && npx playwright test tests/incidents.spec.ts --reporter=line
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add src/dashboard/app/incidents/page.tsx
git commit -m "$(cat <<'EOF'
fix(theme): fix light-theme colour visibility on Incidents list page

Replace dark-only statusBadge() class strings with dark: variants.
Use cluster tokens for incident card container so Link cards show
light background on graphite/prism/md-light.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5 — Fix `incidents/[id]/page.tsx`

**Files:**
- Modify: `src/dashboard/app/incidents/[id]/page.tsx`

- [ ] **Step 1: Fix severity and status badges in the incident header**

Find (lines ~277–283):
```typescript
            <span className={`px-2 py-0.5 text-xs rounded ${
              incident.severity === 'critical' ? 'bg-red-900/30 text-red-300' :
              incident.severity === 'high' ? 'bg-orange-900/30 text-orange-300' :
              'bg-yellow-900/30 text-yellow-300'
            }`}>{incident.severity}</span>
            <span className="px-2 py-0.5 text-xs bg-gray-700 text-gray-300 rounded">{incident.status}</span>
```

Replace with:
```typescript
            <span className={`px-2 py-0.5 text-xs rounded ${
              incident.severity === 'critical' ? 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300' :
              incident.severity === 'high' ? 'bg-orange-100 dark:bg-orange-900/30 text-orange-700 dark:text-orange-300' :
              'bg-yellow-100 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-300'
            }`}>{incident.severity}</span>
            <span className="px-2 py-0.5 text-xs bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300 rounded">{incident.status}</span>
```

- [ ] **Step 2: Fix `markdownComponents` — use cluster tokens instead of hardcoded dark classes**

Find (lines ~141–150):
```typescript
const markdownComponents = {
  pre: MarkdownPre,
  code: MarkdownCode,
  p: ({ children }: { children?: React.ReactNode }) => <p className="mb-1 last:mb-0">{children}</p>,
  ul: ({ children }: { children?: React.ReactNode }) => <ul className="list-disc list-inside space-y-0.5 mb-1">{children}</ul>,
  ol: ({ children }: { children?: React.ReactNode }) => <ol className="list-decimal list-inside space-y-0.5 mb-1">{children}</ol>,
  li: ({ children }: { children?: React.ReactNode }) => <li className="text-gray-300">{children}</li>,
  strong: ({ children }: { children?: React.ReactNode }) => <strong className="text-white font-semibold">{children}</strong>,
  h3: ({ children }: { children?: React.ReactNode }) => <h3 className="text-white font-semibold text-sm mt-2 mb-1">{children}</h3>,
}
```

Replace with:
```typescript
const markdownComponents = {
  pre: MarkdownPre,
  code: MarkdownCode,
  p: ({ children }: { children?: React.ReactNode }) => <p className="mb-1 last:mb-0">{children}</p>,
  ul: ({ children }: { children?: React.ReactNode }) => <ul className="list-disc list-inside space-y-0.5 mb-1">{children}</ul>,
  ol: ({ children }: { children?: React.ReactNode }) => <ol className="list-decimal list-inside space-y-0.5 mb-1">{children}</ol>,
  li: ({ children }: { children?: React.ReactNode }) => <li className="text-cluster-text">{children}</li>,
  strong: ({ children }: { children?: React.ReactNode }) => <strong className="text-cluster-text font-semibold">{children}</strong>,
  h3: ({ children }: { children?: React.ReactNode }) => <h3 className="text-cluster-text font-semibold text-sm mt-2 mb-1">{children}</h3>,
}
```

- [ ] **Step 3: Fix `MarkdownCode` — syntax colours and inline code background**

Find `MarkdownCode` function (lines ~91–105):
```typescript
function MarkdownCode({ className, children }: { className?: string; children?: React.ReactNode }) {
  const inBlock = useContext(InCodeBlock)
  const lang = className?.replace('language-', '') ?? ''
  const colorClass =
    lang === 'bash' || lang === 'sh' || lang === 'shell' ? 'text-yellow-300' :
    lang === 'json' ? 'text-cyan-300' :
    lang === 'yaml' || lang === 'yml' ? 'text-orange-300' :
    lang === 'go' ? 'text-blue-300' :
    'text-green-300'
  return inBlock ? (
    <code className={`text-xs ${colorClass} font-mono ${className ?? ''}`}>{children}</code>
  ) : (
    <code className="bg-gray-900 text-green-300 px-1 py-0.5 rounded text-xs font-mono">{children}</code>
  )
}
```

Replace with:
```typescript
function MarkdownCode({ className, children }: { className?: string; children?: React.ReactNode }) {
  const inBlock = useContext(InCodeBlock)
  const lang = className?.replace('language-', '') ?? ''
  const colorClass =
    lang === 'bash' || lang === 'sh' || lang === 'shell' ? 'text-yellow-700 dark:text-yellow-300' :
    lang === 'json' ? 'text-cyan-700 dark:text-cyan-300' :
    lang === 'yaml' || lang === 'yml' ? 'text-orange-700 dark:text-orange-300' :
    lang === 'go' ? 'text-blue-700 dark:text-blue-300' :
    'text-green-700 dark:text-green-300'
  return inBlock ? (
    <code className={`text-xs ${colorClass} font-mono ${className ?? ''}`}>{children}</code>
  ) : (
    <code className="bg-cluster-border/50 text-cluster-text px-1 py-0.5 rounded text-xs font-mono">{children}</code>
  )
}
```

- [ ] **Step 4: Fix chat user bubble text colour**

Find the chat message rendering block (lines ~460–464):
```typescript
                <div className={`flex-1 min-w-0 rounded-lg px-3 py-2.5 text-sm ${
                  msg.role === 'user'
                    ? 'bg-blue-600/20 border border-blue-700/30 text-blue-100'
                    : 'bg-gray-800/60 border border-gray-700/50 text-gray-200'
                }`}>
```

Replace with:
```typescript
                <div className={`flex-1 min-w-0 rounded-lg px-3 py-2.5 text-sm ${
                  msg.role === 'user'
                    ? 'bg-blue-100 dark:bg-blue-600/20 border border-blue-200 dark:border-blue-700/30 text-blue-800 dark:text-blue-100'
                    : 'bg-cluster-card border border-cluster-border text-cluster-text'
                }`}>
```

- [ ] **Step 5: Fix the chat section border and "Ask AI" section header**

Find (lines ~421–424):
```typescript
      <div className="mt-8 pt-6 border-t border-gray-700">
        <div className="flex items-center justify-between mb-3">
          <h3 className="text-sm font-medium text-gray-400 flex items-center gap-2">
```

Replace with:
```typescript
      <div className="mt-8 pt-6 border-t border-cluster-border">
        <div className="flex items-center justify-between mb-3">
          <h3 className="text-sm font-medium text-cluster-muted flex items-center gap-2">
```

- [ ] **Step 6: Run all light-theme tests — verify all pass**

```bash
cd src/dashboard && npx playwright test tests/light-theme.spec.ts --reporter=line
```

Expected: all 5 tests **PASS**.

- [ ] **Step 7: Run the incidents-detail test suite for regression check**

```bash
cd src/dashboard && npx playwright test tests/incidents-detail.spec.ts --reporter=line
```

Expected: all pass.

- [ ] **Step 8: Commit**

```bash
git add src/dashboard/app/incidents/[id]/page.tsx
git commit -m "$(cat <<'EOF'
fix(theme): fix light-theme colour visibility on Incident detail page

Replace dark-only severity/status badge classes with dark: variants.
Use cluster tokens in markdownComponents (li/strong/h3) and for inline
code background. Fix syntax-highlight colours in code blocks. Fix chat
user bubble colours and Ask AI section border.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6 — Full regression run

- [ ] **Step 1: Run the complete test suite**

```bash
cd src/dashboard && npx playwright test --reporter=line
```

Expected: all tests pass. Pay attention to `visual-audit.spec.ts` — the screenshot tests will capture the new light-theme appearance.

- [ ] **Step 2: Manual smoke check (three light themes)**

Start the dev server if not running:
```bash
bash run-local.sh --yes
```

Then open `http://localhost:3003` in a browser and:
1. Go to Settings → switch to **Graphite** → visit `/errors`, `/incidents`, `/incidents/1`
2. Repeat for **Prism**
3. Repeat for **Material Light**
4. Switch to **Calm Signal** (dark) → visit same pages → verify no regressions

Verify on each light theme: all text is readable, badges have coloured-but-light backgrounds, chart axes have visible labels.

- [ ] **Step 3: If any test fails, investigate and fix before declaring done**

Do not mark this task complete until `npx playwright test` exits 0.

---

## Self-Review

**Spec coverage check:**

| Spec requirement | Covered by |
|---|---|
| `<a>/<Link>` card backgrounds | Task 2 (globals.css) |
| `button` static dark bg | Task 2 (globals.css) |
| `text-*-3xx/4xx` near-invisible text | Task 2 (globals.css) |
| `SEVERITY_STYLE` chips | Task 3 |
| `statusColor()` badges | Task 3 |
| `SummaryCard` accent colours | Task 3 |
| `SpikeBadge` | Task 3 |
| Service/count/rate badges | Task 3 |
| Recharts tick colours | Task 3 |
| Pagination buttons | Task 3 |
| `statusBadge()` incidents list | Task 4 |
| Incident Link card | Task 4 |
| Severity badge incident detail | Task 5 |
| Status badge incident detail | Task 5 |
| `markdownComponents` tokens | Task 5 |
| `MarkdownCode` syntax colours | Task 5 |
| Chat bubble colours | Task 5 |
| Chat border divider | Task 5 |
| Dark themes unaffected | All — scoped by light-theme CSS selectors + `.dark` class presence |
| No API/layout changes | Confirmed — all changes are CSS class strings only |

**Placeholder scan:** No TBDs, no "implement later", all code blocks are complete.

**Type consistency:** `SEVERITY_STYLE` changes `{ bg, text }` to `{ cls }` — `SeverityChip` is updated in the same task (Step 2) to use `s.cls`. No inconsistency window.

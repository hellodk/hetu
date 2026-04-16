# Playwright Test Suite — Design Spec

**Date:** 2026-04-17
**Branch:** deep-analysis-improvement-plan
**Status:** Approved

---

## Goal

Full Playwright coverage for all dashboard pages and navigation, with a single test suite that works against both a mocked backend (default, CI-safe) and a live analyzer backend (`LIVE=true BASE_URL=http://...`). Mocked fixtures must be updated whenever API shapes change.

---

## Architecture

### Mode switching — Option A (LIVE guard)

Every `beforeEach` checks `!!process.env.LIVE`. When false, `page.route()` stubs intercept all API calls. When true, stubs are skipped and real network calls fall through to `BASE_URL`.

```ts
const LIVE = !!process.env.LIVE

test.beforeEach(async ({ page }) => {
  if (!LIVE) {
    await mockHealthReport(page)
    await mockErrorGroups(page, SAMPLE_GROUPS)
  }
  await page.goto('/errors')
})
```

Same assertions run in both modes. This makes mocks a living specification: if a real API response no longer matches mock shape, the live suite catches the drift.

### Run commands

```bash
# Mocked (default, no backend needed)
cd src/dashboard && npx playwright test

# Live (requires running analyzer)
BASE_URL=http://localhost:8080 LIVE=true npx playwright test

# Single spec
npx playwright test tests/errors.spec.ts

# Terse output
npx playwright test --reporter=line
```

---

## File Layout

```
src/dashboard/tests/
  fixtures/
    api.ts               ← extended with new helpers (see below)
  dashboard.spec.ts      ← existing — minor additions only
  drilldown.spec.ts      ← existing — unchanged
  log-scroll.spec.ts     ← existing — unchanged
  score-impact.spec.ts   ← existing — unchanged
  errors.spec.ts         ← NEW
  lb-logs.spec.ts        ← NEW
  incidents.spec.ts      ← NEW
  anomalies.spec.ts      ← NEW
  optimization.spec.ts   ← NEW
  security.spec.ts       ← NEW
  settings.spec.ts       ← NEW
  workloads-list.spec.ts ← NEW
  navigation.spec.ts     ← NEW
```

---

## New Fixture Helpers (`fixtures/api.ts` additions)

Each helper follows the existing pattern: `page.route(glob, handler)`. All are no-ops when `LIVE=true` (callers skip them via the guard).

| Helper | Routes stubbed |
|---|---|
| `mockErrorGroups(page, groups)` | `GET /api/v1/errors/groups`, `GET /api/v1/errors/summary`, `GET /api/v1/llm/config` |
| `mockErrorOccurrences(page, id, data)` | `GET /api/v1/errors/groups/:id` |
| `mockLBList(page, lbs)` | `GET /api/v1/lb/list` |
| `mockLBOverview(page, id, stats, timeline)` | `GET /api/v1/lb/:id/stats`, `GET /api/v1/lb/:id/timeline` |
| `mockLBTab(page, id, tab, data)` | `/api/v1/lb/:id/top-urls`, `/errors`, `/slow`, `/clients`; `GET /api/v1/ingress` |
| `mockIncidents(page, incidents)` | `GET /api/v1/incidents` |
| `mockAnomalies(page, anomalies)` | `GET /api/v1/anomalies` |
| `mockRecommendations(page, recs, summary)` | `GET /api/v1/recommendations`, `GET /api/v1/recommendations/summary` |
| `mockSecurityFindings(page, findings, summary)` | `GET /api/v1/security/findings`, `GET /api/v1/security/summary` |
| `mockSettings(page, config, providers)` | `GET /api/v1/llm/config`, `GET /api/v1/llm/providers` |
| `mockWorkloadList(page, kind, items)` | namespace-scoped or cluster-scoped list endpoint |

---

## Per-Spec Coverage

### `errors.spec.ts`
- Empty state (no groups) renders without crash
- Group list: name, severity badge, occurrence count visible
- Expand group → occurrences table appears
- Status change (resolve/ignore) fires PATCH, list refreshes
- "Analyze with AI" button fires POST `/api/v1/errors/analyze`, shows loading then success toast
- 500 from groups API → error banner shown, no blank page

### `lb-logs.spec.ts`
- Empty LB list → empty state message
- LB selector populates from `/api/v1/lb/list`
- Overview stats (requests/errors/latency) render
- Tab switching: top-urls / errors / slow / clients / ingress each load data
- Search field sends query param to `/api/v1/lb/:id/search`
- Auto-refresh toggle starts/stops interval re-fetch
- 500 from stats API → error banner

### `incidents.spec.ts`
- List renders with title, severity, status
- Severity filter narrows list
- Status filter (open/closed) narrows list
- Empty state when no incidents

### `anomalies.spec.ts`
- List renders anomaly cards
- Refresh button re-fetches
- Empty state when none

### `optimization.spec.ts`
- Recommendations list: title, category, severity visible
- Filter by category narrows list
- Filter by severity narrows list
- "Run Analysis" fires POST, shows loading state, refreshes list on success
- Empty state when no recommendations

### `security.spec.ts`
- Findings list: title, severity badge, resource name visible
- Summary stats (critical/high/medium counts) render
- "Run Scan" fires POST `/api/v1/security/scan`, shows loading then refreshes
- Empty state when no findings

### `settings.spec.ts`
- LLM config fields display (provider, model, endpoint)
- Provider dropdown populated from `/api/v1/llm/providers`
- "Discover Models" fires POST, populates model list
- Save fires PUT `/api/v1/llm/config` with form values
- Capabilities section shows exec/writeActions state

### `workloads-list.spec.ts`
- Pods list: rows render with name, namespace, status
- Empty list → empty state (not blank)
- Row link navigates to `/workloads/pods/{ns}/{name}`
- Cluster-scoped kind (nodes): no namespace column
- 500 from list API → error banner

### `navigation.spec.ts`
- All 8 top-level nav links (`/`, `/errors`, `/lb-logs`, `/incidents`, `/optimization`, `/anomalies`, `/security`, `/settings`) render without 404
- Active nav item highlighted on current route
- Workload dropdown links resolve (spot-check: pods, deployments, services)
- Mobile: nav collapses, hamburger (or equivalent) present

---

## Edge Cases (all specs)

- **500 response** → error banner/message shown; page does not crash or go blank
- **Empty collection** → explicit empty-state UI rendered (not a missing section)
- **POST actions** → loading indicator visible during fetch; success/error feedback after
- **Mobile viewport** (Pixel 5, 393×851) → content visible, no layout overflow
- **`LIVE=true`** → stubs skipped, same assertions validate real API

---

## Maintenance Contract

> **When you add or change an API endpoint, you must update the corresponding mock helper in `tests/fixtures/api.ts` in the same PR.**

This is enforced by convention (code review), not tooling. The mock shape is the contract; drift between mock and real response is caught by the `LIVE` suite.

---

## Out of Scope

- WebSocket live log streaming (covered in `log-scroll.spec.ts` with DOM invariant approach)
- PodExecTerminal interactive tests (requires real cluster exec capability)
- Visual regression / screenshot diffing
- Performance/load testing

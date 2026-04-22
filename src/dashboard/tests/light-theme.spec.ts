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
    (el, p) => (getComputedStyle(el) as unknown as Record<string, string>)[p] ?? '',
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

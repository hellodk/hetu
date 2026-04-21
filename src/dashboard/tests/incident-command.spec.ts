import { test, expect } from '@playwright/test'
import { mockHealthReport } from './fixtures/api'

const CRITICAL_SCORES = {
  overall: 10, reliability: 80, security: 0, cost: 80, architecture: 80,
}
const OK_SCORES = {
  overall: 90, reliability: 90, security: 85, cost: 88, architecture: 92,
}
const DEFAULT_SUMMARY = {
  totalNodes: 3, totalPods: 10, healthyPods: 10, unhealthyPods: 0, pendingPods: 0,
  totalNamespaces: 4, warningEvents: 0, criticalEvents: 0,
}

test.describe('Incident Command Centre', () => {
  // ── CriticalBanner ──────────────────────────────────────────────────────────

  test('CriticalBanner visible when any score ≤ 25', async ({ page }) => {
    await mockHealthReport(page, { scores: CRITICAL_SCORES })
    await page.goto('/')
    const banner = page.getByRole('alert')
    await expect(banner).toBeVisible()
    await expect(banner).toContainText('CRITICAL')
    await expect(banner).toContainText('Security: 0/100')
  })

  test('CriticalBanner hidden when all scores ≥ 26', async ({ page }) => {
    await mockHealthReport(page, { scores: OK_SCORES })
    await page.goto('/')
    await expect(page.getByRole('alert')).not.toBeAttached()
  })

  test('CriticalBanner "View findings" button switches to issues tab', async ({ page }) => {
    await mockHealthReport(page, { scores: CRITICAL_SCORES })
    await page.goto('/')
    await page.getByRole('alert').getByRole('button', { name: /view findings/i }).click()
    await expect(page.getByRole('tab', { name: /issues/i })).toHaveAttribute('aria-selected', 'true')
  })

  // ── StatusBar ───────────────────────────────────────────────────────────────

  test('StatusBar shows LIVE indicator', async ({ page }) => {
    await mockHealthReport(page, {
      scores: OK_SCORES,
      status: { state: 'ok', profile: 'live', message: '', collector: { reachable: true }, llm: { reachable: true }, lastAnalysisAt: new Date().toISOString() },
    })
    await page.goto('/')
    await expect(page.getByRole('banner')).toContainText('LIVE')
  })

  test('StatusBar shows DEMO indicator when mock profile', async ({ page }) => {
    await mockHealthReport(page, {
      scores: OK_SCORES,
      status: { state: 'ok', profile: 'mock', message: '', collector: { reachable: true }, llm: { reachable: true }, lastAnalysisAt: new Date().toISOString() },
    })
    await page.goto('/')
    await expect(page.getByRole('banner')).toContainText('DEMO')
  })

  // ── RiskSummaryPanel ────────────────────────────────────────────────────────

  test('RiskSummaryPanel visible when scores present', async ({ page }) => {
    await mockHealthReport(page, { scores: OK_SCORES })
    await page.goto('/')
    await expect(page.getByRole('region', { name: 'Risk summary' })).toBeVisible()
  })

  test('RiskSummaryPanel shows CRITICAL badge for score ≤ 25', async ({ page }) => {
    await mockHealthReport(page, { scores: { ...OK_SCORES, security: 10 } })
    await page.goto('/')
    const panel = page.getByRole('region', { name: 'Risk summary' })
    const secBtn = panel.getByRole('button', { name: /security/i })
    await expect(secBtn).toContainText('CRITICAL')
  })

  test('RiskSummaryPanel shows OK badge when score ≥ 75', async ({ page }) => {
    await mockHealthReport(page, { scores: OK_SCORES })
    await page.goto('/')
    const panel = page.getByRole('region', { name: 'Risk summary' })
    await expect(panel.getByRole('button').first()).toContainText('OK')
  })

  test('RiskSummaryPanel not rendered when scores null', async ({ page }) => {
    await mockHealthReport(page, {
      scores: null,
      status: { state: 'awaiting', profile: 'live', message: 'Awaiting analysis', collector: { reachable: false }, llm: { reachable: false } },
    })
    await page.goto('/')
    await expect(page.getByRole('region', { name: 'Risk summary' })).not.toBeAttached()
  })

  // ── IncidentsFeed ───────────────────────────────────────────────────────────

  test('IncidentsFeed renders issue titles', async ({ page }) => {
    await mockHealthReport(page, {
      scores: OK_SCORES,
      topIssues: [
        {
          id: 'i1', severity: 'critical', category: 'Security',
          title: 'Privileged container detected',
          description: 'A container is running with privileged: true',
          affectedResources: ['prod/nginx'], confidence: 95,
        },
      ],
    })
    await page.goto('/')
    const feed = page.getByRole('region', { name: 'Active incidents' })
    await expect(feed).toBeVisible()
    await expect(feed.getByText('Privileged container detected')).toBeVisible()
  })

  test('IncidentsFeed shows empty state when no issues', async ({ page }) => {
    await mockHealthReport(page, { scores: OK_SCORES, topIssues: [] })
    await page.goto('/')
    const feed = page.getByRole('region', { name: 'Active incidents' })
    await expect(feed.getByText('No active incidents')).toBeVisible()
  })

  // ── RecommendationsPanel ────────────────────────────────────────────────────

  test('RecommendationsPanel renders recommendation titles', async ({ page }) => {
    await mockHealthReport(page, {
      scores: OK_SCORES,
      recommendations: [{
        id: 'r1', category: 'Cost', title: 'Rightsize nginx deployment',
        severity: 'high', confidence: 90, description: 'CPU requests are 4× actual usage',
        aiReasoning: '',
        impact: { costSavings: { monthly: 45, currency: 'USD' }, riskLevel: 'low', effort: 'low' },
      }],
    })
    await page.goto('/')
    const panel = page.getByRole('region', { name: 'Recommendations' })
    await expect(panel).toBeVisible()
    await expect(panel.getByText('Rightsize nginx deployment')).toBeVisible()
  })

  // ── ClusterVitals ───────────────────────────────────────────────────────────

  test('ClusterVitals shows node and pod counts', async ({ page }) => {
    await mockHealthReport(page, {
      scores: OK_SCORES,
      summary: { ...DEFAULT_SUMMARY, totalNodes: 7, totalPods: 30, healthyPods: 28, unhealthyPods: 1, pendingPods: 1 },
    })
    await page.goto('/')
    const vitals = page.getByRole('region', { name: 'Cluster vitals' })
    await expect(vitals).toBeVisible()
    await expect(vitals.getByText('7')).toBeVisible()
    await expect(vitals.getByText('30')).toBeVisible()
  })

  test('ClusterVitals CPU shows 90% when 9/10 cores used', async ({ page }) => {
    await mockHealthReport(page, {
      scores: OK_SCORES,
      resourceUtilization: {
        cpu:     { used: 9, requested: 9, capacity: 10, unit: 'cores' },
        memory:  { used: 4, requested: 4, capacity: 8, unit: 'GiB' },
        storage: { used: 0, requested: 0, capacity: 0, unit: 'GiB' },
      },
    })
    await page.goto('/')
    const vitals = page.getByRole('region', { name: 'Cluster vitals' })
    await expect(vitals.getByText('90%')).toBeVisible()
  })
})

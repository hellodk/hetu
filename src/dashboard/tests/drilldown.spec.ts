import { test, expect } from '@playwright/test'
import {
  mockHealthReport,
  mockBreakdown,
  mockRuleBreakdown,
  mockWarmingUp,
} from './fixtures/api'

test.describe('Score breakdown drill-down', () => {
  test.beforeEach(async ({ page }) => {
    await mockHealthReport(page)
  })

  test('expands breakdown and lists factor names', async ({ page }) => {
    await mockBreakdown(page, {
      reliability: {
        score: 85,
        factors: [
          { name: 'CrashLoopBackOff pods (3)', impact: -10, resources: ['ns/a', 'ns/b', 'ns/c'], severity: 'high' },
          { name: 'Pending pods (1)', impact: -3, resources: ['ns/d'], severity: 'medium' },
        ],
      },
    })
    await page.goto('/')
    await page.getByRole('button', { name: /Show score breakdown/i }).click()
    await expect(page.getByText('CrashLoopBackOff pods (3)')).toBeVisible()
    await expect(page.getByText('Pending pods (1)')).toBeVisible()
  })

  test('clicks a factor and drills into full resource list', async ({ page }) => {
    // 15 resources — exceeds the legacy 10-truncation limit.
    const resources = Array.from({ length: 15 }, (_, i) => ({
      kind: 'Pod',
      namespace: 'ns',
      name: `pod-${i}`,
      status: 'CrashLoopBackOff',
      impact: -1,
    }))
    await mockBreakdown(page, {
      reliability: {
        score: 60,
        factors: [{ name: 'CrashLoopBackOff pods (15)', impact: -15, resources: [], severity: 'high' }],
      },
    })
    await mockRuleBreakdown(page, 'reliability', 0, {
      rule: 'CrashLoopBackOff pods',
      dimension: 'reliability',
      totalImpact: -15,
      resources,
    })

    await page.goto('/')
    await page.getByRole('button', { name: /Show score breakdown/i }).click()
    await page.getByText('CrashLoopBackOff pods (15)').click()

    // Full list — not truncated. Wait for all 15 links to be attached,
    // then assert exact count. Using a count assertion rather than per-row
    // text checks avoids substring collisions like `ns/pod-1` also
    // matching `ns/pod-10..14`.
    const rows = page.locator('a', { hasText: /ns\/pod-\d+/ })
    await expect(rows).toHaveCount(15)
  })

  test('namespace filter shrinks the resource list', async ({ page }) => {
    const resources = [
      { kind: 'Pod', namespace: 'ns-a', name: 'p1', impact: -1 },
      { kind: 'Pod', namespace: 'ns-a', name: 'p2', impact: -1 },
      { kind: 'Pod', namespace: 'ns-b', name: 'p3', impact: -1 },
    ]
    await mockBreakdown(page, {
      reliability: {
        score: 90,
        factors: [{ name: 'CrashLoopBackOff pods (3)', impact: -3, severity: 'high' }],
      },
    })
    await mockRuleBreakdown(page, 'reliability', 0, {
      rule: 'CrashLoopBackOff pods',
      dimension: 'reliability',
      totalImpact: -3,
      resources,
    })

    await page.goto('/')
    await page.getByRole('button', { name: /Show score breakdown/i }).click()
    await page.getByText('CrashLoopBackOff pods (3)').click()

    // Before filter: all three visible.
    await expect(page.getByText('ns-a/p1')).toBeVisible()
    await expect(page.getByText('ns-a/p2')).toBeVisible()
    await expect(page.getByText('ns-b/p3')).toBeVisible()

    await page.getByLabel('Filter by namespace').selectOption('ns-a')

    await expect(page.getByText('ns-a/p1')).toBeVisible()
    await expect(page.getByText('ns-a/p2')).toBeVisible()
    await expect(page.getByText('ns-b/p3')).toBeHidden()
  })

  test('shows warming-up hint when drill-down returns 503', async ({ page }) => {
    await mockBreakdown(page, {
      reliability: {
        score: 100,
        factors: [{ name: 'CrashLoopBackOff pods (1)', impact: -5, severity: 'high' }],
      },
    })
    await mockWarmingUp(page)

    await page.goto('/')
    await page.getByRole('button', { name: /Show score breakdown/i }).click()
    await page.getByText('CrashLoopBackOff pods (1)').click()

    await expect(page.getByText(/completing its first analysis cycle/i)).toBeVisible()
  })

  test('does not drill on positive-impact bonus rows', async ({ page }) => {
    await mockBreakdown(page, {
      cost: {
        score: 80,
        factors: [
          { name: 'CPU efficiency: 45% used/requested', impact: 18 },
        ],
      },
    })
    // No rule-breakdown route needed — clicking must not trigger a fetch.
    let fetched = false
    await page.route(/\/api\/v1\/health\/breakdown\/cost\/\d+/, async route => {
      fetched = true
      await route.fulfill({ status: 500, body: '' })
    })

    await page.goto('/')
    await page.getByRole('button', { name: /Show score breakdown/i }).click()
    const row = page.getByText('CPU efficiency: 45% used/requested')
    await expect(row).toBeVisible()
    await row.click()
    // Give the UI a tick to fire a fetch if it were going to.
    await page.waitForTimeout(200)
    expect(fetched).toBe(false)
  })
})

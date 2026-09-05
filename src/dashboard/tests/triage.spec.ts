import { test, expect, type Page } from '@playwright/test'
import { mockHealthReport } from './fixtures/api'

async function stubTriage(page: Page) {
  await page.route('**/api/v1/errors/summary**', r =>
    r.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ byReason: { OOMKilled: 4, CrashLoopBackOff: 2, ImagePullBackOff: 5, timeout: 30, 'http.5xx': 12 } }),
    }),
  )
  await page.route('**/api/v1/security/summary**', r =>
    r.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ bySeverity: { critical: 7, high: 40, medium: 0, low: 0 } }),
    }),
  )
  await mockHealthReport(page, {
    summary: {
      totalNodes: 3, totalPods: 10, healthyPods: 8, unhealthyPods: 2, pendingPods: 4,
      totalNamespaces: 4, warningEvents: 500, criticalEvents: 3, namespaces: {},
    },
  })
}

test.describe('Triage board', () => {
  test('renders density-map cells for open issues', async ({ page }) => {
    await stubTriage(page)
    await page.goto('/triage')
    await expect(page.getByTestId('cell-cvecrit')).toBeVisible()
    await expect(page.getByTestId('cell-warnevt')).toBeVisible()
    expect(await page.locator('[data-testid^="cell-"]').count()).toBeGreaterThanOrEqual(6)
  })

  test('Impact weighting enlarges a critical cell vs Count', async ({ page }) => {
    await stubTriage(page)
    await page.goto('/triage')
    const crit = page.getByTestId('cell-cvecrit')
    await expect(crit).toBeVisible()
    const countBox = await crit.boundingBox()
    await page.getByRole('button', { name: /Weight: Impact/i }).click()
    await page.waitForTimeout(700) // allow the layout transition to settle
    const impactBox = await crit.boundingBox()
    expect(impactBox!.width * impactBox!.height).toBeGreaterThan(countBox!.width * countBox!.height)
  })

  test('severity filter scopes the priority spine', async ({ page }) => {
    await stubTriage(page)
    await page.goto('/triage')
    const spine = page.getByTestId('triage-spine')
    await expect(spine.getByText('Warning events')).toBeVisible()
    await page.getByRole('button', { name: /^Critical$/ }).click()
    await expect(spine.getByText('Warning events')).toHaveCount(0)
    await expect(spine.getByText('Critical CVEs')).toBeVisible()
  })
})

import { test, expect, type Page } from '@playwright/test'
import { mockHealthReport } from './fixtures/api'

async function stubPodHealthIssues(page: Page) {
  await page.route('**/api/v1/pods/health**', r =>
    r.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        timestamp: new Date().toISOString(),
        totalPods: 117,
        healthyPods: 95,
        categories: [
          { name: 'crashloop', count: 2, pods: [{ name: 'bookstack' }, { name: 'openproject-web' }] },
          { name: 'oomkilled', count: 1, pods: [{ name: 'victoria-logs-single-server-0' }] },
          { name: 'init-failure', count: 1, pods: [{ name: 'trivy-scan' }] },
          { name: 'high-restarts', count: 18, pods: [{ name: 'jfrog' }, { name: 'kube-proxy' }] },
        ],
      }),
    }),
  )
  await page.route('**/api/v1/errors/summary**', r =>
    r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ byReason: {} }) }),
  )
  await page.route('**/api/v1/security/summary**', r =>
    r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ bySeverity: {} }) }),
  )
  await mockHealthReport(page, {
    summary: {
      totalNodes: 3, totalPods: 117, healthyPods: 95, unhealthyPods: 0, pendingPods: 0,
      totalNamespaces: 30, warningEvents: 0, criticalEvents: 0, namespaces: {},
    },
  })
}

test.describe('Issues page pod health from scanner', () => {
  test('shows High Restarts and Init Failures rows when the scanner reports them', async ({ page }) => {
    await stubPodHealthIssues(page)
    await page.goto('/issues')

    const podCard = page.getByRole('button', { name: /HighRestarts/ })
    await expect(podCard).toBeVisible()
    await podCard.click()

    await expect(page.getByText('High Restarts')).toBeVisible()
    await expect(page.getByText('Init Failures')).toBeVisible()
    await expect(page.getByRole('link', { name: /CrashLoopBackOff/ })).toBeVisible()
    await expect(page.getByRole('link', { name: /OOMKilled/ })).toBeVisible()
  })

  test('does not render "All pod health checks passing" when scanner finds issues', async ({ page }) => {
    await stubPodHealthIssues(page)
    await page.goto('/issues')

    await expect(page.getByText('All pod health checks passing')).toHaveCount(0)
  })
})
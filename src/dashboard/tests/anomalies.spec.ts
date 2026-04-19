import { test, expect } from '@playwright/test'
import { mockAnomalies } from './fixtures/api'

const LIVE = !!process.env.LIVE

const SAMPLE = [
  {
    id: 'anom-1', service: 'api-server', namespace: 'default',
    metric: 'cpu_usage', score: 3.5, expected: 0.2, observed: 0.8,
    severity: 'high', detectedAt: new Date().toISOString(), status: 'active',
  },
  {
    id: 'anom-2', service: 'database', namespace: 'default',
    metric: 'memory_usage', score: 2.1, expected: 0.4, observed: 0.9,
    severity: 'medium', detectedAt: new Date(Date.now() - 1_800_000).toISOString(), status: 'active',
  },
]

test.describe('Anomaly Detection page', () => {
  test.beforeEach(async ({ page }) => {
    if (!LIVE) await mockAnomalies(page, SAMPLE)
  })

  test('renders heading', async ({ page }) => {
    await page.goto('/anomalies')
    await expect(page.getByRole('heading', { name: /Anomaly/i })).toBeVisible()
  })

  test('lists anomaly cards with service name and metric', async ({ page }) => {
    await page.goto('/anomalies')
    await expect(page.getByText('api-server')).toBeVisible()
    await expect(page.getByText('cpu_usage')).toBeVisible()
    await expect(page.getByText('database')).toBeVisible()
  })

  test('shows z-score for each anomaly', async ({ page }) => {
    await page.goto('/anomalies')
    // Z-score is rendered as "z=3.50" or similar
    await expect(page.getByText(/z=3\.5/i)).toBeVisible()
  })

  test('shows severity badge', async ({ page }) => {
    await page.goto('/anomalies')
    await expect(page.getByText('high')).toBeVisible()
    await expect(page.getByText('medium')).toBeVisible()
  })

  test('empty state when no anomalies', async ({ page }) => {
    if (LIVE) return
    await page.route('**/api/v1/anomalies**', r =>
      r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ totalCount: 0, anomalies: [] }) })
    )
    await page.goto('/anomalies')
    await expect(page.getByText(/No anomalies/i)).toBeVisible()
  })

  test('refresh button triggers re-fetch', async ({ page }) => {
    if (LIVE) return
    let callCount = 0
    await page.route('**/api/v1/anomalies**', async route => {
      callCount++
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ totalCount: 0, anomalies: [] }) })
    })
    await page.goto('/anomalies')
    await page.getByRole('button', { name: /Refresh/i }).click()
    await page.waitForTimeout(150)
    expect(callCount).toBeGreaterThanOrEqual(2)
  })

  test('page survives API 500', async ({ page }) => {
    if (LIVE) return
    await page.route('**/api/v1/anomalies**', r => r.fulfill({ status: 500, body: 'error' }))
    await page.goto('/anomalies')
    await expect(page.locator('body')).toBeVisible()
    await expect(page.getByRole('heading', { name: /Anomaly/i })).toBeVisible()
  })
})

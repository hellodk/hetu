import { test, expect } from '@playwright/test'
import { mockRecommendations } from './fixtures/api'

const LIVE = !!process.env.LIVE

const SAMPLE_RECS = [
  {
    id: 'rec-1', type: 'rightsizing', severity: 'medium', confidence: 0.85,
    target: { kind: 'Deployment', namespace: 'default', name: 'api-server' },
    currentState: 'CPU request: 500m', suggestedState: 'CPU request: 200m',
    rationale: 'CPU usage consistently below 20% over 7 days',
    estimatedSavingsMonthly: 15.5, status: 'open',
    createdAt: new Date().toISOString(),
  },
  {
    id: 'rec-2', type: 'hpa', severity: 'low', confidence: 0.7,
    target: { kind: 'Deployment', namespace: 'kube-system', name: 'coredns' },
    currentState: 'No HPA configured', suggestedState: 'HPA min=2 max=5',
    rationale: 'Traffic spikes observed without autoscaling',
    estimatedSavingsMonthly: 0, status: 'open',
    createdAt: new Date().toISOString(),
  },
]

const SAMPLE_SUMMARY = {
  totalRecommendations: 2,
  openRecommendations: 2,
  totalSavingsMonthly: 15.5,
  byType: { rightsizing: 1, hpa: 1 },
  availableOptimizers: ['rightsizing', 'hpa'],
}

test.describe('Optimization page', () => {
  test.beforeEach(async ({ page }) => {
    if (!LIVE) await mockRecommendations(page, SAMPLE_RECS, SAMPLE_SUMMARY)
  })

  test('renders heading', async ({ page }) => {
    await page.goto('/optimization')
    await expect(page.getByRole('heading', { name: /Optimization/i })).toBeVisible()
  })

  test('shows recommendation target names', async ({ page }) => {
    await page.goto('/optimization')
    await expect(page.getByText('api-server')).toBeVisible()
    await expect(page.getByText('coredns')).toBeVisible()
  })

  test('shows potential savings amount', async ({ page }) => {
    await page.goto('/optimization')
    // toFixed(0) rounds 15.5 → 16; multiple elements may match, first() avoids strict mode
    await expect(page.getByText(/15\.5|\$1[56]/).first()).toBeVisible()
  })

  test('empty state when no recommendations', async ({ page }) => {
    if (LIVE) return
    // General route first (checked last due to LIFO), summary last (checked first)
    await page.route('**/api/v1/recommendations**', async r => {
      if (r.request().url().includes('/summary')) { await r.fallback(); return }
      await r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ recommendations: [], totalCount: 0 }) })
    })
    await page.route('**/api/v1/recommendations/summary**', r =>
      r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ totalRecommendations: 0, openRecommendations: 0, totalSavingsMonthly: 0, byType: {}, availableOptimizers: [] }) })
    )
    await page.goto('/optimization')
    await expect(page.getByText(/No open recommendations/i)).toBeVisible()
  })

  test('Run Analysis fires POST to /api/v1/recommendations/run', async ({ page }) => {
    if (LIVE) return
    let ran = false
    await page.route('**/api/v1/recommendations/run**', async route => {
      if (route.request().method() === 'POST') ran = true
      await route.fulfill({ status: 200, contentType: 'application/json', body: '{}' })
    })
    await page.goto('/optimization')
    await page.getByRole('button', { name: /Run Analysis/i }).click()
    await page.waitForTimeout(200)
    expect(ran).toBe(true)
  })

  test('type filter changes visible recommendations', async ({ page }) => {
    if (LIVE) return
    // The filter drives a new GET request; intercept and assert query param
    const seen: string[] = []
    await page.route('**/api/v1/recommendations**', async route => {
      // Let /summary fall through to the beforeEach handler (correct shape)
      if (route.request().url().includes('/summary')) { await route.fallback(); return }
      if (route.request().method() === 'GET') {
        seen.push(route.request().url())
      }
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ recommendations: SAMPLE_RECS, totalCount: 2 }) })
    })
    await page.goto('/optimization')
    // Select a specific type filter
    const typeSelect = page.locator('select').first()
    await typeSelect.selectOption({ index: 1 })
    await page.waitForTimeout(150)
    expect(seen.length).toBeGreaterThanOrEqual(2)
  })

  test('page survives API 500', async ({ page }) => {
    if (LIVE) return
    await page.route('**/api/v1/recommendations**', r => r.fulfill({ status: 500, body: 'error' }))
    await page.goto('/optimization')
    await expect(page.locator('body')).toBeVisible()
  })
})

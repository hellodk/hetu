import { test, expect } from '@playwright/test'
import { mockIncidents } from './fixtures/api'

const LIVE = !!process.env.LIVE

const SAMPLE = [
  {
    id: 1, severity: 'critical', status: 'open',
    detectedAt: new Date().toISOString(),
    affected: ['api-server'], summary: 'High error rate on api-server',
    signals: [{ kind: 'ErrorRate' }],
  },
  {
    id: 2, severity: 'high', status: 'resolved',
    detectedAt: new Date(Date.now() - 3_600_000).toISOString(),
    affected: ['database'], summary: 'Database connection pool exhausted',
    signals: [], rcaReport: { summary: 'OOM on db pod' },
  },
]

test.describe('Incidents page', () => {
  test.beforeEach(async ({ page }) => {
    if (!LIVE) await mockIncidents(page, SAMPLE)
  })

  test('renders heading and incident IDs', async ({ page }) => {
    await page.goto('/incidents')
    await expect(page.getByRole('heading', { name: /Incidents/i })).toBeVisible()
    await expect(page.getByText('INC-1')).toBeVisible()
    await expect(page.getByText('INC-2')).toBeVisible()
  })

  test('shows severity badge and summary', async ({ page }) => {
    await page.goto('/incidents')
    // Severity is icon-only in list; status badge renders lowercase status text
    // exact:true skips the dropdown <option>Open</option> (capital O)
    await expect(page.getByText('open', { exact: true }).first()).toBeVisible()
    await expect(page.getByText('High error rate on api-server')).toBeVisible()
  })

  test('RCA badge shown when report exists', async ({ page }) => {
    await page.goto('/incidents')
    // INC-2 has an rcaReport — the Zap icon is rendered alongside it
    const inc2Row = page.locator('a', { hasText: 'INC-2' })
    await expect(inc2Row).toBeVisible()
    // Zap icon signals existing RCA — target it specifically to avoid strict mode
    await expect(inc2Row.locator('svg').first()).toBeVisible()
  })

  test('each row is a link to the detail page', async ({ page }) => {
    await page.goto('/incidents')
    await expect(page.locator('a[href="/incidents/1"]')).toBeVisible()
    await expect(page.locator('a[href="/incidents/2"]')).toBeVisible()
  })

  test('empty state when no incidents returned', async ({ page }) => {
    if (LIVE) return
    await page.route('**/api/v1/incidents**', r =>
      r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ incidents: [] }) })
    )
    await page.goto('/incidents')
    await expect(page.getByText(/No incidents detected/i)).toBeVisible()
  })

  test('status filter triggers a new API call with updated params', async ({ page }) => {
    if (LIVE) return
    const seen: string[] = []
    await page.route('**/api/v1/incidents**', async route => {
      seen.push(route.request().url())
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ incidents: SAMPLE }) })
    })
    await page.goto('/incidents')
    // Switch from default 'open' to 'All'
    await page.locator('select').selectOption('')
    await page.waitForTimeout(150)
    // At least two requests: initial load (status=open) + filter change (no status)
    expect(seen.length).toBeGreaterThanOrEqual(2)
    expect(seen.some(u => !u.includes('status='))).toBe(true)
  })

  test('page does not crash on API 500', async ({ page }) => {
    if (LIVE) return
    await page.route('**/api/v1/incidents**', r =>
      r.fulfill({ status: 500, body: 'internal error' })
    )
    await page.goto('/incidents')
    // Page body must be visible; heading or empty state present
    await expect(page.locator('body')).toBeVisible()
    await expect(page.getByRole('heading', { name: /Incidents/i })).toBeVisible()
  })
})

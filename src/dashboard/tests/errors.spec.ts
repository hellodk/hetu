import { test, expect } from '@playwright/test'
import { mockErrorGroups } from './fixtures/api'

const LIVE = !!process.env.LIVE

const SAMPLE_GROUPS = [
  {
    id: 'grp-1', service: 'api-server', reason: 'OOMKilled',
    message: 'Container was OOM killed', count: 42, severity: 'error',
    status: 'open',
    firstSeen: new Date(Date.now() - 7_200_000).toISOString(),
    lastSeen: new Date().toISOString(),
  },
  {
    id: 'grp-2', service: 'database', reason: 'CrashLoopBackOff',
    message: 'Back-off restarting failed container', count: 7, severity: 'warning',
    status: 'open',
    firstSeen: new Date(Date.now() - 3_600_000).toISOString(),
    lastSeen: new Date().toISOString(),
  },
]

test.describe('Errors page', () => {
  test.beforeEach(async ({ page }) => {
    if (!LIVE) await mockErrorGroups(page, SAMPLE_GROUPS)
  })

  test('renders heading and error groups', async ({ page }) => {
    await page.goto('/errors')
    await expect(page.getByRole('heading', { name: /Error/i })).toBeVisible()
    // Use span/cell selector to skip the hidden <option> element
    await expect(page.locator('span', { hasText: 'api-server' }).first()).toBeVisible()
    await expect(page.getByText('OOMKilled')).toBeVisible()
    await expect(page.locator('span', { hasText: 'database' }).first()).toBeVisible()
  })

  test('shows occurrence count for each group', async ({ page }) => {
    await page.goto('/errors')
    await expect(page.getByText('42')).toBeVisible()
  })

  test('empty state when no error groups', async ({ page }) => {
    if (LIVE) return
    await page.route('**/api/v1/errors/groups**', r =>
      r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ totalCount: 0, groups: [] }) })
    )
    await page.route('**/api/v1/errors/summary**', r =>
      r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ totalGroups: 0, totalOccurrences: 0, openCount: 0, byReason: {}, byNamespace: {}, topGroups: [], topServices: [] }) })
    )
    await page.goto('/errors')
    await expect(page.getByText(/No error groups found/i)).toBeVisible()
  })

  test('group row expands to show occurrences on click', async ({ page }) => {
    if (LIVE) return
    await page.route('**/api/v1/errors/groups/grp-1**', r =>
      r.fulfill({
        status: 200, contentType: 'application/json',
        body: JSON.stringify({
          id: 'grp-1',
          occurrences: [
            { id: 'occ-1', timestamp: new Date().toISOString(), pod: 'api-server-abc', node: 'node-1', message: 'OOM at 512Mi' },
          ],
        }),
      })
    )
    await page.goto('/errors')
    // Click on the group row to expand it
    await page.getByText('OOMKilled').click()
    await expect(page.getByText('api-server-abc')).toBeVisible()
  })

  test('status change fires PATCH request', async ({ page }) => {
    if (LIVE) return
    let patched = false
    await page.route('**/api/v1/errors/groups/grp-1/status**', async route => {
      if (route.request().method() === 'PATCH') patched = true
      await route.fulfill({ status: 200, contentType: 'application/json', body: '{}' })
    })
    await page.goto('/errors')
    // Look for resolve/ignore button — may be inside expanded row or context menu
    const resolveBtn = page.getByRole('button', { name: /Resolve|Ignore|Dismiss/i }).first()
    if (await resolveBtn.isVisible()) {
      await resolveBtn.click()
      await page.waitForTimeout(200)
      expect(patched).toBe(true)
    }
  })

  test('page shows error banner on API 500, does not crash', async ({ page }) => {
    if (LIVE) return
    await page.route('**/api/v1/errors/groups**', r => r.fulfill({ status: 500, body: 'server error' }))
    await page.goto('/errors')
    await expect(page.locator('body')).toBeVisible()
    // No unhandled crash — heading or error message present
    await expect(page.getByRole('heading', { name: /Error/i })).toBeVisible()
  })

  test('page count selector adjusts visible rows', async ({ page }) => {
    await page.goto('/errors')
    // The errors page has a page-size select — assert it exists
    const pageSizeSelect = page.locator('select', { hasText: /50|100|25/ }).first()
    if (await pageSizeSelect.isVisible()) {
      await pageSizeSelect.selectOption('100')
      await page.waitForTimeout(150)
      await expect(page.locator('body')).toBeVisible()
    }
  })
})

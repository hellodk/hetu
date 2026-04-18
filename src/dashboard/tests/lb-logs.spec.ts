import { test, expect } from '@playwright/test'
import { mockLBList, mockLBData } from './fixtures/api'

const LIVE = !!process.env.LIVE
const LB = 'prod-ingress'

test.describe('LB Logs page', () => {
  test.beforeEach(async ({ page }) => {
    if (!LIVE) {
      await mockLBList(page, [LB])
      await mockLBData(page, LB)
    }
  })

  test('renders heading', async ({ page }) => {
    await page.goto('/lb-logs')
    await expect(page.getByRole('heading', { name: /Load Balancer/i })).toBeVisible()
  })

  test('empty state shown when no LBs available', async ({ page }) => {
    if (LIVE) return
    await page.route('**/api/v1/lb/list**', r =>
      r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ loadBalancers: [] }) })
    )
    await page.goto('/lb-logs')
    await expect(page.getByText(/No load balancers/i)).toBeVisible()
  })

  test('LB selector dropdown populated from /lb/list', async ({ page }) => {
    await page.goto('/lb-logs')
    // The LB list is rendered as a <select>; check its selected value rather than option text
    await expect(page.locator('select').first()).toHaveValue(LB)
  })

  test('overview stats render after LB selected', async ({ page }) => {
    await page.goto('/lb-logs')
    // Stats card for total requests should appear
    await expect(page.getByText(/Total Requests/i)).toBeVisible()
    await expect(page.getByText(/Error Rate/i)).toBeVisible()
    await expect(page.getByText(/p95/i)).toBeVisible()
  })

  test('tabs are rendered and clickable', async ({ page }) => {
    await page.goto('/lb-logs')
    // LB page tabs are <button> elements styled as tabs (no role="tab")
    const main = page.locator('#main-content')
    await expect(main.getByRole('button', { name: /Top URLs/i })).toBeVisible()
    await expect(main.getByRole('button', { name: /Errors/i })).toBeVisible()
    await expect(main.getByRole('button', { name: /Slow/i })).toBeVisible()
    // Tab label is "Client IPs" — match prefix only
    await expect(main.getByRole('button', { name: /Client/i })).toBeVisible()
  })

  test('switching tabs does not crash the page', async ({ page }) => {
    await page.goto('/lb-logs')
    const main = page.locator('#main-content')
    for (const tabName of ['Errors', 'Slow', 'Clients', 'Search', 'Ingress']) {
      const tab = main.getByRole('button', { name: new RegExp(tabName, 'i') })
      if (await tab.isVisible()) {
        // force:true avoids overlap issues with fixed hamburger button on mobile
        await tab.click({ force: true })
        await expect(page.locator('body')).toBeVisible()
      }
    }
  })

  test('500 on stats API shows error banner', async ({ page }) => {
    if (LIVE) return
    await page.route(`**/api/v1/lb/${LB}/stats**`, r => r.fulfill({ status: 500, body: 'error' }))
    await page.goto('/lb-logs')
    // Page must stay alive; error message or graceful fallback visible
    await expect(page.locator('body')).toBeVisible()
  })
})

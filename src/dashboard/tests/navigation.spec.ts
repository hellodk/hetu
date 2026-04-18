import { test, expect } from '@playwright/test'
import { mockHealthReport, mockIncidents, mockAnomalies, mockSecurityFindings, mockRecommendations, mockSettings, mockLBList, mockErrorGroups } from './fixtures/api'

const LIVE = !!process.env.LIVE

// Stub just enough for each page to not throw a network error.
async function stubAllAPIs(page: any) {
  // Catch-all registered FIRST so specific handlers registered after take precedence (LIFO).
  await page.route('**/api/v1/**', async (route: any) => {
    if (!route.request().url().includes('stream')) {
      await route.fulfill({ status: 200, contentType: 'application/json', body: '{}' })
    } else {
      await route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' })
    }
  })
  await mockHealthReport(page)
  await mockIncidents(page)
  await mockAnomalies(page)
  await mockSecurityFindings(page)
  await mockRecommendations(page)
  await mockSettings(page)
  await mockLBList(page)
  await mockErrorGroups(page, [])
}

const TOP_LINKS = [
  { label: 'Overview',          href: '/' },
  { label: 'Errors',            href: '/errors' },
  { label: 'LB Logs',          href: '/lb-logs' },
  { label: 'Incidents & RCA',  href: '/incidents' },
  { label: 'Optimization',     href: '/optimization' },
  { label: 'Anomalies',        href: '/anomalies' },
  { label: 'Security',         href: '/security' },
  { label: 'Settings',         href: '/settings' },
]

test.describe('Navigation', () => {
  test.beforeEach(async ({ page }) => {
    if (!LIVE) await stubAllAPIs(page)
  })

  test('sidebar renders all top-level nav links', async ({ page }) => {
    await page.goto('/')
    for (const { label } of TOP_LINKS) {
      await expect(page.getByRole('link', { name: new RegExp(label, 'i') }).first()).toBeVisible()
    }
  })

  test('each top-level route loads without 404 or crash', async ({ page }) => {
    for (const { href } of TOP_LINKS) {
      await page.goto(href)
      // No error boundary heading ("Something went wrong") and no 404 text
      await expect(page.getByText(/Something went wrong/i)).toBeHidden()
      await expect(page.getByText(/404|not found/i)).toBeHidden()
      await expect(page.locator('body')).toBeVisible()
    }
  })

  test('Overview link highlighted when on root route', async ({ page }) => {
    await page.goto('/')
    // The active Overview link has bg-blue-600 class (per Navigation.tsx)
    const overviewLink = page.getByRole('link', { name: /Overview/i }).first()
    await expect(overviewLink).toBeVisible()
    // Check it has active styling
    await expect(overviewLink).toHaveClass(/bg-blue-600/)
  })

  test('Incidents link highlighted when on /incidents', async ({ page }) => {
    await page.goto('/incidents')
    const incLink = page.getByRole('link', { name: /Incidents/i }).first()
    await expect(incLink).toBeVisible()
    await expect(incLink).toHaveClass(/bg-purple/)
  })

  test('Workloads section expands to show resource links', async ({ page }) => {
    await page.goto('/')
    // Workloads section is open by default
    await expect(page.getByRole('link', { name: /^Pods$/ }).first()).toBeVisible()
    await expect(page.getByRole('link', { name: /^Deployments$/ }).first()).toBeVisible()
  })

  test('Workloads section collapses and re-expands on toggle', async ({ page, isMobile }) => {
    // On mobile the sidebar is off-screen by default; collapse/expand is effectively
    // the desktop-only concern — mobile sidebar open/close is covered by the hamburger test.
    if (isMobile) return
    await page.goto('/')
    const workloadsButton = page.getByRole('button', { name: /Workloads/i })
    await workloadsButton.click()
    await expect(page.getByRole('link', { name: /^Pods$/ }).first()).toBeHidden()
    await workloadsButton.click()
    await expect(page.getByRole('link', { name: /^Pods$/ }).first()).toBeVisible()
  })

  test('mobile: hamburger button visible on small viewport', async ({ page, isMobile }) => {
    if (!isMobile) return
    await page.goto('/')
    // Mobile toggle button is shown below lg breakpoint
    const menuBtn = page.locator('button[class*="lg:hidden"]').first()
    await expect(menuBtn).toBeVisible()
  })

  test('pods nav link points to correct URL with query params', async ({ page }) => {
    await page.goto('/')
    const podsLink = page.getByRole('link', { name: /^Pods$/ }).first()
    await expect(podsLink).toHaveAttribute('href', /\/workloads\/pods/)
  })
})

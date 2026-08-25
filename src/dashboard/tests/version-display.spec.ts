import { test, expect } from '@playwright/test'
import { mockHealthReport } from './fixtures/api'

// Issue #22 — the Hetu version shown in the UI must come from the running
// analyzer (build-injected), never hardcoded in the dashboard bundle.

test('status bar shows the analyzer-reported version', async ({ page }) => {
  await mockHealthReport(page, { version: '9.9.9-test' })
  await page.goto('/')
  const bar = page.getByTestId('status-bar')
  await expect(bar).toContainText('v9.9.9-test')
})

test('footer shows the analyzer-reported version', async ({ page }) => {
  await mockHealthReport(page, { version: '9.9.9-test' })
  await page.goto('/')
  // Gate on data-loaded so the footer isn't read mid-first-fetch.
  await expect(page.getByTestId('status-bar')).toBeVisible()
  await expect(page.locator('footer')).toContainText('Hetu v9.9.9-test')
})

test('version is absent from the JS bundle as a literal', async ({ page }) => {
  // The old source (package.json import) shipped "6.0.0" in every chunk.
  await page.goto('/')
  const body = await page.content()
  expect(body).not.toContain('6.0.0')
})

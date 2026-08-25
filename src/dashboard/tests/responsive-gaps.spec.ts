import { test, expect } from '@playwright/test'
import { mockHealthReport } from './fixtures/api'

// Issue #21 — responsive/UX gaps. These specs pin the behavioral fixes:
// mobile drawer must close after navigation, hash deep links must reach
// every tab, and the chat widget must close on Escape.

test.describe('mobile navigation', () => {
  test.use({ viewport: { width: 390, height: 844 } })

  test('drawer closes after tapping a nav link', async ({ page }) => {
    await page.goto('/')
    const toggle = page.getByRole('button', { name: /toggle navigation/i })
    await expect(toggle).toBeVisible()
    await toggle.click()
    const drawer = page.locator('aside#mobile-nav')
    await expect(drawer).toBeVisible()

    await drawer.getByRole('link', { name: 'Settings' }).first().click()
    await expect(page).toHaveURL(/\/settings/)
    // Drawer must be off-screen again — no lingering overlay after navigate.
    await expect(drawer).not.toBeInViewport()
  })

  test('drawer closes on Escape', async ({ page }) => {
    await page.goto('/')
    await page.getByRole('button', { name: /toggle navigation/i }).click()
    const drawer = page.locator('aside#mobile-nav')
    await expect(drawer).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(drawer).not.toBeInViewport()
  })

  test('hamburger does not overlap the status bar brand', async ({ page }) => {
    await mockHealthReport(page)
    await page.goto('/')
    // StatusBar reserves leading space for the floating toggle below lg.
    const header = page.getByTestId('status-bar')
    await expect(header).toBeVisible()
    const toggleBox = await page.getByRole('button', { name: /toggle navigation/i }).boundingBox()
    const brandBox = await header.locator('h1').boundingBox()
    expect(toggleBox && brandBox).toBeTruthy()
    const overlapX =
      Math.max(0, Math.min(toggleBox!.x + toggleBox!.width, brandBox!.x + brandBox!.width) -
        Math.max(toggleBox!.x, brandBox!.x))
    const overlapY =
      Math.max(0, Math.min(toggleBox!.y + toggleBox!.height, brandBox!.y + brandBox!.height) -
        Math.max(toggleBox!.y, brandBox!.y))
    expect(overlapX * overlapY).toBe(0)
  })
})

test.describe('tab deep links', () => {
  test('#namespaces activates the namespaces tab', async ({ page }) => {
    await mockHealthReport(page)
    await page.goto('/#namespaces')
    await expect(page.getByRole('tab', { name: /namespaces/i })).toHaveAttribute(
      'aria-selected',
      'true'
    )
  })
})

test.describe('chat widget ergonomics', () => {
  test('Escape closes the assistant', async ({ page }) => {
    await page.goto('/')
    await page.getByTestId('chat-launcher').click()
    const panel = page.getByTestId('chat-widget')
    await expect(panel).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(panel).not.toBeVisible()
  })
})

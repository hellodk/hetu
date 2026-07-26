import { test, expect } from '@playwright/test'
import { mockHealthReport } from './fixtures/api'

/**
 * Regression tests for issue #8 — the dashboard was unreachable at its root URL
 * (basePath baked to /hetu) and the StatusBar was hardcoded dark on top of the
 * light theme. These are the executable form of the issue's acceptance criteria.
 */
test.describe('UI regression #8', () => {
  // AC: with no NEXT_BASE_PATH set, the app serves at root `/`.
  // Fails today: basePath defaults to /hetu, so `/` returns Next's 404 page and
  // the app shell never renders.
  test('root URL renders the app shell', async ({ page }) => {
    await mockHealthReport(page)
    const resp = await page.goto('/')
    expect(resp?.status(), 'GET / should be 200, not a 404').toBeLessThan(400)
    await expect(page.getByTestId('status-bar')).toBeVisible()
  })

  // AC: StatusBar respects the active theme. In the default graphite (light)
  // theme the bar must be light, not the hardcoded near-black rgb(20,21,26).
  test('status bar is light in the default (graphite) theme', async ({ page }) => {
    await mockHealthReport(page)
    await page.goto('/')

    const bar = page.getByTestId('status-bar')
    await expect(bar).toBeVisible()

    const bg = await bar.evaluate((el) => getComputedStyle(el).backgroundColor)
    // Parse "rgb(r, g, b)" / "rgba(r, g, b, a)".
    const m = bg.match(/rgba?\((\d+),\s*(\d+),\s*(\d+)/)
    expect(m, `unexpected background color: ${bg}`).not.toBeNull()
    const [r, g, b] = [Number(m![1]), Number(m![2]), Number(m![3])]

    // Hardcoded dark bar was rgb(20, 21, 26); graphite paper is ~rgb(245,243,238).
    expect(bg, 'status bar must not be the hardcoded dark hex').not.toBe('rgb(20, 21, 26)')
    expect(r + g + b, `status bar background ${bg} is not light`).toBeGreaterThan(600)
  })
})

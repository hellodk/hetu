import { test, expect, type Page } from '@playwright/test'
import { mockHealthReport, mockErrorGroups } from './fixtures/api'

// Renders the home page with a single critical pod-health insight so the AI
// Insights feed produces one card that should link to /issues.
async function stubHomeWithPodInsight(page: Page) {
  // Catch-all FIRST (LIFO — specific handlers registered after win).
  await page.route('**/api/v1/**', route => {
    const url = route.request().url()
    if (url.includes('stream')) {
      return route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' })
    }
    return route.fulfill({ status: 200, contentType: 'application/json', body: '{}' })
  })
  await mockHealthReport(page)
  await mockErrorGroups(page, [])
  await page.route('**/api/v1/pods/health**', route =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        categories: [
          { name: 'crashloop', count: 2, pods: [{ namespace: 'production', name: 'checkout-api-abc' }] },
        ],
      }),
    }),
  )
}

test.describe('AI Insights cards are clickable', () => {
  test('a pod-health insight card links to /issues and navigates on click', async ({ page }) => {
    await stubHomeWithPodInsight(page)
    await page.goto('/')

    const card = page.getByRole('link', { name: /crashloop/i }).first()
    await expect(card).toBeVisible()
    await expect(card).toHaveAttribute('href', '/issues')

    await card.click()
    await expect(page).toHaveURL(/\/issues/)
  })

  test('an insight card is keyboard-focusable', async ({ page }) => {
    await stubHomeWithPodInsight(page)
    await page.goto('/')

    const card = page.getByRole('link', { name: /crashloop/i }).first()
    await expect(card).toBeVisible()
    await card.focus()
    await expect(card).toBeFocused()
  })
})

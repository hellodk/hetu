import { test, expect } from '@playwright/test'
import { mockHealthReport, mockSettings } from './fixtures/api'

/**
 * Issue #14 — every password-type input in Settings must expose a
 * show/hide reveal button that flips the field between text and password.
 * The LLM API key field (settings page) is the current password surface.
 */

test.describe('Password reveal #14', () => {
  test('LLM API key field has a reveal toggle', async ({ page }) => {
    await mockHealthReport(page)
    await mockSettings(page)
    await page.goto('/settings')

    const field = page.getByTestId('password-input')
    await expect(field).toBeVisible()
    await expect(field).toHaveAttribute('type', 'password')

    const toggle = page.getByTestId('password-reveal')
    await expect(toggle).toHaveAttribute('aria-label', 'Show password')
    await expect(toggle).toHaveAttribute('aria-pressed', 'false')

    await toggle.click()
    await expect(field).toHaveAttribute('type', 'text')
    await expect(toggle).toHaveAttribute('aria-label', 'Hide password')
    await expect(toggle).toHaveAttribute('aria-pressed', 'true')

    await toggle.click()
    await expect(field).toHaveAttribute('type', 'password')
    // value must survive toggling
    await field.fill('sk-secret-123')
    await page.getByTestId('password-reveal').click()
    await expect(field).toHaveValue('sk-secret-123')
  })
})

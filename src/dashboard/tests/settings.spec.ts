import { test, expect } from '@playwright/test'
import { mockSettings } from './fixtures/api'

const LIVE = !!process.env.LIVE

test.describe('Settings page', () => {
  test.beforeEach(async ({ page }) => {
    if (!LIVE) await mockSettings(page)
  })

  test('renders heading and LLM Configuration section', async ({ page }) => {
    await page.goto('/settings')
    await expect(page.getByRole('heading', { name: /Settings/i })).toBeVisible()
    await expect(page.getByText('LLM Configuration')).toBeVisible()
  })

  test('shows provider value from API', async ({ page }) => {
    await page.goto('/settings')
    // Provider dropdown should contain the configured provider
    const providerSelect = page.locator('select').first()
    await expect(providerSelect).toBeVisible()
    await expect(providerSelect).toHaveValue('ollama')
  })

  test('shows model and endpoint from API', async ({ page }) => {
    await page.goto('/settings')
    const endpointInput = page.locator('input[value="http://localhost:11434"]')
    await expect(endpointInput).toBeVisible()
    const modelInput = page.locator('input[value="llama3.2"]')
    await expect(modelInput).toBeVisible()
  })

  test('provider dropdown is populated from /llm/providers', async ({ page }) => {
    await page.goto('/settings')
    const providerSelect = page.locator('select').first()
    await expect(providerSelect.locator('option', { hasText: 'Ollama' })).toHaveCount(1)
    await expect(providerSelect.locator('option', { hasText: 'OpenAI' })).toHaveCount(1)
  })

  test('Save button disabled when no changes made', async ({ page }) => {
    await page.goto('/settings')
    const saveBtn = page.getByRole('button', { name: /Save Changes/i })
    await expect(saveBtn).toBeDisabled()
  })

  test('Save button enabled after editing a field', async ({ page }) => {
    await page.goto('/settings')
    // Edit the endpoint field to trigger hasChanges
    const endpointInput = page.locator('input[value="http://localhost:11434"]')
    await endpointInput.fill('http://localhost:11435')
    await expect(page.getByRole('button', { name: /Save Changes/i })).toBeEnabled()
  })

  test('Save fires PUT /api/v1/llm/config with updated values', async ({ page }) => {
    if (LIVE) return
    let putBody: string | null = null
    await page.route('**/api/v1/llm/config**', async route => {
      if (route.request().method() === 'PUT') {
        putBody = route.request().postData()
        await route.fulfill({ status: 200, contentType: 'application/json', body: route.request().postData() ?? '{}' })
      } else {
        // Return original config so the endpoint input shows 11434 (the value we'll change)
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
          provider: 'ollama', endpoint: 'http://localhost:11434', model: 'llama3.2',
          maxTokens: 4096, temperature: 0.3, dailyTokenBudget: 0, apiKeySet: false, explainOptimizations: true,
        }) })
      }
    })
    await page.goto('/settings')
    await page.locator('input[value="http://localhost:11434"]').fill('http://localhost:11435')
    await page.getByRole('button', { name: /Save Changes/i }).click()
    await page.waitForTimeout(300)
    expect(putBody).not.toBeNull()
    expect(JSON.parse(putBody!).endpoint).toBe('http://localhost:11435')
  })

  test('Appearance section shows theme dropdown', async ({ page }) => {
    await page.goto('/settings')
    await expect(page.getByText('Appearance')).toBeVisible()
    // Theme picker button should be visible (contains current theme label)
    const themeBtn = page.locator('[aria-haspopup="listbox"]')
    await expect(themeBtn).toBeVisible()
  })

  test('theme dropdown opens and lists themes', async ({ page }) => {
    await page.goto('/settings')
    await page.locator('[aria-haspopup="listbox"]').click()
    await expect(page.getByRole('listbox')).toBeVisible()
    await expect(page.getByRole('option', { name: /Graphite/i })).toBeVisible()
    await expect(page.getByRole('option', { name: /Aurora/i })).toBeVisible()
  })

  test('Cluster Capabilities section rendered', async ({ page }) => {
    await page.goto('/settings')
    await expect(page.getByText('Cluster Capabilities')).toBeVisible()
    await expect(page.getByText('Pod Exec')).toBeVisible()
    await expect(page.getByText('Write Actions')).toBeVisible()
  })
})

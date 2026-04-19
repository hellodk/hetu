import { test, expect } from '@playwright/test'
import { mockSecurityFindings } from './fixtures/api'

const LIVE = !!process.env.LIVE

const SAMPLE_FINDINGS = [
  {
    id: 1, category: 'cis', severity: 'critical',
    title: 'Privileged containers running',
    description: 'Pod is running with privileged: true',
    affectedResources: ['default/nginx-pod'],
    cisControl: 'CIS-5.2.1', remediation: 'Remove privileged flag',
    detectedAt: new Date().toISOString(),
  },
  {
    id: 2, category: 'rbac', severity: 'high',
    title: 'Overly permissive ClusterRole',
    description: 'ClusterRole grants wildcard permissions',
    affectedResources: ['default/cluster-admin-role'],
    cisControl: '', remediation: 'Restrict permissions',
    detectedAt: new Date().toISOString(),
  },
]

test.describe('Security page', () => {
  test.beforeEach(async ({ page }) => {
    if (!LIVE) await mockSecurityFindings(page, SAMPLE_FINDINGS)
  })

  test('renders heading and findings list', async ({ page }) => {
    await page.goto('/security')
    await expect(page.getByRole('heading', { name: /Security/i })).toBeVisible()
    await expect(page.getByText('Privileged containers running')).toBeVisible()
    await expect(page.getByText('Overly permissive ClusterRole')).toBeVisible()
  })

  test('shows severity badges', async ({ page }) => {
    await page.goto('/security')
    // Use exact to avoid matching heading/option elements with different casing
    await expect(page.getByText('critical', { exact: true }).first()).toBeVisible()
    await expect(page.getByText('high', { exact: true }).first()).toBeVisible()
  })

  test('finding row expands on click to show remediation', async ({ page }) => {
    await page.goto('/security')
    await page.getByText('Privileged containers running').click()
    await expect(page.getByText('Remove privileged flag')).toBeVisible()
  })

  test('empty state when no findings', async ({ page }) => {
    if (LIVE) return
    await page.route('**/api/v1/security/findings**', r =>
      r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ totalCount: 0, findings: [] }) })
    )
    await page.route('**/api/v1/security/summary**', r =>
      r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ totalFindings: 0, bySeverity: {}, byCategory: {} }) })
    )
    await page.goto('/security')
    await expect(page.getByText(/No findings found/i)).toBeVisible()
  })

  test('Run Scan button fires POST to /api/v1/security/scan', async ({ page }) => {
    if (LIVE) return
    let scanned = false
    await page.route('**/api/v1/security/scan**', async route => {
      if (route.request().method() === 'POST') scanned = true
      await route.fulfill({ status: 200, contentType: 'application/json', body: '{}' })
    })
    await page.goto('/security')
    await page.getByRole('button', { name: /Run Scan/i }).click()
    await page.waitForTimeout(200)
    expect(scanned).toBe(true)
  })

  test('category tab filter narrows visible findings', async ({ page }) => {
    if (LIVE) return
    await page.goto('/security')
    // Switch to RBAC tab — CIS finding should disappear (scope to main content to avoid nav sidebar button)
    await page.locator('#main-content').getByRole('button', { name: 'RBAC' }).click()
    await expect(page.getByText('Overly permissive ClusterRole')).toBeVisible()
    await expect(page.getByText('Privileged containers running')).toBeHidden()
  })

  test('Load more shown and works when findings exceed 50', async ({ page }) => {
    if (LIVE) return
    const manyFindings = Array.from({ length: 55 }, (_, i) => ({
      id: i + 1, category: 'cis', severity: 'medium',
      title: `Finding ${i + 1}`, description: 'desc',
      affectedResources: [`default/pod-${i}`], cisControl: 'CIS-1.1',
      remediation: 'Fix it', detectedAt: new Date().toISOString(),
    }))
    await page.route('**/api/v1/security/findings**', r =>
      r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ totalCount: 55, findings: manyFindings }) })
    )
    await page.route('**/api/v1/security/summary**', r =>
      r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ totalFindings: 55, bySeverity: { medium: 55 }, byCategory: { cis: 55 } }) })
    )
    await page.goto('/security')
    await expect(page.getByText(/Showing 50 of 55/)).toBeVisible()
    await page.getByRole('button', { name: 'Load more' }).click()
    await expect(page.getByText('Finding 55')).toBeVisible()
    await expect(page.getByText(/Load more/)).toBeHidden()
  })

  test('page survives API 500', async ({ page }) => {
    if (LIVE) return
    await page.route('**/api/v1/security/**', r => r.fulfill({ status: 500, body: 'error' }))
    await page.goto('/security')
    await expect(page.locator('body')).toBeVisible()
  })
})

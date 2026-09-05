import { test, expect } from '@playwright/test'
import { mockHealthReport, mockErrorGroups, mockSecurityFindings } from './fixtures/api'

// Graphite theme severity tokens (globals.css --sev-*). These are the values the
// severity UI MUST resolve to. The "bright" values below are the generic Tailwind
// utilities the page used before #27 and must never appear on a severity element.
const SEV_CRIT = 'rgb(155, 28, 46)' // --sev-crit
const BRIGHT_RED = 'rgb(239, 68, 68)' // red-500 — the old, off-theme critical color

async function loadIssues(page: import('@playwright/test').Page) {
  await mockHealthReport(page, {
    scores: { overall: 28, reliability: 40, security: 0, cost: 97, architecture: 55 },
    topIssues: [
      { id: 'i1', severity: 'critical', category: 'security', title: 'nginx CVE-2024-7347 (RCE)', description: 'Remote code execution' },
    ],
  })
  await mockErrorGroups(page, [])
  await mockSecurityFindings(page, [], { bySeverity: { critical: 7, high: 82, medium: 0, low: 0 } })
  await page.goto('/issues')
}

test.describe('/issues renders severity through graphite theme tokens', () => {
  test('the critical severity counter uses --sev-crit, not bright red-500', async ({ page }) => {
    await loadIssues(page)
    const crit = page.locator('.text-sev-crit').first()
    await expect(crit).toBeVisible()
    const color = await crit.evaluate(el => getComputedStyle(el).color)
    expect(color).toBe(SEV_CRIT)
    expect(color).not.toBe(BRIGHT_RED)
  })

  test('the critical top-issue dot fills with --sev-crit', async ({ page }) => {
    await loadIssues(page)
    const dot = page.locator('span.bg-sev-crit').first()
    await expect(dot).toBeVisible()
    const bg = await dot.evaluate(el => getComputedStyle(el).backgroundColor)
    expect(bg).toBe(SEV_CRIT)
  })

  test('no severity element in the issues summary uses the bright Tailwind palette', async ({ page }) => {
    await loadIssues(page)
    await page.locator('.text-sev-crit').first().waitFor()
    // Scope to the page main content (excludes shared nav / chat widget which
    // are out of scope for #27).
    const bad = await page.locator('main').evaluate(root => {
      const forbidden = new Set([
        'rgb(239, 68, 68)', // red-500
        'rgb(249, 115, 22)', // orange-500
        'rgb(234, 179, 8)', // amber/yellow-500
        'rgb(34, 197, 94)', // green-500
        'rgb(16, 185, 129)', // emerald-500
      ])
      const hits: string[] = []
      root.querySelectorAll('*').forEach(el => {
        const s = getComputedStyle(el)
        for (const c of [s.color, s.backgroundColor, s.borderTopColor]) {
          if (forbidden.has(c)) hits.push(`${el.tagName}.${(el.className || '').toString().slice(0, 40)} → ${c}`)
        }
      })
      return hits
    })
    expect(bad).toEqual([])
  })
})

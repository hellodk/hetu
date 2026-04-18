import { test, expect } from '@playwright/test'
import { mockIncidentDetail, mockAskAI } from './fixtures/api'

const LIVE = !!process.env.LIVE

const NOW = new Date().toISOString()

const SIGNALS = [
  {
    timestamp: NOW,
    source: 'logs', severity: 'critical',
    service: 'api-server', namespace: 'default', pod: 'api-server-abc',
    kind: 'exception', title: 'NullPointerException in request handler',
    details: 'java.lang.NullPointerException at RequestHandler.java:42',
  },
  {
    timestamp: new Date(Date.now() - 30_000).toISOString(),
    source: 'k8s', severity: 'high',
    service: '', namespace: 'default', pod: 'api-server-abc',
    kind: 'restart', title: 'Pod restarted (OOMKilled)',
    details: '',
  },
]

const SAMPLE_RCA = {
  summary: 'Memory pressure in api-server caused OOMKill and exception cascade',
  rootCause: {
    primary: 'Container memory limit exceeded',
    confidence: 0.92,
    description: 'api-server container exceeded its 256Mi memory limit under traffic spike',
  },
  contributingFactors: ['No memory limit set', 'Traffic spike at 14:00'],
  blastRadius: { services: ['api-server'], users: '~500 active users', severity: 'high' },
  remediation: [
    { step: 'Increase memory limit to 512Mi', risk: 'low', automatable: true, estimatedEffort: '5m' },
    { step: 'Add HPA to handle traffic spikes', risk: 'medium', automatable: false, estimatedEffort: '30m' },
  ],
  preventiveMeasures: ['Set resource limits', 'Enable VPA recommendations'],
  evidence: [
    { id: 'e1', type: 'log', ref: 'pod/api-server-abc', snippet: 'OOMKilled: memory limit exceeded' },
  ],
  model: 'llama3.2',
  promptTokens: 1245,
  outputTokens: 487,
  createdAt: NOW,
}

const INC_NO_RCA = {
  id: 1, severity: 'critical', status: 'open',
  detectedAt: NOW,
  affected: ['api-server', 'default/api-server-abc'],
  summary: '2 signals: 1 exception, 1 restart affecting api-server',
  signals: SIGNALS,
}

const INC_WITH_RCA = { ...INC_NO_RCA, rcaReport: SAMPLE_RCA }

// ──────────────────────────────────────────────────────────────────────────────

test.describe('Incident detail page', () => {
  test('renders incident header with ID, severity and status', async ({ page }) => {
    if (!LIVE) await mockIncidentDetail(page, 1, INC_WITH_RCA)
    await page.goto('/incidents/1')
    await expect(page.getByRole('heading', { name: /INC-1/ })).toBeVisible()
    await expect(page.getByText('critical')).toBeVisible()
    await expect(page.getByText('open')).toBeVisible()
    await expect(page.getByText('2 signals: 1 exception, 1 restart affecting api-server')).toBeVisible()
  })

  test('renders signal timeline with source, kind and title', async ({ page }) => {
    if (!LIVE) await mockIncidentDetail(page, 1, INC_WITH_RCA)
    await page.goto('/incidents/1')
    await expect(page.getByText('Signal Timeline (2)')).toBeVisible()
    // exact:true so we don't match the "LB Logs" nav link which also contains "logs"
    await expect(page.getByText('logs', { exact: true })).toBeVisible()
    await expect(page.getByText('exception', { exact: true })).toBeVisible()
    await expect(page.getByText('NullPointerException in request handler')).toBeVisible()
    await expect(page.getByText('restart', { exact: true })).toBeVisible()
    await expect(page.getByText('Pod restarted (OOMKilled)')).toBeVisible()
    // Signal detail text (details field)
    await expect(page.getByText(/NullPointerException at RequestHandler/)).toBeVisible()
  })

  test('displays existing RCA summary, root cause and confidence', async ({ page }) => {
    if (!LIVE) await mockIncidentDetail(page, 1, INC_WITH_RCA)
    await page.goto('/incidents/1')
    await expect(page.getByRole('heading', { name: /Root Cause Analysis/ })).toBeVisible()
    await expect(page.getByText('Memory pressure in api-server caused OOMKill and exception cascade')).toBeVisible()
    await expect(page.getByText('Container memory limit exceeded')).toBeVisible()
    await expect(page.getByText('api-server container exceeded its 256Mi memory limit under traffic spike')).toBeVisible()
    await expect(page.getByText('confidence: 92%')).toBeVisible()
  })

  test('shows remediation steps with risk and effort', async ({ page }) => {
    if (!LIVE) await mockIncidentDetail(page, 1, INC_WITH_RCA)
    await page.goto('/incidents/1')
    await expect(page.getByText('Remediation Steps')).toBeVisible()
    await expect(page.getByText('Increase memory limit to 512Mi')).toBeVisible()
    await expect(page.getByText('automatable')).toBeVisible()
    await expect(page.getByText('Add HPA to handle traffic spikes')).toBeVisible()
  })

  test('shows evidence and preventive measures', async ({ page }) => {
    if (!LIVE) await mockIncidentDetail(page, 1, INC_WITH_RCA)
    await page.goto('/incidents/1')
    await expect(page.getByText('OOMKilled: memory limit exceeded')).toBeVisible()
    await expect(page.getByText('Preventive Measures')).toBeVisible()
    await expect(page.getByText('Set resource limits')).toBeVisible()
  })

  test('SSE stream populates the RCA section on completion', async ({ page }) => {
    // Use explicit routes (not the mockIncidentDetail helper) to avoid any regex
    // interference between the incident route and the SSE route.
    // addInitScript stubs EventSource before React loads so the auto-trigger fires
    // the complete event via our stub rather than via the real network.
    if (!LIVE) {
      await page.route(/\/api\/v1\/incidents\/1(?:\?.*)?$/, async route => {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(INC_NO_RCA) })
      })
      const dataStr = JSON.stringify(JSON.stringify(SAMPLE_RCA))
      await page.addInitScript(`
        (() => {
          var DATA = ${dataStr};
          window.EventSource = function(url) {
            var self = this; var ls = {};
            self.addEventListener = function(t, fn) { (ls[t] = ls[t] || []).push(fn); };
            self.removeEventListener = function(t, fn) { if (ls[t]) ls[t] = ls[t].filter(function(f) { return f !== fn; }); };
            self.close = function() {};
            setTimeout(function() {
              var ev = new MessageEvent('complete', { data: DATA });
              (ls['complete'] || []).forEach(function(fn) { try { fn(ev); } catch(e) {} });
            }, 400);
          };
        })();
      `)
    }
    await page.goto('/incidents/1')
    await expect(page.getByText('Container memory limit exceeded')).toBeVisible({ timeout: 10000 })
    await expect(page.getByRole('heading', { name: /Root Cause Analysis/ })).toBeVisible()
  })

  test('shows RCA-in-progress banner while stream is pending', async ({ page }) => {
    if (LIVE) return
    // Use explicit route literals — avoids any regex match ambiguity between
    // the incident endpoint and the SSE stream endpoint.
    await page.route(/\/api\/v1\/incidents\/1(?:\?.*)?$/, async route => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(INC_NO_RCA) })
    })
    // Stall SSE for 5 s then abort — regenerating=true during the stall → banner visible.
    await page.route(/\/api\/v1\/incidents\/1\/rca\/stream/, async route => {
      await new Promise<void>(res => setTimeout(res, 5000))
      await route.abort()
    })
    await page.goto('/incidents/1')
    await expect(page.getByText(/Running Root Cause Analysis/)).toBeVisible({ timeout: 5000 })
  })

  test('Ask AI sends question via Enter key and displays answer', async ({ page }) => {
    if (!LIVE) {
      await mockIncidentDetail(page, 1, INC_WITH_RCA)
      await mockAskAI(page, 'The api-server-abc pod in the default namespace.')
    }
    await page.goto('/incidents/1')
    const input = page.getByLabel('Ask AI a question about this incident')
    await input.fill('Which pod is affected?')
    await input.press('Enter')
    await expect(page.getByText('Which pod is affected?')).toBeVisible()
    await expect(page.getByText('The api-server-abc pod in the default namespace.')).toBeVisible({ timeout: 6000 })
  })

  test('Ask AI sends question via button click', async ({ page }) => {
    if (!LIVE) {
      await mockIncidentDetail(page, 1, INC_WITH_RCA)
      await mockAskAI(page, 'Yes, it exceeded 256Mi.')
    }
    await page.goto('/incidents/1')
    const input = page.getByLabel('Ask AI a question about this incident')
    await input.fill('Did the memory limit trigger?')
    // Send button is the immediate sibling of the labeled input
    await page.locator('[aria-label="Ask AI a question about this incident"] + button').click()
    await expect(page.getByText('Yes, it exceeded 256Mi.')).toBeVisible({ timeout: 6000 })
  })

  test('restores chat history from localStorage on page load', async ({ page }) => {
    if (!LIVE) await mockIncidentDetail(page, 1, INC_WITH_RCA)
    await page.goto('/incidents/1')
    // Inject persisted messages before reload so useEffect picks them up
    await page.evaluate(() => {
      localStorage.setItem('incident-chat-1', JSON.stringify([
        { id: '1-u', role: 'user',      content: 'Which pod restarted?' },
        { id: '1-a', role: 'assistant', content: 'The api-server-abc pod in default.' },
      ]))
    })
    await page.reload()
    await expect(page.getByText('Which pod restarted?')).toBeVisible()
    await expect(page.getByText('The api-server-abc pod in default.')).toBeVisible()
  })

  test('persists new messages to localStorage after Ask AI', async ({ page }) => {
    if (!LIVE) {
      await mockIncidentDetail(page, 1, INC_WITH_RCA)
      await mockAskAI(page, 'It was a memory limit issue.')
    }
    await page.goto('/incidents/1')
    await page.getByLabel('Ask AI a question about this incident').fill('What caused this?')
    await page.locator('[aria-label="Ask AI a question about this incident"] + button').click()
    await expect(page.getByText('It was a memory limit issue.')).toBeVisible({ timeout: 6000 })
    // localStorage should now contain the question
    const stored = await page.evaluate(() => localStorage.getItem('incident-chat-1'))
    expect(stored).not.toBeNull()
    expect(stored).toContain('What caused this?')
    expect(stored).toContain('It was a memory limit issue.')
  })

  test('clear history removes messages and deletes localStorage key', async ({ page }) => {
    if (!LIVE) await mockIncidentDetail(page, 1, INC_WITH_RCA)
    await page.goto('/incidents/1')
    await page.evaluate(() => {
      localStorage.setItem('incident-chat-1', JSON.stringify([
        { id: '1-u', role: 'user',      content: 'Old question' },
        { id: '1-a', role: 'assistant', content: 'Old answer' },
      ]))
    })
    await page.reload()
    await expect(page.getByText('Old question')).toBeVisible()
    await page.getByText('Clear history').click()
    await expect(page.getByText('Clear all history?')).toBeVisible()
    await page.getByText('Yes').click()
    await expect(page.getByText('Old question')).not.toBeVisible()
    const stored = await page.evaluate(() => localStorage.getItem('incident-chat-1'))
    expect(stored).toBeNull()
  })

  test('copy button in chat code block becomes clickable and toggles icon', async ({ page }) => {
    if (!LIVE) await mockIncidentDetail(page, 1, INC_WITH_RCA)
    await page.goto('/incidents/1')
    // Seed a chat message with a fenced bash code block
    await page.evaluate(() => {
      localStorage.setItem('incident-chat-1', JSON.stringify([
        { id: '1-a', role: 'assistant', content: '```bash\nkubectl get pods -n default\n```' },
      ]))
    })
    await page.reload()
    const pre = page.locator('pre').first()
    await expect(pre).toBeVisible()
    // Override clipboard to avoid headless permission error
    await page.evaluate(() => {
      Object.defineProperty(navigator, 'clipboard', {
        value: { writeText: () => Promise.resolve() },
        configurable: true,
      })
    })
    await pre.hover()
    const copyBtn = page.locator('button[title="Copy code"]')
    await copyBtn.click()
    // After click, Copy icon is replaced by Check (text-green-400)
    await expect(page.locator('button[title="Copy code"] .text-green-400')).toBeVisible()
  })

  test('page survives incident API 500 without crashing', async ({ page }) => {
    if (LIVE) return
    await page.route(new RegExp('/api/v1/incidents/1(?:\\?.*)?$'), r =>
      r.fulfill({ status: 500, body: 'internal error' })
    )
    await page.goto('/incidents/1')
    await page.waitForLoadState('networkidle')
    // Body must remain; no unhandled crash (Next.js error boundary or graceful empty state)
    await expect(page.locator('body')).toBeVisible()
    // The not-found branch renders a specific div when the incident is missing
    await expect(page.getByText('Incident not found')).toBeVisible()
  })
})

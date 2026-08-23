import { test, expect } from '@playwright/test'
import { mockHealthReport } from './fixtures/api'

/**
 * Issue #10 — floating chat widget wired to the analyzer's POST /api/v1/chat SSE.
 * The endpoint is mocked so the test is hermetic (no analyzer/LLM needed).
 */

// A canned SSE stream: conversation → tool → two answer tokens → done.
const SSE_STREAM = [
  'data: {"type":"conversation","conversationId":"conv_test"}',
  '',
  'data: {"type":"tool","name":"get_pods","args":{"namespace":"kube-system"}}',
  '',
  'data: {"type":"token","content":"All "}',
  '',
  'data: {"type":"token","content":"pods are healthy."}',
  '',
  'data: {"type":"done","conversationId":"conv_test"}',
  '',
  '',
].join('\n')

test.describe('Chat widget #10', () => {
  test('opens, sends a message, renders tool chip + streamed answer', async ({ page }) => {
    await mockHealthReport(page)
    // Registered AFTER the health mock so it wins for this exact path (route LIFO).
    await page.route('**/api/v1/chat', async (route) => {
      await route.fulfill({
        status: 200,
        headers: { 'content-type': 'text/event-stream', 'x-conversation-id': 'conv_test' },
        body: SSE_STREAM,
      })
    })
    await page.goto('/')

    // Launcher visible; open the panel.
    await page.getByTestId('chat-launcher').click()
    await expect(page.getByTestId('chat-widget')).toBeVisible()

    // Send a message.
    await page.getByTestId('chat-input').fill('are my pods healthy?')
    await page.getByTestId('chat-send').click()

    // Agentic tool step + streamed grounded answer render.
    await expect(page.getByTestId('chat-tool-chip')).toContainText('get_pods')
    await expect(page.getByTestId('chat-message-assistant')).toContainText('All pods are healthy.')
  })

  // Issue #12 — citation frames emitted by the analyzer must render as
  // grounding chips under the assistant turn (previously dropped).
  test('renders citation chips from citation frames', async ({ page }) => {
    const cited = [
      'data: {"type":"conversation","conversationId":"conv_cited"}',
      '',
      'data: {"type":"citation","citation":{"kind":"doc","ref":"docs/SCORING_SYSTEM.md","title":"Scoring System","snippet":"weighted rules"}}',
      '',
      'data: {"type":"token","content":"Scores come from weighted rules [docs/SCORING_SYSTEM.md]."}',
      '',
      'data: {"type":"done","conversationId":"conv_cited"}',
      '',
      '',
    ].join('\n')

    await mockHealthReport(page)
    await page.route('**/api/v1/chat', async (route) => {
      await route.fulfill({
        status: 200,
        headers: { 'content-type': 'text/event-stream', 'x-conversation-id': 'conv_cited' },
        body: cited,
      })
    })
    await page.goto('/')
    await page.getByTestId('chat-launcher').click()
    await page.getByTestId('chat-input').fill('how are scores computed?')
    await page.getByTestId('chat-send').click()

    const chip = page.getByTestId('chat-citation-chip')
    await expect(chip).toContainText('Scoring System')
    await expect(chip).toHaveAttribute('title', 'docs/SCORING_SYSTEM.md')
  })

  test('widget is light-themed in the default theme', async ({ page }) => {
    await mockHealthReport(page)
    await page.goto('/')
    await page.getByTestId('chat-launcher').click()

    const bg = await page.getByTestId('chat-widget').evaluate(
      (el) => getComputedStyle(el).backgroundColor,
    )
    const m = bg.match(/rgba?\((\d+),\s*(\d+),\s*(\d+)/)
    expect(m, `unexpected background: ${bg}`).not.toBeNull()
    const [r, g, b] = [Number(m![1]), Number(m![2]), Number(m![3])]
    expect(r + g + b, `widget background ${bg} is not light`).toBeGreaterThan(600)
  })
})

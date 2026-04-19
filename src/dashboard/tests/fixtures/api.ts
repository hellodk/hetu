import type { Page } from '@playwright/test'

// Minimal shape helpers for the dashboard API responses the UI consumes.

export interface BreakdownFactor {
  name: string
  impact: number
  resources?: string[]
  severity?: string
}

export interface BreakdownDimension {
  score: number
  factors: BreakdownFactor[]
}

export interface BreakdownResource {
  kind: string
  namespace: string
  name: string
  status?: string
  impact: number
  detail?: string
}

export interface RuleBreakdownResponse {
  rule: string
  dimension: string
  totalImpact: number
  resources: BreakdownResource[]
}

export interface ResourceImpactRule {
  dimension: string
  rule: string
  impact: number
  remediation: string
}

export interface ResourceImpactResponse {
  kind: string
  namespace: string
  name: string
  rules: ResourceImpactRule[]
}

const emptyHealthReport = {
  clusterId: 'test',
  timestamp: new Date().toISOString(),
  scores: {
    overall: 90,
    reliability: 92,
    security: 88,
    cost: 85,
    architecture: 95,
  },
  summary: {
    totalNodes: 3,
    totalPods: 10,
    totalNamespaces: 4,
    healthyPods: 10,
    unhealthyPods: 0,
    pendingPods: 0,
    warningEvents: 0,
    criticalEvents: 0,
    namespaces: {},
  },
  resourceUtilization: {
    cpu:     { used: 0, requested: 0, capacity: 0, unit: 'cores' },
    memory:  { used: 0, requested: 0, capacity: 0, unit: 'bytes' },
    storage: { used: 0, requested: 0, capacity: 0, unit: 'bytes' },
  },
  estimatedMonthlySavings: 0,
  trends: {},
  topIssues: [],
  recommendations: [],
  status: {
    state: 'healthy',
    profile: 'live',
    collector: { reachable: true },
    llm: { reachable: true },
    lastAnalysisAt: new Date().toISOString(),
  },
}

export async function mockHealthReport(page: Page, overrides: any = {}) {
  await page.route('**/api/v1/health', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ ...emptyHealthReport, ...overrides }),
    })
  })
  // The main page also opens an SSE stream — stub it to an empty stream.
  await page.route('**/api/v1/health/stream', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'text/event-stream',
      body: '',
    })
  })
  // Capabilities probe used by the workload page.
  await page.route('**/api/v1/k8s/capabilities', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ exec: false, writeActions: false }),
    })
  })
}

export async function mockBreakdown(
  page: Page,
  dims: {
    reliability?: BreakdownDimension
    security?: BreakdownDimension
    cost?: BreakdownDimension
    architecture?: BreakdownDimension
  } = {}
) {
  const empty: BreakdownDimension = { score: 100, factors: [] }
  const body = {
    reliability: dims.reliability ?? empty,
    security: dims.security ?? empty,
    cost: dims.cost ?? empty,
    architecture: dims.architecture ?? empty,
  }
  await page.route('**/api/v1/health/breakdown', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(body),
    })
  })
}

export async function mockRuleBreakdown(
  page: Page,
  dimension: string,
  index: number,
  response: RuleBreakdownResponse,
) {
  const pattern = new RegExp(`/api/v1/health/breakdown/${dimension}/${index}$`)
  await page.route(pattern, async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(response),
    })
  })
}

export async function mockWarmingUp(page: Page) {
  await page.route(/\/api\/v1\/health\/breakdown\/[^/]+\/\d+$/, async route => {
    await route.fulfill({
      status: 503,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'analyzer warming up' }),
    })
  })
}

export async function mockResourceImpact(page: Page, rules: ResourceImpactRule[] = []) {
  await page.route('**/api/v1/health/resource-impact**', async route => {
    const url = new URL(route.request().url())
    const body: ResourceImpactResponse = {
      kind: url.searchParams.get('kind') || '',
      namespace: url.searchParams.get('namespace') || '',
      name: url.searchParams.get('name') || '',
      rules,
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(body),
    })
  })
}

// Stub the workload detail REST calls so the Score Impact tab renders
// without needing a real analyzer + kubectl client. The path pattern
// accepts any API group/version since the URL-level K8s discovery varies
// by kind (core/v1 for Pods, apps/v1 for Deployments, etc.).
export async function mockWorkloadDetail(page: Page, namespace: string, name: string, kind: string = 'pods') {
  const basePath = new RegExp(`/api/v1/k8s/ns/${namespace}/[^/]+/[^/]+/${kind}/${name}$`)
  await page.route(basePath, async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        metadata: { name, namespace, uid: 'test-uid', creationTimestamp: new Date().toISOString() },
        spec: { containers: [{ name: 'main', image: 'test:latest' }] },
        status: { phase: 'Running', conditions: [] },
      }),
    })
  })
  // Also catch common sub-routes the detail page may hit.
  const subPaths = new RegExp(`/api/v1/k8s/ns/${namespace}/[^/]+/[^/]+/${kind}/${name}/(events|yaml|pods)$`)
  await page.route(subPaths, async route => {
    const url = route.request().url()
    if (url.endsWith('/yaml')) {
      await route.fulfill({ status: 200, contentType: 'text/plain', body: `metadata:\n  name: ${name}\n` })
    } else {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: [] }) })
    }
  })
}

// ── Errors ───────────────────────────────────────────────────────────────────

export async function mockErrorGroups(page: Page, groups: any[] = []) {
  await page.route('**/api/v1/errors/groups**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ totalCount: groups.length, groups }),
    })
  })
  await page.route('**/api/v1/errors/summary**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        totalGroups: groups.length,
        totalOccurrences: groups.reduce((s: number, g: any) => s + (g.count || 0), 0),
        openCount: groups.filter((g: any) => g.status === 'open').length,
        byReason: {},
        byNamespace: {},
        topGroups: [],
        topServices: [],
      }),
    })
  })
  // Errors page fetches LLM config to decide whether to show "Analyze with AI".
  await page.route('**/api/v1/llm/config', async route => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          provider: 'ollama', model: 'llama3.2',
          endpoint: 'http://localhost:11434', apiKeySet: false,
          maxTokens: 4096, temperature: 0.3, dailyTokenBudget: 0,
          explainOptimizations: true,
        }),
      })
    } else {
      await route.continue()
    }
  })
}

// ── Incidents ────────────────────────────────────────────────────────────────

export async function mockIncidents(page: Page, incidents: any[] = []) {
  await page.route('**/api/v1/incidents**', async route => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ incidents }),
      })
    } else {
      await route.continue()
    }
  })
}

// ── Anomalies ────────────────────────────────────────────────────────────────

export async function mockAnomalies(page: Page, anomalies: any[] = []) {
  await page.route('**/api/v1/anomalies**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ totalCount: anomalies.length, anomalies }),
    })
  })
}

// ── Optimization ─────────────────────────────────────────────────────────────

export async function mockRecommendations(page: Page, recs: any[] = [], summary?: any) {
  // LIFO: register general route FIRST (checked last), summary LAST (checked first).
  // The general glob **/api/v1/recommendations** also matches /summary URLs, so it
  // must lose to the more specific summary handler.
  await page.route('**/api/v1/recommendations**', async route => {
    if (route.request().url().includes('/summary')) {
      // fallthrough to the summary handler registered after this one
      await route.fallback()
      return
    }
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ recommendations: recs, totalCount: recs.length }),
      })
    } else {
      // POST /run — return immediately
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ message: 'ok' }) })
    }
  })
  await page.route('**/api/v1/recommendations/summary**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(summary ?? {
        totalRecommendations: recs.length,
        openRecommendations: recs.length,
        totalSavingsMonthly: 0,
        byType: {},
        availableOptimizers: [],
      }),
    })
  })
}

// ── Security ─────────────────────────────────────────────────────────────────

export async function mockSecurityFindings(
  page: Page,
  findings: any[] = [],
  summary?: Partial<{ totalFindings: number; bySeverity: Record<string, number>; byCategory: Record<string, number> }>,
) {
  await page.route('**/api/v1/security/findings**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ totalCount: findings.length, findings }),
    })
  })
  await page.route('**/api/v1/security/summary**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        totalFindings: findings.length,
        bySeverity: { critical: 0, high: 0, medium: findings.length, low: 0 },
        byCategory: { cis: findings.length, rbac: 0, 'pod-security': 0 },
        ...summary,
      }),
    })
  })
}

// ── Settings ─────────────────────────────────────────────────────────────────

export async function mockSettings(
  page: Page,
  config?: Partial<{
    provider: string; endpoint: string; model: string
    maxTokens: number; temperature: number; dailyTokenBudget: number
    apiKeySet: boolean; explainOptimizations: boolean
  }>,
  providers?: { id: string; name: string; defaultEndpoint: string; defaultModel: string; requiresApiKey: boolean; description: string }[],
) {
  const resolved = {
    provider: 'ollama', endpoint: 'http://localhost:11434', model: 'llama3.2',
    maxTokens: 4096, temperature: 0.3, dailyTokenBudget: 0,
    apiKeySet: false, explainOptimizations: true,
    ...config,
  }
  await page.route('**/api/v1/llm/config**', async route => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(resolved) })
  })
  await page.route('**/api/v1/llm/providers**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        providers: providers ?? [
          { id: 'ollama', name: 'Ollama', defaultEndpoint: 'http://localhost:11434', defaultModel: 'llama3.2', requiresApiKey: false, description: 'Local Ollama instance' },
          { id: 'openai', name: 'OpenAI', defaultEndpoint: 'https://api.openai.com', defaultModel: 'gpt-4o', requiresApiKey: true, description: 'OpenAI API' },
        ],
      }),
    })
  })
  await page.route('**/api/v1/k8s/capabilities**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ exec: false, writeActions: false }),
    })
  })
}

// ── LB Logs ──────────────────────────────────────────────────────────────────

export async function mockLBList(page: Page, lbs: string[] = []) {
  await page.route('**/api/v1/lb/list**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ loadBalancers: lbs.map(name => ({ name, type: 'classic' })) }),
    })
  })
}

export async function mockLBData(page: Page, lbName: string) {
  const stats = {
    lbName, totalRequests: 1200, count2xx: 1100, count4xx: 80, count5xx: 20,
    p50Ms: 45, p95Ms: 120, p99Ms: 250, avgMs: 55,
  }
  const empty200 = (body: unknown) => ({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
  await page.route(`**/api/v1/lb/${lbName}/stats**`, r => r.fulfill(empty200(stats)))
  await page.route(`**/api/v1/lb/${lbName}/timeline**`, r => r.fulfill(empty200({ buckets: [] })))
  await page.route(`**/api/v1/lb/${lbName}/top-urls**`, r => r.fulfill(empty200({ urls: [] })))
  await page.route(`**/api/v1/lb/${lbName}/errors**`, r => r.fulfill(empty200({ entries: [] })))
  await page.route(`**/api/v1/lb/${lbName}/slow**`, r => r.fulfill(empty200({ entries: [] })))
  await page.route(`**/api/v1/lb/${lbName}/clients**`, r => r.fulfill(empty200({ clients: [] })))
  await page.route(`**/api/v1/lb/${lbName}/search**`, r => r.fulfill(empty200({ requests: [], total: 0 })))
  await page.route('**/api/v1/ingress**', r => r.fulfill(empty200({ totalCount: 0, ingresses: [] })))
}

// ── Incident detail ──────────────────────────────────────────────────────────

// mockIncidentDetail stubs GET /api/v1/incidents/{id} (and PATCH for status updates).
// Uses a regex anchor so it does NOT match sub-routes like /rca/stream.
// NOTE: if the incident has no rcaReport the page will open an SSE stream to
// /rca/stream — callers must stub that route separately or use an addInitScript
// EventSource stub (see incidents-detail.spec.ts SSE tests for the pattern).
export async function mockIncidentDetail(page: Page, id: number, incident: any) {
  const re = new RegExp(`/api/v1/incidents/${id}(?:\\?.*)?$`)
  await page.route(re, async route => {
    if (route.request().method() === 'GET') {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(incident) })
    } else if (route.request().method() === 'PATCH') {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(incident) })
    } else {
      await route.continue()
    }
  })
}

// mockStreamRCA stubs the SSE endpoint GET /api/v1/incidents/{id}/rca/stream.
// Delivers a single `complete` event carrying the provided report object.
export async function mockStreamRCA(page: Page, id: number, report: any) {
  const re = new RegExp(`/api/v1/incidents/${id}/rca/stream`)
  const body = `event: complete\ndata: ${JSON.stringify(report)}\n\n`
  await page.route(re, async route => {
    await route.fulfill({
      status: 200,
      contentType: 'text/event-stream',
      headers: { 'Cache-Control': 'no-cache', Connection: 'keep-alive' },
      body,
    })
  })
}

// mockAskAI stubs POST /api/v1/llm/ask and returns the given answer string.
export async function mockAskAI(page: Page, answer: string) {
  await page.route('**/api/v1/llm/ask', async route => {
    if (route.request().method() === 'POST') {
      await route.fulfill({
        status: 200, contentType: 'application/json',
        body: JSON.stringify({ answer }),
      })
    } else {
      await route.continue()
    }
  })
}

// ── Workload list ─────────────────────────────────────────────────────────────

export async function mockWorkloadList(page: Page, kind: string, items: any[] = []) {
  // Match namespaced:     /api/v1/k8s/ns/{ns}/{group}/{version}/{kind}
  // cluster-scoped:       /api/v1/k8s/{group}/{version}/{kind}
  // cluster/ prefixed:    /api/v1/k8s/cluster/{group}/{version}/{kind}
  await page.route(
    new RegExp(`/api/v1/k8s/(?:ns/[^/?]+/|cluster/)?[^/?]+/[^/?]+/${kind}(\\?|$)`),
    async route => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ kind, totalCount: items.length, items }),
      })
    },
  )
  await page.route('**/api/v1/k8s/namespaces**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ namespaces: ['default', 'kube-system'] }),
    })
  })
}

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

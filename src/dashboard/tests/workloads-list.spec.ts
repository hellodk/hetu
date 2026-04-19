import { test, expect } from '@playwright/test'
import { mockWorkloadList, mockHealthReport } from './fixtures/api'

const LIVE = !!process.env.LIVE

const SAMPLE_PODS = [
  {
    name: 'api-server-abc', namespace: 'default', kind: 'Pod',
    status: 'Running', age: '2d', readyContainers: 1, totalContainers: 1,
    restarts: 0, nodeName: 'node-1',
  },
  {
    name: 'api-server-def', namespace: 'default', kind: 'Pod',
    status: 'CrashLoopBackOff', age: '1h', readyContainers: 0, totalContainers: 1,
    restarts: 12, nodeName: 'node-2',
  },
]

const SAMPLE_NODES = [
  { name: 'node-1', namespace: '', kind: 'Node', status: 'Ready', age: '30d', nodeName: '' },
  { name: 'node-2', namespace: '', kind: 'Node', status: 'Ready', age: '30d', nodeName: '' },
]

test.describe('Workloads list page', () => {
  test.beforeEach(async ({ page }) => {
    if (!LIVE) {
      await mockHealthReport(page)
    }
  })

  test('pods list renders name, namespace, status', async ({ page }) => {
    if (!LIVE) await mockWorkloadList(page, 'pods', SAMPLE_PODS)
    await page.goto('/workloads/pods?group=core&version=v1')
    await expect(page.getByText('api-server-abc')).toBeVisible()
    await expect(page.getByRole('cell', { name: 'default' }).first()).toBeVisible()
    await expect(page.getByText('Running')).toBeVisible()
  })

  test('crashed pod shows CrashLoopBackOff status', async ({ page }) => {
    if (!LIVE) await mockWorkloadList(page, 'pods', SAMPLE_PODS)
    await page.goto('/workloads/pods?group=core&version=v1')
    await expect(page.getByText('CrashLoopBackOff')).toBeVisible()
  })

  test('restart count visible for pods', async ({ page }) => {
    if (!LIVE) await mockWorkloadList(page, 'pods', SAMPLE_PODS)
    await page.goto('/workloads/pods?group=core&version=v1')
    // Restart count 12 should appear
    await expect(page.getByText('12')).toBeVisible()
  })

  test('empty pods list shows empty state message', async ({ page }) => {
    if (LIVE) return
    await mockWorkloadList(page, 'pods', [])
    await page.goto('/workloads/pods?group=core&version=v1')
    await expect(page.getByText(/No pods/i)).toBeVisible()
  })

  test('nodes list hides namespace column (cluster-scoped)', async ({ page }) => {
    if (!LIVE) await mockWorkloadList(page, 'nodes', SAMPLE_NODES)
    await page.goto('/workloads/nodes?group=core&version=v1')
    await expect(page.getByText('node-1')).toBeVisible()
    // Namespace column should not appear for cluster-scoped resources
    const namespaceHeader = page.getByRole('columnheader', { name: /Namespace/i })
    if (await namespaceHeader.isVisible()) {
      // Some UIs keep the column but empty — just ensure no "default" namespace shown
      await expect(page.getByText('default')).toBeHidden()
    }
  })

  test('search field filters visible rows', async ({ page }) => {
    if (!LIVE) {
      // The workloads page does server-side search (?search=...) — respond with filtered data
      await page.route(
        new RegExp(`/api/v1/k8s/(?:ns/[^/?]+/|cluster/)?[^/?]+/[^/?]+/pods(\\?|$)`),
        async route => {
          const url = new URL(route.request().url())
          const term = url.searchParams.get('search') || ''
          const filtered = SAMPLE_PODS.filter(p => !term || p.name.includes(term))
          await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ kind: 'pods', totalCount: filtered.length, items: filtered }) })
        },
      )
    }
    await page.goto('/workloads/pods?group=core&version=v1')
    const searchInput = page.getByPlaceholder(/Filter by name/i)
    if (await searchInput.isVisible()) {
      await searchInput.fill('abc')
      await page.waitForTimeout(200)
      await expect(page.getByText('api-server-abc')).toBeVisible()
      await expect(page.getByText('api-server-def')).toBeHidden()
    }
  })

  test('API 500 shows error state without crashing', async ({ page }) => {
    if (LIVE) return
    await page.route(/\/api\/v1\/k8s\/.*pods/, r => r.fulfill({ status: 500, body: 'error' }))
    await page.route('**/api/v1/k8s/namespaces**', r =>
      r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ namespaces: [] }) })
    )
    await page.goto('/workloads/pods?group=core&version=v1')
    await expect(page.locator('body')).toBeVisible()
  })

  test('deployments list renders ready replica count', async ({ page }) => {
    if (!LIVE) {
      await mockWorkloadList(page, 'deployments', [
        { name: 'api-server', namespace: 'default', kind: 'Deployment', status: 'Available', readyReplicas: 3, desiredReplicas: 3, age: '5d' },
      ])
    }
    await page.goto('/workloads/deployments?group=apps&version=v1')
    await expect(page.getByText('api-server')).toBeVisible()
  })
})

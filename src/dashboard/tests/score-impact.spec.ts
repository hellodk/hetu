import { test, expect } from '@playwright/test'
import { mockHealthReport, mockResourceImpact, mockWorkloadDetail } from './fixtures/api'

test.describe('Score Impact tab (Level-4 drill-down)', () => {
  test.beforeEach(async ({ page }) => {
    await mockHealthReport(page)
  })

  test('tab is rendered on workload detail page', async ({ page }) => {
    await mockWorkloadDetail(page, 'default', 'my-pod', 'pods')
    await mockResourceImpact(page, [])
    await page.goto('/workloads/pods/default/my-pod')
    await expect(page.getByRole('tab', { name: /Score Impact/i })).toBeVisible()
  })

  test('renders empty state when no rules', async ({ page }) => {
    await mockWorkloadDetail(page, 'default', 'my-pod', 'pods')
    await mockResourceImpact(page, [])
    await page.goto('/workloads/pods/default/my-pod')
    await page.getByRole('tab', { name: /Score Impact/i }).click()
    await expect(page.getByText(/not contributing to any scoring deductions/i)).toBeVisible()
  })

  test('renders one card per rule', async ({ page }) => {
    await mockWorkloadDetail(page, 'default', 'my-pod', 'pods')
    await mockResourceImpact(page, [
      { dimension: 'reliability', rule: 'CrashLoopBackOff pods', impact: -5, remediation: 'Check container logs' },
      { dimension: 'security', rule: 'Privileged containers', impact: -15, remediation: 'Remove privileged' },
      { dimension: 'cost', rule: 'Rightsizing opportunities', impact: -2, remediation: 'Reduce requests' },
    ])
    await page.goto('/workloads/pods/default/my-pod')
    await page.getByRole('tab', { name: /Score Impact/i }).click()
    await expect(page.getByText('CrashLoopBackOff pods')).toBeVisible()
    await expect(page.getByText('Privileged containers')).toBeVisible()
    await expect(page.getByText('Rightsizing opportunities')).toBeVisible()
    await expect(page.getByText('Check container logs')).toBeVisible()
    await expect(page.getByText('Remove privileged')).toBeVisible()
  })

  test('sends kind=Pod for /workloads/pods/... URL', async ({ page }) => {
    await mockWorkloadDetail(page, 'default', 'my-pod', 'pods')

    const fetchPromise = page.waitForRequest(req =>
      req.url().includes('/api/v1/health/resource-impact')
    )
    await mockResourceImpact(page, [])

    await page.goto('/workloads/pods/default/my-pod')
    await page.getByRole('tab', { name: /Score Impact/i }).click()

    const req = await fetchPromise
    const url = new URL(req.url())
    expect(url.searchParams.get('kind')).toBe('Pod')
  })

  test('sends kind=Ingress for /workloads/ingresses/... URL (irregular plural)', async ({ page }) => {
    await mockWorkloadDetail(page, 'default', 'my-ing', 'ingresses')

    const fetchPromise = page.waitForRequest(req =>
      req.url().includes('/api/v1/health/resource-impact')
    )
    await mockResourceImpact(page, [])

    // Ingresses live under the networking.k8s.io group, but for URL
    // purposes the dashboard only cares about the plural segment.
    await page.goto('/workloads/ingresses/default/my-ing?group=networking.k8s.io&version=v1')
    await page.getByRole('tab', { name: /Score Impact/i }).click()

    const req = await fetchPromise
    const url = new URL(req.url())
    expect(url.searchParams.get('kind')).toBe('Ingress')
  })

  test('sends kind=ReplicaSet for /workloads/replicasets/... URL', async ({ page }) => {
    await mockWorkloadDetail(page, 'default', 'my-rs', 'replicasets')

    const fetchPromise = page.waitForRequest(req =>
      req.url().includes('/api/v1/health/resource-impact')
    )
    await mockResourceImpact(page, [])

    await page.goto('/workloads/replicasets/default/my-rs?group=apps&version=v1')
    await page.getByRole('tab', { name: /Score Impact/i }).click()

    const req = await fetchPromise
    const url = new URL(req.url())
    expect(url.searchParams.get('kind')).toBe('ReplicaSet')
  })
})

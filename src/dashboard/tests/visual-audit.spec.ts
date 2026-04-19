import { test, expect } from '@playwright/test'
import {
  mockHealthReport,
  mockIncidents,
  mockIncidentDetail,
  mockAnomalies,
  mockRecommendations,
  mockSecurityFindings,
  mockSettings,
  mockWorkloadList,
  mockErrorGroups,
  mockLBList,
} from './fixtures/api'

const NOW = new Date().toISOString()

const SAMPLE_INCIDENT = {
  id: 1, severity: 'critical', status: 'open',
  detectedAt: NOW,
  affected: ['api-server'],
  summary: '1 signals: 1 exception affecting api-server',
  signals: [{
    timestamp: NOW, source: 'logs', severity: 'critical',
    service: 'api-server', namespace: 'default', pod: 'api-server-xyz',
    kind: 'exception', title: 'NullPointerException in handler', details: '',
  }],
  rcaReport: {
    summary: 'Memory pressure caused OOMKill',
    rootCause: { primary: 'Memory limit exceeded', confidence: 0.9, description: 'Container hit 256Mi limit' },
    contributingFactors: [], blastRadius: { services: [], users: '~10', severity: 'high' },
    remediation: [], preventiveMeasures: [], evidence: [],
    model: 'llama3.2', promptTokens: 100, outputTokens: 50, createdAt: NOW,
  },
}

test.describe('Visual audit — all pages render without crash', () => {
  test('dashboard overview', async ({ page }) => {
    await mockHealthReport(page)
    await page.goto('/')
    await expect(page.locator('body')).toBeVisible()
    await expect(page.locator('nav')).toBeVisible()
    await page.screenshot({ path: 'test-results/visual-audit/overview.png', fullPage: true })
  })

  test('incidents list', async ({ page }) => {
    await mockHealthReport(page)
    await mockIncidents(page, [SAMPLE_INCIDENT])
    await page.goto('/incidents')
    await expect(page.locator('body')).toBeVisible()
    await expect(page.getByText('INC-1').first()).toBeVisible()
    await page.screenshot({ path: 'test-results/visual-audit/incidents.png', fullPage: true })
  })

  test('incident detail', async ({ page }) => {
    await mockHealthReport(page)
    await mockIncidentDetail(page, 1, SAMPLE_INCIDENT)
    await page.goto('/incidents/1')
    await expect(page.getByRole('heading', { name: /INC-1/ })).toBeVisible()
    await page.screenshot({ path: 'test-results/visual-audit/incident-detail.png', fullPage: true })
  })

  test('security page', async ({ page }) => {
    await mockHealthReport(page)
    await mockSecurityFindings(page, [
      { id: 'f1', severity: 'high', category: 'cis', rule: 'no-privileged', resource: 'pod/nginx', namespace: 'default', description: 'Privileged container', status: 'open', detectedAt: NOW },
    ])
    await page.goto('/security')
    await expect(page.locator('body')).toBeVisible()
    await page.screenshot({ path: 'test-results/visual-audit/security.png', fullPage: true })
  })

  test('settings page', async ({ page }) => {
    await mockHealthReport(page)
    await mockSettings(page)
    await page.goto('/settings')
    await expect(page.locator('body')).toBeVisible()
    await page.screenshot({ path: 'test-results/visual-audit/settings.png', fullPage: true })
  })

  test('workloads (pods) list', async ({ page }) => {
    await mockHealthReport(page)
    await mockWorkloadList(page, 'pods', [
      { metadata: { name: 'nginx-abc', namespace: 'default', creationTimestamp: NOW }, status: { phase: 'Running', containerStatuses: [{ ready: true, restartCount: 0 }] } },
    ])
    await page.goto('/workloads/pods?group=core&version=v1')
    await expect(page.locator('body')).toBeVisible()
    await page.screenshot({ path: 'test-results/visual-audit/pods.png', fullPage: true })
  })

  test('anomalies page', async ({ page }) => {
    await mockHealthReport(page)
    await mockAnomalies(page, [
      { id: 'a1', kind: 'cpu-spike', severity: 'high', service: 'api', namespace: 'default', detectedAt: NOW, description: 'CPU spike detected', score: 3.2, expected: 20.0, observed: 95.0 },
    ])
    await page.goto('/anomalies')
    await expect(page.locator('body')).toBeVisible()
    await page.screenshot({ path: 'test-results/visual-audit/anomalies.png', fullPage: true })
  })

  test('optimization page', async ({ page }) => {
    await mockHealthReport(page)
    await mockRecommendations(page, [
      { id: 'r1', type: 'rightsizing', severity: 'medium', confidence: 0.85, target: { kind: 'pod', namespace: 'default', name: 'nginx' }, title: 'Reduce CPU request', description: 'CPU usage is 5% of request', estimatedSavingsMonthly: 12.5, status: 'open', detectedAt: NOW },
    ])
    await page.goto('/optimization')
    await expect(page.locator('body')).toBeVisible()
    await page.screenshot({ path: 'test-results/visual-audit/optimization.png', fullPage: true })
  })

  test('errors page', async ({ page }) => {
    await mockHealthReport(page)
    await mockErrorGroups(page, [
      { id: 'e1', title: 'OOMKilled', reason: 'OOMKilled', service: 'api', namespace: 'default', count: 5, severity: 'critical', level: 'critical', status: 'open', lastSeen: NOW, firstSeen: NOW, pods: ['api-xyz'] },
    ])
    await page.goto('/errors')
    await expect(page.locator('body')).toBeVisible()
    await page.screenshot({ path: 'test-results/visual-audit/errors.png', fullPage: true })
  })

  test('lb-logs page', async ({ page }) => {
    await mockHealthReport(page)
    await mockLBList(page, ['prod-lb'])
    await page.goto('/lb-logs')
    await expect(page.locator('body')).toBeVisible()
    await page.screenshot({ path: 'test-results/visual-audit/lb-logs.png', fullPage: true })
  })
})

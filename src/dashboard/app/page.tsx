'use client'
import { useState, useEffect, useCallback, useRef } from 'react'
import packageJson from '../package.json'
import {
  Activity, Shield, DollarSign, Boxes,
  AlertTriangle, CheckCircle, Clock, TrendingUp,
  Server, Cpu, HardDrive, Network,
  ChevronRight, RefreshCw, Settings, Bell, Download,
  Check, X, Info
} from 'lucide-react'
import { ScoreCard } from '@/components/ScoreCard'
import { IssuesList } from '@/components/IssuesList'
import { RecommendationsList } from '@/components/RecommendationsList'
import { ResourceUtilization } from '@/components/ResourceUtilization'
import { TimelineChart } from '@/components/TimelineChart'
import { ClusterSummary } from '@/components/ClusterSummary'
import { AIInsightFeed } from '@/components/AIInsightFeed'
import { CoreDNSHealth } from '@/components/CoreDNSHealth'
import { SettingsModal } from '@/components/SettingsModal'
import { NamespacesTable, NamespaceStats } from '@/components/NamespacesTable'
import { DiagnosticPanel } from '@/components/DiagnosticPanel'
import { ProfileBadge } from '@/components/ProfileBadge'
import { ScoreBreakdown } from '@/components/ScoreBreakdown'
import { MockWatermark } from '@/components/MockWatermark'

// Types
interface HealthScores {
  overall: number
  reliability: number
  security: number
  cost: number
  architecture: number
}

interface ClusterSummaryData {
  totalNodes: number
  totalPods: number
  totalNamespaces: number
  healthyPods: number
  unhealthyPods: number
  pendingPods: number
  warningEvents: number
  criticalEvents: number
  namespaces?: Record<string, NamespaceStats>
}

interface Issue {
  id: string
  severity: 'critical' | 'high' | 'medium' | 'low'
  category: string
  title: string
  description: string
  affectedResources: string[]
  confidence: number
  rootCause?: string
}

interface Recommendation {
  id: string
  category: string
  title: string
  description: string
  severity: string
  confidence: number
  impact: {
    costSavings?: { monthly: number; currency: string }
    riskLevel: string
    effort: string
  }
  aiReasoning: string
}

interface HealthTrends {
  overall: number
  reliability: number
  security: number
  cost: number
  architecture: number
}

interface ResourceUtilizationData {
  cpu: { used: number, requested: number, capacity: number, unit: string }
  memory: { used: number, requested: number, capacity: number, unit: string }
  storage: { used: number, requested: number, capacity: number, unit: string }
}

// ComponentHealth mirrors the Go type — describes reachability of an
// upstream dependency (collector, LLM).
interface ComponentHealth {
  reachable: boolean
  endpoint?: string
  lastOkAt?: string
  lastError?: string
}

// ReportStatus describes the current state of the analyzer. Always
// populated on a report; the dashboard renders a diagnostic panel when
// `scores` is null, using the fields in this block to explain why.
interface ReportStatus {
  state: 'ok' | 'awaiting' | 'degraded' | 'error'
  message: string
  profile: 'live' | 'mock'
  collector: ComponentHealth
  llm: ComponentHealth
  lastAnalysisAt?: string
  lastAnalysisError?: string
}

interface HealthReport {
  clusterId: string
  timestamp: string
  // Nullable: when the analyzer has no LLM-derived scores (degraded /
  // awaiting / error states), scores is null and the dashboard must render
  // a diagnostic panel instead of score cards. No fabricated defaults.
  scores: HealthScores | null
  summary: ClusterSummaryData
  topIssues: Issue[]
  recommendations: Recommendation[]
  estimatedMonthlySavings: number
  trends: HealthTrends
  resourceUtilization: ResourceUtilizationData
  status?: ReportStatus
}

// Toast notification type
interface Toast {
  id: string
  type: 'success' | 'error' | 'info'
  message: string
}

// API URL: read from runtime config injected by server layout, or fall back to build-time env
const API_URL = typeof window !== 'undefined'
  ? ((window as any).__CLUSTER_INTEL_API__ || '')
  : (process.env.NEXT_PUBLIC_API_URL || '')

// Tab configuration
const TABS = [
  { id: 'overview', label: 'Overview' },
  { id: 'issues', label: 'Issues' },
  { id: 'recommendations', label: 'Recommendations' },
  { id: 'timeline', label: 'Timeline' },
  { id: 'namespaces', label: 'Namespaces' }
] as const

type TabId = typeof TABS[number]['id']

// Toast Component
function ToastContainer({ toasts, onDismiss }: { toasts: Toast[], onDismiss: (id: string) => void }) {
  if (toasts.length === 0) return null

  const icons = {
    success: <Check className="w-5 h-5" />,
    error: <X className="w-5 h-5" />,
    info: <Info className="w-5 h-5" />
  }

  return (
    <div className="toast-container" role="status" aria-live="polite">
      {toasts.map(toast => (
        <div key={toast.id} className={`toast toast-${toast.type}`}>
          {icons[toast.type]}
          <span className="flex-1">{toast.message}</span>
          <button
            onClick={() => onDismiss(toast.id)}
            className="p-1 hover:bg-white/10 rounded"
            aria-label="Dismiss notification"
          >
            <X className="w-4 h-4" />
          </button>
        </div>
      ))}
    </div>
  )
}

// Skeleton Loading Component
function DashboardSkeleton() {
  return (
    <div className="min-h-screen" aria-busy="true" aria-label="Loading dashboard">
      {/* Header Skeleton */}
      <header className="border-b border-cluster-border bg-cluster-card/50 backdrop-blur-sm sticky top-0 z-50">
        <div className="max-w-[1800px] mx-auto px-4 sm:px-6 py-4">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-4">
              <div className="skeleton-circle w-8 h-8" />
              <div className="skeleton h-6 w-48" />
            </div>
            <div className="flex items-center gap-4">
              <div className="skeleton h-4 w-32 hidden sm:block" />
              <div className="skeleton-circle w-9 h-9" />
              <div className="skeleton-circle w-9 h-9" />
            </div>
          </div>
        </div>
      </header>

      <main className="max-w-[1800px] mx-auto px-4 sm:px-6 py-6">
        {/* Score Cards Skeleton */}
        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-3 sm:gap-4 mb-6">
          {[1, 2, 3, 4, 5].map(i => (
            <div key={i} className="bg-cluster-card rounded-xl border border-cluster-border p-4">
              <div className="flex items-start justify-between mb-3">
                <div className="skeleton-circle w-10 h-10" />
                <div className="skeleton h-5 w-12" />
              </div>
              <div className="flex items-center gap-4">
                <div className="skeleton-circle w-20 h-20" />
                <div className="flex-1 space-y-2">
                  <div className="skeleton h-4 w-full" />
                  <div className="skeleton h-3 w-2/3" />
                </div>
              </div>
            </div>
          ))}
        </div>

        {/* Tabs Skeleton */}
        <div className="flex gap-2 mb-6 border-b border-cluster-border">
          {[1, 2, 3, 4].map(i => (
            <div key={i} className="skeleton h-10 w-24" />
          ))}
        </div>

        {/* Content Skeleton */}
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
          <div className="lg:col-span-2 space-y-6">
            <div className="bg-cluster-card rounded-xl border border-cluster-border p-6">
              <div className="skeleton h-6 w-48 mb-4" />
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                {[1, 2, 3, 4].map(i => (
                  <div key={i} className="skeleton h-24" />
                ))}
              </div>
            </div>
            <div className="bg-cluster-card rounded-xl border border-cluster-border p-6">
              <div className="skeleton h-6 w-48 mb-4" />
              <div className="space-y-4">
                {[1, 2, 3].map(i => (
                  <div key={i} className="skeleton h-16" />
                ))}
              </div>
            </div>
          </div>
          <div className="bg-cluster-card rounded-xl border border-cluster-border p-6">
            <div className="skeleton h-6 w-32 mb-4" />
            <div className="space-y-3">
              {[1, 2, 3, 4].map(i => (
                <div key={i} className="skeleton h-20" />
              ))}
            </div>
          </div>
        </div>
      </main>
    </div>
  )
}

export default function Dashboard() {
  const [report, setReport] = useState<HealthReport | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null)
  const [showSettings, setShowSettings] = useState(false)
  const [breakdownExpanded, setBreakdownExpanded] = useState(false)
  const [focusDimension, setFocusDimension] = useState<string | null>(null)
  const [activeTab, _setActiveTab] = useState<TabId>('overview')
  const [toasts, setToasts] = useState<Toast[]>([])

  // Tab routing
  useEffect(() => {
    const handleHashChange = () => {
      const hash = window.location.hash.replace('#', '') as TabId
      if (['overview', 'issues', 'recommendations', 'timeline'].includes(hash)) {
        _setActiveTab(hash)
      }
    }
    if (window.location.hash) handleHashChange()
    window.addEventListener('hashchange', handleHashChange)
    return () => window.removeEventListener('hashchange', handleHashChange)
  }, [])

  const setActiveTab = useCallback((tab: TabId) => {
    _setActiveTab(tab)
    window.history.pushState(null, '', `#${tab}`)
  }, [])

  // Refs for accessibility
  const tabListRef = useRef<HTMLDivElement>(null)
  const mainContentRef = useRef<HTMLDivElement>(null)

  // Toast helpers
  const addToast = useCallback((type: Toast['type'], message: string) => {
    const id = Date.now().toString()
    setToasts(prev => [...prev, { id, type, message }])
    setTimeout(() => {
      setToasts(prev => prev.filter(t => t.id !== id))
    }, 5000)
  }, [])

  const dismissToast = useCallback((id: string) => {
    setToasts(prev => prev.filter(t => t.id !== id))
  }, [])

  // Normalize report to ensure array fields are never null (Go marshals
  // empty slices as null) AND to guarantee a status block always exists,
  // since the dashboard uses status.state/status.profile to decide what to
  // render when scores are absent.
  const normalizeReport = (data: any): HealthReport => ({
    ...data,
    // scores stays as-is, including null — do NOT fabricate defaults here.
    scores: data?.scores ?? null,
    topIssues: data?.topIssues ?? [],
    recommendations: data?.recommendations ?? [],
    summary: {
      ...data?.summary,
      namespaces: data?.summary?.namespaces ?? {},
    },
    status: data?.status ?? {
      state: data?.scores ? 'ok' : 'awaiting',
      message: data?.scores
        ? 'Live analysis is up to date.'
        : 'Awaiting first cluster analysis.',
      profile: 'live',
      collector: { reachable: false },
      llm: { reachable: false },
    },
  })

  // Fetch health report
  const fetchReport = useCallback(async (showToast = false) => {
    try {
      const response = await fetch(`${API_URL}/api/v1/health`)
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`)
      }
      const data = await response.json()
      setReport(normalizeReport(data))
      setLastUpdated(new Date())
      setError(null)
      if (showToast) {
        addToast('success', 'Dashboard data refreshed successfully')
      }
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : 'Failed to fetch data'
      setError(errorMessage)
      if (showToast) {
        addToast('error', `Failed to refresh: ${errorMessage}`)
      }
    } finally {
      setLoading(false)
    }
  }, [addToast])

  // Initial fetch and SSE
  useEffect(() => {
    fetchReport()

    const sse = new EventSource(`${API_URL}/api/v1/health/stream`)

    sse.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data)
        setReport(normalizeReport(data))
        setLastUpdated(new Date())
        setError(null)
      } catch (err) {
        console.error("SSE parse error", err)
      }
    }

    sse.onerror = (err) => {
      console.error("SSE connection error", err)
    }

    return () => {
      sse.close()
    }
  }, [fetchReport])

  // Persistent “fix your config” reminder every 10 minutes while broken.
  useEffect(() => {
    const interval = setInterval(() => {
      const s = report?.status
      if (!s) return
      const isLive = (s.profile ?? 'live') === 'live'
      if (!isLive) return
      const collectorMissing = !s.collector?.endpoint || String(s.collector.endpoint).trim() === ''
      const collectorDown = s.collector?.reachable === false
      const llmDown = s.llm?.reachable === false
      if (collectorMissing || collectorDown) {
        addToast('error', 'Live mode is blocked: Collector is misconfigured or unreachable. Open Settings to set COLLECTOR_URL.')
      } else if (llmDown) {
        addToast('info', 'LLM is unreachable: telemetry is flowing but AI insights are unavailable. Check LLM settings.')
      }
    }, 10 * 60 * 1000)
    return () => clearInterval(interval)
  }, [report, addToast])

  // Trigger manual refresh
  const handleRefresh = useCallback(() => {
    setLoading(true)
    fetchReport(true)
  }, [fetchReport])

  // Switch the analyzer profile (live | mock) via POST /api/v1/profile.
  // Only invoked from the Settings modal — the dashboard itself no longer
  // exposes a "Switch to demo" button (that convenience made demo-vs-live
  // confusion too easy; the toggle now lives in one canonical place).
  const handleSwitchProfile = useCallback(
    async (newProfile: 'live' | 'mock') => {
      try {
        const resp = await fetch(`${API_URL}/api/v1/profile`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ profile: newProfile }),
        })
        if (!resp.ok) {
          throw new Error(`HTTP ${resp.status}`)
        }
        addToast(
          'success',
          newProfile === 'mock'
            ? 'Demo mode enabled — synthetic data will appear shortly'
            : 'Live mode enabled — waiting for real analysis'
        )
        // Fetch immediately so the UI reflects the change without waiting for
        // the SSE stream to catch up.
        fetchReport()
      } catch (err) {
        const msg = err instanceof Error ? err.message : 'Unknown error'
        addToast('error', `Failed to switch profile: ${msg}`)
      }
    },
    [addToast, fetchReport]
  )

  // Update COLLECTOR_URL at runtime (live profile dependency).
  const handleSetCollectorUrl = useCallback(
    async (collectorUrl: string) => {
      try {
        const resp = await fetch(`${API_URL}/api/v1/collector`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ collectorUrl }),
        })
        if (!resp.ok) {
          throw new Error(`HTTP ${resp.status}`)
        }
        addToast('success', collectorUrl ? 'Collector URL saved' : 'Collector URL cleared')
        fetchReport()
      } catch (err) {
        const msg = err instanceof Error ? err.message : 'Unknown error'
        addToast('error', `Failed to save Collector URL: ${msg}`)
        throw err
      }
    },
    [addToast, fetchReport]
  )

  // Drill into a specific score dimension — expands the breakdown panel
  // and scrolls the target dimension card into view.
  const drillIntoDimension = useCallback((dimension: string) => {
    setBreakdownExpanded(true)
    setFocusDimension(dimension)
    // Scroll to the card after a tick so the DOM has rendered
    setTimeout(() => {
      document.getElementById(`breakdown-${dimension}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' })
      // Clear focus highlight after 3 seconds
      setTimeout(() => setFocusDimension(null), 3000)
    }, 100)
  }, [])

  // Keyboard navigation for tabs
  const handleTabKeyDown = useCallback((e: React.KeyboardEvent, currentIndex: number) => {
    const tabCount = TABS.length
    let newIndex = currentIndex

    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') {
      e.preventDefault()
      newIndex = (currentIndex + 1) % tabCount
    } else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
      e.preventDefault()
      newIndex = (currentIndex - 1 + tabCount) % tabCount
    } else if (e.key === 'Home') {
      e.preventDefault()
      newIndex = 0
    } else if (e.key === 'End') {
      e.preventDefault()
      newIndex = tabCount - 1
    }

    if (newIndex !== currentIndex) {
      setActiveTab(TABS[newIndex].id)
      // Focus the new tab
      const tabList = tabListRef.current
      if (tabList) {
        const buttons = tabList.querySelectorAll('[role="tab"]')
          ; (buttons[newIndex] as HTMLElement)?.focus()
      }
    }
  }, [setActiveTab])

  // Show skeleton during initial load
  if (loading && !report) {
    return <DashboardSkeleton />
  }

  // Error state
  if (error && !report) {
    return (
      <div className="min-h-screen flex items-center justify-center px-4" role="alert">
        <div className="text-center max-w-md">
          <div className="w-16 h-16 bg-red-500/10 rounded-full flex items-center justify-center mx-auto mb-4">
            <AlertTriangle className="w-8 h-8 text-red-500" aria-hidden="true" />
          </div>
          <h1 className="text-xl font-semibold text-red-400 mb-2">Failed to load cluster data</h1>
          <p className="text-slate-400 text-sm mb-6">{error}</p>
          <button
            onClick={handleRefresh}
            className="btn-primary"
          >
            <RefreshCw className="w-4 h-4 mr-2 inline" aria-hidden="true" />
            Try Again
          </button>
        </div>
      </div>
    )
  }

  // Show loading state if no report yet
  if (!report) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="text-center">
          <RefreshCw className="w-8 h-8 animate-spin text-blue-400 mx-auto mb-4" />
          <p className="text-gray-400">Connecting to analyzer...</p>
          <p className="text-gray-500 text-sm mt-2">Waiting for first health report from the cluster</p>
        </div>
      </div>
    )
  }

  const displayReport: HealthReport = report

  const criticalIssueCount = displayReport.topIssues.filter(i => i.severity === 'critical').length
  const isMockProfile = (displayReport.status?.profile ?? 'live') === 'mock'

  // Get tab label with count
  const getTabLabel = (tabId: TabId) => {
    switch (tabId) {
      case 'issues':
        return `Issues (${displayReport.topIssues.length})`
      case 'recommendations':
        return `Recommendations (${displayReport.recommendations.length})`
      default:
        return TABS.find(t => t.id === tabId)?.label || tabId
    }
  }

  return (
    <div className="min-h-screen flex flex-col">
      {isMockProfile && <MockWatermark />}
      {/* Header */}
      <header className="border-b border-cluster-border bg-cluster-card/50 backdrop-blur-sm sticky top-0 z-50">
        <div className="max-w-[1800px] mx-auto px-4 sm:px-6 py-3 sm:py-4">
          <div className="flex items-center justify-between gap-4">
            {/* Logo and Title */}
            <div className="flex items-center gap-2 sm:gap-4 min-w-0">
              <div className="flex items-center gap-2 flex-shrink-0">
                <Activity className="w-6 h-6 sm:w-8 sm:h-8 text-blue-500" aria-hidden="true" />
                <h1 className="text-lg sm:text-xl font-bold truncate">
                  <span className="hidden sm:inline">K8s Cluster Intelligence</span>
                  <span className="sm:hidden">K8s Health</span>
                </h1>
              </div>
              <span
                className="hidden sm:inline-flex items-center px-3 py-1 bg-blue-600/20 text-blue-200 border border-blue-500/30 text-sm rounded-full"
                title="Cluster ID"
                aria-label={`Cluster: ${displayReport.clusterId}`}
              >
                {displayReport.clusterId}
              </span>
              {/* Profile badge — tells the operator at a glance whether this
                  dashboard is showing real telemetry or synthetic demo data. */}
              <ProfileBadge profile={displayReport.status?.profile ?? 'live'} />
            </div>

            {/* Header Actions */}
            <div className="flex items-center gap-2 sm:gap-3 flex-shrink-0">
              {/* Last Updated - Hidden on mobile */}
              {lastUpdated && (
                <span className="hidden md:flex items-center gap-1.5 text-slate-400 text-sm">
                  <Clock className="w-4 h-4" aria-hidden="true" />
                  <span>Updated {lastUpdated.toLocaleTimeString()}</span>
                </span>
              )}

              {/* Action Buttons Group */}
              <div className="flex items-center gap-1 sm:gap-2 p-1 bg-cluster-border/50 rounded-lg" role="group" aria-label="Dashboard actions">
                <button
                  onClick={handleRefresh}
                  className="btn-icon tooltip-trigger"
                  disabled={loading}
                  aria-label={loading ? 'Refreshing data...' : 'Refresh dashboard data'}
                >
                  <RefreshCw className={`w-5 h-5 ${loading ? 'animate-spin' : ''}`} aria-hidden="true" />
                  <span className="tooltip">Refresh</span>
                </button>

                <button
                  onClick={() => {
                    const blob = new Blob([JSON.stringify(displayReport, null, 2)], { type: 'application/json' })
                    const url = URL.createObjectURL(blob)
                    const a = document.createElement('a')
                    a.href = url
                    a.download = `k8s-health-report-${displayReport.clusterId}-${new Date().toISOString()}.json`
                    a.click()
                    addToast('success', 'Exported health report to JSON')
                  }}
                  className="btn-icon tooltip-trigger"
                  aria-label="Export JSON Report"
                >
                  <Download className="w-5 h-5" aria-hidden="true" />
                  <span className="tooltip">Export Report</span>
                </button>

                <button
                  className="btn-icon tooltip-trigger relative"
                  onClick={() => setActiveTab('issues')}
                  aria-label={`Notifications${criticalIssueCount > 0 ? `, ${criticalIssueCount} critical` : ''}`}
                >
                  <Bell className="w-5 h-5" aria-hidden="true" />
                  {criticalIssueCount > 0 && (
                    <span className="badge-notification" aria-hidden="true">
                      {criticalIssueCount}
                    </span>
                  )}
                  <span className="tooltip">Notifications</span>
                </button>

                <button
                  className="btn-icon tooltip-trigger"
                  onClick={() => setShowSettings(true)}
                  aria-label="Settings"
                >
                  <Settings className="w-5 h-5" aria-hidden="true" />
                  <span className="tooltip">Settings</span>
                </button>
              </div>
            </div>
          </div>
        </div>
      </header>

      {/* Main Content */}
      <main id="main-content" className="flex-1 max-w-[1800px] w-full mx-auto px-4 sm:px-6 py-4 sm:py-6" ref={mainContentRef}>
        {/*
          Score cards OR diagnostic panel.

          When displayReport.scores is null, we MUST NOT invent numbers —
          instead we show the operator exactly why scores aren't available
          (collector down / LLM down / awaiting) with actionable buttons.
          Cluster summary, resource utilization, and the issue/recommendation
          tabs below still render from real telemetry when available.
        */}
        {displayReport.scores ? (
          <section aria-labelledby="scores-heading">
            <h2 id="scores-heading" className="sr-only">Health Scores</h2>
            <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-3 sm:gap-4 mb-6">
              <ScoreCard
                title="Overall Health"
                score={displayReport.scores.overall}
                icon={<Activity className="w-5 h-5 sm:w-6 sm:h-6" aria-hidden="true" />}
                color="blue"
                trend={displayReport.trends?.overall}
                onClick={() => setBreakdownExpanded(!breakdownExpanded)}
              />
              <ScoreCard
                title="Reliability"
                score={displayReport.scores.reliability}
                icon={<CheckCircle className="w-5 h-5 sm:w-6 sm:h-6" aria-hidden="true" />}
                color="green"
                trend={displayReport.trends?.reliability}
                onClick={() => drillIntoDimension('reliability')}
              />
              <ScoreCard
                title="Security"
                score={displayReport.scores.security}
                icon={<Shield className="w-5 h-5 sm:w-6 sm:h-6" aria-hidden="true" />}
                color="purple"
                trend={displayReport.trends?.security}
                onClick={() => drillIntoDimension('security')}
              />
              <ScoreCard
                title="Cost Efficiency"
                score={displayReport.scores.cost}
                icon={<DollarSign className="w-5 h-5 sm:w-6 sm:h-6" aria-hidden="true" />}
                color="emerald"
                trend={displayReport.trends?.cost}
                subtitle={displayReport.estimatedMonthlySavings > 0 ? `$${displayReport.estimatedMonthlySavings.toLocaleString()}/mo savings` : undefined}
                onClick={() => drillIntoDimension('cost')}
              />
              <ScoreCard
                title="Architecture"
                score={displayReport.scores.architecture}
                icon={<Boxes className="w-5 h-5 sm:w-6 sm:h-6" aria-hidden="true" />}
                color="amber"
                trend={displayReport.trends?.architecture}
                onClick={() => drillIntoDimension('architecture')}
              />
            </div>
          </section>
        ) : (
          <DiagnosticPanel
            status={displayReport.status}
            onRetry={handleRefresh}
          />
        )}

        {/* Score breakdown drill-down — shows what contributes to each score */}
        {displayReport.scores && (
          <ScoreBreakdown
            expanded={breakdownExpanded}
            onToggle={() => setBreakdownExpanded(!breakdownExpanded)}
            focusDimension={focusDimension}
          />
        )}

        {/* Navigation Tabs - Accessible */}
        <div
          ref={tabListRef}
          role="tablist"
          aria-label="Dashboard sections"
          className="tab-list mb-6 -mx-4 sm:mx-0 px-4 sm:px-0"
        >
          {TABS.map((tab, index) => (
            <button
              key={tab.id}
              role="tab"
              id={`tab-${tab.id}`}
              aria-selected={activeTab === tab.id}
              aria-controls={`tabpanel-${tab.id}`}
              tabIndex={activeTab === tab.id ? 0 : -1}
              onClick={() => setActiveTab(tab.id)}
              onKeyDown={(e) => handleTabKeyDown(e, index)}
              className="tab-item whitespace-nowrap"
            >
              {getTabLabel(tab.id)}
            </button>
          ))}
        </div>

        {/* Main Content Area */}
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-4 sm:gap-6">
          {/* Left Column - Main Content */}
          <div
            className="lg:col-span-2 space-y-4 sm:space-y-6"
            role="tabpanel"
            id={`tabpanel-${activeTab}`}
            aria-labelledby={`tab-${activeTab}`}
          >
            {activeTab === 'overview' && (
              <>
                <ClusterSummary summary={displayReport.summary} />
                <ResourceUtilization resources={displayReport.resourceUtilization} />
                <CoreDNSHealth />
                <IssuesList
                  issues={displayReport.topIssues.slice(0, 3)}
                  onViewAll={() => setActiveTab('issues')}
                />
              </>
            )}

            {activeTab === 'issues' && (
              <IssuesList
                issues={displayReport.topIssues}
                expanded
                onToast={addToast}
              />
            )}

            {activeTab === 'recommendations' && (
              <RecommendationsList
                recommendations={displayReport.recommendations}
                onToast={addToast}
              />
            )}

            {activeTab === 'timeline' && (
              <TimelineChart />
            )}

            {activeTab === 'namespaces' && (
              <NamespacesTable namespaces={displayReport.summary.namespaces} />
            )}
          </div>

          {/* Right Column - AI Insights */}
          <aside className="space-y-4 sm:space-y-6" aria-label="AI Insights sidebar">
            <AIInsightFeed />
          </aside>
        </div>
      </main>

      {/* Footer */}
      <footer className="border-t border-cluster-border mt-auto py-4">
        <div className="max-w-[1800px] mx-auto px-4 sm:px-6 text-center text-slate-400 text-sm">
          <p>
            K8s AI Cluster Intelligence Engine v{packageJson.version}
            <span className="hidden sm:inline"> | </span>
            <br className="sm:hidden" />
            Last analysis: {new Date(displayReport.timestamp).toLocaleString()}
          </p>
        </div>
      </footer>

      {/* Toast Notifications */}
      <ToastContainer toasts={toasts} onDismiss={dismissToast} />

      {/* Settings Modal */}
      <SettingsModal
        isOpen={showSettings}
        onClose={() => setShowSettings(false)}
        profile={displayReport.status?.profile ?? 'live'}
        collectorUrl={displayReport.status?.collector?.endpoint ?? ''}
        onSwitchProfile={handleSwitchProfile}
        onSetCollectorUrl={handleSetCollectorUrl}
      />
    </div>
  )
}

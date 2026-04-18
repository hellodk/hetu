'use client'

import { useState, useEffect, useCallback } from 'react'
import Link from 'next/link'
import { apiFetch } from '@/lib/api'
import { DM_Serif_Display, DM_Mono, DM_Sans } from 'next/font/google'
import {
  RefreshCw, Loader2, Shield, Zap, AlertCircle,
  ChevronRight, ChevronDown, ExternalLink, X, Skull,
  CheckCircle2, ArrowRight,
} from 'lucide-react'

const dmSerif = DM_Serif_Display({ subsets: ['latin'], weight: '400', variable: '--font-dm-serif', display: 'swap' })
const dmMono  = DM_Mono({ subsets: ['latin'], weight: ['300', '400', '500'], variable: '--font-dm-mono', display: 'swap' })
const dmSans  = DM_Sans({ subsets: ['latin'], weight: ['400', '500', '600', '700'], variable: '--font-dm-sans', display: 'swap' })

// ─── types ────────────────────────────────────────────────────────────────────

interface ErrorGroup {
  id: number; title: string; namespace: string; service: string
  reason: string; level: string; count: number; status: string
  lastSeen: string; firstSeen: string; lastPod: string; aiSummary: string
}

interface HealthSummaryData {
  totalNodes: number; totalPods: number; totalNamespaces: number
  healthyPods: number; unhealthyPods: number; pendingPods: number
  warningEvents: number; criticalEvents: number
}

interface SecuritySummaryData {
  totalFindings?: number
  bySeverity?: { critical?: number; high?: number; medium?: number; low?: number }
  byCategory?: Record<string, number>
}

interface ErrorSummary {
  totalGroups: number; totalOccurrences: number; openCount: number
  byReason: Record<string, number>; byNamespace: Record<string, number>
}

interface Issue {
  id: string; severity: string; category: string
  title: string; description: string; affectedResources: string[]
}

// ─── helpers ─────────────────────────────────────────────────────────────────

function timeAgo(iso: string): string {
  const s = Math.floor((Date.now() - new Date(iso).getTime()) / 1000)
  if (s < 60) return `${s}s ago`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  return `${Math.floor(h / 24)}d ago`
}

// ─── HealthRing ───────────────────────────────────────────────────────────────

const CIRC = 2 * Math.PI * 50  // 314.16

function HealthRing({ score }: { score: number | null }) {
  const [display, setDisplay] = useState(0)
  const [animated, setAnimated] = useState(false)

  useEffect(() => {
    if (score === null) return
    // reset without transition, then animate after a frame
    setDisplay(0)
    setAnimated(false)
    const t1 = setTimeout(() => setAnimated(true), 50)
    let n = 0
    const t2 = setInterval(() => {
      n = Math.min(n + 1, score)
      setDisplay(n)
      if (n >= score) clearInterval(t2)
    }, 40)
    return () => { clearTimeout(t1); clearInterval(t2) }
  }, [score])

  const color = score === null ? undefined
    : score >= 80 ? '#22c55e' : score >= 60 ? '#f59e0b' : score >= 40 ? '#f97316' : '#ef4444'
  const dashOffset = animated && score !== null ? CIRC * (1 - score / 100) : CIRC

  return (
    <div className="relative flex-shrink-0" style={{ width: 120, height: 120 }}>
      <svg width={120} height={120} viewBox="0 0 120 120" style={{ transform: 'rotate(-90deg)' }}>
        <circle fill="none" stroke="currentColor" strokeOpacity={0.08} strokeWidth={8} cx={60} cy={60} r={50} className="text-cluster-text" />
        <circle fill="none" stroke={color ?? 'currentColor'} strokeWidth={8} strokeLinecap="round"
          cx={60} cy={60} r={50} className={color ? '' : 'text-cluster-muted'}
          style={{
            strokeDasharray: CIRC,
            strokeDashoffset: dashOffset,
            transition: animated ? 'stroke-dashoffset 1.4s cubic-bezier(.22,.61,.36,1)' : 'none',
          }} />
      </svg>
      <div className="absolute inset-0 flex flex-col items-center justify-center">
        <span style={{ fontFamily: 'var(--font-dm-serif,Georgia,serif)', fontSize: 30, color: color ?? 'var(--color-cluster-muted)', lineHeight: 1 }}>
          {display}%
        </span>
        <span style={{ fontFamily: 'var(--font-dm-mono,monospace)', fontSize: 9, letterSpacing: '0.12em', marginTop: 4 }}
              className="text-cluster-muted uppercase">Health</span>
      </div>
    </div>
  )
}

// ─── CategoryDrilldown ────────────────────────────────────────────────────────

type CatId = 'pod' | 'errors' | 'events' | 'security'

function CategoryDrilldown({ categoryId, counts, onClose }: {
  categoryId: CatId
  counts: Record<string, number>
  onClose: () => void
}) {
  const [groups, setGroups] = useState<ErrorGroup[]>([])
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (categoryId !== 'errors') return
    setLoading(true)
    fetch('/api/v1/errors/groups?status=open&limit=25')
      .then(r => r.ok ? r.json() : null)
      .then(d => setGroups(d?.groups || []))
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [categoryId])

  const header = (title: string) => (
    <div className="flex items-center justify-between mb-3">
      <span style={{ fontFamily: 'var(--font-dm-mono,monospace)', fontSize: 10, letterSpacing: '0.12em', textTransform: 'uppercase' }}
            className="text-cluster-muted">{title}</span>
      <button onClick={onClose} className="text-cluster-muted hover:text-cluster-text transition-colors p-1">
        <X className="w-4 h-4" />
      </button>
    </div>
  )

  const wrap = (children: React.ReactNode) => (
    <div className="rounded-b-xl border border-t-0 border-cluster-border bg-cluster-bg px-5 py-4">
      {children}
    </div>
  )

  if (categoryId === 'pod') {
    const items = [
      { label: 'OOMKilled',        count: counts.oom,          href: '/errors?search=OOMKilled',                                  sev: 'critical' },
      { label: 'CrashLoopBackOff', count: counts.crashloop,    href: '/errors?search=CrashLoopBackOff',                           sev: 'critical' },
      { label: 'ImagePullBackOff', count: counts.imagepull,    href: '/errors?search=ImagePullBackOff',                           sev: 'high'     },
      { label: 'Pending Pods',     count: counts.pendingPods,  href: '/workloads/pods?search=Pending&group=core&version=v1',       sev: 'medium'   },
      { label: 'Unhealthy Pods',   count: counts.unhealthyPods, href: '/workloads/pods?group=core&version=v1',                    sev: 'medium'   },
    ]
    return wrap(<>
      {header('Pod Health — breakdown')}
      <div className="divide-y divide-cluster-border/50">
        {items.filter(i => i.count > 0).map(item => (
          <Link key={item.label} href={item.href}
            className="flex items-center justify-between py-2.5 group hover:bg-cluster-border/10 -mx-2 px-2 rounded transition-colors">
            <div className="flex items-center gap-2.5">
              <span className={`w-1.5 h-1.5 rounded-full flex-shrink-0 ${item.sev === 'critical' ? 'bg-red-500' : item.sev === 'high' ? 'bg-orange-500' : 'bg-amber-500'}`} />
              <span className="text-sm text-cluster-text group-hover:text-blue-500 dark:group-hover:text-blue-400 transition-colors">{item.label}</span>
            </div>
            <div className="flex items-center gap-3">
              <span style={{ fontFamily: 'var(--font-dm-mono,monospace)' }} className="text-sm font-medium text-cluster-text">{item.count}</span>
              <ExternalLink className="w-3 h-3 text-cluster-muted/50 group-hover:text-blue-400 transition-colors" />
            </div>
          </Link>
        ))}
        {items.every(i => i.count === 0) && (
          <p className="py-4 text-center text-sm text-cluster-muted">All pod health checks passing</p>
        )}
      </div>
    </>)
  }

  if (categoryId === 'errors') {
    return (
      <div className="rounded-b-xl border border-t-0 border-cluster-border bg-cluster-bg overflow-hidden">
        <div className="flex items-center justify-between px-5 py-3 border-b border-cluster-border">
          <span style={{ fontFamily: 'var(--font-dm-mono,monospace)', fontSize: 10, letterSpacing: '0.12em', textTransform: 'uppercase' }}
                className="text-cluster-muted">App Errors — open groups</span>
          <div className="flex items-center gap-3">
            <Link href="/errors" className="text-xs text-cluster-muted hover:text-cluster-text flex items-center gap-1 transition-colors">
              View all <ExternalLink className="w-3 h-3" />
            </Link>
            <button onClick={onClose} className="text-cluster-muted hover:text-cluster-text transition-colors">
              <X className="w-4 h-4" />
            </button>
          </div>
        </div>
        {loading ? (
          <div className="flex items-center justify-center py-8 gap-2 text-cluster-muted">
            <Loader2 className="w-4 h-4 animate-spin" />
            <span className="text-sm">Loading…</span>
          </div>
        ) : groups.length === 0 ? (
          <p className="py-8 text-center text-sm text-cluster-muted">No open error groups found</p>
        ) : (
          <div className="divide-y divide-cluster-border/50">
            {groups.slice(0, 10).map(g => (
              <Link key={g.id} href="/errors"
                className="flex items-start gap-3 px-5 py-3 hover:bg-cluster-border/20 transition-colors group">
                <span className={`mt-1.5 w-1.5 h-1.5 rounded-full flex-shrink-0 ${g.level === 'fatal' ? 'bg-red-500' : g.level === 'error' ? 'bg-orange-500' : 'bg-amber-500'}`} />
                <div className="flex-1 min-w-0">
                  <p className="text-sm text-cluster-text truncate group-hover:text-blue-500 dark:group-hover:text-blue-400 transition-colors">{g.title}</p>
                  <p className="text-xs text-cluster-muted mt-0.5">{g.namespace} · {g.service} · {timeAgo(g.lastSeen)}</p>
                </div>
                <span style={{ fontFamily: 'var(--font-dm-mono,monospace)' }} className="flex-shrink-0 text-sm text-cluster-text">{g.count}</span>
              </Link>
            ))}
          </div>
        )}
      </div>
    )
  }

  if (categoryId === 'events') {
    return wrap(<>
      {header('Cluster Events')}
      <div className="divide-y divide-cluster-border/50">
        {counts.criticalEvents > 0 && (
          <div className="flex items-center gap-3 py-2.5">
            <span className="w-1.5 h-1.5 rounded-full bg-red-500 flex-shrink-0" />
            <span className="text-sm text-cluster-text flex-1">Critical events</span>
            <span style={{ fontFamily: 'var(--font-dm-mono,monospace)' }} className="text-sm font-medium text-red-500 dark:text-red-400">{counts.criticalEvents}</span>
          </div>
        )}
        {counts.warningEvents > 0 && (
          <div className="flex items-center gap-3 py-2.5">
            <span className="w-1.5 h-1.5 rounded-full bg-amber-500 flex-shrink-0" />
            <span className="text-sm text-cluster-text flex-1">Warning events</span>
            <span style={{ fontFamily: 'var(--font-dm-mono,monospace)' }} className="text-sm font-medium text-amber-500">{counts.warningEvents}</span>
          </div>
        )}
        {counts.criticalEvents === 0 && counts.warningEvents === 0 && (
          <p className="py-2 text-sm text-cluster-muted">No active events</p>
        )}
      </div>
      <Link href="/workloads/events?group=core&version=v1"
        className="inline-flex items-center gap-1.5 mt-3 text-sm text-blue-600 dark:text-blue-400 hover:underline">
        View all events <ExternalLink className="w-3.5 h-3.5" />
      </Link>
    </>)
  }

  // security
  return wrap(<>
    {header('Security Findings')}
    <div className="divide-y divide-cluster-border/50">
      {counts.cveCritical > 0 && (
        <div className="flex items-center gap-3 py-2.5">
          <span className="w-1.5 h-1.5 rounded-full bg-red-500 flex-shrink-0" />
          <span className="text-sm text-cluster-text flex-1">Critical CVEs</span>
          <span style={{ fontFamily: 'var(--font-dm-mono,monospace)' }} className="text-sm font-medium text-red-500 dark:text-red-400">{counts.cveCritical}</span>
        </div>
      )}
      {counts.cveHigh > 0 && (
        <div className="flex items-center gap-3 py-2.5">
          <span className="w-1.5 h-1.5 rounded-full bg-orange-500 flex-shrink-0" />
          <span className="text-sm text-cluster-text flex-1">High CVEs</span>
          <span style={{ fontFamily: 'var(--font-dm-mono,monospace)' }} className="text-sm font-medium text-orange-500">{counts.cveHigh}</span>
        </div>
      )}
      {counts.cveCritical === 0 && counts.cveHigh === 0 && (
        <p className="py-2 text-sm text-cluster-muted">No CVE findings</p>
      )}
    </div>
    <Link href="/security"
      className="inline-flex items-center gap-1.5 mt-3 text-sm text-blue-600 dark:text-blue-400 hover:underline">
      View security dashboard <ExternalLink className="w-3.5 h-3.5" />
    </Link>
  </>)
}

// ─── page ─────────────────────────────────────────────────────────────────────

export default function IssuesPage() {
  const [health, setHealth]               = useState<{ summary: HealthSummaryData; topIssues: Issue[] } | null>(null)
  const [errorSummary, setErrorSummary]   = useState<ErrorSummary | null>(null)
  const [securitySummary, setSecSummary]  = useState<SecuritySummaryData | null>(null)
  const [loading, setLoading]             = useState(true)
  const [refreshing, setRefreshing]       = useState(false)
  const [openCat, setOpenCat]             = useState<CatId | null>(null)
  const [activeTab, setActiveTab]         = useState<'all' | CatId>('all')

  const fetchAll = useCallback(async (isRefresh = false) => {
    if (isRefresh) setRefreshing(true); else setLoading(true)
    try {
      const [h, e, s] = await Promise.allSettled([
        apiFetch<{ summary: HealthSummaryData; topIssues: Issue[] }>('/api/v1/health'),
        apiFetch<ErrorSummary>('/api/v1/errors/summary'),
        apiFetch<SecuritySummaryData>('/api/v1/security/summary'),
      ])
      if (h.status === 'fulfilled') setHealth(h.value)
      if (e.status === 'fulfilled') setErrorSummary(e.value)
      if (s.status === 'fulfilled') setSecSummary(s.value)
    } catch { /* ignore */ }
    finally { setLoading(false); setRefreshing(false) }
  }, [])

  useEffect(() => { fetchAll() }, [fetchAll])

  const counts = {
    oom:           (errorSummary?.byReason?.OOMKilled ?? 0) + (errorSummary?.byReason?.oom ?? 0),
    crashloop:     errorSummary?.byReason?.CrashLoopBackOff ?? 0,
    imagepull:     errorSummary?.byReason?.ImagePullBackOff ?? 0,
    dns:           errorSummary?.byReason?.DNSConfigForming ?? 0,
    timeout:       errorSummary?.byReason?.timeout ?? 0,
    http5xx:       errorSummary?.byReason?.['http.5xx'] ?? 0,
    panic:         (errorSummary?.byReason?.panic ?? 0) + (errorSummary?.byReason?.exception ?? 0),
    pendingPods:   health?.summary?.pendingPods ?? 0,
    unhealthyPods: health?.summary?.unhealthyPods ?? 0,
    warningEvents: health?.summary?.warningEvents ?? 0,
    criticalEvents: health?.summary?.criticalEvents ?? 0,
    cveCritical:   securitySummary?.bySeverity?.critical ?? 0,
    cveHigh:       securitySummary?.bySeverity?.high ?? 0,
  }

  const podTotal      = counts.oom + counts.crashloop + counts.imagepull + counts.pendingPods + counts.unhealthyPods
  const errorTotal    = counts.dns + counts.timeout + counts.http5xx + counts.panic
  const eventTotal    = counts.warningEvents + counts.criticalEvents
  const securityTotal = counts.cveCritical + counts.cveHigh

  const totalCritical = counts.oom + counts.crashloop + counts.criticalEvents + counts.cveCritical
  const totalHigh     = counts.imagepull + counts.dns + counts.timeout + counts.http5xx + counts.panic + counts.cveHigh
  const totalMedium   = counts.pendingPods + counts.unhealthyPods + counts.warningEvents
  const grandTotal    = totalCritical + totalHigh + totalMedium

  const hasData    = !!(health || errorSummary || securitySummary)
  const healthScore = hasData
    ? Math.max(0, Math.round(
        100
        - Math.min(totalCritical * 6, 60)
        - Math.min(totalHigh * 0.5, 20)
        - Math.min(totalMedium * 0.02, 10)
      ))
    : null

  const maxCat = Math.max(podTotal, errorTotal, eventTotal, securityTotal, 1)

  type CatDef = {
    id: CatId; label: string; total: number
    severity: 'critical' | 'high' | 'medium' | 'ok'
    iconBg: string; iconColor: string; barColor: string
    chips: string[]; desc: string
  }

  const categories: CatDef[] = [
    {
      id: 'pod', label: 'Pod Health', total: podTotal,
      severity: podTotal === 0 ? 'ok' : (counts.oom > 0 || counts.crashloop > 0 ? 'critical' : 'high'),
      iconBg:    podTotal === 0 ? 'bg-emerald-500/10' : (counts.oom > 0 || counts.crashloop > 0 ? 'bg-red-500/10' : 'bg-orange-500/10'),
      iconColor: podTotal === 0 ? 'text-emerald-500' : (counts.oom > 0 || counts.crashloop > 0 ? 'text-red-500'  : 'text-orange-500'),
      barColor:  podTotal === 0 ? 'bg-emerald-500' : (counts.oom > 0 || counts.crashloop > 0 ? 'bg-red-500'    : 'bg-orange-500'),
      chips: ([
        counts.oom > 0       && `OOMKilled×${counts.oom}`,
        counts.crashloop > 0 && `CrashLoop×${counts.crashloop}`,
        counts.imagepull > 0 && `ImagePull×${counts.imagepull}`,
        counts.pendingPods > 0 && `Pending×${counts.pendingPods}`,
        counts.unhealthyPods > 0 && `Unhealthy×${counts.unhealthyPods}`,
      ] as (string | false)[]).filter((x): x is string => !!x),
      desc: podTotal === 0 ? 'All pods healthy' : `${counts.oom + counts.crashloop} critical · ${counts.imagepull} high`,
    },
    {
      id: 'errors', label: 'App Errors', total: errorTotal,
      severity: errorTotal === 0 ? 'ok' : (counts.dns > 0 ? 'high' : 'medium'),
      iconBg:    errorTotal === 0 ? 'bg-emerald-500/10' : 'bg-orange-500/10',
      iconColor: errorTotal === 0 ? 'text-emerald-500'  : 'text-orange-500',
      barColor:  errorTotal === 0 ? 'bg-emerald-500'    : 'bg-orange-500',
      chips: ([
        `DNS×${counts.dns}`,
        `Timeout×${counts.timeout}`,
        `5xx×${counts.http5xx}`,
        counts.panic > 0 && `Panic×${counts.panic}`,
      ] as (string | false)[]).filter((x): x is string => !!x),
      desc: `${errorTotal} error groups`,
    },
    {
      id: 'events', label: 'Cluster Events', total: eventTotal,
      severity: eventTotal === 0 ? 'ok' : (counts.criticalEvents > 0 ? 'critical' : 'medium'),
      iconBg:    eventTotal === 0 ? 'bg-emerald-500/10' : (counts.criticalEvents > 0 ? 'bg-red-500/10'   : 'bg-amber-500/10'),
      iconColor: eventTotal === 0 ? 'text-emerald-500'  : (counts.criticalEvents > 0 ? 'text-red-500'    : 'text-amber-500'),
      barColor:  eventTotal === 0 ? 'bg-emerald-500'    : (counts.criticalEvents > 0 ? 'bg-red-500'      : 'bg-amber-500'),
      chips: ([
        counts.criticalEvents > 0 && `Critical×${counts.criticalEvents}`,
        `Warning×${counts.warningEvents}`,
      ] as (string | false)[]).filter((x): x is string => !!x),
      desc: `${counts.warningEvents} warning · ${counts.criticalEvents} critical`,
    },
    {
      id: 'security', label: 'Security CVEs', total: securityTotal,
      severity: securityTotal === 0 ? 'ok' : (counts.cveCritical > 0 ? 'critical' : 'high'),
      iconBg:    securityTotal === 0 ? 'bg-emerald-500/10' : 'bg-red-500/10',
      iconColor: securityTotal === 0 ? 'text-emerald-500'  : 'text-red-500',
      barColor:  securityTotal === 0 ? 'bg-emerald-500'    : 'bg-red-500',
      chips: ([
        counts.cveCritical > 0 && `Critical×${counts.cveCritical}`,
        counts.cveHigh > 0     && `High×${counts.cveHigh}`,
      ] as (string | false)[]).filter((x): x is string => !!x),
      desc: `${counts.cveCritical} critical · ${counts.cveHigh} high CVEs`,
    },
  ]

  // top 3 non-ok categories sorted by severity for the action rail
  const sevOrder: Record<string, number> = { critical: 0, high: 1, medium: 2, ok: 3 }
  const actionItems = categories
    .filter(c => c.total > 0)
    .sort((a, b) => sevOrder[a.severity] - sevOrder[b.severity])
    .slice(0, 3)

  const visibleCats = activeTab === 'all' ? categories : categories.filter(c => c.id === activeTab)
  const toggleCat   = (id: CatId) => setOpenCat(prev => prev === id ? null : id)

  if (loading) {
    return (
      <div className={`${dmSerif.variable} ${dmMono.variable} ${dmSans.variable} min-h-screen flex items-center justify-center bg-cluster-bg`}>
        <div className="flex items-center gap-3 text-cluster-muted">
          <Loader2 className="w-5 h-5 animate-spin" />
          <span className="text-sm">Loading issues…</span>
        </div>
      </div>
    )
  }

  return (
    <div className={`${dmSerif.variable} ${dmMono.variable} ${dmSans.variable} min-h-screen bg-cluster-bg`}
         style={{ fontFamily: 'var(--font-dm-sans, system-ui, sans-serif)' }}>
      <div className="max-w-[1200px] mx-auto px-5 sm:px-8 py-6 space-y-5">

        {/* ── Header ── */}
        <div className="flex items-start justify-between gap-4">
          <div>
            <p style={{ fontFamily: 'var(--font-dm-mono,monospace)', fontSize: 10, letterSpacing: '0.14em', textTransform: 'uppercase' }}
               className="text-cluster-muted mb-1.5">Issues Dashboard</p>
            <h1 className="text-2xl font-semibold tracking-tight text-cluster-text">Cluster Health</h1>
            <p className="text-sm text-cluster-muted mt-1">
              {grandTotal} active issues across {categories.filter(c => c.total > 0).length} categories
            </p>
          </div>
          <div className="flex items-center gap-3 flex-shrink-0">
            <div className="flex items-center gap-1.5" style={{ fontFamily: 'var(--font-dm-mono,monospace)', fontSize: 11 }}>
              <span className="w-1.5 h-1.5 rounded-full bg-emerald-500 animate-pulse flex-shrink-0" />
              <span className="text-cluster-muted">Live</span>
            </div>
            <button
              onClick={() => fetchAll(true)}
              disabled={refreshing}
              className="flex items-center gap-2 px-3 py-2 text-sm text-cluster-muted hover:text-cluster-text border border-cluster-border rounded-lg hover:bg-cluster-border/30 transition-colors"
            >
              <RefreshCw className={`w-4 h-4 ${refreshing ? 'animate-spin' : ''}`} />
              Refresh
            </button>
          </div>
        </div>

        {/* ── Health Hero ── */}
        <div className="rounded-xl border border-cluster-border bg-cluster-card p-6 md:p-8 grid grid-cols-1 md:grid-cols-[auto_1fr_auto] gap-6 md:gap-8 items-center">
          <HealthRing score={healthScore} />

          <div>
            <h2 className="text-base font-semibold text-cluster-text mb-1.5">
              {totalCritical > 0 ? 'Cluster needs attention' : totalHigh > 0 ? 'Minor issues detected' : 'Cluster is healthy'}
            </h2>
            <p className="text-sm text-cluster-muted leading-relaxed max-w-lg">
              {totalCritical > 0
                ? `${totalCritical} critical finding${totalCritical !== 1 ? 's' : ''} require immediate action.${counts.cveCritical > 0 ? ' Security CVE exposure is the primary driver.' : ''}`
                : totalHigh > 0
                ? `${totalHigh} high-priority issues detected. Monitor closely.`
                : 'All systems operating normally. No critical issues detected.'}
            </p>
            <div className="flex gap-5 mt-3.5 flex-wrap">
              {[
                { label: 'Pod Health',  ok: podTotal === 0 },
                { label: 'Security',    ok: securityTotal === 0 },
                { label: 'Events',      ok: eventTotal === 0 },
                { label: 'App Errors',  ok: errorTotal === 0 },
              ].map(({ label, ok }) => (
                <div key={label} className="flex items-center gap-1.5 text-xs" style={{ fontFamily: 'var(--font-dm-mono,monospace)' }}>
                  <span className={`w-2 h-2 rounded-sm ${ok ? 'bg-emerald-500' : 'bg-red-500'}`} />
                  <span className="text-cluster-muted">{label}</span>
                  <span className={ok ? 'text-emerald-500 dark:text-emerald-400' : 'text-red-500 dark:text-red-400'}>
                    {ok ? '✓' : '✗'}
                  </span>
                </div>
              ))}
            </div>
          </div>

          {/* Severity counters */}
          <div className="flex md:flex-col gap-2 md:gap-2.5 flex-wrap">
            {[
              { label: 'CRITICAL', count: totalCritical, bg: 'bg-red-500/10 border-red-500/25',       text: 'text-red-500 dark:text-red-400'       },
              { label: 'HIGH',     count: totalHigh,     bg: 'bg-orange-500/10 border-orange-500/25', text: 'text-orange-500 dark:text-orange-400' },
              { label: 'MEDIUM',   count: totalMedium,   bg: 'bg-amber-500/10 border-amber-500/25',   text: 'text-amber-500 dark:text-amber-400'   },
            ].map(({ label, count, bg, text }) => (
              <div key={label} className={`flex flex-col items-end px-4 py-2.5 rounded-lg border ${bg} min-w-[80px]`}>
                <span style={{ fontFamily: 'var(--font-dm-mono,monospace)', fontSize: 26, lineHeight: 1, fontWeight: 500 }} className={text}>
                  {count}
                </span>
                <span style={{ fontFamily: 'var(--font-dm-mono,monospace)', fontSize: 9, letterSpacing: '0.08em', textTransform: 'uppercase', marginTop: 3 }}
                      className="text-cluster-muted">{label}</span>
              </div>
            ))}
          </div>
        </div>

        {/* ── Action Rail ── */}
        {actionItems.length > 0 && (
          <div>
            <div className="flex items-center gap-2 mb-2">
              <span style={{ fontFamily: 'var(--font-dm-mono,monospace)', fontSize: 10, letterSpacing: '0.12em', textTransform: 'uppercase', fontWeight: 500 }}
                    className="text-red-500 dark:text-red-400">⚡ Needs Action Now</span>
              <span style={{ fontFamily: 'var(--font-dm-mono,monospace)', fontSize: 10, fontWeight: 700 }}
                    className="px-1.5 py-0.5 rounded bg-red-500 text-white">
                {actionItems.length}
              </span>
            </div>
            <div className="flex flex-col gap-1.5">
              {actionItems.map(item => {
                const isCrit  = item.severity === 'critical'
                const isHigh  = item.severity === 'high'
                const leftCol = isCrit ? '#ef4444' : isHigh ? '#f97316' : '#f59e0b'
                const tagCls  = isCrit
                  ? 'bg-red-500/10 text-red-500 dark:text-red-400 border-red-500/25'
                  : isHigh
                  ? 'bg-orange-500/10 text-orange-500 dark:text-orange-400 border-orange-500/25'
                  : 'bg-amber-500/10 text-amber-500 dark:text-amber-400 border-amber-500/25'
                const navHref  = item.id === 'errors' ? '/errors' : item.id === 'events' ? '/workloads/events?group=core&version=v1' : item.id === 'security' ? '/security' : '/workloads/pods?group=core&version=v1'
                const navLabel = item.id === 'errors' ? '→ Errors' : item.id === 'events' ? '→ Events' : item.id === 'security' ? '→ Security' : '→ Workloads'
                return (
                  <div key={item.id}
                    className="flex items-center gap-3 px-4 py-3.5 bg-cluster-card border border-cluster-border rounded-xl hover:bg-cluster-card/80 transition-colors"
                    style={{ borderLeft: `3px solid ${leftCol}` }}>
                    <span className={`w-2 h-2 rounded-full flex-shrink-0 ${isCrit ? 'bg-red-500' : isHigh ? 'bg-orange-500' : 'bg-amber-500'}`}
                          style={{ boxShadow: `0 0 6px ${leftCol}` }} />
                    <div className="flex-1 min-w-0">
                      <p className="text-sm font-semibold text-cluster-text">{item.label}</p>
                      <p className="text-xs text-cluster-muted mt-0.5">{item.desc}</p>
                    </div>
                    <div className="flex items-center gap-3 flex-shrink-0">
                      <span style={{ fontFamily: 'var(--font-dm-mono,monospace)', fontSize: 10, fontWeight: 600, letterSpacing: '0.08em', textTransform: 'uppercase' }}
                            className={`px-2 py-0.5 rounded border ${tagCls}`}>
                        {item.severity}
                      </span>
                      <span style={{ fontFamily: 'var(--font-dm-mono,monospace)', fontSize: 11 }} className="text-cluster-muted">
                        {item.total}
                      </span>
                      <Link href={navHref}
                        style={{ fontFamily: 'var(--font-dm-mono,monospace)', fontSize: 12 }}
                        className="px-2 py-1 border border-cluster-border rounded text-cluster-muted hover:bg-cluster-border/30 hover:text-cluster-text transition-colors whitespace-nowrap">
                        {navLabel}
                      </Link>
                    </div>
                  </div>
                )
              })}
            </div>
          </div>
        )}

        {/* ── Tab bar ── */}
        <div className="flex gap-1 flex-wrap">
          {([
            { id: 'all'      as const, label: 'All Issues',     count: grandTotal    },
            { id: 'pod'      as const, label: 'Pod Health',     count: podTotal      },
            { id: 'errors'   as const, label: 'App Errors',     count: errorTotal    },
            { id: 'events'   as const, label: 'Cluster Events', count: eventTotal    },
            { id: 'security' as const, label: 'Security',       count: securityTotal },
          ]).map(t => {
            const hasCrit = t.id !== 'all' && categories.find(c => c.id === t.id)?.severity === 'critical'
            return (
              <button key={t.id}
                onClick={() => { setActiveTab(t.id); setOpenCat(null) }}
                className={`px-3 py-1.5 rounded-lg text-sm font-medium transition-colors border ${
                  activeTab === t.id
                    ? 'bg-cluster-card border-cluster-border text-cluster-text'
                    : 'border-transparent text-cluster-muted hover:bg-cluster-card hover:text-cluster-text'
                }`}>
                {t.label}
                <span style={{ fontFamily: 'var(--font-dm-mono,monospace)', fontSize: 11, marginLeft: 6, opacity: t.count === 0 ? 0.5 : 0.85 }}
                      className={hasCrit && t.count > 0 ? 'text-red-500 dark:text-red-400' : ''}>
                  {t.count}
                </span>
              </button>
            )
          })}
        </div>

        {/* ── Category Breakdown ── */}
        <div>
          <p style={{ fontFamily: 'var(--font-dm-mono,monospace)', fontSize: 10, letterSpacing: '0.12em', textTransform: 'uppercase' }}
             className="text-cluster-muted mb-2.5">Category Breakdown</p>

          <div className="flex flex-col gap-0.5">
            {visibleCats.map(cat => {
              const isOpen   = openCat === cat.id
              const barWidth = cat.total > 0 ? `${Math.round(cat.total / maxCat * 100)}%` : '0%'
              return (
                <div key={cat.id}>
                  <button
                    onClick={() => toggleCat(cat.id)}
                    className={`w-full text-left grid items-center gap-5 px-4 py-3.5 border transition-all
                      grid-cols-[140px_56px_1fr_200px]
                      ${isOpen
                        ? 'bg-cluster-card border-cluster-border rounded-t-xl'
                        : cat.total === 0
                        ? 'bg-cluster-card/50 border-cluster-border/50 opacity-60 hover:opacity-100 rounded-xl'
                        : 'bg-cluster-card border-cluster-border rounded-xl hover:border-cluster-border/80'
                      }`}
                  >
                    {/* Name */}
                    <div className="flex items-center gap-2.5 min-w-0">
                      <div className={`w-7 h-7 rounded-lg flex items-center justify-center flex-shrink-0 ${cat.iconBg}`}>
                        {cat.id === 'pod'      && <Skull        className={`w-3.5 h-3.5 ${cat.iconColor}`} />}
                        {cat.id === 'errors'   && <AlertCircle  className={`w-3.5 h-3.5 ${cat.iconColor}`} />}
                        {cat.id === 'events'   && <Zap          className={`w-3.5 h-3.5 ${cat.iconColor}`} />}
                        {cat.id === 'security' && <Shield       className={`w-3.5 h-3.5 ${cat.iconColor}`} />}
                      </div>
                      <span className="text-sm font-semibold text-cluster-text truncate">{cat.label}</span>
                    </div>

                    {/* Count */}
                    <span style={{ fontFamily: 'var(--font-dm-mono,monospace)', fontSize: cat.total === 0 ? 14 : 22, fontWeight: 500, textAlign: 'right', display: 'block' }}
                          className={cat.total === 0 ? 'text-cluster-muted' : cat.iconColor}>
                      {cat.total}
                    </span>

                    {/* Bar + description */}
                    <div>
                      <div className="h-1.5 bg-cluster-border rounded-full overflow-hidden">
                        <div className={`h-full rounded-full transition-all duration-1000 ${cat.barColor}`}
                             style={{ width: barWidth }} />
                      </div>
                      <p style={{ fontFamily: 'var(--font-dm-mono,monospace)', fontSize: 11, marginTop: 5 }}
                         className={cat.total === 0 ? 'text-emerald-500 dark:text-emerald-400' : 'text-cluster-muted'}>
                        {cat.total === 0 ? 'All clear' : cat.desc}
                      </p>
                    </div>

                    {/* Chips + chevron */}
                    <div className="flex items-center gap-2 flex-wrap justify-end">
                      {cat.total > 0 && cat.chips.slice(0, 3).map(chip => (
                        <span key={chip}
                              style={{ fontFamily: 'var(--font-dm-mono,monospace)', fontSize: 10 }}
                              className="px-1.5 py-0.5 rounded border border-cluster-border text-cluster-muted">
                          {chip}
                        </span>
                      ))}
                      {cat.total === 0 && (
                        <div className="flex items-center gap-1.5 text-xs text-cluster-muted">
                          <CheckCircle2 className="w-3.5 h-3.5 text-emerald-500" />
                          None detected
                        </div>
                      )}
                      {isOpen
                        ? <ChevronDown  className="w-4 h-4 text-cluster-muted flex-shrink-0 ml-1" />
                        : <ChevronRight className="w-4 h-4 text-cluster-muted flex-shrink-0 ml-1" />}
                    </div>
                  </button>

                  {isOpen && (
                    <CategoryDrilldown
                      categoryId={cat.id}
                      counts={counts}
                      onClose={() => setOpenCat(null)}
                    />
                  )}
                </div>
              )
            })}
          </div>
        </div>

        {/* ── AI-analyzed issues ── */}
        {health?.topIssues && health.topIssues.length > 0 && activeTab === 'all' && (
          <div>
            <div className="flex items-center gap-2 mb-2.5">
              <span style={{ fontFamily: 'var(--font-dm-mono,monospace)', fontSize: 10, letterSpacing: '0.12em', textTransform: 'uppercase' }}
                    className="text-cluster-muted">🤖 AI-Analyzed Issues</span>
              <span style={{ fontFamily: 'var(--font-dm-mono,monospace)', fontSize: 10 }}
                    className="px-2 py-0.5 rounded border border-cluster-border/50 bg-blue-500/10 text-blue-600 dark:text-blue-400">
                {health.topIssues.length} findings
              </span>
            </div>
            <div className="bg-cluster-card border border-cluster-border rounded-xl overflow-hidden divide-y divide-cluster-border/60">
              {health.topIssues.map(issue => {
                const sevDot = issue.severity === 'critical' ? 'bg-red-500'
                  : issue.severity === 'high' ? 'bg-orange-500'
                  : issue.severity === 'medium' ? 'bg-amber-500' : 'bg-blue-500'
                const sevBadge = issue.severity === 'critical'
                  ? 'bg-red-500/10 text-red-500 dark:text-red-400 border-red-500/25'
                  : issue.severity === 'high'
                  ? 'bg-orange-500/10 text-orange-500 dark:text-orange-400 border-orange-500/25'
                  : issue.severity === 'medium'
                  ? 'bg-amber-500/10 text-amber-500 dark:text-amber-400 border-amber-500/25'
                  : 'bg-blue-500/10 text-blue-500 dark:text-blue-400 border-blue-500/25'
                return (
                  <div key={issue.id} className="flex items-start gap-4 px-5 py-4 hover:bg-cluster-border/15 transition-colors">
                    <span className={`mt-1.5 w-1.5 h-1.5 rounded-full flex-shrink-0 ${sevDot}`} />
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 flex-wrap">
                        <p className="text-sm font-medium text-cluster-text">{issue.title}</p>
                        <span style={{ fontFamily: 'var(--font-dm-mono,monospace)', fontSize: 10, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.06em' }}
                              className={`px-1.5 py-0.5 rounded-full border ${sevBadge}`}>
                          {issue.severity}
                        </span>
                        <span style={{ fontFamily: 'var(--font-dm-mono,monospace)', fontSize: 10 }}
                              className="px-1.5 py-0.5 rounded-full border border-cluster-border/60 text-cluster-muted">
                          {issue.category}
                        </span>
                      </div>
                      <p className="text-xs text-cluster-muted mt-1 leading-snug">{issue.description}</p>
                      {(issue.affectedResources ?? []).length > 0 && (
                        <div className="flex items-center gap-1 flex-wrap mt-1.5">
                          {(issue.affectedResources ?? []).slice(0, 4).map((r, i) => {
                            const parts = r.split('/')
                            const ns   = parts.length >= 2 ? parts[0] : 'default'
                            const name = parts.length >= 2 ? parts[1] : parts[0]
                            return (
                              <Link key={i} href={`/workloads/pods/${ns}/${name}?group=core&version=v1`}
                                className="text-[10px] font-mono px-1.5 py-0.5 bg-blue-600/10 text-blue-600 dark:text-blue-400 border border-blue-600/20 rounded hover:bg-blue-600/20 transition-colors">
                                {r}
                              </Link>
                            )
                          })}
                          {(issue.affectedResources ?? []).length > 4 && (
                            <span className="text-[10px] text-cluster-muted">+{(issue.affectedResources ?? []).length - 4} more</span>
                          )}
                        </div>
                      )}
                    </div>
                    <ArrowRight className="w-3.5 h-3.5 text-cluster-muted flex-shrink-0 mt-1" />
                  </div>
                )
              })}
            </div>
          </div>
        )}

      </div>
    </div>
  )
}

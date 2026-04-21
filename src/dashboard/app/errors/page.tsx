'use client'

import { useEffect, useState, useCallback } from 'react'
import { useSearchParams } from 'next/navigation'
import Link from 'next/link'
import { apiFetch } from '@/lib/api'
import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip,
  ResponsiveContainer, Cell
} from 'recharts'
import {
  Search, RefreshCw, AlertCircle, AlertTriangle,
  Clock, Loader2, XCircle, CheckCircle, Sparkles, Bot,
  ChevronDown, ChevronRight, Layers, Hash, CircleDot,
  ShieldAlert, Activity, EyeOff
} from 'lucide-react'

/* ------------------------------------------------------------------ */
/*  Types                                                              */
/* ------------------------------------------------------------------ */

// Phase 1.1: rate aggregates are computed server-side from the occurrence
// ring buffer. `truncated` means the ring was full in the window, so the
// real rate is ≥ the reported count.
interface ErrorRate {
  count1m: number
  count5m: number
  count1h: number
  count24h: number
  spark: number[]
  truncated: boolean
}

interface ErrorGroup {
  id: number
  fingerprint: string
  service: string
  namespace: string
  title: string
  exceptionType: string
  reason: string
  level: string
  firstSeen: string
  lastSeen: string
  count: number
  status: string
  lastPod: string
  lastUrl: string
  aiSummary: string
  sampleMessage: string
  sampleStack: string
  rate?: ErrorRate
}

interface Occurrence {
  timestamp: string
  pod: string
  container: string
  message: string
  url: string
  requestId: string
}

interface ErrorSummary {
  totalGroups: number
  totalOccurrences: number
  openCount: number
  byReason: Record<string, number>
  byNamespace: Record<string, number>
  topGroups: {
    id: number
    title: string
    reason: string
    service: string
    namespace: string
    count: number
    lastSeen: string
    aiSummary: string
  }[]
  topServices: { service: string; count: number }[]
}

interface LLMConfig {
  provider: string
  model: string
}

/* ------------------------------------------------------------------ */
/*  Helpers                                                            */
/* ------------------------------------------------------------------ */

const REASON_COLORS: Record<string, string> = {
  CrashLoopBackOff: '#ef4444',
  OOMKilled: '#f97316',
  DNSConfigForming: '#eab308',
  ImagePullBackOff: '#a855f7',
  'http.5xx': '#ec4899',
  timeout: '#f59e0b',
  exception: '#dc2626',
  oom: '#ea580c',
  panic: '#be123c',
}

function reasonColor(reason: string): string {
  return REASON_COLORS[reason] || '#6b7280'
}

function severityIcon(g: { level: string; reason: string }) {
  if (g.level === 'fatal' || g.level === 'panic')
    return <XCircle className="w-4 h-4 text-red-500" />
  if (g.reason?.startsWith('exception') || g.reason === 'oom' || g.reason === 'OOMKilled')
    return <AlertCircle className="w-4 h-4 text-red-400" />
  if (g.reason === 'timeout' || g.reason === 'http.5xx' || g.reason === 'CrashLoopBackOff')
    return <AlertTriangle className="w-4 h-4 text-orange-400" />
  return <AlertTriangle className="w-4 h-4 text-yellow-400" />
}

function timeSince(iso: string): string {
  const ms = Date.now() - new Date(iso).getTime()
  if (ms < 60000) return `${Math.floor(ms / 1000)}s ago`
  if (ms < 3600000) return `${Math.floor(ms / 60000)}m ago`
  if (ms < 86400000) return `${Math.floor(ms / 3600000)}h ago`
  return `${Math.floor(ms / 86400000)}d ago`
}

function statusColor(status: string) {
  if (status === 'open') return 'bg-red-900/30 text-red-300 border-red-700/50'
  if (status === 'resolved') return 'bg-green-900/30 text-green-300 border-green-700/50'
  return 'bg-gray-700/50 text-gray-400 border-gray-600/50'
}

/* ---------- Phase 1.2: severity chip ---------- */

const SEVERITY_STYLE: Record<string, { bg: string; text: string; label: string; rank: number }> = {
  fatal: { bg: 'bg-red-900/40',    text: 'text-red-300',    label: 'FATAL', rank: 5 },
  panic: { bg: 'bg-red-900/40',    text: 'text-red-300',    label: 'PANIC', rank: 5 },
  error: { bg: 'bg-red-800/30',    text: 'text-red-200',    label: 'ERROR', rank: 4 },
  warn:  { bg: 'bg-amber-900/30',  text: 'text-amber-300',  label: 'WARN',  rank: 3 },
  warning: { bg: 'bg-amber-900/30', text: 'text-amber-300', label: 'WARN',  rank: 3 },
  info:  { bg: 'bg-blue-900/30',   text: 'text-blue-300',   label: 'INFO',  rank: 2 },
  debug: { bg: 'bg-gray-700/40',   text: 'text-gray-300',   label: 'DEBUG', rank: 1 },
  trace: { bg: 'bg-gray-700/40',   text: 'text-gray-400',   label: 'TRACE', rank: 1 },
}

function SeverityChip({ level }: { level: string }) {
  const s = SEVERITY_STYLE[level?.toLowerCase()] || { bg: 'bg-gray-700/40', text: 'text-gray-400', label: (level || '—').toUpperCase(), rank: 0 }
  return (
    <span
      className={`inline-flex items-center justify-center px-1.5 py-0.5 rounded text-[10px] font-mono font-semibold tabular-nums ${s.bg} ${s.text} min-w-[56px]`}
      title={`Level: ${level || 'unknown'}`}
    >
      {s.label}
    </span>
  )
}

/* ---------- Phase 1.1: sparkline + spike badge ---------- */

function Sparkline({ spark, spike, truncated }: { spark: number[]; spike: boolean; truncated?: boolean }) {
  const max = Math.max(1, ...spark)
  return (
    <svg
      viewBox="0 0 60 16"
      width={60}
      height={16}
      aria-label="last-hour rate (5-min buckets)"
      role="img"
      style={{ shapeRendering: 'crispEdges' }}
    >
      {spark.map((v, i) => {
        const h = Math.max(1, (v / max) * 14)
        const y = 15 - h
        return (
          <rect
            key={i}
            x={i * 5}
            y={y}
            width={4}
            height={h}
            rx={0.8}
            fill={spike ? '#f59e0b' : '#6b7280'}
            opacity={v === 0 ? 0.3 : 1}
          />
        )
      })}
      {truncated && <text x={58} y={6} textAnchor="end" fontSize={7} fill="#f59e0b" fontFamily="monospace">≥</text>}
    </svg>
  )
}

/**
 * A group is "spiking" when the 5-min rate projected to an hour exceeds
 * the observed 1h count by 2×. Equivalent to "the last 5 minutes is
 * happening at more than 2× the hourly average". We also require at
 * least 3 events in 5 min so one-offs don't flag.
 */
function isSpike(r?: ErrorRate): boolean {
  if (!r) return false
  if (r.count5m < 3) return false
  const projectedHourly = r.count5m * 12
  return projectedHourly > r.count1h * 2
}

function SpikeBadge() {
  return (
    <span
      className="inline-flex items-center gap-1 px-1.5 py-0.5 text-[10px] font-semibold font-mono bg-amber-500/20 text-amber-300 border border-amber-500/40 rounded"
      title="Recent rate >2× hourly average"
    >
      <Activity className="w-2.5 h-2.5" />
      SPIKE
    </span>
  )
}

/* ------------------------------------------------------------------ */
/*  Custom Recharts tooltip                                            */
/* ------------------------------------------------------------------ */

function ReasonTooltip({ active, payload, label }: any) {
  if (!active || !payload?.length) return null
  return (
    <div className="bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 shadow-xl">
      <p className="text-xs text-gray-400 mb-1 font-mono">{label}</p>
      <p className="text-sm font-medium text-white">{payload[0].value} groups</p>
    </div>
  )
}

/* ------------------------------------------------------------------ */
/*  Page component                                                     */
/* ------------------------------------------------------------------ */

export default function ErrorsPage() {
  const searchParams = useSearchParams()

  // Data state
  const [groups, setGroups] = useState<ErrorGroup[]>([])
  const [summary, setSummary] = useState<ErrorSummary | null>(null)
  const [llmConfig, setLlmConfig] = useState<LLMConfig | null>(null)

  // UI state
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [search, setSearch] = useState(() => searchParams?.get('search') ?? '')
  const [statusFilter, setStatusFilter] = useState('open')
  const [serviceFilter, setServiceFilter] = useState('')
  const [sortBy, setSortBy] = useState<'lastSeen' | 'count' | 'severity' | 'rate5m'>('lastSeen')
  const [pageSize, setPageSize] = useState(50)
  const [offset, setOffset] = useState(0)
  const [totalCount, setTotalCount] = useState(0)
  const [expandedGroups, setExpandedGroups] = useState<Set<number>>(new Set())
  const [expandedMessages, setExpandedMessages] = useState<Set<number>>(new Set())
  const [groupOccurrences, setGroupOccurrences] = useState<Record<number, Occurrence[]>>({})
  const [loadingOccurrences, setLoadingOccurrences] = useState<Set<number>>(new Set())
  const [updatingStatus, setUpdatingStatus] = useState<number | null>(null)

  /* ---- Fetch groups ---- */
  const fetchGroups = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const params = new URLSearchParams()
      if (statusFilter) params.set('status', statusFilter)
      if (serviceFilter) params.set('service', serviceFilter)
      if (search) params.set('search', search)
      params.set('sort', sortBy)
      params.set('limit', String(pageSize))
      params.set('offset', String(offset))

      const [groupsData, summaryData] = await Promise.all([
        apiFetch<{ groups: ErrorGroup[]; totalCount: number }>(
          `/api/v1/errors/groups?${params}`
        ),
        apiFetch<ErrorSummary>('/api/v1/errors/summary'),
      ])
      setGroups(groupsData.groups || [])
      setTotalCount(groupsData.totalCount || 0)
      setSummary(summaryData)
    } catch (e: any) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }, [statusFilter, serviceFilter, search, sortBy, pageSize, offset])

  /* ---- Fetch LLM config ---- */
  useEffect(() => {
    apiFetch<LLMConfig>('/api/v1/llm/config')
      .then(cfg => setLlmConfig(cfg))
      .catch(() => setLlmConfig(null))
  }, [])

  useEffect(() => { fetchGroups() }, [fetchGroups])

  /* ---- Derived data ---- */
  const services = Array.from(
    new Set((groups || []).map(g => g.service).concat(
      (summary?.topServices || []).map(s => s.service)
    ))
  ).sort()

  const reasonChartData = summary
    ? Object.entries(summary.byReason)
        .sort(([, a], [, b]) => b - a)
        .slice(0, 12)
        .map(([reason, count]) => ({ reason, count }))
    : []

  const topReason = summary && Object.keys(summary.byReason).length > 0
    ? Object.entries(summary.byReason).sort(([, a], [, b]) => b - a)[0][0]
    : '-'

  /* ---- Expand / collapse a group (fetch occurrences) ---- */
  const toggleGroup = async (id: number, fingerprint: string) => {
    setExpandedGroups(prev => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
        // Fetch occurrences if not already loaded
        if (!groupOccurrences[id]) {
          setLoadingOccurrences(s => { const n = new Set(s); n.add(id); return n })
          apiFetch<{ group: ErrorGroup; occurrences: Occurrence[] }>(`/api/v1/errors/groups/${id}`)
            .then(data => {
              setGroupOccurrences(prev => ({ ...prev, [id]: (data.occurrences || []).slice(0, 5) }))
            })
            .catch(() => {
              setGroupOccurrences(prev => ({ ...prev, [id]: [] }))
            })
            .finally(() => {
              setLoadingOccurrences(s => { const n = new Set(s); n.delete(id); return n })
            })
        }
      }
      return next
    })
  }

  /* ---- Toggle message expansion ---- */
  const toggleMessage = (id: number) => {
    setExpandedMessages(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  /* ---- Update error group status ---- */
  const updateStatus = async (id: number, newStatus: string, e: React.MouseEvent) => {
    e.stopPropagation()
    e.preventDefault()
    setUpdatingStatus(id)
    try {
      await fetch(`/api/v1/errors/groups/${id}/status`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ status: newStatus }),
      })
      setGroups(prev => prev.map(g => g.id === id ? { ...g, status: newStatus } : g))
      // Also refresh summary
      apiFetch<ErrorSummary>('/api/v1/errors/summary')
        .then(s => setSummary(s))
        .catch(() => {})
    } catch {
      // silently ignore; user can retry
    } finally {
      setUpdatingStatus(null)
    }
  }

  // AI analysis
  const [analyzing, setAnalyzing] = useState(false)
  const [analysisResult, setAnalysisResult] = useState<any>(null)

  const handleRequestAnalysis = async () => {
    setAnalyzing(true)
    setAnalysisResult(null)
    try {
      const res = await fetch('/api/v1/errors/analyze', { method: 'POST' })
      if (!res.ok) {
        const text = await res.text()
        setAnalysisResult({ error: text })
        return
      }
      const data = await res.json()
      setAnalysisResult(data)
      // Refresh groups to pick up aiSummary
      fetchGroups()
    } catch (e: any) {
      setAnalysisResult({ error: e.message })
    } finally {
      setAnalyzing(false)
    }
  }

  /* ================================================================== */
  /*  Render                                                             */
  /* ================================================================== */

  return (
    <div className="p-6 max-w-[1400px] mx-auto">

      {/* ---- Header ---- */}
      <div className="flex items-center justify-between mb-6">
        <div className="flex items-center gap-3">
          <ShieldAlert className="w-7 h-7 text-red-400" />
          <h1 className="text-2xl font-bold text-white">Error Analysis</h1>
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={handleRequestAnalysis}
            disabled={analyzing}
            className="flex items-center gap-2 px-3 py-1.5 text-sm bg-purple-600/20 hover:bg-purple-600/30 border border-purple-500/30 text-purple-300 rounded-lg transition-colors disabled:opacity-50"
            title="Request AI analysis of all open error groups"
          >
            {analyzing ? <Loader2 className="w-4 h-4 animate-spin" /> : <Sparkles className="w-4 h-4" />}
            {analyzing ? 'Analyzing...' : 'Request AI Analysis'}
          </button>
          <button
            onClick={fetchGroups}
            className="flex items-center gap-2 px-3 py-1.5 text-sm bg-cluster-border/40 hover:bg-cluster-border/60 rounded-lg text-cluster-text transition-colors"
          >
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
            Refresh
          </button>
        </div>
      </div>

      {/* ---- Summary cards ---- */}
      {summary && (
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
          <SummaryCard
            icon={<Layers className="w-4 h-4 text-blue-400" />}
            label="Total Error Groups"
            value={summary.totalGroups}
            accent="blue"
          />
          <SummaryCard
            icon={<Hash className="w-4 h-4 text-amber-400" />}
            label="Total Occurrences"
            value={summary.totalOccurrences}
            accent="amber"
          />
          <SummaryCard
            icon={<CircleDot className="w-4 h-4 text-red-400" />}
            label="Open Errors"
            value={summary.openCount}
            accent="red"
          />
          <div className="p-4 bg-gray-800/50 border border-gray-700/50 rounded-lg">
            <div className="flex items-center gap-1.5 text-xs text-gray-400 mb-1">
              <Activity className="w-4 h-4 text-purple-400" />
              Most Common Reason
            </div>
            <div className="text-lg font-bold text-white font-mono truncate" title={topReason}>
              {topReason}
            </div>
          </div>
        </div>
      )}

      {/* ---- Error distribution chart ---- */}
      {reasonChartData.length > 0 && (
        <div className="mb-6 p-4 bg-gray-800/50 border border-gray-700/50 rounded-lg">
          <h2 className="text-sm font-medium text-gray-300 uppercase tracking-wider mb-3">
            Error Distribution by Reason
          </h2>
          <div className="h-56">
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={reasonChartData} margin={{ top: 4, right: 16, bottom: 4, left: 0 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                <XAxis
                  dataKey="reason"
                  tick={{ fill: '#9ca3af', fontSize: 11 }}
                  interval={0}
                  angle={-30}
                  textAnchor="end"
                  height={60}
                />
                <YAxis
                  tick={{ fill: '#9ca3af', fontSize: 11 }}
                  allowDecimals={false}
                />
                <Tooltip content={<ReasonTooltip />} cursor={{ fill: 'rgba(255,255,255,0.04)' }} />
                <Bar dataKey="count" radius={[4, 4, 0, 0]} maxBarSize={48}>
                  {reasonChartData.map((entry, i) => (
                    <Cell key={i} fill={reasonColor(entry.reason)} />
                  ))}
                </Bar>
              </BarChart>
            </ResponsiveContainer>
          </div>
        </div>
      )}

      {/* ---- Top services table ---- */}
      {summary && summary.topServices && summary.topServices.length > 0 && (
        <div className="mb-6 p-4 bg-gray-800/50 border border-gray-700/50 rounded-lg">
          <h2 className="text-sm font-medium text-gray-300 uppercase tracking-wider mb-3">
            Top Services by Error Count
          </h2>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-gray-700">
                  <th className="text-left py-2 px-3 text-xs text-gray-500 font-medium uppercase">Service</th>
                  <th className="text-right py-2 px-3 text-xs text-gray-500 font-medium uppercase">Occurrences</th>
                  <th className="text-left py-2 px-3 text-xs text-gray-500 font-medium uppercase">Top Reason</th>
                </tr>
              </thead>
              <tbody>
                {summary.topServices.map((svc, i) => {
                  // Find the most common reason for this service from topGroups
                  const svcGroups = (summary.topGroups || []).filter(g => g.service === svc.service)
                  const topSvcReason = svcGroups.length > 0 ? svcGroups[0].reason : '-'
                  return (
                    <tr key={i} className="border-b border-gray-700/30 hover:bg-gray-700/20">
                      <td className="py-2 px-3">
                        <span className="font-mono text-white">{svc.service}</span>
                      </td>
                      <td className="py-2 px-3 text-right">
                        <span className="font-medium text-amber-300">{svc.count.toLocaleString()}</span>
                      </td>
                      <td className="py-2 px-3">
                        <span className="text-xs font-mono px-1.5 py-0.5 rounded bg-gray-700 text-gray-300">
                          {topSvcReason}
                        </span>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* ---- Filters ---- */}
      <div className="flex flex-wrap items-center gap-3 mb-4">
        <select
          value={statusFilter}
          onChange={e => { setOffset(0); setStatusFilter(e.target.value) }}
          className="bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors"
        >
          <option value="">All statuses</option>
          <option value="open">Open</option>
          <option value="resolved">Resolved</option>
          <option value="ignored">Ignored</option>
        </select>
        <select
          value={serviceFilter}
          onChange={e => { setOffset(0); setServiceFilter(e.target.value) }}
          className="bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors"
        >
          <option value="">All services</option>
          {services.map(s => <option key={s} value={s}>{s}</option>)}
        </select>
        <div className="relative flex-1 max-w-sm">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500" />
          <input
            type="text"
            placeholder="Search errors..."
            value={search}
            onChange={e => { setOffset(0); setSearch(e.target.value) }}
            className="w-full pl-9 pr-3 py-2 bg-gray-800 border border-gray-700 rounded-lg text-sm text-white placeholder-gray-500 focus:outline-none focus:border-blue-500 transition-colors"
          />
        </div>
        <select
          value={sortBy}
          onChange={e => { setOffset(0); setSortBy(e.target.value as any) }}
          className="bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors"
          title="Sort groups by"
        >
          <option value="lastSeen">Sort: Last seen</option>
          <option value="count">Sort: Total count</option>
          <option value="rate5m">Sort: Rate (5m)</option>
          <option value="severity">Sort: Severity</option>
        </select>
        <select
          value={pageSize}
          onChange={e => { setOffset(0); setPageSize(parseInt(e.target.value, 10)) }}
          className="bg-gray-800 border border-gray-700 rounded-lg px-2 py-2 text-sm text-white focus:outline-none focus:border-blue-500"
          title="Page size"
        >
          <option value="25">25 / page</option>
          <option value="50">50 / page</option>
          <option value="100">100 / page</option>
          <option value="0">All</option>
        </select>
        <span className="text-sm text-gray-500 tabular-nums">
          {totalCount === 0 ? 0 : offset + 1}–{Math.min(offset + groups.length, totalCount)} of {totalCount}
        </span>
      </div>

      {/* ---- Error banner ---- */}
      {error && (
        <div className="mb-4 p-3 bg-red-900/30 border border-red-700 rounded-lg text-sm text-red-300 flex items-center gap-2">
          <AlertCircle className="w-4 h-4 shrink-0" />
          {error}
        </div>
      )}

      {/* ---- Loading ---- */}
      {loading && (
        <div className="flex justify-center py-16">
          <Loader2 className="w-6 h-6 animate-spin text-blue-400" />
        </div>
      )}

      {/* ---- Error groups list ---- */}
      {!loading && groups.length > 0 && (
        <div className="space-y-3">
          {groups.map(g => {
            const isExpanded = expandedGroups.has(g.id)
            const isMessageExpanded = expandedMessages.has(g.id)
            const occs = groupOccurrences[g.id]
            const isLoadingOccs = loadingOccurrences.has(g.id)

            return (
              <div
                key={g.id}
                className="bg-gray-800/50 border border-gray-700/50 rounded-lg hover:border-gray-600/60 transition-colors"
              >
                {/* ---- Group header ---- */}
                <div
                  className="p-4 cursor-pointer"
                  onClick={() => toggleGroup(g.id, g.fingerprint)}
                >
                  <div className="flex items-start gap-3">
                    {/* Expand icon */}
                    <div className="mt-0.5 shrink-0">
                      {isExpanded
                        ? <ChevronDown className="w-4 h-4 text-gray-500" />
                        : <ChevronRight className="w-4 h-4 text-gray-500" />
                      }
                    </div>

                    {/* Severity icon */}
                    <div className="mt-0.5 shrink-0">{severityIcon(g)}</div>

                    {/* Title + metadata */}
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 mb-1.5 flex-wrap">
                        <SeverityChip level={g.level} />
                        <Link
                          href={`/errors/${g.id}`}
                          onClick={e => e.stopPropagation()}
                          className="text-sm font-medium text-white hover:text-blue-300 hover:underline truncate max-w-[500px]"
                          title={g.title}
                        >
                          {g.title}
                        </Link>
                        {g.exceptionType && (
                          <span className="text-xs text-orange-400/70 font-mono">{g.exceptionType}</span>
                        )}
                        {isSpike(g.rate) && <SpikeBadge />}
                      </div>
                      <div className="flex items-center gap-2 flex-wrap text-xs">
                        <span className="px-1.5 py-0.5 bg-blue-900/30 text-blue-300 rounded border border-blue-700/30">
                          {g.service}
                        </span>
                        <span className="px-1.5 py-0.5 bg-gray-700/50 text-gray-400 rounded">
                          {g.namespace}
                        </span>
                        <span
                          className="px-1.5 py-0.5 rounded font-mono"
                          style={{ backgroundColor: reasonColor(g.reason) + '20', color: reasonColor(g.reason) }}
                        >
                          {g.reason}
                        </span>
                        <span className="text-gray-600">|</span>
                        <span className="text-gray-500 flex items-center gap-1">
                          <Clock className="w-3 h-3" />
                          first {timeSince(g.firstSeen)}
                        </span>
                        <span className="text-gray-500 flex items-center gap-1">
                          <Clock className="w-3 h-3" />
                          last {timeSince(g.lastSeen)}
                        </span>
                        {g.rate && (g.rate.count5m > 0 || g.rate.count1h > 0) && (
                          <>
                            <span className="text-gray-600">|</span>
                            <span
                              className="text-gray-400 font-mono tabular-nums"
                              title={`${g.rate.count1m} / min · ${g.rate.count5m} in 5m · ${g.rate.count1h} in 1h · ${g.rate.count24h} in 24h${g.rate.truncated ? ' (≥ — ring buffer full)' : ''}`}
                            >
                              <span className="text-amber-300">{g.rate.count5m}</span>
                              <span className="text-gray-600">/5m</span>
                              <span className="mx-1 text-gray-600">·</span>
                              <span className="text-amber-200">{g.rate.count1h}</span>
                              <span className="text-gray-600">/1h</span>
                            </span>
                          </>
                        )}
                      </div>
                    </div>

                    {/* Sparkline (last-hour 5-min buckets) */}
                    {g.rate && g.rate.spark && (
                      <div className="shrink-0 self-center px-1" aria-hidden={false}>
                        <Sparkline spark={g.rate.spark} spike={isSpike(g.rate)} truncated={g.rate.truncated} />
                      </div>
                    )}

                    {/* Count badge (prominent) */}
                    <div className="shrink-0 flex flex-col items-end gap-1.5">
                      <span className="px-2.5 py-1 text-sm font-bold bg-amber-500/20 text-amber-300 border border-amber-500/30 rounded-lg tabular-nums">
                        {g.count.toLocaleString()}
                      </span>
                    </div>

                    {/* Status badge with click-to-change */}
                    <div className="shrink-0" onClick={e => e.stopPropagation()}>
                      {updatingStatus === g.id ? (
                        <Loader2 className="w-4 h-4 animate-spin text-gray-400" />
                      ) : (
                        <StatusDropdown
                          status={g.status}
                          onChangeStatus={(newStatus) => updateStatus(g.id, newStatus, { stopPropagation: () => {}, preventDefault: () => {} } as any)}
                        />
                      )}
                    </div>
                  </div>
                </div>

                {/* ---- Expanded section ---- */}
                {isExpanded && (
                  <div className="border-t border-gray-700/50 px-4 pb-4 pt-3 ml-11 space-y-3">

                    {/* Sample message */}
                    {g.sampleMessage && (
                      <div>
                        <button
                          onClick={(e) => { e.stopPropagation(); toggleMessage(g.id) }}
                          className="text-xs text-cluster-muted uppercase tracking-wider mb-1 flex items-center gap-1 hover:text-cluster-text"
                        >
                          Sample Message
                          {isMessageExpanded
                            ? <ChevronDown className="w-3 h-3" />
                            : <ChevronRight className="w-3 h-3" />
                          }
                        </button>
                        <pre className={`text-xs font-mono text-gray-300 bg-gray-900/60 border border-gray-700/50 rounded-lg p-3 whitespace-pre-wrap ${
                          isMessageExpanded ? 'max-h-80' : 'max-h-20'
                        } overflow-auto transition-all`}>
                          {g.sampleMessage}
                        </pre>
                      </div>
                    )}

                    {/* Stack trace (if present and different from message) */}
                    {g.sampleStack && (
                      <div>
                        <div className="text-xs text-gray-500 uppercase tracking-wider mb-1">
                          Stack Trace
                        </div>
                        <pre className="text-xs font-mono text-gray-400 bg-gray-900/60 border border-gray-700/50 rounded-lg p-3 whitespace-pre-wrap max-h-48 overflow-auto">
                          {g.sampleStack}
                        </pre>
                      </div>
                    )}

                    {/* AI Analysis section */}
                    <div className="p-3 rounded-lg border bg-gray-900/40 border-gray-700/40">
                      <div className="flex items-center gap-2 mb-2">
                        <Bot className="w-4 h-4 text-purple-400" />
                        <span className="text-xs font-medium text-purple-300 uppercase tracking-wider">
                          AI Analysis
                        </span>
                      </div>
                      {g.aiSummary ? (
                        <div>
                          <div className="text-sm text-gray-300 leading-relaxed whitespace-pre-wrap">
                            {g.aiSummary.split('\n').map((line: string, i: number) => {
                              // Simple markdown bold rendering
                              const parts = line.split(/(\*\*[^*]+\*\*)/g)
                              return (
                                <p key={i} className={line === '' ? 'h-2' : ''}>
                                  {parts.map((part, j) =>
                                    part.startsWith('**') && part.endsWith('**')
                                      ? <strong key={j} className="text-gray-100">{part.slice(2, -2)}</strong>
                                      : part
                                  )}
                                </p>
                              )
                            })}
                          </div>
                          {llmConfig && llmConfig.provider && llmConfig.model && (
                            <div className="mt-2 pt-2 border-t border-gray-700/30 text-xs text-gray-600 flex items-center gap-1">
                              <Sparkles className="w-3 h-3" />
                              Analyzed by {llmConfig.provider}/{llmConfig.model}
                            </div>
                          )}
                        </div>
                      ) : (
                        <div className="flex items-center justify-between">
                          <p className="text-xs text-gray-500">
                            AI analysis pending. Use the &quot;Request AI Analysis&quot; button to analyze this error group.
                          </p>
                          <button
                            onClick={(e) => { e.stopPropagation(); handleRequestAnalysis() }}
                            disabled={analyzing}
                            className="flex items-center gap-1.5 px-2.5 py-1 text-xs bg-purple-600/20 hover:bg-purple-600/30 border border-purple-500/30 text-purple-300 rounded transition-colors shrink-0 ml-3 disabled:opacity-50"
                          >
                            {analyzing ? <Loader2 className="w-3 h-3 animate-spin" /> : <Sparkles className="w-3 h-3" />}
                            {analyzing ? 'Analyzing...' : 'Analyze'}
                          </button>
                        </div>
                      )}
                    </div>

                    {/* Recent Occurrences */}
                    <div>
                      <div className="text-xs text-gray-500 uppercase tracking-wider mb-2">
                        Recent Occurrences
                      </div>
                      {isLoadingOccs ? (
                        <div className="flex items-center gap-2 py-3 text-xs text-gray-500">
                          <Loader2 className="w-3 h-3 animate-spin" />
                          Loading occurrences...
                        </div>
                      ) : occs && occs.length > 0 ? (
                        <div className="space-y-1">
                          {occs.map((occ, i) => (
                            <div
                              key={i}
                              className="flex items-start gap-3 p-2 bg-gray-800/30 border border-gray-700/20 rounded text-xs"
                            >
                              <span className="text-gray-500 shrink-0 w-32 tabular-nums">
                                {new Date(occ.timestamp).toLocaleString()}
                              </span>
                              <span className="text-gray-400 shrink-0 font-mono truncate max-w-[180px]" title={occ.pod}>
                                {occ.pod}
                              </span>
                              <span className="text-gray-300 flex-1 truncate">{occ.message}</span>
                              {occ.url && (
                                <span className="text-gray-600 font-mono shrink-0 truncate max-w-[200px]" title={occ.url}>
                                  {occ.url}
                                </span>
                              )}
                            </div>
                          ))}
                        </div>
                      ) : (
                        <div className="text-xs text-gray-600 py-2">No occurrences stored</div>
                      )}
                      <Link
                        href={`/errors/${g.id}`}
                        className="inline-block mt-2 text-xs text-blue-400 hover:text-blue-300 hover:underline"
                        onClick={e => e.stopPropagation()}
                      >
                        View full detail &rarr;
                      </Link>
                    </div>

                  </div>
                )}
              </div>
            )
          })}
        </div>
      )}

      {/* ---- Pagination controls ---- */}
      {!loading && groups.length > 0 && pageSize > 0 && totalCount > pageSize && (
        <div className="flex items-center justify-between mt-4 px-1">
          <button
            onClick={() => setOffset(Math.max(0, offset - pageSize))}
            disabled={offset === 0}
            className="px-3 py-1.5 text-sm bg-gray-800 border border-gray-700 rounded-lg text-gray-300 hover:bg-gray-700 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
          >
            ← Previous
          </button>
          <span className="text-sm text-gray-500 tabular-nums font-mono">
            Page {Math.floor(offset / pageSize) + 1} of {Math.max(1, Math.ceil(totalCount / pageSize))}
          </span>
          <button
            onClick={() => setOffset(offset + pageSize)}
            disabled={offset + pageSize >= totalCount}
            className="px-3 py-1.5 text-sm bg-gray-800 border border-gray-700 rounded-lg text-gray-300 hover:bg-gray-700 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
          >
            Next →
          </button>
        </div>
      )}

      {/* ---- Empty state ---- */}
      {!loading && groups.length === 0 && !error && (
        <div className="text-center py-16">
          <ShieldAlert className="w-10 h-10 text-gray-600 mx-auto mb-3" />
          <p className="text-gray-500">
            No error groups found. Errors will appear here when the pod log collector detects exceptions, timeouts, or other issues.
          </p>
        </div>
      )}
    </div>
  )
}

/* ------------------------------------------------------------------ */
/*  Sub-components                                                     */
/* ------------------------------------------------------------------ */

function SummaryCard({
  icon, label, value, accent,
}: {
  icon: React.ReactNode
  label: string
  value: number
  accent: 'blue' | 'amber' | 'red' | 'green'
}) {
  const accentMap = {
    blue: 'border-blue-700/30 bg-blue-900/10',
    amber: 'border-amber-700/30 bg-amber-900/10',
    red: 'border-red-700/30 bg-red-900/10',
    green: 'border-green-700/30 bg-green-900/10',
  }
  const textMap = {
    blue: 'text-blue-300',
    amber: 'text-amber-300',
    red: 'text-red-300',
    green: 'text-green-300',
  }
  return (
    <div className={`p-4 rounded-lg border ${accentMap[accent]}`}>
      <div className="flex items-center gap-1.5 text-xs text-gray-400 mb-1">
        {icon}
        {label}
      </div>
      <div className={`text-2xl font-bold ${textMap[accent]} tabular-nums`}>
        {value.toLocaleString()}
      </div>
    </div>
  )
}

function StatusDropdown({
  status,
  onChangeStatus,
}: {
  status: string
  onChangeStatus: (newStatus: string) => void
}) {
  const [open, setOpen] = useState(false)
  const options = ['open', 'resolved', 'ignored'].filter(s => s !== status)

  return (
    <div className="relative">
      <button
        onClick={() => setOpen(!open)}
        className={`px-2 py-0.5 text-xs rounded border flex items-center gap-1 ${statusColor(status)}`}
      >
        {status === 'open' && <CircleDot className="w-3 h-3" />}
        {status === 'resolved' && <CheckCircle className="w-3 h-3" />}
        {status === 'ignored' && <EyeOff className="w-3 h-3" />}
        {status}
        <ChevronDown className="w-3 h-3" />
      </button>
      {open && (
        <>
          <div className="fixed inset-0 z-10" onClick={() => setOpen(false)} />
          <div className="absolute right-0 top-full mt-1 bg-gray-800 border border-gray-700 rounded-lg shadow-xl z-20 py-1 min-w-[110px]">
            {options.map(opt => (
              <button
                key={opt}
                onClick={() => { onChangeStatus(opt); setOpen(false) }}
                className="w-full text-left px-3 py-1.5 text-xs text-gray-300 hover:bg-gray-700/50 flex items-center gap-2"
              >
                {opt === 'open' && <CircleDot className="w-3 h-3 text-red-400" />}
                {opt === 'resolved' && <CheckCircle className="w-3 h-3 text-green-400" />}
                {opt === 'ignored' && <EyeOff className="w-3 h-3 text-gray-500" />}
                {opt.charAt(0).toUpperCase() + opt.slice(1)}
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  )
}

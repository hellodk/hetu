'use client'

import { useEffect, useState, useCallback, useRef, useMemo } from 'react'
import { apiFetch } from '@/lib/api'
import {
  AreaChart, Area, LineChart, Line, XAxis, YAxis,
  CartesianGrid, Tooltip, ResponsiveContainer, Legend
} from 'recharts'
import {
  RefreshCw, Loader2, Activity, AlertTriangle, Clock,
  ArrowUpRight, Search, Globe, Users, Zap, ChevronDown,
  ChevronUp, Shield, Network, ExternalLink, Lock, Unlock,
  ArrowUpDown, Filter
} from 'lucide-react'

/* ------------------------------------------------------------------ */
/*  Type definitions                                                   */
/* ------------------------------------------------------------------ */

interface LBInfo { name: string; type: string }

interface LBStatsData {
  lbName: string; totalRequests: number
  count2xx: number; count4xx: number; count5xx: number
  p50Ms: number; p95Ms: number; p99Ms: number; avgMs: number
}

interface URLStatsRow {
  urlPattern: string; httpMethod: string; totalCount: number
  count5xx: number; count4xx: number; p95Ms: number; p99Ms: number
}

interface TimelineBucket {
  minute: string; total: number
  count2xx: number; count4xx: number; count5xx: number
  errorRate: number; p50Ms: number; p95Ms: number
}

interface ErrorRow {
  urlPattern: string; httpMethod: string
  total: number; count5xx: number; count4xx: number; errorRate: number
}

interface SlowRequest {
  ts: string; urlPattern: string; httpMethod: string
  elbStatus: number; targetStatus: number; targetMs: number
  targetGroup: string; clientIp: string
}

interface ClientRow {
  ip: string; count: number; count5xx: number; lastSeen: string
}

interface SearchResult {
  ts: string; urlPattern: string; httpMethod: string
  elbStatus: number; targetStatus: number; targetMs: number
  targetGroup: string; clientIp: string
}

interface IngressRule {
  host: string; path: string; pathType: string
  serviceName: string; servicePort: string | number
}

interface IngressEntry {
  namespace: string; name: string; ingressClass: string
  hosts: string[]; rules: IngressRule[]; tls: boolean
  loadBalancer: string; createdAt: string
}

type TabKey = 'top-urls' | 'errors' | 'slow' | 'clients' | 'search' | 'ingress'
type SortDir = 'asc' | 'desc'

/* ------------------------------------------------------------------ */
/*  Helpers                                                            */
/* ------------------------------------------------------------------ */

const fmtMs = (ms: number) => ms < 1 ? '<1ms' : `${Math.round(ms)}ms`

const fmtPct = (n: number, total: number) =>
  total === 0 ? '0.00%' : `${((n / total) * 100).toFixed(2)}%`

const fmtTime = (iso: string) => {
  try {
    const d = new Date(iso)
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  } catch { return iso }
}

const fmtTimestamp = (iso: string) => {
  try {
    const d = new Date(iso)
    return d.toLocaleString([], {
      month: 'short', day: 'numeric',
      hour: '2-digit', minute: '2-digit', second: '2-digit',
    })
  } catch { return iso }
}

const timeSince = (iso: string) => {
  const ms = Date.now() - new Date(iso).getTime()
  if (ms < 60000) return `${Math.floor(ms / 1000)}s ago`
  if (ms < 3600000) return `${Math.floor(ms / 60000)}m ago`
  if (ms < 86400000) return `${Math.floor(ms / 3600000)}h ago`
  return `${Math.floor(ms / 86400000)}d ago`
}

const latencyColor = (ms: number) => {
  if (ms > 1000) return 'text-red-400 font-semibold'
  if (ms > 500) return 'text-yellow-400 font-medium'
  return 'text-slate-300'
}

const statusColor = (code: number) => {
  if (code >= 500) return 'text-red-400'
  if (code >= 400) return 'text-yellow-400'
  if (code >= 200 && code < 300) return 'text-green-400'
  return 'text-slate-300'
}

const methodBadge = (method: string) => {
  const colors: Record<string, string> = {
    GET: 'bg-blue-900/40 text-blue-300',
    POST: 'bg-green-900/40 text-green-300',
    PUT: 'bg-yellow-900/40 text-yellow-300',
    PATCH: 'bg-orange-900/40 text-orange-300',
    DELETE: 'bg-red-900/40 text-red-300',
  }
  return colors[method] || 'bg-gray-700 text-gray-300'
}

/* ------------------------------------------------------------------ */
/*  Main component                                                     */
/* ------------------------------------------------------------------ */

export default function LBLogsPage() {
  // LB list & selection
  const [lbs, setLbs] = useState<LBInfo[]>([])
  const [selectedLB, setSelectedLB] = useState('')
  const [autoRefresh, setAutoRefresh] = useState(false)
  const refreshRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // Data state
  const [stats, setStats] = useState<LBStatsData | null>(null)
  const [timeline, setTimeline] = useState<TimelineBucket[]>([])
  const [urls, setUrls] = useState<URLStatsRow[]>([])
  const [errors, setErrors] = useState<ErrorRow[]>([])
  const [slowReqs, setSlowReqs] = useState<SlowRequest[]>([])
  const [clients, setClients] = useState<ClientRow[]>([])
  const [searchResults, setSearchResults] = useState<SearchResult[]>([])
  const [searchTotal, setSearchTotal] = useState(0)
  const [ingresses, setIngresses] = useState<IngressEntry[]>([])

  // UI state
  const [loading, setLoading] = useState(true)
  const [tabLoading, setTabLoading] = useState(false)
  const [activeTab, setActiveTab] = useState<TabKey>('top-urls')
  const [error, setError] = useState<string | null>(null)

  // Sort state for Top URLs
  const [urlSortKey, setUrlSortKey] = useState<keyof URLStatsRow>('totalCount')
  const [urlSortDir, setUrlSortDir] = useState<SortDir>('desc')

  // Search filters
  const [searchStatus, setSearchStatus] = useState('all')
  const [searchUrl, setSearchUrl] = useState('')
  const [searchMinLatency, setSearchMinLatency] = useState('')
  const [searching, setSearching] = useState(false)

  // Ingress expanded row
  const [expandedIngress, setExpandedIngress] = useState<string | null>(null)

  /* ---- Load LB list ---- */
  useEffect(() => {
    apiFetch<{ loadBalancers: LBInfo[] }>('/api/v1/lb/list')
      .then(d => {
        const list = d.loadBalancers || []
        setLbs(list)
        if (list.length > 0) setSelectedLB(list[0].name)
      })
      .catch(() => setLbs([]))
      .finally(() => setLoading(false))
  }, [])

  /* ---- Fetch overview data (stats + timeline) ---- */
  const fetchOverview = useCallback(async () => {
    if (!selectedLB) return
    setError(null)
    try {
      const [s, t] = await Promise.all([
        apiFetch<LBStatsData>(`/api/v1/lb/${selectedLB}/stats`),
        apiFetch<{ buckets: TimelineBucket[] }>(`/api/v1/lb/${selectedLB}/timeline?minutes=60`),
      ])
      setStats(s)
      setTimeline(t.buckets || [])
    } catch (e: any) {
      setStats(null)
      setTimeline([])
      setError(e.message)
    }
  }, [selectedLB])

  /* ---- Fetch tab-specific data ---- */
  const fetchTabData = useCallback(async (tab: TabKey) => {
    if (!selectedLB && tab !== 'ingress') return
    setTabLoading(true)
    try {
      switch (tab) {
        case 'top-urls': {
          const d = await apiFetch<{ urls: URLStatsRow[] }>(`/api/v1/lb/${selectedLB}/top-urls`)
          setUrls(d.urls || [])
          break
        }
        case 'errors': {
          const d = await apiFetch<{ errors: ErrorRow[] }>(`/api/v1/lb/${selectedLB}/errors?minutes=60`)
          setErrors(d.errors || [])
          break
        }
        case 'slow': {
          const d = await apiFetch<{ requests: SlowRequest[] }>(`/api/v1/lb/${selectedLB}/slow?limit=50`)
          setSlowReqs(d.requests || [])
          break
        }
        case 'clients': {
          const d = await apiFetch<{ clients: ClientRow[] }>(`/api/v1/lb/${selectedLB}/clients?limit=50`)
          setClients(d.clients || [])
          break
        }
        case 'ingress': {
          const d = await apiFetch<{ totalCount: number; ingresses: IngressEntry[] }>('/api/v1/ingress')
          setIngresses(d.ingresses || [])
          break
        }
        // search is triggered manually
      }
    } catch {
      // silent fail per tab
    } finally {
      setTabLoading(false)
    }
  }, [selectedLB])

  /* ---- On LB change reload overview + active tab ---- */
  useEffect(() => {
    if (!selectedLB) return
    setLoading(true)
    Promise.all([fetchOverview(), fetchTabData(activeTab)])
      .finally(() => setLoading(false))
  }, [selectedLB]) // eslint-disable-line react-hooks/exhaustive-deps

  /* ---- On tab change fetch tab data ---- */
  useEffect(() => {
    fetchTabData(activeTab)
  }, [activeTab, fetchTabData])

  /* ---- Auto-refresh ---- */
  useEffect(() => {
    if (refreshRef.current) clearInterval(refreshRef.current)
    if (autoRefresh && selectedLB) {
      refreshRef.current = setInterval(() => {
        fetchOverview()
        fetchTabData(activeTab)
      }, 10000)
    }
    return () => { if (refreshRef.current) clearInterval(refreshRef.current) }
  }, [autoRefresh, selectedLB, activeTab, fetchOverview, fetchTabData])

  /* ---- Manual refresh ---- */
  const handleRefresh = () => {
    setLoading(true)
    Promise.all([fetchOverview(), fetchTabData(activeTab)])
      .finally(() => setLoading(false))
  }

  /* ---- Search ---- */
  const runSearch = async () => {
    if (!selectedLB) return
    setSearching(true)
    try {
      const params = new URLSearchParams()
      if (searchStatus !== 'all') params.set('status', searchStatus)
      if (searchUrl.trim()) params.set('url', searchUrl.trim())
      if (searchMinLatency.trim()) params.set('min_latency', searchMinLatency.trim())
      const qs = params.toString()
      const d = await apiFetch<{ requests: SearchResult[]; total: number }>(
        `/api/v1/lb/${selectedLB}/search${qs ? '?' + qs : ''}`
      )
      setSearchResults(d.requests || [])
      setSearchTotal(d.total || 0)
    } catch {
      setSearchResults([])
      setSearchTotal(0)
    } finally {
      setSearching(false)
    }
  }

  /* ---- Sorted URLs ---- */
  const sortedUrls = useMemo(() => {
    const sorted = [...urls].sort((a, b) => {
      const av = a[urlSortKey]
      const bv = b[urlSortKey]
      if (typeof av === 'number' && typeof bv === 'number') {
        return urlSortDir === 'asc' ? av - bv : bv - av
      }
      return urlSortDir === 'asc'
        ? String(av).localeCompare(String(bv))
        : String(bv).localeCompare(String(av))
    })
    return sorted
  }, [urls, urlSortKey, urlSortDir])

  const toggleUrlSort = (key: keyof URLStatsRow) => {
    if (urlSortKey === key) {
      setUrlSortDir(d => d === 'asc' ? 'desc' : 'asc')
    } else {
      setUrlSortKey(key)
      setUrlSortDir('desc')
    }
  }

  /* ---- Derived values ---- */
  const errorRate = stats
    ? fmtPct(stats.count4xx + stats.count5xx, stats.totalRequests)
    : '0.00%'

  const topErrorUrl = useMemo(() => {
    if (urls.length === 0) return 'None'
    const sorted = [...urls].sort((a, b) => b.count5xx - a.count5xx)
    return sorted[0]?.count5xx > 0
      ? `${sorted[0].httpMethod} ${sorted[0].urlPattern}`
      : 'None'
  }, [urls])

  /* ---------------------------------------------------------------- */
  /*  RENDER                                                           */
  /* ---------------------------------------------------------------- */

  const TABS: { key: TabKey; label: string; icon: React.ReactNode }[] = [
    { key: 'top-urls', label: 'Top URLs', icon: <Globe className="w-3.5 h-3.5" /> },
    { key: 'errors', label: 'Errors', icon: <AlertTriangle className="w-3.5 h-3.5" /> },
    { key: 'slow', label: 'Slow Requests', icon: <Clock className="w-3.5 h-3.5" /> },
    { key: 'clients', label: 'Client IPs', icon: <Users className="w-3.5 h-3.5" /> },
    { key: 'search', label: 'Search', icon: <Search className="w-3.5 h-3.5" /> },
    { key: 'ingress', label: 'Ingress', icon: <Network className="w-3.5 h-3.5" /> },
  ]

  return (
    <div className="p-6 min-h-screen">
      {/* ================= HEADER ================= */}
      <div className="flex items-center justify-between mb-6">
        <div className="flex items-center gap-3">
          <Zap className="w-6 h-6 text-blue-400" />
          <h1 className="text-2xl font-bold text-white">Load Balancer Analytics</h1>
        </div>
        <div className="flex items-center gap-3">
          {lbs.length > 0 && (
            <select
              value={selectedLB}
              onChange={e => setSelectedLB(e.target.value)}
              className="bg-cluster-card border border-cluster-border rounded-lg px-3 py-1.5 text-sm text-white focus:outline-none focus:ring-1 focus:ring-blue-500"
            >
              {lbs.map(lb => (
                <option key={lb.name} value={lb.name}>
                  {lb.name} ({lb.type})
                </option>
              ))}
            </select>
          )}
          <label className="flex items-center gap-2 text-sm text-slate-400 cursor-pointer select-none">
            <input
              type="checkbox"
              checked={autoRefresh}
              onChange={e => setAutoRefresh(e.target.checked)}
              className="accent-blue-500"
            />
            Auto (10s)
          </label>
          <button
            onClick={handleRefresh}
            className="flex items-center gap-2 px-3 py-1.5 text-sm bg-cluster-border/40 hover:bg-cluster-border/60 rounded-lg text-cluster-text transition-colors"
          >
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
            Refresh
          </button>
        </div>
      </div>

      {/* ================= ERROR BANNER ================= */}
      {error && (
        <div className="mb-4 p-3 bg-red-900/20 border border-red-700/50 rounded-lg text-red-300 text-sm flex items-center gap-2">
          <AlertTriangle className="w-4 h-4 flex-shrink-0" />
          {error}
        </div>
      )}

      {/* ================= INITIAL LOADING ================= */}
      {loading && !stats && lbs.length === 0 && (
        <div className="flex justify-center py-20">
          <Loader2 className="w-8 h-8 animate-spin text-blue-400" />
        </div>
      )}

      {/* ================= EMPTY STATE ================= */}
      {lbs.length === 0 && !loading && (
        <div className="text-center py-20">
          <Network className="w-12 h-12 text-slate-600 mx-auto mb-4" />
          <p className="text-slate-400 text-lg mb-2">No load balancers configured</p>
          <p className="text-slate-500 text-sm mb-4">
            Define your load balancer sources in Settings, then apply the generated{' '}
            <code className="bg-slate-800 px-1.5 py-0.5 rounded text-xs">LB_CONFIGS</code>{' '}
            env var to the collector-lblogs service.
          </p>
          <a
            href="/settings#lb-config"
            className="inline-flex items-center gap-1.5 px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 text-white rounded-lg transition-colors"
          >
            <ExternalLink className="w-3.5 h-3.5" />
            Configure in Settings
          </a>
        </div>
      )}

      {stats && (
        <>
          {/* ================= OVERVIEW CARDS ================= */}
          <div className="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-6">
            <OverviewCard
              icon={<Activity className="w-4 h-4 text-blue-400" />}
              label="Total Requests"
              value={stats.totalRequests.toLocaleString()}
            />
            <OverviewCard
              icon={<AlertTriangle className="w-4 h-4 text-red-400" />}
              label="Error Rate"
              value={errorRate}
              alert={(stats.count4xx + stats.count5xx) > 0}
              sub={`${stats.count5xx.toLocaleString()} 5xx / ${stats.count4xx.toLocaleString()} 4xx`}
            />
            <OverviewCard
              icon={<Clock className="w-4 h-4 text-yellow-400" />}
              label="p95 Latency"
              value={fmtMs(stats.p95Ms)}
              sub={`p50: ${fmtMs(stats.p50Ms)} / p99: ${fmtMs(stats.p99Ms)}`}
            />
            <OverviewCard
              icon={<ArrowUpRight className="w-4 h-4 text-purple-400" />}
              label="Top Error URL"
              value={topErrorUrl.length > 40 ? topErrorUrl.slice(0, 40) + '...' : topErrorUrl}
              mono
            />
          </div>

          {/* ================= TRAFFIC TIMELINE ================= */}
          <div className="bg-cluster-card border border-cluster-border rounded-lg p-5 mb-6">
            <h2 className="text-sm font-semibold text-white mb-4 flex items-center gap-2">
              <Activity className="w-4 h-4 text-blue-400" />
              Traffic Timeline (last 60 min)
            </h2>
            {timeline.length === 0 ? (
              <div className="text-center py-12 text-slate-500 text-sm">No timeline data available</div>
            ) : (
              <ResponsiveContainer width="100%" height={260}>
                <AreaChart data={timeline}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#334155" />
                  <XAxis
                    dataKey="minute"
                    tickFormatter={fmtTime}
                    stroke="#64748b"
                    fontSize={11}
                    tick={{ fill: '#94a3b8' }}
                  />
                  <YAxis
                    stroke="#64748b"
                    fontSize={11}
                    tick={{ fill: '#94a3b8' }}
                  />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: '#1e293b',
                      border: '1px solid #334155',
                      borderRadius: '8px',
                      fontSize: '12px',
                    }}
                    labelFormatter={fmtTime}
                  />
                  <Legend wrapperStyle={{ fontSize: '12px' }} />
                  <Area
                    type="monotone"
                    dataKey="count2xx"
                    stackId="1"
                    stroke="#22c55e"
                    fill="#22c55e"
                    fillOpacity={0.3}
                    name="2xx"
                  />
                  <Area
                    type="monotone"
                    dataKey="count4xx"
                    stackId="1"
                    stroke="#eab308"
                    fill="#eab308"
                    fillOpacity={0.4}
                    name="4xx"
                  />
                  <Area
                    type="monotone"
                    dataKey="count5xx"
                    stackId="1"
                    stroke="#ef4444"
                    fill="#ef4444"
                    fillOpacity={0.5}
                    name="5xx"
                  />
                </AreaChart>
              </ResponsiveContainer>
            )}
          </div>

          {/* ================= LATENCY TIMELINE ================= */}
          <div className="bg-cluster-card border border-cluster-border rounded-lg p-5 mb-6">
            <h2 className="text-sm font-semibold text-white mb-4 flex items-center gap-2">
              <Clock className="w-4 h-4 text-yellow-400" />
              Latency Over Time (last 60 min)
            </h2>
            {timeline.length === 0 ? (
              <div className="text-center py-12 text-slate-500 text-sm">No latency data available</div>
            ) : (
              <ResponsiveContainer width="100%" height={220}>
                <LineChart data={timeline}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#334155" />
                  <XAxis
                    dataKey="minute"
                    tickFormatter={fmtTime}
                    stroke="#64748b"
                    fontSize={11}
                    tick={{ fill: '#94a3b8' }}
                  />
                  <YAxis
                    stroke="#64748b"
                    fontSize={11}
                    tick={{ fill: '#94a3b8' }}
                    unit="ms"
                  />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: '#1e293b',
                      border: '1px solid #334155',
                      borderRadius: '8px',
                      fontSize: '12px',
                    }}
                    labelFormatter={fmtTime}
                    formatter={(value: number) => [`${Math.round(value)}ms`]}
                  />
                  <Legend wrapperStyle={{ fontSize: '12px' }} />
                  <Line
                    type="monotone"
                    dataKey="p50Ms"
                    stroke="#38bdf8"
                    strokeWidth={2}
                    dot={false}
                    name="p50"
                  />
                  <Line
                    type="monotone"
                    dataKey="p95Ms"
                    stroke="#f97316"
                    strokeWidth={2}
                    dot={false}
                    name="p95"
                  />
                </LineChart>
              </ResponsiveContainer>
            )}
          </div>

          {/* ================= TABS ================= */}
          <div className="border-b border-cluster-border mb-4">
            <div className="flex gap-1 overflow-x-auto">
              {TABS.map(tab => (
                <button
                  key={tab.key}
                  onClick={() => setActiveTab(tab.key)}
                  className={`flex items-center gap-1.5 px-4 py-2.5 text-sm font-medium whitespace-nowrap transition-colors border-b-2 -mb-px ${
                    activeTab === tab.key
                      ? 'border-blue-500 text-blue-400'
                      : 'border-transparent text-cluster-muted hover:text-cluster-text hover:border-cluster-border'
                  }`}
                >
                  {tab.icon}
                  {tab.label}
                </button>
              ))}
            </div>
          </div>

          {/* ---- Tab loading indicator ---- */}
          {tabLoading && (
            <div className="flex justify-center py-8">
              <Loader2 className="w-5 h-5 animate-spin text-blue-400" />
            </div>
          )}

          {/* ================= TOP URLs TAB ================= */}
          {activeTab === 'top-urls' && !tabLoading && (
            <SortableTable
              data={sortedUrls}
              emptyMessage="No URL data yet"
              columns={[
                { key: 'httpMethod', label: 'Method', align: 'left', sortable: true },
                { key: 'urlPattern', label: 'URL Pattern', align: 'left', sortable: true },
                { key: 'totalCount', label: 'Total', align: 'right', sortable: true },
                { key: 'count5xx', label: '5xx', align: 'right', sortable: true },
                { key: 'count4xx', label: '4xx', align: 'right', sortable: true },
                { key: 'p95Ms', label: 'p95', align: 'right', sortable: true },
              ]}
              sortKey={urlSortKey}
              sortDir={urlSortDir}
              onSort={toggleUrlSort}
              renderRow={(row, i) => (
                <tr key={i} className="hover:bg-white/5 transition-colors">
                  <td className="px-4 py-2.5">
                    <span className={`px-2 py-0.5 text-xs rounded font-mono ${methodBadge(row.httpMethod)}`}>
                      {row.httpMethod}
                    </span>
                  </td>
                  <td className="px-4 py-2.5 font-mono text-xs text-slate-300 max-w-md truncate">{row.urlPattern}</td>
                  <td className="px-4 py-2.5 text-right text-slate-300">{row.totalCount.toLocaleString()}</td>
                  <td className={`px-4 py-2.5 text-right ${row.count5xx > 0 ? 'text-red-400 font-medium' : 'text-slate-500'}`}>
                    {row.count5xx.toLocaleString()}
                  </td>
                  <td className={`px-4 py-2.5 text-right ${row.count4xx > 0 ? 'text-yellow-400' : 'text-slate-500'}`}>
                    {row.count4xx.toLocaleString()}
                  </td>
                  <td className={`px-4 py-2.5 text-right ${latencyColor(row.p95Ms)}`}>{fmtMs(row.p95Ms)}</td>
                </tr>
              )}
            />
          )}

          {/* ================= ERRORS TAB ================= */}
          {activeTab === 'errors' && !tabLoading && (
            <>
              {errors.length === 0 ? (
                <EmptyState message="No errors in the last 60 minutes" />
              ) : (
                <div className="overflow-x-auto rounded-lg border border-cluster-border">
                  <table className="w-full text-sm">
                    <thead className="bg-cluster-card">
                      <tr>
                        <th className="text-left px-4 py-2.5 text-slate-400 font-medium">Method</th>
                        <th className="text-left px-4 py-2.5 text-slate-400 font-medium">URL Pattern</th>
                        <th className="text-right px-4 py-2.5 text-slate-400 font-medium">Total</th>
                        <th className="text-right px-4 py-2.5 text-slate-400 font-medium">5xx</th>
                        <th className="text-right px-4 py-2.5 text-slate-400 font-medium">4xx</th>
                        <th className="text-right px-4 py-2.5 text-slate-400 font-medium">Error Rate</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-cluster-border">
                      {errors.map((row, i) => (
                        <tr key={i} className="hover:bg-white/5 transition-colors">
                          <td className="px-4 py-2.5">
                            <span className={`px-2 py-0.5 text-xs rounded font-mono ${methodBadge(row.httpMethod)}`}>
                              {row.httpMethod}
                            </span>
                          </td>
                          <td className="px-4 py-2.5 font-mono text-xs text-slate-300 max-w-md truncate">{row.urlPattern}</td>
                          <td className="px-4 py-2.5 text-right text-slate-300">{row.total.toLocaleString()}</td>
                          <td className={`px-4 py-2.5 text-right ${row.count5xx > 0 ? 'text-red-400 font-semibold' : 'text-slate-500'}`}>
                            {row.count5xx.toLocaleString()}
                          </td>
                          <td className={`px-4 py-2.5 text-right ${row.count4xx > 0 ? 'text-yellow-400' : 'text-slate-500'}`}>
                            {row.count4xx.toLocaleString()}
                          </td>
                          <td className="px-4 py-2.5 text-right">
                            <span className={`px-2 py-0.5 rounded text-xs font-medium ${
                              row.errorRate > 50 ? 'bg-red-900/30 text-red-300'
                                : row.errorRate > 10 ? 'bg-yellow-900/30 text-yellow-300'
                                : 'bg-slate-700 text-slate-300'
                            }`}>
                              {row.errorRate.toFixed(1)}%
                            </span>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </>
          )}

          {/* ================= SLOW REQUESTS TAB ================= */}
          {activeTab === 'slow' && !tabLoading && (
            <>
              {slowReqs.length === 0 ? (
                <EmptyState message="No slow requests recorded" />
              ) : (
                <div className="overflow-x-auto rounded-lg border border-cluster-border">
                  <table className="w-full text-sm">
                    <thead className="bg-cluster-card">
                      <tr>
                        <th className="text-left px-4 py-2.5 text-slate-400 font-medium">Timestamp</th>
                        <th className="text-left px-4 py-2.5 text-slate-400 font-medium">Method</th>
                        <th className="text-left px-4 py-2.5 text-slate-400 font-medium">URL Pattern</th>
                        <th className="text-right px-4 py-2.5 text-slate-400 font-medium">ELB Status</th>
                        <th className="text-right px-4 py-2.5 text-slate-400 font-medium">Target Status</th>
                        <th className="text-right px-4 py-2.5 text-slate-400 font-medium">Latency</th>
                        <th className="text-left px-4 py-2.5 text-slate-400 font-medium">Target Group</th>
                        <th className="text-left px-4 py-2.5 text-slate-400 font-medium">Client IP</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-cluster-border">
                      {slowReqs.map((req, i) => (
                        <tr key={i} className="hover:bg-white/5 transition-colors">
                          <td className="px-4 py-2.5 text-slate-400 text-xs whitespace-nowrap">{fmtTimestamp(req.ts)}</td>
                          <td className="px-4 py-2.5">
                            <span className={`px-2 py-0.5 text-xs rounded font-mono ${methodBadge(req.httpMethod)}`}>
                              {req.httpMethod}
                            </span>
                          </td>
                          <td className="px-4 py-2.5 font-mono text-xs text-slate-300 max-w-xs truncate">{req.urlPattern}</td>
                          <td className={`px-4 py-2.5 text-right font-mono ${statusColor(req.elbStatus)}`}>{req.elbStatus}</td>
                          <td className={`px-4 py-2.5 text-right font-mono ${statusColor(req.targetStatus)}`}>{req.targetStatus}</td>
                          <td className={`px-4 py-2.5 text-right font-mono ${latencyColor(req.targetMs)}`}>
                            {fmtMs(req.targetMs)}
                          </td>
                          <td className="px-4 py-2.5 text-xs text-slate-400 max-w-xs truncate">{req.targetGroup || '-'}</td>
                          <td className="px-4 py-2.5 font-mono text-xs text-slate-400">{req.clientIp}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </>
          )}

          {/* ================= CLIENT IPs TAB ================= */}
          {activeTab === 'clients' && !tabLoading && (
            <>
              {clients.length === 0 ? (
                <EmptyState message="No client data available" />
              ) : (
                <div className="overflow-x-auto rounded-lg border border-cluster-border">
                  <table className="w-full text-sm">
                    <thead className="bg-cluster-card">
                      <tr>
                        <th className="text-left px-4 py-2.5 text-slate-400 font-medium">Client IP</th>
                        <th className="text-right px-4 py-2.5 text-slate-400 font-medium">Request Count</th>
                        <th className="text-right px-4 py-2.5 text-slate-400 font-medium">5xx Count</th>
                        <th className="text-left px-4 py-2.5 text-slate-400 font-medium">Last Seen</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-cluster-border">
                      {clients.map((c, i) => (
                        <tr key={i} className="hover:bg-white/5 transition-colors">
                          <td className="px-4 py-2.5 font-mono text-sm text-slate-300">{c.ip}</td>
                          <td className="px-4 py-2.5 text-right text-slate-300">{c.count.toLocaleString()}</td>
                          <td className={`px-4 py-2.5 text-right ${c.count5xx > 0 ? 'text-red-400 font-semibold' : 'text-slate-500'}`}>
                            {c.count5xx.toLocaleString()}
                          </td>
                          <td className="px-4 py-2.5 text-slate-400 text-sm">{timeSince(c.lastSeen)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </>
          )}

          {/* ================= SEARCH TAB ================= */}
          {activeTab === 'search' && !tabLoading && (
            <div>
              {/* Filter bar */}
              <div className="flex flex-wrap items-end gap-3 mb-4 p-4 bg-cluster-card border border-cluster-border rounded-lg">
                <div className="flex flex-col gap-1">
                  <label className="text-xs text-slate-400">Status</label>
                  <select
                    value={searchStatus}
                    onChange={e => setSearchStatus(e.target.value)}
                    className="bg-slate-800 border border-slate-600 rounded px-3 py-1.5 text-sm text-white focus:outline-none focus:ring-1 focus:ring-blue-500"
                  >
                    <option value="all">All</option>
                    <option value="2xx">2xx</option>
                    <option value="4xx">4xx</option>
                    <option value="5xx">5xx</option>
                  </select>
                </div>
                <div className="flex flex-col gap-1 flex-1 min-w-[200px]">
                  <label className="text-xs text-slate-400">URL Pattern</label>
                  <input
                    type="text"
                    value={searchUrl}
                    onChange={e => setSearchUrl(e.target.value)}
                    placeholder="e.g. /api/users"
                    className="bg-slate-800 border border-slate-600 rounded px-3 py-1.5 text-sm text-white placeholder:text-slate-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
                    onKeyDown={e => e.key === 'Enter' && runSearch()}
                  />
                </div>
                <div className="flex flex-col gap-1">
                  <label className="text-xs text-slate-400">Min Latency (ms)</label>
                  <input
                    type="number"
                    value={searchMinLatency}
                    onChange={e => setSearchMinLatency(e.target.value)}
                    placeholder="e.g. 500"
                    className="bg-slate-800 border border-slate-600 rounded px-3 py-1.5 text-sm text-white placeholder:text-slate-500 w-32 focus:outline-none focus:ring-1 focus:ring-blue-500"
                    onKeyDown={e => e.key === 'Enter' && runSearch()}
                  />
                </div>
                <button
                  onClick={runSearch}
                  disabled={searching}
                  className="flex items-center gap-2 px-4 py-1.5 text-sm bg-blue-600 hover:bg-blue-500 disabled:opacity-50 rounded-lg text-white transition-colors"
                >
                  {searching ? <Loader2 className="w-4 h-4 animate-spin" /> : <Search className="w-4 h-4" />}
                  Search
                </button>
              </div>

              {/* Results */}
              {searchResults.length > 0 && (
                <div className="mb-2 text-sm text-slate-400">
                  Showing {searchResults.length} of {searchTotal.toLocaleString()} results
                </div>
              )}
              {searchResults.length === 0 && !searching ? (
                <EmptyState message="Run a search to see results" />
              ) : searching ? (
                <div className="flex justify-center py-12">
                  <Loader2 className="w-5 h-5 animate-spin text-blue-400" />
                </div>
              ) : (
                <div className="overflow-x-auto rounded-lg border border-cluster-border">
                  <table className="w-full text-sm">
                    <thead className="bg-cluster-card">
                      <tr>
                        <th className="text-left px-4 py-2.5 text-slate-400 font-medium">Timestamp</th>
                        <th className="text-left px-4 py-2.5 text-slate-400 font-medium">Method</th>
                        <th className="text-left px-4 py-2.5 text-slate-400 font-medium">URL Pattern</th>
                        <th className="text-right px-4 py-2.5 text-slate-400 font-medium">ELB</th>
                        <th className="text-right px-4 py-2.5 text-slate-400 font-medium">Target</th>
                        <th className="text-right px-4 py-2.5 text-slate-400 font-medium">Latency</th>
                        <th className="text-left px-4 py-2.5 text-slate-400 font-medium">Target Group</th>
                        <th className="text-left px-4 py-2.5 text-slate-400 font-medium">Client IP</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-cluster-border">
                      {searchResults.map((req, i) => (
                        <tr key={i} className="hover:bg-white/5 transition-colors">
                          <td className="px-4 py-2.5 text-slate-400 text-xs whitespace-nowrap">{fmtTimestamp(req.ts)}</td>
                          <td className="px-4 py-2.5">
                            <span className={`px-2 py-0.5 text-xs rounded font-mono ${methodBadge(req.httpMethod)}`}>
                              {req.httpMethod}
                            </span>
                          </td>
                          <td className="px-4 py-2.5 font-mono text-xs text-slate-300 max-w-xs truncate">{req.urlPattern}</td>
                          <td className={`px-4 py-2.5 text-right font-mono ${statusColor(req.elbStatus)}`}>{req.elbStatus}</td>
                          <td className={`px-4 py-2.5 text-right font-mono ${statusColor(req.targetStatus)}`}>{req.targetStatus}</td>
                          <td className={`px-4 py-2.5 text-right font-mono ${latencyColor(req.targetMs)}`}>{fmtMs(req.targetMs)}</td>
                          <td className="px-4 py-2.5 text-xs text-slate-400 max-w-xs truncate">{req.targetGroup || '-'}</td>
                          <td className="px-4 py-2.5 font-mono text-xs text-slate-400">{req.clientIp}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          )}
        </>
      )}

      {/* ================= INGRESS TAB (independent of LB selection) ================= */}
      {activeTab === 'ingress' && !tabLoading && (
        <>
          {ingresses.length === 0 ? (
            <EmptyState message="No ingress resources found" />
          ) : (
            <div className="overflow-x-auto rounded-lg border border-cluster-border">
              <table className="w-full text-sm">
                <thead className="bg-cluster-card">
                  <tr>
                    <th className="text-left px-4 py-2.5 text-slate-400 font-medium w-8"></th>
                    <th className="text-left px-4 py-2.5 text-slate-400 font-medium">Namespace</th>
                    <th className="text-left px-4 py-2.5 text-slate-400 font-medium">Name</th>
                    <th className="text-left px-4 py-2.5 text-slate-400 font-medium">Class</th>
                    <th className="text-left px-4 py-2.5 text-slate-400 font-medium">Hosts</th>
                    <th className="text-center px-4 py-2.5 text-slate-400 font-medium">TLS</th>
                    <th className="text-left px-4 py-2.5 text-slate-400 font-medium">Load Balancer</th>
                    <th className="text-left px-4 py-2.5 text-slate-400 font-medium">Created</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-cluster-border">
                  {ingresses.map((ing, i) => {
                    const rowKey = `${ing.namespace}/${ing.name}`
                    const isExpanded = expandedIngress === rowKey
                    return (
                      <IngressRowGroup
                        key={i}
                        ingress={ing}
                        isExpanded={isExpanded}
                        onToggle={() => setExpandedIngress(isExpanded ? null : rowKey)}
                      />
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}
    </div>
  )
}

/* ------------------------------------------------------------------ */
/*  Sub-components                                                     */
/* ------------------------------------------------------------------ */

function OverviewCard({
  icon, label, value, alert, sub, mono,
}: {
  icon: React.ReactNode; label: string; value: string
  alert?: boolean; sub?: string; mono?: boolean
}) {
  return (
    <div className={`p-4 rounded-lg border ${
      alert ? 'bg-red-900/10 border-red-700/50' : 'bg-cluster-card border-cluster-border'
    }`}>
      <div className="flex items-center gap-1.5 text-xs text-slate-400 mb-1">{icon}{label}</div>
      <div className={`text-lg font-bold text-white truncate ${mono ? 'font-mono text-sm' : ''}`}>
        {value}
      </div>
      {sub && <div className="text-xs text-slate-500 mt-1">{sub}</div>}
    </div>
  )
}

function EmptyState({ message }: { message: string }) {
  return (
    <div className="text-center py-16 text-slate-500">
      <Filter className="w-8 h-8 mx-auto mb-3 text-slate-600" />
      <p>{message}</p>
    </div>
  )
}

function SortableTable<T extends Record<string, any>>({
  data, emptyMessage, columns, sortKey, sortDir, onSort, renderRow,
}: {
  data: T[]
  emptyMessage: string
  columns: { key: string; label: string; align: 'left' | 'right'; sortable?: boolean }[]
  sortKey: string
  sortDir: SortDir
  onSort: (key: any) => void
  renderRow: (row: T, i: number) => React.ReactNode
}) {
  if (data.length === 0) return <EmptyState message={emptyMessage} />

  return (
    <div className="overflow-x-auto rounded-lg border border-cluster-border">
      <table className="w-full text-sm">
        <thead className="bg-cluster-card">
          <tr>
            {columns.map(col => (
              <th
                key={col.key}
                className={`${col.align === 'right' ? 'text-right' : 'text-left'} px-4 py-2.5 text-slate-400 font-medium ${
                  col.sortable ? 'cursor-pointer hover:text-cluster-text select-none' : ''
                }`}
                onClick={() => col.sortable && onSort(col.key)}
              >
                <span className="inline-flex items-center gap-1">
                  {col.label}
                  {col.sortable && sortKey === col.key && (
                    sortDir === 'desc'
                      ? <ChevronDown className="w-3 h-3" />
                      : <ChevronUp className="w-3 h-3" />
                  )}
                  {col.sortable && sortKey !== col.key && (
                    <ArrowUpDown className="w-3 h-3 opacity-30" />
                  )}
                </span>
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-cluster-border">
          {data.map(renderRow)}
        </tbody>
      </table>
    </div>
  )
}

function IngressRowGroup({
  ingress, isExpanded, onToggle,
}: {
  ingress: IngressEntry; isExpanded: boolean; onToggle: () => void
}) {
  return (
    <>
      <tr
        className="hover:bg-white/5 transition-colors cursor-pointer"
        onClick={onToggle}
      >
        <td className="px-4 py-2.5 text-slate-400">
          {isExpanded
            ? <ChevronDown className="w-4 h-4" />
            : <ExternalLink className="w-4 h-4" />
          }
        </td>
        <td className="px-4 py-2.5">
          <span className="px-2 py-0.5 text-xs rounded bg-indigo-900/30 text-indigo-300">
            {ingress.namespace}
          </span>
        </td>
        <td className="px-4 py-2.5 text-white font-medium">{ingress.name}</td>
        <td className="px-4 py-2.5 text-slate-400 text-xs">{ingress.ingressClass || '-'}</td>
        <td className="px-4 py-2.5">
          <div className="flex flex-wrap gap-1">
            {(ingress.hosts || []).map((h, j) => (
              <span key={j} className="px-2 py-0.5 text-xs rounded bg-slate-700 text-slate-300">
                {h}
              </span>
            ))}
          </div>
        </td>
        <td className="px-4 py-2.5 text-center">
          {ingress.tls
            ? <Lock className="w-4 h-4 text-green-400 inline-block" />
            : <Unlock className="w-4 h-4 text-slate-500 inline-block" />
          }
        </td>
        <td className="px-4 py-2.5 font-mono text-xs text-slate-400 max-w-xs truncate">{ingress.loadBalancer || '-'}</td>
        <td className="px-4 py-2.5 text-slate-400 text-xs whitespace-nowrap">{timeSince(ingress.createdAt)}</td>
      </tr>
      {isExpanded && ingress.rules && ingress.rules.length > 0 && (
        <tr>
          <td colSpan={8} className="px-4 py-0">
            <div className="ml-8 my-2 rounded-lg border border-cluster-border overflow-hidden">
              <table className="w-full text-xs">
                <thead className="bg-slate-800/50">
                  <tr>
                    <th className="text-left px-3 py-2 text-slate-400 font-medium">Host</th>
                    <th className="text-left px-3 py-2 text-slate-400 font-medium">Path</th>
                    <th className="text-left px-3 py-2 text-slate-400 font-medium">Path Type</th>
                    <th className="text-left px-3 py-2 text-slate-400 font-medium">Service</th>
                    <th className="text-left px-3 py-2 text-slate-400 font-medium">Port</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-cluster-border">
                  {ingress.rules.map((rule, k) => (
                    <tr key={k} className="hover:bg-white/5">
                      <td className="px-3 py-2 text-slate-300">{rule.host || '*'}</td>
                      <td className="px-3 py-2 font-mono text-slate-300">{rule.path || '/'}</td>
                      <td className="px-3 py-2 text-slate-400">{rule.pathType || '-'}</td>
                      <td className="px-3 py-2 text-blue-300">{rule.serviceName}</td>
                      <td className="px-3 py-2 text-slate-400">{rule.servicePort}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </td>
        </tr>
      )}
    </>
  )
}

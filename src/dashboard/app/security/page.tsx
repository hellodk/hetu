'use client'

import { useEffect, useState, useCallback, useRef } from 'react'
import Link from 'next/link'
import { apiFetch } from '@/lib/api'
import {
  RefreshCw, Loader2, Shield, AlertCircle, AlertTriangle,
  Info, Play, ChevronDown, ChevronRight, CheckCircle, Clock, XCircle
} from 'lucide-react'

interface Finding {
  id: number
  category: string
  severity: string
  title: string
  description: string
  affectedResources: string[]
  cisControl: string
  remediation: string
  detectedAt: string
}

interface SecuritySummary {
  totalFindings: number
  bySeverity: Record<string, number>
  byCategory: Record<string, number>
  byCISControl?: Record<string, number>
}

const PAGE_SIZE = 50

// All 22 CIS Kubernetes Benchmark controls implemented in security.go.
// Ordered to match the backend check order; used for the pass/fail panel.
const CIS_CONTROLS: Array<{
  id: string; title: string; category: string
  sev: 'critical' | 'high' | 'medium' | 'low'
}> = [
  // RBAC — 5.1.x / 5.7.1
  { id: '5.1.1',  title: 'Limit cluster-admin role usage',                      category: 'rbac',         sev: 'high'     },
  { id: '5.1.3',  title: 'Minimize wildcard use in Roles & ClusterRoles',        category: 'rbac',         sev: 'high'     },
  { id: '5.1.5',  title: 'Default ServiceAccounts must not be actively used',    category: 'rbac',         sev: 'medium'   },
  { id: '5.1.6',  title: 'Disable automountServiceAccountToken where unneeded',  category: 'rbac',         sev: 'medium'   },
  { id: '5.7.1',  title: 'Avoid system:masters group for authorization',         category: 'rbac',         sev: 'high'     },
  // Pod Security — 5.2.x / 5.5.1 / 5.7.2
  { id: '5.2.1',  title: 'No privileged containers',                             category: 'pod-security', sev: 'critical' },
  { id: '5.2.2',  title: 'Block allowPrivilegeEscalation',                       category: 'pod-security', sev: 'high'     },
  { id: '5.2.3',  title: 'Containers must not run as root',                      category: 'pod-security', sev: 'high'     },
  { id: '5.2.4',  title: 'Block hostPID and hostIPC sharing',                    category: 'pod-security', sev: 'high'     },
  { id: '5.2.5',  title: 'Block hostNetwork access',                             category: 'pod-security', sev: 'medium'   },
  { id: '5.2.7',  title: 'No HostPath volume mounts',                            category: 'pod-security', sev: 'high'     },
  { id: '5.2.9',  title: 'Drop NET_RAW capability',                              category: 'pod-security', sev: 'medium'   },
  { id: '5.2.11', title: 'Require read-only root filesystem',                    category: 'pod-security', sev: 'medium'   },
  { id: '5.2.12', title: 'Drop all capabilities by default',                     category: 'pod-security', sev: 'medium'   },
  { id: '5.5.1',  title: 'Use specific image tags — not :latest or missing',     category: 'pod-security', sev: 'low'      },
  { id: '5.7.2',  title: 'Set resource limits and security context',             category: 'pod-security', sev: 'medium'   },
  // Network — 5.3.x
  { id: '5.3.1',  title: 'All namespaces must define a NetworkPolicy',           category: 'network',      sev: 'medium'   },
  { id: '5.3.2',  title: 'Default-deny NetworkPolicy in every namespace',        category: 'network',      sev: 'medium'   },
  // Secrets — 5.4.x
  { id: '5.4.1',  title: 'Prefer secrets as files, not env vars',               category: 'secrets',      sev: 'medium'   },
  { id: '5.4.2',  title: 'No hardcoded credentials in environment variables',    category: 'secrets',      sev: 'high'     },
  // General — 5.7.3 / 5.7.4
  { id: '5.7.3',  title: 'Apply ResourceQuota and LimitRange per namespace',     category: 'general',      sev: 'low'      },
  { id: '5.7.4',  title: 'Avoid deploying workloads to the default namespace',   category: 'general',      sev: 'low'      },
]

const CIS_SECTION_ORDER = ['rbac', 'pod-security', 'network', 'secrets', 'general']
const CIS_SECTION_LABEL: Record<string, string> = {
  rbac: 'RBAC', 'pod-security': 'Pod Security',
  network: 'Network', secrets: 'Secrets', general: 'General',
}

// Keys must match the backend category strings from security.go
const CATEGORY_TABS = [
  { key: '',             label: 'All'          },
  { key: 'rbac',        label: 'RBAC'          },
  { key: 'pod-security',label: 'Pod Security'  },
  { key: 'network',     label: 'Network'       },
  { key: 'secrets',     label: 'Secrets'       },
  { key: 'general',     label: 'General'       },
]

const SEV_ORDER: Record<string, number> = { critical: 4, high: 3, medium: 2, low: 1 }

export default function SecurityPage() {
  const [findings, setFindings]           = useState<Finding[]>([])
  const [summary, setSummary]             = useState<SecuritySummary | null>(null)
  const [loading, setLoading]             = useState(true)
  const [error, setError]                 = useState<string | null>(null)
  const [categoryFilter, setCategoryFilter] = useState('')
  const [severityFilter, setSeverityFilter] = useState('')
  const [scanning, setScanning]           = useState(false)
  const [scanMsg, setScanMsg]             = useState<string | null>(null)
  const [expandedIds, setExpandedIds]     = useState<Set<number>>(new Set())
  const [visibleCount, setVisibleCount]   = useState(PAGE_SIZE)
  const [cisFilter, setCisFilter]         = useState('')
  const [cisPanelOpen, setCisPanelOpen]   = useState(false)
  const pollRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const fetchData = useCallback(async (silent = false) => {
    if (!silent) setLoading(true)
    setError(null)
    try {
      const [findingsData, summaryData] = await Promise.all([
        apiFetch<{ totalCount: number; findings: Finding[] }>('/api/v1/security/findings'),
        apiFetch<SecuritySummary>('/api/v1/security/summary'),
      ])
      setFindings(findingsData.findings || [])
      setSummary(summaryData)
    } catch (e: unknown) {
      setError((e as Error).message)
      setFindings([])
      setSummary(null)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchData() }, [fetchData])
  useEffect(() => () => { if (pollRef.current) clearTimeout(pollRef.current) }, [])

  // Run Scan: trigger the async backend scan, then poll until findings change
  const runScan = async () => {
    if (pollRef.current) clearTimeout(pollRef.current)
    setScanning(true)
    setScanMsg('Scan triggered — waiting for results…')
    try {
      await fetch('/api/v1/security/scan', { method: 'POST' })
    } catch (e: unknown) {
      setError((e as Error).message)
      setScanning(false)
      setScanMsg(null)
      return
    }

    const prevTotal = summary?.totalFindings ?? 0
    let attempts = 0
    const poll = async () => {
      attempts++
      try {
        const s = await apiFetch<SecuritySummary>('/api/v1/security/summary')
        if (s.totalFindings !== prevTotal || attempts >= 20) {
          await fetchData(true)
          setScanning(false)
          setScanMsg(null)
        } else {
          setScanMsg(`Scanning… (${attempts * 2}s elapsed)`)
          pollRef.current = setTimeout(poll, 2000)
        }
      } catch {
        setScanning(false)
        setScanMsg(null)
      }
    }
    pollRef.current = setTimeout(poll, 2000)
  }

  const toggleExpanded = (id: number) => {
    setExpandedIds(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const filtered = findings
    .filter(f => {
      if (categoryFilter && f.category !== categoryFilter) return false
      if (severityFilter && f.severity !== severityFilter) return false
      if (cisFilter && f.cisControl !== cisFilter) return false
      return true
    })
    .sort((a, b) => (SEV_ORDER[b.severity] ?? 0) - (SEV_ORDER[a.severity] ?? 0))

  useEffect(() => { setVisibleCount(PAGE_SIZE) }, [categoryFilter, severityFilter, cisFilter])

  const visible = filtered.slice(0, visibleCount)

  const sevIcon = (sev: string) => {
    if (sev === 'critical') return <AlertCircle className="w-4 h-4 text-red-500 flex-shrink-0" />
    if (sev === 'high')     return <AlertTriangle className="w-4 h-4 text-orange-500 flex-shrink-0" />
    if (sev === 'medium')   return <AlertTriangle className="w-4 h-4 text-amber-500 flex-shrink-0" />
    return <Info className="w-4 h-4 text-blue-400 flex-shrink-0" />
  }

  const sevBadge = (sev: string) => {
    const cls: Record<string, string> = {
      critical: 'bg-red-500/15 text-red-600 dark:text-red-400',
      high:     'bg-orange-500/15 text-orange-600 dark:text-orange-400',
      medium:   'bg-amber-500/15 text-amber-600 dark:text-amber-400',
      low:      'bg-blue-500/15 text-blue-600 dark:text-blue-400',
    }
    return (
      <span className={`px-1.5 py-0.5 text-[10px] font-semibold uppercase rounded-full ${cls[sev] || cls.low}`}>
        {sev}
      </span>
    )
  }

  const bySev = summary?.bySeverity ?? {}
  const byCISControl = summary?.byCISControl ?? {}

  // CIS score: 22 unique controls are implemented in security.go
  const CIS_TOTAL = 22
  const cisFailingControls = Object.keys(byCISControl).length
  const cisPassed = Math.max(0, CIS_TOTAL - cisFailingControls)
  const cisScore = summary ? Math.round(cisPassed / CIS_TOTAL * 100) : null
  const cisColor = cisScore === null ? 'text-cluster-muted'
    : cisScore >= 90 ? 'text-green-500 dark:text-green-400'
    : cisScore >= 70 ? 'text-amber-500 dark:text-amber-400'
    : 'text-red-500 dark:text-red-400'
  const cisRing = cisScore === null ? 'border-cluster-border'
    : cisScore >= 90 ? 'border-green-500/30'
    : cisScore >= 70 ? 'border-amber-500/30'
    : 'border-red-500/30'

  return (
    <div className="min-h-screen bg-cluster-bg">
      <div className="max-w-[1600px] mx-auto px-4 sm:px-6 py-6 space-y-5">

        {/* ── Header ── */}
        <div className="flex items-center justify-between gap-4">
          <div className="flex items-center gap-3">
            <Shield className="w-6 h-6 text-orange-500" />
            <div>
              <h1 className="text-2xl font-bold text-cluster-text">Security Findings</h1>
              <p className="text-sm text-cluster-muted mt-0.5">CIS Kubernetes Benchmark · RBAC · Pod Security · Network Policies</p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={runScan}
              disabled={scanning}
              className="flex items-center gap-1.5 px-4 py-2 text-sm bg-orange-500 hover:bg-orange-600 disabled:opacity-60 text-white rounded-lg font-medium transition-colors"
            >
              {scanning
                ? <Loader2 className="w-3.5 h-3.5 animate-spin" />
                : <Play className="w-3.5 h-3.5" />}
              {scanning ? 'Scanning…' : 'Run Scan'}
            </button>
            <button
              onClick={() => fetchData()}
              disabled={loading}
              className="p-2 text-cluster-muted hover:text-cluster-text border border-cluster-border rounded-lg hover:bg-cluster-border/30 transition-colors"
            >
              <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
            </button>
          </div>
        </div>

        {/* Scan progress message */}
        {scanMsg && (
          <div className="flex items-center gap-2 px-4 py-2.5 bg-orange-500/10 border border-orange-500/20 rounded-lg text-sm text-orange-700 dark:text-orange-300">
            <Clock className="w-4 h-4 animate-pulse flex-shrink-0" />
            {scanMsg}
          </div>
        )}

        {/* ── Summary stats ── */}
        {summary && (
          <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-7 gap-3">
            {/* CIS Benchmark score */}
            <div className={`md:col-span-2 p-4 rounded-xl border ${cisRing} bg-cluster-card flex flex-col justify-between`}>
              <div className="text-xs font-semibold uppercase tracking-wide text-cluster-muted mb-2">CIS Benchmark Score</div>
              <div className="flex items-end gap-2">
                <div className={`text-4xl font-mono font-bold ${cisColor}`}>
                  {cisScore !== null ? `${cisScore}%` : '—'}
                </div>
                <div className="text-xs text-cluster-muted pb-1">
                  {cisPassed}/{CIS_TOTAL} controls passing
                </div>
              </div>
              {cisScore !== null && (
                <div className="mt-3 h-1.5 w-full bg-cluster-border rounded-full overflow-hidden">
                  <div
                    className={`h-full rounded-full transition-all ${
                      cisScore >= 90 ? 'bg-green-500' : cisScore >= 70 ? 'bg-amber-500' : 'bg-red-500'
                    }`}
                    style={{ width: `${cisScore}%` }}
                  />
                </div>
              )}
            </div>

            {/* Total findings */}
            <div className="p-4 rounded-xl border border-cluster-border bg-cluster-card">
              <div className="text-xs text-cluster-muted mb-1">Total Findings</div>
              <div className="text-3xl font-mono font-bold text-cluster-text">{summary.totalFindings}</div>
              <div className="text-xs text-cluster-muted mt-1">
                {Object.keys(summary.byCategory || {}).length} categories
              </div>
            </div>

            {[
              { label: 'Critical', key: 'critical', cls: 'text-red-500 dark:text-red-400',    border: 'border-red-500/20',    bg: 'bg-red-500/5' },
              { label: 'High',     key: 'high',     cls: 'text-orange-500 dark:text-orange-400', border: 'border-orange-500/20', bg: 'bg-orange-500/5' },
              { label: 'Medium',   key: 'medium',   cls: 'text-amber-500 dark:text-amber-400',   border: 'border-amber-500/20',  bg: 'bg-amber-500/5' },
              { label: 'Low',      key: 'low',      cls: 'text-blue-500 dark:text-blue-400',     border: 'border-blue-500/20',   bg: 'bg-blue-500/5' },
            ].map(({ label, key, cls, border, bg }) => (
              <div key={key} className={`p-4 rounded-xl border ${border} ${bg}`}>
                <div className="text-xs text-cluster-muted mb-1">{label}</div>
                <div className={`text-2xl font-mono font-bold ${cls}`}>{bySev[key] ?? 0}</div>
              </div>
            ))}
          </div>
        )}

        {/* ── CIS Controls pass/fail panel ── */}
        {summary && (
          <div className="rounded-xl border border-cluster-border bg-cluster-card overflow-hidden">
            <button
              onClick={() => setCisPanelOpen(o => !o)}
              className="w-full flex items-center justify-between px-5 py-3.5 hover:bg-cluster-border/20 transition-colors"
            >
              <div className="flex items-center gap-2.5">
                <Shield className="w-4 h-4 text-orange-500" />
                <span className="text-sm font-semibold text-cluster-text">CIS Controls Breakdown</span>
                <span className={`text-xs px-2 py-0.5 rounded-full font-medium ${
                  cisFailingControls === 0
                    ? 'bg-green-500/10 text-green-600 dark:text-green-400'
                    : 'bg-red-500/10 text-red-600 dark:text-red-400'
                }`}>
                  {cisPassed}/{CIS_TOTAL} passing
                </span>
                {cisFilter && (
                  <span className="text-xs px-2 py-0.5 rounded-full bg-blue-500/10 text-blue-600 dark:text-blue-400 border border-blue-500/20">
                    filtered: CIS {cisFilter}
                  </span>
                )}
              </div>
              {cisPanelOpen
                ? <ChevronDown className="w-4 h-4 text-cluster-muted" />
                : <ChevronRight className="w-4 h-4 text-cluster-muted" />}
            </button>

            {cisPanelOpen && (
              <div className="border-t border-cluster-border/60 divide-y divide-cluster-border/40">
                {CIS_SECTION_ORDER.map(section => {
                  const controls = CIS_CONTROLS.filter(c => c.category === section)
                  const sectionPassed = controls.filter(c => !byCISControl[c.id]).length
                  return (
                    <div key={section}>
                      <div className="px-5 py-2 bg-cluster-bg/40 flex items-center gap-2">
                        <span className="text-xs font-bold uppercase tracking-widest text-cluster-muted">
                          {CIS_SECTION_LABEL[section]}
                        </span>
                        <span className="text-xs text-cluster-muted">
                          ({sectionPassed}/{controls.length} passing)
                        </span>
                      </div>
                      {controls.map(c => {
                        const failCount = byCISControl[c.id] ?? 0
                        const passing = failCount === 0
                        const active = cisFilter === c.id
                        return (
                          <div
                            key={c.id}
                            onClick={!passing ? () => setCisFilter(prev => prev === c.id ? '' : c.id) : undefined}
                            className={`flex items-center gap-3 px-5 py-2.5 text-sm transition-colors ${
                              !passing ? 'cursor-pointer' : ''
                            } ${
                              active
                                ? 'bg-blue-500/10'
                                : !passing
                                  ? 'hover:bg-red-500/5'
                                  : ''
                            }`}
                          >
                            {passing
                              ? <CheckCircle className="w-3.5 h-3.5 text-green-500 flex-shrink-0" />
                              : <XCircle className="w-3.5 h-3.5 text-red-500 flex-shrink-0" />}
                            <span className="font-mono text-[11px] text-cluster-muted w-10 flex-shrink-0">{c.id}</span>
                            <span className={`flex-1 text-xs ${passing ? 'text-cluster-muted' : 'text-cluster-text'}`}>
                              {c.title}
                            </span>
                            {!passing && (
                              <span className={`text-[10px] font-medium px-1.5 py-0.5 rounded-full ${
                                active
                                  ? 'bg-blue-500/20 text-blue-600 dark:text-blue-400'
                                  : 'bg-red-500/10 text-red-500 dark:text-red-400'
                              }`}>
                                {failCount} {failCount === 1 ? 'finding' : 'findings'}
                              </span>
                            )}
                          </div>
                        )
                      })}
                    </div>
                  )
                })}
              </div>
            )}
          </div>
        )}

        {/* ── Trivy vulnerability report ── */}
        <div className="p-4 rounded-xl border border-cluster-border bg-cluster-card">
          <div className="flex items-center gap-2 mb-2">
            <Shield className="w-4 h-4 text-cluster-muted" />
            <span className="text-sm font-semibold text-cluster-text">Trivy Vulnerability Analysis</span>
            <span className="ml-auto text-[10px] px-2 py-0.5 rounded-full bg-cluster-border/60 text-cluster-muted font-medium uppercase tracking-wide">Not Connected</span>
          </div>
          <p className="text-xs text-cluster-muted leading-relaxed">
            Trivy Operator is not installed in this cluster. Install it to get automated CVE scanning for container images and workload manifests.
          </p>
          <div className="mt-3 flex gap-2 flex-wrap">
            <code className="text-[11px] font-mono px-2 py-1 bg-cluster-border/40 text-cluster-text rounded">helm install trivy-operator aqua/trivy-operator -n trivy-system --create-namespace</code>
          </div>
        </div>

        {/* ── By category breakdown ── */}
        {summary && Object.keys(summary.byCategory || {}).length > 0 && (
          <div className="flex flex-wrap gap-2">
            {Object.entries(summary.byCategory).map(([cat, count]) => (
              <button
                key={cat}
                onClick={() => setCategoryFilter(prev => prev === cat ? '' : cat)}
                className={`flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm border transition-colors ${
                  categoryFilter === cat
                    ? 'bg-orange-500/15 border-orange-500/30 text-orange-700 dark:text-orange-300'
                    : 'bg-cluster-card border-cluster-border text-cluster-muted hover:text-cluster-text hover:bg-cluster-border/30'
                }`}
              >
                <span className="font-medium">{cat}</span>
                <span className="text-xs opacity-70">({count})</span>
              </button>
            ))}
          </div>
        )}

        {/* ── Filters row ── */}
        <div className="flex items-center gap-3 flex-wrap">
          <div className="flex rounded-lg overflow-hidden border border-cluster-border">
            {CATEGORY_TABS.map(tab => (
              <button
                key={tab.key}
                onClick={() => setCategoryFilter(tab.key)}
                className={`px-3 py-1.5 text-sm transition-colors ${
                  categoryFilter === tab.key
                    ? 'bg-orange-500 text-white'
                    : 'bg-cluster-card text-cluster-muted hover:text-cluster-text hover:bg-cluster-border/40'
                }`}
              >
                {tab.label}
                {tab.key && summary?.byCategory?.[tab.key] !== undefined && (
                  <span className="ml-1 opacity-70">({summary.byCategory[tab.key]})</span>
                )}
              </button>
            ))}
          </div>
          <select
            value={severityFilter}
            onChange={e => setSeverityFilter(e.target.value)}
            className="bg-cluster-card border border-cluster-border rounded-lg px-3 py-1.5 text-sm text-cluster-text"
          >
            <option value="">All severities</option>
            <option value="critical">Critical</option>
            <option value="high">High</option>
            <option value="medium">Medium</option>
            <option value="low">Low</option>
          </select>
          {cisFilter && (
            <button
              onClick={() => setCisFilter('')}
              className="flex items-center gap-1 px-2.5 py-1.5 text-xs bg-blue-500/10 border border-blue-500/20 text-blue-600 dark:text-blue-400 rounded-lg hover:bg-blue-500/20 transition-colors"
            >
              CIS {cisFilter} <span className="ml-0.5 opacity-70">×</span>
            </button>
          )}
          <span className="text-sm text-cluster-muted">{filtered.length} findings</span>
        </div>

        {error && (
          <div className="p-3 bg-red-500/10 border border-red-500/20 rounded-lg text-sm text-red-600 dark:text-red-400">
            {error}
          </div>
        )}

        {loading && (
          <div className="flex justify-center py-16">
            <Loader2 className="w-6 h-6 animate-spin text-orange-500" />
          </div>
        )}

        {!loading && filtered.length === 0 && !error && (
          <div className="text-center py-16 rounded-xl border border-dashed border-cluster-border">
            <Shield className="w-10 h-10 text-cluster-muted/40 mx-auto mb-3" />
            <p className="text-sm font-medium text-cluster-text mb-1">No findings found</p>
            <p className="text-xs text-cluster-muted mb-4">
              {categoryFilter
                ? `No ${categoryFilter} findings match the current filter.`
                : 'Click "Run Scan" to perform a CIS Kubernetes Benchmark audit.'}
            </p>
            {!categoryFilter && (
              <button
                onClick={runScan}
                disabled={scanning}
                className="inline-flex items-center gap-2 px-4 py-2 bg-orange-500 hover:bg-orange-600 text-white rounded-lg text-sm font-medium transition-colors disabled:opacity-60"
              >
                <Play className="w-3.5 h-3.5" />
                Run Scan Now
              </button>
            )}
          </div>
        )}

        {/* ── Findings list ── */}
        {!loading && filtered.length > 0 && (
          <div className="rounded-xl border border-cluster-border bg-cluster-card overflow-hidden">
            <div className="divide-y divide-cluster-border/60">
              {visible.map(f => (
                <div key={f.id}>
                  <div
                    className="flex items-start gap-3 px-5 py-4 cursor-pointer hover:bg-cluster-border/20 transition-colors"
                    onClick={() => toggleExpanded(f.id)}
                  >
                    <div className="mt-0.5">{sevIcon(f.severity)}</div>
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 flex-wrap mb-0.5">
                        <span className="text-sm font-medium text-cluster-text">{f.title}</span>
                        {sevBadge(f.severity)}
                        {f.cisControl && (
                          <button
                            onClick={e => { e.stopPropagation(); setCisFilter(prev => prev === f.cisControl ? '' : f.cisControl) }}
                            className={`px-1.5 py-0.5 text-[10px] font-mono rounded border transition-colors ${
                              cisFilter === f.cisControl
                                ? 'bg-blue-500/25 text-blue-600 dark:text-blue-400 border-blue-500/40'
                                : 'bg-blue-500/10 text-blue-600 dark:text-blue-400 border-blue-500/20 hover:bg-blue-500/20'
                            }`}
                            title={`Filter by CIS ${f.cisControl}`}
                          >
                            CIS {f.cisControl}
                          </button>
                        )}
                        <span className="px-1.5 py-0.5 text-[10px] bg-cluster-border/60 text-cluster-muted rounded">
                          {f.category}
                        </span>
                      </div>
                      <p className="text-sm text-cluster-muted truncate">{f.description}</p>
                    </div>
                    <div className="flex-shrink-0 ml-2">
                      {expandedIds.has(f.id)
                        ? <ChevronDown className="w-4 h-4 text-cluster-muted" />
                        : <ChevronRight className="w-4 h-4 text-cluster-muted" />}
                    </div>
                  </div>

                  {expandedIds.has(f.id) && (
                    <div className="px-5 pb-5 ml-7 space-y-4 border-t border-cluster-border/50 pt-4 bg-cluster-bg/30">
                      <p className="text-sm text-cluster-text leading-relaxed">{f.description}</p>

                      {f.affectedResources?.length > 0 && (
                        <div>
                          <p className="text-xs font-semibold text-cluster-muted uppercase tracking-wide mb-2">Affected Resources</p>
                          <div className="flex flex-wrap gap-1.5">
                            {f.affectedResources.map((r, i) => {
                              const parts = r.split('/')
                              const ns = parts.length >= 2 ? parts[0] : null
                              let href: string | null = null
                              if (ns) {
                                if (f.category === 'pod-security' || f.category === 'secrets' || f.category === 'general') {
                                  href = `/workloads/pods?group=core&version=v1&ns=${ns}`
                                }
                              }
                              const cls = 'px-2 py-0.5 text-xs font-mono rounded transition-colors'
                              return href ? (
                                <Link
                                  key={i}
                                  href={href}
                                  onClick={e => e.stopPropagation()}
                                  className={`${cls} bg-orange-500/10 text-orange-700 dark:text-orange-300 border border-orange-500/20 hover:bg-orange-500/20`}
                                  title={`View pods in namespace ${ns}`}
                                >
                                  {r} →
                                </Link>
                              ) : (
                                <span key={i} className={`${cls} bg-cluster-border/60 text-cluster-text`}>
                                  {r}
                                </span>
                              )
                            })}
                          </div>
                        </div>
                      )}

                      {f.remediation && (
                        <div>
                          <p className="text-xs font-semibold text-cluster-muted uppercase tracking-wide mb-2">Remediation</p>
                          <div className="flex items-start gap-2 p-3 bg-green-500/5 border border-green-500/20 rounded-lg">
                            <CheckCircle className="w-4 h-4 text-green-500 flex-shrink-0 mt-0.5" />
                            <p className="text-sm text-cluster-text">{f.remediation}</p>
                          </div>
                        </div>
                      )}

                      <p className="text-xs text-cluster-muted">
                        Detected: {new Date(f.detectedAt).toLocaleString()}
                      </p>
                    </div>
                  )}
                </div>
              ))}
            </div>

            {visibleCount < filtered.length && (
              <div className="flex items-center justify-between px-5 py-3 border-t border-cluster-border bg-cluster-bg/30">
                <span className="text-xs text-cluster-muted">
                  Showing {visibleCount} of {filtered.length} findings
                </span>
                <button
                  onClick={() => setVisibleCount(v => v + PAGE_SIZE)}
                  className="px-4 py-1.5 text-sm bg-cluster-card border border-cluster-border hover:bg-cluster-border/40 text-cluster-text rounded-lg transition-colors"
                >
                  Load more
                </button>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}

'use client'

import { useEffect, useState, useCallback } from 'react'
import { apiFetch, getApiUrl } from '@/lib/api'
import {
  RefreshCw, Loader2, Shield, AlertCircle, AlertTriangle,
  Info, Play, ChevronDown, ChevronRight
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
  bySeverity: { critical: number; high: number; medium: number; low: number }
  byCategory: { cis: number; rbac: number; 'pod-security': number }
}

const CATEGORY_TABS = [
  { key: '', label: 'All' },
  { key: 'cis', label: 'CIS' },
  { key: 'rbac', label: 'RBAC' },
  { key: 'pod-security', label: 'Pod Security' },
]

export default function SecurityPage() {
  const [findings, setFindings] = useState<Finding[]>([])
  const [summary, setSummary] = useState<SecuritySummary | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [categoryFilter, setCategoryFilter] = useState('')
  const [severityFilter, setSeverityFilter] = useState('')
  const [scanning, setScanning] = useState(false)
  const [expandedIds, setExpandedIds] = useState<Set<number>>(new Set())

  const fetchData = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const [findingsData, summaryData] = await Promise.all([
        apiFetch<{ totalCount: number; findings: Finding[] }>('/api/v1/security/findings'),
        apiFetch<SecuritySummary>('/api/v1/security/summary'),
      ])
      setFindings(findingsData.findings || [])
      setSummary(summaryData)
    } catch (e: any) {
      setError(e.message)
      setFindings([])
      setSummary(null)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchData() }, [fetchData])

  const runScan = async () => {
    setScanning(true)
    try {
      await fetch(`${getApiUrl()}/api/v1/security/scan`, { method: 'POST' })
      await fetchData()
    } catch (e: any) {
      setError(e.message)
    } finally {
      setScanning(false)
    }
  }

  const toggleExpanded = (id: number) => {
    setExpandedIds(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const filtered = findings.filter(f => {
    if (categoryFilter && f.category !== categoryFilter) return false
    if (severityFilter && f.severity !== severityFilter) return false
    return true
  })

  const severityIcon = (severity: string) => {
    if (severity === 'critical') return <AlertCircle className="w-4 h-4 text-red-500" />
    if (severity === 'high') return <AlertTriangle className="w-4 h-4 text-orange-400" />
    if (severity === 'medium') return <AlertTriangle className="w-4 h-4 text-yellow-400" />
    return <Info className="w-4 h-4 text-blue-400" />
  }

  const severityBadge = (severity: string) => {
    const styles: Record<string, string> = {
      critical: 'bg-red-900/30 text-red-300',
      high: 'bg-orange-900/30 text-orange-300',
      medium: 'bg-yellow-900/30 text-yellow-300',
      low: 'bg-gray-700 text-gray-400',
    }
    return (
      <span className={`px-1.5 py-0.5 text-xs rounded ${styles[severity] || styles.low}`}>
        {severity}
      </span>
    )
  }

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <div className="flex items-center gap-2">
          <Shield className="w-6 h-6 text-orange-400" />
          <h1 className="text-2xl font-bold text-white">Security Findings</h1>
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={runScan}
            disabled={scanning}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-orange-600 hover:bg-orange-500 disabled:opacity-50 text-white rounded"
          >
            {scanning ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Play className="w-3.5 h-3.5" />}
            Run Scan
          </button>
          <button
            onClick={fetchData}
            className="flex items-center gap-2 px-3 py-1.5 text-sm bg-white/10 hover:bg-white/20 rounded-md text-white"
          >
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </div>

      {/* Summary stats */}
      {summary && (
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
          <div className="p-4 bg-gray-800/50 border border-gray-700/50 rounded-lg">
            <div className="text-xs text-gray-400 mb-1">Total Findings</div>
            <div className="text-2xl font-bold text-white">{summary.totalFindings}</div>
          </div>
          <div className="p-4 bg-red-900/10 border border-red-700/30 rounded-lg">
            <div className="text-xs text-gray-400 mb-1">Critical</div>
            <div className="text-2xl font-bold text-red-400">{summary.bySeverity.critical}</div>
          </div>
          <div className="p-4 bg-orange-900/10 border border-orange-700/30 rounded-lg">
            <div className="text-xs text-gray-400 mb-1">High</div>
            <div className="text-2xl font-bold text-orange-400">{summary.bySeverity.high}</div>
          </div>
          <div className="p-4 bg-gray-800/50 border border-gray-700/50 rounded-lg">
            <div className="text-xs text-gray-400 mb-1">By Category</div>
            <div className="flex gap-3 text-sm">
              {Object.entries(summary.byCategory || {}).map(([k, v]) => (
                <span key={k} className="text-gray-300">
                  {k}: <span className="font-medium text-white">{v}</span>
                </span>
              ))}
            </div>
          </div>
        </div>
      )}

      {/* Filters */}
      <div className="flex items-center gap-3 mb-4">
        <div className="flex rounded-md overflow-hidden border border-gray-700">
          {CATEGORY_TABS.map(tab => (
            <button
              key={tab.key}
              onClick={() => setCategoryFilter(tab.key)}
              className={`px-3 py-1.5 text-sm transition-colors ${
                categoryFilter === tab.key
                  ? 'bg-orange-600 text-white'
                  : 'bg-gray-800 text-gray-400 hover:text-white hover:bg-gray-700'
              }`}
            >
              {tab.label}
            </button>
          ))}
        </div>
        <select
          value={severityFilter}
          onChange={e => setSeverityFilter(e.target.value)}
          className="bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-sm text-white"
        >
          <option value="">All severities</option>
          <option value="critical">Critical</option>
          <option value="high">High</option>
          <option value="medium">Medium</option>
          <option value="low">Low</option>
        </select>
        <span className="text-sm text-gray-400">{filtered.length} findings</span>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-900/30 border border-red-700 rounded text-sm text-red-300">
          {error}
        </div>
      )}

      {loading && (
        <div className="flex justify-center py-12">
          <Loader2 className="w-6 h-6 animate-spin text-orange-400" />
        </div>
      )}

      {!loading && filtered.length === 0 && !error && (
        <div className="text-center py-12 text-gray-500">
          No security findings found. Click &quot;Run Scan&quot; to initiate a security audit.
        </div>
      )}

      {!loading && filtered.length > 0 && (
        <div className="space-y-2">
          {filtered.map(f => (
            <div
              key={f.id}
              className="p-4 bg-gray-800/50 border border-gray-700/50 rounded-lg"
            >
              <div
                className="flex items-start gap-3 cursor-pointer"
                onClick={() => toggleExpanded(f.id)}
              >
                <div className="mt-0.5">{severityIcon(f.severity)}</div>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 mb-1">
                    <span className="text-sm font-medium text-white">{f.title}</span>
                    {severityBadge(f.severity)}
                    {f.cisControl && (
                      <span className="px-1.5 py-0.5 text-xs bg-blue-900/30 text-blue-300 rounded font-mono">
                        {f.cisControl}
                      </span>
                    )}
                  </div>
                  <p className="text-sm text-gray-400 truncate">{f.description}</p>
                </div>
                <div className="shrink-0 flex items-center gap-2">
                  <span className="px-1.5 py-0.5 text-xs bg-gray-700 text-gray-400 rounded">
                    {f.category}
                  </span>
                  {expandedIds.has(f.id) ? (
                    <ChevronDown className="w-4 h-4 text-gray-500" />
                  ) : (
                    <ChevronRight className="w-4 h-4 text-gray-500" />
                  )}
                </div>
              </div>

              {expandedIds.has(f.id) && (
                <div className="mt-3 ml-7 space-y-3">
                  <p className="text-sm text-gray-300">{f.description}</p>

                  {f.affectedResources && f.affectedResources.length > 0 && (
                    <div>
                      <div className="text-xs text-gray-500 mb-1">Affected Resources</div>
                      <div className="flex flex-wrap gap-1">
                        {f.affectedResources.map((r, i) => (
                          <span key={i} className="px-1.5 py-0.5 text-xs bg-gray-700 text-gray-300 rounded font-mono">
                            {r}
                          </span>
                        ))}
                      </div>
                    </div>
                  )}

                  {f.remediation && (
                    <div>
                      <div className="text-xs text-gray-500 mb-1">Remediation</div>
                      <p className="text-sm text-gray-300 bg-gray-900/50 rounded p-2">{f.remediation}</p>
                    </div>
                  )}

                  <div className="text-xs text-gray-500">
                    Detected: {new Date(f.detectedAt).toLocaleString()}
                  </div>
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

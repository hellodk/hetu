'use client'

import { useEffect, useState, useCallback } from 'react'
import { apiFetch, getApiUrl } from '@/lib/api'
import {
  RefreshCw, Loader2, DollarSign, TrendingDown, Cpu,
  Gauge, Copy, Check, CheckCircle, XCircle, Play
} from 'lucide-react'

interface Recommendation {
  id: number
  type: string
  severity: string
  confidence: number
  target: { kind: string; namespace: string; name: string; container?: string }
  currentState: Record<string, any>
  suggestedState: Record<string, any>
  rationale: string
  estimatedSavingsMonthly: number
  status: string
  yamlPatch: string
  createdAt: string
}

interface Summary {
  totalRecommendations: number
  openRecommendations: number
  totalSavingsMonthly: number
  byType: Record<string, number>
  availableOptimizers: string[]
}

const TYPE_LABELS: Record<string, { label: string; icon: React.ReactNode }> = {
  rightsizing: { label: 'Right-Sizing', icon: <Cpu className="w-4 h-4" /> },
  hpa: { label: 'HPA Tuning', icon: <Gauge className="w-4 h-4" /> },
}

export default function OptimizationPage() {
  const [recs, setRecs] = useState<Recommendation[]>([])
  const [summary, setSummary] = useState<Summary | null>(null)
  const [loading, setLoading] = useState(true)
  const [typeFilter, setTypeFilter] = useState('')
  const [copiedId, setCopiedId] = useState<number | null>(null)

  const fetchData = useCallback(async () => {
    setLoading(true)
    try {
      const params = typeFilter ? `?type=${typeFilter}&status=open` : '?status=open'
      const [r, s] = await Promise.all([
        apiFetch<{ recommendations: Recommendation[] }>(`/api/v1/recommendations${params}`),
        apiFetch<Summary>('/api/v1/recommendations/summary'),
      ])
      setRecs(r.recommendations || [])
      setSummary(s)
    } catch { }
    finally { setLoading(false) }
  }, [typeFilter])

  useEffect(() => { fetchData() }, [fetchData])

  const runOptimizer = async (type?: string) => {
    const params = type ? `?type=${type}` : ''
    await fetch(`${getApiUrl()}/api/v1/recommendations/run${params}`, { method: 'POST' })
    fetchData()
  }

  const updateStatus = async (id: number, status: string) => {
    await fetch(`${getApiUrl()}/api/v1/recommendations/${id}/status`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ status }),
    })
    setRecs(prev => prev.filter(r => r.id !== id))
  }

  const copyYaml = (id: number, yaml: string) => {
    navigator.clipboard.writeText(yaml)
    setCopiedId(id)
    setTimeout(() => setCopiedId(null), 2000)
  }

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold text-white">Optimization</h1>
        <div className="flex items-center gap-3">
          <select value={typeFilter} onChange={e => setTypeFilter(e.target.value)}
            className="bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-sm text-white">
            <option value="">All types</option>
            {Object.entries(TYPE_LABELS).map(([k, v]) => <option key={k} value={k}>{v.label}</option>)}
          </select>
          <button onClick={() => runOptimizer(typeFilter || undefined)}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-blue-600 hover:bg-blue-500 text-white rounded">
            <Play className="w-3.5 h-3.5" /> Run Analysis
          </button>
          <button onClick={fetchData}
            className="flex items-center gap-2 px-3 py-1.5 text-sm bg-cluster-border/40 hover:bg-cluster-border/60 rounded text-cluster-text">
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </div>

      {/* Summary cards */}
      {summary && (
        <div className="grid grid-cols-3 gap-4 mb-6">
          <div className="p-4 bg-gray-800/50 border border-gray-700/50 rounded-lg">
            <div className="flex items-center gap-1.5 text-xs text-gray-400 mb-1">
              <TrendingDown className="w-4 h-4 text-green-400" /> Open Recommendations
            </div>
            <div className="text-2xl font-bold text-white">{summary.openRecommendations}</div>
          </div>
          <div className="p-4 bg-green-900/10 border border-green-700/30 rounded-lg">
            <div className="flex items-center gap-1.5 text-xs text-gray-400 mb-1">
              <DollarSign className="w-4 h-4 text-green-400" /> Potential Savings
            </div>
            <div className="text-2xl font-bold text-green-400">
              ${summary.totalSavingsMonthly.toFixed(0)}/mo
            </div>
          </div>
          <div className="p-4 bg-gray-800/50 border border-gray-700/50 rounded-lg">
            <div className="flex items-center gap-1.5 text-xs text-gray-400 mb-1">
              By Type
            </div>
            <div className="flex gap-3 text-sm">
              {Object.entries(summary.byType || {}).map(([k, v]) => (
                <span key={k} className="text-gray-300">{TYPE_LABELS[k]?.label || k}: <span className="font-medium text-white">{v}</span></span>
              ))}
            </div>
          </div>
        </div>
      )}

      {loading && <div className="flex justify-center py-12"><Loader2 className="w-6 h-6 animate-spin text-blue-400" /></div>}

      {!loading && recs.length === 0 && (
        <div className="text-center py-12 text-gray-500">
          No open recommendations. Click "Run Analysis" to scan for optimization opportunities.
        </div>
      )}

      {!loading && recs.length > 0 && (
        <div className="space-y-3">
          {recs.map(rec => (
            <div key={rec.id} className="p-4 bg-gray-800/50 border border-gray-700/50 rounded-lg">
              <div className="flex items-start justify-between mb-3">
                <div>
                  <div className="flex items-center gap-2 mb-1">
                    {TYPE_LABELS[rec.type]?.icon}
                    <span className="text-sm font-medium text-white">{TYPE_LABELS[rec.type]?.label || rec.type}</span>
                    <span className={`px-1.5 py-0.5 text-xs rounded ${
                      rec.severity === 'high' ? 'bg-orange-900/30 text-orange-300' :
                      rec.severity === 'medium' ? 'bg-yellow-900/30 text-yellow-300' :
                      'bg-gray-700 text-gray-400'
                    }`}>{rec.severity}</span>
                    {Number.isFinite(rec.confidence) && (
                      <span className="text-xs text-gray-500">conf: {(rec.confidence * 100).toFixed(0)}%</span>
                    )}
                  </div>
                  <div className="text-xs text-gray-400">
                    {rec.target.namespace}/{rec.target.name}
                    {rec.target.container && ` (${rec.target.container})`}
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  {rec.estimatedSavingsMonthly > 0 && (
                    <span className="text-sm font-medium text-green-400">
                      -${rec.estimatedSavingsMonthly.toFixed(0)}/mo
                    </span>
                  )}
                  <button onClick={() => updateStatus(rec.id, 'accepted')}
                    className="p-1.5 text-green-400 hover:bg-green-900/30 rounded" aria-label="Accept">
                    <CheckCircle className="w-4 h-4" />
                  </button>
                  <button onClick={() => updateStatus(rec.id, 'dismissed')}
                    className="p-1.5 text-gray-400 hover:bg-gray-700 rounded" aria-label="Dismiss">
                    <XCircle className="w-4 h-4" />
                  </button>
                </div>
              </div>

              <p className="text-sm text-gray-300 mb-3">{rec.rationale}</p>

              {/* Current vs Suggested */}
              <div className="grid grid-cols-2 gap-3 mb-3">
                <div className="p-2 bg-gray-900/50 rounded">
                  <div className="text-xs text-gray-500 mb-1">Current</div>
                  {Object.entries(rec.currentState || {}).map(([k, v]) => (
                    <div key={k} className="text-xs"><span className="text-gray-500">{k}: </span><span className="text-gray-300">{String(v)}</span></div>
                  ))}
                </div>
                <div className="p-2 bg-green-900/10 border border-green-700/20 rounded">
                  <div className="text-xs text-green-400 mb-1">Suggested</div>
                  {Object.entries(rec.suggestedState || {}).map(([k, v]) => (
                    <div key={k} className="text-xs"><span className="text-gray-500">{k}: </span><span className="text-green-300 font-medium">{String(v)}</span></div>
                  ))}
                </div>
              </div>

              {/* YAML patch */}
              {rec.yamlPatch && (
                <div className="relative">
                  <button onClick={() => copyYaml(rec.id, rec.yamlPatch)}
                    className="absolute top-2 right-2 flex items-center gap-1 px-2 py-0.5 text-xs bg-gray-700 hover:bg-gray-600 rounded text-gray-300 z-10">
                    {copiedId === rec.id ? <Check className="w-3 h-3 text-green-400" /> : <Copy className="w-3 h-3" />}
                    {copiedId === rec.id ? 'Copied' : 'Copy'}
                  </button>
                  <pre className="text-xs font-mono text-gray-300 bg-gray-900 rounded p-3 overflow-auto max-h-32">
                    {rec.yamlPatch}
                  </pre>
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

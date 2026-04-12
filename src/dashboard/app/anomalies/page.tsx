'use client'

import { useEffect, useState, useCallback } from 'react'
import { apiFetch } from '@/lib/api'
import { RefreshCw, Loader2, Activity, AlertCircle, AlertTriangle } from 'lucide-react'

interface Anomaly {
  id: number
  service: string
  namespace: string
  metric: string
  score: number
  expected: number
  observed: number
  severity: string
  detectedAt: string
  status: string
}

export default function AnomaliesPage() {
  const [anomalies, setAnomalies] = useState<Anomaly[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchData = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await apiFetch<{ totalCount: number; anomalies: Anomaly[] }>(
        '/api/v1/anomalies'
      )
      const sorted = (data.anomalies || []).sort(
        (a, b) => Math.abs(b.score) - Math.abs(a.score)
      )
      setAnomalies(sorted)
    } catch (e: any) {
      setError(e.message)
      setAnomalies([])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchData() }, [fetchData])

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

  const severityIcon = (severity: string) => {
    if (severity === 'critical') return <AlertCircle className="w-4 h-4 text-red-500" />
    if (severity === 'high') return <AlertTriangle className="w-4 h-4 text-orange-400" />
    return <AlertTriangle className="w-4 h-4 text-yellow-400" />
  }

  const timeSince = (iso: string) => {
    const ms = Date.now() - new Date(iso).getTime()
    if (ms < 60000) return `${Math.floor(ms / 1000)}s ago`
    if (ms < 3600000) return `${Math.floor(ms / 60000)}m ago`
    if (ms < 86400000) return `${Math.floor(ms / 3600000)}h ago`
    return `${Math.floor(ms / 86400000)}d ago`
  }

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <div className="flex items-center gap-2">
          <Activity className="w-6 h-6 text-teal-400" />
          <h1 className="text-2xl font-bold text-white">Anomaly Detection</h1>
        </div>
        <button
          onClick={fetchData}
          className="flex items-center gap-2 px-3 py-1.5 text-sm bg-white/10 hover:bg-white/20 rounded-md text-white"
        >
          <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} /> Refresh
        </button>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-900/30 border border-red-700 rounded text-sm text-red-300">
          {error}
        </div>
      )}

      {loading && (
        <div className="flex justify-center py-12">
          <Loader2 className="w-6 h-6 animate-spin text-teal-400" />
        </div>
      )}

      {!loading && anomalies.length === 0 && !error && (
        <div className="text-center py-12 text-gray-500">
          No anomalies detected
        </div>
      )}

      {!loading && anomalies.length > 0 && (
        <div className="space-y-2">
          {anomalies.map(a => (
            <div
              key={a.id}
              className="p-4 bg-gray-800/50 border border-gray-700/50 rounded-lg"
            >
              <div className="flex items-start gap-3">
                <div className="mt-0.5">{severityIcon(a.severity)}</div>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 mb-1">
                    <span className="text-sm font-medium text-white">{a.metric}</span>
                    {severityBadge(a.severity)}
                    <span className={`px-1.5 py-0.5 text-xs rounded font-mono ${
                      Math.abs(a.score) >= 3 ? 'bg-red-900/20 text-red-300' :
                      Math.abs(a.score) >= 2 ? 'bg-orange-900/20 text-orange-300' :
                      'bg-gray-700 text-gray-400'
                    }`}>
                      z={a.score.toFixed(2)}
                    </span>
                  </div>
                  <div className="flex items-center gap-3 text-xs text-gray-400">
                    <span className="px-1.5 py-0.5 bg-gray-700 rounded">{a.service}</span>
                    <span>{a.namespace}</span>
                  </div>
                  <div className="flex items-center gap-4 mt-2 text-xs">
                    <span className="text-gray-500">
                      Expected: <span className="text-gray-300 font-mono">{a.expected.toFixed(2)}</span>
                    </span>
                    <span className="text-gray-500">
                      Observed: <span className={`font-mono ${
                        a.observed > a.expected ? 'text-red-300' : 'text-green-300'
                      }`}>{a.observed.toFixed(2)}</span>
                    </span>
                  </div>
                </div>
                <div className="text-right shrink-0">
                  <div className="text-xs text-gray-500">{timeSince(a.detectedAt)}</div>
                  <div className="text-xs text-gray-600 mt-1">
                    {new Date(a.detectedAt).toLocaleString()}
                  </div>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

'use client'

import { useEffect, useState, useCallback } from 'react'
import Link from 'next/link'
import { apiFetch } from '@/lib/api'
import { RefreshCw, Loader2, AlertCircle, AlertTriangle, Clock, CheckCircle, Zap } from 'lucide-react'

interface Incident {
  id: number
  severity: string
  status: string
  detectedAt: string
  resolvedAt?: string
  affected: string[]
  summary: string
  signals: { kind: string }[]
  rcaReport?: { summary: string }
}

export default function IncidentsPage() {
  const [incidents, setIncidents] = useState<Incident[]>([])
  const [loading, setLoading] = useState(true)
  const [statusFilter, setStatusFilter] = useState('open')

  const fetch_ = useCallback(async () => {
    setLoading(true)
    try {
      const params = statusFilter ? `?status=${statusFilter}` : ''
      const data = await apiFetch<{ incidents: Incident[] }>(`/api/v1/incidents${params}`)
      setIncidents(data.incidents || [])
    } catch { setIncidents([]) }
    finally { setLoading(false) }
  }, [statusFilter])

  useEffect(() => { fetch_() }, [fetch_])

  const sevIcon = (sev: string) => {
    if (sev === 'critical') return <AlertCircle className="w-4 h-4 text-red-500" />
    if (sev === 'high') return <AlertTriangle className="w-4 h-4 text-orange-400" />
    return <AlertTriangle className="w-4 h-4 text-yellow-400" />
  }

  const statusBadge = (s: string) => {
    const styles: Record<string, string> = {
      open: 'bg-red-900/30 text-red-300',
      investigating: 'bg-blue-900/30 text-blue-300',
      resolved: 'bg-green-900/30 text-green-300',
      dismissed: 'bg-gray-700 text-gray-400',
    }
    return <span className={`px-1.5 py-0.5 text-xs rounded ${styles[s] || styles.open}`}>{s}</span>
  }

  const timeSince = (iso: string) => {
    const ms = Date.now() - new Date(iso).getTime()
    if (ms < 60000) return `${Math.floor(ms / 1000)}s`
    if (ms < 3600000) return `${Math.floor(ms / 60000)}m`
    if (ms < 86400000) return `${Math.floor(ms / 3600000)}h`
    return `${Math.floor(ms / 86400000)}d`
  }

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold text-white">Incidents & RCA</h1>
        <div className="flex items-center gap-3">
          <select value={statusFilter} onChange={e => setStatusFilter(e.target.value)}
            className="bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-sm text-white">
            <option value="">All</option>
            <option value="open">Open</option>
            <option value="investigating">Investigating</option>
            <option value="resolved">Resolved</option>
          </select>
          <button onClick={fetch_} className="flex items-center gap-2 px-3 py-1.5 text-sm bg-cluster-border/40 hover:bg-cluster-border/60 rounded text-cluster-text">
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} /> Refresh
          </button>
        </div>
      </div>

      {loading && <div className="flex justify-center py-12"><Loader2 className="w-6 h-6 animate-spin text-blue-400" /></div>}

      {!loading && incidents.length === 0 && (
        <div className="text-center py-12 text-gray-500">
          No incidents detected. Incidents are created automatically when the correlator clusters related error signals.
        </div>
      )}

      {!loading && incidents.length > 0 && (
        <div className="space-y-2">
          {incidents.map(inc => (
            <Link key={inc.id} href={`/incidents/${inc.id}`}
              className="block p-4 bg-gray-800/50 border border-gray-700/50 rounded-lg hover:bg-gray-800 transition-colors">
              <div className="flex items-start gap-3">
                {sevIcon(inc.severity)}
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 mb-1">
                    <span className="text-sm font-medium text-white">INC-{inc.id}</span>
                    {statusBadge(inc.status)}
                    {inc.rcaReport && <Zap className="w-3.5 h-3.5 text-purple-400" />}
                  </div>
                  <p className="text-sm text-gray-400 truncate">{inc.summary || 'No summary'}</p>
                  <div className="flex items-center gap-2 mt-1 text-xs text-gray-500">
                    <Clock className="w-3 h-3" />
                    {timeSince(inc.detectedAt)} ago
                    <span className="text-gray-600">|</span>
                    {inc.signals?.length || 0} signals
                    <span className="text-gray-600">|</span>
                    {inc.affected?.join(', ')}
                  </div>
                </div>
                <div className="text-xs text-gray-500 shrink-0">
                  {new Date(inc.detectedAt).toLocaleString()}
                </div>
              </div>
            </Link>
          ))}
        </div>
      )}
    </div>
  )
}

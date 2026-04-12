'use client'

import { useEffect, useState, useCallback } from 'react'
import Link from 'next/link'
import { apiFetch } from '@/lib/api'
import {
  Search, RefreshCw, AlertCircle, AlertTriangle,
  Clock, Loader2, XCircle, CheckCircle
} from 'lucide-react'

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
}

export default function ErrorsPage() {
  const [groups, setGroups] = useState<ErrorGroup[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [statusFilter, setStatusFilter] = useState('open')
  const [serviceFilter, setServiceFilter] = useState('')

  const fetchGroups = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const params = new URLSearchParams()
      if (statusFilter) params.set('status', statusFilter)
      if (serviceFilter) params.set('service', serviceFilter)
      if (search) params.set('search', search)
      const data = await apiFetch<{ groups: ErrorGroup[]; totalCount: number }>(
        `/api/v1/errors/groups?${params}`
      )
      setGroups(data.groups || [])
    } catch (e: any) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }, [statusFilter, serviceFilter, search])

  useEffect(() => { fetchGroups() }, [fetchGroups])

  const services = Array.from(new Set(groups.map(g => g.service))).sort()

  const severityIcon = (g: ErrorGroup) => {
    if (g.level === 'fatal' || g.level === 'panic') return <XCircle className="w-4 h-4 text-red-500" />
    if (g.reason?.startsWith('exception') || g.reason === 'oom') return <AlertCircle className="w-4 h-4 text-red-400" />
    if (g.reason === 'timeout' || g.reason === 'http.5xx') return <AlertTriangle className="w-4 h-4 text-orange-400" />
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
        <h1 className="text-2xl font-bold text-white">Errors</h1>
        <button onClick={fetchGroups} className="flex items-center gap-2 px-3 py-1.5 text-sm bg-white/10 hover:bg-white/20 rounded-md text-white">
          <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} /> Refresh
        </button>
      </div>

      {/* Filters */}
      <div className="flex gap-3 mb-4">
        <select value={statusFilter} onChange={e => setStatusFilter(e.target.value)}
          className="bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-sm text-white">
          <option value="">All statuses</option>
          <option value="open">Open</option>
          <option value="resolved">Resolved</option>
          <option value="ignored">Ignored</option>
        </select>
        <select value={serviceFilter} onChange={e => setServiceFilter(e.target.value)}
          className="bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-sm text-white">
          <option value="">All services</option>
          {services.map(s => <option key={s} value={s}>{s}</option>)}
        </select>
        <div className="relative flex-1 max-w-sm">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500" />
          <input type="text" placeholder="Search errors..." value={search}
            onChange={e => setSearch(e.target.value)}
            className="w-full pl-9 pr-3 py-1.5 bg-gray-800 border border-gray-700 rounded text-sm text-white placeholder-gray-500" />
        </div>
        <span className="text-sm text-gray-400 self-center">{groups.length} groups</span>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-900/30 border border-red-700 rounded text-sm text-red-300">{error}</div>
      )}

      {loading && (
        <div className="flex justify-center py-12">
          <Loader2 className="w-6 h-6 animate-spin text-blue-400" />
        </div>
      )}

      {!loading && groups.length > 0 && (
        <div className="space-y-2">
          {groups.map(g => (
            <Link key={g.id} href={`/errors/${g.id}`}
              className="block p-4 bg-gray-800/50 border border-gray-700/50 rounded-lg hover:bg-gray-800 transition-colors">
              <div className="flex items-start gap-3">
                <div className="mt-0.5">{severityIcon(g)}</div>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 mb-1">
                    <span className="text-sm font-medium text-white truncate">{g.title}</span>
                    {g.exceptionType && (
                      <span className="text-xs text-gray-500 font-mono">{g.exceptionType}</span>
                    )}
                  </div>
                  <div className="flex items-center gap-3 text-xs text-gray-400">
                    <span className="px-1.5 py-0.5 bg-gray-700 rounded">{g.service}</span>
                    <span>{g.namespace}</span>
                    <span className="text-gray-500">|</span>
                    <span className="font-mono">{g.reason}</span>
                  </div>
                </div>
                <div className="text-right shrink-0">
                  <div className="text-sm font-medium text-white">{g.count.toLocaleString()}</div>
                  <div className="text-xs text-gray-500">{timeSince(g.lastSeen)}</div>
                </div>
                <div className="shrink-0">
                  {g.status === 'open' && <span className="px-1.5 py-0.5 text-xs bg-red-900/30 text-red-300 rounded">open</span>}
                  {g.status === 'resolved' && <CheckCircle className="w-4 h-4 text-green-400" />}
                  {g.status === 'ignored' && <span className="px-1.5 py-0.5 text-xs bg-gray-700 text-gray-400 rounded">ignored</span>}
                </div>
              </div>
            </Link>
          ))}
        </div>
      )}

      {!loading && groups.length === 0 && !error && (
        <div className="text-center py-12 text-gray-500">
          No error groups found. Errors will appear here when the pod log collector detects exceptions, timeouts, or other issues.
        </div>
      )}
    </div>
  )
}

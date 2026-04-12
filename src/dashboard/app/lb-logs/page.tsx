'use client'

import { useEffect, useState, useCallback } from 'react'
import { apiFetch } from '@/lib/api'
import {
  RefreshCw, Loader2, Activity, AlertTriangle,
  Clock, ArrowUpRight
} from 'lucide-react'

interface LBInfo {
  name: string
  type: string
}

interface LBStatsData {
  lbName: string
  lbType: string
  totalRequests: number
  count2xx: number
  count4xx: number
  count5xx: number
  p50Ms: number
  p95Ms: number
  p99Ms: number
  avgMs: number
}

interface URLStatsRow {
  urlPattern: string
  httpMethod: string
  totalCount: number
  count5xx: number
  count4xx: number
  p95Ms: number
  p99Ms: number
}

export default function LBLogsPage() {
  const [lbs, setLbs] = useState<LBInfo[]>([])
  const [selectedLB, setSelectedLB] = useState('')
  const [stats, setStats] = useState<LBStatsData | null>(null)
  const [urls, setUrls] = useState<URLStatsRow[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    apiFetch<{ loadBalancers: LBInfo[] }>('/api/v1/lb/list')
      .then(d => {
        setLbs(d.loadBalancers || [])
        if (d.loadBalancers?.length > 0) {
          setSelectedLB(d.loadBalancers[0].name)
        }
      })
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [])

  const fetchStats = useCallback(async () => {
    if (!selectedLB) return
    setLoading(true)
    try {
      const [s, u] = await Promise.all([
        apiFetch<LBStatsData>(`/api/v1/lb/${selectedLB}/stats`),
        apiFetch<{ urls: URLStatsRow[] }>(`/api/v1/lb/${selectedLB}/top-urls`),
      ])
      setStats(s)
      setUrls(u.urls || [])
    } catch {
      setStats(null)
      setUrls([])
    } finally {
      setLoading(false)
    }
  }, [selectedLB])

  useEffect(() => { fetchStats() }, [fetchStats])

  const fmtMs = (ms: number) => ms < 1 ? '<1ms' : `${Math.round(ms)}ms`
  const fmtPct = (n: number, total: number) => total === 0 ? '0%' : `${((n / total) * 100).toFixed(2)}%`

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold text-white">Load Balancer Logs</h1>
        <div className="flex items-center gap-3">
          {lbs.length > 0 && (
            <select value={selectedLB} onChange={e => setSelectedLB(e.target.value)}
              className="bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-sm text-white">
              {lbs.map(lb => <option key={lb.name} value={lb.name}>{lb.name} ({lb.type})</option>)}
            </select>
          )}
          <button onClick={fetchStats}
            className="flex items-center gap-2 px-3 py-1.5 text-sm bg-white/10 hover:bg-white/20 rounded text-white">
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} /> Refresh
          </button>
        </div>
      </div>

      {loading && !stats && (
        <div className="flex justify-center py-12"><Loader2 className="w-6 h-6 animate-spin text-blue-400" /></div>
      )}

      {lbs.length === 0 && !loading && (
        <div className="text-center py-12 text-gray-500">
          No load balancers configured. Set LB_CONFIGS on the collector-lblogs service to start ingesting logs.
        </div>
      )}

      {stats && (
        <>
          {/* Stats cards */}
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
            <StatCard
              icon={<Activity className="w-4 h-4 text-blue-400" />}
              label="Total Requests"
              value={stats.totalRequests.toLocaleString()}
            />
            <StatCard
              icon={<AlertTriangle className="w-4 h-4 text-red-400" />}
              label="5xx Errors"
              value={`${stats.count5xx.toLocaleString()} (${fmtPct(stats.count5xx, stats.totalRequests)})`}
              alert={stats.count5xx > 0}
            />
            <StatCard
              icon={<Clock className="w-4 h-4 text-yellow-400" />}
              label="P95 Latency"
              value={fmtMs(stats.p95Ms)}
            />
            <StatCard
              icon={<ArrowUpRight className="w-4 h-4 text-purple-400" />}
              label="P99 Latency"
              value={fmtMs(stats.p99Ms)}
            />
          </div>

          {/* Breakdown */}
          <div className="grid grid-cols-3 gap-4 mb-8">
            <div className="p-4 bg-gray-800/50 border border-gray-700/50 rounded-lg">
              <div className="text-xs text-gray-400 mb-1">2xx</div>
              <div className="text-lg font-bold text-green-400">{stats.count2xx.toLocaleString()}</div>
            </div>
            <div className="p-4 bg-gray-800/50 border border-gray-700/50 rounded-lg">
              <div className="text-xs text-gray-400 mb-1">4xx</div>
              <div className="text-lg font-bold text-yellow-400">{stats.count4xx.toLocaleString()}</div>
            </div>
            <div className="p-4 bg-gray-800/50 border border-gray-700/50 rounded-lg">
              <div className="text-xs text-gray-400 mb-1">Avg Latency</div>
              <div className="text-lg font-bold text-white">{fmtMs(stats.avgMs)}</div>
            </div>
          </div>

          {/* Top failing URLs */}
          <h2 className="text-lg font-semibold text-white mb-3">Top URL Patterns</h2>
          {urls.length === 0 ? (
            <div className="text-center py-8 text-gray-500">No URL data yet</div>
          ) : (
            <div className="overflow-x-auto rounded-lg border border-gray-700">
              <table className="w-full text-sm">
                <thead className="bg-gray-800/50">
                  <tr>
                    <th className="text-left px-4 py-2.5 text-gray-400 font-medium">Method</th>
                    <th className="text-left px-4 py-2.5 text-gray-400 font-medium">URL Pattern</th>
                    <th className="text-right px-4 py-2.5 text-gray-400 font-medium">Total</th>
                    <th className="text-right px-4 py-2.5 text-gray-400 font-medium">5xx</th>
                    <th className="text-right px-4 py-2.5 text-gray-400 font-medium">4xx</th>
                    <th className="text-right px-4 py-2.5 text-gray-400 font-medium">P95</th>
                    <th className="text-right px-4 py-2.5 text-gray-400 font-medium">P99</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-700/50">
                  {urls.map((u, i) => (
                    <tr key={i} className="hover:bg-white/5">
                      <td className="px-4 py-2.5">
                        <span className="px-1.5 py-0.5 text-xs bg-gray-700 rounded font-mono">{u.httpMethod}</span>
                      </td>
                      <td className="px-4 py-2.5 font-mono text-xs text-gray-300">{u.urlPattern}</td>
                      <td className="px-4 py-2.5 text-right text-gray-300">{u.totalCount.toLocaleString()}</td>
                      <td className={`px-4 py-2.5 text-right ${u.count5xx > 0 ? 'text-red-400 font-medium' : 'text-gray-500'}`}>
                        {u.count5xx}
                      </td>
                      <td className="px-4 py-2.5 text-right text-gray-400">{u.count4xx}</td>
                      <td className="px-4 py-2.5 text-right text-gray-300">{fmtMs(u.p95Ms)}</td>
                      <td className="px-4 py-2.5 text-right text-gray-300">{fmtMs(u.p99Ms)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}
    </div>
  )
}

function StatCard({ icon, label, value, alert }: { icon: React.ReactNode; label: string; value: string; alert?: boolean }) {
  return (
    <div className={`p-4 rounded-lg border ${alert ? 'bg-red-900/10 border-red-700/50' : 'bg-gray-800/50 border-gray-700/50'}`}>
      <div className="flex items-center gap-1.5 text-xs text-gray-400 mb-1">{icon}{label}</div>
      <div className="text-lg font-bold text-white">{value}</div>
    </div>
  )
}

'use client'

import { useEffect, useState, useCallback } from 'react'
import { useParams, useSearchParams } from 'next/navigation'
import Link from 'next/link'
import { apiFetch } from '@/lib/api'
import {
  Search, RefreshCw, ChevronLeft, ChevronRight,
  AlertCircle, CheckCircle, Clock, Loader2
} from 'lucide-react'

interface ResourceRow {
  name: string
  namespace: string
  kind: string
  status?: string
  age?: string
  createdAt?: string
  readyReplicas?: number
  desiredReplicas?: number
  availableReplicas?: number
  readyContainers?: number
  totalContainers?: number
  restarts?: number
  nodeName?: string
  serviceType?: string
  clusterIP?: string
  ready?: string
  labels?: Record<string, string>
}

interface ListResponse {
  kind: string
  namespace: string
  totalCount: number
  items: ResourceRow[]
}

const PAGE_SIZE = 50

export default function ResourceListPage() {
  const params = useParams()
  const searchParams = useSearchParams()
  const kind = params.kind as string
  const group = searchParams.get('group') || 'core'
  const version = searchParams.get('version') || 'v1'

  const [items, setItems] = useState<ResourceRow[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [nsFilter, setNsFilter] = useState('')
  const [namespaces, setNamespaces] = useState<string[]>([])
  const [page, setPage] = useState(0)

  const isNamespaced = !['nodes', 'namespaces', 'clusterroles', 'clusterrolebindings', 'persistentvolumes', 'storageclasses', 'ingressclasses', 'priorityclasses'].includes(kind)

  const fetchResources = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const g = group === 'core' ? 'core' : group
      let path: string
      if (isNamespaced && nsFilter) {
        path = `/api/v1/k8s/ns/${nsFilter}/${g}/${version}/${kind}`
      } else if (isNamespaced) {
        // List across all namespaces by omitting namespace — backend handles it
        // Actually our API needs a namespace for namespaced resources.
        // Use a special "all" convention or list cluster-scoped.
        path = `/api/v1/k8s/cluster/${g}/${version}/${kind}`
      } else {
        path = `/api/v1/k8s/cluster/${g}/${version}/${kind}`
      }
      if (search) path += `?search=${encodeURIComponent(search)}`
      const data = await apiFetch<ListResponse>(path)
      setItems(data.items || [])
    } catch (e: any) {
      setError(e.message)
      setItems([])
    } finally {
      setLoading(false)
    }
  }, [kind, group, version, nsFilter, search, isNamespaced])

  useEffect(() => {
    fetchResources()
  }, [fetchResources])

  useEffect(() => {
    apiFetch<{ namespaces: string[] }>('/api/v1/k8s/namespaces')
      .then(d => setNamespaces(d.namespaces))
      .catch(() => {})
  }, [])

  const paged = items.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE)
  const totalPages = Math.ceil(items.length / PAGE_SIZE)
  const displayKind = kind.charAt(0).toUpperCase() + kind.slice(1)

  const statusIcon = (row: ResourceRow) => {
    const s = row.status?.toLowerCase()
    if (s === 'running' || s === 'active' || s === 'bound' || row.ready === 'True')
      return <CheckCircle className="w-4 h-4 text-green-400" />
    if (s === 'pending' || s === 'terminating')
      return <Clock className="w-4 h-4 text-yellow-400" />
    if (s === 'failed' || s === 'crashloopbackoff' || s === 'error')
      return <AlertCircle className="w-4 h-4 text-red-400" />
    return null
  }

  const detailHref = (row: ResourceRow) => {
    const ns = row.namespace || '_cluster'
    return `/workloads/${kind}/${ns}/${row.name}?group=${group}&version=${version}`
  }

  return (
    <div className="p-6">
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold text-white">{displayKind}</h1>
        <button
          onClick={fetchResources}
          className="flex items-center gap-2 px-3 py-1.5 text-sm bg-white/10 hover:bg-white/20 rounded-md text-white transition-colors"
        >
          <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
          Refresh
        </button>
      </div>

      {/* Filters */}
      <div className="flex gap-3 mb-4">
        {isNamespaced && (
          <select
            value={nsFilter}
            onChange={e => { setNsFilter(e.target.value); setPage(0) }}
            className="bg-gray-800 border border-gray-700 rounded-md px-3 py-1.5 text-sm text-white"
          >
            <option value="">All namespaces</option>
            {namespaces.map(ns => (
              <option key={ns} value={ns}>{ns}</option>
            ))}
          </select>
        )}
        <div className="relative flex-1 max-w-sm">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500" />
          <input
            type="text"
            placeholder="Filter by name..."
            value={search}
            onChange={e => { setSearch(e.target.value); setPage(0) }}
            className="w-full pl-9 pr-3 py-1.5 bg-gray-800 border border-gray-700 rounded-md text-sm text-white placeholder-gray-500"
          />
        </div>
        <span className="text-sm text-gray-400 self-center">
          {items.length} resource{items.length !== 1 ? 's' : ''}
        </span>
      </div>

      {/* Error */}
      {error && (
        <div className="mb-4 p-3 bg-red-900/30 border border-red-700 rounded-md text-sm text-red-300">
          {error}
        </div>
      )}

      {/* Loading */}
      {loading && (
        <div className="flex items-center justify-center py-12">
          <Loader2 className="w-6 h-6 animate-spin text-blue-400" />
        </div>
      )}

      {/* Table */}
      {!loading && items.length > 0 && (
        <>
          <div className="overflow-x-auto rounded-lg border border-gray-700">
            <table className="w-full text-sm">
              <thead className="bg-gray-800/50">
                <tr>
                  <th className="text-left px-4 py-2.5 text-gray-400 font-medium">Name</th>
                  {isNamespaced && <th className="text-left px-4 py-2.5 text-gray-400 font-medium">Namespace</th>}
                  <th className="text-left px-4 py-2.5 text-gray-400 font-medium">Status</th>
                  <th className="text-left px-4 py-2.5 text-gray-400 font-medium">Age</th>
                  {kind === 'pods' && <th className="text-left px-4 py-2.5 text-gray-400 font-medium">Ready</th>}
                  {kind === 'pods' && <th className="text-left px-4 py-2.5 text-gray-400 font-medium">Restarts</th>}
                  {kind === 'pods' && <th className="text-left px-4 py-2.5 text-gray-400 font-medium">Node</th>}
                  {(kind === 'deployments' || kind === 'statefulsets' || kind === 'replicasets') && (
                    <th className="text-left px-4 py-2.5 text-gray-400 font-medium">Ready</th>
                  )}
                  {kind === 'services' && <th className="text-left px-4 py-2.5 text-gray-400 font-medium">Type</th>}
                  {kind === 'services' && <th className="text-left px-4 py-2.5 text-gray-400 font-medium">Cluster IP</th>}
                  {kind === 'nodes' && <th className="text-left px-4 py-2.5 text-gray-400 font-medium">Ready</th>}
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-700/50">
                {paged.map(row => (
                  <tr key={`${row.namespace}/${row.name}`} className="hover:bg-white/5 transition-colors">
                    <td className="px-4 py-2.5">
                      <Link href={detailHref(row)} className="text-blue-400 hover:text-blue-300 hover:underline">
                        {row.name}
                      </Link>
                    </td>
                    {isNamespaced && <td className="px-4 py-2.5 text-gray-400">{row.namespace}</td>}
                    <td className="px-4 py-2.5">
                      <span className="flex items-center gap-1.5">
                        {statusIcon(row)}
                        <span className="text-gray-300">{row.status || '-'}</span>
                      </span>
                    </td>
                    <td className="px-4 py-2.5 text-gray-400">{row.age || '-'}</td>
                    {kind === 'pods' && (
                      <td className="px-4 py-2.5 text-gray-300">
                        {row.readyContainers ?? '-'}/{row.totalContainers ?? '-'}
                      </td>
                    )}
                    {kind === 'pods' && <td className="px-4 py-2.5 text-gray-400">{row.restarts ?? '-'}</td>}
                    {kind === 'pods' && <td className="px-4 py-2.5 text-gray-400">{row.nodeName || '-'}</td>}
                    {(kind === 'deployments' || kind === 'statefulsets' || kind === 'replicasets') && (
                      <td className="px-4 py-2.5 text-gray-300">
                        {row.readyReplicas ?? 0}/{row.desiredReplicas ?? 0}
                      </td>
                    )}
                    {kind === 'services' && <td className="px-4 py-2.5 text-gray-400">{row.serviceType || '-'}</td>}
                    {kind === 'services' && <td className="px-4 py-2.5 text-gray-400 font-mono text-xs">{row.clusterIP || '-'}</td>}
                    {kind === 'nodes' && <td className="px-4 py-2.5 text-gray-300">{row.ready || '-'}</td>}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {/* Pagination */}
          {totalPages > 1 && (
            <div className="flex items-center justify-between mt-4 text-sm text-gray-400">
              <span>Page {page + 1} of {totalPages}</span>
              <div className="flex gap-2">
                <button
                  onClick={() => setPage(p => Math.max(0, p - 1))}
                  disabled={page === 0}
                  className="p-1 rounded hover:bg-white/10 disabled:opacity-30"
                >
                  <ChevronLeft className="w-4 h-4" />
                </button>
                <button
                  onClick={() => setPage(p => Math.min(totalPages - 1, p + 1))}
                  disabled={page >= totalPages - 1}
                  className="p-1 rounded hover:bg-white/10 disabled:opacity-30"
                >
                  <ChevronRight className="w-4 h-4" />
                </button>
              </div>
            </div>
          )}
        </>
      )}

      {/* Empty state */}
      {!loading && items.length === 0 && !error && (
        <div className="text-center py-12 text-gray-500">
          No {displayKind.toLowerCase()} found
          {nsFilter && ` in namespace "${nsFilter}"`}
          {search && ` matching "${search}"`}
        </div>
      )}
    </div>
  )
}

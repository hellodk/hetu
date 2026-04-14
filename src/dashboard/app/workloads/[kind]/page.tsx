'use client'

import { useEffect, useState, useCallback, useRef, useMemo } from 'react'
import { useParams, useSearchParams } from 'next/navigation'
import Link from 'next/link'
import { apiFetch, getWsUrl } from '@/lib/api'
import {
  Search, RefreshCw, ChevronLeft, ChevronRight,
  AlertCircle, CheckCircle, Clock, Loader2, X,
  ArrowDown, Pause, Play, Trash2, Download, Box,
  ChevronDown
} from 'lucide-react'

/* ------------------------------------------------------------------ */
/*  Types                                                              */
/* ------------------------------------------------------------------ */

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

interface ChildPod {
  metadata: { name: string; namespace: string; creationTimestamp?: string; uid?: string }
  status: {
    phase: string
    podIP?: string
    hostIP?: string
    containerStatuses?: ContainerStatus[]
    initContainerStatuses?: ContainerStatus[]
    conditions?: { type: string; status: string }[]
  }
  spec: {
    nodeName?: string
    containers?: { name: string; image: string }[]
    initContainers?: { name: string; image: string }[]
  }
}

interface ContainerStatus {
  name: string
  ready: boolean
  restartCount: number
  started?: boolean
  state?: {
    running?: { startedAt?: string }
    waiting?: { reason?: string; message?: string }
    terminated?: { reason?: string; exitCode?: number }
  }
  image?: string
}

type LogLevel = 'error' | 'warn' | 'info' | 'debug' | 'default'

interface LogLine {
  id: number
  text: string
  level: LogLevel
  podName?: string
  timestamp?: string
}

/* ------------------------------------------------------------------ */
/*  Helpers                                                            */
/* ------------------------------------------------------------------ */

const PAGE_SIZE = 50

function detectLogLevel(line: string): LogLevel {
  const lower = line.toLowerCase()
  if (/\b(error|fatal|panic|exception)\b/i.test(lower)) return 'error'
  if (/\b(warn|warning)\b/i.test(lower)) return 'warn'
  if (/\b(debug|trace)\b/i.test(lower)) return 'debug'
  if (/\b(info)\b/i.test(lower)) return 'info'
  return 'default'
}

function getLogLevelBg(level: LogLevel): string {
  switch (level) {
    case 'error': return 'bg-red-950/60'
    case 'warn': return 'bg-yellow-950/40'
    case 'debug': return 'text-gray-500'
    default: return ''
  }
}

function getLogLevelColor(level: LogLevel): string {
  switch (level) {
    case 'error': return 'text-red-400'
    case 'warn': return 'text-yellow-400'
    case 'debug': return 'text-gray-500'
    case 'info': return 'text-gray-300'
    default: return 'text-gray-300'
  }
}

function formatAge(ts: string | undefined): string {
  if (!ts) return '-'
  const diff = Date.now() - new Date(ts).getTime()
  const seconds = Math.floor(diff / 1000)
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h`
  const days = Math.floor(hours / 24)
  return `${days}d`
}

function getPodPhaseColor(phase: string | undefined): string {
  switch (phase?.toLowerCase()) {
    case 'running': return 'bg-green-900/50 text-green-300 border-green-700/50'
    case 'succeeded': return 'bg-blue-900/50 text-blue-300 border-blue-700/50'
    case 'pending': return 'bg-yellow-900/50 text-yellow-300 border-yellow-700/50'
    case 'failed': return 'bg-red-900/50 text-red-300 border-red-700/50'
    default: return 'bg-gray-700 text-gray-300 border-gray-600'
  }
}

function getContainerStateBadge(cs: ContainerStatus): { label: string; cls: string } {
  if (cs.state?.running) return { label: 'Running', cls: 'bg-green-900/50 text-green-300' }
  if (cs.state?.waiting) {
    const reason = cs.state.waiting.reason || 'Waiting'
    if (reason === 'CrashLoopBackOff') return { label: reason, cls: 'bg-red-900/50 text-red-300' }
    return { label: reason, cls: 'bg-yellow-900/50 text-yellow-300' }
  }
  if (cs.state?.terminated) {
    const reason = cs.state.terminated.reason || 'Terminated'
    const code = cs.state.terminated.exitCode
    if (code === 0) return { label: `${reason} (0)`, cls: 'bg-gray-700 text-gray-300' }
    return { label: `${reason} (${code})`, cls: 'bg-red-900/50 text-red-300' }
  }
  return { label: 'Unknown', cls: 'bg-gray-700 text-gray-300' }
}

// Pod name color hashing for multi-pod log view
const POD_COLORS = [
  'text-cyan-400', 'text-pink-400', 'text-amber-400', 'text-violet-400',
  'text-emerald-400', 'text-rose-400', 'text-sky-400', 'text-orange-400',
  'text-teal-400', 'text-fuchsia-400', 'text-lime-400', 'text-indigo-400',
]
function podColor(name: string): string {
  let hash = 0
  for (let i = 0; i < name.length; i++) hash = ((hash << 5) - hash + name.charCodeAt(i)) | 0
  return POD_COLORS[Math.abs(hash) % POD_COLORS.length]
}

/* ------------------------------------------------------------------ */
/*  Log Stream Panel                                                   */
/* ------------------------------------------------------------------ */

function LogStreamPanel({
  pods,
  namespace,
  activePodName,
  onClose,
}: {
  pods: ChildPod[]
  namespace: string
  activePodName: string | null
  onClose: () => void
}) {
  const [lines, setLines] = useState<LogLine[]>([])
  const [following, setFollowing] = useState(true)
  const [connected, setConnected] = useState(false)
  const [searchTerm, setSearchTerm] = useState('')
  const [showSearch, setShowSearch] = useState(false)
  const [levelFilter, setLevelFilter] = useState<LogLevel | 'all'>('all')
  const [allPodsMode, setAllPodsMode] = useState(false)
  const [selectedContainer, setSelectedContainer] = useState('')

  const wsRefs = useRef<WebSocket[]>([])
  const bottomRef = useRef<HTMLDivElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const searchInputRef = useRef<HTMLInputElement>(null)
  const lineIdRef = useRef(0)

  // Determine which pod(s) to stream
  const targetPods = useMemo(() => {
    if (allPodsMode) return pods
    if (activePodName) {
      const found = pods.find(p => p.metadata.name === activePodName)
      return found ? [found] : []
    }
    return pods.length > 0 ? [pods[0]] : []
  }, [pods, activePodName, allPodsMode])

  // Containers for the active single pod
  const availableContainers = useMemo(() => {
    if (allPodsMode || targetPods.length !== 1) return []
    const p = targetPods[0]
    return (p.spec.containers || []).map(c => c.name)
  }, [targetPods, allPodsMode])

  // Reset selected container when pod changes
  useEffect(() => {
    setSelectedContainer(availableContainers[0] || '')
  }, [availableContainers])

  const connectAll = useCallback(() => {
    // Close existing connections
    wsRefs.current.forEach(ws => ws.close())
    wsRefs.current = []
    setLines([])
    setConnected(false)
    lineIdRef.current = 0

    const base = getWsUrl()
    const isMulti = targetPods.length > 1

    targetPods.forEach(pod => {
      const podName = pod.metadata.name
      const ns = pod.metadata.namespace || namespace
      const container = !isMulti && selectedContainer ? `&container=${encodeURIComponent(selectedContainer)}` : ''
      const url = `${base}/api/v1/k8s/pods/${ns}/${podName}/logs?follow=true&tail=200${container}`
      const ws = new WebSocket(url)

      ws.onopen = () => setConnected(true)
      ws.onclose = () => {
        // Only mark disconnected if all are closed
        if (wsRefs.current.every(w => w.readyState === WebSocket.CLOSED)) {
          setConnected(false)
        }
      }
      ws.onerror = () => {}
      ws.onmessage = (e) => {
        const text = e.data as string
        // Filter out backend heartbeats — they keep the WS alive but aren't log lines
        if (text.startsWith('{"type":"heartbeat"')) return
        const level = detectLogLevel(text)
        const tsMatch = text.match(/^(\d{4}-\d{2}-\d{2}T[\d:.]+Z?)\s/)
        const logLine: LogLine = {
          id: lineIdRef.current++,
          text,
          level,
          podName: isMulti ? podName : undefined,
          timestamp: tsMatch ? tsMatch[1] : undefined,
        }
        setLines(prev => {
          const next = [...prev, logLine]
          if (next.length > 8000) return next.slice(-6000)
          return next
        })
      }

      wsRefs.current.push(ws)
    })
  }, [targetPods, namespace, selectedContainer])

  useEffect(() => {
    if (targetPods.length > 0) {
      connectAll()
    }
    return () => {
      wsRefs.current.forEach(ws => ws.close())
      wsRefs.current = []
    }
  }, [connectAll])

  // Teleprompter scroll: newest log line sits at the viewport midpoint.
  // A spacer div (50% of container height) below the log lines creates
  // room so the last line can actually be positioned mid-screen.
  const [spacerHeight, setSpacerHeight] = useState(0)
  const programmaticScroll = useRef(false)

  useEffect(() => {
    if (!containerRef.current) return
    const ro = new ResizeObserver(([entry]) => {
      setSpacerHeight(Math.floor(entry.contentRect.height * 0.5))
    })
    ro.observe(containerRef.current)
    return () => ro.disconnect()
  }, [])

  useEffect(() => {
    if (!following || !containerRef.current || !bottomRef.current) return
    programmaticScroll.current = true
    const el = containerRef.current
    const target = bottomRef.current.offsetTop - el.clientHeight * 0.5
    el.scrollTop = Math.max(0, target)
    requestAnimationFrame(() => { programmaticScroll.current = false })
  }, [lines, following, spacerHeight])

  const handleScroll = useCallback(() => {
    if (programmaticScroll.current) return
    if (!containerRef.current || !bottomRef.current) return
    const el = containerRef.current
    const midTarget = bottomRef.current.offsetTop - el.clientHeight * 0.5
    if (midTarget - el.scrollTop > el.clientHeight * 0.3) {
      setFollowing(false)
    }
  }, [])

  // Keyboard shortcut
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'f') {
        e.preventDefault()
        setShowSearch(true)
        setTimeout(() => searchInputRef.current?.focus(), 50)
      }
      if (e.key === 'Escape' && showSearch) {
        setShowSearch(false)
        setSearchTerm('')
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [showSearch])

  const filteredLines = useMemo(() => {
    let result = lines
    if (levelFilter !== 'all') {
      result = result.filter(l => l.level === levelFilter)
    }
    if (searchTerm.trim()) {
      const lower = searchTerm.toLowerCase()
      result = result.filter(l => l.text.toLowerCase().includes(lower))
    }
    return result
  }, [lines, searchTerm, levelFilter])

  const levelCounts = useMemo(() => {
    const counts = { error: 0, warn: 0, info: 0, debug: 0, default: 0 }
    lines.forEach(l => { counts[l.level]++ })
    return counts
  }, [lines])

  const highlightSearch = (text: string) => {
    if (!searchTerm.trim()) return text
    const escaped = searchTerm.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    const parts = text.split(new RegExp(`(${escaped})`, 'gi'))
    return parts.map((part, i) =>
      part.toLowerCase() === searchTerm.toLowerCase()
        ? <mark key={i} className="bg-yellow-500/40 text-yellow-200 rounded-sm px-0.5">{part}</mark>
        : part
    )
  }

  const downloadLogs = () => {
    const text = filteredLines.map(l => l.podName ? `[${l.podName}] ${l.text}` : l.text).join('\n')
    const blob = new Blob([text], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${activePodName || 'all-pods'}.log`
    a.click()
    URL.revokeObjectURL(url)
  }

  return (
    <div className="flex flex-col h-full border-t border-gray-700/50 bg-gray-900/50">
      {/* Toolbar */}
      <div className="flex flex-wrap items-center gap-1.5 px-3 py-2 border-b border-gray-700/50 bg-gray-800/30 text-xs">
        {/* Container selector (single-pod mode only) */}
        {!allPodsMode && availableContainers.length > 1 && (
          <select
            value={selectedContainer}
            onChange={e => setSelectedContainer(e.target.value)}
            className="bg-gray-800 border border-gray-700 rounded px-1.5 py-1 text-xs text-white"
          >
            {availableContainers.map(c => <option key={c} value={c}>{c}</option>)}
          </select>
        )}

        {/* All-pods toggle */}
        {pods.length > 1 && (
          <button
            onClick={() => setAllPodsMode(!allPodsMode)}
            className={`px-2 py-1 rounded text-xs transition-colors ${
              allPodsMode ? 'bg-purple-700 text-white' : 'bg-gray-800 text-gray-400 hover:text-white'
            }`}
          >
            {allPodsMode ? `All ${pods.length} pods` : 'All pods'}
          </button>
        )}

        {/* Connection */}
        <span className={`w-1.5 h-1.5 rounded-full ${connected ? 'bg-green-400 animate-pulse' : 'bg-red-400'}`} />
        <span className="text-gray-500">{connected ? 'Live' : 'Off'}</span>

        {/* Level pills */}
        <div className="flex items-center gap-0.5 ml-1">
          <button
            onClick={() => setLevelFilter('all')}
            className={`px-1.5 py-0.5 rounded text-xs ${levelFilter === 'all' ? 'bg-blue-600 text-white' : 'bg-gray-800 text-gray-400'}`}
          >All</button>
          {levelCounts.error > 0 && (
            <button
              onClick={() => setLevelFilter(levelFilter === 'error' ? 'all' : 'error')}
              className={`px-1.5 py-0.5 rounded text-xs ${levelFilter === 'error' ? 'bg-red-700 text-white' : 'bg-gray-800 text-red-400 hover:bg-red-900/50'}`}
            >ERR {levelCounts.error}</button>
          )}
          {levelCounts.warn > 0 && (
            <button
              onClick={() => setLevelFilter(levelFilter === 'warn' ? 'all' : 'warn')}
              className={`px-1.5 py-0.5 rounded text-xs ${levelFilter === 'warn' ? 'bg-yellow-700 text-white' : 'bg-gray-800 text-yellow-400 hover:bg-yellow-900/50'}`}
            >WARN {levelCounts.warn}</button>
          )}
          {levelCounts.info > 0 && (
            <button
              onClick={() => setLevelFilter(levelFilter === 'info' ? 'all' : 'info')}
              className={`px-1.5 py-0.5 rounded text-xs ${levelFilter === 'info' ? 'bg-blue-700 text-white' : 'bg-gray-800 text-blue-400 hover:bg-blue-900/50'}`}
            >INFO</button>
          )}
          {levelCounts.debug > 0 && (
            <button
              onClick={() => setLevelFilter(levelFilter === 'debug' ? 'all' : 'debug')}
              className={`px-1.5 py-0.5 rounded text-xs ${levelFilter === 'debug' ? 'bg-gray-600 text-white' : 'bg-gray-800 text-gray-500 hover:bg-gray-700'}`}
            >DBG</button>
          )}
        </div>

        <span className="flex-1" />
        <span className="text-gray-500">{filteredLines.length} lines</span>

        <button
          onClick={() => { setShowSearch(!showSearch); if (!showSearch) setTimeout(() => searchInputRef.current?.focus(), 50) }}
          className={`p-1 rounded ${showSearch ? 'bg-blue-600 text-white' : 'text-gray-400 hover:text-white'}`}
          title="Search (Ctrl+F)"
        >
          <Search className="w-3 h-3" />
        </button>
        <button onClick={() => { setLines([]); lineIdRef.current = 0 }} className="p-1 text-gray-400 hover:text-white rounded" title="Clear">
          <Trash2 className="w-3 h-3" />
        </button>
        <button onClick={downloadLogs} className="p-1 text-gray-400 hover:text-white rounded" title="Download">
          <Download className="w-3 h-3" />
        </button>
        {!following && (
          <button
            onClick={() => {
              setFollowing(true)
              if (containerRef.current && bottomRef.current) {
                programmaticScroll.current = true
                containerRef.current.scrollTop = Math.max(0, bottomRef.current.offsetTop - containerRef.current.clientHeight * 0.5)
                requestAnimationFrame(() => { programmaticScroll.current = false })
              }
            }}
            className="flex items-center gap-0.5 px-1.5 py-0.5 bg-blue-600 text-white rounded text-xs"
          >
            <ArrowDown className="w-3 h-3" /> Follow
          </button>
        )}
        <button onClick={onClose} className="p-1 text-gray-400 hover:text-white rounded" title="Close logs">
          <X className="w-3 h-3" />
        </button>
      </div>

      {/* Search bar */}
      {showSearch && (
        <div className="flex items-center gap-2 px-3 py-1.5 border-b border-gray-700/50 bg-gray-800/50">
          <Search className="w-3 h-3 text-gray-500" />
          <input
            ref={searchInputRef}
            type="text"
            value={searchTerm}
            onChange={e => setSearchTerm(e.target.value)}
            placeholder="Search logs..."
            className="flex-1 bg-transparent text-xs text-white placeholder-gray-500 outline-none"
          />
          {searchTerm && (
            <span className="text-xs text-gray-500">{filteredLines.length} matches</span>
          )}
          <button onClick={() => { setShowSearch(false); setSearchTerm('') }} className="text-gray-400 hover:text-white">
            <X className="w-3 h-3" />
          </button>
        </div>
      )}

      {/* Log lines */}
      <div
        ref={containerRef}
        onScroll={handleScroll}
        className="flex-1 overflow-auto font-mono text-xs leading-5 p-2 min-h-0"
      >
        {filteredLines.length === 0 && (
          <div className="text-gray-600 text-center py-4">
            {lines.length === 0 ? 'Waiting for log output...' : 'No lines match filters'}
          </div>
        )}
        {filteredLines.map(line => (
          <div
            key={line.id}
            className={`whitespace-pre-wrap break-all hover:bg-white/5 px-1 ${getLogLevelBg(line.level)}`}
          >
            {line.timestamp && (
              <span className="text-gray-600 mr-2 select-none">{line.timestamp.slice(11, 23)}</span>
            )}
            {line.podName && (
              <span className={`${podColor(line.podName)} mr-1.5 select-none`}>[{line.podName.slice(-20)}]</span>
            )}
            <span className={getLogLevelColor(line.level)}>{highlightSearch(line.text)}</span>
          </div>
        ))}
        {/* Anchor mark — sits right after the last log line */}
        <div ref={bottomRef} />
        {/* Teleprompter spacer: pushes anchor to viewport midpoint */}
        <div style={{ height: spacerHeight }} aria-hidden />
      </div>
    </div>
  )
}

/* ------------------------------------------------------------------ */
/*  Right Panel: Resource Detail / Pod Info                            */
/* ------------------------------------------------------------------ */

function DetailPanel({
  row,
  kind,
  group,
  version,
  onClose,
}: {
  row: ResourceRow
  kind: string
  group: string
  version: string
  onClose: () => void
}) {
  const isWorkloadOwner = ['deployments', 'statefulsets', 'replicasets', 'daemonsets'].includes(kind)
  const isPod = kind === 'pods'
  const ns = row.namespace

  const [childPods, setChildPods] = useState<ChildPod[]>([])
  const [loadingPods, setLoadingPods] = useState(false)
  const [selectedPod, setSelectedPod] = useState<string | null>(null)
  const [showLogs, setShowLogs] = useState(false)

  // For workload owners, fetch child pods
  useEffect(() => {
    if (!isWorkloadOwner || !ns) return
    setLoadingPods(true)
    const g = group === 'core' ? 'core' : group
    apiFetch<{ items: ChildPod[] }>(`/api/v1/k8s/ns/${ns}/${g}/${version}/${kind}/${row.name}/pods`)
      .then(data => {
        setChildPods(data.items || [])
        if (data.items?.length > 0) {
          setSelectedPod(data.items[0].metadata.name)
        }
      })
      .catch(() => setChildPods([]))
      .finally(() => setLoadingPods(false))
  }, [isWorkloadOwner, ns, group, version, kind, row.name])

  // For pods, build a fake child pod from the row for logs
  const podForLogs: ChildPod[] = useMemo(() => {
    if (isPod) {
      return [{
        metadata: { name: row.name, namespace: ns },
        status: { phase: row.status || 'Unknown' },
        spec: { containers: [] }, // containers unknown from list data, log WS defaults to first
      }]
    }
    return childPods
  }, [isPod, row, ns, childPods])

  const activePodForLog = isPod ? row.name : selectedPod

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="flex items-center gap-2 px-4 py-3 border-b border-gray-700/50 bg-gray-800/30">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <h2 className="text-sm font-semibold text-white truncate">{row.name}</h2>
            <button onClick={onClose} className="text-gray-500 hover:text-white ml-auto flex-shrink-0">
              <X className="w-4 h-4" />
            </button>
          </div>
          <div className="flex items-center gap-2 mt-1 text-xs text-gray-400">
            <span>{ns}</span>
            <span className="text-gray-600">/</span>
            <span>{kind}</span>
            {row.age && (
              <>
                <span className="text-gray-600">|</span>
                <span>{row.age}</span>
              </>
            )}
          </div>
        </div>
      </div>

      {/* Status + Key Info */}
      <div className="px-4 py-3 border-b border-gray-700/30 space-y-2.5">
        {/* Status badge */}
        <div className="flex items-center gap-2">
          {row.status && (
            <span className={`px-2 py-0.5 text-xs rounded border ${getPodPhaseColor(row.status)}`}>
              {row.status}
            </span>
          )}
          {(kind === 'deployments' || kind === 'statefulsets' || kind === 'replicasets') && (
            <span className="text-xs text-gray-400">
              {row.readyReplicas ?? 0}/{row.desiredReplicas ?? 0} ready
            </span>
          )}
          {isPod && row.readyContainers !== undefined && (
            <span className="text-xs text-gray-400">
              {row.readyContainers}/{row.totalContainers} containers ready
            </span>
          )}
        </div>

        {/* Key fields grid */}
        <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs">
          {isPod && row.nodeName && (
            <>
              <span className="text-gray-500">Node</span>
              <span className="text-gray-300 font-mono truncate">{row.nodeName}</span>
            </>
          )}
          {isPod && row.restarts !== undefined && (
            <>
              <span className="text-gray-500">Restarts</span>
              <span className={`font-mono ${(row.restarts || 0) > 0 ? 'text-yellow-400' : 'text-gray-300'}`}>{row.restarts}</span>
            </>
          )}
          {row.serviceType && (
            <>
              <span className="text-gray-500">Type</span>
              <span className="text-gray-300">{row.serviceType}</span>
            </>
          )}
          {row.clusterIP && (
            <>
              <span className="text-gray-500">Cluster IP</span>
              <span className="text-gray-300 font-mono">{row.clusterIP}</span>
            </>
          )}
        </div>

        {/* Detail link */}
        <Link
          href={`/workloads/${kind}/${ns || '_cluster'}/${row.name}?group=${group}&version=${version}`}
          className="inline-block text-xs text-blue-400 hover:text-blue-300 hover:underline mt-1"
        >
          View full detail page &rarr;
        </Link>
      </div>

      {/* Child Pods (for workload owners) */}
      {isWorkloadOwner && (
        <div className="border-b border-gray-700/30">
          <div className="px-4 py-2 flex items-center justify-between">
            <span className="text-xs font-medium text-gray-400">
              Pods {loadingPods ? '' : `(${childPods.length})`}
            </span>
            {childPods.length > 0 && (
              <button
                onClick={() => setShowLogs(!showLogs)}
                className={`text-xs px-2 py-0.5 rounded transition-colors ${
                  showLogs ? 'bg-green-700 text-white' : 'bg-gray-800 text-gray-400 hover:text-white'
                }`}
              >
                {showLogs ? 'Hide Logs' : 'Show Logs'}
              </button>
            )}
          </div>
          {loadingPods && (
            <div className="px-4 pb-3">
              <Loader2 className="w-4 h-4 animate-spin text-blue-400" />
            </div>
          )}
          {!loadingPods && childPods.length > 0 && (
            <div className="px-4 pb-2 space-y-1 max-h-48 overflow-y-auto">
              {childPods.map(pod => {
                const name = pod.metadata.name
                const phase = pod.status?.phase
                const cStatuses = pod.status?.containerStatuses || []
                const readyCt = cStatuses.filter(c => c.ready).length
                const totalCt = cStatuses.length
                const restarts = cStatuses.reduce((s, c) => s + (c.restartCount || 0), 0)
                const isSelected = selectedPod === name

                return (
                  <div
                    key={name}
                    onClick={() => setSelectedPod(name)}
                    className={`flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer text-xs transition-colors ${
                      isSelected ? 'bg-blue-900/30 border border-blue-700/50' : 'hover:bg-white/5 border border-transparent'
                    }`}
                  >
                    <span className={`w-1.5 h-1.5 rounded-full flex-shrink-0 ${
                      phase === 'Running' ? 'bg-green-400' : phase === 'Pending' ? 'bg-yellow-400' : 'bg-red-400'
                    }`} />
                    <span className="text-gray-300 font-mono truncate flex-1">{name}</span>
                    <span className="text-gray-500">{readyCt}/{totalCt}</span>
                    {restarts > 0 && (
                      <span className="text-yellow-500">{restarts}x</span>
                    )}

                    {/* Container state badges */}
                    {cStatuses.length > 0 && (
                      <div className="flex gap-0.5">
                        {cStatuses.map(cs => {
                          const badge = getContainerStateBadge(cs)
                          return (
                            <span
                              key={cs.name}
                              title={`${cs.name}: ${badge.label}`}
                              className={`px-1 py-0 rounded text-[10px] ${badge.cls}`}
                            >
                              {cs.name.slice(0, 6)}
                            </span>
                          )
                        })}
                      </div>
                    )}
                  </div>
                )
              })}
            </div>
          )}
        </div>
      )}

      {/* Pod info for selected pod (workload owner) */}
      {isWorkloadOwner && selectedPod && (() => {
        const pod = childPods.find(p => p.metadata.name === selectedPod)
        if (!pod) return null
        return (
          <div className="px-4 py-2 border-b border-gray-700/30 space-y-1.5 text-xs">
            <div className="flex items-center gap-2">
              <span className={`px-1.5 py-0.5 rounded text-[10px] border ${getPodPhaseColor(pod.status.phase)}`}>
                {pod.status.phase}
              </span>
              <span className="text-gray-400 font-mono">{selectedPod}</span>
            </div>
            <div className="grid grid-cols-2 gap-x-3 gap-y-0.5 text-[11px]">
              {pod.status?.podIP && (
                <>
                  <span className="text-gray-500">Pod IP</span>
                  <span className="text-gray-300 font-mono">{pod.status.podIP}</span>
                </>
              )}
              {pod.status?.hostIP && (
                <>
                  <span className="text-gray-500">Host IP</span>
                  <span className="text-gray-300 font-mono">{pod.status.hostIP}</span>
                </>
              )}
              {pod.spec?.nodeName && (
                <>
                  <span className="text-gray-500">Node</span>
                  <span className="text-gray-300 font-mono truncate">{pod.spec.nodeName}</span>
                </>
              )}
              <span className="text-gray-500">Restarts</span>
              <span className="text-gray-300 font-mono">
                {(pod.status?.containerStatuses || []).reduce((s, c) => s + (c.restartCount || 0), 0)}
              </span>
            </div>
          </div>
        )
      })()}

      {/* Show logs button for pods */}
      {isPod && !showLogs && (
        <div className="px-4 py-2 border-b border-gray-700/30">
          <button
            onClick={() => setShowLogs(true)}
            className="text-xs px-2 py-1 rounded bg-gray-800 text-gray-400 hover:text-white transition-colors"
          >
            Show Logs
          </button>
        </div>
      )}

      {/* Log stream */}
      {showLogs && podForLogs.length > 0 && (
        <div className="flex-1 min-h-0 flex flex-col">
          <LogStreamPanel
            pods={podForLogs}
            namespace={ns}
            activePodName={activePodForLog}
            onClose={() => setShowLogs(false)}
          />
        </div>
      )}

      {/* Fill remaining space when no logs */}
      {!showLogs && <div className="flex-1" />}
    </div>
  )
}

/* ------------------------------------------------------------------ */
/*  Main Page Component                                                */
/* ------------------------------------------------------------------ */

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
  const [selected, setSelected] = useState<ResourceRow | null>(null)

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

  // Close panel when kind changes
  useEffect(() => {
    setSelected(null)
  }, [kind])

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

  return (
    <div className="flex h-[calc(100vh-64px)]">
      {/* LEFT PANEL: resource list */}
      <div className={`flex flex-col overflow-hidden transition-all duration-200 ${selected ? 'w-[60%]' : 'w-full'}`}>
        <div className="p-6 flex-1 overflow-y-auto">
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
                    {paged.map(row => {
                      const isActive = selected?.name === row.name && selected?.namespace === row.namespace
                      return (
                        <tr
                          key={`${row.namespace}/${row.name}`}
                          onClick={() => setSelected(isActive ? null : row)}
                          className={`cursor-pointer transition-colors ${
                            isActive
                              ? 'bg-blue-900/20 border-l-2 border-l-blue-500'
                              : 'hover:bg-white/5 border-l-2 border-l-transparent'
                          }`}
                        >
                          <td className="px-4 py-2.5">
                            <span className="text-blue-400">{row.name}</span>
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
                      )
                    })}
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
      </div>

      {/* RIGHT PANEL: detail sidebar */}
      {selected && (
        <div className="w-[40%] border-l border-gray-700 bg-gray-900/60 flex flex-col overflow-hidden">
          <DetailPanel
            key={`${selected.namespace}/${selected.name}`}
            row={selected}
            kind={kind}
            group={group}
            version={version}
            onClose={() => setSelected(null)}
          />
        </div>
      )}
    </div>
  )
}

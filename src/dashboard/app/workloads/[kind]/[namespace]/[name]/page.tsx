'use client'

import { useEffect, useState, useRef, useCallback, useMemo } from 'react'
import { useParams, useSearchParams } from 'next/navigation'
import Link from 'next/link'
import { apiFetch, apiFetchText, getWsUrl } from '@/lib/api'
import { PodExecTerminal } from '@/components/PodExecTerminal'
import { WorkloadActions } from '@/components/WorkloadActions'
import clsx from 'clsx'
import {
  ArrowLeft,
  FileText,
  Code,
  Calendar,
  Loader2,
  Copy,
  Check,
  Terminal,
  ScrollText,
  ChevronDown,
  ChevronRight,
  Search,
  X,
  Download,
  Trash2,
  ArrowDown,
  Pause,
  Play,
  Box,
  Network,
  Server,
  Shield,
  Activity,
  Cpu,
  MemoryStick,
  RefreshCw,
  Hash,
  Globe,
  Container,
  Tag,
  Info,
  AlertCircle,
  CheckCircle,
  Clock,
  XCircle,
} from 'lucide-react'

/* ------------------------------------------------------------------ */
/*  Types                                                              */
/* ------------------------------------------------------------------ */

type Tab = 'summary' | 'yaml' | 'events' | 'logs' | 'exec'

interface K8sEvent {
  type: string
  reason: string
  message: string
  count: number
  firstTimestamp: string
  lastTimestamp: string
  source: { component: string }
}

interface ContainerStatus {
  name: string
  ready: boolean
  restartCount: number
  started?: boolean
  state?: {
    running?: { startedAt?: string }
    waiting?: { reason?: string; message?: string }
    terminated?: { reason?: string; exitCode?: number; startedAt?: string; finishedAt?: string }
  }
  lastState?: {
    terminated?: { reason?: string; exitCode?: number; startedAt?: string; finishedAt?: string }
  }
  image?: string
  imageID?: string
}

interface ContainerSpec {
  name: string
  image: string
  ports?: { containerPort: number; protocol?: string; name?: string }[]
  env?: { name: string; value?: string; valueFrom?: any }[]
  envFrom?: any[]
  resources?: {
    requests?: { cpu?: string; memory?: string }
    limits?: { cpu?: string; memory?: string }
  }
  volumeMounts?: { name: string; mountPath: string; readOnly?: boolean }[]
  livenessProbe?: any
  readinessProbe?: any
  startupProbe?: any
  command?: string[]
  args?: string[]
}

interface PodCondition {
  type: string
  status: string
  reason?: string
  message?: string
  lastTransitionTime?: string
  lastProbeTime?: string
}

type LogLevel = 'error' | 'warn' | 'info' | 'debug' | 'default'

/* ------------------------------------------------------------------ */
/*  Helpers                                                            */
/* ------------------------------------------------------------------ */

function parseResourceValue(val: string | undefined): { value: number; unit: string } | null {
  if (!val) return null
  const match = val.match(/^(\d+\.?\d*)\s*(.*)$/)
  if (!match) return null
  return { value: parseFloat(match[1]), unit: match[2] }
}

function formatAge(timestamp: string | undefined): string {
  if (!timestamp) return '-'
  const diff = Date.now() - new Date(timestamp).getTime()
  const seconds = Math.floor(diff / 1000)
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h`
  const days = Math.floor(hours / 24)
  return `${days}d`
}

function detectLogLevel(line: string): LogLevel {
  const lower = line.toLowerCase()
  // Check common patterns: level=ERROR, [ERROR], "level":"error", ERROR:, etc.
  if (/\b(error|fatal|panic|exception)\b/i.test(lower)) return 'error'
  if (/\b(warn|warning)\b/i.test(lower)) return 'warn'
  if (/\b(debug|trace)\b/i.test(lower)) return 'debug'
  if (/\b(info)\b/i.test(lower)) return 'info'
  return 'default'
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

function getContainerStateLabel(cs: ContainerStatus | undefined): { label: string; color: string } {
  if (!cs?.state) return { label: 'Unknown', color: 'bg-gray-700 text-gray-300' }
  if (cs.state.running) return { label: 'Running', color: 'bg-green-900/50 text-green-300' }
  if (cs.state.waiting) {
    const reason = cs.state.waiting.reason || 'Waiting'
    if (reason === 'CrashLoopBackOff') return { label: reason, color: 'bg-red-900/50 text-red-300' }
    return { label: reason, color: 'bg-yellow-900/50 text-yellow-300' }
  }
  if (cs.state.terminated) {
    const reason = cs.state.terminated.reason || 'Terminated'
    const code = cs.state.terminated.exitCode
    if (code === 0) return { label: `${reason} (0)`, color: 'bg-gray-700 text-gray-300' }
    return { label: `${reason} (${code})`, color: 'bg-red-900/50 text-red-300' }
  }
  return { label: 'Unknown', color: 'bg-gray-700 text-gray-300' }
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

function getConditionIcon(status: string) {
  if (status === 'True') return <CheckCircle className="w-4 h-4 text-green-400" />
  if (status === 'False') return <XCircle className="w-4 h-4 text-red-400" />
  return <Clock className="w-4 h-4 text-yellow-400" />
}

/* ------------------------------------------------------------------ */
/*  Resource bar chart component                                       */
/* ------------------------------------------------------------------ */

function ResourceBar({ label, request, limit }: { label: string; request?: string; limit?: string }) {
  const req = parseResourceValue(request)
  const lim = parseResourceValue(limit)
  if (!req && !lim) return null

  // For visual purposes, show request as percentage of limit
  let pct = 0
  if (req && lim && lim.value > 0) {
    pct = Math.min(100, (req.value / lim.value) * 100)
  } else if (req && !lim) {
    pct = 50 // no limit set, show halfway
  } else if (!req && lim) {
    pct = 0
  }

  const barColor = pct > 80 ? 'bg-red-500' : pct > 60 ? 'bg-yellow-500' : 'bg-blue-500'

  return (
    <div className="space-y-1">
      <div className="flex justify-between text-xs">
        <span className="text-gray-400">{label}</span>
        <span className="text-gray-300 font-mono">
          {request || '-'} / {limit || 'no limit'}
        </span>
      </div>
      <div className="h-1.5 bg-gray-700 rounded-full overflow-hidden">
        <div
          className={clsx('h-full rounded-full transition-all duration-500', barColor)}
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  )
}

/* ------------------------------------------------------------------ */
/*  Enhanced Log Viewer with streaming, search, and level coloring     */
/* ------------------------------------------------------------------ */

interface LogLine {
  id: number
  text: string
  level: LogLevel
  timestamp?: string
}

function EnhancedLogViewer({
  namespace,
  podName,
  containers,
  group,
  version,
  kind,
}: {
  namespace: string
  podName: string
  containers: string[]
  group: string
  version: string
  kind: string
}) {
  const [lines, setLines] = useState<LogLine[]>([])
  const [container, setContainer] = useState(containers[0] || '')
  const [following, setFollowing] = useState(true)
  const [connected, setConnected] = useState(false)
  const [searchTerm, setSearchTerm] = useState('')
  const [showSearch, setShowSearch] = useState(false)
  const [tailLines, setTailLines] = useState<number>(200)
  const [wrapLines, setWrapLines] = useState(true)
  const [levelFilter, setLevelFilter] = useState<LogLevel | 'all'>('all')
  const wsRef = useRef<WebSocket | null>(null)
  const bottomRef = useRef<HTMLDivElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const searchInputRef = useRef<HTMLInputElement>(null)
  const lineIdRef = useRef(0)

  const connect = useCallback(() => {
    wsRef.current?.close()
    setLines([])
    setConnected(false)
    lineIdRef.current = 0

    const base = getWsUrl()
    const url = `${base}/api/v1/k8s/pods/${namespace}/${podName}/logs?container=${encodeURIComponent(container)}&follow=true&tail=${tailLines}`
    const ws = new WebSocket(url)

    ws.onopen = () => setConnected(true)
    ws.onclose = () => setConnected(false)
    ws.onerror = () => setConnected(false)
    ws.onmessage = (e) => {
      const text = e.data as string
      const level = detectLogLevel(text)
      // Try to extract timestamp from the beginning of the line
      const tsMatch = text.match(/^(\d{4}-\d{2}-\d{2}T[\d:.]+Z?)\s/)
      const logLine: LogLine = {
        id: lineIdRef.current++,
        text,
        level,
        timestamp: tsMatch ? tsMatch[1] : undefined,
      }
      setLines(prev => {
        const next = [...prev, logLine]
        if (next.length > 10000) return next.slice(-8000)
        return next
      })
    }

    wsRef.current = ws
  }, [namespace, podName, container, tailLines])

  useEffect(() => {
    connect()
    return () => { wsRef.current?.close() }
  }, [connect])

  useEffect(() => {
    if (following && bottomRef.current) {
      bottomRef.current.scrollIntoView({ behavior: 'auto' })
    }
  }, [lines, following])

  const handleScroll = useCallback(() => {
    if (!containerRef.current) return
    const { scrollTop, scrollHeight, clientHeight } = containerRef.current
    if (scrollHeight - scrollTop - clientHeight > 80) {
      setFollowing(false)
    }
  }, [])

  const scrollToBottom = () => {
    setFollowing(true)
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }

  const downloadLogs = () => {
    const text = filteredLines.map(l => l.text).join('\n')
    const blob = new Blob([text], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${podName}-${container}.log`
    a.click()
    URL.revokeObjectURL(url)
  }

  // Keyboard shortcut for search
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

  const searchMatchCount = searchTerm.trim() ? filteredLines.length : 0

  const highlightSearch = (text: string) => {
    if (!searchTerm.trim()) return text
    const parts = text.split(new RegExp(`(${searchTerm.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')})`, 'gi'))
    return parts.map((part, i) =>
      part.toLowerCase() === searchTerm.toLowerCase()
        ? <mark key={i} className="bg-yellow-500/40 text-yellow-200 rounded-sm px-0.5">{part}</mark>
        : part
    )
  }

  const levelCounts = useMemo(() => {
    const counts = { error: 0, warn: 0, info: 0, debug: 0, default: 0 }
    lines.forEach(l => { counts[l.level]++ })
    return counts
  }, [lines])

  return (
    <div className="flex flex-col h-[calc(100vh-280px)] min-h-[400px]">
      {/* Log toolbar */}
      <div className="flex flex-wrap items-center gap-2 mb-2 p-2 bg-gray-800/50 border border-gray-700/50 rounded-t-lg">
        {/* Container selector */}
        {containers.length > 1 && (
          <select
            value={container}
            onChange={e => { setContainer(e.target.value); setLines([]) }}
            className="bg-gray-800 border border-gray-700 rounded px-2 py-1.5 text-sm text-white"
            aria-label="Select container"
          >
            {containers.map(c => <option key={c} value={c}>{c}</option>)}
          </select>
        )}
        {containers.length === 1 && (
          <span className="text-xs text-gray-400 font-mono px-2 py-1.5 bg-gray-800 border border-gray-700 rounded">
            {containers[0]}
          </span>
        )}

        {/* Connection status */}
        <div className="flex items-center gap-1.5">
          <span className={clsx('w-2 h-2 rounded-full', connected ? 'bg-green-400 animate-pulse' : 'bg-red-400')} />
          <span className="text-xs text-gray-400">{connected ? 'Live' : 'Disconnected'}</span>
        </div>

        {/* Tail lines selector */}
        <select
          value={tailLines}
          onChange={e => setTailLines(Number(e.target.value))}
          className="bg-gray-800 border border-gray-700 rounded px-2 py-1.5 text-xs text-gray-300"
          aria-label="Tail lines"
        >
          <option value={100}>100 lines</option>
          <option value={200}>200 lines</option>
          <option value={500}>500 lines</option>
          <option value={1000}>1000 lines</option>
          <option value={5000}>5000 lines</option>
        </select>

        {/* Level filter pills */}
        <div className="flex items-center gap-1">
          <button
            onClick={() => setLevelFilter('all')}
            className={clsx(
              'px-2 py-1 text-xs rounded transition-colors',
              levelFilter === 'all' ? 'bg-blue-600 text-white' : 'bg-gray-800 text-gray-400 hover:text-white'
            )}
          >
            All
          </button>
          {levelCounts.error > 0 && (
            <button
              onClick={() => setLevelFilter(levelFilter === 'error' ? 'all' : 'error')}
              className={clsx(
                'px-2 py-1 text-xs rounded transition-colors',
                levelFilter === 'error' ? 'bg-red-700 text-white' : 'bg-gray-800 text-red-400 hover:bg-red-900/50'
              )}
            >
              ERR {levelCounts.error}
            </button>
          )}
          {levelCounts.warn > 0 && (
            <button
              onClick={() => setLevelFilter(levelFilter === 'warn' ? 'all' : 'warn')}
              className={clsx(
                'px-2 py-1 text-xs rounded transition-colors',
                levelFilter === 'warn' ? 'bg-yellow-700 text-white' : 'bg-gray-800 text-yellow-400 hover:bg-yellow-900/50'
              )}
            >
              WARN {levelCounts.warn}
            </button>
          )}
          {levelCounts.info > 0 && (
            <button
              onClick={() => setLevelFilter(levelFilter === 'info' ? 'all' : 'info')}
              className={clsx(
                'px-2 py-1 text-xs rounded transition-colors',
                levelFilter === 'info' ? 'bg-blue-700 text-white' : 'bg-gray-800 text-blue-400 hover:bg-blue-900/50'
              )}
            >
              INFO {levelCounts.info}
            </button>
          )}
          {levelCounts.debug > 0 && (
            <button
              onClick={() => setLevelFilter(levelFilter === 'debug' ? 'all' : 'debug')}
              className={clsx(
                'px-2 py-1 text-xs rounded transition-colors',
                levelFilter === 'debug' ? 'bg-gray-600 text-white' : 'bg-gray-800 text-gray-400 hover:bg-gray-700/50'
              )}
            >
              DEBUG {levelCounts.debug}
            </button>
          )}
        </div>

        <span className="flex-1" />

        {/* Line count */}
        <span className="text-xs text-gray-500">{filteredLines.length} lines</span>

        {/* Search toggle */}
        <button
          onClick={() => {
            setShowSearch(!showSearch)
            if (!showSearch) setTimeout(() => searchInputRef.current?.focus(), 50)
          }}
          className={clsx(
            'p-1.5 rounded transition-colors',
            showSearch ? 'bg-blue-600 text-white' : 'text-gray-400 hover:text-white hover:bg-white/10'
          )}
          title="Search (Ctrl+F)"
        >
          <Search className="w-3.5 h-3.5" />
        </button>

        {/* Wrap toggle */}
        <button
          onClick={() => setWrapLines(!wrapLines)}
          className={clsx(
            'p-1.5 text-xs rounded transition-colors',
            wrapLines ? 'bg-gray-700 text-white' : 'text-gray-400 hover:text-white hover:bg-white/10'
          )}
          title="Toggle word wrap"
        >
          Wrap
        </button>

        {/* Clear */}
        <button
          onClick={() => { setLines([]); lineIdRef.current = 0 }}
          className="p-1.5 text-gray-400 hover:text-white hover:bg-white/10 rounded"
          title="Clear logs"
        >
          <Trash2 className="w-3.5 h-3.5" />
        </button>

        {/* Download */}
        <button
          onClick={downloadLogs}
          className="p-1.5 text-gray-400 hover:text-white hover:bg-white/10 rounded"
          title="Download logs"
        >
          <Download className="w-3.5 h-3.5" />
        </button>

        {/* Reconnect */}
        {!connected && (
          <button
            onClick={connect}
            className="flex items-center gap-1 px-2 py-1 text-xs bg-blue-600 text-white rounded hover:bg-blue-500"
          >
            <RefreshCw className="w-3 h-3" /> Reconnect
          </button>
        )}

        {/* Follow toggle */}
        {!following && (
          <button
            onClick={scrollToBottom}
            className="flex items-center gap-1 px-2 py-1.5 text-xs bg-blue-600 text-white rounded hover:bg-blue-500"
          >
            <ArrowDown className="w-3 h-3" /> Follow
          </button>
        )}
        {following && connected && (
          <button
            onClick={() => setFollowing(false)}
            className="flex items-center gap-1 px-2 py-1.5 text-xs bg-green-700 text-white rounded hover:bg-green-600"
          >
            <Pause className="w-3 h-3" /> Following
          </button>
        )}
      </div>

      {/* Search bar */}
      {showSearch && (
        <div className="flex items-center gap-2 px-3 py-2 bg-gray-800 border-x border-gray-700/50">
          <Search className="w-4 h-4 text-gray-500 shrink-0" />
          <input
            ref={searchInputRef}
            type="text"
            value={searchTerm}
            onChange={e => setSearchTerm(e.target.value)}
            placeholder="Filter logs..."
            className="flex-1 bg-transparent text-sm text-white placeholder-gray-500 outline-none"
          />
          {searchTerm && (
            <span className="text-xs text-gray-400">{searchMatchCount} matches</span>
          )}
          <button
            onClick={() => { setShowSearch(false); setSearchTerm('') }}
            className="p-1 text-gray-400 hover:text-white"
          >
            <X className="w-3.5 h-3.5" />
          </button>
        </div>
      )}

      {/* Log output */}
      <div
        ref={containerRef}
        onScroll={handleScroll}
        className={clsx(
          'flex-1 bg-gray-950 border border-gray-700 rounded-b-lg font-mono text-xs leading-relaxed overflow-auto',
          showSearch ? 'border-t-0' : ''
        )}
      >
        {filteredLines.length === 0 && (
          <div className="flex items-center justify-center h-full text-gray-500 text-sm">
            {lines.length === 0
              ? (connected ? 'Waiting for log output...' : 'Not connected')
              : 'No lines match the current filter'}
          </div>
        )}
        {filteredLines.map((line) => (
          <div
            key={line.id}
            className={clsx(
              'px-3 py-0.5 hover:bg-white/5 border-l-2 transition-colors',
              line.level === 'error' ? 'border-l-red-500/60 bg-red-950/20' :
              line.level === 'warn' ? 'border-l-yellow-500/40' :
              'border-l-transparent',
              wrapLines ? 'whitespace-pre-wrap break-all' : 'whitespace-pre'
            )}
          >
            {line.timestamp && (
              <span className="text-gray-600 mr-2 select-none">{line.timestamp}</span>
            )}
            <span className={getLogLevelColor(line.level)}>
              {highlightSearch(line.timestamp ? line.text.replace(line.timestamp, '').trimStart() : line.text)}
            </span>
          </div>
        ))}
        <div ref={bottomRef} />
      </div>
    </div>
  )
}

/* ------------------------------------------------------------------ */
/*  Main Page Component                                                */
/* ------------------------------------------------------------------ */

export default function ResourceDetailPage() {
  const params = useParams()
  const searchParams = useSearchParams()
  const kind = params.kind as string
  const namespace = params.namespace as string
  const name = params.name as string
  const group = searchParams.get('group') || 'core'
  const version = searchParams.get('version') || 'v1'

  const [tab, setTab] = useState<Tab>('summary')
  const [resource, setResource] = useState<any>(null)
  const [yamlContent, setYamlContent] = useState<string>('')
  const [events, setEvents] = useState<K8sEvent[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const [capabilities, setCapabilities] = useState<{ exec: boolean; writeActions: boolean }>({ exec: false, writeActions: false })
  const [expandedContainers, setExpandedContainers] = useState<Set<string>>(new Set())
  const [expandedEnv, setExpandedEnv] = useState(false)
  const [expandedConditions, setExpandedConditions] = useState(true)
  const [expandedVolumes, setExpandedVolumes] = useState(false)
  const [expandedRawStatus, setExpandedRawStatus] = useState(false)
  const [childPods, setChildPods] = useState<any[]>([])

  const isPod = kind === 'pods'
  const isDeployment = ['deployments', 'statefulsets', 'replicasets', 'daemonsets'].includes(kind)
  const g = group === 'core' ? 'core' : group
  const isCluster = namespace === '_cluster'
  const basePath = isCluster
    ? `/api/v1/k8s/cluster/${g}/${version}/${kind}/${name}`
    : `/api/v1/k8s/ns/${namespace}/${g}/${version}/${kind}/${name}`

  useEffect(() => {
    setLoading(true)
    setError(null)
    apiFetch<any>(basePath)
      .then(data => {
        setResource(data)
        // Auto-expand the first container
        const firstContainer = data?.spec?.containers?.[0]?.name
        if (firstContainer) {
          setExpandedContainers(new Set([firstContainer]))
        }
      })
      .catch(e => setError(e.message))
      .finally(() => setLoading(false))
  }, [basePath])

  useEffect(() => {
    apiFetch<{ exec: boolean; writeActions: boolean }>('/api/v1/k8s/capabilities')
      .then(setCapabilities)
      .catch(() => {})
  }, [])

  // Fetch child pods for deployments/statefulsets
  useEffect(() => {
    if (isDeployment && resource) {
      apiFetch<any>(`${basePath}/pods`)
        .then(data => setChildPods(data.items || []))
        .catch(() => setChildPods([]))
    }
  }, [isDeployment, resource, basePath])

  // Container names for pod log/exec
  const containers: string[] = resource?.spec?.containers?.map((c: any) => c.name) || []
  const initContainers: string[] = resource?.spec?.initContainers?.map((c: any) => c.name) || []

  useEffect(() => {
    if (tab === 'yaml' && !yamlContent) {
      apiFetchText(`${basePath}/yaml`)
        .then(setYamlContent)
        .catch(e => setYamlContent(`# Error loading YAML: ${e.message}`))
    }
  }, [tab, basePath, yamlContent])

  useEffect(() => {
    if (tab === 'events') {
      apiFetch<any>(`${basePath}/events`)
        .then(data => setEvents(data.items || []))
        .catch(() => setEvents([]))
    }
  }, [tab, basePath])

  const copyYaml = async () => {
    await navigator.clipboard.writeText(yamlContent)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  const toggleContainer = (name: string) => {
    setExpandedContainers(prev => {
      const next = new Set(prev)
      if (next.has(name)) next.delete(name)
      else next.add(name)
      return next
    })
  }

  const backHref = `/workloads/${kind}?group=${group}&version=${version}`
  const displayKind = kind.charAt(0).toUpperCase() + kind.slice(1)

  // Derived data
  const meta = resource?.metadata || {}
  const status = resource?.status || {}
  const spec = resource?.spec || {}
  const labels: Record<string, string> = meta.labels || {}
  const annotations: Record<string, string> = meta.annotations || {}
  const containerSpecs: ContainerSpec[] = spec.containers || []
  const initContainerSpecs: ContainerSpec[] = spec.initContainers || []
  const containerStatuses: ContainerStatus[] = status.containerStatuses || []
  const initContainerStatuses: ContainerStatus[] = status.initContainerStatuses || []
  const conditions: PodCondition[] = status.conditions || []

  const getContainerStatus = (name: string): ContainerStatus | undefined =>
    [...containerStatuses, ...initContainerStatuses].find(cs => cs.name === name)

  const tabs: { id: Tab; label: string; icon: React.ReactNode }[] = [
    { id: 'summary', label: 'Summary', icon: <FileText className="w-4 h-4" /> },
    { id: 'yaml', label: 'YAML', icon: <Code className="w-4 h-4" /> },
    { id: 'events', label: 'Events', icon: <Calendar className="w-4 h-4" /> },
    ...(isPod ? [{ id: 'logs' as Tab, label: 'Logs', icon: <ScrollText className="w-4 h-4" /> }] : []),
    ...(isPod && capabilities.exec ? [{ id: 'exec' as Tab, label: 'Exec', icon: <Terminal className="w-4 h-4" /> }] : []),
  ]

  /* ---- Loading skeleton ---- */
  if (loading) {
    return (
      <div className="p-6 space-y-6">
        <div className="skeleton h-4 w-32" />
        <div className="flex items-center gap-3">
          <div className="skeleton h-8 w-64" />
          <div className="skeleton h-6 w-20 rounded" />
          <div className="skeleton h-6 w-24 rounded" />
        </div>
        <div className="flex gap-2 border-b border-gray-700 pb-2">
          {[1, 2, 3, 4].map(i => <div key={i} className="skeleton h-8 w-20 rounded" />)}
        </div>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div className="skeleton h-48 rounded-lg" />
          <div className="skeleton h-48 rounded-lg" />
        </div>
        <div className="skeleton h-32 rounded-lg" />
        <div className="skeleton h-32 rounded-lg" />
      </div>
    )
  }

  if (error) {
    return (
      <div className="p-6">
        <Link href={backHref} className="flex items-center gap-1 text-sm text-gray-400 hover:text-white mb-4">
          <ArrowLeft className="w-4 h-4" /> Back to {displayKind}
        </Link>
        <div className="p-4 bg-red-900/30 border border-red-700 rounded-md text-red-300 flex items-start gap-3">
          <AlertCircle className="w-5 h-5 shrink-0 mt-0.5" />
          <div>
            <p className="font-medium">Failed to load resource</p>
            <p className="text-sm mt-1">{error}</p>
          </div>
        </div>
      </div>
    )
  }

  const podPhase = status.phase
  const podIP = status.podIP
  const hostIP = status.hostIP
  const nodeName = spec.nodeName
  const serviceAccount = spec.serviceAccountName || spec.serviceAccount
  const qosClass = status.qosClass

  return (
    <div className="p-4 sm:p-6">
      {/* Breadcrumb / back link */}
      <Link href={backHref} className="inline-flex items-center gap-1 text-sm text-gray-400 hover:text-white mb-4 transition-colors">
        <ArrowLeft className="w-4 h-4" /> Back to {displayKind}
      </Link>

      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center gap-3 mb-6">
        <div className="flex items-center gap-3 min-w-0">
          <h1 className="text-xl sm:text-2xl font-bold text-white truncate">{name}</h1>
          <span className="px-2 py-0.5 text-xs bg-gray-700 text-gray-300 rounded shrink-0">
            {resource?.kind || kind}
          </span>
          {!isCluster && (
            <span className="px-2 py-0.5 text-xs bg-blue-900/40 text-blue-300 rounded shrink-0">
              {namespace}
            </span>
          )}
          {isPod && podPhase && (
            <span className={clsx('px-2.5 py-0.5 text-xs font-medium rounded border shrink-0', getPodPhaseColor(podPhase))}>
              {podPhase}
            </span>
          )}
        </div>
        <span className="flex-1" />
        {capabilities.writeActions && (isDeployment || isPod) && (
          <WorkloadActions
            kind={kind} group={group} version={version}
            namespace={namespace} name={name}
            currentReplicas={resource?.spec?.replicas}
          />
        )}
      </div>

      {/* Tabs */}
      <div className="flex gap-1 border-b border-gray-700 mb-6 overflow-x-auto" role="tablist">
        {tabs.map(t => (
          <button
            key={t.id}
            role="tab"
            aria-selected={tab === t.id}
            onClick={() => setTab(t.id)}
            className={clsx(
              'flex items-center gap-1.5 px-4 py-2.5 text-sm font-medium border-b-2 transition-colors whitespace-nowrap',
              tab === t.id
                ? 'border-blue-500 text-blue-400'
                : 'border-transparent text-gray-400 hover:text-white hover:border-gray-600'
            )}
          >
            {t.icon}
            {t.label}
            {t.id === 'events' && events.length > 0 && tab !== 'events' && (
              <span className="ml-1 px-1.5 py-0.5 text-[10px] bg-gray-700 text-gray-400 rounded-full">
                {events.length}
              </span>
            )}
          </button>
        ))}
      </div>

      {/* ================================================================ */}
      {/*  SUMMARY TAB                                                     */}
      {/* ================================================================ */}
      {tab === 'summary' && (
        <div className="space-y-6">
          {/* Pod Info Section — compact key-value grid */}
          {isPod && (
            <Section title="Pod Info" icon={<Info className="w-4 h-4" />}>
              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-x-8 gap-y-2">
                <KVRow icon={<Activity className="w-3.5 h-3.5 text-gray-500" />} label="Phase" value={podPhase} badge badgeColor={getPodPhaseColor(podPhase)} />
                <KVRow icon={<Network className="w-3.5 h-3.5 text-gray-500" />} label="Pod IP" value={podIP} mono />
                <KVRow icon={<Server className="w-3.5 h-3.5 text-gray-500" />} label="Host IP" value={hostIP} mono />
                <KVRow icon={<Globe className="w-3.5 h-3.5 text-gray-500" />} label="Node" value={nodeName} />
                <KVRow icon={<Shield className="w-3.5 h-3.5 text-gray-500" />} label="Service Account" value={serviceAccount} />
                <KVRow icon={<Tag className="w-3.5 h-3.5 text-gray-500" />} label="QoS Class" value={qosClass} />
                <KVRow icon={<Hash className="w-3.5 h-3.5 text-gray-500" />} label="UID" value={meta.uid} mono />
                <KVRow icon={<Clock className="w-3.5 h-3.5 text-gray-500" />} label="Created" value={meta.creationTimestamp ? `${new Date(meta.creationTimestamp).toLocaleString()} (${formatAge(meta.creationTimestamp)} ago)` : undefined} />
                {meta.deletionTimestamp && (
                  <KVRow icon={<XCircle className="w-3.5 h-3.5 text-red-500" />} label="Deleting" value={meta.deletionTimestamp} />
                )}
              </div>
            </Section>
          )}

          {/* Non-pod metadata */}
          {!isPod && (
            <Section title="Metadata" icon={<Info className="w-4 h-4" />}>
              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-x-8 gap-y-2">
                <KVRow label="Name" value={meta.name} />
                {meta.namespace && <KVRow label="Namespace" value={meta.namespace} />}
                <KVRow label="UID" value={meta.uid} mono />
                <KVRow label="Created" value={meta.creationTimestamp ? `${new Date(meta.creationTimestamp).toLocaleString()} (${formatAge(meta.creationTimestamp)} ago)` : undefined} />
                {meta.deletionTimestamp && <KVRow label="Deleting" value={meta.deletionTimestamp} />}
                {isDeployment && <KVRow label="Replicas" value={`${status.readyReplicas ?? 0}/${spec.replicas ?? 0} ready`} />}
                {isDeployment && <KVRow label="Strategy" value={spec.strategy?.type || spec.updateStrategy?.type} />}
              </div>
            </Section>
          )}

          {/* Containers section (for pods) */}
          {isPod && containerSpecs.length > 0 && (
            <div>
              <h3 className="text-sm font-medium text-gray-400 uppercase tracking-wider mb-3 flex items-center gap-2">
                <Container className="w-4 h-4" />
                Containers ({containerSpecs.length})
              </h3>
              <div className="space-y-3">
                {containerSpecs.map(cs => {
                  const cStatus = getContainerStatus(cs.name)
                  const stateInfo = getContainerStateLabel(cStatus)
                  const isExpanded = expandedContainers.has(cs.name)

                  return (
                    <div
                      key={cs.name}
                      className="bg-gray-800/30 border border-gray-700/50 rounded-lg overflow-hidden"
                    >
                      {/* Container header — always visible */}
                      <button
                        onClick={() => toggleContainer(cs.name)}
                        className="w-full flex items-center gap-3 p-4 hover:bg-white/5 transition-colors text-left"
                      >
                        {isExpanded
                          ? <ChevronDown className="w-4 h-4 text-gray-400 shrink-0" />
                          : <ChevronRight className="w-4 h-4 text-gray-400 shrink-0" />
                        }
                        <span className="font-medium text-white text-sm">{cs.name}</span>
                        <span className={clsx('px-2 py-0.5 text-xs font-medium rounded', stateInfo.color)}>
                          {stateInfo.label}
                        </span>
                        {cStatus && cStatus.restartCount > 0 && (
                          <span className="px-2 py-0.5 text-xs bg-orange-900/40 text-orange-300 rounded">
                            {cStatus.restartCount} restart{cStatus.restartCount !== 1 ? 's' : ''}
                          </span>
                        )}
                        <span className="flex-1" />
                        <span className="text-xs text-gray-500 font-mono truncate max-w-xs hidden sm:block">
                          {cs.image}
                        </span>
                      </button>

                      {/* Container details — expanded */}
                      {isExpanded && (
                        <div className="border-t border-gray-700/50 p-4 space-y-4">
                          {/* Image */}
                          <div className="text-xs">
                            <span className="text-gray-400">Image: </span>
                            <span className="text-gray-200 font-mono break-all">{cs.image}</span>
                          </div>

                          {/* Ports */}
                          {cs.ports && cs.ports.length > 0 && (
                            <div>
                              <span className="text-xs text-gray-400 block mb-1">Ports</span>
                              <div className="flex flex-wrap gap-2">
                                {cs.ports.map((p, i) => (
                                  <span key={i} className="px-2 py-0.5 text-xs bg-gray-700 text-gray-300 rounded font-mono">
                                    {p.name ? `${p.name}: ` : ''}{p.containerPort}/{p.protocol || 'TCP'}
                                  </span>
                                ))}
                              </div>
                            </div>
                          )}

                          {/* Command / Args */}
                          {(cs.command || cs.args) && (
                            <div>
                              <span className="text-xs text-gray-400 block mb-1">Command</span>
                              <code className="text-xs text-gray-300 font-mono bg-gray-800/50 px-2 py-1 rounded block break-all">
                                {cs.command?.join(' ')}{cs.args ? ' ' + cs.args.join(' ') : ''}
                              </code>
                            </div>
                          )}

                          {/* Resources */}
                          {cs.resources && (cs.resources.requests || cs.resources.limits) && (
                            <div className="space-y-2">
                              <span className="text-xs text-gray-400 block">Resources</span>
                              <ResourceBar
                                label="CPU"
                                request={cs.resources.requests?.cpu}
                                limit={cs.resources.limits?.cpu}
                              />
                              <ResourceBar
                                label="Memory"
                                request={cs.resources.requests?.memory}
                                limit={cs.resources.limits?.memory}
                              />
                            </div>
                          )}

                          {/* Probes */}
                          {(cs.livenessProbe || cs.readinessProbe || cs.startupProbe) && (
                            <div>
                              <span className="text-xs text-gray-400 block mb-1">Probes</span>
                              <div className="flex flex-wrap gap-2">
                                {cs.livenessProbe && (
                                  <span className="px-2 py-0.5 text-xs bg-green-900/30 text-green-300 rounded border border-green-700/30">
                                    Liveness
                                  </span>
                                )}
                                {cs.readinessProbe && (
                                  <span className="px-2 py-0.5 text-xs bg-blue-900/30 text-blue-300 rounded border border-blue-700/30">
                                    Readiness
                                  </span>
                                )}
                                {cs.startupProbe && (
                                  <span className="px-2 py-0.5 text-xs bg-purple-900/30 text-purple-300 rounded border border-purple-700/30">
                                    Startup
                                  </span>
                                )}
                              </div>
                            </div>
                          )}

                          {/* Volume mounts */}
                          {cs.volumeMounts && cs.volumeMounts.length > 0 && (
                            <div>
                              <span className="text-xs text-gray-400 block mb-1">
                                Volume Mounts ({cs.volumeMounts.length})
                              </span>
                              <div className="space-y-1">
                                {cs.volumeMounts.map((vm, i) => (
                                  <div key={i} className="flex items-center gap-2 text-xs">
                                    <span className="text-gray-300 font-mono">{vm.mountPath}</span>
                                    <span className="text-gray-500">from</span>
                                    <span className="text-gray-400 font-mono">{vm.name}</span>
                                    {vm.readOnly && (
                                      <span className="px-1 py-0.5 text-[10px] bg-gray-700 text-gray-400 rounded">RO</span>
                                    )}
                                  </div>
                                ))}
                              </div>
                            </div>
                          )}

                          {/* Last terminated state */}
                          {cStatus?.lastState?.terminated && (
                            <div className="p-3 bg-red-950/20 border border-red-700/30 rounded-lg">
                              <span className="text-xs text-red-400 block mb-1">Last Termination</span>
                              <div className="text-xs text-gray-300 space-y-0.5">
                                <div>Reason: {cStatus.lastState.terminated.reason || 'Unknown'}</div>
                                <div>Exit Code: {cStatus.lastState.terminated.exitCode}</div>
                                {cStatus.lastState.terminated.finishedAt && (
                                  <div>Finished: {new Date(cStatus.lastState.terminated.finishedAt).toLocaleString()}</div>
                                )}
                              </div>
                            </div>
                          )}
                        </div>
                      )}
                    </div>
                  )
                })}
              </div>
            </div>
          )}

          {/* Init Containers */}
          {isPod && initContainerSpecs.length > 0 && (
            <div>
              <h3 className="text-sm font-medium text-gray-400 uppercase tracking-wider mb-3 flex items-center gap-2">
                <Container className="w-4 h-4" />
                Init Containers ({initContainerSpecs.length})
              </h3>
              <div className="space-y-3">
                {initContainerSpecs.map(cs => {
                  const cStatus = getContainerStatus(cs.name)
                  const stateInfo = getContainerStateLabel(cStatus)
                  const isExpanded = expandedContainers.has(`init-${cs.name}`)

                  return (
                    <div key={cs.name} className="bg-gray-800/30 border border-gray-700/50 rounded-lg overflow-hidden">
                      <button
                        onClick={() => toggleContainer(`init-${cs.name}`)}
                        className="w-full flex items-center gap-3 p-3 hover:bg-white/5 transition-colors text-left"
                      >
                        {isExpanded
                          ? <ChevronDown className="w-4 h-4 text-gray-400 shrink-0" />
                          : <ChevronRight className="w-4 h-4 text-gray-400 shrink-0" />
                        }
                        <span className="text-xs text-gray-500 uppercase">init</span>
                        <span className="font-medium text-white text-sm">{cs.name}</span>
                        <span className={clsx('px-2 py-0.5 text-xs font-medium rounded', stateInfo.color)}>
                          {stateInfo.label}
                        </span>
                      </button>
                      {isExpanded && (
                        <div className="border-t border-gray-700/50 p-4 space-y-3">
                          <div className="text-xs">
                            <span className="text-gray-400">Image: </span>
                            <span className="text-gray-200 font-mono break-all">{cs.image}</span>
                          </div>
                          {(cs.command || cs.args) && (
                            <div>
                              <span className="text-xs text-gray-400 block mb-1">Command</span>
                              <code className="text-xs text-gray-300 font-mono bg-gray-800/50 px-2 py-1 rounded block break-all">
                                {cs.command?.join(' ')}{cs.args ? ' ' + cs.args.join(' ') : ''}
                              </code>
                            </div>
                          )}
                        </div>
                      )}
                    </div>
                  )
                })}
              </div>
            </div>
          )}

          {/* Environment Variables (collapsible) */}
          {isPod && containerSpecs.some(cs => cs.env && cs.env.length > 0) && (
            <CollapsibleSection
              title={`Environment Variables`}
              expanded={expandedEnv}
              onToggle={() => setExpandedEnv(!expandedEnv)}
            >
              <div className="space-y-4">
                {containerSpecs.filter(cs => cs.env && cs.env.length > 0).map(cs => (
                  <div key={cs.name}>
                    {containerSpecs.length > 1 && (
                      <p className="text-xs text-gray-400 mb-2 font-medium">{cs.name}</p>
                    )}
                    <div className="space-y-1">
                      {cs.env!.map((envVar, i) => (
                        <div key={i} className="flex gap-2 text-xs font-mono">
                          <span className="text-blue-400 shrink-0">{envVar.name}</span>
                          <span className="text-gray-600">=</span>
                          {envVar.value ? (
                            <span className="text-gray-300 break-all">{envVar.value}</span>
                          ) : envVar.valueFrom ? (
                            <span className="text-yellow-400 italic">
                              {envVar.valueFrom.secretKeyRef
                                ? `secret:${envVar.valueFrom.secretKeyRef.name}/${envVar.valueFrom.secretKeyRef.key}`
                                : envVar.valueFrom.configMapKeyRef
                                  ? `configmap:${envVar.valueFrom.configMapKeyRef.name}/${envVar.valueFrom.configMapKeyRef.key}`
                                  : envVar.valueFrom.fieldRef
                                    ? `fieldRef:${envVar.valueFrom.fieldRef.fieldPath}`
                                    : 'ref'
                              }
                            </span>
                          ) : (
                            <span className="text-gray-500">-</span>
                          )}
                        </div>
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            </CollapsibleSection>
          )}

          {/* Conditions table (for pods) */}
          {isPod && conditions.length > 0 && (
            <CollapsibleSection
              title={`Conditions (${conditions.length})`}
              expanded={expandedConditions}
              onToggle={() => setExpandedConditions(!expandedConditions)}
            >
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-gray-700/50">
                      <th className="text-left py-2 pr-4 text-xs text-gray-400 font-medium">Type</th>
                      <th className="text-left py-2 pr-4 text-xs text-gray-400 font-medium">Status</th>
                      <th className="text-left py-2 pr-4 text-xs text-gray-400 font-medium hidden sm:table-cell">Reason</th>
                      <th className="text-left py-2 pr-4 text-xs text-gray-400 font-medium hidden md:table-cell">Last Transition</th>
                      <th className="text-left py-2 text-xs text-gray-400 font-medium hidden lg:table-cell">Message</th>
                    </tr>
                  </thead>
                  <tbody>
                    {conditions.map((c, i) => (
                      <tr key={i} className="border-b border-gray-700/30 last:border-0">
                        <td className="py-2 pr-4 text-gray-200 text-xs font-medium">{c.type}</td>
                        <td className="py-2 pr-4">
                          <span className="flex items-center gap-1.5">
                            {getConditionIcon(c.status)}
                            <span className="text-xs text-gray-300">{c.status}</span>
                          </span>
                        </td>
                        <td className="py-2 pr-4 text-xs text-gray-400 hidden sm:table-cell">{c.reason || '-'}</td>
                        <td className="py-2 pr-4 text-xs text-gray-500 hidden md:table-cell">
                          {c.lastTransitionTime ? formatAge(c.lastTransitionTime) + ' ago' : '-'}
                        </td>
                        <td className="py-2 text-xs text-gray-500 hidden lg:table-cell max-w-xs truncate">
                          {c.message || '-'}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </CollapsibleSection>
          )}

          {/* Labels */}
          {Object.keys(labels).length > 0 && (
            <Section title="Labels" icon={<Tag className="w-4 h-4" />}>
              <div className="flex flex-wrap gap-1.5">
                {Object.entries(labels).map(([k, v]) => (
                  <span key={k} className="px-2 py-0.5 text-xs bg-gray-700 text-gray-300 rounded font-mono">
                    {k}=<span className="text-blue-300">{v}</span>
                  </span>
                ))}
              </div>
            </Section>
          )}

          {/* Annotations */}
          {Object.keys(annotations).length > 0 && (
            <CollapsibleSection
              title={`Annotations (${Object.keys(annotations).length})`}
              expanded={false}
              onToggle={() => {}}
              defaultOpen={false}
            >
              <div className="space-y-1 max-h-48 overflow-y-auto">
                {Object.entries(annotations).map(([k, v]) => (
                  <div key={k} className="text-xs font-mono">
                    <span className="text-gray-400">{k}: </span>
                    <span className="text-gray-300 break-all">{v}</span>
                  </div>
                ))}
              </div>
            </CollapsibleSection>
          )}

          {/* Volumes (for pods) */}
          {isPod && spec.volumes && spec.volumes.length > 0 && (
            <CollapsibleSection
              title={`Volumes (${spec.volumes.length})`}
              expanded={expandedVolumes}
              onToggle={() => setExpandedVolumes(!expandedVolumes)}
            >
              <div className="space-y-2">
                {spec.volumes.map((vol: any, i: number) => {
                  const type = Object.keys(vol).filter(k => k !== 'name')[0] || 'unknown'
                  return (
                    <div key={i} className="flex items-start gap-3 text-xs">
                      <span className="text-gray-200 font-mono shrink-0 w-40 truncate" title={vol.name}>{vol.name}</span>
                      <span className="px-1.5 py-0.5 bg-gray-700 text-gray-400 rounded text-[10px] uppercase shrink-0">{type}</span>
                      <span className="text-gray-500 font-mono break-all">
                        {typeof vol[type] === 'object' ? JSON.stringify(vol[type]) : vol[type]}
                      </span>
                    </div>
                  )
                })}
              </div>
            </CollapsibleSection>
          )}

          {/* Child Pods for Deployments/StatefulSets */}
          {isDeployment && childPods.length > 0 && (
            <Section title={`Pods (${childPods.length})`} icon={<Box className="w-4 h-4" />}>
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-gray-700/50">
                      <th className="text-left py-2 pr-4 text-xs text-gray-400 font-medium">Name</th>
                      <th className="text-left py-2 pr-4 text-xs text-gray-400 font-medium">Status</th>
                      <th className="text-left py-2 pr-4 text-xs text-gray-400 font-medium hidden sm:table-cell">Ready</th>
                      <th className="text-left py-2 pr-4 text-xs text-gray-400 font-medium hidden md:table-cell">Restarts</th>
                      <th className="text-left py-2 text-xs text-gray-400 font-medium hidden md:table-cell">Age</th>
                    </tr>
                  </thead>
                  <tbody>
                    {childPods.map((pod: any, i: number) => {
                      const podMeta = pod.metadata || {}
                      const podStatus = pod.status || {}
                      const phase = podStatus.phase
                      const ready = (podStatus.containerStatuses || []).filter((cs: any) => cs.ready).length
                      const total = (podStatus.containerStatuses || []).length
                      const restarts = (podStatus.containerStatuses || []).reduce((sum: number, cs: any) => sum + (cs.restartCount || 0), 0)
                      return (
                        <tr key={i} className="border-b border-gray-700/30 last:border-0 hover:bg-white/5">
                          <td className="py-2 pr-4">
                            <Link
                              href={`/workloads/pods/${podMeta.namespace}/${podMeta.name}?group=core&version=v1`}
                              className="text-blue-400 hover:text-blue-300 hover:underline text-xs font-mono"
                            >
                              {podMeta.name}
                            </Link>
                          </td>
                          <td className="py-2 pr-4">
                            <span className={clsx('px-2 py-0.5 text-xs rounded', getPodPhaseColor(phase))}>
                              {phase}
                            </span>
                          </td>
                          <td className="py-2 pr-4 text-xs text-gray-300 hidden sm:table-cell">{ready}/{total}</td>
                          <td className="py-2 pr-4 text-xs text-gray-400 hidden md:table-cell">{restarts}</td>
                          <td className="py-2 text-xs text-gray-500 hidden md:table-cell">{formatAge(podMeta.creationTimestamp)}</td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            </Section>
          )}

          {/* Raw Status / Spec (collapsible) */}
          {!isPod && Object.keys(status).length > 0 && (
            <CollapsibleSection
              title="Raw Status"
              expanded={expandedRawStatus}
              onToggle={() => setExpandedRawStatus(!expandedRawStatus)}
            >
              <pre className="text-xs font-mono text-gray-300 bg-gray-800/50 rounded p-3 overflow-x-auto max-h-96">
                {JSON.stringify(status, null, 2)}
              </pre>
            </CollapsibleSection>
          )}
        </div>
      )}

      {/* ================================================================ */}
      {/*  YAML TAB                                                        */}
      {/* ================================================================ */}
      {tab === 'yaml' && (
        <div className="relative">
          <button
            onClick={copyYaml}
            className="absolute top-3 right-3 flex items-center gap-1.5 px-3 py-1.5 text-xs bg-gray-700 hover:bg-gray-600 rounded text-gray-300 transition-colors z-10"
          >
            {copied ? <Check className="w-3 h-3 text-green-400" /> : <Copy className="w-3 h-3" />}
            {copied ? 'Copied' : 'Copy'}
          </button>
          <pre className="text-xs font-mono text-gray-300 bg-gray-800/50 border border-gray-700 rounded-lg p-4 overflow-auto max-h-[calc(100vh-260px)] whitespace-pre leading-relaxed">
            {yamlContent || (
              <div className="flex items-center gap-2 text-gray-500">
                <Loader2 className="w-4 h-4 animate-spin" /> Loading YAML...
              </div>
            )}
          </pre>
        </div>
      )}

      {/* ================================================================ */}
      {/*  EVENTS TAB                                                      */}
      {/* ================================================================ */}
      {tab === 'events' && (
        <div>
          {events.length === 0 ? (
            <div className="text-center py-12 text-gray-500">
              <Calendar className="w-8 h-8 mx-auto mb-3 opacity-50" />
              <p>No events found</p>
              <p className="text-xs mt-1 text-gray-600">Events are typically retained for 1 hour</p>
            </div>
          ) : (
            <div className="space-y-2">
              {events.map((ev, i) => (
                <div
                  key={i}
                  className={clsx(
                    'p-3 rounded-lg border transition-colors',
                    ev.type === 'Warning'
                      ? 'border-yellow-700/50 bg-yellow-900/10'
                      : 'border-gray-700/50 bg-gray-800/30'
                  )}
                >
                  <div className="flex items-center gap-2 mb-1 flex-wrap">
                    <span className={clsx(
                      'text-xs font-medium px-1.5 py-0.5 rounded',
                      ev.type === 'Warning' ? 'bg-yellow-800 text-yellow-300' : 'bg-gray-700 text-gray-300'
                    )}>
                      {ev.type}
                    </span>
                    <span className="text-xs font-medium text-gray-300">{ev.reason}</span>
                    {ev.count > 1 && (
                      <span className="text-xs text-gray-500">x{ev.count}</span>
                    )}
                    {ev.source?.component && (
                      <span className="text-xs text-gray-600 font-mono">{ev.source.component}</span>
                    )}
                    <span className="flex-1" />
                    <span className="text-xs text-gray-500">
                      {ev.lastTimestamp ? new Date(ev.lastTimestamp).toLocaleString() : '-'}
                    </span>
                  </div>
                  <p className="text-sm text-gray-400">{ev.message}</p>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* ================================================================ */}
      {/*  LOGS TAB (pods only)                                            */}
      {/* ================================================================ */}
      {tab === 'logs' && isPod && containers.length > 0 && (
        <EnhancedLogViewer
          namespace={namespace}
          podName={name}
          containers={containers}
          group={group}
          version={version}
          kind={kind}
        />
      )}
      {tab === 'logs' && isPod && containers.length === 0 && (
        <div className="text-center py-12 text-gray-500">
          <ScrollText className="w-8 h-8 mx-auto mb-3 opacity-50" />
          <p>No containers found for this pod</p>
        </div>
      )}

      {/* ================================================================ */}
      {/*  EXEC TAB (pods only, when enabled)                              */}
      {/* ================================================================ */}
      {tab === 'exec' && isPod && capabilities.exec && containers.length > 0 && (
        <PodExecTerminal namespace={namespace} podName={name} containers={containers} />
      )}
    </div>
  )
}

/* ------------------------------------------------------------------ */
/*  Sub-components                                                     */
/* ------------------------------------------------------------------ */

function Section({
  title,
  icon,
  children,
}: {
  title: string
  icon?: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <div>
      <h3 className="text-sm font-medium text-gray-400 uppercase tracking-wider mb-3 flex items-center gap-2">
        {icon}
        {title}
      </h3>
      <div className="bg-gray-800/30 border border-gray-700/50 rounded-lg p-4">
        {children}
      </div>
    </div>
  )
}

function CollapsibleSection({
  title,
  expanded: controlledExpanded,
  onToggle: controlledToggle,
  defaultOpen,
  children,
}: {
  title: string
  expanded?: boolean
  onToggle?: () => void
  defaultOpen?: boolean
  children: React.ReactNode
}) {
  const [internalExpanded, setInternalExpanded] = useState(defaultOpen ?? false)
  const isExpanded = controlledExpanded !== undefined ? controlledExpanded : internalExpanded
  const toggle = controlledToggle || (() => setInternalExpanded(!internalExpanded))

  return (
    <div className="bg-gray-800/30 border border-gray-700/50 rounded-lg overflow-hidden">
      <button
        onClick={toggle}
        className="w-full flex items-center gap-2 p-4 hover:bg-white/5 transition-colors text-left"
      >
        {isExpanded
          ? <ChevronDown className="w-4 h-4 text-gray-400 shrink-0" />
          : <ChevronRight className="w-4 h-4 text-gray-400 shrink-0" />
        }
        <h3 className="text-sm font-medium text-gray-400 uppercase tracking-wider">{title}</h3>
      </button>
      {isExpanded && (
        <div className="border-t border-gray-700/50 p-4">
          {children}
        </div>
      )}
    </div>
  )
}

function KVRow({
  icon,
  label,
  value,
  mono,
  badge,
  badgeColor,
}: {
  icon?: React.ReactNode
  label: string
  value?: string
  mono?: boolean
  badge?: boolean
  badgeColor?: string
}) {
  if (!value) return null
  return (
    <div className="flex items-center gap-2 py-1.5 text-sm">
      {icon}
      <span className="text-gray-400 w-28 shrink-0 text-xs">{label}</span>
      {badge ? (
        <span className={clsx('px-2 py-0.5 text-xs font-medium rounded border', badgeColor)}>
          {value}
        </span>
      ) : (
        <span className={clsx('text-gray-200 break-all text-xs', mono && 'font-mono')}>{value}</span>
      )}
    </div>
  )
}

'use client'

// DiagnosticPanel is what the dashboard renders IN PLACE OF the score
// cards when the analyzer has no LLM-derived scores. Its job is to tell
// the operator:
//
//   1. WHY scores aren't showing (awaiting, collector unreachable, LLM
//      unreachable, other error)
//   2. WHAT the reachability of each upstream dependency looks like
//   3. WHAT they can do about it (retry, switch to demo mode)
//
// The component takes a ReportStatus (shape mirrors the Go type in
// pkg/types) plus two callbacks. It does not own any state itself.

import {
  AlertTriangle,
  CheckCircle2,
  XCircle,
  Clock,
  RefreshCw,
  PlayCircle,
  Cloud,
  Cpu,
} from 'lucide-react'

export interface ComponentHealth {
  reachable: boolean
  endpoint?: string
  lastOkAt?: string
  lastError?: string
}

export interface ReportStatus {
  state: 'ok' | 'awaiting' | 'degraded' | 'error'
  message: string
  profile: 'live' | 'mock'
  collector: ComponentHealth
  llm: ComponentHealth
  lastAnalysisAt?: string
  lastAnalysisError?: string
}

interface DiagnosticPanelProps {
  status?: ReportStatus
  onRetry: () => void
  onSwitchToDemo: () => void
}

// formatRelative turns an ISO timestamp into something human-friendly
// ("2m ago", "just now"). Falls back to the raw string when parsing
// fails or input is missing.
function formatRelative(ts?: string): string {
  if (!ts) return 'never'
  const d = new Date(ts)
  if (isNaN(d.getTime())) return ts
  const diffMs = Date.now() - d.getTime()
  if (diffMs < 0) return 'just now'
  const sec = Math.floor(diffMs / 1000)
  if (sec < 60) return `${sec}s ago`
  const min = Math.floor(sec / 60)
  if (min < 60) return `${min}m ago`
  const hr = Math.floor(min / 60)
  if (hr < 24) return `${hr}h ago`
  const day = Math.floor(hr / 24)
  return `${day}d ago`
}

function CheckRow({
  label,
  reachable,
  endpoint,
  lastOkAt,
  lastError,
  icon: Icon,
}: ComponentHealth & { label: string; icon: typeof Cloud }) {
  const statusColor = reachable ? 'text-emerald-400' : 'text-red-400'
  const StatusIcon = reachable ? CheckCircle2 : XCircle
  return (
    <div className="flex items-start gap-3 p-3 rounded-lg bg-cluster-border/30 border border-cluster-border">
      <Icon className="w-5 h-5 text-slate-400 mt-0.5 flex-shrink-0" aria-hidden="true" />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="font-medium text-slate-200">{label}</span>
          <StatusIcon className={`w-4 h-4 ${statusColor}`} aria-hidden="true" />
          <span className={`text-xs uppercase tracking-wide ${statusColor}`}>
            {reachable ? 'reachable' : 'unreachable'}
          </span>
        </div>
        {endpoint && (
          <div className="text-xs text-slate-500 mt-0.5 font-mono truncate" title={endpoint}>
            {endpoint}
          </div>
        )}
        <div className="text-xs text-slate-400 mt-1">
          Last OK: <span className="text-slate-300">{formatRelative(lastOkAt)}</span>
        </div>
        {lastError && (
          <pre className="mt-2 text-xs text-red-300 bg-red-950/30 border border-red-500/20 rounded p-2 whitespace-pre-wrap break-words">
            {lastError}
          </pre>
        )}
      </div>
    </div>
  )
}

export function DiagnosticPanel({ status, onRetry, onSwitchToDemo }: DiagnosticPanelProps) {
  // Defensive defaults so the panel renders even if the API returns an
  // unexpectedly partial status block.
  const state = status?.state ?? 'awaiting'
  const message = status?.message ?? 'Awaiting first cluster analysis.'
  const collector = status?.collector ?? { reachable: false }
  const llm = status?.llm ?? { reachable: false }
  const lastAnalysisAt = status?.lastAnalysisAt
  const lastAnalysisError = status?.lastAnalysisError

  // Choose the headline icon and color based on the state.
  const iconByState = {
    ok: { Icon: CheckCircle2, color: 'text-emerald-400', bg: 'bg-emerald-500/10', border: 'border-emerald-500/30' },
    awaiting: { Icon: Clock, color: 'text-sky-400', bg: 'bg-sky-500/10', border: 'border-sky-500/30' },
    degraded: { Icon: AlertTriangle, color: 'text-amber-400', bg: 'bg-amber-500/10', border: 'border-amber-500/30' },
    error: { Icon: XCircle, color: 'text-red-400', bg: 'bg-red-500/10', border: 'border-red-500/30' },
  }[state]

  const { Icon, color, bg, border } = iconByState

  return (
    <section
      className={`rounded-xl border p-6 sm:p-8 mb-6 ${bg} ${border}`}
      aria-labelledby="diagnostic-heading"
    >
      <div className="flex items-start gap-4">
        <div className={`w-12 h-12 rounded-full flex items-center justify-center flex-shrink-0 ${bg} border ${border}`}>
          <Icon className={`w-6 h-6 ${color}`} aria-hidden="true" />
        </div>
        <div className="flex-1 min-w-0">
          <h2 id="diagnostic-heading" className={`text-xl font-semibold ${color} mb-1`}>
            {state === 'awaiting' && 'Awaiting first cluster analysis'}
            {state === 'degraded' && 'Analysis degraded'}
            {state === 'error' && 'Analysis error'}
            {state === 'ok' && 'All systems nominal'}
          </h2>
          <p className="text-slate-300">{message}</p>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-3 mt-5">
            <CheckRow
              label="Collector"
              reachable={collector.reachable}
              endpoint={collector.endpoint}
              lastOkAt={collector.lastOkAt}
              lastError={collector.lastError}
              icon={Cloud}
            />
            <CheckRow
              label="LLM Analyzer"
              reachable={llm.reachable}
              endpoint={llm.endpoint}
              lastOkAt={llm.lastOkAt}
              lastError={llm.lastError}
              icon={Cpu}
            />
          </div>

          <div className="mt-4 flex flex-wrap items-center gap-x-6 gap-y-1 text-sm text-slate-400">
            <span>
              Last analysis:{' '}
              <span className="text-slate-200">{formatRelative(lastAnalysisAt)}</span>
            </span>
            {lastAnalysisError && (
              <span className="text-red-300 font-mono text-xs break-all">
                {lastAnalysisError}
              </span>
            )}
          </div>

          <div className="mt-6 flex flex-wrap gap-3">
            <button
              onClick={onRetry}
              className="inline-flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg font-medium transition-colors"
            >
              <RefreshCw className="w-4 h-4" aria-hidden="true" />
              Retry now
            </button>
            <button
              onClick={onSwitchToDemo}
              className="inline-flex items-center gap-2 px-4 py-2 bg-amber-600/20 hover:bg-amber-600/30 text-amber-300 border border-amber-500/40 rounded-lg font-medium transition-colors"
            >
              <PlayCircle className="w-4 h-4" aria-hidden="true" />
              Switch to demo mode
            </button>
          </div>
          <p className="mt-3 text-xs text-slate-500">
            Demo mode shows synthetic data for presentations — no real cluster is analyzed.
          </p>
        </div>
      </div>
    </section>
  )
}

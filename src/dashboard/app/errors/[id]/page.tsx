'use client'

import { useEffect, useState, useCallback } from 'react'
import { useParams } from 'next/navigation'
import Link from 'next/link'
import { apiFetch } from '@/lib/api'
import {
  ArrowLeft, Loader2, Clock, Boxes, AlertCircle, Check, EyeOff, RotateCcw,
  Search, GitMerge, Sparkles, AlertTriangle, Info, ExternalLink, Filter,
} from 'lucide-react'

interface AnalysisEvidence {
  kind: string
  ref: string
  note?: string
}
interface ErrorAnalysis {
  rootCause: string
  impact?: string
  fix?: string
  severity: string
  confidence: number
  evidence?: AnalysisEvidence[]
  model?: string
  generatedAt: string
  trigger?: string
}

interface ErrorRate {
  count1m: number
  count5m: number
  count1h: number
  count24h: number
  spark: number[]
  truncated: boolean
}

interface MergeRef {
  id: number
  fingerprint: string
  service?: string
  mergedAt: string
  count: number
}

interface MergeSuggestion {
  targetId: number
  score: number
  suggestedAt: string
  reason: string
}

interface ErrorGroup {
  id: number
  fingerprint: string
  faultKey?: string
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
  sampleMessage: string
  sampleStack: string
  aiSummary: string
  rate?: ErrorRate
  analysis?: ErrorAnalysis
  mergedFrom?: MergeRef[]
  mergeSuggestion?: MergeSuggestion
}

interface Occurrence {
  timestamp: string
  pod: string
  container: string
  message: string
  url: string
  requestId: string
}

interface DetailResponse {
  group: ErrorGroup
  occurrences: Occurrence[]
  filteredCount: number
  totalCount: number
  occurrencesTruncated: boolean
  occurrenceCap: number
}

interface ContextResponse {
  groupId: number
  incidents?: any[]
  recommendations?: any[]
  siblings?: { id: number; service: string; namespace: string; title: string; count: number; lastSeen: string }[]
}

const SEVERITY_CHIP: Record<string, string> = {
  critical: 'bg-red-900/50 text-red-200 border-red-700/40',
  high: 'bg-orange-900/40 text-orange-200 border-orange-700/40',
  medium: 'bg-amber-900/30 text-amber-200 border-amber-700/40',
  low: 'bg-blue-900/30 text-blue-300 border-blue-700/40',
}

function ConfidenceBar({ value }: { value: number }) {
  const pct = Math.round(value * 100)
  const tone = pct >= 75 ? 'bg-green-500' : pct >= 50 ? 'bg-amber-500' : 'bg-red-500'
  return (
    <div className="inline-flex items-center gap-1.5">
      <div className="w-20 h-1.5 bg-gray-800 rounded">
        <div className={`h-1.5 rounded ${tone}`} style={{ width: `${pct}%` }} />
      </div>
      <span className="text-xs font-mono text-gray-400 tabular-nums">{pct}%</span>
    </div>
  )
}

export default function ErrorDetailPage() {
  const params = useParams()
  const id = params.id as string

  const [group, setGroup] = useState<ErrorGroup | null>(null)
  const [occurrences, setOccurrences] = useState<Occurrence[]>([])
  const [filteredCount, setFilteredCount] = useState(0)
  const [totalCount, setTotalCount] = useState(0)
  const [evicted, setEvicted] = useState<{ truncated: boolean; cap: number } | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [context, setContext] = useState<ContextResponse | null>(null)

  // Phase 1.4 — detail page filters
  const [pod, setPod] = useState('')
  const [search, setSearch] = useState('')
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')

  const fetchDetail = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const params = new URLSearchParams()
      if (pod) params.set('pod', pod)
      if (search) params.set('search', search)
      if (from) params.set('from', new Date(from).toISOString())
      if (to) params.set('to', new Date(to).toISOString())
      const data = await apiFetch<DetailResponse>(`/api/v1/errors/groups/${id}?${params}`)
      setGroup(data.group)
      setOccurrences(data.occurrences || [])
      setFilteredCount(data.filteredCount || 0)
      setTotalCount(data.totalCount || 0)
      setEvicted({ truncated: !!data.occurrencesTruncated, cap: data.occurrenceCap || 0 })
    } catch (e: any) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }, [id, pod, search, from, to])

  useEffect(() => { fetchDetail() }, [fetchDetail])

  // Phase 1.5 — context panel
  useEffect(() => {
    apiFetch<ContextResponse>(`/api/v1/errors/groups/${id}/context`)
      .then(setContext)
      .catch(() => setContext(null))
  }, [id])

  const updateStatus = async (status: string) => {
    await fetch(`/api/v1/errors/groups/${id}/status`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ status }),
    })
    setGroup(prev => prev ? { ...prev, status } : null)
  }

  // Phase 2.3 — accept the merge suggestion
  const acceptMergeSuggestion = async () => {
    if (!group?.mergeSuggestion) return
    const target = group.mergeSuggestion.targetId
    const ok = window.confirm(
      `Merge this group (#${group.id}) into #${target}?\n` +
      `Score: ${group.mergeSuggestion.score.toFixed(2)} (${group.mergeSuggestion.reason})`
    )
    if (!ok) return
    const res = await fetch(`/api/v1/errors/groups/${group.id}/merge-into/${target}`, { method: 'POST' })
    if (res.ok) {
      window.location.href = `/errors/${target}`
    } else {
      alert(`Merge failed: ${res.status}`)
    }
  }

  if (loading && !group) {
    return <div className="flex justify-center items-center min-h-[50vh]"><Loader2 className="w-6 h-6 animate-spin text-blue-400" /></div>
  }

  if (error || !group) {
    return (
      <div className="p-6">
        <Link href="/errors" className="flex items-center gap-1 text-sm text-cluster-muted hover:text-cluster-text mb-4">
          <ArrowLeft className="w-4 h-4" /> Back to Errors
        </Link>
        <div className="p-4 bg-red-900/30 border border-red-700 rounded text-red-300">{error || 'Not found'}</div>
      </div>
    )
  }

  return (
    <div className="p-6 max-w-[1400px] mx-auto">
      <Link href="/errors" className="flex items-center gap-1 text-sm text-cluster-muted hover:text-cluster-text mb-4">
        <ArrowLeft className="w-4 h-4" /> Back to Errors
      </Link>

      {/* Header */}
      <div className="flex items-start justify-between mb-6">
        <div>
          <h1 className="text-xl font-bold text-white mb-2">{group.title}</h1>
          <div className="flex items-center gap-3 text-sm text-gray-400 flex-wrap">
            <span className="px-2 py-0.5 bg-gray-700 rounded">{group.service}</span>
            <span>{group.namespace}</span>
            <span className="font-mono text-xs text-gray-500">{group.reason}</span>
            {group.exceptionType && <span className="font-mono text-xs text-orange-400">{group.exceptionType}</span>}
            {group.faultKey && (
              <Link href={`/errors/faults/${group.faultKey}`} className="font-mono text-xs text-purple-400 hover:underline">
                fault: {group.faultKey.slice(0, 8)}…
              </Link>
            )}
          </div>
        </div>
        <div className="flex items-center gap-2">
          {group.status === 'open' && (
            <>
              <button onClick={() => updateStatus('resolved')}
                className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-green-900/30 hover:bg-green-800/30 text-green-300 rounded">
                <Check className="w-3.5 h-3.5" /> Resolve
              </button>
              <button onClick={() => updateStatus('ignored')}
                className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-gray-700 hover:bg-gray-600 text-gray-300 rounded">
                <EyeOff className="w-3.5 h-3.5" /> Ignore
              </button>
            </>
          )}
          {group.status !== 'open' && (
            <button onClick={() => updateStatus('open')}
              className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-gray-700 hover:bg-gray-600 text-gray-300 rounded">
              <RotateCcw className="w-3.5 h-3.5" /> Reopen
            </button>
          )}
        </div>
      </div>

      {/* Phase 2.2 — near-duplicate suggestion */}
      {group.mergeSuggestion && (
        <div className="mb-6 p-3 bg-purple-900/20 border border-purple-700/40 rounded-lg flex items-center justify-between">
          <div className="flex items-center gap-2 text-sm text-purple-200">
            <GitMerge className="w-4 h-4 text-purple-400" />
            <span>
              Possible duplicate of <Link href={`/errors/${group.mergeSuggestion.targetId}`} className="font-mono underline">#{group.mergeSuggestion.targetId}</Link>{' '}
              <span className="text-purple-400 font-mono text-xs">(score {group.mergeSuggestion.score.toFixed(2)})</span>
            </span>
          </div>
          <button onClick={acceptMergeSuggestion}
            className="px-3 py-1.5 text-xs bg-purple-600/30 hover:bg-purple-600/50 text-purple-200 rounded">
            Merge into #{group.mergeSuggestion.targetId}
          </button>
        </div>
      )}

      {/* Phase 3.1 — typed analysis (preferred over markdown blob) */}
      {group.analysis ? (
        <div className="mb-6 p-4 bg-blue-900/10 border border-blue-700/30 rounded-lg">
          <div className="flex items-center justify-between mb-3">
            <h3 className="text-sm font-medium text-blue-300 flex items-center gap-1.5">
              <Sparkles className="w-4 h-4" /> AI Analysis
              {group.analysis.trigger && (
                <span className="ml-1 px-1.5 py-0.5 text-[10px] font-mono bg-gray-800 text-gray-400 rounded">
                  {group.analysis.trigger}
                </span>
              )}
            </h3>
            <div className="flex items-center gap-3">
              <span className={`px-2 py-0.5 text-xs font-mono uppercase border rounded ${SEVERITY_CHIP[group.analysis.severity] || 'bg-gray-700 text-gray-300'}`}>
                {group.analysis.severity}
              </span>
              <ConfidenceBar value={group.analysis.confidence} />
            </div>
          </div>
          <dl className="space-y-3 text-sm">
            <div>
              <dt className="text-xs uppercase text-gray-500 font-mono mb-1">Root cause</dt>
              <dd className="text-gray-200">{group.analysis.rootCause}</dd>
            </div>
            {group.analysis.impact && (
              <div>
                <dt className="text-xs uppercase text-gray-500 font-mono mb-1">Impact</dt>
                <dd className="text-gray-200">{group.analysis.impact}</dd>
              </div>
            )}
            {group.analysis.fix && (
              <div>
                <dt className="text-xs uppercase text-gray-500 font-mono mb-1">Fix</dt>
                <dd className="text-gray-200">{group.analysis.fix}</dd>
              </div>
            )}
            {group.analysis.evidence && group.analysis.evidence.length > 0 && (
              <div>
                <dt className="text-xs uppercase text-gray-500 font-mono mb-1">Evidence</dt>
                <dd className="flex flex-wrap gap-2">
                  {group.analysis.evidence.map((e, i) => {
                    const href = e.kind === 'incident' ? `/incidents/${e.ref}` : null
                    const inner = (
                      <span className="inline-flex items-center gap-1 px-2 py-0.5 text-xs bg-gray-800 border border-gray-700 rounded">
                        <span className="font-mono text-gray-500">{e.kind}</span>
                        <span className="text-gray-300">{e.ref}</span>
                        {e.note && <span className="text-gray-500 italic">· {e.note}</span>}
                      </span>
                    )
                    return href
                      ? <Link key={i} href={href} className="hover:underline">{inner}</Link>
                      : <span key={i}>{inner}</span>
                  })}
                </dd>
              </div>
            )}
          </dl>
          {group.analysis.model && (
            <div className="mt-3 pt-2 border-t border-gray-700/50 text-xs text-gray-500 font-mono">
              {group.analysis.model} · {new Date(group.analysis.generatedAt).toLocaleString()}
            </div>
          )}
        </div>
      ) : group.aiSummary && (
        <div className="mb-6 p-4 bg-blue-900/10 border border-blue-700/30 rounded-lg">
          <h3 className="text-sm font-medium text-blue-400 mb-1">AI Summary <span className="text-[10px] font-mono text-gray-500 ml-1">(legacy)</span></h3>
          <div className="text-sm text-gray-300 whitespace-pre-wrap">{group.aiSummary}</div>
        </div>
      )}

      {/* Phase 1.5 — context panel */}
      {context && ((context.incidents?.length || 0) + (context.recommendations?.length || 0) + (context.siblings?.length || 0) > 0) && (
        <div className="mb-6 p-4 bg-gray-800/30 border border-gray-700/40 rounded-lg">
          <h3 className="text-sm font-medium text-gray-300 mb-3 flex items-center gap-1.5">
            <Info className="w-4 h-4 text-cyan-400" /> Context
          </h3>
          <div className="grid grid-cols-3 gap-4 text-sm">
            <ContextSection title="Open incidents" items={(context.incidents || []).map((i: any) => ({
              href: `/incidents/${i.id}`,
              label: `#${i.id} · ${i.severity}`,
              note: i.summary,
            }))} />
            <ContextSection title="Recommendations" items={(context.recommendations || []).map((r: any) => ({
              href: '/recommendations',
              label: `${r.type} · ${r.severity}`,
              note: r.rationale,
            }))} />
            <ContextSection title="Siblings (same fault)" items={(context.siblings || []).map(s => ({
              href: `/errors/${s.id}`,
              label: s.service,
              note: `${s.count} events`,
            }))} />
          </div>
        </div>
      )}

      {/* Stats */}
      <div className="grid grid-cols-4 gap-4 mb-6">
        <Stat icon={<AlertCircle className="w-4 h-4 text-red-400" />} label="Total occurrences" value={group.count.toLocaleString()} />
        <Stat icon={<Clock className="w-4 h-4 text-gray-400" />} label="First seen" value={new Date(group.firstSeen).toLocaleString()} />
        <Stat icon={<Clock className="w-4 h-4 text-blue-400" />} label="Last seen" value={new Date(group.lastSeen).toLocaleString()} />
        <Stat icon={<Boxes className="w-4 h-4 text-purple-400" />} label="Last pod" value={group.lastPod || '-'} />
      </div>

      {/* Sample stack trace */}
      {group.sampleStack && (
        <div className="mb-6">
          <h3 className="text-sm font-medium text-gray-400 uppercase tracking-wider mb-2">Stack Trace</h3>
          <pre className="text-xs font-mono text-gray-300 bg-gray-900 border border-gray-700 rounded-lg p-4 overflow-auto max-h-80 whitespace-pre-wrap">
            {group.sampleStack}
          </pre>
        </div>
      )}

      {/* Sample message */}
      {group.sampleMessage && !group.sampleStack && (
        <div className="mb-6">
          <h3 className="text-sm font-medium text-gray-400 uppercase tracking-wider mb-2">Sample Message</h3>
          <pre className="text-xs font-mono text-gray-300 bg-gray-900 border border-gray-700 rounded-lg p-4 overflow-auto max-h-40 whitespace-pre-wrap">
            {group.sampleMessage}
          </pre>
        </div>
      )}

      {/* Phase 1.4 — occurrence filters */}
      <div className="flex items-center justify-between mb-2">
        <h3 className="text-sm font-medium text-gray-400 uppercase tracking-wider">
          Recent Occurrences
          <span className="ml-2 normal-case text-gray-500 text-xs font-normal">
            ({filteredCount === totalCount ? totalCount : `${filteredCount} of ${totalCount}`})
          </span>
        </h3>
        {evicted?.truncated && (
          <span className="inline-flex items-center gap-1 text-[11px] font-mono text-amber-300 bg-amber-900/20 border border-amber-700/30 px-2 py-0.5 rounded"
            title={`The ring buffer is full at ${evicted.cap}. Older events have been dropped — total count is the source of truth.`}>
            <AlertTriangle className="w-3 h-3" /> ≥ {evicted.cap} — older events evicted
          </span>
        )}
      </div>
      <div className="grid grid-cols-4 gap-2 mb-3 text-xs">
        <input
          placeholder="filter pod"
          value={pod}
          onChange={e => setPod(e.target.value)}
          className="px-2 py-1.5 bg-gray-800 border border-gray-700 rounded text-gray-200"
        />
        <input
          placeholder="search message"
          value={search}
          onChange={e => setSearch(e.target.value)}
          className="px-2 py-1.5 bg-gray-800 border border-gray-700 rounded text-gray-200"
        />
        <input
          type="datetime-local"
          placeholder="from"
          value={from}
          onChange={e => setFrom(e.target.value)}
          className="px-2 py-1.5 bg-gray-800 border border-gray-700 rounded text-gray-300"
        />
        <input
          type="datetime-local"
          placeholder="to"
          value={to}
          onChange={e => setTo(e.target.value)}
          className="px-2 py-1.5 bg-gray-800 border border-gray-700 rounded text-gray-300"
        />
      </div>

      {occurrences.length === 0 ? (
        <div className="text-center py-8 text-gray-500">
          {filteredCount === 0 && totalCount > 0 ? 'No occurrences match the current filter.' : 'No occurrences stored.'}
        </div>
      ) : (
        <div className="space-y-1">
          {occurrences.map((occ, i) => (
            <div key={i} className="flex items-start gap-3 p-3 bg-gray-800/30 border border-gray-700/30 rounded-lg text-sm">
              <span className="text-xs text-gray-500 shrink-0 w-36 tabular-nums">
                {new Date(occ.timestamp).toLocaleString()}
              </span>
              <span className="text-xs text-gray-400 shrink-0 font-mono">{occ.pod}</span>
              <span className="text-gray-300 flex-1 truncate">{occ.message}</span>
              {occ.url && <span className="text-xs text-gray-500 font-mono shrink-0">{occ.url}</span>}
            </div>
          ))}
        </div>
      )}

      {/* Phase 2.3 — merged history */}
      {group.mergedFrom && group.mergedFrom.length > 0 && (
        <div className="mt-6 p-3 bg-gray-800/30 border border-gray-700/40 rounded-lg">
          <h3 className="text-sm font-medium text-gray-300 mb-2">Merged history</h3>
          <ul className="space-y-1 text-xs font-mono text-gray-400">
            {group.mergedFrom.map(m => (
              <li key={m.id}>
                #{m.id} ({m.fingerprint.slice(0, 8)}…) · {m.service} · +{m.count} events ·{' '}
                {new Date(m.mergedAt).toLocaleString()}
              </li>
            ))}
          </ul>
        </div>
      )}

      {/* Link to workload */}
      {group.lastPod && (
        <div className="mt-6 pt-4 border-t border-gray-700">
          <Link href={`/workloads/pods/${group.namespace}/${group.lastPod}?group=core&version=v1`}
            className="text-sm text-blue-400 hover:text-blue-300 hover:underline inline-flex items-center gap-1">
            View pod {group.lastPod} in Workloads <ExternalLink className="w-3 h-3" />
          </Link>
        </div>
      )}
    </div>
  )
}

function Stat({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }) {
  return (
    <div className="p-3 bg-gray-800/50 border border-gray-700/50 rounded-lg">
      <div className="flex items-center gap-1.5 text-xs text-gray-400 mb-1">{icon}{label}</div>
      <div className="text-sm font-medium text-white">{value}</div>
    </div>
  )
}

function ContextSection({ title, items }: {
  title: string
  items: { href: string; label: string; note?: string }[]
}) {
  return (
    <div>
      <div className="text-xs uppercase text-gray-500 font-mono mb-1">{title} ({items.length})</div>
      {items.length === 0 ? (
        <div className="text-xs text-gray-600 italic">none</div>
      ) : (
        <ul className="space-y-1">
          {items.slice(0, 5).map((it, i) => (
            <li key={i} className="text-xs">
              <Link href={it.href} className="text-cyan-300 hover:underline">{it.label}</Link>
              {it.note && <div className="text-gray-500 truncate" title={it.note}>{it.note}</div>}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

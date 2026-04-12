'use client'

import { useEffect, useState } from 'react'
import { useParams } from 'next/navigation'
import Link from 'next/link'
import { apiFetch } from '@/lib/api'
import { ArrowLeft, Loader2, Clock, Boxes, AlertCircle, Check, EyeOff, RotateCcw } from 'lucide-react'

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
  sampleMessage: string
  sampleStack: string
  aiSummary: string
}

interface Occurrence {
  timestamp: string
  pod: string
  container: string
  message: string
  url: string
  requestId: string
}

export default function ErrorDetailPage() {
  const params = useParams()
  const id = params.id as string

  const [group, setGroup] = useState<ErrorGroup | null>(null)
  const [occurrences, setOccurrences] = useState<Occurrence[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setLoading(true)
    apiFetch<{ group: ErrorGroup; occurrences: Occurrence[] }>(`/api/v1/errors/groups/${id}`)
      .then(data => {
        setGroup(data.group)
        setOccurrences(data.occurrences || [])
      })
      .catch(e => setError(e.message))
      .finally(() => setLoading(false))
  }, [id])

  const updateStatus = async (status: string) => {
    const base = typeof window !== 'undefined' ? (window as any).__CLUSTER_INTEL_API__ || '' : ''
    await fetch(`${base}/api/v1/errors/groups/${id}/status`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ status }),
    })
    setGroup(prev => prev ? { ...prev, status } : null)
  }

  if (loading) {
    return <div className="flex justify-center items-center min-h-[50vh]"><Loader2 className="w-6 h-6 animate-spin text-blue-400" /></div>
  }

  if (error || !group) {
    return (
      <div className="p-6">
        <Link href="/errors" className="flex items-center gap-1 text-sm text-gray-400 hover:text-white mb-4">
          <ArrowLeft className="w-4 h-4" /> Back to Errors
        </Link>
        <div className="p-4 bg-red-900/30 border border-red-700 rounded text-red-300">{error || 'Not found'}</div>
      </div>
    )
  }

  return (
    <div className="p-6">
      <Link href="/errors" className="flex items-center gap-1 text-sm text-gray-400 hover:text-white mb-4">
        <ArrowLeft className="w-4 h-4" /> Back to Errors
      </Link>

      {/* Header */}
      <div className="flex items-start justify-between mb-6">
        <div>
          <h1 className="text-xl font-bold text-white mb-2">{group.title}</h1>
          <div className="flex items-center gap-3 text-sm text-gray-400">
            <span className="px-2 py-0.5 bg-gray-700 rounded">{group.service}</span>
            <span>{group.namespace}</span>
            <span className="font-mono text-xs text-gray-500">{group.reason}</span>
            {group.exceptionType && <span className="font-mono text-xs text-orange-400">{group.exceptionType}</span>}
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

      {/* Stats */}
      <div className="grid grid-cols-4 gap-4 mb-6">
        <Stat icon={<AlertCircle className="w-4 h-4 text-red-400" />} label="Total occurrences" value={group.count.toLocaleString()} />
        <Stat icon={<Clock className="w-4 h-4 text-gray-400" />} label="First seen" value={new Date(group.firstSeen).toLocaleString()} />
        <Stat icon={<Clock className="w-4 h-4 text-blue-400" />} label="Last seen" value={new Date(group.lastSeen).toLocaleString()} />
        <Stat icon={<Boxes className="w-4 h-4 text-purple-400" />} label="Last pod" value={group.lastPod || '-'} />
      </div>

      {/* AI Summary placeholder */}
      {group.aiSummary && (
        <div className="mb-6 p-4 bg-blue-900/10 border border-blue-700/30 rounded-lg">
          <h3 className="text-sm font-medium text-blue-400 mb-1">AI Summary</h3>
          <p className="text-sm text-gray-300">{group.aiSummary}</p>
        </div>
      )}

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

      {/* Occurrences */}
      <div>
        <h3 className="text-sm font-medium text-gray-400 uppercase tracking-wider mb-2">
          Recent Occurrences ({occurrences.length})
        </h3>
        {occurrences.length === 0 ? (
          <div className="text-center py-8 text-gray-500">No occurrences stored</div>
        ) : (
          <div className="space-y-1">
            {occurrences.map((occ, i) => (
              <div key={i} className="flex items-start gap-3 p-3 bg-gray-800/30 border border-gray-700/30 rounded-lg text-sm">
                <span className="text-xs text-gray-500 shrink-0 w-36">
                  {new Date(occ.timestamp).toLocaleString()}
                </span>
                <span className="text-xs text-gray-400 shrink-0 font-mono">{occ.pod}</span>
                <span className="text-gray-300 flex-1 truncate">{occ.message}</span>
                {occ.url && <span className="text-xs text-gray-500 font-mono shrink-0">{occ.url}</span>}
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Link to workload */}
      {group.lastPod && (
        <div className="mt-6 pt-4 border-t border-gray-700">
          <Link href={`/workloads/pods/${group.namespace}/${group.lastPod}?group=core&version=v1`}
            className="text-sm text-blue-400 hover:text-blue-300 hover:underline">
            View pod {group.lastPod} in Workloads
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

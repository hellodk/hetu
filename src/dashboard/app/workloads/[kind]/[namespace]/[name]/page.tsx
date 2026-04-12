'use client'

import { useEffect, useState } from 'react'
import { useParams, useSearchParams } from 'next/navigation'
import Link from 'next/link'
import { apiFetch, apiFetchText } from '@/lib/api'
import { PodLogViewer } from '@/components/PodLogViewer'
import { PodExecTerminal } from '@/components/PodExecTerminal'
import { WorkloadActions } from '@/components/WorkloadActions'
import { ArrowLeft, FileText, Code, Calendar, Loader2, Copy, Check, Terminal, ScrollText } from 'lucide-react'

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
      .then(setResource)
      .catch(e => setError(e.message))
      .finally(() => setLoading(false))
  }, [basePath])

  useEffect(() => {
    apiFetch<{ exec: boolean; writeActions: boolean }>('/api/v1/k8s/capabilities')
      .then(setCapabilities)
      .catch(() => {})
  }, [])

  // Extract container names for pod log/exec
  const containers: string[] = resource?.spec?.containers?.map((c: any) => c.name) || []

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

  const backHref = `/workloads/${kind}?group=${group}&version=${version}`
  const displayKind = kind.charAt(0).toUpperCase() + kind.slice(1)

  if (loading) {
    return (
      <div className="flex items-center justify-center min-h-[50vh]">
        <Loader2 className="w-6 h-6 animate-spin text-blue-400" />
      </div>
    )
  }

  if (error) {
    return (
      <div className="p-6">
        <Link href={backHref} className="flex items-center gap-1 text-sm text-gray-400 hover:text-white mb-4">
          <ArrowLeft className="w-4 h-4" /> Back to {displayKind}
        </Link>
        <div className="p-4 bg-red-900/30 border border-red-700 rounded-md text-red-300">{error}</div>
      </div>
    )
  }

  const meta = resource?.metadata || {}
  const status = resource?.status || {}
  const spec = resource?.spec || {}
  const labels = meta.labels || {}
  const annotations = meta.annotations || {}

  const tabs: { id: Tab; label: string; icon: React.ReactNode }[] = [
    { id: 'summary', label: 'Summary', icon: <FileText className="w-4 h-4" /> },
    { id: 'yaml', label: 'YAML', icon: <Code className="w-4 h-4" /> },
    { id: 'events', label: 'Events', icon: <Calendar className="w-4 h-4" /> },
    ...(isPod ? [{ id: 'logs' as Tab, label: 'Logs', icon: <ScrollText className="w-4 h-4" /> }] : []),
    ...(isPod && capabilities.exec ? [{ id: 'exec' as Tab, label: 'Exec', icon: <Terminal className="w-4 h-4" /> }] : []),
  ]

  return (
    <div className="p-6">
      {/* Header */}
      <Link href={backHref} className="flex items-center gap-1 text-sm text-gray-400 hover:text-white mb-4">
        <ArrowLeft className="w-4 h-4" /> Back to {displayKind}
      </Link>

      <div className="flex items-center gap-3 mb-6">
        <h1 className="text-2xl font-bold text-white">{name}</h1>
        <span className="px-2 py-0.5 text-xs bg-gray-700 text-gray-300 rounded">{resource?.kind || kind}</span>
        {!isCluster && (
          <span className="px-2 py-0.5 text-xs bg-blue-900/40 text-blue-300 rounded">{namespace}</span>
        )}
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
      <div className="flex gap-1 border-b border-gray-700 mb-6">
        {tabs.map(t => (
          <button
            key={t.id}
            onClick={() => setTab(t.id)}
            className={`flex items-center gap-1.5 px-4 py-2 text-sm font-medium border-b-2 transition-colors ${
              tab === t.id
                ? 'border-blue-500 text-blue-400'
                : 'border-transparent text-gray-400 hover:text-white'
            }`}
          >
            {t.icon}
            {t.label}
          </button>
        ))}
      </div>

      {/* Summary tab */}
      {tab === 'summary' && (
        <div className="space-y-6">
          {/* Metadata */}
          <Section title="Metadata">
            <KV label="Name" value={meta.name} />
            {meta.namespace && <KV label="Namespace" value={meta.namespace} />}
            <KV label="UID" value={meta.uid} mono />
            <KV label="Created" value={meta.creationTimestamp} />
            {meta.deletionTimestamp && <KV label="Deleting" value={meta.deletionTimestamp} />}
          </Section>

          {/* Labels */}
          {Object.keys(labels).length > 0 && (
            <Section title="Labels">
              <div className="flex flex-wrap gap-1.5">
                {Object.entries(labels).map(([k, v]) => (
                  <span key={k} className="px-2 py-0.5 text-xs bg-gray-700 text-gray-300 rounded font-mono">
                    {k}={v as string}
                  </span>
                ))}
              </div>
            </Section>
          )}

          {/* Annotations */}
          {Object.keys(annotations).length > 0 && (
            <Section title={`Annotations (${Object.keys(annotations).length})`}>
              <div className="space-y-1 max-h-48 overflow-y-auto">
                {Object.entries(annotations).map(([k, v]) => (
                  <div key={k} className="text-xs font-mono">
                    <span className="text-gray-400">{k}: </span>
                    <span className="text-gray-300 break-all">{v as string}</span>
                  </div>
                ))}
              </div>
            </Section>
          )}

          {/* Status */}
          {Object.keys(status).length > 0 && (
            <Section title="Status">
              <pre className="text-xs font-mono text-gray-300 bg-gray-800/50 rounded p-3 overflow-x-auto max-h-96">
                {JSON.stringify(status, null, 2)}
              </pre>
            </Section>
          )}

          {/* Spec summary */}
          {Object.keys(spec).length > 0 && (
            <Section title="Spec">
              <pre className="text-xs font-mono text-gray-300 bg-gray-800/50 rounded p-3 overflow-x-auto max-h-96">
                {JSON.stringify(spec, null, 2)}
              </pre>
            </Section>
          )}
        </div>
      )}

      {/* YAML tab */}
      {tab === 'yaml' && (
        <div className="relative">
          <button
            onClick={copyYaml}
            className="absolute top-3 right-3 flex items-center gap-1.5 px-2 py-1 text-xs bg-gray-700 hover:bg-gray-600 rounded text-gray-300 transition-colors z-10"
          >
            {copied ? <Check className="w-3 h-3 text-green-400" /> : <Copy className="w-3 h-3" />}
            {copied ? 'Copied' : 'Copy'}
          </button>
          <pre className="text-xs font-mono text-gray-300 bg-gray-800/50 border border-gray-700 rounded-lg p-4 overflow-auto max-h-[70vh] whitespace-pre">
            {yamlContent || 'Loading...'}
          </pre>
        </div>
      )}

      {/* Events tab */}
      {tab === 'events' && (
        <div>
          {events.length === 0 ? (
            <div className="text-center py-8 text-gray-500">No events found</div>
          ) : (
            <div className="space-y-2">
              {events.map((ev, i) => (
                <div
                  key={i}
                  className={`p-3 rounded-lg border ${
                    ev.type === 'Warning'
                      ? 'border-yellow-700/50 bg-yellow-900/10'
                      : 'border-gray-700/50 bg-gray-800/30'
                  }`}
                >
                  <div className="flex items-center gap-2 mb-1">
                    <span className={`text-xs font-medium px-1.5 py-0.5 rounded ${
                      ev.type === 'Warning' ? 'bg-yellow-800 text-yellow-300' : 'bg-gray-700 text-gray-300'
                    }`}>
                      {ev.type}
                    </span>
                    <span className="text-xs font-medium text-gray-300">{ev.reason}</span>
                    {ev.count > 1 && (
                      <span className="text-xs text-gray-500">x{ev.count}</span>
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

      {/* Logs tab (pods only) */}
      {tab === 'logs' && isPod && containers.length > 0 && (
        <PodLogViewer namespace={namespace} podName={name} containers={containers} />
      )}

      {/* Exec tab (pods only, when enabled) */}
      {tab === 'exec' && isPod && capabilities.exec && containers.length > 0 && (
        <PodExecTerminal namespace={namespace} podName={name} containers={containers} />
      )}
    </div>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div>
      <h3 className="text-sm font-medium text-gray-400 uppercase tracking-wider mb-2">{title}</h3>
      <div className="bg-gray-800/30 border border-gray-700/50 rounded-lg p-4">{children}</div>
    </div>
  )
}

function KV({ label, value, mono }: { label: string; value?: string; mono?: boolean }) {
  if (!value) return null
  return (
    <div className="flex gap-4 py-1 text-sm">
      <span className="text-gray-400 w-32 shrink-0">{label}</span>
      <span className={`text-gray-200 break-all ${mono ? 'font-mono text-xs' : ''}`}>{value}</span>
    </div>
  )
}

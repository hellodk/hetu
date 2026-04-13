'use client'

import { useState, useEffect, useCallback } from 'react'
import Link from 'next/link'
import { apiFetch } from '@/lib/api'
import {
  ChevronDown, ChevronUp, Activity, Shield, DollarSign, Boxes,
  AlertTriangle, CheckCircle, TrendingDown, Loader2, ExternalLink
} from 'lucide-react'
import clsx from 'clsx'

interface Factor {
  name: string
  impact: number
  resources?: string[]
  severity?: string
}

interface Dimension {
  score: number
  factors: Factor[]
}

interface Breakdown {
  reliability: Dimension
  security: Dimension
  cost: Dimension
  architecture: Dimension
}

const dimensionConfig = {
  reliability: { label: 'Reliability', icon: Activity, color: 'blue' },
  security: { label: 'Security', icon: Shield, color: 'purple' },
  cost: { label: 'Cost Efficiency', icon: DollarSign, color: 'emerald' },
  architecture: { label: 'Architecture', icon: Boxes, color: 'amber' },
} as const

// Convert a resource string like "namespace/name" into a workload detail link.
// Returns null for resources that don't map cleanly (e.g., plain titles).
function resourceToLink(resource: string): string | null {
  const parts = resource.split('/')
  if (parts.length !== 2) return null
  const [ns, name] = parts
  if (!ns || !name) return null
  // Link any namespace/name pair to the pod list filtered by namespace
  return `/workloads/pods?group=core&version=v1`
}

const severityColors: Record<string, string> = {
  critical: 'text-red-400 bg-red-500/10 border-red-500/20',
  high: 'text-orange-400 bg-orange-500/10 border-orange-500/20',
  medium: 'text-yellow-400 bg-yellow-500/10 border-yellow-500/20',
  low: 'text-slate-400 bg-slate-500/10 border-slate-500/20',
}

function FactorRow({ factor }: { factor: Factor }) {
  const [expanded, setExpanded] = useState(false)
  const hasResources = factor.resources && factor.resources.length > 0
  const sevClass = severityColors[factor.severity || 'medium'] || severityColors.medium

  return (
    <div className="border border-cluster-border rounded-lg overflow-hidden">
      <button
        onClick={() => hasResources && setExpanded(!expanded)}
        className={clsx(
          'w-full flex items-center gap-3 px-3 py-2 text-left text-sm',
          hasResources && 'hover:bg-white/5 cursor-pointer',
          !hasResources && 'cursor-default'
        )}
      >
        <TrendingDown className="w-4 h-4 text-red-400 flex-shrink-0" aria-hidden="true" />
        <span className="flex-1 text-slate-200 truncate">{factor.name}</span>
        <span className={clsx('text-xs font-medium px-2 py-0.5 rounded-full border', sevClass)}>
          {factor.impact > 0 ? '+' : ''}{factor.impact}
        </span>
        {hasResources && (
          expanded
            ? <ChevronUp className="w-4 h-4 text-slate-500 flex-shrink-0" />
            : <ChevronDown className="w-4 h-4 text-slate-500 flex-shrink-0" />
        )}
      </button>
      {expanded && hasResources && (
        <div className="px-3 pb-2 pt-1 border-t border-cluster-border bg-black/20">
          <div className="flex flex-wrap gap-1.5">
            {factor.resources!.slice(0, 20).map((r, i) => {
              const link = resourceToLink(r)
              if (link) {
                return (
                  <Link
                    key={i}
                    href={link}
                    className="text-xs font-mono text-blue-400 bg-blue-500/10 border border-blue-500/20 rounded px-1.5 py-0.5 hover:bg-blue-500/20 transition-colors inline-flex items-center gap-1"
                  >
                    {r}
                    <ExternalLink className="w-3 h-3" />
                  </Link>
                )
              }
              return (
                <span key={i} className="text-xs font-mono text-slate-400 bg-cluster-border/50 rounded px-1.5 py-0.5">
                  {r}
                </span>
              )
            })}
            {factor.resources!.length > 20 && (
              <span className="text-xs text-slate-500">+{factor.resources!.length - 20} more</span>
            )}
          </div>
          {/* Level 3-4: related pages */}
          <div className="flex gap-2 mt-2 pt-2 border-t border-cluster-border/50">
            {factor.severity === 'critical' || factor.severity === 'high' ? (
              <Link href="/incidents" className="text-[10px] text-blue-400 hover:underline">View related incidents</Link>
            ) : null}
            {factor.name?.includes('security') || factor.name?.includes('severity') ? (
              <Link href="/security" className="text-[10px] text-blue-400 hover:underline">Security findings</Link>
            ) : null}
            {factor.name?.includes('rightsizing') || factor.name?.includes('opportunities') ? (
              <Link href="/optimization" className="text-[10px] text-blue-400 hover:underline">Optimization details</Link>
            ) : null}
            {factor.name?.includes('pods') || factor.name?.includes('crashloop') || factor.name?.includes('pending') ? (
              <Link href="/workloads/pods?group=core&version=v1" className="text-[10px] text-blue-400 hover:underline">Pod health</Link>
            ) : null}
            {factor.name?.includes('anomal') ? (
              <Link href="/anomalies" className="text-[10px] text-blue-400 hover:underline">Anomaly details</Link>
            ) : null}
          </div>
        </div>
      )}
    </div>
  )
}

function DimensionCard({ name, dim }: { name: keyof typeof dimensionConfig; dim: Dimension }) {
  const cfg = dimensionConfig[name]
  const Icon = cfg.icon
  const totalImpact = dim.factors.reduce((sum, f) => sum + f.impact, 0)

  return (
    <div className="bg-cluster-card rounded-xl border border-cluster-border p-4">
      <div className="flex items-center gap-2 mb-3">
        <Icon className={clsx('w-5 h-5', `text-${cfg.color}-400`)} aria-hidden="true" />
        <h3 className="font-semibold text-slate-200">{cfg.label}</h3>
        {dim.score > 0 && (
          <span className="ml-auto text-lg font-bold text-slate-100">{dim.score}</span>
        )}
      </div>
      {dim.factors.length === 0 ? (
        <div className="flex items-center gap-2 text-sm text-emerald-400 py-2">
          <CheckCircle className="w-4 h-4" />
          <span>No issues detected</span>
        </div>
      ) : (
        <>
          <div className="space-y-1.5 mb-2">
            {dim.factors.map((f, i) => (
              <FactorRow key={i} factor={f} />
            ))}
          </div>
          <div className="text-xs text-slate-500 pt-2 border-t border-cluster-border">
            Total impact: <span className="text-red-400 font-medium">{totalImpact}</span>
            {' '} across {dim.factors.reduce((s, f) => s + (f.resources?.length || 0), 0)} resources
          </div>
        </>
      )}
    </div>
  )
}

interface ScoreBreakdownProps {
  expanded: boolean
  onToggle: () => void
  focusDimension?: string | null
}

export function ScoreBreakdown({ expanded, onToggle, focusDimension }: ScoreBreakdownProps) {
  const [data, setData] = useState<Breakdown | null>(null)
  const [loading, setLoading] = useState(false)

  const fetchBreakdown = useCallback(async () => {
    setLoading(true)
    try {
      const bd = await apiFetch<Breakdown>('/api/v1/health/breakdown')
      setData(bd)
    } catch {
      setData(null)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (expanded && !data) {
      fetchBreakdown()
    }
  }, [expanded, data, fetchBreakdown])

  // Auto-refresh when expanded
  useEffect(() => {
    if (!expanded) return
    const id = setInterval(fetchBreakdown, 60000)
    return () => clearInterval(id)
  }, [expanded, fetchBreakdown])

  return (
    <div className="mb-6">
      <button
        onClick={onToggle}
        className="flex items-center gap-2 text-sm text-slate-400 hover:text-slate-200 transition-colors mb-3"
      >
        {loading ? (
          <Loader2 className="w-4 h-4 animate-spin" />
        ) : expanded ? (
          <ChevronUp className="w-4 h-4" />
        ) : (
          <ChevronDown className="w-4 h-4" />
        )}
        <AlertTriangle className="w-4 h-4" aria-hidden="true" />
        <span>{expanded ? 'Hide' : 'Show'} score breakdown</span>
        <span className="text-xs text-slate-500">— click a score card or here to drill down</span>
      </button>

      {expanded && data && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4 animate-in fade-in duration-300">
          {(['reliability', 'security', 'cost', 'architecture'] as const).map(dim => (
            <div
              key={dim}
              id={`breakdown-${dim}`}
              className={clsx(
                'transition-all duration-500',
                focusDimension === dim && 'ring-2 ring-blue-500/50 rounded-xl'
              )}
            >
              <DimensionCard name={dim} dim={data[dim]} />
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

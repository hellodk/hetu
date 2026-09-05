'use client'

import { useEffect, useState } from 'react'
import { apiFetch } from '@/lib/api'
import {
  Sparkles, AlertTriangle, Lightbulb, TrendingUp, Shield,
  Activity, Cpu, Bug, Clock, ChevronDown, ChevronUp
} from 'lucide-react'
import clsx from 'clsx'

interface Insight {
  id: string
  type: 'critical' | 'warning' | 'opportunity' | 'anomaly' | 'security' | 'info'
  source: string
  title: string
  description: string
  metadata?: string
  timestamp?: string
}

const typeConfig = {
  critical: { icon: AlertTriangle, bg: 'bg-sev-crit/10', border: 'border-sev-crit/30', color: 'text-sev-crit', label: 'CRITICAL' },
  warning: { icon: AlertTriangle, bg: 'bg-sev-warn/10', border: 'border-sev-warn/30', color: 'text-sev-warn', label: 'WARNING' },
  opportunity: { icon: Lightbulb, bg: 'bg-sev-ok/10', border: 'border-sev-ok/30', color: 'text-sev-ok', label: 'OPTIMIZATION' },
  anomaly: { icon: TrendingUp, bg: 'bg-sev-info/10', border: 'border-sev-info/30', color: 'text-sev-info', label: 'ANOMALY' },
  security: { icon: Shield, bg: 'bg-sev-high/10', border: 'border-sev-high/30', color: 'text-sev-high', label: 'SECURITY' },
  info: { icon: Activity, bg: 'bg-sev-info/10', border: 'border-sev-info/30', color: 'text-sev-info', label: 'INFO' },
}

function timeSince(iso?: string): string {
  if (!iso) return ''
  const ms = Date.now() - new Date(iso).getTime()
  if (ms < 60000) return 'just now'
  if (ms < 3600000) return `${Math.floor(ms / 60000)}m ago`
  if (ms < 86400000) return `${Math.floor(ms / 3600000)}h ago`
  return `${Math.floor(ms / 86400000)}d ago`
}

// Fetch real data from all v7 handlers and convert to insights
async function fetchInsights(): Promise<Insight[]> {
  const insights: Insight[] = []

  // Security findings (top critical/high)
  try {
    const sec = await apiFetch<{ findings: any[] }>('/api/v1/security/findings?severity=critical')
    const secHigh = await apiFetch<{ findings: any[] }>('/api/v1/security/findings?severity=high')
    const all = [...(sec.findings || []).slice(0, 3), ...(secHigh.findings || []).slice(0, 3)]
    for (const f of all) {
      insights.push({
        id: `sec-${f.id}`,
        type: 'security',
        source: 'Security Scanner',
        title: f.title,
        description: f.remediation || f.description,
        metadata: `CIS ${f.cisControl || 'N/A'} · ${(f.affectedResources || []).length} resources`,
        timestamp: f.detectedAt,
      })
    }
  } catch {}

  // Anomalies
  try {
    const anom = await apiFetch<{ anomalies: any[] }>('/api/v1/anomalies')
    for (const a of (anom.anomalies || []).slice(0, 4)) {
      insights.push({
        id: `anom-${a.id}`,
        type: 'anomaly',
        source: 'Anomaly Detector',
        title: `${a.metric} ${a.score > 0 ? 'spike' : 'drop'} on ${a.service}`,
        description: `Expected ${a.expected.toFixed(2)}, observed ${a.observed.toFixed(2)} (z-score: ${a.score.toFixed(1)})`,
        metadata: `${a.namespace}/${a.service}`,
        timestamp: a.detectedAt,
      })
    }
  } catch {}

  // Optimization recommendations (top by savings)
  try {
    const opts = await apiFetch<{ recommendations: any[] }>('/api/v1/recommendations?status=open')
    const recs = (opts.recommendations || [])
      .sort((a: any, b: any) => (b.estimatedSavingsMonthly || 0) - (a.estimatedSavingsMonthly || 0))
      .slice(0, 4)
    for (const r of recs) {
      insights.push({
        id: `opt-${r.id}`,
        type: 'opportunity',
        source: `Optimizer (${r.type})`,
        title: `${r.type}: ${r.target?.namespace || ''}/${r.target?.name || ''}`,
        description: r.rationale,
        metadata: r.estimatedSavingsMonthly > 0 ? `$${r.estimatedSavingsMonthly.toFixed(0)}/mo savings` : r.type,
        timestamp: r.createdAt,
      })
    }
  } catch {}

  // Pod health issues
  try {
    const ph = await apiFetch<{ categories: any[] }>('/api/v1/pods/health')
    for (const cat of (ph.categories || [])) {
      if (cat.count === 0) continue
      const sev = (cat.name === 'crashloop' || cat.name === 'oomkilled') ? 'critical' : 'warning'
      insights.push({
        id: `pod-${cat.name}`,
        type: sev as any,
        source: 'Pod Health',
        title: `${cat.count} pod(s) in ${cat.name}`,
        description: cat.pods?.slice(0, 3).map((p: any) => `${p.namespace}/${p.name}`).join(', ') || '',
        metadata: `${cat.count} affected`,
      })
    }
  } catch {}

  // Error groups (top 3 by count)
  try {
    const errs = await apiFetch<{ groups: any[] }>('/api/v1/errors/groups?status=open')
    const top = (errs.groups || []).sort((a: any, b: any) => b.count - a.count).slice(0, 3)
    for (const g of top) {
      insights.push({
        id: `err-${g.id}`,
        type: 'warning',
        source: 'Error Aggregator',
        title: `${g.reason}: ${g.service}`,
        description: g.title || g.sampleMessage || '',
        metadata: `${g.count} occurrences · ${g.namespace}`,
        timestamp: g.lastSeen,
      })
    }
  } catch {}

  // Sort by severity then recency
  const sevOrder: Record<string, number> = { critical: 5, security: 4, anomaly: 3, warning: 2, opportunity: 1, info: 0 }
  insights.sort((a, b) => (sevOrder[b.type] || 0) - (sevOrder[a.type] || 0))

  return insights
}

export function AIInsightFeed() {
  const [insights, setInsights] = useState<Insight[]>([])
  const [loading, setLoading] = useState(true)
  const [expanded, setExpanded] = useState(true)

  useEffect(() => {
    fetchInsights()
      .then(setInsights)
      .finally(() => setLoading(false))

    const interval = setInterval(() => {
      fetchInsights().then(setInsights)
    }, 60000)
    return () => clearInterval(interval)
  }, [])

  const criticalCount = insights.filter(i => i.type === 'critical').length
  const warningCount = insights.filter(i => i.type === 'warning' || i.type === 'security').length
  const opportunityCount = insights.filter(i => i.type === 'opportunity').length

  return (
    <section className="bg-cluster-card rounded-xl border border-cluster-border p-4 sm:p-5" aria-label="AI Insights">
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex items-center justify-between w-full mb-3"
      >
        <h2 className="text-sm font-semibold flex items-center gap-2">
          <Sparkles className="w-4 h-4 text-accent" aria-hidden="true" />
          AI Insights
          <span className="text-xs text-cluster-muted font-normal">
            ({insights.length} from {new Set(insights.map(i => i.source)).size} sources)
          </span>
        </h2>
        <div className="flex items-center gap-2">
          {criticalCount > 0 && (
            <span className="px-1.5 py-0.5 text-xs bg-sev-crit/20 text-sev-crit rounded">{criticalCount}</span>
          )}
          {warningCount > 0 && (
            <span className="px-1.5 py-0.5 text-xs bg-sev-warn/20 text-sev-warn rounded">{warningCount}</span>
          )}
          {opportunityCount > 0 && (
            <span className="px-1.5 py-0.5 text-xs bg-sev-ok/20 text-sev-ok rounded">{opportunityCount}</span>
          )}
          {expanded ? <ChevronUp className="w-4 h-4 text-cluster-muted" /> : <ChevronDown className="w-4 h-4 text-cluster-muted" />}
        </div>
      </button>

      {expanded && (
        <div className="space-y-2 max-h-[60vh] overflow-y-auto pr-1">
          {loading && (
            <div className="py-4 text-center text-sm text-cluster-muted">Loading insights...</div>
          )}

          {!loading && insights.length === 0 && (
            <div className="py-4 text-center text-sm text-cluster-muted">
              No insights available. Data will appear as scanners produce findings.
            </div>
          )}

          {insights.map(insight => {
            const cfg = typeConfig[insight.type]
            const Icon = cfg.icon
            return (
              <div
                key={insight.id}
                className={clsx('rounded-lg border p-3', cfg.bg, cfg.border)}
              >
                <div className="flex items-start gap-2.5">
                  <Icon className={clsx('w-4 h-4 mt-0.5 flex-shrink-0', cfg.color)} aria-hidden="true" />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-0.5">
                      <span className={clsx('text-[10px] font-bold uppercase tracking-wider', cfg.color)}>
                        {cfg.label}
                      </span>
                      <span className="text-[10px] text-cluster-muted">{insight.source}</span>
                      {insight.timestamp && (
                        <span className="text-[10px] text-cluster-muted ml-auto flex items-center gap-1">
                          <Clock className="w-3 h-3" />{timeSince(insight.timestamp)}
                        </span>
                      )}
                    </div>
                    <p className="text-sm text-cluster-text leading-snug">{insight.title}</p>
                    <p className="text-xs text-cluster-muted mt-1 line-clamp-2">{insight.description}</p>
                    {insight.metadata && (
                      <p className="text-[10px] text-cluster-muted mt-1 font-mono">{insight.metadata}</p>
                    )}
                  </div>
                </div>
              </div>
            )
          })}
        </div>
      )}
    </section>
  )
}

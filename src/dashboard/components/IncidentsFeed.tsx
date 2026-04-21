'use client'

import { ChevronRight } from 'lucide-react'

interface Issue {
  id:                string
  severity:          'critical' | 'high' | 'medium' | 'low'
  category:          string
  title:             string
  description:       string
  affectedResources: string[]
  confidence:        number
}

interface IncidentsFeedProps {
  issues:     Issue[]
  onViewAll:  () => void
}

const SEV_BAR: Record<string, string> = {
  critical: 'bg-red-500',
  high:     'bg-orange-500',
  medium:   'bg-yellow-500',
  low:      'bg-green-500',
}

const MAX_VISIBLE = 6

export function IncidentsFeed({ issues, onViewAll }: IncidentsFeedProps) {
  const shown       = issues.slice(0, MAX_VISIBLE)
  const hasCritical = issues.some(i => i.severity === 'critical')

  return (
    <section
      className="bg-cluster-card rounded-xl border border-cluster-border overflow-hidden flex flex-col"
      aria-label="Active incidents"
    >
      <div className="px-4 py-3 border-b border-cluster-border flex items-center justify-between flex-shrink-0">
        <span className="text-xs font-bold uppercase tracking-widest text-cluster-muted">
          Active Incidents
        </span>
        <span
          className={`text-xs font-bold px-2 py-0.5 rounded-full ${
            hasCritical
              ? 'bg-red-500/15 text-red-500 border border-red-500/30'
              : 'bg-cluster-border text-cluster-muted'
          }`}
        >
          {issues.length} total
        </span>
      </div>

      {shown.length === 0 ? (
        <div className="px-4 py-10 text-center text-cluster-muted text-sm flex-1">
          No active incidents
        </div>
      ) : (
        <ul role="list" className="flex-1 overflow-y-auto">
          {shown.map(issue => (
            <li
              key={issue.id}
              className="flex items-start gap-3 px-4 py-3 border-b border-cluster-border/50 last:border-0"
            >
              {/* Left severity bar */}
              <div
                className={`w-1 self-stretch rounded-full flex-shrink-0 min-h-[44px] ${SEV_BAR[issue.severity] ?? SEV_BAR.low}`}
                aria-hidden="true"
              />

              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 mb-0.5">
                  <span className="text-[10px] font-bold uppercase tracking-wider text-cluster-muted">
                    {issue.category}
                  </span>
                </div>
                <p className="text-sm font-semibold text-cluster-text leading-snug">
                  {issue.title}
                </p>
                <p className="text-xs text-cluster-muted mt-1 leading-relaxed line-clamp-2">
                  {issue.description}
                </p>
                {issue.affectedResources.length > 0 && (
                  <div className="flex flex-wrap gap-1 mt-2">
                    {issue.affectedResources.slice(0, 3).map(r => (
                      <span
                        key={r}
                        className="text-[10px] bg-cluster-border/50 text-cluster-muted rounded px-1.5 py-0.5 font-mono"
                      >
                        {r}
                      </span>
                    ))}
                    {issue.affectedResources.length > 3 && (
                      <span className="text-[10px] text-cluster-muted">
                        +{issue.affectedResources.length - 3} more
                      </span>
                    )}
                  </div>
                )}
              </div>

              <ChevronRight
                className="w-4 h-4 text-cluster-muted/40 mt-2 flex-shrink-0"
                aria-hidden="true"
              />
            </li>
          ))}
        </ul>
      )}

      {issues.length > MAX_VISIBLE && (
        <div className="px-4 py-2.5 border-t border-cluster-border text-center flex-shrink-0">
          <button
            onClick={onViewAll}
            className="text-xs font-semibold text-blue-400 hover:text-blue-300 transition-colors"
          >
            View all {issues.length} incidents →
          </button>
        </div>
      )}
    </section>
  )
}

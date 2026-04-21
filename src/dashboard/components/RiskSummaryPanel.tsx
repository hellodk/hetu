'use client'

import { scoreLevel, LEVEL_LABEL, LEVEL_COLORS } from '@/lib/scoreLevel'

interface RiskSummaryPanelProps {
  scores: {
    reliability:  number
    security:     number
    cost:         number
    architecture: number
  }
  onDrillDown: (dimension: string) => void
}

const DIMENSIONS = [
  { key: 'reliability',  label: 'Reliability',  detail: 'Pod restarts & availability' },
  { key: 'security',     label: 'Security',      detail: 'RBAC, privileges & policies' },
  { key: 'cost',         label: 'Cost',          detail: 'Resource waste & rightsizing' },
  { key: 'architecture', label: 'Architecture',  detail: 'Design & best practices' },
] as const

export function RiskSummaryPanel({ scores, onDrillDown }: RiskSummaryPanelProps) {
  return (
    <section
      className="bg-cluster-card rounded-xl border border-cluster-border overflow-hidden"
      aria-label="Risk summary"
    >
      <div className="px-4 py-3 border-b border-cluster-border flex items-center justify-between">
        <span className="text-xs font-bold uppercase tracking-widest text-cluster-muted">
          Risk Summary
        </span>
        <span className="text-xs text-cluster-muted">/ 100</span>
      </div>

      <div>
        {DIMENSIONS.map(({ key, label, detail }) => {
          const score  = scores[key]
          const level  = scoreLevel(score)
          const colors = LEVEL_COLORS[level]

          return (
            <button
              key={key}
              onClick={() => onDrillDown(key)}
              className={`w-full text-left flex items-center gap-3 px-4 py-3 border-b border-cluster-border/50 last:border-0 border-l-[3px] ${colors.leftBorder} card-hover`}
              aria-label={`${label}: ${score} out of 100, severity ${LEVEL_LABEL[level]}`}
            >
              <span
                className={`text-[10px] font-bold tracking-widest px-2 py-0.5 rounded ${colors.badge} min-w-[66px] text-center flex-shrink-0`}
              >
                {LEVEL_LABEL[level]}
              </span>

              <span className="flex-1 min-w-0">
                <span className="block text-sm font-semibold text-cluster-text">{label}</span>
                <span className="block text-xs text-cluster-muted mt-0.5 truncate">{detail}</span>
              </span>

              <span className={`text-2xl font-extrabold flex-shrink-0 ${colors.text}`}>
                {score}
              </span>
            </button>
          )
        })}
      </div>
    </section>
  )
}

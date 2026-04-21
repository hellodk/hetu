'use client'

interface Recommendation {
  id:       string
  category: string
  title:    string
  severity: string
  impact: {
    costSavings?: { monthly: number; currency: string }
    effort:       string
  }
}

interface RecommendationsPanelProps {
  recommendations: Recommendation[]
  onViewAll:       () => void
}

const SEV_CHIP: Record<string, string> = {
  critical: 'bg-red-500/15 text-red-600 border border-red-500/30',
  high:     'bg-orange-500/15 text-orange-600 border border-orange-500/30',
  medium:   'bg-yellow-500/15 text-yellow-700 border border-yellow-500/30',
  low:      'bg-green-500/15 text-green-700 border border-green-500/30',
}

const MAX_VISIBLE = 5

export function RecommendationsPanel({ recommendations, onViewAll }: RecommendationsPanelProps) {
  const shown = recommendations.slice(0, MAX_VISIBLE)

  return (
    <section
      className="bg-cluster-card rounded-xl border border-cluster-border overflow-hidden flex flex-col"
      aria-label="Recommendations"
    >
      <div className="px-4 py-3 border-b border-cluster-border flex-shrink-0">
        <span className="text-xs font-bold uppercase tracking-widest text-cluster-muted">
          Recommendations
        </span>
      </div>

      {shown.length === 0 ? (
        <div className="px-4 py-10 text-center text-cluster-muted text-sm flex-1">
          No recommendations
        </div>
      ) : (
        <ul role="list" className="flex-1 overflow-y-auto">
          {shown.map((rec, i) => (
            <li
              key={rec.id}
              className="px-4 py-3 border-b border-cluster-border/50 last:border-0 card-hover cursor-pointer"
            >
              <div className="text-[10px] font-bold text-cluster-muted mb-1">
                #{i + 1} · {rec.category.toUpperCase()}
              </div>
              <p className="text-sm font-semibold text-cluster-text leading-snug mb-2">
                {rec.title}
              </p>
              <div className="flex flex-wrap gap-1.5">
                <span
                  className={`text-[10px] font-semibold px-1.5 py-0.5 rounded ${SEV_CHIP[rec.severity] ?? SEV_CHIP.low}`}
                >
                  {rec.severity.toUpperCase()}
                </span>
                <span className="text-[10px] font-medium px-1.5 py-0.5 rounded bg-cluster-border/50 text-cluster-muted border border-cluster-border">
                  {rec.impact.effort} effort
                </span>
                {rec.impact.costSavings && rec.impact.costSavings.monthly > 0 && (
                  <span className="text-[10px] font-medium px-1.5 py-0.5 rounded bg-blue-500/10 text-blue-500 border border-blue-500/30">
                    ${rec.impact.costSavings.monthly}/mo
                  </span>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}

      {recommendations.length > MAX_VISIBLE && (
        <div className="px-4 py-2.5 border-t border-cluster-border text-center flex-shrink-0">
          <button
            onClick={onViewAll}
            className="text-xs font-semibold text-blue-400 hover:text-blue-300 transition-colors"
          >
            View all {recommendations.length} →
          </button>
        </div>
      )}
    </section>
  )
}

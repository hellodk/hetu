'use client'

import { Sparkles, AlertTriangle, Lightbulb, TrendingUp, Clock, ChevronRight } from 'lucide-react'
import clsx from 'clsx'

interface Issue {
  id: string
  severity: 'critical' | 'high' | 'medium' | 'low'
  category: string
  title: string
  description: string
  confidence: number
  rootCause?: string
  timestamp?: string
}

interface Recommendation {
  id: string
  category: string
  title: string
  description: string
  impact: {
    costSavings?: { monthly: number; currency: string }
  }
  aiReasoning: string
  timestamp?: string
}

interface AIInsightFeedProps {
  issues: Issue[]
  recommendations: Recommendation[]
}

interface Insight {
  id: string
  type: 'critical' | 'warning' | 'opportunity' | 'prediction'
  title: string
  description: string
  metadata?: string
  timestamp: Date
}

function formatRelativeTime(date: Date): string {
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffSeconds = Math.floor(diffMs / 1000)
  const diffMinutes = Math.floor(diffSeconds / 60)
  const diffHours = Math.floor(diffMinutes / 60)
  const diffDays = Math.floor(diffHours / 24)

  if (diffSeconds < 60) return 'Just now'
  if (diffMinutes < 60) return `${diffMinutes}m ago`
  if (diffHours < 24) return `${diffHours}h ago`
  if (diffDays < 7) return `${diffDays}d ago`
  return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
}

export function AIInsightFeed({ issues, recommendations }: AIInsightFeedProps) {
  // Convert issues and recommendations to insights
  const insights: Insight[] = [
    ...issues.map(issue => ({
      id: `issue-${issue.id}`,
      type: issue.severity === 'critical' ? 'critical' as const : 'warning' as const,
      title: issue.title,
      description: issue.rootCause || issue.description,
      metadata: `${Math.round(issue.confidence * 100)}% confidence`,
      timestamp: issue.timestamp ? new Date(issue.timestamp) : new Date()
    })),
    ...recommendations
      .filter(rec => rec.impact.costSavings && rec.impact.costSavings.monthly > 100)
      .map(rec => ({
        id: `rec-${rec.id}`,
        type: 'opportunity' as const,
        title: rec.title,
        description: rec.aiReasoning,
        metadata: `$${rec.impact.costSavings?.monthly}/mo savings`,
        timestamp: rec.timestamp ? new Date(rec.timestamp) : new Date()
      })),
    // Add some predictive insights
    {
      id: 'pred-1',
      type: 'prediction',
      title: 'Memory pressure expected',
      description: 'Based on current growth trends, node-pool-2 may experience memory pressure within 48 hours',
      metadata: '72% probability',
      timestamp: new Date()
    }
  ]

  const typeConfig = {
    critical: {
      icon: AlertTriangle,
      bg: 'bg-red-500/10',
      border: 'border-red-500/30',
      color: 'text-red-400',
      label: 'CRITICAL'
    },
    warning: {
      icon: AlertTriangle,
      bg: 'bg-yellow-500/10',
      border: 'border-yellow-500/30',
      color: 'text-yellow-400',
      label: 'WARNING'
    },
    opportunity: {
      icon: Lightbulb,
      bg: 'bg-emerald-500/10',
      border: 'border-emerald-500/30',
      color: 'text-emerald-400',
      label: 'OPPORTUNITY'
    },
    prediction: {
      icon: TrendingUp,
      bg: 'bg-purple-500/10',
      border: 'border-purple-500/30',
      color: 'text-purple-400',
      label: 'PREDICTION'
    }
  }

  const criticalCount = insights.filter(i => i.type === 'critical').length
  const warningCount = insights.filter(i => i.type === 'warning').length
  const opportunityCount = insights.filter(i => i.type === 'opportunity').length
  const totalSavings = recommendations.reduce((sum, r) => sum + (r.impact.costSavings?.monthly || 0), 0)

  return (
    <section className="bg-cluster-card rounded-xl border border-cluster-border p-4 sm:p-6" aria-labelledby="ai-insights-heading">
      <div className="flex items-center justify-between mb-4">
        <h2 id="ai-insights-heading" className="text-lg font-semibold flex items-center gap-2">
          <Sparkles className="w-5 h-5 text-purple-500" aria-hidden="true" />
          AI Insights
        </h2>
        <span className="flex items-center gap-1 text-xs text-green-400" aria-label="Live updates active">
          <span className="w-2 h-2 bg-green-500 rounded-full animate-pulse" aria-hidden="true" />
          Live
        </span>
      </div>

      <div
        className="space-y-3 max-h-[500px] sm:max-h-[600px] overflow-y-auto pr-1 sm:pr-2"
        role="feed"
        aria-label="AI-generated insights"
        aria-live="polite"
      >
        {insights.map((insight) => {
          const config = typeConfig[insight.type]
          const Icon = config.icon

          return (
            <article
              key={insight.id}
              className={clsx(
                'rounded-lg border p-3 card-interactive',
                config.bg,
                config.border
              )}
              tabIndex={0}
              aria-label={`${config.label}: ${insight.title}`}
            >
              <div className="flex items-start gap-3">
                <div className={clsx('p-1.5 rounded flex-shrink-0', config.bg)} aria-hidden="true">
                  <Icon className={clsx('w-4 h-4', config.color)} />
                </div>

                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 mb-1 flex-wrap">
                    <span className={clsx(
                      'px-1.5 py-0.5 text-[10px] font-bold rounded',
                      config.bg, config.color
                    )}>
                      {config.label}
                    </span>
                    {insight.metadata && (
                      <span className="text-xs text-slate-400">{insight.metadata}</span>
                    )}
                  </div>

                  <h3 className="text-sm font-medium text-cluster-text mb-1 leading-snug">
                    {insight.title}
                  </h3>
                  <p className="text-xs text-slate-400 line-clamp-2">
                    {insight.description}
                  </p>

                  <div className="flex items-center gap-2 mt-2 text-xs text-slate-400">
                    <Clock className="w-3 h-3" aria-hidden="true" />
                    <time dateTime={insight.timestamp.toISOString()}>{formatRelativeTime(insight.timestamp)}</time>
                  </div>
                </div>

                <ChevronRight className="w-4 h-4 text-slate-400 flex-shrink-0" aria-hidden="true" />
              </div>
            </article>
          )
        })}
      </div>

      {/* Quick Stats */}
      <div className="mt-4 pt-4 border-t border-cluster-border">
        <div className="grid grid-cols-3 gap-2 text-center">
          <div>
            <p className="text-lg font-bold text-red-400" aria-label={`${criticalCount} critical issues`}>
              {criticalCount}
            </p>
            <p className="text-xs text-slate-400">Critical</p>
          </div>
          <div>
            <p className="text-lg font-bold text-yellow-400" aria-label={`${warningCount} warnings`}>
              {warningCount}
            </p>
            <p className="text-xs text-slate-400">Warnings</p>
          </div>
          <div>
            <p className="text-lg font-bold text-emerald-400" aria-label={`${opportunityCount} opportunities`}>
              {opportunityCount}
            </p>
            <p className="text-xs text-slate-400">Opportunities</p>
          </div>
        </div>
      </div>

      {/* AI Summary */}
      <div className="mt-4 p-3 bg-purple-500/10 rounded-lg border border-purple-500/20">
        <div className="flex items-center gap-2 mb-2">
          <Sparkles className="w-4 h-4 text-purple-400" aria-hidden="true" />
          <span className="text-xs font-medium text-purple-400">AI Summary</span>
        </div>
        <p className="text-sm text-cluster-text">
          Your cluster is experiencing elevated activity with {criticalCount + warningCount} active
          issues. There are {opportunityCount} optimization opportunities
          that could save approximately ${totalSavings.toLocaleString()}/month.
        </p>
      </div>
    </section>
  )
}

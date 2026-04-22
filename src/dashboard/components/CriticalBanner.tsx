'use client'

import { AlertTriangle, CheckCircle } from 'lucide-react'
import { scoreLevel } from '@/lib/scoreLevel'

interface HealthScores {
  overall:      number
  reliability:  number
  security:     number
  cost:         number
  architecture: number
}

interface CriticalBannerProps {
  scores:        HealthScores
  findingsCount: number
  onViewIssues:  () => void
}

const DIM_LABEL: Record<keyof HealthScores, string> = {
  overall:      'Overall',
  reliability:  'Reliability',
  security:     'Security',
  cost:         'Cost',
  architecture: 'Architecture',
}

export function CriticalBanner({ scores, findingsCount, onViewIssues }: CriticalBannerProps) {
  const criticalDims = (Object.keys(scores) as (keyof HealthScores)[])
    .filter(k => scoreLevel(scores[k]) === 'critical')

  if (criticalDims.length === 0) return null

  const summary = criticalDims
    .map(d => `${DIM_LABEL[d]}: ${scores[d]}/100`)
    .join(' · ')

  const hasFindings = findingsCount > 0

  if (!hasFindings) {
    return (
      <div
        className="bg-green-600 text-white px-4 sm:px-6 py-2.5 flex items-center gap-3 text-sm font-medium"
        role="status"
        aria-label="Low health scores but no active findings"
        data-testid="critical-banner"
      >
        <CheckCircle className="w-4 h-4 flex-shrink-0" aria-hidden="true" />
        <strong className="font-bold tracking-wide">NO FINDINGS</strong>
        <span className="hidden sm:inline text-green-200">—</span>
        <span className="text-green-100">Low scores detected ({summary}) but no active issues found</span>
      </div>
    )
  }

  return (
    <div
      className="bg-red-600 text-white px-4 sm:px-6 py-2.5 flex items-center gap-3 text-sm font-medium"
      role="alert"
      aria-live="assertive"
      aria-label="Critical health alert"
      data-testid="critical-banner"
    >
      <AlertTriangle className="w-4 h-4 flex-shrink-0" aria-hidden="true" />
      <strong className="font-bold tracking-wide">CRITICAL</strong>
      <span className="hidden sm:inline text-red-200">—</span>
      <span className="text-red-100">{summary}</span>
      <button
        onClick={onViewIssues}
        className="ml-auto text-red-200 hover:text-white underline font-semibold whitespace-nowrap text-xs sm:text-sm transition-colors"
        aria-label="View critical findings"
      >
        View findings →
      </button>
    </div>
  )
}

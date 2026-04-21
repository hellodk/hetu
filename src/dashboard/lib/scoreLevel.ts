export type ScoreLevel = 'critical' | 'high' | 'degraded' | 'ok'

export function scoreLevel(score: number): ScoreLevel {
  if (score <= 25) return 'critical'
  if (score <= 50) return 'high'
  if (score <= 74) return 'degraded'
  return 'ok'
}

export const LEVEL_LABEL: Record<ScoreLevel, string> = {
  critical: 'CRITICAL',
  high:     'HIGH',
  degraded: 'DEGRADED',
  ok:       'OK',
}

// Tailwind opacity classes — work on both light and dark themes.
export const LEVEL_COLORS: Record<ScoreLevel, {
  text:        string
  bg:          string
  border:      string
  badge:       string
  leftBorder:  string
}> = {
  critical: {
    text:        'text-red-500',
    bg:          'bg-red-500/10',
    border:      'border-red-500/30',
    badge:       'bg-red-500/15 text-red-600 border border-red-500/40',
    leftBorder:  'border-l-red-600',
  },
  high: {
    text:        'text-orange-500',
    bg:          'bg-orange-500/10',
    border:      'border-orange-500/30',
    badge:       'bg-orange-500/15 text-orange-600 border border-orange-500/40',
    leftBorder:  'border-l-orange-500',
  },
  degraded: {
    text:        'text-yellow-500',
    bg:          'bg-yellow-500/10',
    border:      'border-yellow-500/30',
    badge:       'bg-yellow-500/15 text-yellow-700 border border-yellow-500/40',
    leftBorder:  'border-l-yellow-500',
  },
  ok: {
    text:        'text-green-500',
    bg:          'bg-green-500/10',
    border:      'border-green-500/30',
    badge:       'bg-green-500/15 text-green-700 border border-green-500/40',
    leftBorder:  'border-l-green-500',
  },
}

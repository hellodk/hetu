'use client'

import { ReactNode, useEffect, useState, useId } from 'react'
import { TrendingUp, TrendingDown, Minus } from 'lucide-react'
import clsx from 'clsx'

interface ScoreCardProps {
  title: string
  score: number
  icon: ReactNode
  color: 'blue' | 'green' | 'purple' | 'emerald' | 'amber' | 'red'
  trend?: number
  subtitle?: string
}

const colorMap = {
  blue: {
    bg: 'bg-blue-500/10',
    border: 'border-blue-500/30',
    text: 'text-blue-400',
    ring: 'stroke-blue-500',
  },
  green: {
    bg: 'bg-green-500/10',
    border: 'border-green-500/30',
    text: 'text-green-400',
    ring: 'stroke-green-500',
  },
  purple: {
    bg: 'bg-purple-500/10',
    border: 'border-purple-500/30',
    text: 'text-purple-400',
    ring: 'stroke-purple-500',
  },
  emerald: {
    bg: 'bg-emerald-500/10',
    border: 'border-emerald-500/30',
    text: 'text-emerald-400',
    ring: 'stroke-emerald-500',
  },
  amber: {
    bg: 'bg-amber-500/10',
    border: 'border-amber-500/30',
    text: 'text-amber-400',
    ring: 'stroke-amber-500',
  },
  red: {
    bg: 'bg-red-500/10',
    border: 'border-red-500/30',
    text: 'text-red-400',
    ring: 'stroke-red-500',
  },
}

function getScoreColor(score: number): string {
  if (score >= 90) return 'text-green-400'
  if (score >= 75) return 'text-lime-400'
  if (score >= 60) return 'text-yellow-400'
  if (score >= 40) return 'text-orange-400'
  return 'text-red-400'
}

function getScoreLabel(score: number): string {
  if (score >= 90) return 'Excellent'
  if (score >= 75) return 'Good'
  if (score >= 60) return 'Fair'
  if (score >= 40) return 'Poor'
  return 'Critical'
}

function getTrendDescription(trend: number | undefined): string {
  if (trend === undefined) return ''
  if (trend > 0) return `Up ${trend}% from last period`
  if (trend < 0) return `Down ${Math.abs(trend)}% from last period`
  return 'No change from last period'
}

export function ScoreCard({ title, score, icon, color, trend, subtitle }: ScoreCardProps) {
  const colors = colorMap[color]
  const circumference = 2 * Math.PI * 45 // radius = 45
  const offset = circumference - (score / 100) * circumference
  const uniqueId = useId()
  const titleId = `score-title-${uniqueId}`
  const descId = `score-desc-${uniqueId}`
  
  // Animation state for score ring
  const [isAnimated, setIsAnimated] = useState(false)
  
  useEffect(() => {
    // Trigger animation after mount
    const timer = setTimeout(() => setIsAnimated(true), 100)
    return () => clearTimeout(timer)
  }, [])

  return (
    <article 
      className={clsx(
        'rounded-xl p-3 sm:p-4 border card-hover',
        colors.bg,
        colors.border
      )}
      aria-labelledby={titleId}
      aria-describedby={descId}
    >
      <div className="flex items-start justify-between mb-2 sm:mb-3">
        <div className={clsx('p-1.5 sm:p-2 rounded-lg', colors.bg)} aria-hidden="true">
          <div className={colors.text}>{icon}</div>
        </div>
        {trend !== undefined && trend !== 0 && (
          <div 
            className={clsx(
              'flex items-center gap-1 text-xs px-2 py-1 rounded-full',
              trend > 0 ? 'bg-green-500/20 text-green-400' : 'bg-red-500/20 text-red-400'
            )}
            aria-label={getTrendDescription(trend)}
          >
            {trend > 0 ? (
              <TrendingUp className="w-3 h-3" aria-hidden="true" />
            ) : (
              <TrendingDown className="w-3 h-3" aria-hidden="true" />
            )}
            <span>{Math.abs(trend)}%</span>
          </div>
        )}
        {trend === 0 && (
          <div 
            className="flex items-center gap-1 text-xs px-2 py-1 rounded-full bg-slate-500/20 text-slate-400"
            aria-label="No change from last period"
          >
            <Minus className="w-3 h-3" aria-hidden="true" />
            <span>Stable</span>
          </div>
        )}
      </div>

      <div className="flex items-center gap-3 sm:gap-4">
        {/* Score Ring - Accessible */}
        <div 
          className="relative w-16 h-16 sm:w-20 sm:h-20 flex-shrink-0"
          role="img"
          aria-label={`${title} score: ${score} out of 100, ${getScoreLabel(score)}`}
        >
          <svg 
            className={clsx('w-full h-full score-ring', isAnimated && 'score-ring-animate')} 
            viewBox="0 0 100 100"
            aria-hidden="true"
          >
            {/* Background ring */}
            <circle
              cx="50"
              cy="50"
              r="45"
              fill="none"
              stroke="currentColor"
              strokeWidth="8"
              className="text-cluster-border"
            />
            {/* Score ring */}
            <circle
              cx="50"
              cy="50"
              r="45"
              fill="none"
              strokeWidth="8"
              strokeLinecap="round"
              className={clsx('score-value transition-all duration-1000', colors.ring)}
              style={{
                strokeDasharray: circumference,
                strokeDashoffset: isAnimated ? offset : circumference,
              }}
            />
          </svg>
          <div className="absolute inset-0 flex items-center justify-center">
            <span className={clsx('text-xl sm:text-2xl font-bold', getScoreColor(score))} aria-hidden="true">
              {score}
            </span>
          </div>
        </div>

        <div className="flex-1 min-w-0">
          <h3 id={titleId} className="text-xs sm:text-sm font-medium text-slate-400 truncate">
            {title}
          </h3>
          <p id={descId} className={clsx('text-xs sm:text-sm font-medium', getScoreColor(score))}>
            {getScoreLabel(score)}
          </p>
          {subtitle && (
            <p className="text-[10px] sm:text-xs text-slate-400 mt-1 truncate" title={subtitle}>
              {subtitle}
            </p>
          )}
        </div>
      </div>
    </article>
  )
}

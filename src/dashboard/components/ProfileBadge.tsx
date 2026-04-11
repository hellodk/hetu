'use client'

// ProfileBadge renders a small pill indicating whether the analyzer is
// serving live data or synthetic demo data. It lives in the dashboard
// header so operators can tell at a glance which mode they're looking at.
//
// The "DEMO MODE" variant uses an amber color scheme with a pulsing dot
// to make it visually distinct from a healthy "LIVE" state.

import { Activity, PlayCircle } from 'lucide-react'

export type ProfileName = 'live' | 'mock'

interface ProfileBadgeProps {
  profile: ProfileName
}

export function ProfileBadge({ profile }: ProfileBadgeProps) {
  if (profile === 'mock') {
    return (
      <span
        className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-semibold uppercase tracking-wide bg-amber-500/15 text-amber-300 border border-amber-500/30"
        role="status"
        aria-label="Demo mode active — synthetic data is being shown"
        title="Demo mode: synthetic data is being shown. No real cluster analysis is running."
      >
        <PlayCircle className="w-3.5 h-3.5 animate-pulse" aria-hidden="true" />
        DEMO MODE
      </span>
    )
  }

  return (
    <span
      className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-semibold uppercase tracking-wide bg-emerald-500/15 text-emerald-300 border border-emerald-500/30"
      role="status"
      aria-label="Live mode — real cluster data"
      title="Live mode: data is being computed from real cluster telemetry."
    >
      <Activity className="w-3.5 h-3.5" aria-hidden="true" />
      LIVE
    </span>
  )
}

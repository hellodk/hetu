'use client'

import { Activity, RefreshCw, Download, Bell, Settings } from 'lucide-react'

interface StatusBarProps {
  clusterId:    string
  profile:      'live' | 'mock'
  lastUpdated:  Date | null
  loading:      boolean
  criticalCount: number
  version:      string
  onRefresh:    () => void
  onExport:     () => void
  onBell:       () => void
  onSettings:   () => void
}

export function StatusBar({
  clusterId,
  profile,
  lastUpdated,
  loading,
  criticalCount,
  version,
  onRefresh,
  onExport,
  onBell,
  onSettings,
}: StatusBarProps) {
  const isLive = profile === 'live'

  return (
    <>
      {/* Theme-aware: light in graphite, dark in dark themes (uses cluster-* tokens). */}
      <header
        className="bg-cluster-card text-cluster-text border-b border-cluster-border px-4 sm:px-6 py-2 flex items-center gap-3 sm:gap-4 text-xs font-mono sticky top-0 z-50"
        aria-label="Dashboard status bar"
        data-testid="status-bar"
      >
      {/* Brand */}
      <h1 className="flex items-center gap-2 font-sans font-bold text-cluster-text text-sm mr-1 flex-shrink-0">
        <Activity className="w-4 h-4 text-blue-500" aria-hidden="true" />
        <span className="hidden sm:inline">Hetu</span>
        <span className="sm:hidden">K8s Intel</span>
      </h1>

      <span className="text-cluster-border hidden sm:inline" aria-hidden="true">|</span>

      <span
        className="text-cluster-muted hidden sm:inline truncate max-w-[120px]"
        aria-label={`Cluster: ${clusterId}`}
        title={clusterId}
      >
        {clusterId}
      </span>

      <span className="text-cluster-border hidden md:inline" aria-hidden="true">|</span>

      {/* Live / Demo pill */}
      <span
        className={`flex items-center gap-1.5 flex-shrink-0 ${isLive ? 'text-green-600' : 'text-amber-600'}`}
        aria-label={`Profile: ${isLive ? 'live' : 'demo mode'}`}
      >
        <span
          className={`w-1.5 h-1.5 rounded-full ${isLive ? "bg-green-500" : "bg-amber-500"} animate-pulse`}
          aria-hidden="true"
        />
        {isLive ? 'LIVE' : 'DEMO'}
      </span>

      {lastUpdated && (
        <>
          <span className="text-cluster-border hidden md:inline" aria-hidden="true">|</span>
          <span className="text-cluster-muted hidden md:inline">
            {lastUpdated.toLocaleTimeString()}
          </span>
        </>
      )}

      <span className="text-cluster-border hidden lg:inline" aria-hidden="true">|</span>
      <span className="text-cluster-muted hidden lg:inline">v{version}</span>

      {/* Actions */}
      <div className="ml-auto flex items-center gap-0.5" role="group" aria-label="Dashboard actions">
        <button
          onClick={onRefresh}
          disabled={loading}
          className="p-1.5 rounded hover:bg-cluster-bg text-cluster-muted hover:text-cluster-text transition-colors disabled:opacity-50"
          aria-label={loading ? 'Refreshing data…' : 'Refresh dashboard data'}
        >
          <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} aria-hidden="true" />
        </button>

        <button
          onClick={onExport}
          className="p-1.5 rounded hover:bg-cluster-bg text-cluster-muted hover:text-cluster-text transition-colors"
          aria-label="Export JSON report"
        >
          <Download className="w-4 h-4" aria-hidden="true" />
        </button>

        <button
          onClick={onBell}
          className="relative p-1.5 rounded hover:bg-cluster-bg text-cluster-muted hover:text-cluster-text transition-colors"
          aria-label={`Alerts${criticalCount > 0 ? `, ${criticalCount} critical` : ', none'}`}
        >
          <Bell className="w-4 h-4" aria-hidden="true" />
          {criticalCount > 0 && (
            <span
              className="absolute -top-0.5 -right-0.5 w-4 h-4 bg-red-500 text-white text-[9px] rounded-full flex items-center justify-center font-bold leading-none"
              aria-hidden="true"
            >
              {criticalCount > 9 ? '9+' : criticalCount}
            </span>
          )}
        </button>

        <button
          onClick={onSettings}
          className="p-1.5 rounded hover:bg-cluster-bg text-cluster-muted hover:text-cluster-text transition-colors"
          aria-label="Settings"
        >
          <Settings className="w-4 h-4" aria-hidden="true" />
        </button>
      </div>
    </header>
    </>
  )
}

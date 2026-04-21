'use client'

import { useState, useEffect } from 'react'
import { Clock, AlertTriangle, CheckCircle, Activity } from 'lucide-react'
import clsx from 'clsx'
import { apiFetch } from '@/lib/api'

interface TimelineEvent {
  id: string
  timestamp: string
  type: 'incident' | 'recovery' | 'warning' | 'info'
  title: string
  description: string
  severity?: string
}

// No mock data — timeline shows real events from the cluster or empty state

const typeConfig = {
  incident: {
    icon: AlertTriangle,
    color: 'text-red-400',
    bg: 'bg-red-500/10',
    border: 'border-red-500/30',
    dot: 'bg-red-500'
  },
  recovery: {
    icon: CheckCircle,
    color: 'text-green-400',
    bg: 'bg-green-500/10',
    border: 'border-green-500/30',
    dot: 'bg-green-500'
  },
  warning: {
    icon: AlertTriangle,
    color: 'text-yellow-400',
    bg: 'bg-yellow-500/10',
    border: 'border-yellow-500/30',
    dot: 'bg-yellow-500'
  },
  info: {
    icon: Activity,
    color: 'text-blue-400',
    bg: 'bg-blue-500/10',
    border: 'border-blue-500/30',
    dot: 'bg-blue-500'
  }
}

export function TimelineChart() {
  const [filter, setFilter] = useState<'all' | 'incident' | 'warning' | 'recovery'>('all')
  const [events, setEvents] = useState<TimelineEvent[]>([])

  useEffect(() => {
    const fetchEvents = async () => {
      try {
        // Use relative URL + Next.js proxy (via apiFetch) instead of the
        // old `${API_URL}/api/v1/...` pattern. The old fallback was
        // `http://localhost:8081`, which failed two ways: it pointed at
        // port 8081 (analyzer dev is on 18081) and, from a LAN browser,
        // "localhost" was the client's own loopback.
        const data = await apiFetch<{ actions?: any[] }>('/api/v1/actions/log')
        const mappedEvents: TimelineEvent[] = (data.actions || []).map((action: any, i: number) => ({
          id: `action-${i}`,
          timestamp: action.timestamp,
          type: action.success ? 'recovery' : 'incident',
          title: `${action.action} on ${action.target}`,
          description: `${action.message} (by ${action.initiatedBy})`,
          severity: action.success ? undefined : 'high'
        }))
        setEvents(mappedEvents)
      } catch (err) {
        console.error("Failed to fetch action logs", err)
      }
    }
    fetchEvents()
    const interval = setInterval(fetchEvents, 30000)
    return () => clearInterval(interval)
  }, [])

  const filteredEvents = events.filter(event => {
    if (filter === 'all') return true
    return event.type === filter
  })

  const formatTime = (timestamp: string) => {
    const date = new Date(timestamp)
    return date.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' })
  }

  const formatDate = (timestamp: string) => {
    const date = new Date(timestamp)
    return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
  }

  // Format relative time (e.g., "2 hours ago", "5 minutes ago")
  const formatRelativeTime = (timestamp: string): string => {
    const date = new Date(timestamp)
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
    return formatDate(timestamp)
  }

  return (
    <section className="bg-cluster-card rounded-xl border border-cluster-border p-4 sm:p-6" aria-labelledby="timeline-heading">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 mb-6">
        <h2 id="timeline-heading" className="text-lg font-semibold flex items-center gap-2">
          <Clock className="w-5 h-5 text-blue-500" aria-hidden="true" />
          Event Timeline
        </h2>

        {/* Filters */}
        <div className="flex items-center gap-2 flex-wrap" role="group" aria-label="Filter events by type">
          {(['all', 'incident', 'warning', 'recovery'] as const).map((type) => (
            <button
              key={type}
              onClick={() => setFilter(type)}
              className={clsx(
                'px-3 py-1.5 text-xs font-medium rounded-lg transition-colors capitalize',
                filter === type
                  ? 'bg-blue-600 text-white'
                  : 'bg-cluster-border text-cluster-muted hover:text-cluster-text'
              )}
              aria-pressed={filter === type}
            >
              {type}
            </button>
          ))}
        </div>
      </div>

      {/* Timeline */}
      <div className="relative" role="list" aria-label="Timeline events">
        {/* Vertical line */}
        <div className="absolute left-5 sm:left-6 top-0 bottom-0 w-0.5 bg-cluster-border" aria-hidden="true" />

        <div className="space-y-4">
          {filteredEvents.map((event) => {
            const config = typeConfig[event.type]
            const Icon = config.icon

            return (
              <article
                key={event.id}
                className="relative flex gap-3 sm:gap-4"
                role="listitem"
              >
                {/* Dot on timeline */}
                <div className="relative z-10 flex-shrink-0" aria-hidden="true">
                  <div className={clsx(
                    'w-10 h-10 sm:w-12 sm:h-12 rounded-full flex items-center justify-center',
                    config.bg
                  )}>
                    <Icon className={clsx('w-4 h-4 sm:w-5 sm:h-5', config.color)} />
                  </div>
                </div>

                {/* Event content */}
                <div className={clsx(
                  'flex-1 rounded-lg border p-3 sm:p-4',
                  config.bg,
                  config.border
                )}>
                  <div className="flex flex-col sm:flex-row sm:items-start justify-between gap-2 sm:gap-4">
                    <div className="min-w-0">
                      <div className="flex items-center gap-2 mb-1 flex-wrap">
                        <span className={clsx(
                          'px-2 py-0.5 text-xs font-medium rounded-full capitalize',
                          config.bg, config.color
                        )}>
                          {event.type}
                        </span>
                        {event.severity && (
                          <span className={clsx(
                            'px-2 py-0.5 text-xs font-medium rounded-full capitalize',
                            event.severity === 'critical' ? 'bg-red-900/50 text-red-300' :
                              event.severity === 'high' ? 'bg-orange-900/50 text-orange-300' :
                                'bg-yellow-900/50 text-yellow-300'
                          )}>
                            {event.severity}
                          </span>
                        )}
                      </div>
                      <h3 className="font-medium text-cluster-text">{event.title}</h3>
                      <p className="text-sm text-slate-400 mt-1">{event.description}</p>
                    </div>

                    <div className="flex sm:flex-col items-center sm:items-end gap-2 sm:gap-0 flex-shrink-0">
                      <time
                        dateTime={event.timestamp}
                        className="text-sm font-medium text-blue-400"
                        title={`${formatDate(event.timestamp)} ${formatTime(event.timestamp)}`}
                      >
                        {formatRelativeTime(event.timestamp)}
                      </time>
                      <span className="text-xs text-slate-400 hidden sm:block">
                        {formatTime(event.timestamp)}
                      </span>
                    </div>
                  </div>
                </div>
              </article>
            )
          })}
        </div>
      </div>

      {filteredEvents.length === 0 && (
        <div className="text-center py-8">
          <Activity className="w-12 h-12 text-slate-500 mx-auto mb-4" aria-hidden="true" />
          <p className="text-slate-400">No events matching filter</p>
        </div>
      )}
    </section>
  )
}

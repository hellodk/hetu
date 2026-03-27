'use client'

import { useState, useEffect } from 'react'
import { Clock, AlertTriangle, CheckCircle, Activity } from 'lucide-react'
import clsx from 'clsx'

const API_URL = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8081'

interface TimelineEvent {
  id: string
  timestamp: string
  type: 'incident' | 'recovery' | 'warning' | 'info'
  title: string
  description: string
  severity?: string
}

// Mock timeline data
const mockEvents: TimelineEvent[] = [
  {
    id: '1',
    timestamp: '2026-02-14T10:30:00Z',
    type: 'incident',
    title: 'CrashLoopBackOff detected',
    description: 'Pod api-gateway started experiencing repeated crashes',
    severity: 'critical'
  },
  {
    id: '2',
    timestamp: '2026-02-14T10:15:00Z',
    type: 'warning',
    title: 'Memory pressure increasing',
    description: 'Node node-pool-1-abc123 memory usage reached 85%',
    severity: 'high'
  },
  {
    id: '3',
    timestamp: '2026-02-14T09:45:00Z',
    type: 'recovery',
    title: 'Deployment rollout complete',
    description: 'Frontend deployment successfully rolled out 3 replicas'
  },
  {
    id: '4',
    timestamp: '2026-02-14T09:30:00Z',
    type: 'info',
    title: 'HPA scaled deployment',
    description: 'worker-pool scaled from 3 to 5 replicas'
  },
  {
    id: '5',
    timestamp: '2026-02-14T09:00:00Z',
    type: 'warning',
    title: 'High latency detected',
    description: 'API response time exceeded 500ms threshold',
    severity: 'medium'
  },
  {
    id: '6',
    timestamp: '2026-02-14T08:30:00Z',
    type: 'recovery',
    title: 'Issue resolved',
    description: 'Database connection pool recovered'
  }
]

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
        const res = await fetch(`${API_URL}/api/v1/events/timeline`)
        if (res.ok) {
          const data = await res.json()
          setEvents(data || [])
        }
      } catch (err) {
        console.error("Failed to fetch timeline events", err)
      }
    }
    fetchEvents()
    const interval = setInterval(fetchEvents, 30000)
    return () => clearInterval(interval)
  }, [])

  const displayEvents = events.length > 0 ? events : mockEvents // fallback to mock if no real events

  const filteredEvents = displayEvents.filter(event => {
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
                  : 'bg-cluster-border text-slate-400 hover:text-white'
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

'use client'

import { useState, useMemo, useCallback } from 'react'
import Link from 'next/link'
import { AlertTriangle, AlertCircle, Info, ChevronRight, ExternalLink, X, Sparkles, Wrench, Search, Filter } from 'lucide-react'
import clsx from 'clsx'
import { Modal } from './Modal'

interface Issue {
  id: string
  severity: 'critical' | 'high' | 'medium' | 'low'
  category: string
  title: string
  description: string
  affectedResources: string[] | null
  confidence: number
  rootCause?: string
  suggestedFix?: string
  evidence?: CorrelatedEvidence[]
}

interface CorrelatedEvidence {
  event?: any
  metrics?: Record<string, any[]>
  logLines?: string[]
  relatedPods?: string[]
}

interface IssuesListProps {
  issues: Issue[]
  expanded?: boolean
  onViewAll?: () => void
  onIssueClick?: (issue: Issue) => void
  onToast?: (type: 'success' | 'error' | 'info', message: string) => void
}

const severityConfig = {
  critical: {
    icon: AlertTriangle,
    bg: 'bg-red-500/10',
    border: 'border-red-500/30',
    text: 'text-red-400',
    badge: 'bg-red-900/50 text-red-300 border-red-700',
    priority: 1,
  },
  high: {
    icon: AlertTriangle,
    bg: 'bg-orange-500/10',
    border: 'border-orange-500/30',
    text: 'text-orange-400',
    badge: 'bg-orange-900/50 text-orange-300 border-orange-700',
    priority: 2,
  },
  medium: {
    icon: AlertCircle,
    bg: 'bg-yellow-500/10',
    border: 'border-yellow-500/30',
    text: 'text-yellow-400',
    badge: 'bg-yellow-900/50 text-yellow-300 border-yellow-700',
    priority: 3,
  },
  low: {
    icon: Info,
    bg: 'bg-blue-500/10',
    border: 'border-blue-500/30',
    text: 'text-blue-400',
    badge: 'bg-blue-900/50 text-blue-300 border-blue-700',
    priority: 4,
  },
}

const categoryColors: Record<string, string> = {
  reliability: 'bg-blue-500/20 text-blue-300',
  security: 'bg-purple-500/20 text-purple-300',
  cost: 'bg-emerald-500/20 text-emerald-300',
  architecture: 'bg-amber-500/20 text-amber-300',
}

type SeverityFilter = 'all' | 'critical' | 'high' | 'medium' | 'low'

export function IssuesList({ issues, expanded = false, onViewAll, onIssueClick, onToast }: IssuesListProps) {
  const [selectedIssue, setSelectedIssue] = useState<Issue | null>(null)
  const [showModal, setShowModal] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [severityFilter, setSeverityFilter] = useState<SeverityFilter>('all')

  // Filter and search issues
  const filteredIssues = useMemo(() => {
    let result = [...issues]

    // Apply severity filter
    if (severityFilter !== 'all') {
      result = result.filter(issue => issue.severity === severityFilter)
    }

    // Apply search query
    if (searchQuery.trim()) {
      const query = searchQuery.toLowerCase()
      result = result.filter(issue =>
        issue.title.toLowerCase().includes(query) ||
        issue.description.toLowerCase().includes(query) ||
        issue.category.toLowerCase().includes(query) ||
        (issue.affectedResources ?? []).some(r => r.toLowerCase().includes(query))
      )
    }

    // Sort by severity
    result.sort((a, b) =>
      severityConfig[a.severity].priority - severityConfig[b.severity].priority
    )

    return result
  }, [issues, searchQuery, severityFilter])

  const handleIssueClick = useCallback((issue: Issue) => {
    setSelectedIssue(issue)
    setShowModal(true)
    if (onIssueClick) {
      onIssueClick(issue)
    }
  }, [onIssueClick])

  const handleViewAll = useCallback(() => {
    if (onViewAll) {
      onViewAll()
    }
  }, [onViewAll])

  // Generate suggested fix based on issue type
  const getSuggestedFix = useCallback((issue: Issue): string => {
    if (issue.suggestedFix) return issue.suggestedFix

    if (issue.title.toLowerCase().includes('crashloop')) {
      return `1. Check pod logs: kubectl logs -n <namespace> <pod-name> --previous
2. Describe the pod: kubectl describe pod -n <namespace> <pod-name>
3. Check resource limits and increase if OOMKilled
4. Verify environment variables and secrets
5. Check liveness/readiness probe configurations`
    }
    if (issue.category === 'security') {
      return `1. Review RBAC policies: kubectl get rolebindings,clusterrolebindings
2. Apply least-privilege principle
3. Use dedicated service accounts per workload
4. Remove cluster-admin bindings where not needed
5. Implement network policies for isolation`
    }
    if (issue.category === 'reliability') {
      return `1. Add Pod Disruption Budget (PDB)
2. Configure proper resource requests/limits
3. Add health checks (liveness/readiness probes)
4. Consider adding replicas for high availability
5. Review restart policies`
    }
    return `1. Review the affected resources
2. Check cluster events: kubectl get events --sort-by='.lastTimestamp'
3. Analyze resource metrics
4. Apply recommended configuration changes
5. Monitor after remediation`
  }, [])

  const handleCopyFix = useCallback(async (issue: Issue) => {
    try {
      await navigator.clipboard.writeText(getSuggestedFix(issue))
      onToast?.('success', 'Fix steps copied to clipboard')
    } catch {
      onToast?.('error', 'Failed to copy to clipboard')
    }
  }, [getSuggestedFix, onToast])

  // Severity counts for filter buttons
  const severityCounts = useMemo(() => {
    return issues.reduce((acc, issue) => {
      acc[issue.severity] = (acc[issue.severity] || 0) + 1
      return acc
    }, {} as Record<string, number>)
  }, [issues])

  if (issues.length === 0) {
    return (
      <section className="bg-cluster-card rounded-xl border border-cluster-border p-6" aria-labelledby="issues-heading-empty">
        <h2 id="issues-heading-empty" className="text-lg font-semibold mb-4 flex items-center gap-2">
          <AlertTriangle className="w-5 h-5 text-yellow-500" aria-hidden="true" />
          Active Issues
        </h2>
        <div className="text-center py-8">
          <div className="w-16 h-16 bg-green-500/10 rounded-full flex items-center justify-center mx-auto mb-4">
            <AlertCircle className="w-8 h-8 text-green-500" aria-hidden="true" />
          </div>
          <p className="text-slate-300 font-medium">No active issues detected</p>
          <p className="text-sm text-slate-400 mt-1">Your cluster is running smoothly</p>
        </div>
      </section>
    )
  }

  return (
    <section className="bg-cluster-card rounded-xl border border-cluster-border p-4 sm:p-6" aria-labelledby="issues-heading">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 mb-4">
        <h2 id="issues-heading" className="text-lg font-semibold flex items-center gap-2">
          <AlertTriangle className="w-5 h-5 text-yellow-500" aria-hidden="true" />
          Active Issues
        </h2>
        <span className="text-sm text-slate-400" aria-live="polite">
          {filteredIssues.length} of {issues.length} issue{issues.length !== 1 ? 's' : ''}
        </span>
      </div>

      {/* Search and Filter - Only in expanded view */}
      {expanded && (
        <div className="flex flex-col sm:flex-row gap-3 mb-4">
          {/* Search Input */}
          <div className="relative flex-1">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-500" aria-hidden="true" />
            <input
              type="search"
              placeholder="Search issues..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="search-input"
              aria-label="Search issues"
            />
          </div>

          {/* Severity Filter */}
          <div className="flex items-center gap-2 flex-wrap" role="group" aria-label="Filter by severity">
            <Filter className="w-4 h-4 text-slate-400 hidden sm:block" aria-hidden="true" />
            {(['all', 'critical', 'high', 'medium', 'low'] as const).map((severity) => (
              <button
                key={severity}
                onClick={() => setSeverityFilter(severity)}
                className={clsx(
                  'px-3 py-1.5 text-xs font-medium rounded-lg transition-colors capitalize',
                  severityFilter === severity
                    ? 'bg-blue-600 text-white'
                    : 'bg-cluster-border text-slate-400 hover:text-white'
                )}
                aria-pressed={severityFilter === severity}
              >
                {severity === 'all' ? 'All' : `${severity} ${severityCounts[severity] || 0}`}
              </button>
            ))}
          </div>
        </div>
      )}

      {/* Issues List */}
      <div className="space-y-3" role="list" aria-label="Issues list">
        {filteredIssues.length === 0 ? (
          <div className="text-center py-8">
            <Search className="w-8 h-8 text-slate-500 mx-auto mb-2" aria-hidden="true" />
            <p className="text-slate-400">No issues match your search</p>
          </div>
        ) : (
          filteredIssues.map((issue) => {
            const config = severityConfig[issue.severity]
            const Icon = config.icon

            return (
              <article
                key={issue.id}
                onClick={() => handleIssueClick(issue)}
                onKeyDown={(e) => e.key === 'Enter' && handleIssueClick(issue)}
                className={clsx(
                  'rounded-lg border p-3 sm:p-4 card-interactive',
                  config.bg,
                  config.border
                )}
                role="listitem"
                tabIndex={0}
                aria-label={`${issue.severity} severity ${issue.category} issue: ${issue.title}`}
              >
                <div className="flex items-start gap-3">
                  <div className={clsx('p-2 rounded-lg flex-shrink-0', config.bg)} aria-hidden="true">
                    <Icon className={clsx('w-5 h-5', config.text)} />
                  </div>

                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-1 flex-wrap">
                      <span className={clsx(
                        'px-2 py-0.5 text-xs font-medium rounded-full border uppercase',
                        config.badge
                      )}>
                        {issue.severity}
                      </span>
                      <span className={clsx(
                        'px-2 py-0.5 text-xs font-medium rounded-full capitalize',
                        categoryColors[issue.category] || 'bg-slate-500/20 text-slate-300'
                      )}>
                        {issue.category}
                      </span>
                      <span className="text-xs text-slate-400">
                        {Math.round(issue.confidence * 100)}% confidence
                      </span>
                    </div>

                    <h3 className="font-medium text-cluster-text mb-1">{issue.title}</h3>
                    <p className="text-sm text-slate-400 mb-2">{issue.description}</p>

                    {expanded && (
                      <>
                        {issue.rootCause && (
                          <div className="mt-3 p-3 bg-black/20 rounded-lg">
                            <p className="text-xs text-slate-400 uppercase tracking-wide mb-1">Root Cause Analysis</p>
                            <p className="text-sm text-cluster-text">{issue.rootCause}</p>
                          </div>
                        )}

                        <div className="mt-3">
                          <p className="text-xs text-slate-400 uppercase tracking-wide mb-2">Affected Resources</p>
                          <div className="flex flex-wrap gap-2">
                            {(issue.affectedResources ?? []).map((resource, idx) => {
                              const parts = resource.split('/')
                              const ns = parts.length >= 2 ? parts[0] : 'default'
                              const name = parts.length >= 2 ? parts[1] : parts[0]
                              return (
                                <Link
                                  key={idx}
                                  href={`/workloads/pods/${ns}/${name}?group=core&version=v1`}
                                  className="px-2 py-1 bg-black/20 hover:bg-blue-600/20 rounded text-xs font-mono text-cluster-text hover:text-blue-400 flex items-center gap-1 transition-colors"
                                >
                                  {resource}
                                  <ExternalLink className="w-3 h-3 text-slate-400" aria-hidden="true" />
                                </Link>
                              )
                            })}
                          </div>
                        </div>
                      </>
                    )}
                  </div>

                  <ChevronRight className="w-5 h-5 text-slate-400 flex-shrink-0" aria-hidden="true" />
                </div>
              </article>
            )
          })
        )}
      </div>

      {!expanded && issues.length > 3 && (
        <button
          onClick={handleViewAll}
          className="w-full mt-4 py-2 text-sm text-blue-400 hover:text-blue-300 transition-colors focus-visible:ring-2 focus-visible:ring-blue-500 rounded-lg"
        >
          View all {issues.length} issues
        </button>
      )}

      {/* Issue Details Modal */}
      <Modal
        isOpen={showModal}
        onClose={() => { setShowModal(false); setSelectedIssue(null); }}
        title={selectedIssue?.title || 'Issue Details'}
        size="lg"
      >
        {selectedIssue && (
          <div className="space-y-6">
            {/* Severity & Category */}
            <div className="flex items-center gap-3 flex-wrap">
              <span className={clsx(
                'px-3 py-1 text-sm font-medium rounded-full border uppercase',
                severityConfig[selectedIssue.severity].badge
              )}>
                {selectedIssue.severity}
              </span>
              <span className={clsx(
                'px-3 py-1 text-sm font-medium rounded-full capitalize',
                categoryColors[selectedIssue.category] || 'bg-slate-500/20 text-slate-300'
              )}>
                {selectedIssue.category}
              </span>
              <span className="text-sm text-slate-400">
                {Math.round(selectedIssue.confidence * 100)}% confidence
              </span>
            </div>

            {/* Description */}
            <div>
              <h4 className="text-sm font-medium text-slate-400 mb-2">Description</h4>
              <p className="text-cluster-text">{selectedIssue.description}</p>
            </div>

            {/* Root Cause Analysis */}
            {selectedIssue.rootCause && (
              <div className="p-4 bg-purple-500/10 rounded-lg border border-purple-500/30">
                <div className="flex items-center gap-2 mb-2">
                  <Sparkles className="w-4 h-4 text-purple-400" aria-hidden="true" />
                  <span className="text-sm font-medium text-purple-400">Root Cause Analysis</span>
                </div>
                <p className="text-cluster-text">{selectedIssue.rootCause}</p>
              </div>
            )}

            {/* Affected Resources */}
            <div>
              <h4 className="text-sm font-medium text-slate-400 mb-2">Affected Resources</h4>
              <div className="flex flex-wrap gap-2">
                {(selectedIssue.affectedResources ?? []).map((resource, idx) => (
                  <span
                    key={idx}
                    className="px-3 py-1.5 bg-black/20 rounded-lg text-sm font-mono text-cluster-text flex items-center gap-2"
                  >
                    {resource}
                    <ExternalLink className="w-3.5 h-3.5 text-slate-400" aria-hidden="true" />
                  </span>
                ))}
              </div>
            </div>

            {/* Correlated Evidence */}
            {selectedIssue.evidence && selectedIssue.evidence.length > 0 && (
              <div>
                <h4 className="text-sm font-medium text-slate-400 mb-2">Correlated Evidence</h4>
                <div className="space-y-3">
                  {selectedIssue.evidence.map((ev, idx) => (
                    <div key={idx} className="p-3 bg-black/20 rounded-lg border border-cluster-border text-sm">
                      {ev.event && (
                        <div className="mb-2">
                          <span className="font-semibold text-slate-300">Event: </span>
                          <span className="text-cluster-text">{ev.event.reason || ev.event.type}</span>
                          <p className="text-slate-400 text-xs mt-1">{ev.event.message}</p>
                        </div>
                      )}
                      {ev.logLines && ev.logLines.length > 0 && (
                        <div className="mt-2">
                          <span className="text-xs font-semibold text-slate-400 uppercase">Related Logs</span>
                          <pre className="mt-1 p-2 bg-black/40 rounded text-xs text-slate-300 overflow-x-auto whitespace-pre font-mono">
                            {ev.logLines.join('\n')}
                          </pre>
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              </div>
            )}

            {/* Suggested Fix */}
            <div className="p-4 bg-blue-500/10 rounded-lg border border-blue-500/30">
              <div className="flex items-center gap-2 mb-3">
                <Wrench className="w-4 h-4 text-blue-400" aria-hidden="true" />
                <span className="text-sm font-medium text-blue-400">Suggested Fix</span>
              </div>
              <div className="text-sm text-cluster-text whitespace-pre-line font-mono bg-black/20 p-3 rounded-lg overflow-x-auto">
                {getSuggestedFix(selectedIssue)}
              </div>
            </div>

            {/* Actions */}
            <div className="flex flex-col sm:flex-row justify-end gap-3 pt-4 border-t border-cluster-border">
              <button
                onClick={() => { setShowModal(false); setSelectedIssue(null); }}
                className="btn-ghost px-4 py-2 text-slate-400 hover:text-cluster-text"
              >
                Close
              </button>
              <button
                onClick={() => handleCopyFix(selectedIssue)}
                className="btn-secondary"
              >
                Copy Fix Steps
              </button>
            </div>
          </div>
        )}
      </Modal>
    </section>
  )
}

'use client'

import { useState } from 'react'
import { Lightbulb, DollarSign, Clock, AlertTriangle, CheckCircle, ChevronRight, Sparkles, Copy, Check, Terminal } from 'lucide-react'
import clsx from 'clsx'
import { Modal } from './Modal'

interface Recommendation {
  id: string
  category: string
  title: string
  description: string
  severity: string
  confidence: number
  impact: {
    costSavings?: { monthly: number; currency: string }
    riskLevel: string
    effort: string
  }
  aiReasoning: string
  fix?: {
    yaml?: string
    command?: string
    steps?: string[]
  }
}

interface RecommendationsListProps {
  recommendations: Recommendation[]
  onApplyFix?: (recommendation: Recommendation) => void
  onDismiss?: (recommendation: Recommendation) => void
  onToast?: (type: 'success' | 'error' | 'info', message: string) => void
}

const categoryIcons: Record<string, typeof Lightbulb> = {
  cost: DollarSign,
  reliability: CheckCircle,
  security: AlertTriangle,
  architecture: Lightbulb,
}

const categoryColors: Record<string, { bg: string; text: string; border: string }> = {
  cost: { bg: 'bg-emerald-500/10', text: 'text-emerald-400', border: 'border-emerald-500/30' },
  reliability: { bg: 'bg-blue-500/10', text: 'text-blue-400', border: 'border-blue-500/30' },
  security: { bg: 'bg-purple-500/10', text: 'text-purple-400', border: 'border-purple-500/30' },
  architecture: { bg: 'bg-amber-500/10', text: 'text-amber-400', border: 'border-amber-500/30' },
}

const effortColors: Record<string, string> = {
  low: 'bg-green-500/20 text-green-300',
  medium: 'bg-yellow-500/20 text-yellow-300',
  high: 'bg-red-500/20 text-red-300',
}

const riskColors: Record<string, string> = {
  low: 'text-green-400',
  medium: 'text-yellow-400',
  high: 'text-red-400',
}

export function RecommendationsList({ recommendations, onApplyFix, onDismiss, onToast }: RecommendationsListProps) {
  const [selectedRec, setSelectedRec] = useState<Recommendation | null>(null)
  const [showDetailsModal, setShowDetailsModal] = useState(false)
  const [showApplyModal, setShowApplyModal] = useState(false)
  const [copiedId, setCopiedId] = useState<string | null>(null)
  const [dismissedIds, setDismissedIds] = useState<Set<string>>(new Set())

  const handleViewDetails = (rec: Recommendation) => {
    setSelectedRec(rec)
    setShowDetailsModal(true)
  }

  const handleApplyFix = (rec: Recommendation) => {
    setSelectedRec(rec)
    setShowApplyModal(true)
  }

  const handleConfirmApply = () => {
    if (selectedRec) {
      onToast?.('success', `Applied fix: ${selectedRec.title}`)
      if (onApplyFix) {
        onApplyFix(selectedRec)
      }
    }
    setShowApplyModal(false)
    setSelectedRec(null)
  }

  const handleDismiss = (rec: Recommendation) => {
    setDismissedIds(prev => new Set([...Array.from(prev), rec.id]))
    onToast?.('info', `Dismissed: ${rec.title}`)
    if (onDismiss) {
      onDismiss(rec)
    }
  }

  const copyToClipboard = async (text: string, id: string) => {
    try {
      await navigator.clipboard.writeText(text)
      setCopiedId(id)
      onToast?.('success', 'Configuration copied to clipboard')
      setTimeout(() => setCopiedId(null), 2000)
    } catch (err) {
      console.error('Failed to copy:', err)
      onToast?.('error', 'Failed to copy to clipboard')
    }
  }

  // Return fix YAML from the recommendation if available
  const generateFixYaml = (rec: Recommendation): string => {
    if (rec.fix?.yaml) return rec.fix.yaml
    return `# No specific YAML patch available for this recommendation.
# Visit the Optimization page for detailed, data-driven recommendations
# with copy-paste YAML patches based on actual cluster metrics.`
  }

  const visibleRecommendations = recommendations.filter(rec => !dismissedIds.has(rec.id))

  if (visibleRecommendations.length === 0) {
    return (
      <div className="bg-cluster-card rounded-xl border border-cluster-border p-6">
        <h2 className="text-lg font-semibold mb-4 flex items-center gap-2">
          <Lightbulb className="w-5 h-5 text-yellow-500" />
          AI Recommendations
        </h2>
        <div className="text-center py-8">
          <div className="w-16 h-16 bg-green-500/10 rounded-full flex items-center justify-center mx-auto mb-4">
            <CheckCircle className="w-8 h-8 text-green-500" />
          </div>
          <p className="text-cluster-muted">No recommendations at this time</p>
          <p className="text-sm text-cluster-muted mt-1">Your cluster is well optimized</p>
        </div>
      </div>
    )
  }

  // Calculate total potential savings
  const totalSavings = recommendations.reduce((sum, rec) => {
    return sum + (rec.impact.costSavings?.monthly || 0)
  }, 0)

  return (
    <div className="bg-cluster-card rounded-xl border border-cluster-border p-6">
      <div className="flex items-center justify-between mb-6">
        <h2 className="text-lg font-semibold flex items-center gap-2">
          <Lightbulb className="w-5 h-5 text-yellow-500" />
          AI Recommendations
        </h2>
        {totalSavings > 0 && (
          <div className="flex items-center gap-2 px-3 py-1.5 bg-emerald-500/10 rounded-lg border border-emerald-500/30">
            <DollarSign className="w-4 h-4 text-emerald-400" />
            <span className="text-sm text-emerald-400 font-medium">
              ${totalSavings.toLocaleString()}/mo potential savings
            </span>
          </div>
        )}
      </div>

      <div className="space-y-4">
        {visibleRecommendations.map((rec) => {
          const colors = categoryColors[rec.category] || categoryColors.architecture
          const Icon = categoryIcons[rec.category] || Lightbulb

          return (
            <div
              key={rec.id}
              className={clsx(
                'rounded-lg border p-4 transition-all duration-200 hover:shadow-md cursor-pointer',
                colors.bg,
                colors.border
              )}
            >
              <div className="flex items-start gap-4">
                <div className={clsx('p-2 rounded-lg flex-shrink-0', colors.bg)}>
                  <Icon className={clsx('w-5 h-5', colors.text)} />
                </div>

                <div className="flex-1 min-w-0">
                  {/* Header */}
                  <div className="flex items-center gap-2 mb-2 flex-wrap">
                    <span className={clsx(
                      'px-2 py-0.5 text-xs font-medium rounded-full capitalize',
                      colors.bg, colors.text
                    )}>
                      {rec.category}
                    </span>
                    <span className={clsx(
                      'px-2 py-0.5 text-xs font-medium rounded-full',
                      effortColors[rec.impact.effort] || effortColors.medium
                    )}>
                      {rec.impact.effort} effort
                    </span>
                    <span className="text-xs text-cluster-muted">
                      {Math.round(rec.confidence * 100)}% confidence
                    </span>
                  </div>

                  {/* Title & Description */}
                  <h3 className="font-medium text-cluster-text mb-1">{rec.title}</h3>
                  <p className="text-sm text-cluster-muted mb-3">{rec.description}</p>

                  {/* Impact Metrics */}
                  <div className="flex items-center gap-4 flex-wrap">
                    {rec.impact.costSavings && (
                      <div className="flex items-center gap-1.5">
                        <DollarSign className="w-4 h-4 text-emerald-400" />
                        <span className="text-sm font-medium text-emerald-400">
                          ${rec.impact.costSavings.monthly.toLocaleString()}/mo
                        </span>
                      </div>
                    )}
                    <div className="flex items-center gap-1.5">
                      <AlertTriangle className={clsx('w-4 h-4', riskColors[rec.impact.riskLevel])} />
                      <span className={clsx('text-sm capitalize', riskColors[rec.impact.riskLevel])}>
                        {rec.impact.riskLevel} risk
                      </span>
                    </div>
                  </div>

                  {/* AI Reasoning */}
                  <div className="mt-3 p-3 bg-black/20 rounded-lg">
                    <div className="flex items-center gap-1.5 mb-1.5">
                      <Sparkles className="w-3.5 h-3.5 text-purple-400" />
                      <span className="text-xs text-purple-400 font-medium">AI Analysis</span>
                    </div>
                    <p className="text-sm text-cluster-muted">{rec.aiReasoning}</p>
                  </div>

                  {/* Actions */}
                  <div className="flex items-center gap-3 mt-4">
                    <button
                      onClick={(e) => { e.stopPropagation(); handleApplyFix(rec); }}
                      className="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded-lg transition-colors"
                    >
                      Apply Fix
                    </button>
                    <button
                      onClick={(e) => { e.stopPropagation(); handleViewDetails(rec); }}
                      className="px-4 py-2 bg-cluster-border hover:bg-slate-600 text-cluster-text text-sm font-medium rounded-lg transition-colors"
                    >
                      View Details
                    </button>
                    <button
                      onClick={(e) => { e.stopPropagation(); handleDismiss(rec); }}
                      className="px-4 py-2 text-cluster-muted hover:text-cluster-text text-sm transition-colors"
                    >
                      Dismiss
                    </button>
                  </div>
                </div>

                <ChevronRight className="w-5 h-5 text-cluster-muted flex-shrink-0 mt-2" />
              </div>
            </div>
          )
        })}
      </div>

      {/* View Details Modal */}
      <Modal
        isOpen={showDetailsModal}
        onClose={() => { setShowDetailsModal(false); setSelectedRec(null); }}
        title={selectedRec?.title || 'Recommendation Details'}
        size="lg"
      >
        {selectedRec && (
          <div className="space-y-6">
            {/* Header Info */}
            <div className="flex items-center gap-3 flex-wrap">
              <span className={clsx(
                'px-3 py-1 text-sm font-medium rounded-full capitalize',
                categoryColors[selectedRec.category]?.bg,
                categoryColors[selectedRec.category]?.text
              )}>
                {selectedRec.category}
              </span>
              <span className={clsx(
                'px-3 py-1 text-sm font-medium rounded-full',
                effortColors[selectedRec.impact.effort]
              )}>
                {selectedRec.impact.effort} effort
              </span>
              <span className="text-sm text-cluster-muted">
                {Math.round(selectedRec.confidence * 100)}% confidence
              </span>
            </div>

            {/* Description */}
            <div>
              <h4 className="text-sm font-medium text-cluster-muted mb-2">Description</h4>
              <p className="text-cluster-text">{selectedRec.description}</p>
            </div>

            {/* Impact */}
            <div className="grid grid-cols-2 gap-4">
              {selectedRec.impact.costSavings && (
                <div className="p-4 bg-emerald-500/10 rounded-lg border border-emerald-500/30">
                  <div className="flex items-center gap-2 mb-1">
                    <DollarSign className="w-4 h-4 text-emerald-400" />
                    <span className="text-sm font-medium text-emerald-400">Estimated Savings</span>
                  </div>
                  <span className="text-2xl font-bold text-emerald-400">
                    ${selectedRec.impact.costSavings.monthly.toLocaleString()}/mo
                  </span>
                </div>
              )}
              <div className="p-4 bg-black/20 rounded-lg">
                <div className="flex items-center gap-2 mb-1">
                  <AlertTriangle className={clsx('w-4 h-4', riskColors[selectedRec.impact.riskLevel])} />
                  <span className="text-sm font-medium text-cluster-muted">Risk Level</span>
                </div>
                <span className={clsx('text-lg font-semibold capitalize', riskColors[selectedRec.impact.riskLevel])}>
                  {selectedRec.impact.riskLevel}
                </span>
              </div>
            </div>

            {/* AI Reasoning */}
            <div className="p-4 bg-purple-500/10 rounded-lg border border-purple-500/30">
              <div className="flex items-center gap-2 mb-2">
                <Sparkles className="w-4 h-4 text-purple-400" />
                <span className="text-sm font-medium text-purple-400">AI Analysis</span>
              </div>
              <p className="text-cluster-text">{selectedRec.aiReasoning}</p>
            </div>

            {/* Recommended Fix YAML */}
            <div>
              <div className="flex items-center justify-between mb-2">
                <h4 className="text-sm font-medium text-cluster-muted">Recommended Configuration</h4>
                <button
                  onClick={() => copyToClipboard(generateFixYaml(selectedRec), `details-${selectedRec.id}`)}
                  className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-cluster-border hover:bg-slate-600 rounded-lg transition-colors"
                >
                  {copiedId === `details-${selectedRec.id}` ? (
                    <>
                      <Check className="w-4 h-4 text-green-400" />
                      <span className="text-green-400">Copied!</span>
                    </>
                  ) : (
                    <>
                      <Copy className="w-4 h-4" />
                      <span>Copy YAML</span>
                    </>
                  )}
                </button>
              </div>
              <pre className="p-4 bg-slate-900 rounded-lg overflow-x-auto text-sm text-emerald-400 font-mono">
                {generateFixYaml(selectedRec)}
              </pre>
            </div>

            {/* Actions */}
            <div className="flex justify-end gap-3 pt-4 border-t border-cluster-border">
              <button
                onClick={() => { setShowDetailsModal(false); setSelectedRec(null); }}
                className="px-4 py-2 text-cluster-muted hover:text-cluster-text transition-colors"
              >
                Close
              </button>
              <button
                onClick={() => { setShowDetailsModal(false); handleApplyFix(selectedRec); }}
                className="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white font-medium rounded-lg transition-colors"
              >
                Apply Fix
              </button>
            </div>
          </div>
        )}
      </Modal>

      {/* Apply Fix Confirmation Modal */}
      <Modal
        isOpen={showApplyModal}
        onClose={() => { setShowApplyModal(false); setSelectedRec(null); }}
        title="Apply Fix"
        size="lg"
      >
        {selectedRec && (
          <div className="space-y-6">
            <div className="p-4 bg-amber-500/10 rounded-lg border border-amber-500/30">
              <div className="flex items-center gap-2 mb-2">
                <AlertTriangle className="w-5 h-5 text-amber-400" />
                <span className="font-medium text-amber-400">Review Before Applying</span>
              </div>
              <p className="text-sm text-cluster-muted">
                Please review the following configuration changes before applying them to your cluster.
              </p>
            </div>

            <div>
              <h4 className="font-medium mb-2">{selectedRec.title}</h4>
              <p className="text-sm text-cluster-muted mb-4">{selectedRec.description}</p>
            </div>

            {/* Fix YAML */}
            <div>
              <div className="flex items-center justify-between mb-2">
                <h4 className="text-sm font-medium text-cluster-muted">Configuration to Apply</h4>
                <button
                  onClick={() => copyToClipboard(generateFixYaml(selectedRec), `apply-${selectedRec.id}`)}
                  className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-cluster-border hover:bg-slate-600 rounded-lg transition-colors"
                >
                  {copiedId === `apply-${selectedRec.id}` ? (
                    <>
                      <Check className="w-4 h-4 text-green-400" />
                      <span className="text-green-400">Copied!</span>
                    </>
                  ) : (
                    <>
                      <Copy className="w-4 h-4" />
                      <span>Copy YAML</span>
                    </>
                  )}
                </button>
              </div>
              <pre className="p-4 bg-slate-900 rounded-lg overflow-x-auto text-sm text-emerald-400 font-mono">
                {generateFixYaml(selectedRec)}
              </pre>
            </div>

            {/* Manual Apply Instructions */}
            <div className="p-4 bg-black/20 rounded-lg">
              <div className="flex items-center gap-2 mb-2">
                <Terminal className="w-4 h-4 text-cluster-muted" />
                <span className="text-sm font-medium text-cluster-muted">Manual Apply Command</span>
              </div>
              <code className="text-sm text-blue-400">
                kubectl apply -f recommended-fix.yaml
              </code>
            </div>

            {/* Actions */}
            <div className="flex justify-end gap-3 pt-4 border-t border-cluster-border">
              <button
                onClick={() => { setShowApplyModal(false); setSelectedRec(null); }}
                className="px-4 py-2 text-cluster-muted hover:text-cluster-text transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={handleConfirmApply}
                className="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white font-medium rounded-lg transition-colors"
              >
                Confirm & Apply
              </button>
            </div>
          </div>
        )}
      </Modal>
    </div>
  )
}

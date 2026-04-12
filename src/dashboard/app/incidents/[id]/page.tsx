'use client'

import { useEffect, useState } from 'react'
import { useParams } from 'next/navigation'
import Link from 'next/link'
import { apiFetch, getApiUrl } from '@/lib/api'
import {
  ArrowLeft, Loader2, Clock, AlertCircle, Zap,
  RefreshCw, Send, CheckCircle, Shield, Wrench
} from 'lucide-react'

interface Signal {
  timestamp: string
  source: string
  severity: string
  service: string
  namespace: string
  pod: string
  kind: string
  title: string
  details: string
}

interface RCAReport {
  summary: string
  rootCause: { primary: string; confidence: number; description: string }
  contributingFactors: string[]
  blastRadius: { services: string[]; users: string; severity: string }
  remediation: { step: string; risk: string; automatable: boolean; estimatedEffort: string }[]
  preventiveMeasures: string[]
  evidence: { id: string; type: string; ref: string; snippet: string }[]
  model: string
  promptTokens: number
  outputTokens: number
  createdAt: string
}

interface Incident {
  id: number
  severity: string
  status: string
  detectedAt: string
  affected: string[]
  summary: string
  signals: Signal[]
  rcaReport?: RCAReport
}

export default function IncidentDetailPage() {
  const params = useParams()
  const id = params.id as string
  const [incident, setIncident] = useState<Incident | null>(null)
  const [loading, setLoading] = useState(true)
  const [regenerating, setRegenerating] = useState(false)
  const [question, setQuestion] = useState('')
  const [aiAnswer, setAiAnswer] = useState('')
  const [asking, setAsking] = useState(false)

  useEffect(() => {
    setLoading(true)
    apiFetch<Incident>(`/api/v1/incidents/${id}`)
      .then(setIncident)
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [id])

  const regenerateRCA = async () => {
    setRegenerating(true)
    try {
      const report = await apiFetch<RCAReport>(`/api/v1/incidents/${id}/rca/regenerate`)
      setIncident(prev => prev ? { ...prev, rcaReport: report, status: 'investigating' } : null)
    } catch {}
    finally { setRegenerating(false) }
  }

  const askAI = async () => {
    if (!question.trim()) return
    setAsking(true)
    try {
      const res = await fetch(`${getApiUrl()}/api/v1/llm/ask`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ question, incidentId: parseInt(id) }),
      })
      const data = await res.json()
      setAiAnswer(data.answer || 'No response')
    } catch { setAiAnswer('Failed to get response') }
    finally { setAsking(false) }
  }

  if (loading) return <div className="flex justify-center items-center min-h-[50vh]"><Loader2 className="w-6 h-6 animate-spin text-blue-400" /></div>
  if (!incident) return <div className="p-6"><div className="p-4 bg-red-900/30 border border-red-700 rounded text-red-300">Incident not found</div></div>

  const rca = incident.rcaReport

  return (
    <div className="p-6">
      <Link href="/incidents" className="flex items-center gap-1 text-sm text-gray-400 hover:text-white mb-4">
        <ArrowLeft className="w-4 h-4" /> Back to Incidents
      </Link>

      {/* Header */}
      <div className="flex items-start justify-between mb-6">
        <div>
          <div className="flex items-center gap-3 mb-2">
            <h1 className="text-2xl font-bold text-white">INC-{incident.id}</h1>
            <span className={`px-2 py-0.5 text-xs rounded ${
              incident.severity === 'critical' ? 'bg-red-900/30 text-red-300' :
              incident.severity === 'high' ? 'bg-orange-900/30 text-orange-300' :
              'bg-yellow-900/30 text-yellow-300'
            }`}>{incident.severity}</span>
            <span className="px-2 py-0.5 text-xs bg-gray-700 text-gray-300 rounded">{incident.status}</span>
          </div>
          <p className="text-sm text-gray-400">{incident.summary}</p>
        </div>
        <button onClick={regenerateRCA} disabled={regenerating}
          className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-purple-600 hover:bg-purple-500 text-white rounded disabled:opacity-50">
          {regenerating ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Zap className="w-3.5 h-3.5" />}
          {rca ? 'Regenerate RCA' : 'Run RCA'}
        </button>
      </div>

      {/* Signal Timeline */}
      <h2 className="text-lg font-semibold text-white mb-3 flex items-center gap-2">
        <Clock className="w-4 h-4" /> Signal Timeline ({incident.signals?.length || 0})
      </h2>
      <div className="space-y-2 mb-8">
        {(incident.signals || []).map((sig, i) => (
          <div key={i} className="flex items-start gap-3 p-3 bg-gray-800/30 border border-gray-700/30 rounded-lg">
            <span className={`mt-0.5 w-2 h-2 rounded-full shrink-0 ${
              sig.severity === 'critical' ? 'bg-red-500' :
              sig.severity === 'high' ? 'bg-orange-400' : 'bg-yellow-400'
            }`} />
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2 text-sm">
                <span className="text-xs px-1.5 py-0.5 bg-gray-700 rounded text-gray-400">{sig.source}</span>
                <span className="text-white font-medium">{sig.kind}</span>
                <span className="text-gray-500">—</span>
                <span className="text-gray-300 truncate">{sig.title}</span>
              </div>
              <div className="text-xs text-gray-500 mt-0.5">
                {sig.namespace}/{sig.service || sig.pod} &bull; {new Date(sig.timestamp).toLocaleTimeString()}
              </div>
            </div>
          </div>
        ))}
      </div>

      {/* RCA Report */}
      {rca && (
        <div className="space-y-6">
          <h2 className="text-lg font-semibold text-white flex items-center gap-2">
            <Zap className="w-4 h-4 text-purple-400" /> Root Cause Analysis
            <span className="text-xs text-gray-500 font-normal">by {rca.model} &bull; {new Date(rca.createdAt).toLocaleString()}</span>
          </h2>

          {/* Summary */}
          <div className="p-4 bg-purple-900/10 border border-purple-700/30 rounded-lg">
            <p className="text-sm text-gray-200">{rca.summary}</p>
          </div>

          {/* Root Cause */}
          <div className="p-4 bg-gray-800/50 border border-gray-700/50 rounded-lg">
            <h3 className="text-sm font-medium text-gray-400 mb-2 flex items-center gap-2">
              <AlertCircle className="w-4 h-4 text-red-400" /> Root Cause
              <span className="text-xs text-gray-500">confidence: {(rca.rootCause.confidence * 100).toFixed(0)}%</span>
            </h3>
            <p className="text-sm font-medium text-white mb-1">{rca.rootCause.primary}</p>
            <p className="text-sm text-gray-400">{rca.rootCause.description}</p>
          </div>

          {/* Remediation */}
          {rca.remediation?.length > 0 && (
            <div>
              <h3 className="text-sm font-medium text-gray-400 mb-2 flex items-center gap-2">
                <Wrench className="w-4 h-4 text-blue-400" /> Remediation Steps
              </h3>
              <div className="space-y-2">
                {rca.remediation.map((step, i) => (
                  <div key={i} className="flex items-start gap-3 p-3 bg-gray-800/30 border border-gray-700/30 rounded-lg">
                    <span className="text-xs font-bold text-gray-500 mt-0.5">{i + 1}</span>
                    <div className="flex-1">
                      <p className="text-sm text-gray-200">{step.step}</p>
                      <div className="flex gap-3 mt-1 text-xs text-gray-500">
                        <span>Risk: <span className={step.risk === 'high' ? 'text-red-400' : step.risk === 'medium' ? 'text-yellow-400' : 'text-green-400'}>{step.risk}</span></span>
                        <span>Effort: {step.estimatedEffort}</span>
                        {step.automatable && <span className="text-blue-400">automatable</span>}
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Evidence */}
          {rca.evidence?.length > 0 && (
            <div>
              <h3 className="text-sm font-medium text-gray-400 mb-2 flex items-center gap-2">
                <Shield className="w-4 h-4 text-green-400" /> Evidence
              </h3>
              <div className="space-y-1">
                {rca.evidence.map((ev, i) => (
                  <div key={i} className="p-2 bg-gray-800/30 rounded text-xs">
                    <span className="text-gray-500">[{ev.type}] </span>
                    <span className="text-gray-300">{ev.snippet || ev.ref}</span>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Preventive Measures */}
          {rca.preventiveMeasures?.length > 0 && (
            <div>
              <h3 className="text-sm font-medium text-gray-400 mb-2 flex items-center gap-2">
                <CheckCircle className="w-4 h-4 text-green-400" /> Preventive Measures
              </h3>
              <ul className="list-disc list-inside text-sm text-gray-300 space-y-1">
                {rca.preventiveMeasures.map((m, i) => <li key={i}>{m}</li>)}
              </ul>
            </div>
          )}

          {/* Token usage */}
          <div className="text-xs text-gray-500">
            Tokens: {rca.promptTokens} prompt + {rca.outputTokens} output
          </div>
        </div>
      )}

      {/* Ask AI follow-up */}
      <div className="mt-8 pt-6 border-t border-gray-700">
        <h3 className="text-sm font-medium text-gray-400 mb-2">Ask AI</h3>
        <div className="flex gap-2">
          <input type="text" value={question} onChange={e => setQuestion(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && askAI()}
            placeholder="Ask a follow-up question about this incident..."
            className="flex-1 bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-white placeholder-gray-500" />
          <button onClick={askAI} disabled={asking || !question.trim()}
            className="px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded text-sm disabled:opacity-50">
            {asking ? <Loader2 className="w-4 h-4 animate-spin" /> : <Send className="w-4 h-4" />}
          </button>
        </div>
        {aiAnswer && (
          <div className="mt-3 p-4 bg-gray-800/50 border border-gray-700/50 rounded-lg">
            <pre className="text-sm text-gray-300 whitespace-pre-wrap">{aiAnswer}</pre>
          </div>
        )}
      </div>
    </div>
  )
}

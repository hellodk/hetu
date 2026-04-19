'use client'

import { createContext, useContext, useEffect, useRef, useState } from 'react'
import { useParams } from 'next/navigation'
import Link from 'next/link'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { apiFetch, getApiUrl } from '@/lib/api'
import {
  ArrowLeft, Loader2, Clock, AlertCircle, Zap,
  Send, CheckCircle, Shield, Wrench, Bot, User, Copy, Check
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

interface ChatMessage {
  id: string
  role: 'user' | 'assistant'
  content: string
}

// Context that lets MarkdownCode know it's inside a MarkdownPre block.
// This correctly handles bare fenced code blocks that carry no language-* class.
const InCodeBlock = createContext(false)

function MarkdownPre({ children }: { children?: React.ReactNode }) {
  const [copied, setCopied] = useState(false)
  const preRef = useRef<HTMLPreElement>(null)

  const handleCopy = () => {
    const text = preRef.current?.textContent ?? ''
    navigator.clipboard.writeText(text).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    }).catch(() => {})
  }

  return (
    <InCodeBlock.Provider value={true}>
      <div className="relative group mt-2 mb-2">
        <pre ref={preRef} className="bg-gray-900 rounded p-3 overflow-x-auto pr-10">{children}</pre>
        <button
          onClick={handleCopy}
          className="absolute top-2 right-2 p-1 rounded opacity-0 group-hover:opacity-100 transition-opacity bg-gray-700 hover:bg-gray-600"
          title="Copy code"
        >
          {copied
            ? <Check className="w-3.5 h-3.5 text-green-400" />
            : <Copy className="w-3.5 h-3.5 text-gray-400" />}
        </button>
      </div>
    </InCodeBlock.Provider>
  )
}

function MarkdownCode({ className, children }: { className?: string; children?: React.ReactNode }) {
  const inBlock = useContext(InCodeBlock)
  const lang = className?.replace('language-', '') ?? ''
  const colorClass =
    lang === 'bash' || lang === 'sh' || lang === 'shell' ? 'text-yellow-300' :
    lang === 'json' ? 'text-cyan-300' :
    lang === 'yaml' || lang === 'yml' ? 'text-orange-300' :
    lang === 'go' ? 'text-blue-300' :
    'text-green-300'
  return inBlock ? (
    <code className={`text-xs ${colorClass} font-mono ${className ?? ''}`}>{children}</code>
  ) : (
    <code className="bg-gray-900 text-green-300 px-1 py-0.5 rounded text-xs font-mono">{children}</code>
  )
}

// streamRCA opens an SSE connection to /rca/stream and calls callbacks for
// each phase update, the completed report, and finally the done signal.
// abortSignal may be null (manual regenerate — no AbortController needed).
function streamRCA(
  id: string,
  abortSignal: AbortSignal | null,
  onPhase: (phase: string) => void,
  onComplete: (report: RCAReport) => void,
  onDone: () => void,
) {
  const url = `${getApiUrl()}/api/v1/incidents/${id}/rca/stream`
  const es = new EventSource(url)
  let closed = false

  const cleanup = () => {
    if (closed) return
    closed = true
    es.close()
    onDone()
  }

  es.addEventListener('running', (e: MessageEvent) => {
    try { onPhase((JSON.parse(e.data) as { phase: string }).phase) } catch {}
  })
  es.addEventListener('complete', (e: MessageEvent) => {
    try { onComplete(JSON.parse(e.data) as RCAReport) } catch {}
    cleanup()
  })
  es.addEventListener('error', () => cleanup())

  if (abortSignal) abortSignal.addEventListener('abort', () => cleanup())
}

// Module-level constant — not recreated on every render.
const markdownComponents = {
  pre: MarkdownPre,
  code: MarkdownCode,
  p: ({ children }: { children?: React.ReactNode }) => <p className="mb-1 last:mb-0">{children}</p>,
  ul: ({ children }: { children?: React.ReactNode }) => <ul className="list-disc list-inside space-y-0.5 mb-1">{children}</ul>,
  ol: ({ children }: { children?: React.ReactNode }) => <ol className="list-decimal list-inside space-y-0.5 mb-1">{children}</ol>,
  li: ({ children }: { children?: React.ReactNode }) => <li className="text-gray-300">{children}</li>,
  strong: ({ children }: { children?: React.ReactNode }) => <strong className="text-white font-semibold">{children}</strong>,
  h3: ({ children }: { children?: React.ReactNode }) => <h3 className="text-white font-semibold text-sm mt-2 mb-1">{children}</h3>,
}

export default function IncidentDetailPage() {
  const params = useParams()
  const id = params.id as string
  const [incident, setIncident] = useState<Incident | null>(null)
  const [loading, setLoading] = useState(true)
  const [regenerating, setRegenerating] = useState(false)
  const [rcaPhase, setRcaPhase] = useState('')
  const [question, setQuestion] = useState('')
  const [chatHistory, setChatHistory] = useState<ChatMessage[]>([])
  const [asking, setAsking] = useState(false)
  const [confirmClear, setConfirmClear] = useState(false)
  const chatEndRef = useRef<HTMLDivElement>(null)
  // Stored so we can clearTimeout on unmount (prevents setState after unmount).
  const scrollTimeoutRef = useRef<ReturnType<typeof setTimeout>>()

  // Restore chat history from localStorage keyed by incident id.
  useEffect(() => {
    try {
      const stored = localStorage.getItem(`incident-chat-${id}`)
      if (stored) {
        const parsed = JSON.parse(stored) as ChatMessage[]
        if (Array.isArray(parsed) && parsed.length > 0) setChatHistory(parsed)
      }
    } catch {}
  }, [id])

  // Persist chat history; cap at 50 messages to bound localStorage growth.
  useEffect(() => {
    if (chatHistory.length === 0) return
    try {
      localStorage.setItem(`incident-chat-${id}`, JSON.stringify(chatHistory.slice(-50)))
    } catch {}
  }, [chatHistory, id])

  useEffect(() => {
    const ac = new AbortController()
    setLoading(true)
    apiFetch<Incident>(`/api/v1/incidents/${id}`)
      .then(data => {
        if (ac.signal.aborted) return
        setIncident(data)
        // Auto-run RCA via SSE stream if none exists yet.
        if (!data.rcaReport) {
          setRegenerating(true)
          streamRCA(id, ac.signal,
            phase => { if (!ac.signal.aborted) setRcaPhase(phase) },
            report => { if (!ac.signal.aborted) setIncident(prev => prev ? { ...prev, rcaReport: report } : null) },
            () => { if (!ac.signal.aborted) setRegenerating(false) },
          )
        }
      })
      .catch(() => {})
      .finally(() => { if (!ac.signal.aborted) setLoading(false) })
    return () => { ac.abort(); clearTimeout(scrollTimeoutRef.current) }
  }, [id])

  const regenerateRCA = () => {
    setRegenerating(true)
    setRcaPhase('')
    streamRCA(id, null,
      phase => setRcaPhase(phase),
      report => setIncident(prev => prev ? { ...prev, rcaReport: report, status: 'investigating' } : null),
      () => setRegenerating(false),
    )
  }

  const askAI = async () => {
    if (asking) return
    const q = question.trim()
    if (!q) return
    setQuestion('')
    // Snapshot history BEFORE setState so the outgoing request includes
    // all prior turns (reading chatHistory after setChatHistory would be
    // a stale closure — the state update is async).
    const historySnapshot = chatHistory.map(m => ({ role: m.role, content: m.content }))
    const userMsg: ChatMessage = { id: `${Date.now()}-u`, role: 'user', content: q }
    setChatHistory(prev => [...prev, userMsg])
    setAsking(true)
    scrollTimeoutRef.current = setTimeout(() => chatEndRef.current?.scrollIntoView({ behavior: 'smooth' }), 50)
    try {
      const res = await fetch(`${getApiUrl()}/api/v1/llm/ask`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          question: q,
          incidentId: parseInt(id, 10),
          // history and context are forwarded for when the backend is
          // wired to accept them (currently handleAsk only reads question
          // + incidentId — backend needs updating to consume these).
          history: historySnapshot,
          context: {
            summary: incident?.summary,
            severity: incident?.severity,
            status: incident?.status,
          },
        }),
      })
      if (!res.ok) throw new Error(`${res.status}`)
      const data = await res.json()
      const answer = data.answer || 'No response from AI'
      setChatHistory(prev => [...prev, { id: `${Date.now()}-a`, role: 'assistant', content: answer }])
    } catch {
      setChatHistory(prev => [...prev, { id: `${Date.now()}-e`, role: 'assistant', content: 'Failed to reach AI — check LLM configuration in Settings.' }])
    } finally {
      setAsking(false)
      scrollTimeoutRef.current = setTimeout(() => chatEndRef.current?.scrollIntoView({ behavior: 'smooth' }), 50)
    }
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
                {[sig.namespace, sig.pod || sig.service].filter(Boolean).join('/') || 'cluster-wide'}
                {' '}&bull;{' '}
                {new Date(sig.timestamp).toLocaleTimeString()}
              </div>
              {sig.details && (
                <p className="text-xs text-gray-400 mt-1 leading-relaxed">{sig.details}</p>
              )}
            </div>
          </div>
        ))}
      </div>

      {/* RCA in progress */}
      {!rca && regenerating && (
        <div className="flex items-center gap-3 p-4 bg-purple-900/10 border border-purple-700/30 rounded-lg mb-6">
          <Loader2 className="w-4 h-4 animate-spin text-purple-400 shrink-0" />
          <div>
            <p className="text-sm text-purple-300 font-medium">Running Root Cause Analysis…</p>
            <p className="text-xs text-gray-500 mt-0.5">
              {rcaPhase || 'AI is analysing signals and correlating cluster state'}
            </p>
          </div>
        </div>
      )}

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

      {/* Ask AI — multi-turn chat */}
      <div className="mt-8 pt-6 border-t border-gray-700">
        <div className="flex items-center justify-between mb-3">
          <h3 className="text-sm font-medium text-gray-400 flex items-center gap-2">
            <Bot className="w-4 h-4 text-blue-400" /> Ask AI
          </h3>
          {chatHistory.length > 0 && !confirmClear && (
            <button
              onClick={() => setConfirmClear(true)}
              className="text-xs text-gray-600 hover:text-gray-400 transition-colors"
            >
              Clear history
            </button>
          )}
          {confirmClear && (
            <span className="flex items-center gap-2 text-xs">
              <span className="text-gray-500">Clear all history?</span>
              <button onClick={() => { setChatHistory([]); localStorage.removeItem(`incident-chat-${id}`); setConfirmClear(false) }} className="text-red-400 hover:text-red-300 transition-colors">Yes</button>
              <button onClick={() => setConfirmClear(false)} className="text-gray-500 hover:text-gray-400 transition-colors">No</button>
            </span>
          )}
        </div>

        {/* Chat history */}
        {chatHistory.length > 0 && (
          <div
            role="log"
            aria-live="polite"
            aria-label="Chat history"
            className="space-y-3 mb-4 max-h-[480px] overflow-y-auto pr-1"
          >
            {chatHistory.map(msg => (
              <div key={msg.id} className={`flex gap-2.5 ${msg.role === 'user' ? 'flex-row-reverse' : ''}`}>
                <span className={`mt-0.5 w-6 h-6 rounded-full shrink-0 flex items-center justify-center ${
                  msg.role === 'user' ? 'bg-blue-600' : 'bg-purple-900/60 border border-purple-700/40'
                }`}>
                  {msg.role === 'user'
                    ? <User className="w-3.5 h-3.5 text-white" />
                    : <Bot className="w-3.5 h-3.5 text-purple-300" />}
                </span>
                <div className={`flex-1 min-w-0 rounded-lg px-3 py-2.5 text-sm ${
                  msg.role === 'user'
                    ? 'bg-blue-600/20 border border-blue-700/30 text-blue-100'
                    : 'bg-gray-800/60 border border-gray-700/50 text-gray-200'
                }`}>
                  {msg.role === 'assistant' ? (
                    <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents}>
                      {msg.content}
                    </ReactMarkdown>
                  ) : (
                    <span>{msg.content}</span>
                  )}
                </div>
              </div>
            ))}
            {asking && (
              <div className="flex gap-2.5">
                <span className="mt-0.5 w-6 h-6 rounded-full shrink-0 flex items-center justify-center bg-purple-900/60 border border-purple-700/40">
                  <Bot className="w-3.5 h-3.5 text-purple-300" />
                </span>
                <div className="flex items-center gap-1.5 px-3 py-2.5 bg-gray-800/60 border border-gray-700/50 rounded-lg">
                  <span className="w-1.5 h-1.5 bg-gray-500 rounded-full animate-bounce [animation-delay:0ms]" />
                  <span className="w-1.5 h-1.5 bg-gray-500 rounded-full animate-bounce [animation-delay:150ms]" />
                  <span className="w-1.5 h-1.5 bg-gray-500 rounded-full animate-bounce [animation-delay:300ms]" />
                </div>
              </div>
            )}
            <div ref={chatEndRef} />
          </div>
        )}

        {/* Input */}
        <div className="flex gap-2">
          <input
            type="text"
            value={question}
            onChange={e => setQuestion(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && !e.shiftKey && !asking && askAI()}
            disabled={asking}
            aria-label="Ask AI a question about this incident"
            placeholder={chatHistory.length === 0 ? 'Ask about this incident — e.g. "What is the affected pod?"' : 'Follow up…'}
            className="flex-1 bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white placeholder-gray-500 focus:outline-none focus:border-blue-600 disabled:opacity-60"
          />
          <button
            onClick={askAI}
            disabled={asking || !question.trim()}
            className="px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg text-sm disabled:opacity-50 transition-colors"
          >
            {asking ? <Loader2 className="w-4 h-4 animate-spin" /> : <Send className="w-4 h-4" />}
          </button>
        </div>
        <p className="text-xs text-gray-600 mt-2">
          AI answers using incident signals{incident?.rcaReport ? ', RCA report' : ''} and cluster context.
        </p>
      </div>
    </div>
  )
}

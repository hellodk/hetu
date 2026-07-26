'use client'

import { useRef, useState, useEffect } from 'react'
import { MessageSquare, X, Send, Square, Wrench } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { streamChat, type ChatToolEvent } from '@/lib/chat'

interface ChatTurn {
  role: 'user' | 'assistant'
  content: string
  tools?: ChatToolEvent[]
  error?: string
}

/**
 * Floating agentic-chat widget wired to the analyzer's POST /api/v1/chat SSE.
 * Mounted once in the app shell so it is available on every page.
 */
export function ChatWidget() {
  const [open, setOpen] = useState(false)
  const [input, setInput] = useState('')
  const [turns, setTurns] = useState<ChatTurn[]>([])
  const [streaming, setStreaming] = useState(false)
  const convIdRef = useRef<string | null>(null)
  const abortRef = useRef<AbortController | null>(null)
  const endRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [turns, streaming])

  // Cancel any in-flight stream on unmount.
  useEffect(() => () => abortRef.current?.abort(), [])

  const stop = () => {
    abortRef.current?.abort()
    abortRef.current = null
    setStreaming(false)
  }

  const send = async () => {
    const message = input.trim()
    if (!message || streaming) return
    setInput('')

    // Push the user turn and an empty assistant turn we stream into.
    setTurns((prev) => [
      ...prev,
      { role: 'user', content: message },
      { role: 'assistant', content: '', tools: [] },
    ])
    setStreaming(true)

    const controller = new AbortController()
    abortRef.current = controller

    const appendAssistant = (patch: (t: ChatTurn) => ChatTurn) => {
      setTurns((prev) => {
        const next = [...prev]
        for (let i = next.length - 1; i >= 0; i--) {
          if (next[i].role === 'assistant') {
            next[i] = patch(next[i])
            break
          }
        }
        return next
      })
    }

    try {
      await streamChat(
        message,
        convIdRef.current,
        {
          onConversation: (id) => {
            if (id) convIdRef.current = id
          },
          onTool: (tool) =>
            appendAssistant((t) => ({ ...t, tools: [...(t.tools ?? []), tool] })),
          onToken: (text) =>
            appendAssistant((t) => ({ ...t, content: t.content + text })),
          onError: (msg) => appendAssistant((t) => ({ ...t, error: msg })),
          onDone: (id) => {
            if (id) convIdRef.current = id
          },
        },
        controller.signal,
      )
    } catch (err) {
      if (!(err instanceof DOMException && err.name === 'AbortError')) {
        appendAssistant((t) => ({
          ...t,
          error: 'Failed to reach the assistant — check LLM configuration in Settings.',
        }))
      }
    } finally {
      setStreaming(false)
      abortRef.current = null
    }
  }

  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      send()
    }
  }

  if (!open) {
    return (
      <button
        onClick={() => setOpen(true)}
        data-testid="chat-launcher"
        aria-label="Open AI assistant"
        className="fixed bottom-5 right-5 z-40 w-12 h-12 rounded-full bg-blue-600 hover:bg-blue-700 text-white shadow-lg flex items-center justify-center transition-colors"
      >
        <MessageSquare className="w-5 h-5" aria-hidden="true" />
      </button>
    )
  }

  return (
    <div
      data-testid="chat-widget"
      role="dialog"
      aria-modal="false"
      aria-label="AI assistant"
      className="fixed bottom-5 right-5 z-40 flex flex-col w-[min(24rem,calc(100vw-2.5rem))] h-[min(600px,calc(100vh-2.5rem))] bg-cluster-card text-cluster-text border border-cluster-border rounded-lg shadow-xl overflow-hidden"
    >
      {/* Header */}
      <header className="flex items-center gap-2 px-4 py-2.5 border-b border-cluster-border bg-cluster-card">
        <MessageSquare className="w-4 h-4 text-blue-500" aria-hidden="true" />
        <h2 className="text-sm font-semibold text-cluster-text flex-1">Cluster Assistant</h2>
        <button
          onClick={() => setOpen(false)}
          aria-label="Close assistant"
          className="p-1 rounded hover:bg-cluster-bg text-cluster-muted hover:text-cluster-text transition-colors"
        >
          <X className="w-4 h-4" aria-hidden="true" />
        </button>
      </header>

      {/* Messages */}
      <div className="flex-1 overflow-y-auto px-3 py-3 space-y-3">
        {turns.length === 0 && (
          <p className="text-xs text-cluster-muted px-1 py-4 text-center">
            Ask about cluster health, incidents, pods, metrics, security findings, or recommendations.
          </p>
        )}
        {turns.map((t, i) =>
          t.role === 'user' ? (
            <div key={i} className="flex justify-end">
              <div className="max-w-[85%] rounded-lg bg-blue-600 text-white px-3 py-1.5 text-sm whitespace-pre-wrap break-words">
                {t.content}
              </div>
            </div>
          ) : (
            <div key={i} data-testid="chat-message-assistant" className="flex flex-col gap-1.5">
              {t.tools && t.tools.length > 0 && (
                <div className="flex flex-wrap gap-1">
                  {t.tools.map((tool, j) => (
                    <span
                      key={j}
                      data-testid="chat-tool-chip"
                      className="inline-flex items-center gap-1 rounded-full border border-cluster-border bg-cluster-bg px-2 py-0.5 text-[11px] font-medium text-cluster-muted"
                    >
                      <Wrench className="w-3 h-3" aria-hidden="true" />
                      {tool.name}
                    </span>
                  ))}
                </div>
              )}
              <div className="max-w-[92%] rounded-lg bg-cluster-bg border border-cluster-border px-3 py-2 text-sm text-cluster-text prose-chat break-words">
                {t.content ? (
                  <ReactMarkdown remarkPlugins={[remarkGfm]}>{t.content}</ReactMarkdown>
                ) : (
                  !t.error && <span className="text-cluster-muted">…</span>
                )}
                {t.error && (
                  <p className="mt-1 text-xs text-[color:var(--sev-crit,#DC2626)]">{t.error}</p>
                )}
              </div>
            </div>
          ),
        )}
        <div ref={endRef} />
      </div>

      {/* Input */}
      <div className="border-t border-cluster-border p-2 flex items-end gap-2">
        <textarea
          data-testid="chat-input"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={onKeyDown}
          rows={1}
          placeholder="Ask the cluster assistant…"
          aria-label="Message"
          className="flex-1 resize-none rounded-md border border-cluster-border bg-cluster-bg px-2.5 py-1.5 text-sm text-cluster-text placeholder:text-cluster-muted focus:outline-none focus:ring-2 focus:ring-blue-500 max-h-32"
        />
        {streaming ? (
          <button
            onClick={stop}
            data-testid="chat-stop"
            aria-label="Stop"
            className="flex-shrink-0 w-9 h-9 rounded-md bg-cluster-bg border border-cluster-border text-cluster-text hover:bg-cluster-card flex items-center justify-center transition-colors"
          >
            <Square className="w-4 h-4" aria-hidden="true" />
          </button>
        ) : (
          <button
            onClick={send}
            disabled={!input.trim()}
            data-testid="chat-send"
            aria-label="Send"
            className="flex-shrink-0 w-9 h-9 rounded-md bg-blue-600 hover:bg-blue-700 disabled:opacity-40 text-white flex items-center justify-center transition-colors"
          >
            <Send className="w-4 h-4" aria-hidden="true" />
          </button>
        )}
      </div>
    </div>
  )
}

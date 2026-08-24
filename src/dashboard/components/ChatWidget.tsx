'use client'

import { useRef, useState, useEffect } from 'react'
import { MessageSquare, X, Send, Square, Wrench, BookMarked } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { streamChat, type ChatToolEvent } from '@/lib/chat'

// Shape emitted by the analyzer's SSE `citation` frames (analyzer/chat_tools.go).
interface ChatCitation {
  kind: string
  ref: string
  title: string
  snippet?: string
}

interface ChatTurn {
  role: 'user' | 'assistant'
  content: string
  tools?: ChatToolEvent[]
  citations?: ChatCitation[]
  error?: string
}

/**
 * Floating agentic-chat widget wired to the analyzer's POST /api/v1/chat SSE.
 * Mounted once in the app shell so it is available on every page.
 * Glass styling per issue #14 (light-first, token-driven).
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

  // Typing indicator shows while the turn has started but produced no
  // visible content yet (no tokens, no tool chips).
  const last = turns[turns.length - 1]
  const showTyping =
    streaming && last?.role === 'assistant' && !last.content && !(last.tools && last.tools.length > 0)

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
          onCitation: (raw) => {
            const c = raw as ChatCitation | undefined
            if (!c || !c.ref) return
            appendAssistant((t) => ({
              ...t,
              citations: [...(t.citations ?? []), c],
            }))
          },
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
        className="fixed bottom-5 right-5 z-40 w-12 h-12 rounded-full bg-gradient-to-br from-[rgb(var(--accent))] to-[rgb(var(--accent-soft))] text-white shadow-lg flex items-center justify-center transition-transform hover:scale-105"
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
      className="fixed bottom-5 right-5 z-40 flex flex-col w-[min(24rem,calc(100vw-2.5rem))] h-[min(600px,calc(100vh-2.5rem))] rounded-2xl overflow-hidden border border-cluster-border/70 shadow-xl glass-panel bg-cluster-card/70"
    >
      {/* Header */}
      <header className="flex items-center gap-2 px-4 py-2.5 border-b border-cluster-border/60 bg-cluster-card/40">
        <span className="w-6 h-6 rounded-lg bg-gradient-to-br from-[rgb(var(--accent))] to-[rgb(var(--accent-soft))] flex items-center justify-center text-white text-xs">
          ✦
        </span>
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
      <div role="log" aria-live="polite" className="flex-1 overflow-y-auto px-3 py-3 space-y-3">
        {turns.length === 0 && (
          <p className="text-xs text-cluster-muted px-1 py-4 text-center">
            Ask about cluster health, incidents, pods, metrics, security findings, or recommendations.
          </p>
        )}
        {turns.map((t, i) =>
          t.role === 'user' ? (
            <div key={i} className="flex justify-end">
              <div className="msg-rise max-w-[85%] rounded-xl rounded-br-sm bg-gradient-to-br from-[rgb(var(--accent))] to-[rgb(var(--accent-soft))] text-white px-3 py-1.5 text-sm whitespace-pre-wrap break-words shadow-sm">
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
                      className="inline-flex items-center gap-1 rounded-full border border-cluster-border/80 bg-cluster-bg/60 backdrop-blur px-2 py-0.5 text-[11px] font-medium text-cluster-muted msg-rise"
                    >
                      <Wrench className="w-3 h-3" aria-hidden="true" />
                      {tool.name}
                    </span>
                  ))}
                </div>
              )}
              <div className="msg-rise max-w-[92%] rounded-xl rounded-bl-sm bg-cluster-bg/70 border border-cluster-border/60 px-3 py-2 text-sm text-cluster-text prose-chat break-words shadow-sm">
                {t.content ? (
                  <ReactMarkdown remarkPlugins={[remarkGfm]}>{t.content}</ReactMarkdown>
                ) : null}
                {t.error && (
                  <p className="mt-1 text-xs text-[color:rgb(var(--sev-crit))]">{t.error}</p>
                )}
              </div>
              {t.citations && t.citations.length > 0 && (
                <div className="flex flex-wrap gap-1">
                  {t.citations.map((c, j) => (
                    <span
                      key={j}
                      data-testid="chat-citation-chip"
                      title={c.ref}
                      className="inline-flex items-center gap-1 rounded-full border border-cluster-border/80 bg-cluster-bg/60 px-2 py-0.5 text-[11px] font-medium text-cluster-muted msg-rise"
                    >
                      <BookMarked className="w-3 h-3" aria-hidden="true" />
                      {c.title || c.ref}
                    </span>
                  ))}
                </div>
              )}
            </div>
          ),
        )}
        {showTyping && (
          <div
            data-testid="chat-typing"
            className="inline-flex items-center gap-1 rounded-xl rounded-bl-sm bg-cluster-bg/70 border border-cluster-border/60 px-3 py-2.5"
          >
            {[0, 1, 2].map((j) => (
              <i
                key={j}
                style={{ animation: `typing-bounce 1.2s ${j * 0.15}s infinite` }}
                className="w-1.5 h-1.5 rounded-full bg-[rgb(var(--accent-soft))]"
              />
            ))}
          </div>
        )}
        <div ref={endRef} />
      </div>

      {/* Input */}
      <div className="border-t border-cluster-border/60 p-2 flex items-end gap-2 bg-cluster-card/40">
        <textarea
          data-testid="chat-input"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={onKeyDown}
          rows={1}
          placeholder="Ask the cluster assistant…"
          aria-label="Message"
          className="flex-1 resize-none rounded-md border border-cluster-border/70 bg-cluster-bg/70 px-2.5 py-1.5 text-sm text-cluster-text placeholder:text-cluster-muted focus:outline-none focus:ring-2 focus:ring-[rgb(var(--accent)/0.35)] max-h-32"
        />
        {streaming ? (
          <button
            onClick={stop}
            data-testid="chat-stop"
            aria-label="Stop"
            className="flex-shrink-0 w-9 h-9 rounded-md bg-cluster-bg/70 border border-cluster-border/70 text-cluster-text hover:bg-cluster-card flex items-center justify-center transition-colors"
          >
            <Square className="w-4 h-4" aria-hidden="true" />
          </button>
        ) : (
          <button
            onClick={send}
            disabled={!input.trim()}
            data-testid="chat-send"
            aria-label="Send"
            className="flex-shrink-0 w-9 h-9 rounded-md bg-gradient-to-br from-[rgb(var(--accent))] to-[rgb(var(--accent-soft))] disabled:opacity-40 text-white flex items-center justify-center transition-transform hover:scale-105"
          >
            <Send className="w-4 h-4" aria-hidden="true" />
          </button>
        )}
      </div>
    </div>
  )
}

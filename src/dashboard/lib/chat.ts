import { getApiUrl } from './api'

// SSE client for the analyzer's agentic chat (POST /api/v1/chat).
// The endpoint streams `data: {json}\n\n` frames; event shapes:
//   {type:"conversation", conversationId}
//   {type:"tool", name, args}
//   {type:"citation", citation}
//   {type:"token", content}
//   {type:"error", message}
//   {type:"done", conversationId}

export interface ChatToolEvent {
  name: string
  args?: Record<string, unknown>
}

export interface ChatHandlers {
  onConversation?: (id: string) => void
  onTool?: (tool: ChatToolEvent) => void
  onCitation?: (citation: unknown) => void
  onToken?: (text: string) => void
  onError?: (message: string) => void
  onDone?: (id?: string) => void
}

/**
 * Streams a chat turn. Resolves when the stream ends (`done` or EOF); rejects on
 * a transport/HTTP error. Pass an AbortSignal to cancel an in-flight turn.
 */
export async function streamChat(
  message: string,
  conversationId: string | null,
  handlers: ChatHandlers,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch(`${getApiUrl()}/api/v1/chat`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      message,
      conversationId: conversationId ?? undefined,
    }),
    signal,
  })

  if (!res.ok || !res.body) {
    throw new Error(`chat request failed: ${res.status}`)
  }

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  for (;;) {
    const { value, done } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })

    let sep: number
    while ((sep = buffer.indexOf('\n\n')) !== -1) {
      const frame = buffer.slice(0, sep)
      buffer = buffer.slice(sep + 2)

      const dataLine = frame.split('\n').find((l) => l.startsWith('data:'))
      if (!dataLine) continue
      const json = dataLine.slice(5).trim()
      if (!json) continue

      let ev: { type?: string; [k: string]: unknown }
      try {
        ev = JSON.parse(json)
      } catch {
        continue
      }

      switch (ev.type) {
        case 'conversation':
          handlers.onConversation?.(String(ev.conversationId ?? ''))
          break
        case 'tool':
          handlers.onTool?.({
            name: String(ev.name ?? ''),
            args: ev.args as Record<string, unknown> | undefined,
          })
          break
        case 'citation':
          handlers.onCitation?.(ev.citation)
          break
        case 'token':
          handlers.onToken?.(String(ev.content ?? ''))
          break
        case 'error':
          handlers.onError?.(String(ev.message ?? 'chat error'))
          break
        case 'done':
          handlers.onDone?.(ev.conversationId ? String(ev.conversationId) : undefined)
          break
      }
    }
  }
}

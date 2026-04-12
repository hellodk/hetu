'use client'

import { useEffect, useRef, useState, useCallback } from 'react'
import { getApiUrl } from '@/lib/api'
import { Play, Pause, Trash2, Download, ArrowDown } from 'lucide-react'

interface Props {
  namespace: string
  podName: string
  containers: string[]
}

export function PodLogViewer({ namespace, podName, containers }: Props) {
  const [lines, setLines] = useState<string[]>([])
  const [container, setContainer] = useState(containers[0] || '')
  const [following, setFollowing] = useState(true)
  const [connected, setConnected] = useState(false)
  const wsRef = useRef<WebSocket | null>(null)
  const bottomRef = useRef<HTMLDivElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)

  const connect = useCallback(() => {
    if (wsRef.current) {
      wsRef.current.close()
    }

    const base = getApiUrl().replace(/^http/, 'ws')
    const url = `${base}/api/v1/k8s/pods/${namespace}/${podName}/logs?container=${container}&follow=true&tail=200`
    const ws = new WebSocket(url)

    ws.onopen = () => setConnected(true)
    ws.onclose = () => setConnected(false)
    ws.onerror = () => setConnected(false)
    ws.onmessage = (e) => {
      setLines(prev => {
        const next = [...prev, e.data]
        // Keep last 5000 lines to prevent memory bloat
        if (next.length > 5000) return next.slice(-5000)
        return next
      })
    }

    wsRef.current = ws
  }, [namespace, podName, container])

  useEffect(() => {
    connect()
    return () => {
      wsRef.current?.close()
    }
  }, [connect])

  useEffect(() => {
    if (following && bottomRef.current) {
      bottomRef.current.scrollIntoView({ behavior: 'auto' })
    }
  }, [lines, following])

  const handleScroll = () => {
    if (!containerRef.current) return
    const { scrollTop, scrollHeight, clientHeight } = containerRef.current
    // Auto-disable follow when user scrolls up
    if (scrollHeight - scrollTop - clientHeight > 100) {
      setFollowing(false)
    }
  }

  const scrollToBottom = () => {
    setFollowing(true)
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }

  const downloadLogs = () => {
    const blob = new Blob([lines.join('\n')], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${podName}-${container}.log`
    a.click()
    URL.revokeObjectURL(url)
  }

  return (
    <div className="flex flex-col h-[60vh]">
      {/* Controls */}
      <div className="flex items-center gap-3 mb-2">
        {containers.length > 1 && (
          <select
            value={container}
            onChange={e => { setContainer(e.target.value); setLines([]) }}
            className="bg-gray-800 border border-gray-700 rounded px-2 py-1 text-sm text-white"
          >
            {containers.map(c => <option key={c} value={c}>{c}</option>)}
          </select>
        )}
        <span className={`w-2 h-2 rounded-full ${connected ? 'bg-green-400' : 'bg-red-400'}`} />
        <span className="text-xs text-gray-500">{connected ? 'Streaming' : 'Disconnected'}</span>
        <span className="flex-1" />
        <span className="text-xs text-gray-500">{lines.length} lines</span>
        <button onClick={() => setLines([])} className="p-1.5 text-gray-400 hover:text-white hover:bg-white/10 rounded" title="Clear">
          <Trash2 className="w-3.5 h-3.5" />
        </button>
        <button onClick={downloadLogs} className="p-1.5 text-gray-400 hover:text-white hover:bg-white/10 rounded" title="Download">
          <Download className="w-3.5 h-3.5" />
        </button>
        {!following && (
          <button onClick={scrollToBottom} className="flex items-center gap-1 px-2 py-1 text-xs bg-blue-600 text-white rounded hover:bg-blue-500">
            <ArrowDown className="w-3 h-3" /> Follow
          </button>
        )}
      </div>

      {/* Log output */}
      <div
        ref={containerRef}
        onScroll={handleScroll}
        className="flex-1 bg-gray-950 border border-gray-700 rounded-lg font-mono text-xs leading-5 overflow-auto p-3"
      >
        {lines.map((line, i) => (
          <div key={i} className="text-gray-300 whitespace-pre-wrap break-all hover:bg-white/5">
            {line}
          </div>
        ))}
        <div ref={bottomRef} />
      </div>
    </div>
  )
}

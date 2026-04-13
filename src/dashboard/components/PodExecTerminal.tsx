'use client'

import { useEffect, useRef, useState } from 'react'
import { getWsUrl } from '@/lib/api'

interface Props {
  namespace: string
  podName: string
  containers: string[]
}

export function PodExecTerminal({ namespace, podName, containers }: Props) {
  const termRef = useRef<HTMLDivElement>(null)
  const [container, setContainer] = useState(containers[0] || '')
  const [command, setCommand] = useState('/bin/sh')
  const [connected, setConnected] = useState(false)
  const [started, setStarted] = useState(false)
  const wsRef = useRef<WebSocket | null>(null)
  const terminalRef = useRef<any>(null)

  const startExec = async () => {
    if (!termRef.current) return

    // Dynamically import xterm to avoid SSR issues
    const { Terminal } = await import('@xterm/xterm')
    const { FitAddon } = await import('@xterm/addon-fit')

    // Load xterm CSS via link element (dynamic CSS import not supported in Next.js)
    if (!document.querySelector('link[href*="xterm.css"]')) {
      const link = document.createElement('link')
      link.rel = 'stylesheet'
      link.href = '/_next/static/css/xterm.css'
      // Inline the critical xterm styles as fallback
      const style = document.createElement('style')
      style.textContent = `.xterm { position: relative; user-select: none; } .xterm-viewport { overflow-y: scroll; }`
      document.head.appendChild(style)
    }

    const term = new Terminal({
      cursorBlink: true,
      fontFamily: 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace',
      fontSize: 13,
      theme: {
        background: '#0a0a0a',
        foreground: '#e5e5e5',
        cursor: '#60a5fa',
      },
    })

    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.open(termRef.current)
    fitAddon.fit()

    terminalRef.current = term

    // Connect WebSocket
    const base = getWsUrl()
    const url = `${base}/api/v1/k8s/pods/${namespace}/${podName}/exec?container=${container}&command=${encodeURIComponent(command)}`
    const ws = new WebSocket(url)
    ws.binaryType = 'arraybuffer'

    ws.onopen = () => {
      setConnected(true)
      term.writeln(`\x1b[32mConnected to ${podName}/${container}\x1b[0m`)
      term.writeln(`\x1b[90m$ ${command}\x1b[0m`)
      term.writeln('')
    }

    ws.onmessage = (e) => {
      if (e.data instanceof ArrayBuffer) {
        term.write(new Uint8Array(e.data))
      } else {
        term.write(e.data)
      }
    }

    ws.onclose = () => {
      setConnected(false)
      term.writeln('')
      term.writeln('\x1b[31mConnection closed\x1b[0m')
    }

    ws.onerror = () => {
      setConnected(false)
      term.writeln('\x1b[31mConnection error\x1b[0m')
    }

    // Send keystrokes to WS
    term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(data)
      }
    })

    wsRef.current = ws
    setStarted(true)

    // Handle resize
    const resizeObserver = new ResizeObserver(() => fitAddon.fit())
    resizeObserver.observe(termRef.current)

    return () => {
      resizeObserver.disconnect()
    }
  }

  useEffect(() => {
    return () => {
      wsRef.current?.close()
      terminalRef.current?.dispose()
    }
  }, [])

  if (!started) {
    return (
      <div className="space-y-4">
        <div className="flex items-center gap-3">
          {containers.length > 1 && (
            <select
              value={container}
              onChange={e => setContainer(e.target.value)}
              className="bg-gray-800 border border-gray-700 rounded px-2 py-1 text-sm text-white"
            >
              {containers.map(c => <option key={c} value={c}>{c}</option>)}
            </select>
          )}
          <select
            value={command}
            onChange={e => setCommand(e.target.value)}
            className="bg-gray-800 border border-gray-700 rounded px-2 py-1 text-sm text-white"
          >
            <option value="/bin/sh">/bin/sh</option>
            <option value="/bin/bash">/bin/bash</option>
            <option value="/bin/ash">/bin/ash</option>
          </select>
          <button
            onClick={startExec}
            className="px-4 py-1.5 bg-blue-600 hover:bg-blue-500 text-white text-sm rounded transition-colors"
          >
            Connect
          </button>
        </div>
        <div className="bg-gray-900 border border-gray-700 rounded-lg h-[50vh] flex items-center justify-center text-gray-500 text-sm">
          Click "Connect" to start a terminal session
        </div>
      </div>
    )
  }

  return (
    <div>
      <div className="flex items-center gap-2 mb-2">
        <span className={`w-2 h-2 rounded-full ${connected ? 'bg-green-400' : 'bg-red-400'}`} />
        <span className="text-xs text-gray-500">
          {connected ? `Connected — ${container} (${command})` : 'Disconnected'}
        </span>
      </div>
      <div ref={termRef} className="h-[50vh] rounded-lg overflow-hidden" />
    </div>
  )
}

'use client'

import { useState } from 'react'
import { getApiUrl } from '@/lib/api'
import { Scale, RefreshCw, Trash2, X, AlertTriangle } from 'lucide-react'

interface Props {
  kind: string
  group: string
  version: string
  namespace: string
  name: string
  currentReplicas?: number
  onAction?: () => void
}

export function WorkloadActions({ kind, group, version, namespace, name, currentReplicas, onAction }: Props) {
  const [modal, setModal] = useState<'scale' | 'restart' | 'delete' | null>(null)
  const [replicas, setReplicas] = useState(currentReplicas ?? 1)
  const [confirmName, setConfirmName] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const canScale = ['deployments', 'statefulsets', 'replicasets'].includes(kind)
  const canRestart = ['deployments'].includes(kind)

  const doAction = async (action: string, body: any) => {
    setLoading(true)
    setError(null)
    try {
      const res = await fetch(`${getApiUrl()}/api/v1/k8s/actions/${action}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ kind, group, version, namespace, name, ...body }),
      })
      if (!res.ok) {
        const text = await res.text()
        throw new Error(text)
      }
      setModal(null)
      onAction?.()
    } catch (e: any) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }

  return (
    <>
      <div className="flex items-center gap-2">
        {canScale && (
          <button
            onClick={() => setModal('scale')}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-gray-700 hover:bg-gray-600 text-white rounded transition-colors"
          >
            <Scale className="w-3.5 h-3.5" /> Scale
          </button>
        )}
        {canRestart && (
          <button
            onClick={() => setModal('restart')}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-gray-700 hover:bg-gray-600 text-white rounded transition-colors"
          >
            <RefreshCw className="w-3.5 h-3.5" /> Restart
          </button>
        )}
        <button
          onClick={() => setModal('delete')}
          className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-red-900/50 hover:bg-red-800/50 text-red-300 rounded transition-colors"
        >
          <Trash2 className="w-3.5 h-3.5" /> Delete
        </button>
      </div>

      {/* Scale modal */}
      {modal === 'scale' && (
        <Modal onClose={() => setModal(null)} title="Scale Deployment">
          <div className="space-y-4">
            <div>
              <label className="block text-sm text-gray-400 mb-1">Replicas</label>
              <input
                type="number"
                min={0}
                max={100}
                value={replicas}
                onChange={e => setReplicas(parseInt(e.target.value) || 0)}
                className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white"
              />
            </div>
            {error && <div className="text-sm text-red-400">{error}</div>}
            <div className="flex justify-end gap-2">
              <button onClick={() => setModal(null)} className="px-3 py-1.5 text-sm text-gray-400 hover:text-white">Cancel</button>
              <button
                onClick={() => doAction('scale', { replicas })}
                disabled={loading}
                className="px-4 py-1.5 text-sm bg-blue-600 hover:bg-blue-500 text-white rounded disabled:opacity-50"
              >
                {loading ? 'Scaling...' : `Scale to ${replicas}`}
              </button>
            </div>
          </div>
        </Modal>
      )}

      {/* Restart modal */}
      {modal === 'restart' && (
        <Modal onClose={() => setModal(null)} title="Restart Deployment">
          <div className="space-y-4">
            <p className="text-sm text-gray-400">
              This will perform a rolling restart of <span className="text-white font-medium">{name}</span> by
              updating the pod template annotation. All pods will be recreated.
            </p>
            {error && <div className="text-sm text-red-400">{error}</div>}
            <div className="flex justify-end gap-2">
              <button onClick={() => setModal(null)} className="px-3 py-1.5 text-sm text-gray-400 hover:text-white">Cancel</button>
              <button
                onClick={() => doAction('restart', {})}
                disabled={loading}
                className="px-4 py-1.5 text-sm bg-yellow-600 hover:bg-yellow-500 text-white rounded disabled:opacity-50"
              >
                {loading ? 'Restarting...' : 'Restart'}
              </button>
            </div>
          </div>
        </Modal>
      )}

      {/* Delete modal */}
      {modal === 'delete' && (
        <Modal onClose={() => setModal(null)} title="Delete Resource">
          <div className="space-y-4">
            <div className="flex items-start gap-3 p-3 bg-red-900/20 border border-red-700/50 rounded-lg">
              <AlertTriangle className="w-5 h-5 text-red-400 shrink-0 mt-0.5" />
              <p className="text-sm text-red-300">
                This will permanently delete <span className="font-medium text-white">{namespace}/{name}</span>.
                This action cannot be undone.
              </p>
            </div>
            <div>
              <label className="block text-sm text-gray-400 mb-1">
                Type <span className="font-mono text-white">{name}</span> to confirm
              </label>
              <input
                type="text"
                value={confirmName}
                onChange={e => setConfirmName(e.target.value)}
                placeholder={name}
                className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white"
              />
            </div>
            {error && <div className="text-sm text-red-400">{error}</div>}
            <div className="flex justify-end gap-2">
              <button onClick={() => setModal(null)} className="px-3 py-1.5 text-sm text-gray-400 hover:text-white">Cancel</button>
              <button
                onClick={() => doAction('delete', {})}
                disabled={loading || confirmName !== name}
                className="px-4 py-1.5 text-sm bg-red-600 hover:bg-red-500 text-white rounded disabled:opacity-50"
              >
                {loading ? 'Deleting...' : 'Delete'}
              </button>
            </div>
          </div>
        </Modal>
      )}
    </>
  )
}

function Modal({ onClose, title, children }: { onClose: () => void; title: string; children: React.ReactNode }) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/60" onClick={onClose} />
      <div className="relative bg-gray-900 border border-gray-700 rounded-xl shadow-xl w-full max-w-md p-6">
        <div className="flex items-center justify-between mb-4">
          <h3 className="text-lg font-medium text-white">{title}</h3>
          <button onClick={onClose} className="text-gray-400 hover:text-white">
            <X className="w-5 h-5" />
          </button>
        </div>
        {children}
      </div>
    </div>
  )
}

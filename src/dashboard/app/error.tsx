'use client'

import { useEffect } from 'react'
import { AlertTriangle, RefreshCw } from 'lucide-react'

export default function ErrorBoundary({
    error,
    reset,
}: {
    error: Error & { digest?: string }
    reset: () => void
}) {
    useEffect(() => {
        // Log the error to an error reporting service
        console.error('Dashboard Application Error:', error)
    }, [error])

    return (
        <div className="min-h-screen bg-cluster-bg flex items-center justify-center p-4">
            <div className="bg-cluster-card border border-cluster-border rounded-xl p-8 max-w-md w-full text-center shadow-2xl">
                <div className="w-16 h-16 bg-red-500/10 rounded-full flex items-center justify-center mx-auto mb-6">
                    <AlertTriangle className="w-8 h-8 text-red-500" />
                </div>
                <h2 className="text-xl font-bold text-cluster-text mb-2">Something went wrong!</h2>
                <p className="text-cluster-muted mb-6 text-sm">
                    {error.message || 'An unexpected error occurred while rendering the dashboard. Our systems have logged the issue.'}
                </p>
                <button
                    onClick={reset}
                    className="w-full inline-flex items-center justify-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg font-medium transition-colors focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2"
                >
                    <RefreshCw className="w-4 h-4" />
                    Try Again
                </button>
            </div>
        </div>
    )
}

import type { Metadata } from 'next'
import { Inter } from 'next/font/google'
import './globals.css'

const inter = Inter({ subsets: ['latin'] })

// Force dynamic rendering so env vars are read at request time, not build time
export const dynamic = 'force-dynamic'

export const metadata: Metadata = {
  title: 'K8s Cluster Intelligence',
  description: 'AI-Powered Kubernetes Cluster Health & Optimization Dashboard',
}

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  // K8s injects ANALYZER_SERVICE_HOST and ANALYZER_SERVICE_PORT_API for the analyzer service
  const host = process.env.ANALYZER_SERVICE_HOST
  const port = process.env.ANALYZER_SERVICE_PORT_API || process.env.ANALYZER_SERVICE_PORT || '8081'
  const apiUrl = host ? `http://${host}:${port}` : (process.env.ANALYZER_URL || process.env.NEXT_PUBLIC_API_URL || '')

  return (
    <html lang="en" className="dark">
      <head>
        <script
          dangerouslySetInnerHTML={{
            __html: `window.__CLUSTER_INTEL_API__=${JSON.stringify(apiUrl)};`,
          }}
        />
      </head>
      <body className={`${inter.className} bg-cluster-bg text-cluster-text min-h-screen`}>
        {/* Skip to main content link for keyboard accessibility */}
        <a href="#main-content" className="skip-link">
          Skip to main content
        </a>
        {children}
      </body>
    </html>
  )
}

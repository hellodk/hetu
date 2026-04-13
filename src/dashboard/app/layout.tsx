import type { Metadata } from 'next'
import { Inter } from 'next/font/google'
import './globals.css'
import { Navigation } from '@/components/Navigation'

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
  // Browser-side API URL: empty string means use relative URLs (/api/v1/...)
  // which are proxied server-side by Next.js route handlers.
  // For WebSocket (logs, exec), the browser needs the direct analyzer URL
  // since Next.js can't proxy WebSocket upgrades.
  const apiUrl = process.env.ANALYZER_URL || process.env.NEXT_PUBLIC_API_URL || ''

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
        <Navigation />
        <main id="main-content" className="lg:ml-56">
          {children}
        </main>
      </body>
    </html>
  )
}

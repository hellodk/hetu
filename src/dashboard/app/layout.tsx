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
  // Browser-side API URL injected into window.__CLUSTER_INTEL_API__.
  //
  // IMPORTANT: this URL must be reachable from whatever device loads the
  // page, NOT from the Next.js server. Only read NEXT_PUBLIC_API_URL —
  // ANALYZER_URL is intentionally excluded because it's often something
  // like "http://localhost:18081" that only the host running next dev
  // can resolve. Injecting it causes every LAN visitor (phone, another
  // laptop at http://<lan-ip>:3003) to try and fetch the analyzer at
  // their OWN localhost, which 5xx's.
  //
  // When this is empty (the common local-dev case), the client uses:
  //   - relative URLs for REST    → proxied by app/api/v1/[...path]/route.ts
  //   - ws://${window.location.hostname}:18081 for WebSocket (logs/exec)
  // Both work from any origin that can reach the dashboard.
  //
  // Set NEXT_PUBLIC_API_URL only when the browser must bypass the proxy
  // (e.g. dashboard and analyzer served from different origins with no
  //  same-origin path).
  const apiUrl = process.env.NEXT_PUBLIC_API_URL || ''

  return (
    <html lang="en">
      <head>
        <script
          dangerouslySetInnerHTML={{
            __html: `
              window.__CLUSTER_INTEL_API__=${JSON.stringify(apiUrl)};
              (function () {
                try {
                  var stored = localStorage.getItem('ci_theme'); // 'light' | 'dark' | 'system'
                  var prefersDark = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches;
                  var theme = stored || 'system';
                  var shouldDark = theme === 'dark' || (theme === 'system' && prefersDark);
                  document.documentElement.classList.toggle('dark', shouldDark);
                } catch (e) {
                  // no-op (SSR / privacy mode)
                }
              })();
            `,
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

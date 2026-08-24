import type { Metadata } from 'next'
import { Inter, Space_Grotesk, Newsreader, Fraunces, JetBrains_Mono, Roboto } from 'next/font/google'
import './globals.css'
import { Navigation } from '@/components/Navigation'
import { GlobalSearch } from '@/components/GlobalSearch'
import { ChatWidget } from '@/components/ChatWidget'

// Body (every theme uses Inter for UI copy)
const inter = Inter({
  subsets: ['latin'],
  variable: '--font-inter',
  display: 'swap',
})

// Display — aurora
const spaceGrotesk = Space_Grotesk({
  subsets: ['latin'],
  weight: ['400', '500', '600', '700'],
  variable: '--font-space-grotesk',
  display: 'swap',
})

// Display — graphite (secondary fallback for Fraunces)
const newsreader = Newsreader({
  subsets: ['latin'],
  weight: ['400', '500', '600'],
  style: ['normal', 'italic'],
  variable: '--font-newsreader',
  display: 'swap',
})

// Display — graphite + prism (variable with optical sizing)
const fraunces = Fraunces({
  subsets: ['latin'],
  weight: 'variable',
  axes: ['SOFT', 'opsz'],
  variable: '--font-fraunces',
  display: 'swap',
})

// MD3 themes
const roboto = Roboto({
  subsets: ['latin'],
  weight: ['300', '400', '500', '700'],
  variable: '--font-roboto',
  display: 'swap',
})

// Tabular numbers across every theme
const jetbrainsMono = JetBrains_Mono({
  subsets: ['latin'],
  weight: ['400', '500', '600'],
  variable: '--font-jetbrains-mono',
  display: 'swap',
})

export const dynamic = 'force-dynamic'

export const metadata: Metadata = {
  title: 'Hetu',
  description: 'AI-Powered Kubernetes Cluster Health & Optimization Dashboard',
}

// Space-separated CSS-variable class list for <html>
const fontVars = [
  inter.variable,
  spaceGrotesk.variable,
  newsreader.variable,
  fraunces.variable,
  jetbrainsMono.variable,
  roboto.variable,
].join(' ')

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  // See previous version for why only NEXT_PUBLIC_API_URL is injected.
  const apiUrl = process.env.NEXT_PUBLIC_API_URL || ''
  // Escape < so an adversarial URL cannot inject </script> inside the inline script block.
  const inlineApiUrl = JSON.stringify(apiUrl).replace(/</g, '\\u003c')

  return (
    <html lang="en" className={fontVars} suppressHydrationWarning>
      <head>
        <script
          dangerouslySetInnerHTML={{
            __html: `
              window.__HETU_API__=${inlineApiUrl};
              (function () {
                // FOUC-safe theme init.
                // ci_theme may be: graphite | calm-signal | aurora | prism | auto | md-dark | md-light
                // "auto"   → dark OS prefers-dark ⇒ calm-signal, else graphite.
                // unset    → graphite (system default).
                // Legacy values from the old toggle (light/dark/system) get
                // migrated: light ⇒ graphite, dark ⇒ calm-signal, system ⇒ auto.
                try {
                  var VALID = ['graphite','calm-signal','aurora','prism','auto','md-dark','md-light'];
                  var LIGHT = ['graphite','prism','md-light'];
                  var LEGACY = { light: 'graphite', dark: 'calm-signal', system: 'auto' };
                  var stored = localStorage.getItem('ci_theme');
                  if (stored && LEGACY[stored]) {
                    stored = LEGACY[stored];
                    localStorage.setItem('ci_theme', stored);
                  }
                  if (!stored || VALID.indexOf(stored) === -1) stored = 'graphite';

                  var prefersDark = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches;
                  var resolved = stored === 'auto'
                    ? (prefersDark ? 'calm-signal' : 'graphite')
                    : stored;

                  document.documentElement.setAttribute('data-theme', resolved);
                  // Tailwind dark: variants fire for dark themes (not light-palette themes).
                  var isDark = LIGHT.indexOf(resolved) === -1;
                  document.documentElement.classList.toggle('dark', isDark);
                } catch (e) {
                  // SSR / privacy mode — keep the data-theme='graphite' default.
                }
              })();
            `,
          }}
        />
      </head>
      <body className="bg-cluster-bg text-cluster-text min-h-screen">
        {/* Skip to main content link for keyboard accessibility */}
        <a href="#main-content" className="skip-link">
          Skip to main content
        </a>
        <Navigation />
        <GlobalSearch />
        <main id="main-content" className="lg:ml-56">
          {children}
        </main>
        <ChatWidget />
      </body>
    </html>
  )
}

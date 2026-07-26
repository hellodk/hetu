/** @type {import('next').NextConfig} */
const nextConfig = {
  output: 'standalone',
  reactStrictMode: true,
  poweredByHeader: false,

  // Serve the app under a subpath (e.g. abc.com/hetu) by building with
  // NEXT_BASE_PATH=/hetu (basePath is BUILD-TIME in Next.js). Default is root ''
  // so local/dev/compose/run-local all work at http://host:port/ out of the box.
  basePath: process.env.NEXT_BASE_PATH ?? '',

  // Environment variables - empty string means use relative URLs (proxied via rewrites)
  env: {
    NEXT_PUBLIC_API_URL: process.env.NEXT_PUBLIC_API_URL || '',
    // Expose the (build-time) basePath to client code so browser fetches can be
    // prefixed correctly. Raw `fetch('/api/v1/..')` is NOT rewritten by Next's
    // basePath, so without this the client would hit `/api/v1/*` at the root and
    // 404 (the route handler is mounted under `${basePath}/api/v1/*`).
    NEXT_PUBLIC_BASE_PATH: process.env.NEXT_BASE_PATH ?? '',
  },
  
  // Headers for security
  async headers() {
    return [
      {
        source: '/:path*',
        headers: [
          {
            key: 'X-Frame-Options',
            value: 'DENY',
          },
          {
            key: 'X-Content-Type-Options',
            value: 'nosniff',
          },
          {
            key: 'X-XSS-Protection',
            value: '1; mode=block',
          },
          {
            key: 'Referrer-Policy',
            value: 'strict-origin-when-cross-origin',
          },
        ],
      },
    ]
  },
  
  // API proxy is handled by the route handler at app/api/v1/[...path]/route.ts
  // No rewrites needed — the route handler uses K8s DNS at runtime.
}

module.exports = nextConfig

/** @type {import('next').NextConfig} */
const nextConfig = {
  output: 'standalone',
  reactStrictMode: true,
  poweredByHeader: false,

  // Serve the app under a subpath (e.g. abc.com/hetu).
  // Set NEXT_BASE_PATH='' to serve from root (default when not behind a reverse proxy).
  basePath: process.env.NEXT_BASE_PATH ?? '/hetu',

  // Environment variables - empty string means use relative URLs (proxied via rewrites)
  env: {
    NEXT_PUBLIC_API_URL: process.env.NEXT_PUBLIC_API_URL || '',
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

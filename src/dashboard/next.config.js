/** @type {import('next').NextConfig} */
const nextConfig = {
  output: 'standalone',
  reactStrictMode: true,
  poweredByHeader: false,
  
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
  
  // Rewrites to proxy API calls to the analyzer service
  async rewrites() {
    const analyzerUrl = process.env.ANALYZER_URL || 'http://analyzer:8081'
    return [
      {
        source: '/api/:path*',
        destination: `${analyzerUrl}/api/:path*`,
      },
    ]
  },
}

module.exports = nextConfig

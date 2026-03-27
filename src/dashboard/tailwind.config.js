/** @type {import('tailwindcss').Config} */
module.exports = {
  content: [
    './pages/**/*.{js,ts,jsx,tsx,mdx}',
    './components/**/*.{js,ts,jsx,tsx,mdx}',
    './app/**/*.{js,ts,jsx,tsx,mdx}',
  ],
  theme: {
    extend: {
      colors: {
        // Custom color palette for the dashboard
        'cluster': {
          'bg': '#0f172a',
          'card': '#1e293b',
          'border': '#334155',
          'text': '#e2e8f0',
          'muted': '#94a3b8',
        },
        'score': {
          'excellent': '#22c55e',
          'good': '#84cc16',
          'warning': '#eab308',
          'danger': '#ef4444',
          'critical': '#dc2626',
        },
        'category': {
          'reliability': '#3b82f6',
          'security': '#8b5cf6',
          'cost': '#10b981',
          'architecture': '#f59e0b',
        }
      },
      animation: {
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        'score-fill': 'scoreFill 1s ease-out forwards',
      },
      keyframes: {
        scoreFill: {
          '0%': { strokeDashoffset: '282.7' },
          '100%': { strokeDashoffset: 'var(--score-offset)' },
        }
      }
    },
  },
  plugins: [],
}

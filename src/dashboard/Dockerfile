# Build stage
FROM node:20-alpine AS builder

WORKDIR /app

# pnpm is the only package manager for this repo (see AGENTS/CLAUDE conventions).
# Version is pinned by the packageManager field in package.json.
RUN corepack enable

# Copy lockfile + manifest for reproducible install
COPY package.json pnpm-lock.yaml ./

# Install dependencies (frozen lockfile — CI/builds never mutate deps)
RUN pnpm install --frozen-lockfile

# Copy source code
COPY . .

# Create public dir if it doesn't exist (Next.js expects it)
RUN mkdir -p public

# Subpath deploy is opt-in at BUILD time (Next.js basePath is build-time).
# Default empty => served at root. For the homelab /hetu deploy build with:
#   docker build --build-arg NEXT_BASE_PATH=/hetu ...
ARG NEXT_BASE_PATH=""
ENV NEXT_BASE_PATH=${NEXT_BASE_PATH}

# Build the application
RUN pnpm run build

# Runtime stage
FROM node:20-alpine AS runner

WORKDIR /app

# Create non-root user
RUN addgroup --system --gid 1001 nodejs && \
    adduser --system --uid 1001 nextjs

# Copy built application
COPY --from=builder /app/public ./public
COPY --from=builder /app/.next/standalone ./
COPY --from=builder /app/.next/static ./.next/static

# Set ownership
RUN chown -R nextjs:nodejs /app

USER nextjs

EXPOSE 3000

ENV NODE_ENV=production
ENV PORT=3000
ENV HOSTNAME="0.0.0.0"

# Carry the build-time basePath into the runtime image so the healthcheck hits
# the URL the app actually serves (root by default, /hetu for subpath builds).
ARG NEXT_BASE_PATH=""
ENV NEXT_BASE_PATH=${NEXT_BASE_PATH}

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider "http://localhost:3000${NEXT_BASE_PATH}/" || exit 1

CMD ["node", "server.js"]

# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install dependencies
RUN apk add --no-cache git ca-certificates

# Copy shared packages first (for layer caching)
COPY pkg/ ./pkg/

# Copy analyzer module
COPY src/analyzer/ ./src/analyzer/

# Build the binary (GOWORK=off to use replace directives in go.mod)
WORKDIR /app/src/analyzer
# VERSION comes from the repo VERSION file via --build-arg (scripts/ci);
# default "dev" keeps local builds honest instead of faking a semver.
ARG VERSION=dev
RUN GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -X main.version=${VERSION}" \
    -o /analyzer .

# Runtime stage
FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /analyzer /analyzer

USER 65534:65534

EXPOSE 8081 9091

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD ["/analyzer", "-healthcheck"]

ENTRYPOINT ["/analyzer"]

# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install dependencies
RUN apk add --no-cache git ca-certificates

# Copy shared packages first (for layer caching)
COPY pkg/ ./pkg/

# Copy collector module
COPY src/collector/ ./src/collector/

# Build the binary (GOWORK=off to use replace directives in go.mod)
WORKDIR /app/src/collector
RUN GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -X main.version=6.0.0" \
    -o /collector .

# Runtime stage
FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /collector /collector

USER 65534:65534

EXPOSE 8080 9090

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/collector", "-healthcheck"]

ENTRYPOINT ["/collector"]

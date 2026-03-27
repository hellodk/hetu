# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install dependencies
RUN apk add --no-cache git ca-certificates

# Copy go mod files
COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true

# Copy source code
COPY . .

# Initialize module if needed
RUN [ -f go.mod ] || go mod init github.com/your-org/cluster-intel-analyzer

# Download dependencies
RUN go mod tidy

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -X main.version=1.0.0" \
    -o /analyzer .

# Runtime stage
FROM scratch

# Copy CA certificates for HTTPS (needed for LLM API calls)
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the binary
COPY --from=builder /analyzer /analyzer

# Non-root user
USER 65534:65534

# Expose ports
EXPOSE 8081 9091

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD ["/analyzer", "-healthcheck"]

ENTRYPOINT ["/analyzer"]

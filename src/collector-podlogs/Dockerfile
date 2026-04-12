# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git ca-certificates

COPY pkg/ ./pkg/
COPY src/collector-podlogs/ ./src/collector-podlogs/

WORKDIR /app/src/collector-podlogs
RUN GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o /collector-podlogs .

# Runtime stage
FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /collector-podlogs /collector-podlogs

USER 65534:65534

ENTRYPOINT ["/collector-podlogs"]

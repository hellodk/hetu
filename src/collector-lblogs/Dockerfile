# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git ca-certificates

COPY pkg/ ./pkg/
COPY src/collector-lblogs/ ./src/collector-lblogs/

WORKDIR /app/src/collector-lblogs
RUN GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o /collector-lblogs .

# Runtime stage
FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /collector-lblogs /collector-lblogs

USER 65534:65534

ENTRYPOINT ["/collector-lblogs"]

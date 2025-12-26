# Build stage for Go backend
FROM golang:1.23-alpine AS go-builder

RUN apk add --no-cache git make

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev) -X main.commit=$(git rev-parse --short HEAD 2>/dev/null || echo none)" \
    -o /nebulaio ./cmd/nebulaio

# Build stage for web console
FROM node:20-alpine AS web-builder

WORKDIR /app/web

# Copy web source
COPY web/package*.json ./
RUN npm ci

COPY web/ ./
RUN npm run build

# Final stage
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 nebulaio && \
    adduser -u 1000 -G nebulaio -s /bin/sh -D nebulaio

WORKDIR /app

# Copy binary
COPY --from=go-builder /nebulaio /app/nebulaio

# Copy web assets
COPY --from=web-builder /app/web/dist /app/web/dist

# Create data directory
RUN mkdir -p /data && chown -R nebulaio:nebulaio /data

# Switch to non-root user
USER nebulaio

# Expose ports
# 9000 - S3 API
# 9001 - Admin/Console API
# 9002 - Web Console (static)
# 9003 - Raft (cluster communication)
EXPOSE 9000 9001 9002 9003

# Default data directory
VOLUME ["/data"]

# Environment variables
ENV NEBULAIO_DATA_DIR=/data

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:9001/health || exit 1

# Run
ENTRYPOINT ["/app/nebulaio"]
CMD ["--data", "/data"]

# Build stage
FROM golang:1.24-alpine AS builder

# Set working directory
WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Copy go.mod and go.sum first to leverage Docker cache
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary with optimizations, static linking, and version info
ARG VERSION=dev
ARG BUILD_DATE=unknown
ARG COMMIT_HASH=unknown

RUN CGO_ENABLED=0 go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.buildDate=${BUILD_DATE} -X main.commitHash=${COMMIT_HASH}" \
    -o heatpump2mqtt ./cmd/heatpump2mqtt

# Runtime stage - use distroless for minimal attack surface
FROM gcr.io/distroless/static:nonroot

# Labels for container metadata
LABEL org.opencontainers.image.source="https://github.com/jkaflik/heatpump2mqtt" \
      org.opencontainers.image.description="Panasonic heatpump to MQTT bridge for Home Assistant" \
      org.opencontainers.image.licenses="MIT"

# Copy the binary and config files from builder
COPY --from=builder /app/heatpump2mqtt /app/
COPY --from=builder /app/topics.yaml /app/
COPY --from=builder /app/config.yaml.example /app/config.yaml.example

# Copy timezone data
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Create a non-root user and set permissions
USER nonroot:nonroot

# Create a volume for configuration
VOLUME /config

# Set working directory
WORKDIR /app

# Environment variables
ENV HEATPUMP_CONFIGFILE=/config/config.yaml

# Health check
HEALTHCHECK --interval=60s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/app/heatpump2mqtt", "-v"] || exit 1

# Default command
ENTRYPOINT ["/app/heatpump2mqtt"]
CMD ["--config", "/config/config.yaml", "--topics", "/app/topics.yaml"]
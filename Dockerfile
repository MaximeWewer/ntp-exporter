# Multi-stage Dockerfile for NTP Prometheus Exporter

# =============================================================================
# Stage 1: Builder
# =============================================================================
FROM --platform=$BUILDPLATFORM golang:1.25.3-alpine AS builder

# Install build dependencies
RUN apk update && \
    apk upgrade && \
    apk add --no-cache \
        git \
        ca-certificates \
        tzdata && \
    rm -rf /var/cache/apk/*

# Declare build arguments for cross-compilation
ARG TARGETOS
ARG TARGETARCH

# Set working directory
WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download
RUN go mod verify

# Copy source code
COPY . .

# Build arguments for versioning
ARG VERSION=dev

# Build the binary
# - CGO_ENABLED=0: Disable CGO for static binary
# - GOOS/GOARCH: Use Docker Buildx automatic platform detection
# - -a: Force rebuilding of packages
# - -installsuffix cgo: Use different install suffix
# - -ldflags: Linker flags for smaller binary and version info
# - -trimpath: Remove file system paths from binary
# - -tags netgo: Use pure Go networking stack  
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-w -s \
    -X 'main.version=${VERSION}' \
    -extldflags '-static'" \
    -a -installsuffix cgo \
    -tags netgo \
    -o ntp-exporter \
    ./cmd/ntp-exporter

# Verify the binary
RUN chmod +x ntp-exporter && \
    ./ntp-exporter --version || true

# =============================================================================
# Stage 2: Final Runtime Image (Distroless)
# =============================================================================
FROM gcr.io/distroless/static:nonroot

# Pass build arguments to runtime stage
ARG VERSION

# Metadata labels following OCI standard
LABEL org.opencontainers.image.title="NTP Prometheus Exporter" \
      org.opencontainers.image.description="Modern NTP monitoring exporter for Prometheus" \
      org.opencontainers.image.vendor="Maxime Wewer" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.source="https://github.com/MaximeWewer/ntp-exporter" \
      org.opencontainers.image.documentation="https://github.com/MaximeWewer/ntp-exporter/blob/main/README.md"

# # Copy timezone data and CA certificates from builder
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Copy the binary from builder
COPY --from=builder /build/ntp-exporter /ntp-exporter

# Use non-root user (from distroless)
USER nonroot:nonroot

# Expose metrics port (standard NTP exporter port)
EXPOSE 9559

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD ["/ntp-exporter", "--version"]

# Default command
ENTRYPOINT ["/ntp-exporter"]

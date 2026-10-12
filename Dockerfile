# Multi-stage Dockerfile for NTP Prometheus Exporter

# =============================================================================
# Stage 1: Builder
# =============================================================================
FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine AS builder

# Target platform, set automatically by Docker Buildx
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

WORKDIR /build

# Download modules first for better layer caching
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .

# Build metadata
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

# Static binary: CGO is disabled, so no extra linker flag is needed
RUN GOARM="${TARGETVARIANT#v}" CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
    -ldflags="-s -w \
      -X 'main.version=${VERSION}' \
      -X 'main.commit=${COMMIT}' \
      -X 'main.buildDate=${BUILD_DATE}'" \
    -o ntp-exporter \
    ./cmd/ntp-exporter

# =============================================================================
# Stage 2: Runtime image (distroless, non-root)
# =============================================================================
FROM gcr.io/distroless/static:nonroot

ARG VERSION=dev
ARG COMMIT=unknown

LABEL org.opencontainers.image.title="NTP Prometheus Exporter" \
      org.opencontainers.image.description="NTP monitoring exporter for Prometheus" \
      org.opencontainers.image.vendor="Maxime Wewer" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.source="https://github.com/MaximeWewer/ntp-exporter" \
      org.opencontainers.image.documentation="https://github.com/MaximeWewer/ntp-exporter/blob/main/README.md"

COPY --from=builder /build/ntp-exporter /ntp-exporter

USER nonroot:nonroot

EXPOSE 9559

# Distroless has no shell: the binary checks its own /healthz endpoint
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/ntp-exporter", "--healthcheck"]

ENTRYPOINT ["/ntp-exporter"]

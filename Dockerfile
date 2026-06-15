# =============================================================================
# OpenClaw Dashboard — Dockerfile (Go binary only)
#
# Build:
#   docker build --build-arg VERSION=$(cat VERSION) -t openclaw-dashboard .
#
# Run (LAN — opts into non-loopback bind via env var):
#   docker run -p 8080:8080 \
#     -e OPENCLAW_DASHBOARD_ALLOW_NON_LOOPBACK=1 \
#     -v ~/.openclaw:/home/dashboard/.openclaw \
#     openclaw-dashboard
#
# Run (host-network — preserves loopback-only design, Linux only):
#   docker run --network=host \
#     -v ~/.openclaw:/home/dashboard/.openclaw \
#     openclaw-dashboard
#
# The dashboard rejects 0.0.0.0 binds unless OPENCLAW_DASHBOARD_ALLOW_NON_LOOPBACK=1
# is set. The opt-in is intentionally awkward — containers expose the chat
# rate-limit map's unbounded growth as a DoS surface on hostile networks.
# =============================================================================

# --- Stage 1: Build Go binary ---
FROM golang:1.26-alpine AS builder

ARG VERSION=dev
WORKDIR /build
COPY go.mod ./
COPY *.go ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/ ./web/
COPY VERSION ./
RUN CGO_ENABLED=0 go build \
    -ldflags="-s -w -X github.com/mudrii/openclaw-dashboard.BuildVersion=${VERSION}" \
    -o openclaw-dashboard ./cmd/openclaw-dashboard

# --- Stage 2: Runtime ---
FROM alpine:3.24

# wget is needed for HEALTHCHECK. Busybox in Alpine ships a wget applet, but
# install the full package so HEALTHCHECK behavior is stable across Alpine
# minor releases regardless of busybox config.
RUN apk add --no-cache wget

WORKDIR /app
COPY --from=builder /build/openclaw-dashboard .
COPY --from=builder /build/VERSION ./
COPY assets/runtime ./assets/runtime
RUN chmod +x assets/runtime/refresh.sh openclaw-dashboard

RUN adduser -D -u 1001 dashboard && \
    mkdir -p /home/dashboard/.openclaw && \
    chown -R dashboard:dashboard /app /home/dashboard
USER dashboard

VOLUME ["/home/dashboard/.openclaw"]
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget -qO /dev/null http://localhost:8080/ || exit 1

ENTRYPOINT ["./openclaw-dashboard"]
CMD ["--bind", "0.0.0.0", "--port", "8080"]

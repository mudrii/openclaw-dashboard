# =============================================================================
# OpenClaw Dashboard — Dockerfile (Go binary only)
#
# Build:
#   docker build -t openclaw-dashboard .
#
# Run:
#   docker run -p 8080:8080 -v ~/.openclaw:/home/dashboard/.openclaw openclaw-dashboard
# =============================================================================

# --- Stage 1: Build Go binary ---
FROM golang:1.26-alpine AS builder

WORKDIR /build
COPY go.mod ./
COPY *.go ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/ ./web/
COPY VERSION ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o openclaw-dashboard ./cmd/openclaw-dashboard

# --- Stage 2: Runtime ---
FROM alpine:3.21

RUN apk add --no-cache bash git

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

CMD ["./openclaw-dashboard", "--bind", "0.0.0.0", "--port", "8080"]

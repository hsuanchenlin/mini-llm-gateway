# syntax=docker/dockerfile:1.6

# --- builder ---
# Pure-Go SQLite (modernc.org/sqlite) means we don't need CGO, so the runtime
# image can be a tiny static-binary container with no C toolchain.
FROM golang:1.25-alpine AS builder
WORKDIR /src

# Cache module downloads in a separate layer so changes to source don't bust them.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/gateway \
    ./cmd/server

# --- runtime ---
FROM alpine:3.19

# ca-certificates is needed so the gateway can call HTTPS upstreams (api.openai.com etc).
RUN apk add --no-cache ca-certificates wget && \
    addgroup -S gateway && adduser -S gateway -G gateway && \
    mkdir -p /data && chown gateway:gateway /data

WORKDIR /app
COPY --from=builder /out/gateway /app/gateway

USER gateway

# Sensible container defaults; override via `docker run -e` or compose.
ENV GATEWAY_PORT=8090 \
    GATEWAY_DB_PATH=/data/mini-llm-gateway.db

EXPOSE 8090
VOLUME ["/data"]

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=5 \
    CMD wget -qO- http://localhost:8090/health >/dev/null 2>&1 || exit 1

ENTRYPOINT ["/app/gateway"]

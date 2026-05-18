FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o agentshield .

# ── Final image ────────────────────────────────────────────────────────────
FROM alpine:3.21

# wget is used for HEALTHCHECK against /health/live; ca-certificates for
# HTTPS calls to OpenAI-compatible backends.
RUN apk add --no-cache ca-certificates tzdata wget && \
    adduser -D -u 1000 app

USER app
WORKDIR /app
COPY --from=builder /app/agentshield .

EXPOSE 8080
ENV PORT=8080
# OLLAMA_URL is intentionally NOT baked in. On Mac with docker-compose the
# compose file injects host.docker.internal; in production deployments
# (TrueFoundry, k8s, plain Docker on Linux) it must come from the env at
# runtime. Baking host.docker.internal here would break every Linux deploy.

HEALTHCHECK --interval=15s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/health/live || exit 1

CMD ["./agentshield"]

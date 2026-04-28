FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o agentshield .

# ── Final image ────────────────────────────────────────────────────────────
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 1000 app

USER app
WORKDIR /app
COPY --from=builder /app/agentshield .

EXPOSE 8080
ENV PORT=8080
ENV OLLAMA_URL=http://host.docker.internal:11434

CMD ["./agentshield"]

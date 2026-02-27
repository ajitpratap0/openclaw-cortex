FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /openclaw-cortex ./cmd/openclaw-cortex

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata curl && \
    addgroup -S cortex && adduser -S cortex -G cortex

COPY --from=builder /openclaw-cortex /usr/local/bin/openclaw-cortex

USER cortex

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["openclaw-cortex", "stats", "--help"]

ENTRYPOINT ["openclaw-cortex"]

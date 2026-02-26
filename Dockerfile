FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /openclaw-cortex ./cmd/openclaw-cortex

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /openclaw-cortex /usr/local/bin/cortex

ENTRYPOINT ["openclaw-cortex"]

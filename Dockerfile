FROM golang:1.24-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o rms ./cmd/rms
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o subcrawler ./cmd/subcrawler
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o metacrawler ./cmd/metacrawler

FROM alpine:3.21

RUN apk add --no-cache ffmpeg tzdata

WORKDIR /app
COPY --from=builder /build/rms .
COPY --from=builder /build/subcrawler .
COPY --from=builder /build/metacrawler .
COPY --from=builder /build/web ./web

EXPOSE 8082

ENTRYPOINT ["./rms", "-config", "/app/config/config.yml"]

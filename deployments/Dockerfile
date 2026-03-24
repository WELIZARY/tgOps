# Стадия 1: сборка бинарника
FROM golang:1.26.1-alpine AS builder

RUN apk add --no-cache git

WORKDIR /build

# Сначала копируем зависимости - Docker кэширует этот слой
COPY go.mod go.sum ./
RUN go mod download

# Копируем исходники и собираем
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /tgops \
    ./cmd/tgops

# Стадия 2: минимальный образ для запуска
FROM alpine:3.19

# Сертификаты нужны для HTTPS запросов к Telegram API
RUN apk add --no-cache ca-certificates tzdata

# Создаём непривилегированного пользователя
RUN addgroup -S tgops && adduser -S tgops -G tgops

WORKDIR /app

# Копируем бинарник и миграции из builder
COPY --from=builder /tgops .
COPY --chown=tgops:tgops migrations/ ./migrations/

USER tgops

ENTRYPOINT ["/app/tgops"]
CMD ["--config", "/app/config.yaml"]

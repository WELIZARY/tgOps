BINARY  = bin/tgops
CONFIG  = configs/config.yaml
COMPOSE = docker compose -f deployments/docker-compose.yml

.PHONY: all build run test test-cover lint clean \
        setup \
        docker-up docker-down docker-clean docker-restart docker-rebuild \
        docker-logs docker-logs-pg \
        psql tidy

all: build


# Сборка


build:
	@mkdir -p bin
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BINARY) ./cmd/tgops

run:
	go run ./cmd/tgops --config $(CONFIG)

tidy:
	go mod tidy


# Проверка качество кода


lint:
	golangci-lint run ./...

test:
	go test -race -count=1 ./...

test-cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

clean:
	rm -rf bin/ coverage.out coverage.html


# Первоначальная настройка
# Создаём .env и configs/config.yaml из примеров, если их ещё нет.


setup:
	@test -f .env || cp .env.example .env
	@test -f configs/config.yaml || cp configs/config.example.yaml configs/config.yaml
	@mkdir -p keys


# Docker Compose


# Собрать образ и поднять весь стек (бот + PostgreSQL)
docker-up:
	$(COMPOSE) up -d --build

# Остановить стек. Данные PostgreSQL сохраняются в volume.
docker-down:
	$(COMPOSE) down

# Остановить стек и удалить volume с данными БД (полный сброс)
docker-clean:
	$(COMPOSE) down -v

# Перезапустить контейнер бота без пересборки (применяет правки config.yaml)
docker-restart:
	$(COMPOSE) restart tgops

# Пересобрать образ бота и перезапустить (применяет правки в Go-коде)
docker-rebuild:
	$(COMPOSE) up -d --build tgops


# Логи и диагностика


# Логи бота в реальном времени (Ctrl+C для выхода)
docker-logs:
	$(COMPOSE) logs -f tgops

# Логи PostgreSQL в реальном времени
docker-logs-pg:
	$(COMPOSE) logs -f postgres

# Открыть psql внутри контейнера PostgreSQL
psql:
	$(COMPOSE) exec postgres psql -U tgops -d tgops

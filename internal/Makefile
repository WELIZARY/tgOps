BINARY  = bin/tgops
CONFIG  = configs/config.yaml
COMPOSE = docker compose -f deployments/docker-compose.yml

.PHONY: all build run test lint clean docker-up docker-down docker-logs

all: build

## Сборка бинарника
build:
	@mkdir -p bin
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BINARY) ./cmd/tgops

## Запуск локально (нужен запущенный PostgreSQL)
run:
	go run ./cmd/tgops --config $(CONFIG)

## Запуск тестов
test:
	go test -race -count=1 ./...

## Запуск тестов с покрытием
test-cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Отчёт: coverage.html"

## Линтер
lint:
	golangci-lint run ./...

## Очистка артефактов сборки
clean:
	rm -rf bin/ coverage.out coverage.html

## Поднять стек (бот + PostgreSQL) через Docker Compose
docker-up:
	$(COMPOSE) up -d --build

## Остановить стек
docker-down:
	$(COMPOSE) down

## Остановить стек и удалить volumes (сбросить БД)
docker-clean:
	$(COMPOSE) down -v

## Логи бота в реальном времени
docker-logs:
	$(COMPOSE) logs -f tgops

## Логи PostgreSQL
docker-logs-pg:
	$(COMPOSE) logs -f postgres

## Подключиться к PostgreSQL (psql)
psql:
	$(COMPOSE) exec postgres psql -U tgops -d tgops

## Обновить зависимости
tidy:
	go mod tidy

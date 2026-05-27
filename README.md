# tgOps

Telegram-бот на Go для удаленного мониторинга и управления серверной
инфраструктурой. Метрики, логи, контейнеры, запуск Ansible-плейбуков,
сканирование уязвимостей, статусы CI/CD и сроки SSL - вс через чат, с
разграничением прав и журналом действий. Идея: телефон всегда под рукой,
картину сбоя можно собрать и среагировать ещ до того, как дойдшь до
рабочего места.

## Возможности

- Мониторинг серверов: CPU, RAM, диск, средняя нагрузка, топ процессов.
- Алерты (с дедупликацией) и подтверждением кнопкой прямо в чате.
- SSL-сертификаты: сроки и авто-уведомления за n-дней.
- HTTP-чеки эндпоинтов по списку, с интервалом и алертом при падении.
- Сетевые утилиты: ping, traceroute, nslookup - с контейнера бота, либо
  через SSH с любого из добавленных серверов.
- Логи сервисов (длинный вывод файлом).
- Работа Docker: список контейнеров, образов, логи.
- CI/CD: события от GitHub, GitLab, Jenkins, апрув деплоя.
- Ansible: запуск плейбуков из белого списка, история запусков в БД.
- Доступные обновления пакетов (apt), статусы бэкапов, cron-задачи и
  systemd-таймеры.
- Сканирование уязвимостей: lynis для хоста, trivy для docker-образов.
- Версии ключевого ПО на серверах (docker, compose, python, ядро, ОС).

14 модулей-плагинов с единым интерфейсом, разграничение прав
(admin / operator / viewer), полный журнал действий в `audit_log`, хранящийся в БД.

## Стек

- Go 1.26+ ([`go-telegram-bot-api/telegram-bot-api`](https://github.com/go-telegram-bot-api/telegram-bot-api) v5)
- PostgreSQL 16 (основной узел и реплика для чтения)
- SSH: `golang.org/x/crypto/ssh` (пул соединений)
- Логи: `uber-go/zap` (структурированный JSON)
- Хранилище: `jackc/pgx` v5, миграции `golang-migrate`
- Docker + docker-compose v2

Облачное развртывание этой системы — отдельный репозиторий
[`tgOps_infra`](https://github.com/WELIZARY/tgOps_infra): модульаный
Terraform, Ansible-роли, Jenkins-пайплайны, IAP-туннелированный SSH,
Workload Identity Federation для CI без долгоживущих ключей, Cloud SQL в
HA + реплика, стек наблюдаемости (Prometheus / Loki /
Grafana).

## Архитектура

```
       ┌───────────┐        ┌─────────────────────────────────────────┐
       │ Telegram  │ <----> │ tgOPS Bot (Go)                          │
       │ user/admin│        │   Router (диспетчер команд)             │
       └───────────┘        │   Middleware (RBAC + audit_log)         │
                            │   Modules (14 плагинов, единый интерф.) │
                            │   Config (YAML, поздняя инъекция сикр.) │
                            └─────┬──────────────────────┬────────────┘
                                  │ SSH (pool)           │ pgx
                                  v                      v
                       ┌────────────────────┐   ┌────────────────────┐
                       │ Managed servers    │   │ PostgreSQL 16      │
                       │ docker/systemd     │   │ primary + replica  │
                       │ /proc, journalctl  │   │ users, audit,      │
                       └────────────────────┘   │ alerts, pipelines  │
                                                └────────────────────┘
```

## Меню

Reply-меню прикрепляется к чату на `/start` и доступно кнопкой
клавиатуры. Двухуровневое:

```
🏠 главное меню
├ 📊 мониторинг      /status  /top  /health  /list
├ 🚨 алерты          /alerts  /ssl
├ 🌐 сеть            /ping  /traceroute  /nslookup  /endpoints
├ 📜 логи            /logs
├ 🐳 docker          /docker ps  /docker images  /docker logs
├ 🔧 ci/cd           /pipelines
├ ⚙ ansible          /ansible playbooks  /ansible run  /ansible status
├ 🛠 обслуживание    /updates  /backups  /cron  /scan  /versions
└ ❓ помощь          /help
```

Если имя сервера известно — пишешь команду напрямую (`/status vps-prod`).
Без аргумента команды выводят inline-кнопки выбора (или подкоманды для
`/cron`, `/scan`).

## Быстрый старт (локально)

Требуется Go 1.26+, Docker 20+, Docker Compose v2.

```bash
git clone https://github.com/welizary/tgops.git
cd tgops

cp .env.example .env                              # токен бота, пароль БД
cp configs/config.example.yaml configs/config.yaml
mkdir -p keys

# создать бота через @BotFather, токен подставить в .env (TGOPS_TELEGRAM_TOKEN)
# узнать свой Telegram ID через @userinfobot и вписать в config.yaml:
# telegram.initial_admin_id  и  notify.chat_id

# сгенерировать ключ под управляемый сервер (повторить для каждой VPS)
ssh-keygen -t ed25519 -f keys/vps-main -N "" -C "tgops@vps-main"
chmod 600 keys/*
# публичный ключ положить на сервер через ssh-copy-id или вручную

make docker-up                                    # бот + PostgreSQL
make docker-logs
```

Бот должен ответить на `/start` пользователю с указанным
`initial_admin_id` — он получит роль администратора при первом запуске.

## Конфигурация (минимальный пример)

```yaml
telegram:
  token: ""                  # из TGOPS_TELEGRAM_TOKEN в .env
  initial_admin_id: 123456789
  mode: polling              # webhook в проде за балансировщиком

database:
  primary:
    host: postgres
    port: 5432
    user: tgops
    name: tgops
    password: ""             # из TGOPS_DATABASE_PRIMARY_PASSWORD

ssh:
  keys_dir: keys
  servers:
    - name: vps-main
      host: 1.2.3.4
      port: 22
      user: deploy
      key_name: vps-main

notify:
  chat_id: 123456789

monitoring:
  interval: 60s
  thresholds:
    cpu_critical: 90
    ram_critical: 85
    disk_critical: 90
  alert_cooldown: 10m

ssl:
  warn_days: [30, 14, 7, 1]
  domains: [example.com]

health_checks:
  interval: 60s
  endpoints:
    - { name: site, url: https://example.com, expected_status: 200 }
```

Полный пример со всеми секциями — `configs/config.example.yaml`.

## Управление через CLI-скрипт

`scripts/tgops-ctl.sh` — интерактивный скрипт для повседневной
эксплуатацией: пользователи, серверы, алерты, конфигурация, БД и
docker-стек.

```bash
./scripts/tgops-ctl.sh                       # интерактивное меню
./scripts/tgops-ctl.sh user:add
./scripts/tgops-ctl.sh server:add            # добавит сервер в БД и
                                              # сгенерирует SSH-ключ
./scripts/tgops-ctl.sh alert:unacked
./scripts/tgops-ctl.sh config:threshold      # пороги мониторинга
./scripts/tgops-ctl.sh db:dump
./scripts/tgops-ctl.sh help                  # полный список команд
```

Серверы можно хранить в двух местах: статично в `configs/config.yaml`
(под git, для известных хостов) или динамически в таблице `servers`
(через скрипт). Бот объединяет оба источника.

## Структура репозитория

```
tgops/
├── cmd/tgops/main.go               # точка входа в приложение, сборка модулей
├── internal/
│   ├── bot/                        # маршрутизатор, RBAC, аудит, меню
│   ├── config/                     # парсер YAML
│   ├── storage/                    # репозитории на pgx
│   ├── ssh/                        # SSH-клиент с пулом соединений
│   ├── modules/                    # 14 модулей-плагинов
│   │   ├── system/                 # /status /top /health /list
│   │   ├── alerts/                 # фоновый сбор и алерт-менеджер
│   │   ├── ssl/                    # проверка сертификатов
│   │   ├── network/                # ping/traceroute/nslookup/endpoints
│   │   ├── logs/                   # journalctl по SSH
│   │   ├── docker/                 # docker ps/images/logs
│   │   ├── cicd/                   # webhook GitHub/GitLab/Jenkins
│   │   ├── ansible/                # запуск плейбуков
│   │   ├── updates/                # apt-обновления
│   │   ├── backups/                # актуальность бэкапов
│   │   ├── cron/                   # crontab и systemd-таймеры
│   │   ├── scan/                   # trivy + lynis
│   │   ├── versions/               # версии ПО
│   │   └── core/                   # /start /menu /help
│   ├── audit/                      # запись действий в audit_log
│   └── formatter/                  # форматирование Telegram-сообщений
├── migrations/                     # SQL-миграции схемы
├── configs/config.example.yaml     # пример конфига со всеми секциями
├── deployments/                    # Dockerfile, docker-compose
└── scripts/
    └── tgops-ctl.sh                # CLI управления
```

## Безопасность

- Разграничение доступа на 3 роли (admin / operator / viewer); проверка
  до запуска обработчика.
- Все действия и отказы (`denied`) пишутся в `audit_log` с
  длительностью.
- SSH к управляемым серверам - по приватным ключам, перечень разрешнных
  сервисов журналов и плейбуков ansible (белыми списками), пользовательский
  ввод не попадает в напрямую.
- Секреты не хранятся в репозитории и не вшиваются в образ. В рантайме
  токен Telegram, пароль БД и SSH-ключ доставляются как переменные
  окружения / монтируются файлами.

## Развертывание в облаке

Пример прод-инфраструктуры с разделенным CI/CD, HA-базой, IAP-доступом
и собственной наблюдаемостью лежит в отдельном репозитории
[`WELIZARY/tgOps_infra`](https://github.com/WELIZARY/tgOps_infra). Бот и
инфра связаны «образ в Artifact Registry + git-sha» - CI
бота проверяет, собирает и публикует образ, CD из инфра-репо его разворачивает.

# tgOPS

Telegram-бот для управления и мониторинга серверной инфраструктуры. Позволяет получать алерты, просматривать логи, контейнеры, запускать Ansible-плейбуки - всё прямо Telegram. Бот поможет экстренно среагировать на инциденты, ведь телефон всегда под рукой, можно сформировать картину сбоя еще не добравшись до компьютера.

## Статус реализации

| Модуль | Статус |
|--------|--------|
| Ядро бота (модель управления доступом, аудит, конфиг) | ✔ |
| Мониторинг (CPU, RAM, Disk, LA) | ✔ |
| Алерты о сбоях | ✔ |
| SSL-сертификаты — проверка сроков | ✔ |
| Сетевые утилиты (ping, traceroute, nslookup) | ✔ |
| Доступ к логам сервисов | ✔ |
| Управление Docker | ✔ |
| CI/CD уведомления и подтверждение деплоя | ✔ |
| Ansible (inventory, playbooks, modules) | В разработке |
| Планировщик задач (cron, ?systemd timers) | Планируется |
| Сканирование уязвимостей | Планируется |
| Проверка обновлений ПО | Планируется |
| Статусы бэкапов | Планируется |

## Возможности

* Мониторинг серверов: CPU, RAM, диск, Load Average, топ процессов
* Алерты при превышении пороговых значений
* Проверка доступности сервисов и сроков SSL-сертификатов
* Просмотр логов (tail, head, за период), либо отправка файлом
* Управление Docker-контейнерами (ps, images, logs)
* Интеграция с CI/CD: статусы пайплайнов, подтверждение деплоя
* Запуск Ansible-плейбуков и модулей из whitelist'а
* Планировщик: просмотр cron/timers, уведомления о выполнении
* Сканирование уязвимостей (??)
* Сетевые утилиты: ping, traceroute, nslookup
* Разграничение доступа внутри бота (admin / operator / viewer)
* Лог всех действий через бота

## Стек

* Golang + go-telegram-bot-api
* PostgreSQL 16
* GCP (Compute Engine) + VPS + домен
* Docker, Ansible
* GitHub Actions (CI/CD), Jenkins

## Структура проекта

```
┌────────────────┐      ┌───────────────────────────────────┐
│  Telegram      │◄────►│  tgOPS Bot (Go binary)            │
│  User/Admin    │      │  ├─ Router (command dispatcher)   │
└────────────────┘      │  ├─ Middleware (RBAC, audit log)  │
                        │  ├─ Modules (plugins)             │
                        │  └─ Config (YAML)                 │
                        └───────────────┬───────────────────┘
                                        │
              ┌─────────────────────────┼─────────────────────────┐
              ▼                         ▼                         ▼
┌────────────────┐      ┌────────────────┐      ┌────────────────┐
│  PostgreSQL    │      │  VPS           │      │  GCP           │
│  primary +     │      │  SSH/API       │      │  Terraform     │
│  replica       │      │  Docker        │      │  GCE, GKE      │
└────────────────┘      │  systemd       │      │  Cloud API     │
                        └────────────────┘      └────────────────┘
```
## Требования
Golang (v.1.26.1)
Docker (версия 20+)
Docker Compose v2
Git
Make (опционально)

## Первоначальная настройка

```bash
#Клонируем репозиторий
git clone https://github.com/welizary/tgops.git
cd tgops

#Создаем конфигурационные файлы
cp .env.example .env
cp configs/config.example.yaml configs/config.yaml
mkdir -p keys

#Создаем тг-бота и его токен указываем в .env, там же указываем пароли к БД
#Узнаем свой айди своего тг-аккаунта

#Генерируем SSH-ключи для серверов
ssh-keygen -t ed25519 -f keys/vps-name -N "" -C "tgops@vps-name" #так для каждой управляемой машины
chmod 600 keys/*

#Добавляем ключи на управляемый сервер через ssh-copy, либо вручную

#Настраиваем config
nano configs/config.yaml

...

telegram:
  token: ""                     # оставляем пустым, токен берётся из TGOPS_TELEGRAM_TOKEN в .env
  initial_admin_id: 123456789   # вставляем свой Telegram ID (от @userinfobot)
  mode: "polling"

initial_admin_id - одноразовая конфигурация, дабы присвоить указанному юзеру права администратора

...

database:
  primary:
    host: "postgres"   # именно "postgres" для Docker Compose, не localhost
    port: 5432
    user: "tgops"
    password: ""       # оставляем пустым - берётся из .env
    name: "tgops"
    ssl_mode: "disable"

ssh:
  keys_dir: "keys"
  default_key_path: ""
  connect_timeout: "10s"
  command_timeout: "30s"
  max_connections_per_host: 3
  servers:
    - name: "vps-test"      # произвольное имя, будет использоваться в командах бота
      host: "1.2.3.4"       # реальный IP сервера
      port: 22              
      user: "ubuntu"        # SSH-пользователь на сервере
      key_name: "vps-main"  # имя файла в папке keys/

...

notify:
  chat_id: 123456789   # свой Telegram ID - алерты будут приходить сюда

...

monitoring:
  interval: "60s"      # как часто опрашивать серверы
  thresholds:
    cpu_warning: 80    # % CPU для предупреждения
    cpu_critical: 90   # % CPU для критического алерта
    ram_warning: 75
    ram_critical: 85
    disk_warning: 80
    disk_critical: 90
  alert_cooldown: "10m"  # не слать повторный алерт чаще чем раз в 10 минут

```
## Запуск

```
make docker-up
#или вручную
docker compose -f deployments/docker-compose.yml up -d --build

make docker-logs
#или
docker compose -f deployments/docker-compose.yml logs -f tgops

#проверяем, что оба контейнера (БД и бот) запущены

docker compose -f deployments/docker-compose.yml ps


```

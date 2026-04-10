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
| Ansible — запуск плейбуков из whitelist | ✔ |
| Проверка обновлений ПО (apt/yum/dnf) | ✔ |
| Статусы бэкапов | ✔ |
| Планировщик задач (cron/systemd timers) | ✔ |
| Сканирование уязвимостей (trivy/lynis) | ✔ |
| Проверка версий ключевого ПО | ✔ |
| Bash-скрипт управления инфрой (пользователи, VM, конфиги) | Планируется |

## Возможности

* Мониторинг серверов: CPU, RAM, диск, Load Average, топ процессов
* Алерты при превышении пороговых значений с дедупликацией и inline-подтверждением
* Проверка доступности сервисов и сроков SSL-сертификатов
* Просмотр логов сервисов через SSH (journalctl + fallback), либо отправка файлом
* Управление Docker-контейнерами (ps, images, logs)
* Интеграция с CI/CD: статусы пайплайнов GitHub/GitLab/Jenkins, подтверждение деплоя
* Запуск Ansible-плейбуков из whitelist с историей запусков в БД
* Проверка доступных обновлений пакетов (apt/yum/dnf) через SSH
* Мониторинг статуса резервных копий: последний файл, возраст, размер
* Просмотр cron-задач и systemd-таймеров на серверах
* Сканирование уязвимостей: trivy (Docker-образы), lynis (хост)
* Проверка версий ПО: docker, ansible, go, nginx, postgres и др.
* Сетевые утилиты: ping, traceroute, nslookup
* Разграничение доступа внутри бота (admin / operator / viewer)
* Лог всех действий через бота

## Стек

* Golang + go-telegram-bot-api
* PostgreSQL 16 (primary + replica)
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
│  PostgreSQL    │      │  VPS / GCP VM  │      │  CI/CD         │
│  primary +     │      │  SSH/API       │      │  GitHub Actions│
│  replica       │      │  Docker        │      │  GitLab CI     │
└────────────────┘      │  systemd       │      │  Jenkins       │
                        └────────────────┘      └────────────────┘
```
## Требования
Golang (v.1.26.1)
Docker (версия 20+)
Docker Compose v2
Git
Make (опционально)
Ansible (для модуля /ansible — должен быть установлен на хосте с ботом)

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

cicd:
  webhook_port: 8080         # порт для входящих webhook от GitHub/GitLab/Jenkins
  webhook_secret: "secret"   # HMAC-ключ, указать в настройках webhook на стороне CI
  notify_chat_id: 123456789  # chat_id для уведомлений о деплоях

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

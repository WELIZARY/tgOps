#!/usr/bin/env bash

#! tgops-ctl.sh - скрипт управления проектом tgOPS (пользователи, серверы, логи, алерты, конфигурации)


set -euo pipefail

# пути относительно корня проекта

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
COMPOSE="docker compose -f $PROJECT_ROOT/deployments/docker-compose.yml"
CONFIG="$PROJECT_ROOT/configs/config.yaml"
KEYS_DIR="$PROJECT_ROOT/keys"

# цвета

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'


#! Вспомогательные функции


# вывод сообщений разного уровня
info()  { echo -e "${CYAN}[info]${NC} $*"; }
ok()    { echo -e "${GREEN}[ok]${NC} $*"; }
warn()  { echo -e "${YELLOW}[!]${NC} $*"; }
err()   { echo -e "${RED}[ошибка]${NC} $*" >&2; }
die()   { err "$*"; exit 1; }

# sql запрос без шапки (для получения значений в переменные)
pg_query() {
    $COMPOSE exec -T postgres psql -U tgops -d tgops -t -A -c "$1" 2>/dev/null
}

# sql запрос с шапкой (для красивого вывода пользователю)
pg_table() {
    $COMPOSE exec -T postgres psql -U tgops -d tgops -c "$1" 2>/dev/null
}

# проверяем что контейнер postgres запущен
check_stack() {
    if ! $COMPOSE ps --status running 2>/dev/null | grep -q "tgops-postgres"; then
        die "контейнер postgres не запущен. сначала: make docker-up"
    fi
}

# спрашиваем подтверждение перед опасными действиями
confirm() {
    local msg="${1:-продолжить?}"
    read -rp "$(echo -e "${YELLOW}$msg [y/n]: ${NC}")" ans
    [[ "$ans" =~ ^[yYдД] ]] || { info "отменено"; exit 0; }
}

# визуальный разделитель
separator() {
    echo -e "${CYAN}────────────────────────────────────────────${NC}"
}


#! Пользователи (таблица users)


# показать всех пользователей
user_list() {
    check_stack
    info "пользователи бота:"
    separator
    pg_table "SELECT id, telegram_id, username, role, is_active, created_at::date FROM users ORDER BY id;"
}

# добавить нового пользователя в бд
user_add() {
    check_stack
    read -rp "telegram id: " tg_id
    [[ "$tg_id" =~ ^[0-9]+$ ]] || die "telegram id должен быть числом"

    read -rp "username (без @, можно пусто): " uname
    echo "роли: admin, operator, viewer"
    read -rp "роль [viewer]: " role
    role="${role:-viewer}"
    [[ "$role" =~ ^(admin|operator|viewer)$ ]] || die "роль должна быть admin, operator или viewer"

    # проверяем наличие дубликатов
    existing=$(pg_query "SELECT id FROM users WHERE telegram_id = $tg_id;" | tr -d '[:space:]')
    if [[ -n "$existing" ]]; then
        die "пользователь с telegram_id=$tg_id уже существует (id=$existing)"
    fi

    pg_query "INSERT INTO users (telegram_id, username, role) VALUES ($tg_id, '$uname', '$role');" >/dev/null
    ok "пользователь добавлен: tg_id=$tg_id, role=$role"
}

# изменить роль существующего пользователя
user_role() {
    check_stack
    user_list
    separator
    read -rp "telegram id пользователя: " tg_id
    [[ "$tg_id" =~ ^[0-9]+$ ]] || die "telegram id должен быть числом"

    echo "роли: admin, operator, viewer"
    read -rp "новая роль: " role
    [[ "$role" =~ ^(admin|operator|viewer)$ ]] || die "роль должна быть admin, operator или viewer"

    result=$(pg_query "UPDATE users SET role = '$role', updated_at = now() WHERE telegram_id = $tg_id RETURNING id;" | tr -d '[:space:]')
    [[ -n "$result" ]] || die "пользователь с telegram_id=$tg_id не найден"
    ok "роль изменена на $role"
}

# активация/деактивация пользователей
user_toggle() {
    check_stack
    user_list
    separator
    read -rp "telegram id пользователя: " tg_id
    [[ "$tg_id" =~ ^[0-9]+$ ]] || die "telegram id должен быть числом"

    result=$(pg_query "UPDATE users SET is_active = NOT is_active, updated_at = now() WHERE telegram_id = $tg_id RETURNING is_active;" | tr -d '[:space:]')
    [[ -n "$result" ]] || die "пользователь с telegram_id=$tg_id не найден"

    if [[ "$result" == "t" ]]; then
        ok "пользователь активирован"
    else
        ok "пользователь деактивирован"
    fi
}

# удалить пользователя из бд
user_delete() {
    check_stack
    user_list
    separator
    read -rp "telegram id пользователя: " tg_id
    [[ "$tg_id" =~ ^[0-9]+$ ]] || die "telegram id должен быть числом"

    confirm "удалить пользователя с tg_id=$tg_id?"
    pg_query "DELETE FROM users WHERE telegram_id = $tg_id;" >/dev/null
    ok "пользователь удален"
}


#! Серверы (таблица servers + config.yaml + ssh ключи)


# показать серверы из бд
server_list() {
    check_stack
    info "серверы в базе данных:"
    separator
    pg_table "SELECT id, name, host, port, ssh_user, key_name, is_active FROM servers ORDER BY id;"
}

# добавить сервер: запись в бд, генерация ssh ключа, добавление в config.yaml
server_add() {
    check_stack
    read -rp "имя сервера (латиница, напр. vps-prod): " srv_name
    [[ "$srv_name" =~ ^[a-zA-Z0-9_-]+$ ]] || die "имя может содержать только латиницу, цифры, дефис, подчеркивание"

    read -rp "ip адрес или хост: " srv_host
    [[ -n "$srv_host" ]] || die "адрес не может быть пустым"

    read -rp "ssh порт [22]: " srv_port
    srv_port="${srv_port:-22}"
    [[ "$srv_port" =~ ^[0-9]+$ ]] || die "порт должен быть числом"

    read -rp "ssh пользователь [ubuntu]: " srv_user
    srv_user="${srv_user:-ubuntu}"

    # проверяем что имя не занято
    existing=$(pg_query "SELECT id FROM servers WHERE name = '$srv_name';" | tr -d '[:space:]')
    if [[ -n "$existing" ]]; then
        die "сервер с именем '$srv_name' уже есть в бд (id=$existing)"
    fi

    # генерируем ssh ключ если его еще нет
    key_file="$KEYS_DIR/$srv_name"
    if [[ -f "$key_file" ]]; then
        warn "ключ $key_file уже существует, используем его"
    else
        info "генерирую ssh ключ: $key_file"
        mkdir -p "$KEYS_DIR"
        ssh-keygen -t ed25519 -f "$key_file" -N "" -C "tgops@$srv_name" -q
        chmod 600 "$key_file"
        ok "ключ создан"
    fi

    # показываем публичный ключ чтобы можно было скопировать на сервер
    separator
    info "публичный ключ (добавь на сервер в ~/.ssh/authorized_keys):"
    echo ""
    cat "${key_file}.pub"
    echo ""
    separator

    # записываем в бд
    pg_query "INSERT INTO servers (name, host, port, ssh_user, key_name) VALUES ('$srv_name', '$srv_host', $srv_port, '$srv_user', '$srv_name');" >/dev/null
    ok "сервер добавлен в бд"

    # добавляем в config.yaml если такого сервера там еще нет
    if grep -q "name: \"$srv_name\"" "$CONFIG" 2>/dev/null; then
        warn "сервер '$srv_name' уже есть в config.yaml, пропускаю"
    else
        info "добавляю сервер в config.yaml"
        cat >> "$CONFIG" <<YAML_BLOCK

    # сервер $srv_name, добавлен $(date +%Y-%m-%d)
    - name: "$srv_name"
      host: "$srv_host"
      port: $srv_port
      user: "$srv_user"
      key_name: "$srv_name"
YAML_BLOCK
        warn "запись добавлена в конец файла, проверь что она в секции ssh.servers"
        ok "config.yaml обновлен"
    fi

    echo ""
    warn "не забудь:"
    echo "  1) добавить публичный ключ на сервер $srv_host"
    echo "  2) перезапустить бота: make docker-restart"
}

# удалить сервер из бд (ключи и конфиг не трогаем)
server_delete() {
    check_stack
    server_list
    separator
    read -rp "имя сервера для удаления: " srv_name

    confirm "удалить сервер '$srv_name' из бд? (ключи и config.yaml не трогаем)"
    pg_query "DELETE FROM servers WHERE name = '$srv_name';" >/dev/null
    ok "сервер удален из бд"
    warn "ключ keys/$srv_name и запись в config.yaml удали вручную если нужно"
}

# активация/деактивация сервера
server_toggle() {
    check_stack
    server_list
    separator
    read -rp "имя сервера: " srv_name

    result=$(pg_query "UPDATE servers SET is_active = NOT is_active WHERE name = '$srv_name' RETURNING is_active;" | tr -d '[:space:]')
    [[ -n "$result" ]] || die "сервер '$srv_name' не найден"

    if [[ "$result" == "t" ]]; then
        ok "сервер '$srv_name' активирован"
    else
        ok "сервер '$srv_name' деактивирован"
    fi
}


#! SSH ключи


# показать существующие ключи в директории keys/
keys_list() {
    info "ssh ключи в $KEYS_DIR:"
    separator
    if [[ -d "$KEYS_DIR" ]]; then
        for key in "$KEYS_DIR"/*; do
            # пропускаем .pub файлы, показываем только приватные
            [[ "$key" == *.pub ]] && continue
            [[ -f "$key" ]] || continue
            local name
            name=$(basename "$key")
            local pub="${key}.pub"
            if [[ -f "$pub" ]]; then
                echo -e "  ${GREEN}$name${NC} (pub: есть)"
            else
                echo -e "  ${YELLOW}$name${NC} (pub: нет)"
            fi
        done
    else
        warn "директория keys/ не найдена"
    fi
}

# сгенерировать новый ключ (без привязки к серверу)
keys_generate() {
    read -rp "имя ключа (напр. vps-prod): " key_name
    [[ "$key_name" =~ ^[a-zA-Z0-9_-]+$ ]] || die "имя может содержать только латиницу, цифры, дефис, подчеркивание"

    local key_file="$KEYS_DIR/$key_name"
    if [[ -f "$key_file" ]]; then
        die "ключ $key_file уже существует"
    fi

    mkdir -p "$KEYS_DIR"
    ssh-keygen -t ed25519 -f "$key_file" -N "" -C "tgops@$key_name" -q
    chmod 600 "$key_file"
    ok "ключ создан: $key_file"
    separator
    info "публичный ключ:"
    cat "${key_file}.pub"
}

# показать публичный ключ (удобно для копирования на сервер)
keys_show_pub() {
    read -rp "имя ключа: " key_name
    local pub="$KEYS_DIR/${key_name}.pub"
    [[ -f "$pub" ]] || die "файл $pub не найден"
    info "публичный ключ $key_name:"
    cat "$pub"
}


#!Справка


show_help() {
    echo -e "${BOLD}tgops-ctl.sh${NC} - управление проектом tgOPS"
    echo ""
    echo -e "${BOLD}использование:${NC} $0 <раздел>:<действие> [аргументы]"
    echo ""
    echo -e "${CYAN}--- пользователи ---${NC}"
    echo "  user:list              показать всех пользователей"
    echo "  user:add               добавить пользователя"
    echo "  user:role              изменить роль"
    echo "  user:toggle            включить/выключить пользователя"
    echo "  user:delete            удалить пользователя"
    echo ""
    echo -e "${CYAN}--- серверы ---${NC}"
    echo "  server:list            показать серверы"
    echo "  server:add             добавить сервер (бд + ssh ключ + config)"
    echo "  server:delete          удалить сервер из бд"
    echo "  server:toggle          включить/выключить сервер"
    echo ""
    echo -e "${CYAN}--- ssh ключи ---${NC}"
    echo "  keys:list              показать ключи"
    echo "  keys:generate          сгенерировать новый ключ"
    echo "  keys:pub               показать публичный ключ"
}


#! Интерактивное меню


interactive_menu() {
    while true; do
        echo ""
        echo -e "${BOLD}*** управление tgOPS ***${NC}"
        echo ""
        echo -e "  ${CYAN}1)${NC}  пользователи"
        echo -e "  ${CYAN}2)${NC}  серверы"
        echo -e "  ${CYAN}3)${NC}  ssh ключи"
        echo -e "  ${CYAN}0)${NC}  выход"
        echo ""
        read -rp "выбор: " section

        case "$section" in
            1)
                echo ""
                echo "  1) список    2) добавить    3) роль    4) вкл/выкл    5) удалить"
                read -rp "  выбор: " act
                case "$act" in
                    1) user_list ;;
                    2) user_add ;;
                    3) user_role ;;
                    4) user_toggle ;;
                    5) user_delete ;;
                    *) warn "неизвестный выбор" ;;
                esac
                ;;
            2)
                echo ""
                echo "  1) список    2) добавить    3) удалить    4) вкл/выкл"
                read -rp "  выбор: " act
                case "$act" in
                    1) server_list ;;
                    2) server_add ;;
                    3) server_delete ;;
                    4) server_toggle ;;
                    *) warn "неизвестный выбор" ;;
                esac
                ;;
            3)
                echo ""
                echo "  1) список    2) сгенерировать    3) показать pub"
                read -rp "  выбор: " act
                case "$act" in
                    1) keys_list ;;
                    2) keys_generate ;;
                    3) keys_show_pub ;;
                    *) warn "неизвестный выбор" ;;
                esac
                ;;
            0) info "пока!"; exit 0 ;;
            *) warn "неизвестный раздел" ;;
        esac
    done
}


#! Точка входа


# без аргументов (интерактивный режим)
if [[ $# -eq 0 ]]; then
    interactive_menu
    exit 0
fi

command="$1"
shift

case "$command" in
    user:list)      user_list ;;
    user:add)       user_add ;;
    user:role)      user_role ;;
    user:toggle)    user_toggle ;;
    user:delete)    user_delete ;;

    server:list)    server_list ;;
    server:add)     server_add ;;
    server:delete)  server_delete ;;
    server:toggle)  server_toggle ;;

    keys:list)      keys_list ;;
    keys:generate)  keys_generate ;;
    keys:pub)       keys_show_pub ;;

    help|--help|-h) show_help ;;

    *)
        err "неизвестная команда: $command"
        echo ""
        show_help
        exit 1
        ;;
esac

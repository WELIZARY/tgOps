!/usr/bin/env bash

#! tgops-ctl.sh - скрипт управления проектом tgOPS (пользователи, серверы, логи, алерты, конфигурации)


set -euo pipefail

# пути относительно корня проекта

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
COMPOSE="docker compose -f $PROJECT_ROOT/deployments/docker-compose.yml"

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
}


#! Интерактивное меню


interactive_menu() {
    while true; do
        echo ""
        echo -e "${BOLD}*** управление tgOPS ***${NC}"
        echo ""
        echo -e "  ${CYAN}1)${NC}  пользователи"
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

    help|--help|-h) show_help ;;

    *)
        err "неизвестная команда: $command"
        echo ""
        show_help
        exit 1
        ;;
esac

package cron

import (
	"context"
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"

	"github.com/WELIZARY/tgOps/internal/config"
	"github.com/WELIZARY/tgOps/internal/formatter"
	"github.com/WELIZARY/tgOps/internal/modules"
	internalssh "github.com/WELIZARY/tgOps/internal/ssh"
	"github.com/WELIZARY/tgOps/internal/storage"
)

// Module - модуль просмотра cron-задач и systemd-таймеров (/cron)
type Module struct {
	sshClient *internalssh.Client
	src       internalssh.ServerSource
	cfg       *config.CronConfig
	cronRepo  storage.CronRepo
	log       *zap.Logger
}

// New создаёт модуль cron
func New(
	sshClient *internalssh.Client,
	src internalssh.ServerSource,
	cfg *config.CronConfig,
	cronRepo storage.CronRepo,
	log *zap.Logger,
) *Module {
	return &Module{sshClient: sshClient, src: src, cfg: cfg, cronRepo: cronRepo, log: log}
}

func (m *Module) Name() string { return "cron" }

func (m *Module) Commands() []modules.BotCommand {
	return []modules.BotCommand{
		{Command: "/cron", Description: "cron list|timers [сервер] - расписание задач", MinRole: "viewer"},
	}
}

func (m *Module) Handle(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	args := strings.Fields(msg.CommandArguments())
	if len(args) == 0 {
		// без аргументов - кнопки выбора подкоманды
		return m.askSubcommand(bot, msg.Chat.ID)
	}

	// гибкий парсер: подкоманда и сервер могут идти в любом порядке
	sub, serverName := parseArgs(args)

	// если подкоманда есть, но сервер не задан - покажем кнопки серверов
	if (sub == "list" || sub == "timers") && serverName == "" {
		return m.askServer(ctx, bot, msg.Chat.ID, sub)
	}

	rest := []string{}
	if serverName != "" {
		rest = []string{serverName}
	}

	switch sub {
	case "list":
		return m.handleList(ctx, bot, msg.Chat.ID, rest)
	case "timers":
		return m.handleTimers(ctx, bot, msg.Chat.ID, rest)
	default:
		return replyText(bot, msg.Chat.ID,
			fmt.Sprintf("Неизвестная подкоманда %q. Доступны: list, timers", sub))
	}
}

// HandleCallback обрабатывает inline-кнопки cron.
// callback data:
//
//	"cron_sub_list" / "cron_sub_timers" - выбор подкоманды (потом показывает серверы)
//	"cron_list_<server>" / "cron_timers_<server>" - финальное выполнение
func (m *Module) HandleCallback(ctx context.Context, bot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery) error {
	_, _ = bot.Request(tgbotapi.NewCallback(query.ID, ""))
	data := query.Data

	// первый уровень: выбор подкоманды
	if strings.HasPrefix(data, "cron_sub_") {
		sub := strings.TrimPrefix(data, "cron_sub_")
		hideKeyboard(bot, query)
		return m.askServer(ctx, bot, query.Message.Chat.ID, sub)
	}

	// второй уровень: финальное выполнение
	for _, sub := range []string{"list", "timers"} {
		prefix := "cron_" + sub + "_"
		if strings.HasPrefix(data, prefix) {
			name := strings.TrimPrefix(data, prefix)
			hideKeyboard(bot, query)
			rest := []string{name}
			if sub == "list" {
				return m.handleList(ctx, bot, query.Message.Chat.ID, rest)
			}
			return m.handleTimers(ctx, bot, query.Message.Chat.ID, rest)
		}
	}
	return nil
}

// askSubcommand показывает кнопки выбора list/timers
func (m *Module) askSubcommand(bot *tgbotapi.BotAPI, chatID int64) error {
	buttons := []formatter.ButtonRow{
		{Label: "cron-задачи", Data: "cron_sub_list"},
		{Label: "systemd-таймеры", Data: "cron_sub_timers"},
	}
	msg := tgbotapi.NewMessage(chatID, "Что показать?")
	msg.ReplyMarkup = formatter.SubcommandKeyboard(buttons)
	_, err := bot.Send(msg)
	return err
}

// askServer показывает кнопки выбора сервера для конкретной подкоманды
func (m *Module) askServer(ctx context.Context, bot *tgbotapi.BotAPI, chatID int64, sub string) error {
	servers, err := m.src.GetServers(ctx)
	if err != nil || len(servers) == 0 {
		return replyText(bot, chatID, "Серверы не настроены.")
	}
	prompt := "Выберите сервер для /cron " + sub + ":"
	msg := tgbotapi.NewMessage(chatID, prompt)
	msg.ReplyMarkup = formatter.ServerKeyboard(servers, "cron_"+sub+"_")
	_, err = bot.Send(msg)
	return err
}

// hideKeyboard убирает inline-кнопки в исходном сообщении
func hideKeyboard(bot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery) {
	edit := tgbotapi.NewEditMessageReplyMarkup(
		query.Message.Chat.ID,
		query.Message.MessageID,
		tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}},
	)
	_, _ = bot.Send(edit)
}

// parseArgs ищет подкоманду (list/timers) и имя сервера независимо от порядка
func parseArgs(args []string) (sub, serverName string) {
	for _, a := range args {
		switch a {
		case "list", "timers":
			sub = a
		default:
			serverName = a
		}
	}
	// если подкоманда не найдена — берём первое слово как подкоманду (для понятной ошибки)
	if sub == "" && len(args) > 0 {
		sub = args[0]
	}
	return
}

// handleList собирает crontab всех пользователей и системные задачи
func (m *Module) handleList(ctx context.Context, bot *tgbotapi.BotAPI, chatID int64, args []string) error {
	srv, err := m.resolveServer(ctx, args)
	if err != nil {
		return replyText(bot, chatID, err.Error())
	}

	cmdCtx, cancel := context.WithTimeout(ctx, m.timeout())
	defer cancel()

	// собираем системные cron-задачи и crontab текущего ssh-пользователя.
	// требование root намеренно убрано: crontab -u нужен root, поэтому читаем только свой.
	// перенаправление 2>/dev/null после glob - синтаксическая ошибка bash, поэтому
	// глобам разрешаем расширяться как есть, а несуществующие файлы фильтруем через [ -f ]
	sshCmd := `
FOUND=0
# системные задачи из /etc/cron.* директорий
for d in /etc/cron.d /etc/cron.daily /etc/cron.weekly /etc/cron.monthly /etc/cron.hourly; do
    [ -d "$d" ] || continue
    for f in "$d"/*; do
        [ -f "$f" ] || continue
        content=$(grep -vE '^(#|[[:space:]]*$)' "$f" 2>/dev/null)
        if [ -n "$content" ]; then
            base=$(basename "$f")
            echo "$content" | sed "s|^|system:$base |"
            FOUND=1
        fi
    done
done
# главный системный /etc/crontab (без переменных окружения)
if [ -f /etc/crontab ]; then
    content=$(grep -vE '^(#|[[:space:]]*$|^[A-Z_]+=)' /etc/crontab 2>/dev/null)
    if [ -n "$content" ]; then
        echo "$content" | sed 's|^|system:crontab |'
        FOUND=1
    fi
fi
# crontab текущего ssh-пользователя (root не требуется)
user_cron=$(crontab -l 2>/dev/null | grep -vE '^(#|[[:space:]]*$)')
if [ -n "$user_cron" ]; then
    me=$(whoami)
    echo "$user_cron" | sed "s|^|$me |"
    FOUND=1
fi
[ "$FOUND" = "0" ] && echo "NO_CRON_JOBS"
`

	out, runErr := m.sshClient.Run(cmdCtx, internalssh.SpecFromServer(srv), sshCmd)
	if runErr != nil {
		m.log.Warn("cron list: ошибка SSH", zap.String("server", srv.Name), zap.Error(runErr))
	}

	out = strings.TrimSpace(out)

	// сохраняем снапшот в БД (игнорируем ошибку - не критично)
	snap := &storage.CronSnapshot{
		ServerName: srv.Name,
		Source:     storage.CronSourceCrontab,
		RawOutput:  out,
	}
	if saveErr := m.cronRepo.Save(ctx, snap); saveErr != nil {
		m.log.Warn("не удалось сохранить cron_snapshot", zap.Error(saveErr))
	}

	if out == "" || out == "NO_CRON_JOBS" {
		text := fmt.Sprintf("%s @ %s\n\nАктивных cron-задач не найдено.",
			formatter.Bold("Cron"), formatter.EscapeHTML(srv.Name))
		reply := tgbotapi.NewMessage(chatID, text)
		reply.ParseMode = "HTML"
		_, err = bot.Send(reply)
		return err
	}

	formatted := formatCrontab(out)
	header := fmt.Sprintf("%s @ %s\n\n",
		formatter.Bold("Cron tasks"), formatter.EscapeHTML(srv.Name))

	return m.sendOutput(bot, chatID, header, formatted)
}

// handleTimers выводит активные systemd-таймеры
func (m *Module) handleTimers(ctx context.Context, bot *tgbotapi.BotAPI, chatID int64, args []string) error {
	srv, err := m.resolveServer(ctx, args)
	if err != nil {
		return replyText(bot, chatID, err.Error())
	}

	cmdCtx, cancel := context.WithTimeout(ctx, m.timeout())
	defer cancel()

	sshCmd := `systemctl list-timers --all --no-pager 2>/dev/null | head -25 || echo "SYSTEMD_UNAVAILABLE"`

	out, runErr := m.sshClient.Run(cmdCtx, internalssh.SpecFromServer(srv), sshCmd)
	if runErr != nil {
		m.log.Warn("cron timers: ошибка SSH", zap.String("server", srv.Name), zap.Error(runErr))
	}

	out = strings.TrimSpace(out)

	// сохраняем снапшот
	snap := &storage.CronSnapshot{
		ServerName: srv.Name,
		Source:     storage.CronSourceSystemd,
		RawOutput:  out,
	}
	if saveErr := m.cronRepo.Save(ctx, snap); saveErr != nil {
		m.log.Warn("не удалось сохранить cron_snapshot", zap.Error(saveErr))
	}

	if out == "" || out == "SYSTEMD_UNAVAILABLE" {
		text := fmt.Sprintf("%s @ %s\n\nsystemd недоступен или таймеры не настроены.",
			formatter.Bold("Systemd timers"), formatter.EscapeHTML(srv.Name))
		reply := tgbotapi.NewMessage(chatID, text)
		reply.ParseMode = "HTML"
		_, err = bot.Send(reply)
		return err
	}

	header := fmt.Sprintf("%s @ %s\n\n",
		formatter.Bold("Systemd timers"), formatter.EscapeHTML(srv.Name))

	return m.sendOutput(bot, chatID, header, out)
}

// formatCrontab форматирует вывод crontab в читаемую таблицу
func formatCrontab(raw string) string {
	lines := strings.Split(raw, "\n")
	var sb strings.Builder

	fmt.Fprintf(&sb, "%-16s  %-22s  %s\n", "ПОЛЬЗОВАТЕЛЬ", "РАСПИСАНИЕ", "КОМАНДА")
	sb.WriteString(strings.Repeat("-", 70) + "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// формат: "username schedule_fields... command"
		parts := strings.Fields(line)
		if len(parts) < 7 {
			// строки из cron.d могут иметь другой формат — выводим как есть
			fmt.Fprintf(&sb, "%s\n", line)
			continue
		}
		user := parts[0]
		schedule := strings.Join(parts[1:6], " ")
		command := strings.Join(parts[6:], " ")
		fmt.Fprintf(&sb, "%-16s  %-22s  %s\n",
			truncate(user, 16),
			truncate(schedule, 22),
			truncate(command, 40),
		)
	}
	return sb.String()
}

// sendOutput отправляет вывод в pre-блоке; если слишком длинный — файлом
func (m *Module) sendOutput(bot *tgbotapi.BotAPI, chatID int64, header, out string) error {
	const maxChars = 3500
	if len(out) <= maxChars {
		reply := tgbotapi.NewMessage(chatID, header+formatter.Pre(formatter.EscapeHTML(out)))
		reply.ParseMode = "HTML"
		_, err := bot.Send(reply)
		return err
	}

	ts := time.Now().Format("20060102-150405")
	doc := tgbotapi.NewDocument(chatID, tgbotapi.FileReader{
		Name:   fmt.Sprintf("cron-%s.txt", ts),
		Reader: strings.NewReader(out),
	})
	doc.Caption = header
	_, err := bot.Send(doc)
	return err
}

// resolveServer находит сервер по имени из аргументов или возвращает первый
func (m *Module) resolveServer(ctx context.Context, args []string) (*storage.Server, error) {
	servers, err := m.src.GetServers(ctx)
	if err != nil || len(servers) == 0 {
		return nil, fmt.Errorf("серверы не настроены")
	}
	if len(args) > 0 {
		name := args[0]
		for _, s := range servers {
			if s.Name == name {
				return s, nil
			}
		}
		names := make([]string, len(servers))
		for i, s := range servers {
			names[i] = s.Name
		}
		return nil, fmt.Errorf("сервер %q не найден. Доступные: %s",
			name, strings.Join(names, ", "))
	}
	return servers[0], nil
}

// timeout возвращает таймаут из конфига или 15s по умолчанию
func (m *Module) timeout() time.Duration {
	d, err := time.ParseDuration(m.cfg.Timeout)
	if err != nil {
		return 15 * time.Second
	}
	return d
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "~"
}

func replyText(bot *tgbotapi.BotAPI, chatID int64, text string) error {
	_, err := bot.Send(tgbotapi.NewMessage(chatID, text))
	return err
}

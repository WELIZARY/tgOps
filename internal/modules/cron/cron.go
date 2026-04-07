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
		return replyText(bot, msg.Chat.ID,
			"Использование:\n"+
				"  /cron list [сервер]   — cron-задачи\n"+
				"  /cron timers [сервер] — systemd-таймеры",
		)
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "list":
		return m.handleList(ctx, bot, msg, rest)
	case "timers":
		return m.handleTimers(ctx, bot, msg, rest)
	default:
		return replyText(bot, msg.Chat.ID,
			fmt.Sprintf("Неизвестная подкоманда %q. Доступны: list, timers", sub))
	}
}

// handleList собирает crontab всех пользователей и системные задачи
func (m *Module) handleList(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message, args []string) error {
	srv, err := m.resolveServer(ctx, args)
	if err != nil {
		return replyText(bot, msg.Chat.ID, err.Error())
	}

	cmdCtx, cancel := context.WithTimeout(ctx, m.timeout())
	defer cancel()

	// собираем crontab всех пользователей + системные задачи в /etc/cron.d/
	sshCmd := `
FOUND=0
for user in $(cut -f1 -d: /etc/passwd 2>/dev/null); do
    entries=$(crontab -u "$user" -l 2>/dev/null | grep -v "^#" | grep -v "^[[:space:]]*$")
    if [ -n "$entries" ]; then
        echo "$entries" | while IFS= read -r line; do
            echo "$user $line"
        done
        FOUND=1
    fi
done
for f in /etc/cron.d/* /etc/cron.daily/* /etc/cron.weekly/* /etc/cron.monthly/* 2>/dev/null; do
    [ -f "$f" ] || continue
    grep -v "^#" "$f" 2>/dev/null | grep -v "^[[:space:]]*$" | \
        sed "s|^|system:$(basename $f) |"
    FOUND=1
done
[ "$FOUND" = "0" ] && echo "NO_CRON_JOBS"`

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
		reply := tgbotapi.NewMessage(msg.Chat.ID, text)
		reply.ParseMode = "HTML"
		_, err = bot.Send(reply)
		return err
	}

	formatted := formatCrontab(out)
	header := fmt.Sprintf("%s @ %s\n\n",
		formatter.Bold("Cron tasks"), formatter.EscapeHTML(srv.Name))

	return m.sendOutput(bot, msg.Chat.ID, header, formatted)
}

// handleTimers выводит активные systemd-таймеры
func (m *Module) handleTimers(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message, args []string) error {
	srv, err := m.resolveServer(ctx, args)
	if err != nil {
		return replyText(bot, msg.Chat.ID, err.Error())
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
		reply := tgbotapi.NewMessage(msg.Chat.ID, text)
		reply.ParseMode = "HTML"
		_, err = bot.Send(reply)
		return err
	}

	header := fmt.Sprintf("%s @ %s\n\n",
		formatter.Bold("Systemd timers"), formatter.EscapeHTML(srv.Name))

	return m.sendOutput(bot, msg.Chat.ID, header, out)
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

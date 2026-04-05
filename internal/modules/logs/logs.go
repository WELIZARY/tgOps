package logs

import (
	"context"
	"fmt"
	"regexp"
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

// validService разрешает только безопасные символы в имени сервиса
var validService = regexp.MustCompile(`^[a-zA-Z0-9\-_.@]+$`)

// Module - модуль просмотра логов сервисов (/logs)
type Module struct {
	sshClient *internalssh.Client
	src       internalssh.ServerSource
	cfg       *config.LogsConfig
	log       *zap.Logger
}

// New создаёт модуль логов
func New(sshClient *internalssh.Client, src internalssh.ServerSource, cfg *config.LogsConfig, log *zap.Logger) *Module {
	return &Module{sshClient: sshClient, src: src, cfg: cfg, log: log}
}

func (m *Module) Name() string { return "logs" }

func (m *Module) Commands() []modules.BotCommand {
	return []modules.BotCommand{
		{Command: "/logs", Description: "logs <сервис> [сервер] - журнал сервиса", MinRole: "viewer"},
	}
}

func (m *Module) Handle(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	if msg.Command() == "logs" {
		return m.handleLogs(ctx, bot, msg)
	}
	return nil
}

func (m *Module) handleLogs(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	args := strings.Fields(msg.CommandArguments())
	if len(args) == 0 {
		return replyText(bot, msg.Chat.ID, "Укажите сервис. Пример: /logs nginx")
	}

	service := args[0]
	if !m.allowedService(service) {
		allowed := strings.Join(m.cfg.AllowedServices, ", ")
		if allowed == "" {
			allowed = "список не настроен"
		}
		return replyText(bot, msg.Chat.ID,
			fmt.Sprintf("Сервис %q не разрешён.\nДоступные сервисы: %s", service, allowed))
	}

	srv, err := m.resolveServer(ctx, args[1:])
	if err != nil {
		return replyText(bot, msg.Chat.ID, err.Error())
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// journalctl с fallback на tail для не-systemd хостов
	cmd := fmt.Sprintf(
		"journalctl -u %s -n %d --no-pager -o short 2>&1 || tail -n %d /var/log/%s.log 2>/dev/null || echo 'журнал недоступен'",
		service, m.cfg.MaxLines, m.cfg.MaxLines, service,
	)

	out, err := m.sshClient.Run(cmdCtx, internalssh.SpecFromServer(srv), cmd)
	if err != nil {
		m.log.Warn("ошибка получения логов", zap.String("service", service), zap.String("server", srv.Name), zap.Error(err))
	}

	out = strings.TrimSpace(out)
	if out == "" {
		out = "вывод пуст"
	}

	header := fmt.Sprintf("%s @ %s — последние %d строк\n\n",
		formatter.Bold(formatter.EscapeHTML(service)), formatter.EscapeHTML(srv.Name), m.cfg.MaxLines)

	return m.sendLog(bot, msg.Chat.ID, header, out, service)
}

// sendLog отправляет лог: короткий — в pre-блоке, длинный — несколькими сообщениями или файлом
func (m *Module) sendLog(bot *tgbotapi.BotAPI, chatID int64, header, out, service string) error {
	// запас на HTML-теги и заголовок
	maxChars := m.cfg.MaxMessageChars - 200
	if maxChars <= 0 {
		maxChars = 3800
	}

	parts := splitText(out, maxChars)

	if len(parts) == 1 {
		reply := tgbotapi.NewMessage(chatID, header+formatter.Pre(formatter.EscapeHTML(parts[0])))
		reply.ParseMode = "HTML"
		_, err := bot.Send(reply)
		return err
	}

	if len(parts) <= 3 {
		first := tgbotapi.NewMessage(chatID, header+formatter.Pre(formatter.EscapeHTML(parts[0])))
		first.ParseMode = "HTML"
		if _, err := bot.Send(first); err != nil {
			return err
		}
		for _, part := range parts[1:] {
			next := tgbotapi.NewMessage(chatID, formatter.Pre(formatter.EscapeHTML(part)))
			next.ParseMode = "HTML"
			if _, err := bot.Send(next); err != nil {
				return err
			}
		}
		return nil
	}

	// слишком длинный — отдаём файлом
	ts := time.Now().Format("20060102-150405")
	fileName := fmt.Sprintf("%s-%s.log", service, ts)
	doc := tgbotapi.NewDocument(chatID, tgbotapi.FileReader{
		Name:   fileName,
		Reader: strings.NewReader(out),
	})
	doc.Caption = fmt.Sprintf("Журнал %s (файл, слишком длинный для сообщения)", service)
	_, err := bot.Send(doc)
	return err
}

// allowedService проверяет что сервис в whitelist и не содержит спецсимволов
func (m *Module) allowedService(name string) bool {
	if !validService.MatchString(name) {
		return false
	}
	for _, s := range m.cfg.AllowedServices {
		if s == name {
			return true
		}
	}
	return false
}

// resolveServer возвращает первый сервер из источника, или сервер по имени из args
func (m *Module) resolveServer(ctx context.Context, args []string) (*storage.Server, error) {
	servers, err := m.src.GetServers(ctx)
	if err != nil || len(servers) == 0 {
		return nil, fmt.Errorf("серверы не настроены")
	}
	if len(args) == 0 {
		return servers[0], nil
	}
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
	return nil, fmt.Errorf("сервер %q не найден.\nДоступные серверы: %s",
		name, strings.Join(names, ", "))
}

// splitText разбивает строку на части не длиннее maxChars, разрезая по символу переноса строки
func splitText(s string, maxChars int) []string {
	if len(s) <= maxChars {
		return []string{s}
	}
	var parts []string
	for len(s) > 0 {
		if len(s) <= maxChars {
			parts = append(parts, s)
			break
		}
		cut := maxChars
		// ищем ближайший перенос строки чтобы не рвать строки посередине
		if idx := strings.LastIndex(s[:cut], "\n"); idx > 0 {
			cut = idx
		}
		parts = append(parts, s[:cut])
		s = strings.TrimLeft(s[cut:], "\n")
	}
	return parts
}

func replyText(bot *tgbotapi.BotAPI, chatID int64, text string) error {
	_, err := bot.Send(tgbotapi.NewMessage(chatID, text))
	return err
}

package network

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"

	"github.com/WELIZARY/tgOps/internal/formatter"
	"github.com/WELIZARY/tgOps/internal/modules"
)

// validHost разрешает только hostname/IP символы, чтобы не допустить инъекций команд
var validHost = regexp.MustCompile(`^[a-zA-Z0-9.\-]+$`)

// Module - модуль сетевых утилит (/ping, /traceroute, /nslookup)
// Команды выполняются локально на хосте бота через os/exec
type Module struct {
	log *zap.Logger
}

// New создаёт модуль сетевых утилит
func New(log *zap.Logger) *Module {
	return &Module{log: log}
}

func (m *Module) Name() string { return "network" }

func (m *Module) Commands() []modules.BotCommand {
	return []modules.BotCommand{
		{Command: "/ping", Description: "ping <хост> - проверка доступности", MinRole: "viewer"},
		{Command: "/traceroute", Description: "traceroute <хост> - маршрут до хоста", MinRole: "viewer"},
		{Command: "/nslookup", Description: "nslookup <хост> - DNS-запрос", MinRole: "viewer"},
	}
}

func (m *Module) Handle(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	switch msg.Command() {
	case "ping":
		return m.handlePing(ctx, bot, msg)
	case "traceroute":
		return m.handleTraceroute(ctx, bot, msg)
	case "nslookup":
		return m.handleNslookup(ctx, bot, msg)
	}
	return nil
}

func (m *Module) handlePing(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	host, err := m.validateHost(bot, msg)
	if err != nil {
		return nil
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	out, err := runCmd(cmdCtx, "ping", "-c", "4", "-W", "3", host)
	if err != nil {
		m.log.Warn("ping завершился с ошибкой", zap.String("host", host), zap.Error(err))
	}
	return replyPre(bot, msg.Chat.ID, fmt.Sprintf("ping %s\n\n%s", host, strings.TrimSpace(out)))
}

func (m *Module) handleTraceroute(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	host, err := m.validateHost(bot, msg)
	if err != nil {
		return nil
	}

	_, _ = bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Выполняю traceroute, подождите..."))

	cmdCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	out, err := runCmd(cmdCtx, "traceroute", "-m", "15", host)
	if err != nil {
		m.log.Warn("traceroute завершился с ошибкой", zap.String("host", host), zap.Error(err))
	}
	return replyPre(bot, msg.Chat.ID, fmt.Sprintf("traceroute %s\n\n%s", host, strings.TrimSpace(out)))
}

func (m *Module) handleNslookup(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	host, err := m.validateHost(bot, msg)
	if err != nil {
		return nil
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	out, err := runCmd(cmdCtx, "nslookup", host)
	if err != nil {
		m.log.Warn("nslookup завершился с ошибкой", zap.String("host", host), zap.Error(err))
	}
	return replyPre(bot, msg.Chat.ID, fmt.Sprintf("nslookup %s\n\n%s", host, strings.TrimSpace(out)))
}

// validateHost проверяет аргумент команды на допустимые символы.
// Если не прошёл - отправляет ответ и возвращает ошибку.
func (m *Module) validateHost(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) (string, error) {
	host := strings.TrimSpace(msg.CommandArguments())
	if host == "" {
		_ = replyText(bot, msg.Chat.ID, "Укажите хост. Пример: /ping example.com")
		return "", fmt.Errorf("пустой хост")
	}
	if !validHost.MatchString(host) {
		_ = replyText(bot, msg.Chat.ID, "Недопустимый хост. Разрешены только буквы, цифры, точки и дефисы.")
		return "", fmt.Errorf("недопустимый хост: %s", host)
	}
	return host, nil
}

// runCmd запускает команду и возвращает combined output
func runCmd(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func replyText(bot *tgbotapi.BotAPI, chatID int64, text string) error {
	_, err := bot.Send(tgbotapi.NewMessage(chatID, text))
	return err
}

func replyPre(bot *tgbotapi.BotAPI, chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, formatter.Pre(formatter.EscapeHTML(text)))
	msg.ParseMode = "HTML"
	_, err := bot.Send(msg)
	return err
}

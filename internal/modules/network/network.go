package network

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"

	"github.com/WELIZARY/tgOps/internal/formatter"
	"github.com/WELIZARY/tgOps/internal/modules"
	internalssh "github.com/WELIZARY/tgOps/internal/ssh"
	"github.com/WELIZARY/tgOps/internal/storage"
)

// validHost разрешает только hostname/IP символы, чтобы не допустить инъекций команд
var validHost = regexp.MustCompile(`^[a-zA-Z0-9.\-]+$`)

// Module - модуль сетевых утилит (/ping, /traceroute, /nslookup)
type Module struct {
	sshClient *internalssh.Client
	src       internalssh.ServerSource
	log       *zap.Logger
}

// New создаёт модуль сетевых утилит
func New(sshClient *internalssh.Client, src internalssh.ServerSource, log *zap.Logger) *Module {
	return &Module{
		sshClient: sshClient,
		src:       src,
		log:       log,
	}
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
		return nil // ответ уже отправлен
	}
	srv, err := m.firstServer(ctx)
	if err != nil {
		return replyText(bot, msg.Chat.ID, "Серверы не настроены.")
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	out, err := m.sshClient.Run(cmdCtx, internalssh.SpecFromServer(srv),
		fmt.Sprintf("ping -c 4 -W 3 %s", host))
	if err != nil {
		m.log.Warn("ping завершился с ошибкой", zap.String("host", host), zap.Error(err))
	}
	return replyPre(bot, msg.Chat.ID,
		fmt.Sprintf("ping %s (через %s)\n\n%s", host, srv.Name, strings.TrimSpace(out)))
}

func (m *Module) handleTraceroute(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	host, err := m.validateHost(bot, msg)
	if err != nil {
		return nil
	}
	srv, err := m.firstServer(ctx)
	if err != nil {
		return replyText(bot, msg.Chat.ID, "Серверы не настроены.")
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	out, err := m.sshClient.Run(cmdCtx, internalssh.SpecFromServer(srv),
		fmt.Sprintf("traceroute -m 15 %s", host))
	if err != nil {
		m.log.Warn("traceroute завершился с ошибкой", zap.String("host", host), zap.Error(err))
	}
	return replyPre(bot, msg.Chat.ID,
		fmt.Sprintf("traceroute %s (через %s)\n\n%s", host, srv.Name, strings.TrimSpace(out)))
}

func (m *Module) handleNslookup(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	host, err := m.validateHost(bot, msg)
	if err != nil {
		return nil
	}
	srv, err := m.firstServer(ctx)
	if err != nil {
		return replyText(bot, msg.Chat.ID, "Серверы не настроены.")
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	out, err := m.sshClient.Run(cmdCtx, internalssh.SpecFromServer(srv),
		fmt.Sprintf("nslookup %s", host))
	if err != nil {
		m.log.Warn("nslookup завершился с ошибкой", zap.String("host", host), zap.Error(err))
	}
	return replyPre(bot, msg.Chat.ID,
		fmt.Sprintf("nslookup %s (через %s)\n\n%s", host, srv.Name, strings.TrimSpace(out)))
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

// firstServer возвращает первый доступный сервер из источника
func (m *Module) firstServer(ctx context.Context) (*storage.Server, error) {
	servers, err := m.src.GetServers(ctx)
	if err != nil || len(servers) == 0 {
		return nil, fmt.Errorf("серверов нет")
	}
	return servers[0], nil
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

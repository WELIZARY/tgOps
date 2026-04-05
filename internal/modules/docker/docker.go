package docker

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

// validContainer разрешает только безопасные символы в имени или id контейнера
var validContainer = regexp.MustCompile(`^[a-zA-Z0-9_\-.]+$`)

// Module - модуль просмотра Docker-контейнеров (/docker ps|logs|images)
type Module struct {
	sshClient *internalssh.Client
	src       internalssh.ServerSource
	cfg       *config.DockerConfig
	log       *zap.Logger
}

// New создаёт модуль docker
func New(sshClient *internalssh.Client, src internalssh.ServerSource, cfg *config.DockerConfig, log *zap.Logger) *Module {
	return &Module{sshClient: sshClient, src: src, cfg: cfg, log: log}
}

func (m *Module) Name() string { return "docker" }

func (m *Module) Commands() []modules.BotCommand {
	return []modules.BotCommand{
		{Command: "/docker", Description: "docker ps|logs|images [аргументы] - контейнеры", MinRole: "operator"},
	}
}

func (m *Module) Handle(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	args := strings.Fields(msg.CommandArguments())
	if len(args) == 0 {
		return replyText(bot, msg.Chat.ID,
			"Использование:\n"+
				"  /docker ps [сервер]\n"+
				"  /docker logs <контейнер> [сервер]\n"+
				"  /docker images [сервер]",
		)
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "ps":
		return m.handlePS(ctx, bot, msg, rest)
	case "logs":
		return m.handleLogs(ctx, bot, msg, rest)
	case "images":
		return m.handleImages(ctx, bot, msg, rest)
	default:
		return replyText(bot, msg.Chat.ID,
			fmt.Sprintf("Неизвестная подкоманда %q. Доступны: ps, logs, images", sub))
	}
}

func (m *Module) handlePS(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message, args []string) error {
	srv, _, err := m.resolveServer(ctx, args)
	if err != nil {
		return replyText(bot, msg.Chat.ID, err.Error())
	}

	cmdCtx, cancel := context.WithTimeout(ctx, m.timeout())
	defer cancel()

	out, runErr := m.sshClient.Run(cmdCtx, internalssh.SpecFromServer(srv),
		"docker ps --format 'table {{.Names}}\\t{{.Image}}\\t{{.Status}}\\t{{.Ports}}' 2>&1")
	if runErr != nil {
		m.log.Warn("docker ps: ошибка", zap.String("server", srv.Name), zap.Error(runErr))
	}
	out = strings.TrimSpace(out)
	if out == "" {
		out = "нет запущенных контейнеров"
	}

	header := fmt.Sprintf("%s @ %s\n\n", formatter.Bold("Docker PS"), formatter.EscapeHTML(srv.Name))
	return m.sendOutput(bot, msg.Chat.ID, header, out, "docker-ps")
}

func (m *Module) handleLogs(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message, args []string) error {
	if len(args) == 0 {
		return replyText(bot, msg.Chat.ID, "Укажите имя контейнера. Пример: /docker logs nginx")
	}

	container := args[0]
	if !validContainer.MatchString(container) {
		return replyText(bot, msg.Chat.ID, "Недопустимое имя контейнера")
	}

	srv, _, err := m.resolveServer(ctx, args[1:])
	if err != nil {
		return replyText(bot, msg.Chat.ID, err.Error())
	}

	cmdCtx, cancel := context.WithTimeout(ctx, m.timeout())
	defer cancel()

	cmd := fmt.Sprintf("docker logs --tail 100 --timestamps %s 2>&1", container)
	out, runErr := m.sshClient.Run(cmdCtx, internalssh.SpecFromServer(srv), cmd)
	if runErr != nil {
		m.log.Warn("docker logs: ошибка",
			zap.String("container", container),
			zap.String("server", srv.Name),
			zap.Error(runErr),
		)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		out = "журнал пуст"
	}

	header := fmt.Sprintf("%s %s @ %s\n\n",
		formatter.Bold("Logs:"),
		formatter.EscapeHTML(container),
		formatter.EscapeHTML(srv.Name),
	)
	return m.sendOutput(bot, msg.Chat.ID, header, out, "docker-logs-"+container)
}

func (m *Module) handleImages(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message, args []string) error {
	srv, _, err := m.resolveServer(ctx, args)
	if err != nil {
		return replyText(bot, msg.Chat.ID, err.Error())
	}

	cmdCtx, cancel := context.WithTimeout(ctx, m.timeout())
	defer cancel()

	out, runErr := m.sshClient.Run(cmdCtx, internalssh.SpecFromServer(srv),
		"docker images --format 'table {{.Repository}}\\t{{.Tag}}\\t{{.Size}}\\t{{.CreatedSince}}' 2>&1")
	if runErr != nil {
		m.log.Warn("docker images: ошибка", zap.String("server", srv.Name), zap.Error(runErr))
	}
	out = strings.TrimSpace(out)
	if out == "" {
		out = "нет образов"
	}

	header := fmt.Sprintf("%s @ %s\n\n", formatter.Bold("Docker Images"), formatter.EscapeHTML(srv.Name))
	return m.sendOutput(bot, msg.Chat.ID, header, out, "docker-images")
}

// resolveServer ищет сервер среди доступных.
// если последний аргумент совпадает с именем сервера - использует его и убирает из args.
// иначе возвращает первый сервер.
func (m *Module) resolveServer(ctx context.Context, args []string) (*storage.Server, []string, error) {
	servers, err := m.src.GetServers(ctx)
	if err != nil || len(servers) == 0 {
		return nil, nil, fmt.Errorf("серверы не настроены")
	}

	if len(args) > 0 {
		last := args[len(args)-1]
		for _, s := range servers {
			if s.Name == last {
				return s, args[:len(args)-1], nil
			}
		}
	}

	return servers[0], args, nil
}

// timeout возвращает таймаут из конфига или 30s по умолчанию
func (m *Module) timeout() time.Duration {
	d, err := time.ParseDuration(m.cfg.Timeout)
	if err != nil {
		return 30 * time.Second
	}
	return d
}

// sendOutput отправляет вывод: 1-3 части сообщениями, больше - файлом
func (m *Module) sendOutput(bot *tgbotapi.BotAPI, chatID int64, header, out, filePrefix string) error {
	const maxChars = 3800
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

	ts := time.Now().Format("20060102-150405")
	fileName := fmt.Sprintf("%s-%s.txt", filePrefix, ts)
	doc := tgbotapi.NewDocument(chatID, tgbotapi.FileReader{
		Name:   fileName,
		Reader: strings.NewReader(out),
	})
	doc.Caption = fmt.Sprintf("Вывод %s (файл, слишком большой для сообщения)", filePrefix)
	_, err := bot.Send(doc)
	return err
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

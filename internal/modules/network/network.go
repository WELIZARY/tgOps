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
	internalssh "github.com/WELIZARY/tgOps/internal/ssh"
	"github.com/WELIZARY/tgOps/internal/storage"
)

// validHost разрешает только hostname/IP символы, чтобы не допустить инъекций команд
var validHost = regexp.MustCompile(`^[a-zA-Z0-9.\-]+$`)

// Module - модуль сетевых утилит (/ping, /traceroute, /nslookup)
// по умолчанию выполняет команды локально через os/exec.
// если указан `from <server>` - выполняет через SSH на указанном сервере.
// если первый аргумент - имя сервера из конфига/бд, оно резолвится в IP.
type Module struct {
	sshClient *internalssh.Client
	src       internalssh.ServerSource
	log       *zap.Logger
}

// New создаёт модуль сетевых утилит
func New(sshClient *internalssh.Client, src internalssh.ServerSource, log *zap.Logger) *Module {
	return &Module{sshClient: sshClient, src: src, log: log}
}

func (m *Module) Name() string { return "network" }

func (m *Module) Commands() []modules.BotCommand {
	return []modules.BotCommand{
		{Command: "/ping", Description: "ping <хост> [from <сервер>] - проверка доступности", MinRole: "viewer"},
		{Command: "/traceroute", Description: "traceroute <хост> [from <сервер>] - маршрут до хоста", MinRole: "viewer"},
		{Command: "/nslookup", Description: "nslookup <хост> [from <сервер>] - DNS-запрос", MinRole: "viewer"},
	}
}

func (m *Module) Handle(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	switch msg.Command() {
	case "ping":
		return m.run(ctx, bot, msg, "ping", []string{"-c", "4", "-W", "3"}, 15*time.Second, false)
	case "traceroute":
		return m.run(ctx, bot, msg, "traceroute", []string{"-m", "15"}, 60*time.Second, true)
	case "nslookup":
		return m.run(ctx, bot, msg, "nslookup", nil, 15*time.Second, false)
	}
	return nil
}

// run - универсальный обработчик. resolveHost: если первый аргумент - имя сервера из
// списка, подменяет его на host. fromServer: если задан "from <name>", команда уходит
// по SSH на указанный сервер. notice - отправлять ли "выполняю..." перед запуском.
func (m *Module) run(
	ctx context.Context,
	bot *tgbotapi.BotAPI,
	msg *tgbotapi.Message,
	tool string,
	extraArgs []string,
	timeout time.Duration,
	notice bool,
) error {
	host, fromSrv, origInput, err := m.parseArgs(ctx, msg)
	if err != nil {
		return replyText(bot, msg.Chat.ID, err.Error())
	}

	if notice {
		_, _ = bot.Send(tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("Выполняю %s, подождите...", tool)))
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var out string
	var runErr error
	header := fmt.Sprintf("%s %s", tool, origInput)

	if fromSrv != nil {
		// собираем shell-команду для удалённого выполнения
		shellArgs := append([]string{tool}, extraArgs...)
		shellArgs = append(shellArgs, host)
		out, runErr = m.sshClient.Run(cmdCtx, internalssh.SpecFromServer(fromSrv), strings.Join(shellArgs, " "))
		header = fmt.Sprintf("%s %s @ %s", tool, origInput, fromSrv.Name)
	} else {
		args := append(extraArgs, host)
		out, runErr = runCmd(cmdCtx, tool, args...)
	}

	if runErr != nil {
		m.log.Warn(tool+" завершился с ошибкой", zap.String("host", host), zap.Error(runErr))
	}

	return replyPre(bot, msg.Chat.ID, fmt.Sprintf("%s\n\n%s", header, strings.TrimSpace(out)))
}

// parseArgs разбирает аргументы команды:
//
//	<host>                       — пинг с хоста бота
//	<server-name>                — резолвит имя сервера в его IP, пинг с хоста бота
//	<host> from <server>         — пинг через SSH с указанного сервера
//	<host> <server>              — то же самое (краткая форма)
//
// возвращает: целевой хост, сервер-источник (или nil), оригинальную строку аргументов.
func (m *Module) parseArgs(ctx context.Context, msg *tgbotapi.Message) (string, *storage.Server, string, error) {
	raw := strings.TrimSpace(msg.CommandArguments())
	if raw == "" {
		return "", nil, "", fmt.Errorf("укажите хост. Примеры:\n  /ping example.com\n  /ping example.com k8s-c2\n  /ping vps-prod  (резолвится в IP сервера)")
	}

	tokens := strings.Fields(raw)
	// убираем "from" если он там есть (синтаксис: host from server)
	cleaned := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if strings.EqualFold(t, "from") {
			continue
		}
		cleaned = append(cleaned, t)
	}

	if len(cleaned) == 0 {
		return "", nil, "", fmt.Errorf("укажите хост")
	}

	servers, _ := m.src.GetServers(ctx)
	findServer := func(name string) *storage.Server {
		for _, s := range servers {
			if s.Name == name {
				return s
			}
		}
		return nil
	}

	host := cleaned[0]
	var fromSrv *storage.Server

	// второй токен - возможно имя сервера-источника
	if len(cleaned) >= 2 {
		if s := findServer(cleaned[1]); s != nil {
			fromSrv = s
		}
	}

	// первый токен - возможно имя сервера, тогда резолвим его в IP
	if s := findServer(host); s != nil {
		host = s.Host
	}

	if !validHost.MatchString(host) {
		return "", nil, "", fmt.Errorf("недопустимый хост %q. Разрешены буквы, цифры, точки и дефисы", host)
	}

	return host, fromSrv, raw, nil
}

// runCmd запускает локальную команду и возвращает combined output
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

package scan

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

// Module - модуль сканирования уязвимостей (/scan)
type Module struct {
	sshClient *internalssh.Client
	src       internalssh.ServerSource
	cfg       *config.ScanConfig
	log       *zap.Logger
}

// New создаёт модуль scan
func New(sshClient *internalssh.Client, src internalssh.ServerSource, cfg *config.ScanConfig, log *zap.Logger) *Module {
	return &Module{sshClient: sshClient, src: src, cfg: cfg, log: log}
}

func (m *Module) Name() string { return "scan" }

func (m *Module) Commands() []modules.BotCommand {
	return []modules.BotCommand{
		{Command: "/scan", Description: "scan [host|<image>] [сервер] - сканирование уязвимостей", MinRole: "operator"},
	}
}

func (m *Module) Handle(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	args := strings.Fields(msg.CommandArguments())

	// последний аргумент может быть именем сервера
	srv, scanArgs, err := m.resolveServerLast(ctx, args)
	if err != nil {
		return replyText(bot, msg.Chat.ID, err.Error())
	}

	// определяем что сканируем: образ или хост
	target := "host"
	if len(scanArgs) > 0 && scanArgs[0] != "host" {
		target = scanArgs[0]
	}

	timeout, parseErr := time.ParseDuration(m.cfg.Timeout)
	if parseErr != nil {
		timeout = 5 * time.Minute
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	var out string
	var runErr error

	if isDockerImage(target) {
		out, runErr = m.scanImage(cmdCtx, srv, target)
	} else {
		out, runErr = m.scanHost(cmdCtx, srv)
	}

	if runErr != nil {
		m.log.Warn("scan: ошибка SSH", zap.String("server", srv.Name), zap.Error(runErr))
	}

	elapsed := time.Since(start).Round(time.Second)
	return m.sendResult(bot, msg.Chat.ID, srv, target, out, elapsed)
}

// scanImage запускает trivy через docker run на сервере
func (m *Module) scanImage(ctx context.Context, srv *storage.Server, image string) (string, error) {
	trivyImage := m.cfg.TrivyImage
	cmd := fmt.Sprintf(
		`docker run --rm -v /var/run/docker.sock:/var/run/docker.sock `+
			`%s image --severity HIGH,CRITICAL --no-progress --quiet %s 2>&1 || `+
			`trivy image --severity HIGH,CRITICAL --no-progress --quiet %s 2>&1`,
		trivyImage, image, image,
	)
	return m.sshClient.Run(ctx, internalssh.SpecFromServer(srv), cmd)
}

// scanHost запускает lynis audit на сервере
func (m *Module) scanHost(ctx context.Context, srv *storage.Server) (string, error) {
	cmd := `
if command -v lynis >/dev/null 2>&1; then
    lynis audit system --quiet --no-colors 2>/dev/null | grep -E "^\s*(\[WARNING\]|\[SUGGESTION\])" | head -50
else
    echo "LYNIS_NOT_FOUND"
fi`
	return m.sshClient.Run(ctx, internalssh.SpecFromServer(srv), cmd)
}

// sendResult форматирует и отправляет результат сканирования
func (m *Module) sendResult(
	bot *tgbotapi.BotAPI,
	chatID int64,
	srv *storage.Server,
	target, out string,
	elapsed time.Duration,
) error {
	out = strings.TrimSpace(out)

	srvName := formatter.EscapeHTML(srv.Name)

	// lynis не установлен
	if out == "LYNIS_NOT_FOUND" {
		text := fmt.Sprintf(
			"%s @ %s\n\nlynis не установлен. Установите:\n<code>apt install lynis</code>",
			formatter.Bold("Scan host"), srvName,
		)
		reply := tgbotapi.NewMessage(chatID, text)
		reply.ParseMode = "HTML"
		_, err := bot.Send(reply)
		return err
	}

	// пустой результат - уязвимостей нет
	if out == "" {
		var title string
		if isDockerImage(target) {
			title = fmt.Sprintf("✅ %s — уязвимостей HIGH/CRITICAL не обнаружено",
				formatter.Bold(formatter.EscapeHTML(target)))
		} else {
			title = fmt.Sprintf("✅ %s @ %s — предупреждений lynis нет",
				formatter.Bold("Scan"), srvName)
		}
		title += fmt.Sprintf("\n\nСканирование за %s", elapsed)
		reply := tgbotapi.NewMessage(chatID, title)
		reply.ParseMode = "HTML"
		_, err := bot.Send(reply)
		return err
	}

	var header string
	if isDockerImage(target) {
		header = fmt.Sprintf("⚠️ %s @ %s\n\n",
			formatter.Bold(formatter.EscapeHTML(target)), srvName)
	} else {
		header = fmt.Sprintf("⚠️ %s @ %s\n\n",
			formatter.Bold("Lynis"), srvName)
	}
	footer := fmt.Sprintf("\n\nСканирование за %s", elapsed)

	const maxChars = 3200
	if len(out) > maxChars {
		ts := time.Now().Format("20060102-150405")
		doc := tgbotapi.NewDocument(chatID, tgbotapi.FileReader{
			Name:   fmt.Sprintf("scan-%s.txt", ts),
			Reader: strings.NewReader(out),
		})
		doc.Caption = header + footer
		_, err := bot.Send(doc)
		return err
	}

	text := header + formatter.Pre(formatter.EscapeHTML(out)) + footer
	reply := tgbotapi.NewMessage(chatID, text)
	reply.ParseMode = "HTML"
	_, err := bot.Send(reply)
	return err
}

// isDockerImage проверяет что аргумент похож на имя docker-образа
func isDockerImage(s string) bool {
	if s == "host" {
		return false
	}
	// образ содержит : (тег) или / (реестр/имя) или известные имена
	return strings.Contains(s, ":") || strings.Contains(s, "/")
}

// resolveServerLast ищет сервер по последнему аргументу, остальные возвращает как аргументы команды
func (m *Module) resolveServerLast(ctx context.Context, args []string) (*storage.Server, []string, error) {
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

func replyText(bot *tgbotapi.BotAPI, chatID int64, text string) error {
	_, err := bot.Send(tgbotapi.NewMessage(chatID, text))
	return err
}

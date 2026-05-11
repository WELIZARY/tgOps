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
// Поддерживает явные подкоманды:
//
//	/scan host [сервер|all]            - lynis на хосте(ах)
//	/scan image <образ> [сервер|all]   - trivy для docker-образа
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
		{Command: "/scan", Description: "scan host|image <образ> [сервер|all] - сканирование", MinRole: "operator"},
	}
}

const usageText = "Использование:\n" +
	"  /scan host [сервер|all]           — lynis на хосте\n" +
	"  /scan image <образ> [сервер|all]  — trivy для docker-образа\n\n" +
	"Примеры:\n" +
	"  /scan host vps-prod\n" +
	"  /scan host all\n" +
	"  /scan image nginx:latest k8s-c2"

func (m *Module) Handle(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	args := strings.Fields(msg.CommandArguments())
	if len(args) == 0 {
		// без аргументов - кнопки выбора подкоманды
		return m.askSubcommand(bot, msg.Chat.ID)
	}

	switch args[0] {
	case "host":
		// /scan host без сервера - показываем кнопки серверов
		if len(args) == 1 {
			return m.askServer(ctx, bot, msg.Chat.ID, "host", "")
		}
		return m.handleHost(ctx, bot, msg.Chat.ID, args[1:])
	case "image":
		if len(args) < 2 {
			return replyText(bot, msg.Chat.ID, "Укажите имя образа.\n\n"+usageText)
		}
		// /scan image <образ> без сервера - кнопки серверов
		if len(args) == 2 {
			return m.askServer(ctx, bot, msg.Chat.ID, "image", args[1])
		}
		return m.handleImage(ctx, bot, msg.Chat.ID, args[1:])
	default:
		return replyText(bot, msg.Chat.ID,
			fmt.Sprintf("Неизвестная подкоманда %q.\n\n%s", args[0], usageText))
	}
}

// HandleCallback обрабатывает inline-кнопки scan.
// callback data:
//
//	"scan_sub_host"               - выбор подкоманды host (потом серверы)
//	"scan_sub_image"              - выбор image (запрашиваем имя образа текстом)
//	"scan_host_<server|all>"      - финальное выполнение host
func (m *Module) HandleCallback(ctx context.Context, bot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery) error {
	_, _ = bot.Request(tgbotapi.NewCallback(query.ID, ""))
	data := query.Data

	if strings.HasPrefix(data, "scan_sub_") {
		sub := strings.TrimPrefix(data, "scan_sub_")
		hideKeyboard(bot, query)
		if sub == "image" {
			// для image нужно имя образа - просим текстом
			return replyText(bot, query.Message.Chat.ID,
				"Введите команду:\n/scan image <имя_образа> [сервер|all]\n\nПример: /scan image nginx:latest k8s-c2")
		}
		return m.askServer(ctx, bot, query.Message.Chat.ID, sub, "")
	}

	if strings.HasPrefix(data, "scan_host_") {
		name := strings.TrimPrefix(data, "scan_host_")
		hideKeyboard(bot, query)
		return m.handleHost(ctx, bot, query.Message.Chat.ID, []string{name})
	}
	return nil
}

// askSubcommand показывает кнопки выбора host/image
func (m *Module) askSubcommand(bot *tgbotapi.BotAPI, chatID int64) error {
	buttons := []formatter.ButtonRow{
		{Label: "хост (lynis)", Data: "scan_sub_host"},
		{Label: "docker-образ (trivy)", Data: "scan_sub_image"},
	}
	msg := tgbotapi.NewMessage(chatID, "Что сканировать?")
	msg.ReplyMarkup = formatter.SubcommandKeyboard(buttons)
	_, err := bot.Send(msg)
	return err
}

// askServer показывает кнопки серверов с опцией "все" (только для host;
// для image после кнопок ещё нужно имя образа - тут не используется)
func (m *Module) askServer(ctx context.Context, bot *tgbotapi.BotAPI, chatID int64, sub, _ string) error {
	servers, err := m.src.GetServers(ctx)
	if err != nil || len(servers) == 0 {
		return replyText(bot, chatID, "Серверы не настроены.")
	}
	prompt := "Выберите сервер для /scan " + sub + ":"
	msg := tgbotapi.NewMessage(chatID, prompt)
	msg.ReplyMarkup = formatter.ServerKeyboardWithAll(servers, "scan_"+sub+"_")
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

// handleHost - lynis audit на одном сервере или сразу на всех (all)
func (m *Module) handleHost(ctx context.Context, bot *tgbotapi.BotAPI, chatID int64, args []string) error {
	targetServers, err := m.resolveTargets(ctx, args)
	if err != nil {
		return replyText(bot, chatID, err.Error())
	}

	if len(targetServers) > 1 {
		_, _ = bot.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("Запускаю lynis на %d серверах, подождите...", len(targetServers))))
	}

	timeout := m.timeout()
	for _, srv := range targetServers {
		cmdCtx, cancel := context.WithTimeout(ctx, timeout)
		start := time.Now()
		out, runErr := m.scanHost(cmdCtx, srv)
		cancel()

		if runErr != nil {
			m.log.Warn("scan host: ошибка SSH", zap.String("server", srv.Name), zap.Error(runErr))
		}

		elapsed := time.Since(start).Round(time.Second)
		if err := m.sendResult(bot, chatID, srv, "host", "", out, elapsed); err != nil {
			return err
		}
	}
	return nil
}

// handleImage - trivy для образа на одном сервере или на всех (all)
func (m *Module) handleImage(ctx context.Context, bot *tgbotapi.BotAPI, chatID int64, args []string) error {
	if len(args) == 0 {
		return replyText(bot, chatID, "Укажите имя образа.\n\n"+usageText)
	}
	image := args[0]
	rest := args[1:]

	targetServers, err := m.resolveTargets(ctx, rest)
	if err != nil {
		return replyText(bot, chatID, err.Error())
	}

	if len(targetServers) > 1 {
		_, _ = bot.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("Сканирую образ %s на %d серверах, подождите...", image, len(targetServers))))
	}

	timeout := m.timeout()
	for _, srv := range targetServers {
		cmdCtx, cancel := context.WithTimeout(ctx, timeout)
		start := time.Now()
		out, runErr := m.scanImage(cmdCtx, srv, image)
		cancel()

		if runErr != nil {
			m.log.Warn("scan image: ошибка SSH", zap.String("server", srv.Name), zap.Error(runErr))
		}

		elapsed := time.Since(start).Round(time.Second)
		if err := m.sendResult(bot, chatID, srv, "image", image, out, elapsed); err != nil {
			return err
		}
	}
	return nil
}

// resolveTargets возвращает список серверов для сканирования.
// args: пусто - первый из списка, "all" - все, имя - конкретный
func (m *Module) resolveTargets(ctx context.Context, args []string) ([]*storage.Server, error) {
	servers, err := m.src.GetServers(ctx)
	if err != nil || len(servers) == 0 {
		return nil, fmt.Errorf("серверы не настроены")
	}

	if len(args) == 0 {
		return []*storage.Server{servers[0]}, nil
	}

	target := args[0]
	if target == "all" {
		return servers, nil
	}

	for _, s := range servers {
		if s.Name == target {
			return []*storage.Server{s}, nil
		}
	}
	names := make([]string, len(servers))
	for i, s := range servers {
		names[i] = s.Name
	}
	return nil, fmt.Errorf("сервер %q не найден. Доступные: %s, all", target, strings.Join(names, ", "))
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
	target, image, out string,
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

	// пустой результат - всё ок
	if out == "" {
		var title string
		if target == "image" {
			title = fmt.Sprintf("✅ %s @ %s — уязвимостей HIGH/CRITICAL не обнаружено",
				formatter.Bold(formatter.EscapeHTML(image)), srvName)
		} else {
			title = fmt.Sprintf("✅ %s @ %s — предупреждений lynis нет",
				formatter.Bold("Scan host"), srvName)
		}
		title += fmt.Sprintf("\n\nСканирование за %s", elapsed)
		reply := tgbotapi.NewMessage(chatID, title)
		reply.ParseMode = "HTML"
		_, err := bot.Send(reply)
		return err
	}

	var header string
	if target == "image" {
		header = fmt.Sprintf("⚠️ %s @ %s\n\n",
			formatter.Bold(formatter.EscapeHTML(image)), srvName)
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

// timeout возвращает таймаут из конфига или 5m по умолчанию
func (m *Module) timeout() time.Duration {
	d, err := time.ParseDuration(m.cfg.Timeout)
	if err != nil {
		return 5 * time.Minute
	}
	return d
}

func replyText(bot *tgbotapi.BotAPI, chatID int64, text string) error {
	_, err := bot.Send(tgbotapi.NewMessage(chatID, text))
	return err
}

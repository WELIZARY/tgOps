package updates

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

// Module - модуль проверки доступных обновлений пакетов (/updates)
type Module struct {
	sshClient *internalssh.Client
	src       internalssh.ServerSource
	cfg       *config.UpdatesConfig
	log       *zap.Logger
}

// New создаёт модуль updates
func New(sshClient *internalssh.Client, src internalssh.ServerSource, cfg *config.UpdatesConfig, log *zap.Logger) *Module {
	return &Module{sshClient: sshClient, src: src, cfg: cfg, log: log}
}

func (m *Module) Name() string { return "updates" }

func (m *Module) Commands() []modules.BotCommand {
	return []modules.BotCommand{
		{Command: "/updates", Description: "updates [сервер] - доступные обновления пакетов", MinRole: "viewer"},
	}
}

func (m *Module) Handle(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	args := strings.Fields(msg.CommandArguments())

	srv, err := m.resolveServer(ctx, args)
	if err != nil {
		return replyText(bot, msg.Chat.ID, err.Error())
	}

	timeout, parseErr := time.ParseDuration(m.cfg.Timeout)
	if parseErr != nil {
		timeout = 60 * time.Second
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// одна команда автоматически определяет пакетный менеджер
	sshCmd := `
if command -v apt-get >/dev/null 2>&1; then
    apt-get -qq update 2>/dev/null
    echo "PKG_MGR=apt"
    apt list --upgradable 2>/dev/null | grep -v "^Listing"
elif command -v dnf >/dev/null 2>&1; then
    echo "PKG_MGR=dnf"
    dnf check-update --quiet 2>/dev/null || true
elif command -v yum >/dev/null 2>&1; then
    echo "PKG_MGR=yum"
    yum check-update --quiet 2>/dev/null || true
else
    echo "PKG_MGR=unknown"
fi`

	out, runErr := m.sshClient.Run(cmdCtx, internalssh.SpecFromServer(srv), sshCmd)
	if runErr != nil {
		m.log.Warn("updates: ошибка SSH", zap.String("server", srv.Name), zap.Error(runErr))
	}

	pkgMgr, lines := parseOutput(out)
	pkgs := parsePackages(pkgMgr, lines)

	return m.sendResult(bot, msg.Chat.ID, srv, pkgMgr, pkgs)
}

// sendResult форматирует и отправляет результат проверки обновлений
func (m *Module) sendResult(bot *tgbotapi.BotAPI, chatID int64, srv *storage.Server, pkgMgr string, pkgs []updateEntry) error {
	now := time.Now().Format("02.01.2006 15:04")

	if pkgMgr == "unknown" {
		text := fmt.Sprintf("%s — пакетный менеджер не определён\nПроверено: %s",
			formatter.Bold(formatter.EscapeHTML(srv.Name)), now)
		reply := tgbotapi.NewMessage(chatID, text)
		reply.ParseMode = "HTML"
		_, err := bot.Send(reply)
		return err
	}

	if len(pkgs) == 0 {
		text := fmt.Sprintf("✅ %s — обновлений нет\nПроверено: %s",
			formatter.Bold(formatter.EscapeHTML(srv.Name)), now)
		reply := tgbotapi.NewMessage(chatID, text)
		reply.ParseMode = "HTML"
		_, err := bot.Send(reply)
		return err
	}

	// показываем первые 20 пакетов, остальное считаем
	shown := pkgs
	extra := 0
	if len(pkgs) > 20 {
		shown = pkgs[:20]
		extra = len(pkgs) - 20
	}

	var sb strings.Builder
	for _, p := range shown {
		if p.newVersion != "" && p.oldVersion != "" {
			fmt.Fprintf(&sb, "%-28s %s (было %s)\n",
				truncate(p.name, 28), p.newVersion, p.oldVersion)
		} else if p.newVersion != "" {
			fmt.Fprintf(&sb, "%-28s %s\n", truncate(p.name, 28), p.newVersion)
		} else {
			fmt.Fprintf(&sb, "%s\n", p.name)
		}
	}
	if extra > 0 {
		fmt.Fprintf(&sb, "... ещё %d", extra)
	}

	header := fmt.Sprintf("⚠️ %s — доступно %d обновлений\nПроверено: %s\n\n",
		formatter.Bold(formatter.EscapeHTML(srv.Name)), len(pkgs), now)

	reply := tgbotapi.NewMessage(chatID, header+formatter.Pre(formatter.EscapeHTML(sb.String())))
	reply.ParseMode = "HTML"
	_, err := bot.Send(reply)
	return err
}

// updateEntry - одно доступное обновление
type updateEntry struct {
	name       string
	newVersion string
	oldVersion string
}

// parseOutput извлекает тип пакетного менеджера и строки пакетов из вывода
func parseOutput(out string) (pkgMgr string, lines []string) {
	pkgMgr = "unknown"
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "PKG_MGR=") {
			pkgMgr = strings.TrimPrefix(line, "PKG_MGR=")
			continue
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	return pkgMgr, lines
}

// parsePackages парсит строки вывода в список пакетов с версиями
func parsePackages(pkgMgr string, lines []string) []updateEntry {
	var pkgs []updateEntry
	for _, line := range lines {
		switch pkgMgr {
		case "apt":
			// формат: nginx/jammy-updates 1.24.0-1 amd64 [upgradable from: 1.22.0-1]
			p := parseAptLine(line)
			if p != nil {
				pkgs = append(pkgs, *p)
			}
		case "yum", "dnf":
			// формат: nginx.x86_64   1.24.0-1.el8   baseos
			p := parseYumLine(line)
			if p != nil {
				pkgs = append(pkgs, *p)
			}
		}
	}
	return pkgs
}

func parseAptLine(line string) *updateEntry {
	// nginx/jammy-updates 1.24.0-1ubuntu1 amd64 [upgradable from: 1.22.0-1ubuntu1]
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return nil
	}
	nameWithRepo := parts[0]
	name := nameWithRepo
	if idx := strings.Index(nameWithRepo, "/"); idx > 0 {
		name = nameWithRepo[:idx]
	}
	newVer := parts[1]
	oldVer := ""
	// ищем "upgradable from: X"
	if idx := strings.Index(line, "upgradable from: "); idx > 0 {
		rest := line[idx+len("upgradable from: "):]
		oldVer = strings.TrimRight(rest, "]")
	}
	return &updateEntry{name: name, newVersion: newVer, oldVersion: oldVer}
}

func parseYumLine(line string) *updateEntry {
	// nginx.x86_64   1.24.0-1.el8   baseos
	// пропускаем строки-заголовки и пустые
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return nil
	}
	// пропускаем строки вида "Last metadata..." или "Security:"
	if !strings.Contains(parts[0], ".") {
		return nil
	}
	name := parts[0]
	// убираем архитектуру (nginx.x86_64 -> nginx)
	if idx := strings.LastIndex(name, "."); idx > 0 {
		name = name[:idx]
	}
	return &updateEntry{name: name, newVersion: parts[1]}
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
		return nil, fmt.Errorf("сервер %q не найден. Доступные: %s", name, strings.Join(names, ", "))
	}
	return servers[0], nil
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

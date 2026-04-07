package versions

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

// Module - модуль проверки версий ключевого ПО (/versions)
type Module struct {
	sshClient *internalssh.Client
	src       internalssh.ServerSource
	cfg       *config.VersionsConfig
	log       *zap.Logger
}

// New создаёт модуль versions
func New(sshClient *internalssh.Client, src internalssh.ServerSource, cfg *config.VersionsConfig, log *zap.Logger) *Module {
	return &Module{sshClient: sshClient, src: src, cfg: cfg, log: log}
}

func (m *Module) Name() string { return "versions" }

func (m *Module) Commands() []modules.BotCommand {
	return []modules.BotCommand{
		{Command: "/versions", Description: "versions [сервер] - версии ключевого ПО", MinRole: "viewer"},
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
		timeout = 30 * time.Second
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	sshCmd := m.buildCmd()

	out, runErr := m.sshClient.Run(cmdCtx, internalssh.SpecFromServer(srv), sshCmd)
	if runErr != nil {
		m.log.Warn("versions: ошибка SSH", zap.String("server", srv.Name), zap.Error(runErr))
	}

	entries := parseVersions(out)
	return m.sendResult(bot, msg.Chat.ID, srv, entries)
}

// buildCmd строит SSH-команду для проверки версий
func (m *Module) buildCmd() string {
	// стандартный набор инструментов
	standard := []string{
		`echo "docker=$(docker --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)"`,
		`echo "compose=$(docker compose version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)"`,
		`echo "ansible=$(ansible --version 2>/dev/null | head -1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+'| head -1)"`,
		`echo "go=$(go version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+'| head -1)"`,
		`echo "nginx=$(nginx -v 2>&1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')"`,
		`echo "postgres=$(psql --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+' | head -1)"`,
		`echo "python3=$(python3 --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')"`,
		`echo "node=$(node --version 2>/dev/null | tr -d 'v')"`,
		`echo "kernel=$(uname -r)"`,
		`echo "os=$(lsb_release -rs 2>/dev/null || awk -F= '/^VERSION_ID/{gsub(/"/, "", $2); print $2}' /etc/os-release 2>/dev/null)"`,
	}

	// если в конфиге задан список - берём только нужные
	if len(m.cfg.Packages) > 0 {
		allowed := make(map[string]bool, len(m.cfg.Packages))
		for _, p := range m.cfg.Packages {
			allowed[strings.ToLower(p)] = true
		}
		filtered := standard[:0]
		for _, line := range standard {
			key := extractKey(line)
			if allowed[key] {
				filtered = append(filtered, line)
			}
		}
		standard = filtered
	}

	return strings.Join(standard, "\n")
}

// extractKey вытягивает имя инструмента из строки вида `echo "key=..."`
func extractKey(line string) string {
	start := strings.Index(line, `"`)
	if start < 0 {
		return ""
	}
	rest := line[start+1:]
	end := strings.Index(rest, "=")
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// versionEntry - одна запись версии
type versionEntry struct {
	label   string
	version string
}

// labels содержит читаемые названия для ключей
var labels = map[string]string{
	"docker":   "Docker",
	"compose":  "Compose",
	"ansible":  "Ansible",
	"go":       "Go",
	"nginx":    "Nginx",
	"postgres": "PostgreSQL",
	"python3":  "Python3",
	"node":     "Node.js",
	"kernel":   "Kernel",
	"os":       "OS",
}

// parseVersions разбирает вывод key=value и пропускает пустые значения
func parseVersions(out string) []versionEntry {
	var entries []versionEntry
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if val == "" {
			// инструмент не установлен - пропускаем
			continue
		}
		label := labels[key]
		if label == "" {
			label = key
		}
		entries = append(entries, versionEntry{label: label, version: val})
	}
	return entries
}

// sendResult форматирует и отправляет список версий
func (m *Module) sendResult(bot *tgbotapi.BotAPI, chatID int64, srv *storage.Server, entries []versionEntry) error {
	now := time.Now().Format("02.01.2006 15:04")

	if len(entries) == 0 {
		text := fmt.Sprintf("%s @ %s\n\nНе удалось определить версии ПО.\nПроверено: %s",
			formatter.Bold("Версии ПО"), formatter.EscapeHTML(srv.Name), now)
		reply := tgbotapi.NewMessage(chatID, text)
		reply.ParseMode = "HTML"
		_, err := bot.Send(reply)
		return err
	}

	var sb strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&sb, "%-14s %s\n", e.label, e.version)
	}

	header := fmt.Sprintf("%s @ %s\n\n",
		formatter.Bold("Версии ПО"), formatter.EscapeHTML(srv.Name))
	footer := fmt.Sprintf("\nПроверено: %s", now)

	text := header + formatter.Pre(formatter.EscapeHTML(sb.String())) + footer
	reply := tgbotapi.NewMessage(chatID, text)
	reply.ParseMode = "HTML"
	_, err := bot.Send(reply)
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
		return nil, fmt.Errorf("сервер %q не найден. Доступные: %s", name, strings.Join(names, ", "))
	}
	return servers[0], nil
}

func replyText(bot *tgbotapi.BotAPI, chatID int64, text string) error {
	_, err := bot.Send(tgbotapi.NewMessage(chatID, text))
	return err
}

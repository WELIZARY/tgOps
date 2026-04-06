package backups

import (
	"context"
	"fmt"
	"strconv"
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

// Module - модуль проверки статуса резервных копий (/backups)
type Module struct {
	sshClient *internalssh.Client
	src       internalssh.ServerSource
	cfg       *config.BackupsConfig
	log       *zap.Logger
}

// New создаёт модуль backups
func New(sshClient *internalssh.Client, src internalssh.ServerSource, cfg *config.BackupsConfig, log *zap.Logger) *Module {
	return &Module{sshClient: sshClient, src: src, cfg: cfg, log: log}
}

func (m *Module) Name() string { return "backups" }

func (m *Module) Commands() []modules.BotCommand {
	return []modules.BotCommand{
		{Command: "/backups", Description: "backups [сервер] - статус резервных копий", MinRole: "viewer"},
	}
}

func (m *Module) Handle(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	if len(m.cfg.Paths) == 0 {
		return replyText(bot, msg.Chat.ID,
			"Пути для проверки бэкапов не настроены.\nДобавьте backups.paths в configs/config.yaml")
	}

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

	// проверяем все директории одной SSH-командой
	sshCmd := m.buildCheckCmd()
	out, runErr := m.sshClient.Run(cmdCtx, internalssh.SpecFromServer(srv), sshCmd)
	if runErr != nil {
		m.log.Warn("backups: ошибка SSH", zap.String("server", srv.Name), zap.Error(runErr))
	}

	statuses := m.parseResults(out)
	return m.sendResult(bot, msg.Chat.ID, srv, statuses)
}

// buildCheckCmd формирует bash-команду для проверки всех директорий
func (m *Module) buildCheckCmd() string {
	var sb strings.Builder
	for _, p := range m.cfg.Paths {
		// для каждой директории: маркер, последний файл с датой и размером
		fmt.Fprintf(&sb,
			`echo "=== %s ==="; `+
				`ls -lt %s 2>/dev/null | grep -v "^total" | grep -v "^$" | head -1; `+
				`stat -c "%%Y %%s" $(ls -t %s 2>/dev/null | head -1 | awk '{print "%s/"$NF}') 2>/dev/null || echo "STAT_ERR"; `,
			p.Path, p.Path, p.Path, p.Path,
		)
	}
	return sb.String()
}

// backupStatus - результат проверки одной директории
type backupStatus struct {
	Name        string
	Path        string
	LastFile    string
	ModTime     time.Time
	SizeBytes   int64
	HasData     bool
	AgeOK       bool
	MaxAgeHours int
}

// parseResults разбирает вывод SSH-команды по маркерам "=== path ==="
func (m *Module) parseResults(out string) []backupStatus {
	statuses := make([]backupStatus, len(m.cfg.Paths))
	for i, p := range m.cfg.Paths {
		statuses[i] = backupStatus{
			Name:        p.Name,
			Path:        p.Path,
			MaxAgeHours: p.MaxAgeHours,
			AgeOK:       true,
		}
	}

	sections := splitSections(out, m.cfg.Paths)
	for i, section := range sections {
		if i >= len(statuses) {
			break
		}
		lines := strings.Split(strings.TrimSpace(section), "\n")
		if len(lines) == 0 {
			continue
		}

		// первая строка — вывод ls (имя файла в конце)
		lsLine := strings.TrimSpace(lines[0])
		if lsLine != "" && lsLine != "STAT_ERR" {
			parts := strings.Fields(lsLine)
			if len(parts) > 0 {
				statuses[i].LastFile = parts[len(parts)-1]
				statuses[i].HasData = true
			}
		}

		// вторая строка — вывод stat: "unixtime size_bytes"
		if len(lines) > 1 {
			statLine := strings.TrimSpace(lines[1])
			if statLine != "" && statLine != "STAT_ERR" {
				parts := strings.Fields(statLine)
				if len(parts) >= 2 {
					if unixSec, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
						statuses[i].ModTime = time.Unix(unixSec, 0)
						statuses[i].HasData = true
					}
					if sz, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
						statuses[i].SizeBytes = sz
					}
				}
			}
		}

		// проверяем возраст если задан MaxAgeHours
		if statuses[i].HasData && statuses[i].MaxAgeHours > 0 && !statuses[i].ModTime.IsZero() {
			maxAge := time.Duration(statuses[i].MaxAgeHours) * time.Hour
			statuses[i].AgeOK = time.Since(statuses[i].ModTime) <= maxAge
		}
	}

	return statuses
}

// splitSections разбивает вывод по маркерам "=== path ===" для каждого пути
func splitSections(out string, paths []config.BackupPathConfig) []string {
	sections := make([]string, len(paths))
	for i, p := range paths {
		marker := fmt.Sprintf("=== %s ===", p.Path)
		start := strings.Index(out, marker)
		if start < 0 {
			continue
		}
		start += len(marker)
		// ищем следующий маркер
		end := len(out)
		for j := i + 1; j < len(paths); j++ {
			nextMarker := fmt.Sprintf("=== %s ===", paths[j].Path)
			if idx := strings.Index(out[start:], nextMarker); idx >= 0 {
				end = start + idx
				break
			}
		}
		sections[i] = out[start:end]
	}
	return sections
}

// sendResult форматирует и отправляет итоговый статус бэкапов
func (m *Module) sendResult(bot *tgbotapi.BotAPI, chatID int64, srv *storage.Server, statuses []backupStatus) error {
	now := time.Now().Format("02.01.2006 15:04")
	var sb strings.Builder

	fmt.Fprintf(&sb, "%s @ %s\nПроверено: %s\n\n",
		formatter.Bold("Backups"),
		formatter.EscapeHTML(srv.Name),
		now,
	)

	for _, s := range statuses {
		if !s.HasData {
			fmt.Fprintf(&sb, "❓ %s\n   директория пуста или недоступна\n   %s\n\n",
				formatter.Bold(formatter.EscapeHTML(s.Name)),
				formatter.EscapeHTML(s.Path),
			)
			continue
		}

		emoji := "✅"
		ageNote := ""
		if !s.AgeOK {
			emoji = "⚠️"
			ageNote = fmt.Sprintf(" (ожидалось менее %dч)", s.MaxAgeHours)
		}

		age := ""
		if !s.ModTime.IsZero() {
			age = formatAge(time.Since(s.ModTime)) + " назад" + ageNote
		}

		sizeStr := ""
		if s.SizeBytes > 0 {
			sizeStr = "  |  " + formatter.FormatBytes(uint64(s.SizeBytes)) //nolint:gosec // размер файла неотрицателен
		}

		fmt.Fprintf(&sb, "%s %s\n   %s\n   %s%s\n\n",
			emoji,
			formatter.Bold(formatter.EscapeHTML(s.Name)),
			formatter.EscapeHTML(s.LastFile),
			age,
			sizeStr,
		)
	}

	reply := tgbotapi.NewMessage(chatID, sb.String())
	reply.ParseMode = "HTML"
	_, err := bot.Send(reply)
	return err
}

// formatAge форматирует продолжительность в читаемый вид
func formatAge(d time.Duration) string {
	h := int(d.Hours())
	if h < 1 {
		return fmt.Sprintf("%dм", int(d.Minutes()))
	}
	if h < 24 {
		return fmt.Sprintf("%dч", h)
	}
	days := h / 24
	remH := h % 24
	if remH == 0 {
		return fmt.Sprintf("%dд", days)
	}
	return fmt.Sprintf("%dд %dч", days, remH)
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

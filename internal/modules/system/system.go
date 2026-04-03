package system

import (
	"context"
	"fmt"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"

	"github.com/WELIZARY/tgOps/internal/config"
	"github.com/WELIZARY/tgOps/internal/formatter"
	"github.com/WELIZARY/tgOps/internal/modules"
	internalssh "github.com/WELIZARY/tgOps/internal/ssh"
	"github.com/WELIZARY/tgOps/internal/storage"
)

// Module - модуль системного мониторинга (/status, /top, /health)
type Module struct {
	ssh *internalssh.Client
	src internalssh.ServerSource
	cfg *config.Config
	log *zap.Logger
}

// New создаёт модуль системного мониторинга
func New(sshClient *internalssh.Client, src internalssh.ServerSource, cfg *config.Config, log *zap.Logger) *Module {
	return &Module{
		ssh: sshClient,
		src: src,
		cfg: cfg,
		log: log,
	}
}

func (m *Module) Name() string { return "system" }

func (m *Module) Commands() []modules.BotCommand {
	return []modules.BotCommand{
		{Command: "/status", Description: "состояние сервера (CPU, RAM, Disk) [имя]", MinRole: "viewer"},
		{Command: "/top", Description: "топ процессов по CPU [имя сервера]", MinRole: "viewer"},
		{Command: "/health", Description: "сводка по всем серверам", MinRole: "viewer"},
	}
}

func (m *Module) Handle(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	switch msg.Command() {
	case "status":
		return m.handleStatus(ctx, bot, msg)
	case "top":
		return m.handleTop(ctx, bot, msg)
	case "health":
		return m.handleHealth(ctx, bot, msg)
	}
	return nil
}

func (m *Module) handleStatus(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	srv, err := m.findServer(ctx, msg.CommandArguments())
	if err != nil {
		return replyText(bot, msg.Chat.ID, "Сервер не найден. Список серверов: /health")
	}

	metrics := Collect(ctx, m.ssh, internalssh.SpecFromServer(srv))
	return replyHTML(bot, msg.Chat.ID, formatStatus(srv.Name, metrics, m.cfg.Monitoring.Thresholds))
}

func (m *Module) handleTop(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	srv, err := m.findServer(ctx, msg.CommandArguments())
	if err != nil {
		return replyText(bot, msg.Chat.ID, "Сервер не найден. Список серверов: /health")
	}

	text, err := CollectTop(ctx, m.ssh, internalssh.SpecFromServer(srv), srv.Name)
	if err != nil {
		m.log.Error("ошибка сбора топ процессов", zap.String("server", srv.Name), zap.Error(err))
		return replyText(bot, msg.Chat.ID, fmt.Sprintf("Ошибка получения процессов с %s.", srv.Name))
	}
	return replyHTML(bot, msg.Chat.ID, text)
}

func (m *Module) handleHealth(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	servers, err := m.src.GetServers(ctx)
	if err != nil || len(servers) == 0 {
		return replyText(bot, msg.Chat.ID, "Серверы не настроены. Добавьте серверы в конфиг или таблицу servers.")
	}

	// Опрашиваем все серверы параллельно
	type result struct {
		name    string
		metrics *Metrics
	}
	results := make([]result, len(servers))
	var wg sync.WaitGroup
	for i, srv := range servers {
		wg.Add(1)
		go func(idx int, s *storage.Server) {
			defer wg.Done()
			results[idx] = result{
				name:    s.Name,
				metrics: Collect(ctx, m.ssh, internalssh.SpecFromServer(s)),
			}
		}(i, srv)
	}
	wg.Wait()

	var sb strings.Builder
	sb.WriteString(formatter.Bold("Health Dashboard") + "\n\n")
	for _, r := range results {
		sb.WriteString(formatHealthLine(r.name, r.metrics, m.cfg.Monitoring.Thresholds))
		sb.WriteString("\n")
	}
	return replyHTML(bot, msg.Chat.ID, sb.String())
}

// findServer возвращает сервер по имени из аргумента команды.
// Если аргумент пустой - возвращает первый сервер.
func (m *Module) findServer(ctx context.Context, arg string) (*storage.Server, error) {
	servers, err := m.src.GetServers(ctx)
	if err != nil || len(servers) == 0 {
		return nil, fmt.Errorf("серверов нет")
	}
	name := strings.TrimSpace(arg)
	if name == "" {
		return servers[0], nil
	}
	for _, s := range servers {
		if s.Name == name {
			return s, nil
		}
	}
	return nil, fmt.Errorf("сервер %q не найден", name)
}

// formatStatus формирует карточку состояния одного сервера
func formatStatus(name string, m *Metrics, t config.ThresholdsConfig) string {
	if m.Error != nil {
		return fmt.Sprintf("🔴 %s\n<i>недоступен: %s</i>",
			formatter.Bold(name), formatter.EscapeHTML(m.Error.Error()))
	}

	ramPct := pct(m.RAMUsed, m.RAMTotal)
	dskPct := pct(m.DiskUsed, m.DiskTotal)

	return fmt.Sprintf(
		"%s ✅ online\n\n"+
			"%s CPU:  %s\n"+
			"%s RAM:  %s (%s / %s)\n"+
			"%s Disk: %s (%s / %s)\n\n"+
			"Load:   %.2f %.2f %.2f\n"+
			"Uptime: %s",
		formatter.Bold(name),
		formatter.SeverityEmoji(m.CPU, t.CPUWarning, t.CPUCritical),
		formatter.ProgressBar(m.CPU, 10),
		formatter.SeverityEmoji(ramPct, t.RAMWarning, t.RAMCritical),
		formatter.ProgressBar(ramPct, 10),
		formatter.FormatBytes(m.RAMUsed), formatter.FormatBytes(m.RAMTotal),
		formatter.SeverityEmoji(dskPct, t.DiskWarning, t.DiskCritical),
		formatter.ProgressBar(dskPct, 10),
		formatter.FormatBytes(m.DiskUsed), formatter.FormatBytes(m.DiskTotal),
		m.Load1, m.Load5, m.Load15,
		formatter.FormatDuration(m.Uptime),
	)
}

// formatHealthLine формирует одну строку сводки для /health
func formatHealthLine(name string, m *Metrics, t config.ThresholdsConfig) string {
	if m.Error != nil {
		return fmt.Sprintf("🔴 %s - недоступен", formatter.Bold(name))
	}
	ramPct := pct(m.RAMUsed, m.RAMTotal)
	dskPct := pct(m.DiskUsed, m.DiskTotal)

	return fmt.Sprintf("%s | %s%s | %s%s | %s%s",
		formatter.Bold(name),
		formatter.SeverityEmoji(m.CPU, t.CPUWarning, t.CPUCritical),
		formatter.ProgressBar(m.CPU, 5),
		formatter.SeverityEmoji(ramPct, t.RAMWarning, t.RAMCritical),
		formatter.ProgressBar(ramPct, 5),
		formatter.SeverityEmoji(dskPct, t.DiskWarning, t.DiskCritical),
		formatter.ProgressBar(dskPct, 5),
	)
}

// pct вычисляет процент использования: used/total*100
func pct(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) / float64(total) * 100
}

func replyText(bot *tgbotapi.BotAPI, chatID int64, text string) error {
	_, err := bot.Send(tgbotapi.NewMessage(chatID, text))
	return err
}

func replyHTML(bot *tgbotapi.BotAPI, chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML"
	_, err := bot.Send(msg)
	return err
}

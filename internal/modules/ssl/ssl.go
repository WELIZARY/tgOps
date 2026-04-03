package ssl

import (
	"context"
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"

	"github.com/WELIZARY/tgOps/internal/config"
	"github.com/WELIZARY/tgOps/internal/formatter"
	"github.com/WELIZARY/tgOps/internal/modules"
	"github.com/WELIZARY/tgOps/internal/modules/alerts"
	"github.com/WELIZARY/tgOps/internal/storage"
)

// Module - модуль проверки SSL-сертификатов (/ssl)
type Module struct {
	checker *Checker
	sslRepo storage.SSLRepo
	log     *zap.Logger
}

// New создаёт SSL-модуль
func New(cfg *config.SSLConfig, sslRepo storage.SSLRepo, alertMgr *alerts.Manager, log *zap.Logger) *Module {
	return &Module{
		checker: NewChecker(cfg, sslRepo, alertMgr, log),
		sslRepo: sslRepo,
		log:     log,
	}
}

func (m *Module) Name() string { return "ssl" }

func (m *Module) Commands() []modules.BotCommand {
	return []modules.BotCommand{
		{Command: "/ssl", Description: "статус SSL-сертификатов", MinRole: "viewer"},
	}
}

func (m *Module) Handle(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	checks, err := m.sslRepo.GetAll(ctx)
	if err != nil {
		m.log.Error("ошибка получения SSL-проверок", zap.Error(err))
		return err
	}

	if len(checks) == 0 {
		// Данных в БД нет - запускаем проверку прямо сейчас
		return m.handleLiveCheck(ctx, bot, msg)
	}

	return m.replyHTML(bot, msg.Chat.ID, formatChecks(checks))
}

// handleLiveCheck выполняет проверку на лету (когда БД ещё пустая)
func (m *Module) handleLiveCheck(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	if len(m.checker.cfg.Domains) == 0 {
		_, err := bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Домены для проверки не настроены."))
		return err
	}

	_, _ = bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Проверяю сертификаты..."))

	var results []*CheckResult
	for _, domain := range m.checker.cfg.Domains {
		results = append(results, m.checker.Check(domain))
		// Сохраняем результат в БД
		m.checker.save(ctx, results[len(results)-1])
	}

	return m.replyHTML(bot, msg.Chat.ID, formatLiveResults(results))
}

// Checker возвращает внутренний чекер (для запуска фоновой горутины из main.go)
func (m *Module) Checker() *Checker {
	return m.checker
}

// formatChecks форматирует список проверок из БД
func formatChecks(checks []*storage.SSLCheck) string {
	var sb strings.Builder
	sb.WriteString(formatter.Bold("SSL-сертификаты") + "\n\n")
	for _, c := range checks {
		fmt.Fprintf(&sb, "%s %s - %d дн. (истекает %s)\n",
			statusEmoji(c.Status),
			formatter.EscapeHTML(c.Domain),
			c.DaysLeft,
			c.ExpiresAt.Format("02.01.2006"),
		)
	}
	return sb.String()
}

// formatLiveResults форматирует результаты live-проверки
func formatLiveResults(results []*CheckResult) string {
	var sb strings.Builder
	sb.WriteString(formatter.Bold("SSL-сертификаты") + "\n\n")
	for _, r := range results {
		if r.Error != nil {
			fmt.Fprintf(&sb, "❌ %s - ошибка проверки\n", formatter.EscapeHTML(r.Domain))
			continue
		}
		fmt.Fprintf(&sb, "%s %s - %d дн. (истекает %s)\n",
			statusEmoji(r.Status),
			formatter.EscapeHTML(r.Domain),
			r.DaysLeft,
			r.ExpiresAt.Format("02.01.2006"),
		)
	}
	return sb.String()
}

func statusEmoji(status string) string {
	switch status {
	case "ok":
		return "✅"
	case "warning":
		return "⚠️"
	case "critical", "expired":
		return "🔴"
	default:
		return "❓"
	}
}

func (m *Module) replyHTML(bot *tgbotapi.BotAPI, chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML"
	_, err := bot.Send(msg)
	return err
}

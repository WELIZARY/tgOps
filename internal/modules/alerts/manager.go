package alerts

import (
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"

	"github.com/WELIZARY/tgOps/internal/formatter"
	"github.com/WELIZARY/tgOps/internal/storage"
)

// Manager отправляет уведомления об алертах в Telegram-чат
type Manager struct {
	bot    *tgbotapi.BotAPI
	chatID int64
	log    *zap.Logger
}

// NewManager создаёт менеджер уведомлений
func NewManager(bot *tgbotapi.BotAPI, chatID int64, log *zap.Logger) *Manager {
	return &Manager{bot: bot, chatID: chatID, log: log}
}

// SendAlert форматирует алерт и отправляет его в чат
func (m *Manager) SendAlert(alert *storage.Alert) {
	if m.chatID == 0 {
		m.log.Warn("notify.chat_id не задан, алерт не отправлен",
			zap.String("server", alert.ServerName),
			zap.String("type", alert.AlertType),
		)
		return
	}

	text := fmt.Sprintf(
		"%s %s: %s на %s\n%s",
		formatter.AlertEmoji(alert.Severity),
		severityRU(alert.Severity),
		alertTypeRU(alert.AlertType),
		formatter.Bold(formatter.EscapeHTML(alert.ServerName)),
		formatter.EscapeHTML(alert.Message),
	)

	msg := tgbotapi.NewMessage(m.chatID, text)
	msg.ParseMode = "HTML"
	if _, err := m.bot.Send(msg); err != nil {
		m.log.Error("ошибка отправки алерта",
			zap.String("server", alert.ServerName),
			zap.Error(err),
		)
	}
}

func severityRU(s string) string {
	switch s {
	case storage.SeverityCritical:
		return "КРИТИЧНО"
	case storage.SeverityWarning:
		return "ПРЕДУПРЕЖДЕНИЕ"
	default:
		return "ИНФО"
	}
}

func alertTypeRU(t string) string {
	switch t {
	case storage.AlertTypeCPU:
		return "CPU"
	case storage.AlertTypeRAM:
		return "RAM"
	case storage.AlertTypeDisk:
		return "Диск"
	case storage.AlertTypeServiceDown:
		return "Сервис недоступен"
	case storage.AlertTypeHTTP:
		return "HTTP-чек упал"
	case storage.AlertTypeSSL:
		return "SSL-сертификат"
	default:
		return t
	}
}

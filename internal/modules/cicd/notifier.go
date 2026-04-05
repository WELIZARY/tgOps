package cicd

import (
	"context"
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"

	"github.com/WELIZARY/tgOps/internal/formatter"
	"github.com/WELIZARY/tgOps/internal/storage"
)

// Notifier отправляет Telegram-уведомления о событиях CI/CD
type Notifier struct {
	bot          *tgbotapi.BotAPI
	chatID       int64
	pipelineRepo storage.PipelineRepo
	log          *zap.Logger
}

// NewNotifier создаёт notifier для уведомлений о деплоях
func NewNotifier(bot *tgbotapi.BotAPI, chatID int64, pipelineRepo storage.PipelineRepo, log *zap.Logger) *Notifier {
	return &Notifier{bot: bot, chatID: chatID, pipelineRepo: pipelineRepo, log: log}
}

// Notify отправляет уведомление о новом событии пайплайна.
// для завершённых деплоев добавляет кнопки approve/reject и сохраняет message_id.
func (n *Notifier) Notify(ctx context.Context, e *storage.PipelineEvent) {
	text := n.formatEvent(e)

	msg := tgbotapi.NewMessage(n.chatID, text)
	msg.ParseMode = "HTML"

	// кнопки только для финальных статусов, требующих подтверждения
	if e.Status == storage.PipelineStatusFailed || e.Status == storage.PipelineStatusSuccess {
		msg.ReplyMarkup = approveKeyboard(e.ID)
	}

	sent, err := n.bot.Send(msg)
	if err != nil {
		n.log.Error("ошибка отправки уведомления pipeline", zap.Error(err))
		return
	}

	// сохраняем message_id для последующего редактирования при approve/reject
	if err := n.pipelineRepo.UpdateTGMessage(ctx, e.ID, sent.MessageID); err != nil {
		n.log.Warn("не удалось сохранить tg_message_id",
			zap.Int("pipeline_id", e.ID),
			zap.Error(err),
		)
	}
}

// NotifyUpdate редактирует ранее отправленное сообщение после approve/reject.
// убирает кнопки и дописывает кто принял решение.
func (n *Notifier) NotifyUpdate(ctx context.Context, e *storage.PipelineEvent, byUser *storage.User) {
	if e.TGMessageID == 0 {
		return
	}

	suffix := ""
	if byUser != nil {
		suffix = fmt.Sprintf("\n\n<i>%s: @%s</i>",
			actionVerb(e.Status),
			formatter.EscapeHTML(byUser.Username),
		)
	}

	text := n.formatEvent(e) + suffix

	edit := tgbotapi.NewEditMessageText(n.chatID, e.TGMessageID, text)
	edit.ParseMode = "HTML"
	// пустая клавиатура убирает кнопки с сообщения
	edit.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{
		InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{},
	}
	if _, err := n.bot.Send(edit); err != nil {
		n.log.Warn("не удалось обновить сообщение pipeline",
			zap.Int("tg_message_id", e.TGMessageID),
			zap.Error(err),
		)
	}
}

// formatEvent форматирует событие пайплайна в HTML-строку для отправки в чат
func (n *Notifier) formatEvent(e *storage.PipelineEvent) string {
	return fmt.Sprintf(
		"%s %s\nРепо: %s\nВетка: %s\nАвтор: %s\nСтатус: %s\n<i>%s</i>",
		pipelineEmoji(e.Status),
		formatter.Bold(formatter.EscapeHTML(e.Source+" #"+e.PipelineID)),
		formatter.EscapeHTML(e.Repo),
		formatter.EscapeHTML(e.Branch),
		formatter.EscapeHTML(e.Author),
		formatter.EscapeHTML(statusRU(e.Status)),
		e.CreatedAt.Format("02.01.2006 15:04"),
	)
}

// approveKeyboard создаёт inline-клавиатуру с кнопками approve и reject
func approveKeyboard(pipelineID int) tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Approve ✅", fmt.Sprintf("deploy_approve_%d", pipelineID)),
			tgbotapi.NewInlineKeyboardButtonData("Reject ❌", fmt.Sprintf("deploy_reject_%d", pipelineID)),
		),
	)
}

// pipelineEmoji возвращает эмодзи по статусу пайплайна
func pipelineEmoji(status string) string {
	switch status {
	case storage.PipelineStatusSuccess, storage.PipelineStatusApproved:
		return "✅"
	case storage.PipelineStatusFailed:
		return "❌"
	case storage.PipelineStatusRunning:
		return "🔄"
	case storage.PipelineStatusRejected:
		return "🚫"
	default:
		return "⏳"
	}
}

// statusRU возвращает русское название статуса пайплайна
func statusRU(status string) string {
	switch status {
	case storage.PipelineStatusPending:
		return "ожидает"
	case storage.PipelineStatusRunning:
		return "выполняется"
	case storage.PipelineStatusSuccess:
		return "успешно"
	case storage.PipelineStatusFailed:
		return "ошибка"
	case storage.PipelineStatusApproved:
		return "подтверждён"
	case storage.PipelineStatusRejected:
		return "отклонён"
	default:
		return status
	}
}

// actionVerb возвращает текстовое описание действия по статусу
func actionVerb(status string) string {
	switch status {
	case storage.PipelineStatusApproved:
		return "Подтверждено"
	case storage.PipelineStatusRejected:
		return "Отклонено"
	default:
		return "Обновлено"
	}
}

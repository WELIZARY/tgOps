package alerts

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"

	"github.com/WELIZARY/tgOps/internal/formatter"
	"github.com/WELIZARY/tgOps/internal/modules"
	"github.com/WELIZARY/tgOps/internal/storage"
)

// Module - модуль алертов (/alerts + inline-подтверждение)
type Module struct {
	alertRepo storage.AlertRepo
	log       *zap.Logger
}

// New создаёт модуль алертов
func New(alertRepo storage.AlertRepo, log *zap.Logger) *Module {
	return &Module{alertRepo: alertRepo, log: log}
}

func (m *Module) Name() string { return "alerts" }

func (m *Module) Commands() []modules.BotCommand {
	return []modules.BotCommand{
		{Command: "/alerts", Description: "список активных алертов", MinRole: "viewer"},
	}
}

func (m *Module) Handle(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	alerts, err := m.alertRepo.ListUnacknowledged(ctx)
	if err != nil {
		m.log.Error("ошибка получения алертов", zap.Error(err))
		return err
	}

	if len(alerts) == 0 {
		_, err = bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Активных алертов нет ✅"))
		return err
	}

	// Каждый алерт - отдельное сообщение с inline-кнопкой подтверждения
	for _, a := range alerts {
		text := fmt.Sprintf(
			"%s %s\n%s\n<i>%s</i>",
			formatter.AlertEmoji(a.Severity),
			formatter.Bold(formatter.EscapeHTML(alertTypeRU(a.AlertType)+" на "+a.ServerName)),
			formatter.EscapeHTML(a.Message),
			a.CreatedAt.Format("02.01.2006 15:04"),
		)

		reply := tgbotapi.NewMessage(msg.Chat.ID, text)
		reply.ParseMode = "HTML"
		reply.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(
					"Принять ✓",
					fmt.Sprintf("ack_%d", a.ID),
				),
			),
		)
		if _, err = bot.Send(reply); err != nil {
			m.log.Error("ошибка отправки алерта", zap.Int("alert_id", a.ID), zap.Error(err))
		}
	}
	return nil
}

// HandleAck обрабатывает нажатие кнопки "Принять ✓".
// Регистрируется в роутере с префиксом "ack_".
func (m *Module) HandleAck(ctx context.Context, bot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery) error {
	// Парсим ID алерта из callback data ("ack_42" -> 42)
	idStr := strings.TrimPrefix(query.Data, "ack_")
	alertID, err := strconv.Atoi(idStr)
	if err != nil {
		return fmt.Errorf("неверный ID алерта: %w", err)
	}

	// Берём пользователя из контекста (роутер кладёт его перед вызовом)
	user := storage.UserFromContext(ctx)
	if user == nil {
		return fmt.Errorf("пользователь не найден в контексте")
	}

	if err := m.alertRepo.Acknowledge(ctx, alertID, user.ID); err != nil {
		m.log.Error("ошибка подтверждения алерта", zap.Int("alert_id", alertID), zap.Error(err))
		// Отвечаем на callback даже при ошибке
		_, _ = bot.Request(tgbotapi.NewCallback(query.ID, "Ошибка. Попробуйте ещё раз."))
		return err
	}

	// Подтверждаем callback (убирает "часики" на кнопке)
	_, _ = bot.Request(tgbotapi.NewCallback(query.ID, "Принято ✓"))

	// Редактируем сообщение: убираем кнопки
	edit := tgbotapi.NewEditMessageReplyMarkup(
		query.Message.Chat.ID,
		query.Message.MessageID,
		tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}},
	)
	_, _ = bot.Send(edit)

	m.log.Info("алерт подтверждён",
		zap.Int("alert_id", alertID),
		zap.Int64("by_user", query.From.ID),
	)
	return nil
}

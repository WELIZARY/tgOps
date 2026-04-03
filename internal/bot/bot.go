package bot

import (
	"context"
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"
)

// Bot обёртка над Telegram API с роутингом команд
type Bot struct {
	api    *tgbotapi.BotAPI
	router *Router
	log    *zap.Logger
}

// API возвращает внутренний объект Telegram-бота.
// Нужен для передачи в модули, которые сами отправляют сообщения (например, alertMgr).
func (b *Bot) API() *tgbotapi.BotAPI {
	return b.api
}

// New создаёт и авторизует Telegram-бота
func New(token string, router *Router, log *zap.Logger) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("авторизация Telegram-бота: %w", err)
	}
	log.Info("бот авторизован", zap.String("username", api.Self.UserName))
	return &Bot{api: api, router: router, log: log}, nil
}

// Start запускает long-polling и блокирует до отмены контекста
func (b *Bot) Start(ctx context.Context) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)
	b.log.Info("бот запущен, ожидаем сообщений")

	for {
		select {
		case <-ctx.Done():
			b.log.Info("получен сигнал завершения, останавливаем polling")
			b.api.StopReceivingUpdates()
			return
		case update, ok := <-updates:
			if !ok {
				b.log.Info("канал обновлений закрыт")
				return
			}
			if update.Message != nil {
				// Каждое сообщение обрабатывается в отдельной горутине
				go b.router.Dispatch(ctx, b.api, update.Message)
			}
			if update.CallbackQuery != nil {
				// Нажатие inline-кнопки
				go b.router.DispatchCallback(ctx, b.api, update.CallbackQuery)
			}
		}
	}
}

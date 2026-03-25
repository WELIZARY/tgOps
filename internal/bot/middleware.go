package bot

import (
	"context"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// HandlerFunc - тип обработчика команды
type HandlerFunc func(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error

// MiddlewareFunc - тип middleware-функции
type MiddlewareFunc func(HandlerFunc) HandlerFunc

// Chain соединяет несколько middleware в цепочку.
// Выполняются в порядке передачи: chain(a, b, c)(h) = a(b(c(h)))
func Chain(middlewares ...MiddlewareFunc) MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		for i := len(middlewares) - 1; i >= 0; i-- {
			next = middlewares[i](next)
		}
		return next
	}
}

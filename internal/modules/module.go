package modules

import (
	"context"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// BotCommand описывает одну команду бота
type BotCommand struct {
	Command     string // например "/start"
	Description string // описание для /help
	MinRole     string // минимальная роль: viewer, operator, admin
}

// CallbackHandler обрабатывает входящий CallbackQuery (нажатие inline-кнопки)
type CallbackHandler func(ctx context.Context, bot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery) error

// Module - интерфейс, который должен реализовать каждый модуль бота
type Module interface {
	// Name возвращает имя модуля
	Name() string
	// Commands возвращает список команд модуля с их описаниями и минимальными ролями
	Commands() []BotCommand
	// Handle обрабатывает входящее сообщение
	Handle(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error
}

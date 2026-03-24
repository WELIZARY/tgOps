package core

import (
	"context"
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/WELIZARY/tgOps/internal/modules"
	"github.com/WELIZARY/tgOps/internal/storage"
)

// Module - базовый модуль с командами /start и /help
type Module struct {
	// commandsForRole возвращает список команд, доступных для роли
	// Функция передаётся снаружи, чтобы избежать циклического импорта с router
	commandsForRole func(role string) []modules.BotCommand
}

// New создаёт базовый модуль.
// commandsForRole - коллбэк к router.CommandsForRole, передаётся из main.go.
func New(commandsForRole func(role string) []modules.BotCommand) *Module {
	return &Module{commandsForRole: commandsForRole}
}

func (m *Module) Name() string { return "core" }

func (m *Module) Commands() []modules.BotCommand {
	return []modules.BotCommand{
		{
			Command:     "/start",
			Description: "Приветствие и информация о вашей роли",
			MinRole:     storage.RoleViewer,
		},
		{
			Command:     "/help",
			Description: "Список доступных команд",
			MinRole:     storage.RoleViewer,
		},
	}
}

func (m *Module) Handle(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	switch msg.Command() {
	case "start":
		return m.handleStart(bot, msg, storage.UserFromContext(ctx))
	case "help":
		return m.handleHelp(bot, msg, storage.UserFromContext(ctx))
	}
	return nil
}

func (m *Module) handleStart(bot *tgbotapi.BotAPI, msg *tgbotapi.Message, user *storage.User) error {
	name := msg.From.FirstName
	if msg.From.UserName != "" {
		name = "@" + msg.From.UserName
	}

	text := fmt.Sprintf(
		"Привет, %s!\n\nДобро пожаловать в *tgOPS*.\nВаша роль: `%s`\n\nИспользуйте /help для просмотра доступных команд.",
		name, user.Role,
	)

	reply := tgbotapi.NewMessage(msg.Chat.ID, text)
	reply.ParseMode = tgbotapi.ModeMarkdown
	_, err := bot.Send(reply)
	return err
}

func (m *Module) handleHelp(bot *tgbotapi.BotAPI, msg *tgbotapi.Message, user *storage.User) error {
	cmds := m.commandsForRole(user.Role)

	var sb strings.Builder
	sb.WriteString("*Доступные команды:*\n\n")
	for _, c := range cmds {
		sb.WriteString(fmt.Sprintf("%s - %s\n", c.Command, c.Description))
	}

	reply := tgbotapi.NewMessage(msg.Chat.ID, sb.String())
	reply.ParseMode = tgbotapi.ModeMarkdown
	_, err := bot.Send(reply)
	return err
}

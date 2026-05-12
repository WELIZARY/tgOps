package bot

import (
	"context"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"

	"github.com/WELIZARY/tgOps/internal/audit"
	"github.com/WELIZARY/tgOps/internal/menu"
	"github.com/WELIZARY/tgOps/internal/modules"
	"github.com/WELIZARY/tgOps/internal/storage"
)

// Router распределяет входящие команды по модулям,
// выполняет RBAC-проверку и пишет аудит-лог
type Router struct {
	handlers         map[string]modules.Module          // "/command" -> модуль
	commands         []modules.BotCommand               // все зарегистрированные команды
	minRoles         map[string]string                  // "/command" -> минимальная роль
	callbackHandlers map[string]modules.CallbackHandler // prefix -> обработчик
	menuState        *menu.State                        // текущий раздел меню по user_id
	userRepo         storage.UserRepo
	auditLog         *audit.Logger
	log              *zap.Logger
}

// NewRouter создаёт Router
func NewRouter(userRepo storage.UserRepo, auditLog *audit.Logger, log *zap.Logger) *Router {
	return &Router{
		handlers:         make(map[string]modules.Module),
		minRoles:         make(map[string]string),
		callbackHandlers: make(map[string]modules.CallbackHandler),
		menuState:        menu.NewState(),
		userRepo:         userRepo,
		auditLog:         auditLog,
		log:              log,
	}
}

// RegisterCallback регистрирует обработчик для CallbackQuery с указанным префиксом.
// Например, prefix = "ack_" перехватит callback data "ack_42".
func (r *Router) RegisterCallback(prefix string, handler modules.CallbackHandler) {
	r.callbackHandlers[prefix] = handler
	r.log.Info("callback обработчик зарегистрирован", zap.String("prefix", prefix))
}

// DispatchCallback обрабатывает входящий CallbackQuery.
// Ищет обработчик по префиксу callback data, проверяет пользователя и вызывает обработчик.
func (r *Router) DispatchCallback(ctx context.Context, bot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery) {
	user, err := r.userRepo.GetByTelegramID(ctx, query.From.ID)
	if err != nil {
		r.log.Error("ошибка получения пользователя (callback)", zap.Int64("telegram_id", query.From.ID), zap.Error(err))
		return
	}
	if user == nil || !user.IsActive {
		return
	}
	ctx = storage.WithUser(ctx, user)

	for prefix, handler := range r.callbackHandlers {
		if len(query.Data) >= len(prefix) && query.Data[:len(prefix)] == prefix {
			if err := handler(ctx, bot, query); err != nil {
				r.log.Error("ошибка обработки callback",
					zap.String("prefix", prefix),
					zap.Int64("user", query.From.ID),
					zap.Error(err),
				)
			}
			return
		}
	}
	r.log.Warn("необработанный callback", zap.String("data", query.Data))
}

// Register регистрирует все команды модуля в роутере
func (r *Router) Register(m modules.Module) {
	for _, cmd := range m.Commands() {
		r.handlers[cmd.Command] = m
		r.commands = append(r.commands, cmd)
		r.minRoles[cmd.Command] = cmd.MinRole
	}
	r.log.Info("модуль зарегистрирован",
		zap.String("module", m.Name()),
		zap.Int("commands", len(m.Commands())),
	)
}

// CommandsForRole возвращает команды, доступные пользователю с указанной ролью
func (r *Router) CommandsForRole(role string) []modules.BotCommand {
	var result []modules.BotCommand
	for _, cmd := range r.commands {
		if storage.HasAccess(role, cmd.MinRole) {
			result = append(result, cmd)
		}
	}
	return result
}

// Dispatch обрабатывает одно входящее сообщение.
// Вызывается в отдельной горутине из Bot.Start.
func (r *Router) Dispatch(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	// Проверяем пользователя в БД (нужно и для команд, и для нажатий меню)
	user, err := r.userRepo.GetByTelegramID(ctx, msg.From.ID)
	if err != nil {
		r.log.Error("ошибка получения пользователя из БД",
			zap.Int64("telegram_id", msg.From.ID),
			zap.Error(err),
		)
		return
	}
	if user == nil || !user.IsActive {
		return
	}
	ctx = storage.WithUser(ctx, user)

	// если это команда - обрабатываем команду
	if msg.IsCommand() {
		r.handleCommand(ctx, bot, msg, user)
		return
	}

	// иначе - проверяем не нажатие ли это кнопки reply-меню
	r.handleMenuText(bot, msg.Chat.ID, msg.Text, msg.From.ID)
}

// handleMenuText распознает текст из reply-меню и реагирует:
// раздел → показать подменю, команда → синтезировать выполнение, "назад" → главное меню.
func (r *Router) handleMenuText(bot *tgbotapi.BotAPI, chatID int64, text string, userID int64) {
	kind, value := menu.Lookup(text)
	switch kind {
	case "section":
		// открываем подменю раздела
		r.menuState.Set(userID, value)
		reply := tgbotapi.NewMessage(chatID, value)
		reply.ReplyMarkup = menu.SubmenuKeyboard(value)
		_, _ = bot.Send(reply)
	case "back":
		// возвращаемся в главное меню
		r.menuState.Clear(userID)
		reply := tgbotapi.NewMessage(chatID, "🏠 главное меню")
		reply.ReplyMarkup = menu.MainKeyboard()
		_, _ = bot.Send(reply)
	case "command":
		// нажатие на команду в подменю - синтезируем команду
		// "/docker ps" → command="docker", args="ps"
		fakeMsg := makeCommandMessage(chatID, userID, value)
		// рекурсивный вызов: обрабатываем как обычную команду
		r.Dispatch(context.Background(), bot, fakeMsg)
	case "none":
		// просто текст, не из меню - игнорируем
		return
	}
}

// makeCommandMessage синтезирует Message с командой как будто пользователь ввёл её.
// нужно для обработки нажатий кнопок reply-меню через обычную логику команд.
func makeCommandMessage(chatID, userID int64, fullCmd string) *tgbotapi.Message {
	// fullCmd вида "/docker ps" - разбиваем на команду и аргументы
	parts := strings.SplitN(strings.TrimPrefix(fullCmd, "/"), " ", 2)
	cmd := parts[0]
	args := ""
	if len(parts) > 1 {
		args = parts[1]
	}
	// собираем сообщение и сразу entity для /cmd, чтобы IsCommand() и Command() работали корректно
	text := "/" + cmd
	if args != "" {
		text += " " + args
	}
	return &tgbotapi.Message{
		MessageID: 0,
		From:      &tgbotapi.User{ID: userID},
		Chat:      &tgbotapi.Chat{ID: chatID, Type: "private"},
		Text:      text,
		Entities: []tgbotapi.MessageEntity{
			{Type: "bot_command", Offset: 0, Length: len("/" + cmd)},
		},
	}
}

// handleCommand выполняет команду (вынесена из Dispatch чтобы можно было звать после проверки юзера)
func (r *Router) handleCommand(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message, user *storage.User) {
	command := "/" + msg.Command()

	// ищем модуль для команды
	mod, ok := r.handlers[command]
	if !ok {
		sendText(bot, msg.Chat.ID, "Неизвестная команда. Используйте /help.")
		return
	}

	// проверяем роль
	minRole := r.minRoles[command]
	if !storage.HasAccess(user.Role, minRole) {
		sendText(bot, msg.Chat.ID, "Недостаточно прав для выполнения этой команды.")
		r.auditLog.Log(ctx, user.ID, command, msg.CommandArguments(), storage.ResultDenied, 0)
		return
	}

	// выполняем команду, замеряем время
	start := time.Now()
	result := storage.ResultSuccess

	if err := mod.Handle(ctx, bot, msg); err != nil {
		r.log.Error("ошибка выполнения команды",
			zap.String("command", command),
			zap.Int64("user", msg.From.ID),
			zap.Error(err),
		)
		result = storage.ResultError
		sendText(bot, msg.Chat.ID, "Произошла ошибка при выполнении команды.")
	}

	// аудит-лог
	r.auditLog.Log(
		ctx, user.ID, command, msg.CommandArguments(),
		result, int(time.Since(start).Milliseconds()),
	)
}

// sendText отправляет простое текстовое сообщение. Ошибку отправки игнорирует (уже залогируется Telegram SDK).
func sendText(bot *tgbotapi.BotAPI, chatID int64, text string) {
	_, _ = bot.Send(tgbotapi.NewMessage(chatID, text))
}

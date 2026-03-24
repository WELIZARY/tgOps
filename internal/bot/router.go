package bot

import (
	"context"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"

	"github.com/WELIZARY/tgOps/internal/audit"
	"github.com/WELIZARY/tgOps/internal/modules"
	"github.com/WELIZARY/tgOps/internal/storage"
)

// Router распределяет входящие команды по модулям,
// выполняет RBAC-проверку и пишет аудит-лог
type Router struct {
	handlers map[string]modules.Module // "/command" -> модуль
	commands []modules.BotCommand      // все зарегистрированные команды
	minRoles map[string]string         // "/command" -> минимальная роль
	userRepo storage.UserRepo
	auditLog *audit.Logger
	log      *zap.Logger
}

// NewRouter создаёт Router
func NewRouter(userRepo storage.UserRepo, auditLog *audit.Logger, log *zap.Logger) *Router {
	return &Router{
		handlers: make(map[string]modules.Module),
		minRoles: make(map[string]string),
		userRepo: userRepo,
		auditLog: auditLog,
		log:      log,
	}
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
	if !msg.IsCommand() {
		return
	}

	command := "/" + msg.Command()

	// Шаг 1: проверяем пользователя в БД
	user, err := r.userRepo.GetByTelegramID(ctx, msg.From.ID)
	if err != nil {
		r.log.Error("ошибка получения пользователя из БД",
			zap.Int64("telegram_id", msg.From.ID),
			zap.Error(err),
		)
		return
	}
	// Неизвестный или заблокированный пользователь - молчим
	if user == nil || !user.IsActive {
		return
	}

	// Шаг 2: кладём пользователя в контекст
	ctx = storage.WithUser(ctx, user)

	// Шаг 3: ищем модуль для команды
	mod, ok := r.handlers[command]
	if !ok {
		sendText(bot, msg.Chat.ID, "Неизвестная команда. Используйте /help.")
		return
	}

	// Шаг 4: проверяем роль
	minRole := r.minRoles[command]
	if !storage.HasAccess(user.Role, minRole) {
		sendText(bot, msg.Chat.ID, "Недостаточно прав для выполнения этой команды.")
		r.auditLog.Log(ctx, user.ID, command, msg.CommandArguments(), storage.ResultDenied, 0)
		return
	}

	// Шаг 5: выполняем команду, замеряем время
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

	// Шаг 6: пишем в аудит-лог
	r.auditLog.Log(
		ctx, user.ID, command, msg.CommandArguments(),
		result, int(time.Since(start).Milliseconds()),
	)
}

// sendText отправляет простое текстовое сообщение. Ошибку отправки игнорирует (уже залогируется Telegram SDK).
func sendText(bot *tgbotapi.BotAPI, chatID int64, text string) {
	_, _ = bot.Send(tgbotapi.NewMessage(chatID, text))
}

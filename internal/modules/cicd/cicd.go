package cicd

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

// Module - модуль CI/CD: список пайплайнов и просмотр деталей
type Module struct {
	pipelineRepo storage.PipelineRepo
	notifier     *Notifier
	log          *zap.Logger
}

// New создаёт модуль CI/CD
func New(pipelineRepo storage.PipelineRepo, notifier *Notifier, log *zap.Logger) *Module {
	return &Module{
		pipelineRepo: pipelineRepo,
		notifier:     notifier,
		log:          log,
	}
}

func (m *Module) Name() string { return "cicd" }

func (m *Module) Commands() []modules.BotCommand {
	return []modules.BotCommand{
		{Command: "/pipelines", Description: "последние 10 событий CI/CD", MinRole: "viewer"},
		{Command: "/pipeline", Description: "pipeline <id> - детали события", MinRole: "viewer"},
	}
}

func (m *Module) Handle(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	switch msg.Command() {
	case "pipelines":
		return m.handleList(ctx, bot, msg)
	case "pipeline":
		return m.handleGet(ctx, bot, msg)
	}
	return nil
}

func (m *Module) handleList(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	events, err := m.pipelineRepo.GetRecent(ctx, 10)
	if err != nil {
		m.log.Error("ошибка получения pipeline событий", zap.Error(err))
		return err
	}

	if len(events) == 0 {
		_, err = bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Событий CI/CD пока нет."))
		return err
	}

	var sb strings.Builder
	sb.WriteString(formatter.Bold("Последние деплои") + "\n\n")
	for _, e := range events {
		fmt.Fprintf(&sb, "%s %s  <code>#%d</code>\n  %s / %s\n  <i>%s</i>\n\n",
			pipelineEmoji(e.Status),
			formatter.EscapeHTML(e.Source),
			e.ID,
			formatter.EscapeHTML(e.Repo),
			formatter.EscapeHTML(e.Branch),
			e.CreatedAt.Format("02.01.2006 15:04"),
		)
	}

	reply := tgbotapi.NewMessage(msg.Chat.ID, sb.String())
	reply.ParseMode = "HTML"
	_, err = bot.Send(reply)
	return err
}

func (m *Module) handleGet(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	args := strings.Fields(msg.CommandArguments())
	if len(args) == 0 {
		_, err := bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Укажите ID. Пример: /pipeline 42"))
		return err
	}

	id, err := strconv.Atoi(args[0])
	if err != nil {
		_, err = bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Неверный ID"))
		return err
	}

	e, err := m.pipelineRepo.GetByID(ctx, id)
	if err != nil {
		m.log.Error("ошибка получения pipeline", zap.Int("id", id), zap.Error(err))
		return err
	}

	text := m.notifier.formatEvent(e)
	reply := tgbotapi.NewMessage(msg.Chat.ID, text)
	reply.ParseMode = "HTML"
	_, err = bot.Send(reply)
	return err
}

// HandleApprove обрабатывает нажатие кнопки "Approve".
// регистрируется в роутере с префиксом "deploy_approve_".
func (m *Module) HandleApprove(ctx context.Context, bot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery) error {
	return m.handleAction(ctx, bot, query, "deploy_approve_", storage.PipelineStatusApproved)
}

// HandleReject обрабатывает нажатие кнопки "Reject".
// регистрируется в роутере с префиксом "deploy_reject_".
func (m *Module) HandleReject(ctx context.Context, bot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery) error {
	return m.handleAction(ctx, bot, query, "deploy_reject_", storage.PipelineStatusRejected)
}

// handleAction выполняет общую логику для approve и reject
func (m *Module) handleAction(
	ctx context.Context,
	bot *tgbotapi.BotAPI,
	query *tgbotapi.CallbackQuery,
	prefix string,
	newStatus string,
) error {
	user := storage.UserFromContext(ctx)
	if user == nil {
		return fmt.Errorf("пользователь не найден в контексте")
	}

	// только admin может подтверждать или отклонять деплои
	if !storage.HasAccess(user.Role, storage.RoleAdmin) {
		_, _ = bot.Request(tgbotapi.NewCallback(query.ID, "Недостаточно прав"))
		return nil
	}

	idStr := strings.TrimPrefix(query.Data, prefix)
	pipelineID, err := strconv.Atoi(idStr)
	if err != nil {
		return fmt.Errorf("неверный ID пайплайна: %w", err)
	}

	if newStatus == storage.PipelineStatusApproved {
		err = m.pipelineRepo.Approve(ctx, pipelineID, user.ID)
	} else {
		err = m.pipelineRepo.Reject(ctx, pipelineID, user.ID)
	}
	if err != nil {
		m.log.Error("ошибка обновления статуса pipeline",
			zap.Int("id", pipelineID),
			zap.Error(err),
		)
		_, _ = bot.Request(tgbotapi.NewCallback(query.ID, "Ошибка. Попробуйте ещё раз."))
		return err
	}

	label := "Подтверждено ✅"
	if newStatus == storage.PipelineStatusRejected {
		label = "Отклонено ❌"
	}
	_, _ = bot.Request(tgbotapi.NewCallback(query.ID, label))

	// обновляем сообщение: убираем кнопки, добавляем инфо о том, кто нажал
	if e, getErr := m.pipelineRepo.GetByID(ctx, pipelineID); getErr == nil {
		m.notifier.NotifyUpdate(ctx, e, user)
	}

	m.log.Info("pipeline action выполнен",
		zap.String("action", newStatus),
		zap.Int("pipeline_id", pipelineID),
		zap.Int64("by_telegram_id", query.From.ID),
	)
	return nil
}

package ansible

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"

	"github.com/WELIZARY/tgOps/internal/config"
	"github.com/WELIZARY/tgOps/internal/formatter"
	"github.com/WELIZARY/tgOps/internal/modules"
	"github.com/WELIZARY/tgOps/internal/storage"
)

// Module - модуль запуска Ansible-плейбуков (/ansible)
type Module struct {
	cfg         *config.AnsibleConfig
	ansibleRepo storage.AnsibleRepo
	log         *zap.Logger
}

// New создаёт модуль ansible
func New(cfg *config.AnsibleConfig, ansibleRepo storage.AnsibleRepo, log *zap.Logger) *Module {
	return &Module{cfg: cfg, ansibleRepo: ansibleRepo, log: log}
}

func (m *Module) Name() string { return "ansible" }

func (m *Module) Commands() []modules.BotCommand {
	return []modules.BotCommand{
		{Command: "/ansible", Description: "ansible playbooks|run|status - управление плейбуками", MinRole: "operator"},
	}
}

func (m *Module) Handle(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	args := strings.Fields(msg.CommandArguments())
	if len(args) == 0 {
		return replyText(bot, msg.Chat.ID,
			"Использование:\n"+
				"  /ansible playbooks — список разрешённых плейбуков\n"+
				"  /ansible run <name> — запустить плейбук (admin)\n"+
				"  /ansible status [id] — история запусков",
		)
	}

	switch args[0] {
	case "playbooks":
		return m.handlePlaybooks(bot, msg)
	case "run":
		return m.handleRun(ctx, bot, msg, args[1:])
	case "status":
		return m.handleStatus(ctx, bot, msg, args[1:])
	default:
		return replyText(bot, msg.Chat.ID,
			fmt.Sprintf("Неизвестная подкоманда %q. Доступны: playbooks, run, status", args[0]))
	}
}

// handlePlaybooks выводит список разрешённых плейбуков из whitelist
func (m *Module) handlePlaybooks(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	if len(m.cfg.Playbooks) == 0 {
		return replyText(bot, msg.Chat.ID,
			"Whitelist плейбуков не настроен.\nДобавьте ansible.playbooks в configs/config.yaml")
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\n\n", formatter.Bold("Доступные плейбуки"))
	for _, p := range m.cfg.Playbooks {
		desc := p.Description
		if desc == "" {
			desc = p.File
		}
		fmt.Fprintf(&sb, "  %s — %s\n",
			formatter.Code(formatter.EscapeHTML(p.Name)),
			formatter.EscapeHTML(desc),
		)
	}
	fmt.Fprintf(&sb, "\nДля запуска: /ansible run &lt;name&gt;")

	reply := tgbotapi.NewMessage(msg.Chat.ID, sb.String())
	reply.ParseMode = "HTML"
	_, err := bot.Send(reply)
	return err
}

// handleRun запускает плейбук из whitelist (только admin)
func (m *Module) handleRun(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message, args []string) error {
	user := storage.UserFromContext(ctx)
	if user == nil {
		return fmt.Errorf("пользователь не найден в контексте")
	}

	// только admin может запускать плейбуки
	if !storage.HasAccess(user.Role, storage.RoleAdmin) {
		return replyText(bot, msg.Chat.ID, "Запуск плейбуков доступен только администратору.")
	}

	if len(args) == 0 {
		return replyText(bot, msg.Chat.ID, "Укажите имя плейбука. Пример: /ansible run deploy")
	}

	name := args[0]
	entry := m.findPlaybook(name)
	if entry == nil {
		names := m.playbookNames()
		return replyText(bot, msg.Chat.ID,
			fmt.Sprintf("Плейбук %q не найден.\nДоступные: %s", name, strings.Join(names, ", ")))
	}

	// проверяем путь до запуска (защита от path traversal)
	playbookPath, err := safePath(m.cfg.PlaybooksDir, entry.File)
	if err != nil {
		return replyText(bot, msg.Chat.ID, "Ошибка: недопустимый путь к плейбуку.")
	}

	// создаём запись о запуске в БД
	run := &storage.AnsibleRun{
		PlaybookName: entry.Name,
		PlaybookFile: entry.File,
		StartedBy:    user.ID,
		Status:       storage.AnsibleRunRunning,
	}
	if err := m.ansibleRepo.Create(ctx, run); err != nil {
		m.log.Error("ошибка создания ansible_run", zap.Error(err))
		return err
	}

	// сообщаем пользователю что запуск начался
	_, _ = bot.Send(tgbotapi.NewMessage(msg.Chat.ID,
		fmt.Sprintf("⏳ Запускаю плейбук %s (run #%d)...", formatter.Code(entry.Name), run.ID)))

	// выполняем плейбук асинхронно
	go m.execPlaybook(context.Background(), bot, msg.Chat.ID, run, playbookPath)
	return nil
}

// execPlaybook запускает ansible-playbook и отправляет результат в чат
func (m *Module) execPlaybook(ctx context.Context, bot *tgbotapi.BotAPI, chatID int64, run *storage.AnsibleRun, playbookPath string) {
	timeout, err := time.ParseDuration(m.cfg.Timeout)
	if err != nil {
		timeout = 5 * time.Minute
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	//nolint:gosec // путь проверен через safePath перед вызовом
	cmd := exec.CommandContext(cmdCtx, "ansible-playbook",
		"-i", m.cfg.InventoryPath,
		playbookPath,
	)
	out, execErr := cmd.CombinedOutput()
	durationMs := int(time.Since(start).Milliseconds())

	status := storage.AnsibleRunSuccess
	if execErr != nil {
		status = storage.AnsibleRunFailed
	}

	output := strings.TrimSpace(string(out))

	// сохраняем результат в БД
	if finishErr := m.ansibleRepo.Finish(ctx, run.ID, status, output, durationMs); finishErr != nil {
		m.log.Error("ошибка сохранения результата ansible_run",
			zap.Int("run_id", run.ID),
			zap.Error(finishErr),
		)
	}

	// формируем и отправляем результат
	durSec := durationMs / 1000
	var header string
	if status == storage.AnsibleRunSuccess {
		header = fmt.Sprintf("✅ Плейбук %s завершён за %dс (run #%d)\n\n",
			formatter.Code(run.PlaybookName), durSec, run.ID)
	} else {
		header = fmt.Sprintf("❌ Плейбук %s завершился с ошибкой (%dс, run #%d)\n\n",
			formatter.Code(run.PlaybookName), durSec, run.ID)
	}

	if output == "" {
		output = "вывод пуст"
	}
	m.sendOutput(bot, chatID, header, output, run.PlaybookName)

	m.log.Info("ansible playbook завершён",
		zap.String("playbook", run.PlaybookName),
		zap.String("status", status),
		zap.Int("duration_ms", durationMs),
	)
}

// handleStatus показывает историю запусков или детали одного запуска
func (m *Module) handleStatus(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message, args []string) error {
	if len(args) > 0 {
		return m.handleStatusOne(ctx, bot, msg, args[0])
	}
	return m.handleStatusList(ctx, bot, msg)
}

func (m *Module) handleStatusList(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) error {
	runs, err := m.ansibleRepo.GetRecent(ctx, 5)
	if err != nil {
		m.log.Error("ошибка получения истории ansible", zap.Error(err))
		return err
	}

	if len(runs) == 0 {
		return replyText(bot, msg.Chat.ID, "Запусков плейбуков пока не было.")
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\n\n", formatter.Bold("История запусков"))
	for _, r := range runs {
		emoji := runEmoji(r.Status)
		dur := ""
		if r.FinishedAt != nil {
			dur = fmt.Sprintf(" %dс", r.DurationMs/1000)
		}
		fmt.Fprintf(&sb, "%s <code>#%d</code>  %s%s\n   %s\n\n",
			emoji,
			r.ID,
			formatter.EscapeHTML(r.PlaybookName),
			dur,
			r.StartedAt.Format("02.01.2006 15:04"),
		)
	}
	fmt.Fprintf(&sb, "Детали: /ansible status &lt;id&gt;")

	reply := tgbotapi.NewMessage(msg.Chat.ID, sb.String())
	reply.ParseMode = "HTML"
	_, err = bot.Send(reply)
	return err
}

func (m *Module) handleStatusOne(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message, idStr string) error {
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return replyText(bot, msg.Chat.ID, "Неверный ID. Пример: /ansible status 12")
	}

	run, err := m.ansibleRepo.GetByID(ctx, id)
	if err != nil {
		m.log.Error("ошибка получения ansible_run", zap.Int("id", id), zap.Error(err))
		return err
	}

	fin := "выполняется..."
	dur := ""
	if run.FinishedAt != nil {
		fin = run.FinishedAt.Format("02.01.2006 15:04:05")
		dur = fmt.Sprintf("%dс", run.DurationMs/1000)
	}

	header := fmt.Sprintf(
		"%s %s <code>#%d</code>\n\nФайл: %s\nСтатус: %s\nНачало: %s\nКонец: %s\nВремя: %s\n\n",
		runEmoji(run.Status),
		formatter.Bold(formatter.EscapeHTML(run.PlaybookName)),
		run.ID,
		formatter.EscapeHTML(run.PlaybookFile),
		formatter.EscapeHTML(run.Status),
		run.StartedAt.Format("02.01.2006 15:04:05"),
		fin,
		dur,
	)

	output := run.Output
	if output == "" {
		output = "вывод пуст"
	}
	m.sendOutput(bot, msg.Chat.ID, header, output, run.PlaybookName)
	return nil
}

// findPlaybook ищет плейбук в whitelist по имени
func (m *Module) findPlaybook(name string) *config.PlaybookEntry {
	for i := range m.cfg.Playbooks {
		if m.cfg.Playbooks[i].Name == name {
			return &m.cfg.Playbooks[i]
		}
	}
	return nil
}

// playbookNames возвращает список имён плейбуков из whitelist
func (m *Module) playbookNames() []string {
	names := make([]string, len(m.cfg.Playbooks))
	for i, p := range m.cfg.Playbooks {
		names[i] = p.Name
	}
	return names
}

// sendOutput отправляет вывод: короткий в pre-блоке, длинный файлом
func (m *Module) sendOutput(bot *tgbotapi.BotAPI, chatID int64, header, out, name string) {
	const maxChars = 3500

	if len(out) <= maxChars {
		reply := tgbotapi.NewMessage(chatID, header+formatter.Pre(formatter.EscapeHTML(out)))
		reply.ParseMode = "HTML"
		_, _ = bot.Send(reply)
		return
	}

	// вывод слишком длинный — отдаём файлом
	ts := time.Now().Format("20060102-150405")
	fileName := fmt.Sprintf("%s-%s.log", name, ts)
	doc := tgbotapi.NewDocument(chatID, tgbotapi.FileReader{
		Name:   fileName,
		Reader: strings.NewReader(out),
	})
	doc.Caption = header
	_, _ = bot.Send(doc)
}

// safePath проверяет что file находится внутри baseDir (защита от path traversal)
func safePath(baseDir, file string) (string, error) {
	base := filepath.Clean(baseDir)
	full := filepath.Clean(filepath.Join(base, file))
	if !strings.HasPrefix(full, base+string(filepath.Separator)) {
		return "", fmt.Errorf("недопустимый путь: %q выходит за пределы %q", file, baseDir)
	}
	return full, nil
}

// runEmoji возвращает эмодзи по статусу запуска
func runEmoji(status string) string {
	switch status {
	case storage.AnsibleRunSuccess:
		return "✅"
	case storage.AnsibleRunFailed:
		return "❌"
	default:
		return "⏳"
	}
}

func replyText(bot *tgbotapi.BotAPI, chatID int64, text string) error {
	_, err := bot.Send(tgbotapi.NewMessage(chatID, text))
	return err
}

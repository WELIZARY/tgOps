// Package menu - постоянное reply-меню бота с навигацией по разделам.
//
// Меню прикрепляется к чату как ReplyKeyboardMarkup и видно у пользователя
// над клавиатурой ввода (показывается/скрывается "квадратиком" в Telegram).
// При нажатии на кнопку бот получает её текст как обычное сообщение.
//
// Структура двухуровневая:
//   - главное меню - разделы (📊 Мониторинг, 🌐 Сеть, ...)
//   - подменю раздела - команды + кнопка ⬅ назад
//
// Router распознает текст кнопок через Lookup и реагирует:
//   - текст раздела - переключает клавиатуру на подменю
//   - текст команды - вызывает соответствующий /command
//   - "⬅ назад" - возвращает главное меню
package menu

import (
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// специальные метки навигации
const (
	BackLabel = "⬅ назад"
	HomeLabel = "🏠 главное меню"
)

// Item - один пункт меню. Если Children не пустой - это раздел (подменю),
// иначе - команда (нажатие = ввод Command).
type Item struct {
	Label    string
	Command  string
	Children []Item
}

// MainMenu - дерево пунктов главного меню. Декларативно описывает всё дерево.
var MainMenu = []Item{
	{
		Label: "📊 мониторинг",
		Children: []Item{
			{Label: "/status", Command: "/status"},
			{Label: "/top", Command: "/top"},
			{Label: "/health", Command: "/health"},
			{Label: "/list", Command: "/list"},
		},
	},
	{
		Label: "🚨 алерты",
		Children: []Item{
			{Label: "/alerts", Command: "/alerts"},
			{Label: "/ssl", Command: "/ssl"},
		},
	},
	{
		Label: "🌐 сеть",
		Children: []Item{
			{Label: "/ping", Command: "/ping"},
			{Label: "/traceroute", Command: "/traceroute"},
			{Label: "/nslookup", Command: "/nslookup"},
		},
	},
	{
		Label: "📜 логи",
		Children: []Item{
			{Label: "/logs", Command: "/logs"},
		},
	},
	{
		Label: "🐳 docker",
		Children: []Item{
			{Label: "/docker ps", Command: "/docker ps"},
			{Label: "/docker images", Command: "/docker images"},
			{Label: "/docker logs", Command: "/docker logs"},
		},
	},
	{
		Label: "🔧 ci/cd",
		Children: []Item{
			{Label: "/pipelines", Command: "/pipelines"},
		},
	},
	{
		Label: "⚙ ansible",
		Children: []Item{
			{Label: "/ansible playbooks", Command: "/ansible playbooks"},
			{Label: "/ansible run", Command: "/ansible run"},
			{Label: "/ansible status", Command: "/ansible status"},
		},
	},
	{
		Label: "🛠 обслуживание",
		Children: []Item{
			{Label: "/updates", Command: "/updates"},
			{Label: "/backups", Command: "/backups"},
			{Label: "/cron", Command: "/cron"},
			{Label: "/scan", Command: "/scan"},
			{Label: "/versions", Command: "/versions"},
		},
	},
	{
		Label: "❓ помощь",
		Children: []Item{
			{Label: "/help", Command: "/help"},
		},
	},
}

// MainKeyboard собирает главное меню по 2 кнопки в ряд
func MainKeyboard() tgbotapi.ReplyKeyboardMarkup {
	rows := chunkSections(MainMenu, 2)
	kb := tgbotapi.NewReplyKeyboard(rows...)
	kb.ResizeKeyboard = true
	return kb
}

// SubmenuKeyboard собирает подменю раздела с кнопкой "назад" в конце.
// если sectionLabel не найден - вернёт главное меню.
func SubmenuKeyboard(sectionLabel string) tgbotapi.ReplyKeyboardMarkup {
	section, ok := findSection(sectionLabel)
	if !ok {
		return MainKeyboard()
	}
	rows := chunkItems(section.Children, 2)
	// добавляем последним рядом кнопку "назад"
	rows = append(rows, tgbotapi.NewKeyboardButtonRow(
		tgbotapi.NewKeyboardButton(BackLabel),
	))
	kb := tgbotapi.NewReplyKeyboard(rows...)
	kb.ResizeKeyboard = true
	return kb
}

// Lookup определяет что значит присланный текст:
//
//	kind="section" - это раздел главного меню (текст = label раздела)
//	kind="command" - это команда, value = "/команда [аргументы]"
//	kind="back"    - кнопка возврата
//	kind="none"    - текст не из меню (обычное сообщение)
func Lookup(text string) (kind, value string) {
	if text == BackLabel || text == HomeLabel {
		return "back", ""
	}
	for _, section := range MainMenu {
		if section.Label == text {
			return "section", section.Label
		}
		for _, item := range section.Children {
			if item.Label == text {
				return "command", item.Command
			}
		}
	}
	return "none", ""
}

// findSection ищет раздел в дереве по его метке
func findSection(label string) (Item, bool) {
	for _, s := range MainMenu {
		if s.Label == label {
			return s, true
		}
	}
	return Item{}, false
}

// chunkSections режет разделы по N в ряд для ReplyKeyboard
func chunkSections(items []Item, perRow int) [][]tgbotapi.KeyboardButton {
	rows := make([][]tgbotapi.KeyboardButton, 0)
	var row []tgbotapi.KeyboardButton
	for i, it := range items {
		row = append(row, tgbotapi.NewKeyboardButton(it.Label))
		if (i+1)%perRow == 0 {
			rows = append(rows, row)
			row = nil
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	return rows
}

// chunkItems - то же самое, но для пунктов подменю
func chunkItems(items []Item, perRow int) [][]tgbotapi.KeyboardButton {
	return chunkSections(items, perRow)
}

// State хранит, в каком разделе сейчас находится каждый пользователь.
// In-memory, без персистентности - перезапуск бота сбрасывает всех на главное меню.
type State struct {
	m sync.Map // user_id (int64) -> section_label (string)
}

// NewState создаёт пустое хранилище состояний
func NewState() *State {
	return &State{}
}

// Set запоминает текущий раздел пользователя
func (s *State) Set(userID int64, section string) {
	s.m.Store(userID, section)
}

// Get возвращает текущий раздел пользователя (или пусто если не выбран)
func (s *State) Get(userID int64) string {
	v, ok := s.m.Load(userID)
	if !ok {
		return ""
	}
	return v.(string)
}

// Clear сбрасывает пользователя на главное меню
func (s *State) Clear(userID int64) {
	s.m.Delete(userID)
}

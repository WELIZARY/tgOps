package formatter

import (
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/WELIZARY/tgOps/internal/storage"
)

// ServerKeyboard строит клавиатуру с кнопками всех серверов.
// callback data каждой кнопки = prefix + имя сервера.
// серверы раскладываются по 2 в ряд для удобства на мобильном.
func ServerKeyboard(servers []*storage.Server, prefix string) tgbotapi.InlineKeyboardMarkup {
	rows := make([][]tgbotapi.InlineKeyboardButton, 0)
	var row []tgbotapi.InlineKeyboardButton
	for i, s := range servers {
		btn := tgbotapi.NewInlineKeyboardButtonData(s.Name, prefix+s.Name)
		row = append(row, btn)
		// формируем ряд из 2 кнопок
		if i%2 == 1 {
			rows = append(rows, row)
			row = nil
		}
	}
	// добиваем последний неполный ряд
	if len(row) > 0 {
		rows = append(rows, row)
	}
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// ServerKeyboardWithAll - то же что ServerKeyboard, но добавляет кнопку "все серверы"
// в самом начале. callback для неё = prefix + "all".
func ServerKeyboardWithAll(servers []*storage.Server, prefix string) tgbotapi.InlineKeyboardMarkup {
	all := tgbotapi.NewInlineKeyboardButtonData("⬛ все серверы", prefix+"all")
	rows := [][]tgbotapi.InlineKeyboardButton{{all}}

	var row []tgbotapi.InlineKeyboardButton
	for i, s := range servers {
		btn := tgbotapi.NewInlineKeyboardButtonData(s.Name, prefix+s.Name)
		row = append(row, btn)
		if i%2 == 1 {
			rows = append(rows, row)
			row = nil
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// ButtonRow - элемент для SubcommandKeyboard
type ButtonRow struct {
	Label string
	Data  string
}

// SubcommandKeyboard строит вертикальную клавиатуру из переданных кнопок.
// удобно для выбора подкоманды (host/image, list/timers и т.п.)
func SubcommandKeyboard(buttons []ButtonRow) tgbotapi.InlineKeyboardMarkup {
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(buttons))
	for _, b := range buttons {
		row := []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(b.Label, b.Data),
		}
		rows = append(rows, row)
	}
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// StringKeyboard строит клавиатуру для произвольного списка строк.
// callback data = prefix + строка. по 2 кнопки в ряд.
func StringKeyboard(items []string, prefix string) tgbotapi.InlineKeyboardMarkup {
	rows := make([][]tgbotapi.InlineKeyboardButton, 0)
	var row []tgbotapi.InlineKeyboardButton
	for i, item := range items {
		btn := tgbotapi.NewInlineKeyboardButtonData(item, prefix+item)
		row = append(row, btn)
		if i%2 == 1 {
			rows = append(rows, row)
			row = nil
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

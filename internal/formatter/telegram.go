package formatter

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// ProgressBar рисует прогресс-бар вида: ▓▓▓▓░░░░░░ 42%
// width - ширина полосы в символах (обычно 10)
func ProgressBar(percent float64, width int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := int(math.Round(percent / 100 * float64(width)))
	bar := strings.Repeat("▓", filled) + strings.Repeat("░", width-filled)
	return fmt.Sprintf("%s %.0f%%", bar, percent)
}

// SeverityEmoji возвращает эмодзи по уровню нагрузки относительно порогов
//
//	< warn  -> ✅
//	< crit  -> ⚠️
//	>= crit -> 🔴
func SeverityEmoji(percent, warn, crit float64) string {
	switch {
	case percent >= crit:
		return "🔴"
	case percent >= warn:
		return "⚠️"
	default:
		return "✅"
	}
}

// AlertEmoji возвращает эмодзи по строковому уровню severity
func AlertEmoji(severity string) string {
	switch severity {
	case "critical":
		return "🔴"
	case "warning":
		return "⚠️"
	default:
		return "ℹ️"
	}
}

// FormatBytes форматирует количество байт в читаемый вид
// 1023 -> "1023 B", 1024 -> "1.0 KB", 1048576 -> "1.0 MB" и т.д.
func FormatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// FormatDuration форматирует duration в вид "2д 3ч 15м"
// Для значений меньше минуты возвращает "< 1м"
func FormatDuration(d time.Duration) string {
	if d < time.Minute {
		return "< 1м"
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dд", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dч", hours))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%dм", minutes))
	}
	return strings.Join(parts, " ")
}

// EscapeHTML экранирует символы < > & для безопасной вставки в Telegram HTML
func EscapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// Bold оборачивает текст в <b>...</b>
func Bold(s string) string {
	return "<b>" + s + "</b>"
}

// Code оборачивает текст в <code>...</code>
func Code(s string) string {
	return "<code>" + s + "</code>"
}

// Pre оборачивает текст в <pre>...</pre> (моноширинный блок)
func Pre(s string) string {
	return "<pre>" + s + "</pre>"
}

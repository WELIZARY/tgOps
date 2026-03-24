package audit

import (
	"context"

	"go.uber.org/zap"

	"github.com/WELIZARY/tgOps/internal/storage"
)

// Logger пишет записи аудита асинхронно, не блокируя основной поток
type Logger struct {
	repo storage.AuditRepo
	log  *zap.Logger
}

// New создаёт audit Logger
func New(repo storage.AuditRepo, log *zap.Logger) *Logger {
	return &Logger{repo: repo, log: log}
}

// Log записывает одно действие пользователя в audit_log.
// Запись выполняется в горутине, чтобы не задерживать ответ бота.
func (l *Logger) Log(ctx context.Context, userID int, command, args, result string, durationMs int) {
	entry := &storage.AuditEntry{
		UserID:     userID,
		Command:    command,
		Args:       args,
		Result:     result,
		DurationMs: durationMs,
	}
	go func() {
		if err := l.repo.Write(context.Background(), entry); err != nil {
			l.log.Error("ошибка записи в audit_log",
				zap.String("command", command),
				zap.Error(err),
			)
		}
	}()
}

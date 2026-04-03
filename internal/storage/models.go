package storage

import (
	"context"
	"time"
)

// Роли пользователей
const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleViewer   = "viewer"
)

// Результаты действий для audit_log
const (
	ResultSuccess = "success"
	ResultError   = "error"
	ResultDenied  = "denied"
)

// roleLevel - числовой уровень роли для сравнения
var roleLevel = map[string]int{
	RoleViewer:   1,
	RoleOperator: 2,
	RoleAdmin:    3,
}

// HasAccess проверяет, достаточно ли у пользователя прав для требуемой роли
func HasAccess(userRole, requiredRole string) bool {
	return roleLevel[userRole] >= roleLevel[requiredRole]
}

// User - запись о пользователе бота
type User struct {
	ID         int
	TelegramID int64
	Username   string
	Role       string
	IsActive   bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// AuditEntry - запись аудит-лога
type AuditEntry struct {
	ID         int
	UserID     int
	Command    string
	Args       string
	Result     string
	DurationMs int
	CreatedAt  time.Time
}

// Server - управляемый сервер из таблицы servers
type Server struct {
	ID        int
	Name      string
	Host      string
	Port      int
	SSHUser   string
	KeyName   string // имя файла ключа в keys_dir (пусто - используется default)
	IsActive  bool
	CreatedAt time.Time
}

// Типы алертов
const (
	AlertTypeCPU         = "cpu"
	AlertTypeRAM         = "ram"
	AlertTypeDisk        = "disk"
	AlertTypeServiceDown = "service_down"
	AlertTypeSSL         = "ssl"
	AlertTypeHTTP        = "http"
)

// Уровни критичности алертов
const (
	SeverityCritical = "critical"
	SeverityWarning  = "warning"
	SeverityInfo     = "info"
)

// Alert - алерт о проблеме на сервере
type Alert struct {
	ID           int
	ServerName   string
	AlertType    string
	Severity     string
	Message      string
	Acknowledged bool
	AckBy        *int
	AckAt        *time.Time
	CreatedAt    time.Time
}

// - Хелперы для передачи User через context -

type contextKey string

const userKey contextKey = "user"

// WithUser кладёт пользователя в контекст (используется в middleware)
func WithUser(ctx context.Context, user *User) context.Context {
	return context.WithValue(ctx, userKey, user)
}

// UserFromContext достаёт пользователя из контекста
func UserFromContext(ctx context.Context) *User {
	u, _ := ctx.Value(userKey).(*User)
	return u
}

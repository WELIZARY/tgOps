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

// PipelineEvent - событие CI/CD пайплайна (GitHub/GitLab/Jenkins)
type PipelineEvent struct {
	ID          int
	PipelineID  string     // внешний идентификатор (run_id у GitHub, pipeline_id у GitLab)
	Source      string     // github, gitlab, jenkins
	Repo        string
	Branch      string
	Status      string
	Author      string
	ApprovedBy  *int       // FK → users.id, кто нажал approve/reject
	ApprovedAt  *time.Time
	TGMessageID int        // message_id Telegram-уведомления для редактирования
	Payload     []byte     // raw JSON от webhook
	CreatedAt   time.Time
}

// статусы CI/CD пайплайна
const (
	PipelineStatusPending  = "pending"
	PipelineStatusRunning  = "running"
	PipelineStatusSuccess  = "success"
	PipelineStatusFailed   = "failed"
	PipelineStatusApproved = "approved"
	PipelineStatusRejected = "rejected"
)

// AnsibleRun - запись о запуске Ansible-плейбука
type AnsibleRun struct {
	ID           int
	PlaybookName string     // короткое имя из whitelist
	PlaybookFile string     // имя файла плейбука
	StartedBy    int        // FK users.id
	Status       string     // running, success, failed
	Output       string     // stdout+stderr ansible-playbook
	StartedAt    time.Time
	FinishedAt   *time.Time
	DurationMs   int
}

// статусы запуска плейбука
const (
	AnsibleRunRunning = "running"
	AnsibleRunSuccess = "success"
	AnsibleRunFailed  = "failed"
)

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

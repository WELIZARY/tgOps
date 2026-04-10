package config

import (
	"fmt"

	"github.com/spf13/viper"
)

// Config - корневая структура конфигурации
type Config struct {
	Telegram     TelegramConfig     `mapstructure:"telegram"`
	Database     DatabaseConfig     `mapstructure:"database"`
	Logger       LoggerConfig       `mapstructure:"logger"`
	SSH          SSHConfig          `mapstructure:"ssh"`
	Monitoring   MonitoringConfig   `mapstructure:"monitoring"`
	SSL          SSLConfig          `mapstructure:"ssl"`
	HealthChecks HealthChecksConfig `mapstructure:"health_checks"`
	Notify       NotifyConfig       `mapstructure:"notify"`
	Logs         LogsConfig         `mapstructure:"logs"`
	Docker       DockerConfig       `mapstructure:"docker"`
	CICD         CICDConfig         `mapstructure:"cicd"`
	Ansible      AnsibleConfig      `mapstructure:"ansible"`
	Updates      UpdatesConfig      `mapstructure:"updates"`
	Backups      BackupsConfig      `mapstructure:"backups"`
	Cron         CronConfig         `mapstructure:"cron"`
	Scan         ScanConfig         `mapstructure:"scan"`
	Versions     VersionsConfig     `mapstructure:"versions"`
}

// TelegramConfig - настройки Telegram-бота
type TelegramConfig struct {
	Token          string        `mapstructure:"token"`
	InitialAdminID int64         `mapstructure:"initial_admin_id"`
	Mode           string        `mapstructure:"mode"`
	Webhook        WebhookConfig `mapstructure:"webhook"`
}

// WebhookConfig - настройки webhook-режима
type WebhookConfig struct {
	URL      string `mapstructure:"url"`
	Port     int    `mapstructure:"port"`
	CertPath string `mapstructure:"cert_path"`
	KeyPath  string `mapstructure:"key_path"`
}

// DatabaseConfig - настройки PostgreSQL
type DatabaseConfig struct {
	Primary DBConnConfig `mapstructure:"primary"`
	Replica DBConnConfig `mapstructure:"replica"`
}

// DBConnConfig - параметры одного подключения к БД
type DBConnConfig struct {
	Host         string `mapstructure:"host"`
	Port         int    `mapstructure:"port"`
	User         string `mapstructure:"user"`
	Password     string `mapstructure:"password"`
	Name         string `mapstructure:"name"`
	SSLMode      string `mapstructure:"ssl_mode"`
	MaxOpenConns int    `mapstructure:"max_open_conns"`
	MaxIdleConns int    `mapstructure:"max_idle_conns"`
}

// LoggerConfig - настройки логирования
type LoggerConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
	Output string `mapstructure:"output"`
}

// SSHConfig - настройки SSH-подключений
type SSHConfig struct {
	// KeysDir - директория с приватными ключами (относительно рабочей директории)
	// Именование ключей: keys/{server-name} без расширения
	KeysDir               string        `mapstructure:"keys_dir"`
	DefaultKeyPath        string        `mapstructure:"default_key_path"`
	ConnectTimeout        string        `mapstructure:"connect_timeout"`
	CommandTimeout        string        `mapstructure:"command_timeout"`
	MaxConnectionsPerHost int           `mapstructure:"max_connections_per_host"`
	Servers               []ServerEntry `mapstructure:"servers"`
}

// ServerEntry - запись об управляемом сервере из конфига
type ServerEntry struct {
	Name    string `mapstructure:"name"`
	Host    string `mapstructure:"host"`
	Port    int    `mapstructure:"port"`
	User    string `mapstructure:"user"`
	// KeyName - имя файла ключа в KeysDir (например "vps-main" -> keys/vps-main)
	// Если пусто - используется DefaultKeyPath
	KeyName string `mapstructure:"key_name"`
}

// MonitoringConfig - настройки фонового сборщика метрик и алертов
type MonitoringConfig struct {
	// Interval - как часто опрашивать серверы (например "60s")
	Interval   string           `mapstructure:"interval"`
	Thresholds ThresholdsConfig `mapstructure:"thresholds"`
	// AlertCooldown - минимальное время между повторными алертами одного типа
	AlertCooldown string `mapstructure:"alert_cooldown"`
}

// ThresholdsConfig - пороги для срабатывания алертов (в процентах)
type ThresholdsConfig struct {
	CPUWarning   float64 `mapstructure:"cpu_warning"`
	CPUCritical  float64 `mapstructure:"cpu_critical"`
	RAMWarning   float64 `mapstructure:"ram_warning"`
	RAMCritical  float64 `mapstructure:"ram_critical"`
	DiskWarning  float64 `mapstructure:"disk_warning"`
	DiskCritical float64 `mapstructure:"disk_critical"`
}

// SSLConfig - настройки проверки SSL-сертификатов
type SSLConfig struct {
	// Domains - список доменов для проверки
	Domains []string `mapstructure:"domains"`
	// WarnDays - уведомлять за N дней до истечения (например [30, 14, 7, 1])
	WarnDays      []int  `mapstructure:"warn_days"`
	CheckInterval string `mapstructure:"check_interval"`
}

// HealthChecksConfig - настройки HTTP-проверок доступности
type HealthChecksConfig struct {
	Interval  string           `mapstructure:"interval"`
	Timeout   string           `mapstructure:"timeout"`
	Endpoints []EndpointConfig `mapstructure:"endpoints"`
}

// EndpointConfig - один HTTP-эндпоинт для проверки
type EndpointConfig struct {
	Name           string `mapstructure:"name"`
	URL            string `mapstructure:"url"`
	ExpectedStatus int    `mapstructure:"expected_status"`
}

// NotifyConfig - куда отправлять алерты и уведомления
type NotifyConfig struct {
	// ChatID - Telegram chat_id для отправки алертов (обычно совпадает с admin)
	ChatID int64 `mapstructure:"chat_id"`
}

// LogsConfig - настройки модуля просмотра логов сервисов
type LogsConfig struct {
	// AllowedServices - whitelist сервисов, логи которых разрешено смотреть через бота
	AllowedServices []string `mapstructure:"allowed_services"`
	// MaxLines - количество строк журнала по умолчанию
	MaxLines int `mapstructure:"max_lines"`
	// MaxMessageChars - лимит символов в одном Telegram-сообщении
	MaxMessageChars int `mapstructure:"max_message_chars"`
}

// DockerConfig - настройки Docker-модуля
type DockerConfig struct {
	// Host - зарезервировано для будущей интеграции с Docker API напрямую
	Host    string `mapstructure:"host"`
	Timeout string `mapstructure:"timeout"`
}

// CICDConfig - настройки CI/CD webhook-приёмника и уведомлений
type CICDConfig struct {
	// WebhookPort - порт HTTP-сервера для входящих webhook от GitHub/GitLab/Jenkins
	WebhookPort int `mapstructure:"webhook_port"`
	// WebhookSecret - HMAC-ключ для проверки подписи X-Hub-Signature-256
	WebhookSecret string `mapstructure:"webhook_secret"`
	// NotifyChatID - chat_id для уведомлений о деплоях (может отличаться от алертов)
	NotifyChatID int64 `mapstructure:"notify_chat_id"`
}

// AnsibleConfig - настройки Ansible-модуля
type AnsibleConfig struct {
	// PlaybooksDir - директория с плейбуками (относительно рабочей директории)
	PlaybooksDir string `mapstructure:"playbooks_dir"`
	// InventoryPath - путь к inventory-файлу
	InventoryPath string `mapstructure:"inventory_path"`
	// Timeout - максимальное время выполнения одного плейбука (например "5m")
	Timeout string `mapstructure:"timeout"`
	// Playbooks - whitelist разрешённых плейбуков
	Playbooks []PlaybookEntry `mapstructure:"playbooks"`
}

// PlaybookEntry - один разрешённый плейбук из whitelist
type PlaybookEntry struct {
	// Name - короткое имя для команды бота (например "deploy")
	Name string `mapstructure:"name"`
	// File - имя файла в PlaybooksDir (например "deploy.yml")
	File string `mapstructure:"file"`
	// Description - описание для /ansible playbooks
	Description string `mapstructure:"description"`
}

// UpdatesConfig - настройки модуля проверки обновлений пакетов
type UpdatesConfig struct {
	// Timeout - таймаут SSH-команды проверки обновлений
	Timeout string `mapstructure:"timeout"`
}

// BackupsConfig - настройки модуля проверки статуса бэкапов
type BackupsConfig struct {
	// Paths - список директорий для проверки на управляемых серверах
	Paths []BackupPathConfig `mapstructure:"paths"`
	// Timeout - таймаут SSH-команды
	Timeout string `mapstructure:"timeout"`
}

// BackupPathConfig - одна директория с резервными копиями
type BackupPathConfig struct {
	// Name - читаемое имя (например "PostgreSQL")
	Name string `mapstructure:"name"`
	// Path - путь к директории на сервере (например "/var/backups/postgres")
	Path string `mapstructure:"path"`
	// MaxAgeHours - показывать предупреждение если последний файл старше N часов (0 - не проверять)
	MaxAgeHours int `mapstructure:"max_age_hours"`
}

// CronConfig - настройки модуля просмотра cron-задач и systemd-таймеров
type CronConfig struct {
	// Timeout - таймаут SSH-команды сбора расписания
	Timeout string `mapstructure:"timeout"`
}

// ScanConfig - настройки модуля сканирования уязвимостей
type ScanConfig struct {
	// Timeout - таймаут сканирования (trivy и lynis работают долго)
	Timeout string `mapstructure:"timeout"`
	// TrivyImage - образ trivy для запуска через docker run на сервере
	TrivyImage string `mapstructure:"trivy_image"`
}

// VersionsConfig - настройки модуля проверки версий установленного ПО
type VersionsConfig struct {
	// Timeout - таймаут SSH-команды
	Timeout string `mapstructure:"timeout"`
	// Packages - список инструментов для проверки (пусто - стандартный набор)
	Packages []string `mapstructure:"packages"`
}

// Load загружает конфигурацию из YAML-файла.
// Переменные окружения с префиксом TGOPS_ имеют приоритет над файлом.
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetEnvPrefix("TGOPS")
	v.AutomaticEnv()

	// Явный биндинг секретных переменных
	_ = v.BindEnv("telegram.token", "TGOPS_TELEGRAM_TOKEN")
	_ = v.BindEnv("database.primary.password", "TGOPS_DATABASE_PRIMARY_PASSWORD")
	_ = v.BindEnv("database.replica.password", "TGOPS_DATABASE_REPLICA_PASSWORD")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("чтение конфига %q: %w", path, err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("парсинг конфига: %w", err)
	}

	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("валидация конфига: %w", err)
	}

	return &cfg, nil
}

func validate(cfg *Config) error {
	if cfg.Telegram.Token == "" {
		return fmt.Errorf("telegram.token не задан")
	}
	if cfg.Telegram.InitialAdminID == 0 {
		return fmt.Errorf("telegram.initial_admin_id не задан")
	}
	if cfg.Database.Primary.Host == "" {
		return fmt.Errorf("database.primary.host не задан")
	}
	if cfg.Database.Primary.Port == 0 {
		cfg.Database.Primary.Port = 5432
	}
	if cfg.Database.Primary.SSLMode == "" {
		cfg.Database.Primary.SSLMode = "disable"
	}
	if cfg.Logger.Level == "" {
		cfg.Logger.Level = "info"
	}

	// Дефолты для SSH
	if cfg.SSH.KeysDir == "" {
		cfg.SSH.KeysDir = "keys"
	}
	if cfg.SSH.MaxConnectionsPerHost == 0 {
		cfg.SSH.MaxConnectionsPerHost = 3
	}

	// Дефолты для мониторинга
	if cfg.Monitoring.Interval == "" {
		cfg.Monitoring.Interval = "60s"
	}
	if cfg.Monitoring.AlertCooldown == "" {
		cfg.Monitoring.AlertCooldown = "10m"
	}
	if cfg.Monitoring.Thresholds.CPUWarning == 0 {
		cfg.Monitoring.Thresholds.CPUWarning = 80
	}
	if cfg.Monitoring.Thresholds.CPUCritical == 0 {
		cfg.Monitoring.Thresholds.CPUCritical = 90
	}
	if cfg.Monitoring.Thresholds.RAMWarning == 0 {
		cfg.Monitoring.Thresholds.RAMWarning = 75
	}
	if cfg.Monitoring.Thresholds.RAMCritical == 0 {
		cfg.Monitoring.Thresholds.RAMCritical = 85
	}
	if cfg.Monitoring.Thresholds.DiskWarning == 0 {
		cfg.Monitoring.Thresholds.DiskWarning = 80
	}
	if cfg.Monitoring.Thresholds.DiskCritical == 0 {
		cfg.Monitoring.Thresholds.DiskCritical = 90
	}

	// Дефолты для SSL
	if cfg.SSL.CheckInterval == "" {
		cfg.SSL.CheckInterval = "24h"
	}
	if len(cfg.SSL.WarnDays) == 0 {
		cfg.SSL.WarnDays = []int{30, 14, 7, 1}
	}

	// дефолты для HTTP-чеков
	if cfg.HealthChecks.Interval == "" {
		cfg.HealthChecks.Interval = "60s"
	}
	if cfg.HealthChecks.Timeout == "" {
		cfg.HealthChecks.Timeout = "10s"
	}

	// дефолты для модуля логов
	if cfg.Logs.MaxLines == 0 {
		cfg.Logs.MaxLines = 100
	}
	if cfg.Logs.MaxMessageChars == 0 {
		cfg.Logs.MaxMessageChars = 4096
	}

	// дефолты для Docker-модуля
	if cfg.Docker.Timeout == "" {
		cfg.Docker.Timeout = "30s"
	}

	// дефолты для CI/CD
	if cfg.CICD.WebhookPort == 0 {
		cfg.CICD.WebhookPort = 8080
	}

	// дефолты для Ansible
	if cfg.Ansible.PlaybooksDir == "" {
		cfg.Ansible.PlaybooksDir = "playbooks"
	}
	if cfg.Ansible.InventoryPath == "" {
		cfg.Ansible.InventoryPath = "inventory/hosts"
	}
	if cfg.Ansible.Timeout == "" {
		cfg.Ansible.Timeout = "5m"
	}

	// дефолты для Updates
	if cfg.Updates.Timeout == "" {
		cfg.Updates.Timeout = "60s"
	}

	// дефолты для Backups
	if cfg.Backups.Timeout == "" {
		cfg.Backups.Timeout = "30s"
	}

	// дефолты для Cron
	if cfg.Cron.Timeout == "" {
		cfg.Cron.Timeout = "15s"
	}

	// дефолты для Scan
	if cfg.Scan.Timeout == "" {
		cfg.Scan.Timeout = "5m"
	}
	if cfg.Scan.TrivyImage == "" {
		cfg.Scan.TrivyImage = "aquasec/trivy:latest"
	}

	// дефолты для Versions
	if cfg.Versions.Timeout == "" {
		cfg.Versions.Timeout = "30s"
	}

	return nil
}

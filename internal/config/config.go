package config

import (
	"fmt"

	"github.com/spf13/viper"
)

// Config - корневая структура конфигурации
type Config struct {
	Telegram TelegramConfig `mapstructure:"telegram"`
	Database DatabaseConfig `mapstructure:"database"`
	Logger   LoggerConfig   `mapstructure:"logger"`
	SSH      SSHConfig      `mapstructure:"ssh"`
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
	KeyPath string `mapstructure:"key_path"`
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
	return nil
}

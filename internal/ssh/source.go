package ssh

import (
	"context"

	"github.com/WELIZARY/tgOps/internal/config"
	"github.com/WELIZARY/tgOps/internal/storage"
)

// ServerSource - откуда брать список серверов для мониторинга
type ServerSource interface {
	GetServers(ctx context.Context) ([]*storage.Server, error)
}

// ConfigSource читает серверы из конфига (статический список)
type ConfigSource struct {
	entries []config.ServerEntry
}

// NewConfigSource создаёт источник серверов из конфига
func NewConfigSource(cfg *config.SSHConfig) *ConfigSource {
	return &ConfigSource{entries: cfg.Servers}
}

func (s *ConfigSource) GetServers(_ context.Context) ([]*storage.Server, error) {
	servers := make([]*storage.Server, 0, len(s.entries))
	for _, e := range s.entries {
		port := e.Port
		if port == 0 {
			port = 22
		}
		servers = append(servers, &storage.Server{
			Name:    e.Name,
			Host:    e.Host,
			Port:    port,
			SSHUser: e.User,
			KeyName: e.KeyName,
		})
	}
	return servers, nil
}

// DBSource читает серверы из таблицы servers в PostgreSQL
type DBSource struct {
	repo storage.ServerRepo
}

// NewDBSource создаёт источник серверов из БД
func NewDBSource(repo storage.ServerRepo) *DBSource {
	return &DBSource{repo: repo}
}

func (s *DBSource) GetServers(ctx context.Context) ([]*storage.Server, error) {
	return s.repo.GetAll(ctx)
}

// ComboSource объединяет конфиг и БД.
// При совпадении имён сервера конфиг имеет приоритет.
// Если БД недоступна - работает только с конфигом.
type ComboSource struct {
	cfg *ConfigSource
	db  *DBSource
}

// NewComboSource создаёт комбинированный источник серверов
func NewComboSource(cfg *config.SSHConfig, repo storage.ServerRepo) *ComboSource {
	return &ComboSource{
		cfg: NewConfigSource(cfg),
		db:  NewDBSource(repo),
	}
}

func (s *ComboSource) GetServers(ctx context.Context) ([]*storage.Server, error) {
	cfgServers, err := s.cfg.GetServers(ctx)
	if err != nil {
		return nil, err
	}

	dbServers, _ := s.db.GetServers(ctx)
	// Ошибку БД игнорируем - работаем с тем что есть из конфига

	// Строим map имён из конфига чтобы не дублировать
	names := make(map[string]struct{}, len(cfgServers))
	for _, srv := range cfgServers {
		names[srv.Name] = struct{}{}
	}

	result := make([]*storage.Server, len(cfgServers))
	copy(result, cfgServers)

	// Добавляем из БД только те серверы, которых нет в конфиге
	for _, srv := range dbServers {
		if _, exists := names[srv.Name]; !exists {
			result = append(result, srv)
		}
	}

	return result, nil
}

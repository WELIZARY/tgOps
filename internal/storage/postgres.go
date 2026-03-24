package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/WELIZARY/tgOps/internal/config"
)

// DB хранит пулы соединений к primary и replica
type DB struct {
	Primary *pgxpool.Pool // чтение + запись
	Replica *pgxpool.Pool // только чтение (nil если репликация не настроена)
}

// Connect создаёт пулы соединений и пингует оба
func Connect(ctx context.Context, cfg config.DatabaseConfig, log *zap.Logger) (*DB, error) {
	primaryDSN := buildDSN(cfg.Primary)

	primary, err := pgxpool.New(ctx, primaryDSN)
	if err != nil {
		return nil, fmt.Errorf("подключение к primary: %w", err)
	}
	if err := primary.Ping(ctx); err != nil {
		primary.Close()
		return nil, fmt.Errorf("ping primary: %w", err)
	}
	log.Info("подключение к primary установлено",
		zap.String("host", cfg.Primary.Host),
		zap.Int("port", cfg.Primary.Port),
	)

	db := &DB{Primary: primary}

	// Replica опциональна - если недоступна, работаем только с primary
	if cfg.Replica.Host != "" {
		replicaDSN := buildDSN(cfg.Replica)
		replica, err := pgxpool.New(ctx, replicaDSN)
		if err != nil {
			log.Warn("не удалось создать пул replica, работаем без неё", zap.Error(err))
		} else if err := replica.Ping(ctx); err != nil {
			replica.Close()
			log.Warn("ping replica не прошёл, работаем без неё", zap.Error(err))
		} else {
			db.Replica = replica
			log.Info("подключение к replica установлено",
				zap.String("host", cfg.Replica.Host),
			)
		}
	}

	return db, nil
}

// ReadPool возвращает replica если доступна, иначе primary
func (db *DB) ReadPool() *pgxpool.Pool {
	if db.Replica != nil {
		return db.Replica
	}
	return db.Primary
}

// Close закрывает все пулы соединений
func (db *DB) Close() {
	db.Primary.Close()
	if db.Replica != nil {
		db.Replica.Close()
	}
}

// RunMigrations применяет все новые .sql миграции из директории.
// Уже применённые миграции отслеживаются в таблице schema_migrations.
func RunMigrations(ctx context.Context, db *DB, migrationsDir string, log *zap.Logger) error {
	// Создаём таблицу учёта, если ещё нет
	_, err := db.Primary.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		return fmt.Errorf("создание schema_migrations: %w", err)
	}

	// Читаем уже применённые версии
	rows, err := db.Primary.Query(ctx, "SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		return fmt.Errorf("запрос применённых миграций: %w", err)
	}
	applied := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return fmt.Errorf("сканирование версии миграции: %w", err)
		}
		applied[v] = true
	}
	rows.Close()

	// Находим все .sql файлы и сортируем по имени
	files, err := filepath.Glob(filepath.Join(migrationsDir, "*.sql"))
	if err != nil {
		return fmt.Errorf("поиск файлов миграций: %w", err)
	}
	sort.Strings(files)

	for _, f := range files {
		version := strings.TrimSuffix(filepath.Base(f), ".sql")
		if applied[version] {
			log.Debug("миграция уже применена, пропуск", zap.String("version", version))
			continue
		}

		content, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("чтение файла миграции %s: %w", f, err)
		}

		// Выполняем в транзакции: SQL + запись версии
		tx, err := db.Primary.Begin(ctx)
		if err != nil {
			return fmt.Errorf("начало транзакции для %s: %w", version, err)
		}

		if _, err := tx.Exec(ctx, string(content)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("выполнение миграции %s: %w", version, err)
		}

		if _, err := tx.Exec(ctx,
			"INSERT INTO schema_migrations(version) VALUES($1)", version,
		); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("запись версии миграции %s: %w", version, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("коммит миграции %s: %w", version, err)
		}

		log.Info("миграция применена", zap.String("version", version))
	}

	return nil
}

func buildDSN(c config.DBConnConfig) string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		c.User, c.Password, c.Host, c.Port, c.Name, c.SSLMode,
	)
}

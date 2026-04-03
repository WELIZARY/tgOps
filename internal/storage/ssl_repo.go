package storage

import (
	"context"
	"fmt"
	"time"
)

// SSLCheck - результат проверки SSL-сертификата
type SSLCheck struct {
	ID        int
	Domain    string
	Issuer    string
	ExpiresAt time.Time
	DaysLeft  int
	Status    string // ok, warning, critical, expired
	CheckedAt time.Time
}

// SSLRepo - интерфейс для работы с таблицей ssl_checks
type SSLRepo interface {
	// Upsert сохраняет результат проверки: вставляет или обновляет по домену
	Upsert(ctx context.Context, check *SSLCheck) error
	// GetAll возвращает последние результаты по каждому домену
	GetAll(ctx context.Context) ([]*SSLCheck, error)
}

type pgSSLRepo struct {
	db *DB
}

// NewSSLRepo создаёт репозиторий SSL-проверок
func NewSSLRepo(db *DB) SSLRepo {
	return &pgSSLRepo{db: db}
}

func (r *pgSSLRepo) Upsert(ctx context.Context, c *SSLCheck) error {
	_, err := r.db.Primary.Exec(ctx,
		`INSERT INTO ssl_checks (domain, issuer, expires_at, days_left, status, checked_at)
		 VALUES ($1, $2, $3, $4, $5, now())
		 ON CONFLICT (domain) DO UPDATE SET
		     issuer     = EXCLUDED.issuer,
		     expires_at = EXCLUDED.expires_at,
		     days_left  = EXCLUDED.days_left,
		     status     = EXCLUDED.status,
		     checked_at = now()`,
		c.Domain, c.Issuer, c.ExpiresAt, c.DaysLeft, c.Status,
	)
	if err != nil {
		return fmt.Errorf("Upsert ssl_check %q: %w", c.Domain, err)
	}
	return nil
}

func (r *pgSSLRepo) GetAll(ctx context.Context) ([]*SSLCheck, error) {
	rows, err := r.db.ReadPool().Query(ctx,
		`SELECT id, domain, issuer, expires_at, days_left, status, checked_at
		 FROM ssl_checks ORDER BY domain`,
	)
	if err != nil {
		return nil, fmt.Errorf("GetAll ssl_checks: %w", err)
	}
	defer rows.Close()

	var checks []*SSLCheck
	for rows.Next() {
		c := &SSLCheck{}
		if err := rows.Scan(
			&c.ID, &c.Domain, &c.Issuer, &c.ExpiresAt,
			&c.DaysLeft, &c.Status, &c.CheckedAt,
		); err != nil {
			return nil, fmt.Errorf("scan ssl_check: %w", err)
		}
		checks = append(checks, c)
	}
	return checks, nil
}

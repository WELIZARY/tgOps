package storage

import (
	"context"
	"fmt"
)

// CronRepo - интерфейс для работы со снапшотами cron-задач
type CronRepo interface {
	// Save сохраняет новый снапшот после сбора с сервера
	Save(ctx context.Context, snap *CronSnapshot) error
	// GetLatest возвращает последний снапшот для сервера по источнику
	GetLatest(ctx context.Context, serverName, source string) (*CronSnapshot, error)
}

type pgCronRepo struct {
	db *DB
}

// NewCronRepo создаёт репозиторий снапшотов cron
func NewCronRepo(db *DB) CronRepo {
	return &pgCronRepo{db: db}
}

func (r *pgCronRepo) Save(ctx context.Context, snap *CronSnapshot) error {
	err := r.db.Primary.QueryRow(ctx,
		`INSERT INTO cron_snapshots (server_name, source, raw_output)
		 VALUES ($1, $2, $3)
		 RETURNING id, collected_at`,
		snap.ServerName, snap.Source, snap.RawOutput,
	).Scan(&snap.ID, &snap.CollectedAt)
	if err != nil {
		return fmt.Errorf("Save cron_snapshot: %w", err)
	}
	return nil
}

func (r *pgCronRepo) GetLatest(ctx context.Context, serverName, source string) (*CronSnapshot, error) {
	snap := &CronSnapshot{}
	err := r.db.ReadPool().QueryRow(ctx,
		`SELECT id, server_name, source, raw_output, collected_at
		 FROM cron_snapshots
		 WHERE server_name = $1 AND source = $2
		 ORDER BY collected_at DESC
		 LIMIT 1`,
		serverName, source,
	).Scan(&snap.ID, &snap.ServerName, &snap.Source, &snap.RawOutput, &snap.CollectedAt)
	if err != nil {
		return nil, fmt.Errorf("GetLatest cron_snapshot %s/%s: %w", serverName, source, err)
	}
	return snap, nil
}

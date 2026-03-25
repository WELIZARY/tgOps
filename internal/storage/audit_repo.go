package storage

import (
	"context"
	"fmt"
)

// AuditRepo - интерфейс для записи и чтения аудит-лога
type AuditRepo interface {
	Write(ctx context.Context, entry *AuditEntry) error
	List(ctx context.Context, limit int) ([]*AuditEntry, error)
	ListByUser(ctx context.Context, userID, limit int) ([]*AuditEntry, error)
}

type pgAuditRepo struct {
	db *DB
}

// NewAuditRepo создаёт репозиторий аудит-лога
func NewAuditRepo(db *DB) AuditRepo {
	return &pgAuditRepo{db: db}
}

func (r *pgAuditRepo) Write(ctx context.Context, e *AuditEntry) error {
	_, err := r.db.Primary.Exec(ctx,
		`INSERT INTO audit_log (user_id, command, args, result, duration_ms)
		 VALUES ($1, $2, $3, $4, $5)`,
		e.UserID, e.Command, e.Args, e.Result, e.DurationMs,
	)
	if err != nil {
		return fmt.Errorf("Write audit: %w", err)
	}
	return nil
}

func (r *pgAuditRepo) List(ctx context.Context, limit int) ([]*AuditEntry, error) {
	rows, err := r.db.ReadPool().Query(ctx,
		`SELECT id, user_id, command, args, result, duration_ms, created_at
		 FROM audit_log ORDER BY created_at DESC LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("List audit: %w", err)
	}
	defer rows.Close()

	var entries []*AuditEntry
	for rows.Next() {
		e := &AuditEntry{}
		if err := rows.Scan(
			&e.ID, &e.UserID, &e.Command, &e.Args, &e.Result, &e.DurationMs, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan audit entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func (r *pgAuditRepo) ListByUser(ctx context.Context, userID, limit int) ([]*AuditEntry, error) {
	rows, err := r.db.ReadPool().Query(ctx,
		`SELECT id, user_id, command, args, result, duration_ms, created_at
		 FROM audit_log WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2`,
		userID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("ListByUser audit: %w", err)
	}
	defer rows.Close()

	var entries []*AuditEntry
	for rows.Next() {
		e := &AuditEntry{}
		if err := rows.Scan(
			&e.ID, &e.UserID, &e.Command, &e.Args, &e.Result, &e.DurationMs, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan audit entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

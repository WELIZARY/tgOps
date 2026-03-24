package storage

import (
	"context"
	"fmt"
	"time"
)

// AlertRepo - интерфейс для работы с алертами
type AlertRepo interface {
	Create(ctx context.Context, a *Alert) error
	ListUnacknowledged(ctx context.Context) ([]*Alert, error)
	Acknowledge(ctx context.Context, alertID, byUserID int) error
}

type pgAlertRepo struct {
	db *DB
}

// NewAlertRepo создаёт репозиторий алертов
func NewAlertRepo(db *DB) AlertRepo {
	return &pgAlertRepo{db: db}
}

func (r *pgAlertRepo) Create(ctx context.Context, a *Alert) error {
	_, err := r.db.Primary.Exec(ctx,
		`INSERT INTO alerts (server_name, alert_type, severity, message)
		 VALUES ($1, $2, $3, $4)`,
		a.ServerName, a.AlertType, a.Severity, a.Message,
	)
	if err != nil {
		return fmt.Errorf("Create alert: %w", err)
	}
	return nil
}

func (r *pgAlertRepo) ListUnacknowledged(ctx context.Context) ([]*Alert, error) {
	rows, err := r.db.ReadPool().Query(ctx,
		`SELECT id, server_name, alert_type, severity, message,
		        acknowledged, ack_by, ack_at, created_at
		 FROM alerts WHERE NOT acknowledged ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("ListUnacknowledged: %w", err)
	}
	defer rows.Close()

	var alerts []*Alert
	for rows.Next() {
		a := &Alert{}
		if err := rows.Scan(
			&a.ID, &a.ServerName, &a.AlertType, &a.Severity, &a.Message,
			&a.Acknowledged, &a.AckBy, &a.AckAt, &a.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan alert: %w", err)
		}
		alerts = append(alerts, a)
	}
	return alerts, nil
}

func (r *pgAlertRepo) Acknowledge(ctx context.Context, alertID, byUserID int) error {
	now := time.Now()
	_, err := r.db.Primary.Exec(ctx,
		`UPDATE alerts SET acknowledged = true, ack_by = $1, ack_at = $2 WHERE id = $3`,
		byUserID, now, alertID,
	)
	if err != nil {
		return fmt.Errorf("Acknowledge alert: %w", err)
	}
	return nil
}

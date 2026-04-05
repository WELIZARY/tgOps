package storage

import (
	"context"
	"fmt"
	"time"
)

// PipelineRepo - интерфейс для работы с событиями CI/CD пайплайнов
type PipelineRepo interface {
	Create(ctx context.Context, e *PipelineEvent) error
	GetByID(ctx context.Context, id int) (*PipelineEvent, error)
	GetRecent(ctx context.Context, limit int) ([]*PipelineEvent, error)
	// Approve помечает пайплайн как подтверждённый: статус → approved, фиксирует кто и когда
	Approve(ctx context.Context, id int, approvedBy int) error
	// Reject помечает пайплайн как отклонённый: статус → rejected
	Reject(ctx context.Context, id int, rejectedBy int) error
	// UpdateTGMessage сохраняет Telegram message_id для последующего редактирования сообщения
	UpdateTGMessage(ctx context.Context, id int, tgMessageID int) error
	// UpdateStatus обновляет статус при получении нового webhook-события
	UpdateStatus(ctx context.Context, id int, status string) error
}

type pgPipelineRepo struct {
	db *DB
}

// NewPipelineRepo создаёт репозиторий пайплайнов
func NewPipelineRepo(db *DB) PipelineRepo {
	return &pgPipelineRepo{db: db}
}

func (r *pgPipelineRepo) Create(ctx context.Context, e *PipelineEvent) error {
	err := r.db.Primary.QueryRow(ctx,
		`INSERT INTO pipeline_events (pipeline_id, source, repo, branch, status, author, payload)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, created_at`,
		e.PipelineID, e.Source, e.Repo, e.Branch, e.Status, e.Author, e.Payload,
	).Scan(&e.ID, &e.CreatedAt)
	if err != nil {
		return fmt.Errorf("Create pipeline_event: %w", err)
	}
	return nil
}

func (r *pgPipelineRepo) GetByID(ctx context.Context, id int) (*PipelineEvent, error) {
	e := &PipelineEvent{}
	err := r.db.ReadPool().QueryRow(ctx,
		`SELECT id, pipeline_id, source, repo, branch, status, author,
		        approved_by, approved_at, tg_message_id, payload, created_at
		 FROM pipeline_events WHERE id = $1`,
		id,
	).Scan(
		&e.ID, &e.PipelineID, &e.Source, &e.Repo, &e.Branch, &e.Status, &e.Author,
		&e.ApprovedBy, &e.ApprovedAt, &e.TGMessageID, &e.Payload, &e.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("GetByID pipeline_event %d: %w", id, err)
	}
	return e, nil
}

func (r *pgPipelineRepo) GetRecent(ctx context.Context, limit int) ([]*PipelineEvent, error) {
	rows, err := r.db.ReadPool().Query(ctx,
		`SELECT id, pipeline_id, source, repo, branch, status, author,
		        approved_by, approved_at, tg_message_id, payload, created_at
		 FROM pipeline_events
		 ORDER BY created_at DESC
		 LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("GetRecent pipeline_events: %w", err)
	}
	defer rows.Close()

	var events []*PipelineEvent
	for rows.Next() {
		e := &PipelineEvent{}
		if err := rows.Scan(
			&e.ID, &e.PipelineID, &e.Source, &e.Repo, &e.Branch, &e.Status, &e.Author,
			&e.ApprovedBy, &e.ApprovedAt, &e.TGMessageID, &e.Payload, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan pipeline_event: %w", err)
		}
		events = append(events, e)
	}
	return events, nil
}

func (r *pgPipelineRepo) Approve(ctx context.Context, id int, approvedBy int) error {
	now := time.Now()
	_, err := r.db.Primary.Exec(ctx,
		`UPDATE pipeline_events
		 SET status = $1, approved_by = $2, approved_at = $3
		 WHERE id = $4`,
		PipelineStatusApproved, approvedBy, now, id,
	)
	if err != nil {
		return fmt.Errorf("Approve pipeline_event %d: %w", id, err)
	}
	return nil
}

func (r *pgPipelineRepo) Reject(ctx context.Context, id int, rejectedBy int) error {
	now := time.Now()
	_, err := r.db.Primary.Exec(ctx,
		`UPDATE pipeline_events
		 SET status = $1, approved_by = $2, approved_at = $3
		 WHERE id = $4`,
		PipelineStatusRejected, rejectedBy, now, id,
	)
	if err != nil {
		return fmt.Errorf("Reject pipeline_event %d: %w", id, err)
	}
	return nil
}

func (r *pgPipelineRepo) UpdateTGMessage(ctx context.Context, id int, tgMessageID int) error {
	_, err := r.db.Primary.Exec(ctx,
		`UPDATE pipeline_events SET tg_message_id = $1 WHERE id = $2`,
		tgMessageID, id,
	)
	if err != nil {
		return fmt.Errorf("UpdateTGMessage pipeline_event %d: %w", id, err)
	}
	return nil
}

func (r *pgPipelineRepo) UpdateStatus(ctx context.Context, id int, status string) error {
	_, err := r.db.Primary.Exec(ctx,
		`UPDATE pipeline_events SET status = $1 WHERE id = $2`,
		status, id,
	)
	if err != nil {
		return fmt.Errorf("UpdateStatus pipeline_event %d: %w", id, err)
	}
	return nil
}

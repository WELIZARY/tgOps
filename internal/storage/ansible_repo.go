package storage

import (
	"context"
	"fmt"
	"time"
)

// AnsibleRepo - интерфейс для работы с историей запусков Ansible-плейбуков
type AnsibleRepo interface {
	// Create сохраняет новый запуск, заполняет ID и StartedAt из БД
	Create(ctx context.Context, r *AnsibleRun) error
	// Finish обновляет статус, вывод и время завершения после окончания плейбука
	Finish(ctx context.Context, id int, status, output string, durationMs int) error
	// GetRecent возвращает последние N запусков, отсортированных по времени
	GetRecent(ctx context.Context, limit int) ([]*AnsibleRun, error)
	// GetByID возвращает запуск по ID
	GetByID(ctx context.Context, id int) (*AnsibleRun, error)
}

type pgAnsibleRepo struct {
	db *DB
}

// NewAnsibleRepo создаёт репозиторий запусков Ansible
func NewAnsibleRepo(db *DB) AnsibleRepo {
	return &pgAnsibleRepo{db: db}
}

func (r *pgAnsibleRepo) Create(ctx context.Context, run *AnsibleRun) error {
	err := r.db.Primary.QueryRow(ctx,
		`INSERT INTO ansible_runs (playbook_name, playbook_file, started_by, status)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, started_at`,
		run.PlaybookName, run.PlaybookFile, run.StartedBy, AnsibleRunRunning,
	).Scan(&run.ID, &run.StartedAt)
	if err != nil {
		return fmt.Errorf("Create ansible_run: %w", err)
	}
	return nil
}

func (r *pgAnsibleRepo) Finish(ctx context.Context, id int, status, output string, durationMs int) error {
	now := time.Now()
	_, err := r.db.Primary.Exec(ctx,
		`UPDATE ansible_runs
		 SET status = $1, output = $2, finished_at = $3, duration_ms = $4
		 WHERE id = $5`,
		status, output, now, durationMs, id,
	)
	if err != nil {
		return fmt.Errorf("Finish ansible_run %d: %w", id, err)
	}
	return nil
}

func (r *pgAnsibleRepo) GetRecent(ctx context.Context, limit int) ([]*AnsibleRun, error) {
	rows, err := r.db.ReadPool().Query(ctx,
		`SELECT id, playbook_name, playbook_file, started_by, status,
		        output, started_at, finished_at, duration_ms
		 FROM ansible_runs
		 ORDER BY started_at DESC
		 LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("GetRecent ansible_runs: %w", err)
	}
	defer rows.Close()

	var runs []*AnsibleRun
	for rows.Next() {
		run := &AnsibleRun{}
		if err := rows.Scan(
			&run.ID, &run.PlaybookName, &run.PlaybookFile, &run.StartedBy, &run.Status,
			&run.Output, &run.StartedAt, &run.FinishedAt, &run.DurationMs,
		); err != nil {
			return nil, fmt.Errorf("scan ansible_run: %w", err)
		}
		runs = append(runs, run)
	}
	return runs, nil
}

func (r *pgAnsibleRepo) GetByID(ctx context.Context, id int) (*AnsibleRun, error) {
	run := &AnsibleRun{}
	err := r.db.ReadPool().QueryRow(ctx,
		`SELECT id, playbook_name, playbook_file, started_by, status,
		        output, started_at, finished_at, duration_ms
		 FROM ansible_runs WHERE id = $1`,
		id,
	).Scan(
		&run.ID, &run.PlaybookName, &run.PlaybookFile, &run.StartedBy, &run.Status,
		&run.Output, &run.StartedAt, &run.FinishedAt, &run.DurationMs,
	)
	if err != nil {
		return nil, fmt.Errorf("GetByID ansible_run %d: %w", id, err)
	}
	return run, nil
}

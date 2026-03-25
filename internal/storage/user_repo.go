package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// UserRepo - интерфейс для работы с пользователями
type UserRepo interface {
	GetByTelegramID(ctx context.Context, telegramID int64) (*User, error)
	Create(ctx context.Context, u *User) error
	UpdateRole(ctx context.Context, telegramID int64, role string) error
	List(ctx context.Context) ([]*User, error)
	Count(ctx context.Context) (int, error)
}

type pgUserRepo struct {
	db *DB
}

// NewUserRepo создаёт репозиторий пользователей
func NewUserRepo(db *DB) UserRepo {
	return &pgUserRepo{db: db}
}

func (r *pgUserRepo) GetByTelegramID(ctx context.Context, telegramID int64) (*User, error) {
	u := &User{}
	err := r.db.ReadPool().QueryRow(ctx,
		`SELECT id, telegram_id, username, role, is_active, created_at, updated_at
		 FROM users WHERE telegram_id = $1`,
		telegramID,
	).Scan(&u.ID, &u.TelegramID, &u.Username, &u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("GetByTelegramID: %w", err)
	}
	return u, nil
}

func (r *pgUserRepo) Create(ctx context.Context, u *User) error {
	_, err := r.db.Primary.Exec(ctx,
		`INSERT INTO users (telegram_id, username, role, is_active)
		 VALUES ($1, $2, $3, $4)`,
		u.TelegramID, u.Username, u.Role, u.IsActive,
	)
	if err != nil {
		return fmt.Errorf("Create user: %w", err)
	}
	return nil
}

func (r *pgUserRepo) UpdateRole(ctx context.Context, telegramID int64, role string) error {
	_, err := r.db.Primary.Exec(ctx,
		`UPDATE users SET role = $1, updated_at = $2 WHERE telegram_id = $3`,
		role, time.Now(), telegramID,
	)
	if err != nil {
		return fmt.Errorf("UpdateRole: %w", err)
	}
	return nil
}

func (r *pgUserRepo) List(ctx context.Context) ([]*User, error) {
	rows, err := r.db.ReadPool().Query(ctx,
		`SELECT id, telegram_id, username, role, is_active, created_at, updated_at
		 FROM users ORDER BY created_at`,
	)
	if err != nil {
		return nil, fmt.Errorf("List users: %w", err)
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		u := &User{}
		if err := rows.Scan(
			&u.ID, &u.TelegramID, &u.Username, &u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, u)
	}
	return users, nil
}

func (r *pgUserRepo) Count(ctx context.Context) (int, error) {
	var count int
	err := r.db.ReadPool().QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("Count users: %w", err)
	}
	return count, nil
}

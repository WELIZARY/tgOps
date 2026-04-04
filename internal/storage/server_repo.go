package storage

import (
	"context"
	"fmt"
)

// ServerRepo - интерфейс для работы с таблицей servers
type ServerRepo interface {
	GetAll(ctx context.Context) ([]*Server, error)
	GetByName(ctx context.Context, name string) (*Server, error)
}

type pgServerRepo struct {
	db *DB
}

// NewServerRepo создаёт репозиторий серверов
func NewServerRepo(db *DB) ServerRepo {
	return &pgServerRepo{db: db}
}

func (r *pgServerRepo) GetAll(ctx context.Context) ([]*Server, error) {
	rows, err := r.db.ReadPool().Query(ctx,
		`SELECT id, name, host, port, ssh_user, key_name, is_active, created_at
		 FROM servers WHERE is_active = true ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("GetAll servers: %w", err)
	}
	defer rows.Close()

	var servers []*Server
	for rows.Next() {
		s := &Server{}
		if err := rows.Scan(
			&s.ID, &s.Name, &s.Host, &s.Port, &s.SSHUser,
			&s.KeyName, &s.IsActive, &s.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan server: %w", err)
		}
		servers = append(servers, s)
	}
	return servers, nil
}

func (r *pgServerRepo) GetByName(ctx context.Context, name string) (*Server, error) {
	s := &Server{}
	err := r.db.ReadPool().QueryRow(ctx,
		`SELECT id, name, host, port, ssh_user, key_name, is_active, created_at
		 FROM servers WHERE name = $1 AND is_active = true`,
		name,
	).Scan(&s.ID, &s.Name, &s.Host, &s.Port, &s.SSHUser,
		&s.KeyName, &s.IsActive, &s.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("GetByName server %q: %w", name, err)
	}
	return s, nil
}

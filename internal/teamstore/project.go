package teamstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func (s *Store) CreateProject(ctx context.Context, p *Project) error {
	if s.db == nil {
		return errors.New("teamstore: db not configured")
	}

	s.db.WMu.Lock()
	defer s.db.WMu.Unlock()

	now := time.Now().Format(time.RFC3339)
	p.CreatedAt = now
	p.UpdatedAt = now

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO projects (id, name, path, description, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Path, p.Description, p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("teamstore: create project: %w", err)
	}
	return nil
}

func (s *Store) GetProject(ctx context.Context, id string) (*Project, error) {
	if s.db == nil {
		return nil, errors.New("teamstore: db not configured")
	}

	var p Project
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, path, description, created_at, updated_at FROM projects WHERE id = ? OR name = ?`,
		id, id,
	).Scan(&p.ID, &p.Name, &p.Path, &p.Description, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("teamstore: project %q not found", id)
		}
		return nil, fmt.Errorf("teamstore: get project: %w", err)
	}
	return &p, nil
}

func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	if s.db == nil {
		return nil, nil
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, path, description, created_at, updated_at FROM projects ORDER BY name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("teamstore: list projects: %w", err)
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Path, &p.Description, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("teamstore: scan project: %w", err)
		}
		projects = append(projects, p)
	}
	return projects, nil
}

func (s *Store) UpdateProject(ctx context.Context, id string, p *Project) error {
	if s.db == nil {
		return errors.New("teamstore: db not configured")
	}

	s.db.WMu.Lock()
	defer s.db.WMu.Unlock()

	now := time.Now().Format(time.RFC3339)
	p.UpdatedAt = now

	_, err := s.db.ExecContext(ctx,
		`UPDATE projects SET name = ?, path = ?, description = ?, updated_at = ? WHERE id = ?`,
		p.Name, p.Path, p.Description, p.UpdatedAt, id,
	)
	if err != nil {
		return fmt.Errorf("teamstore: update project: %w", err)
	}
	return nil
}

func (s *Store) DeleteProject(ctx context.Context, id string) error {
	if s.db == nil {
		return errors.New("teamstore: db not configured")
	}

	s.db.WMu.Lock()
	defer s.db.WMu.Unlock()

	_, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("teamstore: delete project: %w", err)
	}
	return nil
}

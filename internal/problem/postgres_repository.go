package problem

import (
	"context"
	"database/sql"
)

type postgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(db *sql.DB) Repository {
	return postgresRepository{db: db}
}

func (r postgresRepository) List(ctx context.Context) ([]Problem, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, title, description, difficulty, starter_code, test_code, created_at, updated_at
		FROM problems
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var problems []Problem
	for rows.Next() {
		var p Problem
		if err := rows.Scan(&p.ID, &p.Title, &p.Description, &p.Difficulty, &p.StarterCode, &p.TestCode, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		problems = append(problems, p)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return problems, nil
}

func (r postgresRepository) Get(ctx context.Context, id string) (Problem, error) {
	var p Problem
	err := r.db.QueryRowContext(ctx, `
		SELECT id, title, description, difficulty, starter_code, test_code, created_at, updated_at
		FROM problems
		WHERE id = $1
	`, id).Scan(&p.ID, &p.Title, &p.Description, &p.Difficulty, &p.StarterCode, &p.TestCode, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return Problem{}, ErrNotFound
		}
		return Problem{}, err
	}

	return p, nil
}

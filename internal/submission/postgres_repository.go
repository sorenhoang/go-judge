package submission

import (
	"context"
	"database/sql"
	"errors"
)

type postgresRepository struct {
	db *sql.DB
}

// UpdateResult implements [Repository].
func (r postgresRepository) UpdateResult(ctx context.Context, id string, status Status, output string, totalTests int, passedTests int) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE submissions
		SET status = $1, output = $2, total_tests = $3, passed_tests = $4, updated_at = NOW()
		WHERE id = $5
	`, status, output, totalTests, passedTests, id)

	return err
}

func NewPostgresRepository(db *sql.DB) Repository {
	return postgresRepository{db: db}
}

func (r postgresRepository) Create(ctx context.Context, s Submission) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO submissions (id, problem_id, code, status, output, total_tests, passed_tests)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, s.ID, s.ProblemID, s.Code, s.Status, s.Output, s.TotalTests, s.PassedTests)

	return err
}

func (r postgresRepository) GetByID(ctx context.Context, id string) (Submission, error) {
	var s Submission
	err := r.db.QueryRowContext(ctx, `
		SELECT id, problem_id, code, status, output, total_tests, passed_tests, created_at, updated_at
		FROM submissions
		WHERE id = $1
	`, id).Scan(
		&s.ID,
		&s.ProblemID,
		&s.Code,
		&s.Status,
		&s.Output,
		&s.TotalTests,
		&s.PassedTests,
		&s.CreatedAt,
		&s.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Submission{}, ErrNotFound
	}
	if err != nil {
		return Submission{}, err
	}

	return s, nil
}

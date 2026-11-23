package submission

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type postgresRepository struct {
	db *sql.DB
}

// UpdateStatus implements [Repository].
func (r postgresRepository) UpdateStatus(ctx context.Context, id string, status Status) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE submissions
		SET status = $1, updated_at = NOW()
		WHERE id = $2
	`, status, id)

	return err
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

func (r postgresRepository) IncrementRetryCount(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE submissions
		SET retry_count = retry_count + 1, updated_at = NOW()
		WHERE id = $1
	`, id)

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
		SELECT id, problem_id, code, status, output, total_tests, passed_tests, retry_count, created_at, updated_at
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
		&s.RetryCount,
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

func (r postgresRepository) RequeueStale(ctx context.Context, staleAfter, maxAge time.Duration, limit int) ([]Submission, error) {
	// SKIP LOCKED + touching updated_at makes this a claim: concurrent
	// sweepers (several API instances) never republish the same row twice
	rows, err := r.db.QueryContext(ctx, `
		UPDATE submissions
		SET updated_at = NOW()
		WHERE id IN (
			SELECT id FROM submissions
			WHERE status IN ($1, $2)
				AND updated_at < NOW() - make_interval(secs => $3)
				AND created_at >= NOW() - make_interval(secs => $4)
			ORDER BY updated_at
			LIMIT $5
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, problem_id
	`, StatusPending, StatusRunning, staleAfter.Seconds(), maxAge.Seconds(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stale []Submission
	for rows.Next() {
		var s Submission
		if err := rows.Scan(&s.ID, &s.ProblemID); err != nil {
			return nil, err
		}
		stale = append(stale, s)
	}
	return stale, rows.Err()
}

func (r postgresRepository) FailAbandoned(ctx context.Context, maxAge time.Duration) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE submissions
		SET status = $1, output = $2, updated_at = NOW()
		WHERE status IN ($3, $4)
			AND created_at < NOW() - make_interval(secs => $5)
	`, StatusError, fmt.Sprintf("gave up: not judged within %s", maxAge), StatusPending, StatusRunning, maxAge.Seconds())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

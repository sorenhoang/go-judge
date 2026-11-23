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

func (r postgresRepository) Claim(ctx context.Context, id string, lease time.Duration) (bool, error) {
	// updated_at is the lease: a RUNNING row can only be taken over once its
	// worker hasn't touched it for lease (it died or hung)
	res, err := r.db.ExecContext(ctx, `
		UPDATE submissions
		SET status = $1, updated_at = NOW()
		WHERE id = $2
			AND (status = $3 OR (status = $1 AND updated_at < NOW() - make_interval(secs => $4)))
	`, StatusRunning, id, StatusPending, lease.Seconds())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (r postgresRepository) UpdateResult(ctx context.Context, id string, status Status, output string, totalTests int, passedTests int) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE submissions
		SET status = $1, output = $2, total_tests = $3, passed_tests = $4, updated_at = NOW()
		WHERE id = $5 AND status IN ($6, $7)
	`, status, output, totalTests, passedTests, id, StatusPending, StatusRunning)

	return err
}

func (r postgresRepository) MarkForRetry(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE submissions
		SET status = $1, retry_count = retry_count + 1, updated_at = NOW()
		WHERE id = $2 AND status IN ($1, $3)
	`, StatusPending, id, StatusRunning)

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

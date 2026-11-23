package submission

import (
	"context"
	"database/sql"
	"os"
	"sort"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Needs a migrated throwaway Postgres, e.g. TEST_DATABASE_URL=postgres://e2e:e2e@localhost:55432/e2e?sslmode=disable
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`DELETE FROM submissions`); err != nil {
		t.Fatal(err)
	}
	return db
}

func insertAged(t *testing.T, db *sql.DB, id string, status Status, created, updated time.Duration) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO submissions (id, problem_id, code, status, created_at, updated_at)
		VALUES ($1, 'sum-slice', 'x', $2, NOW() - make_interval(secs => $3), NOW() - make_interval(secs => $4))
	`, id, status, created.Seconds(), updated.Seconds())
	if err != nil {
		t.Fatal(err)
	}
}

func TestRequeueStaleAndFailAbandoned(t *testing.T) {
	db := testDB(t)
	repo := NewPostgresRepository(db)
	ctx := context.Background()

	insertAged(t, db, "fresh-pending", StatusPending, time.Minute, time.Minute)
	insertAged(t, db, "stale-pending", StatusPending, 10*time.Minute, 10*time.Minute)
	insertAged(t, db, "stale-running", StatusRunning, 10*time.Minute, 10*time.Minute)
	insertAged(t, db, "stale-passed", StatusPassed, 10*time.Minute, 10*time.Minute)
	insertAged(t, db, "ancient-pending", StatusPending, 3*time.Hour, 3*time.Hour)

	stale, err := repo.RequeueStale(ctx, 5*time.Minute, time.Hour, 100)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range stale {
		ids = append(ids, s.ID)
		if s.ProblemID != "sum-slice" {
			t.Fatalf("ProblemID = %q", s.ProblemID)
		}
	}
	sort.Strings(ids)
	if len(ids) != 2 || ids[0] != "stale-pending" || ids[1] != "stale-running" {
		t.Fatalf("requeued %v, want [stale-pending stale-running]", ids)
	}

	// requeueing touches updated_at, so the next sweep doesn't publish them again right away
	again, err := repo.RequeueStale(ctx, 5*time.Minute, time.Hour, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("second sweep requeued %d rows, want 0", len(again))
	}

	failed, err := repo.FailAbandoned(ctx, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if failed != 1 {
		t.Fatalf("FailAbandoned = %d, want 1", failed)
	}
	s, err := repo.GetByID(ctx, "ancient-pending")
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != StatusError || s.Output == nil || *s.Output == "" {
		t.Fatalf("ancient-pending = %s %v, want ERROR with a message", s.Status, s.Output)
	}
}

func TestClaimIsExclusiveUntilTheLeaseExpires(t *testing.T) {
	db := testDB(t)
	repo := NewPostgresRepository(db)
	ctx := context.Background()
	lease := time.Minute

	insertAged(t, db, "pending", StatusPending, 0, 0)
	insertAged(t, db, "abandoned-running", StatusRunning, 5*time.Minute, 5*time.Minute)
	insertAged(t, db, "passed", StatusPassed, 0, 0)

	claim := func(id string) bool {
		t.Helper()
		ok, err := repo.Claim(ctx, id, lease)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}

	if !claim("pending") {
		t.Fatal("first claim of a PENDING submission failed")
	}
	if claim("pending") {
		t.Fatal("second worker claimed a submission whose lease is still held")
	}
	if !claim("abandoned-running") {
		t.Fatal("couldn't claim a RUNNING submission whose lease expired (its worker died)")
	}
	if claim("passed") {
		t.Fatal("claimed a finished submission")
	}
}

func TestUpdateResultDoesNotOverwriteAFinishedSubmission(t *testing.T) {
	db := testDB(t)
	repo := NewPostgresRepository(db)
	ctx := context.Background()

	insertAged(t, db, "s", StatusRunning, 0, 0)
	if err := repo.UpdateResult(ctx, "s", StatusPassed, "first", 1, 1); err != nil {
		t.Fatal(err)
	}
	// a duplicate run finishing later must not replace the first verdict
	if err := repo.UpdateResult(ctx, "s", StatusError, "second", 0, 0); err != nil {
		t.Fatal(err)
	}

	s, err := repo.GetByID(ctx, "s")
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != StatusPassed || s.Output == nil || *s.Output != "first" {
		t.Fatalf("got %s %v, want the first result PASSED/first", s.Status, s.Output)
	}
}

func TestMarkForRetryReleasesTheClaim(t *testing.T) {
	db := testDB(t)
	repo := NewPostgresRepository(db)
	ctx := context.Background()

	insertAged(t, db, "s", StatusPending, 0, 0)
	if ok, err := repo.Claim(ctx, "s", time.Minute); err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if err := repo.MarkForRetry(ctx, "s"); err != nil {
		t.Fatal(err)
	}

	s, err := repo.GetByID(ctx, "s")
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != StatusPending || s.RetryCount != 1 {
		t.Fatalf("got %s retry_count=%d, want PENDING retry_count=1", s.Status, s.RetryCount)
	}
	// the retried job must not have to wait for the lease to expire
	if ok, err := repo.Claim(ctx, "s", time.Minute); err != nil || !ok {
		t.Fatalf("reclaim after MarkForRetry: %v %v", ok, err)
	}
}

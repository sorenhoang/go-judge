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

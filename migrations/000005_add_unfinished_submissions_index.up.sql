-- the stale-submission sweeper scans unfinished rows by updated_at every minute
CREATE INDEX idx_submissions_unfinished_updated_at ON submissions (updated_at)
WHERE status IN ('PENDING', 'RUNNING');

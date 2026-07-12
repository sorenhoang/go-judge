CREATE TABLE submissions (
    id TEXT PRIMARY KEY,
    problem_id TEXT NOT NULL,
    code TEXT NOT NULL,
    status TEXT NOT NULL,
    output TEXT,
    total_tests INT NOT NULL DEFAULT 0,
    passed_tests INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
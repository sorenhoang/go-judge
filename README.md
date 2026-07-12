# go-judge

A local LeetCode-style judge system built with Go to practice Go syntax, REST APIs, gRPC, message queues, and worker-based async processing.

## Status

✅ Phase 1 — Monolith Judge MVP: React frontend → Go API → Postgres → runs real `go test` and returns a verdict (PASSED/FAILED/ERROR).
✅ Phase 2 — Code execution extracted into a separate `code-runner` gRPC service; the API no longer runs `go test` itself.
Next up: Phase 3 (RabbitMQ + async Judge Worker). See the implementation plan for details.

## Running locally

```bash
docker compose up -d              # postgres
migrate -path migrations -database "$DATABASE_URL" up

go run ./cmd/runner                # code runner (gRPC) on :50051
go run ./cmd/api                   # backend on :8080

cd frontend && npm install && npm run dev   # frontend on :5173
```

## Docs

- [High-level idea](docs/go-judge-high-level-idea.md) — what this project is and why
- [Implementation plan](docs/go-judge-implementation-plan.md) — detailed phase-by-phase plan with input/output/acceptance criteria ([HTML version](docs/go-judge-implementation-plan.html))

## Architecture (target)

```
Frontend → Backend API → Postgres
              |
              | (Phase 2+) gRPC
              v
        Code Runner Service
              ^
              | (Phase 3+) RabbitMQ
              |
        Judge Worker
```

## Tech stack

- **Backend:** Go, chi, pgx/database/sql, golang-migrate
- **Communication:** gRPC (Backend ↔ Code Runner, done), RabbitMQ (Backend ↔ Worker, Phase 3)
- **Frontend:** React, Vite, TypeScript
- **Infra:** Docker Compose, PostgreSQL, RabbitMQ

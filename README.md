# go-judge

A local LeetCode-style judge system built with Go to practice Go syntax, REST APIs, gRPC, message queues, and worker-based async processing.

## Status

✅ Phase 1 — Monolith Judge MVP: React frontend → Go API → Postgres → runs real `go test` and returns a verdict (PASSED/FAILED/ERROR).
✅ Phase 2 — Code execution extracted into a separate `code-runner` gRPC service; the API no longer runs `go test` itself.
✅ Phase 3 — Judging is now async: the API publishes a job to RabbitMQ and returns `PENDING` immediately; a separate `judge-worker` consumes it, runs the code via the gRPC runner, and writes the verdict back. Frontend polls until the verdict lands.
Next up: Phase 4 (retry, DLQ, idempotency, graceful shutdown). See the implementation plan for details.

## Running locally

Four processes, run in order (each needs the previous one up):

```bash
docker compose up -d                            # postgres + rabbitmq
migrate -path migrations -database "$DATABASE_URL" up

go run ./cmd/runner                              # code runner (gRPC) on :50051
go run ./cmd/worker                              # judge worker (consumes RabbitMQ)
go run ./cmd/api                                 # backend on :8080

cd frontend && npm install && npm run dev        # frontend on :5173
```

RabbitMQ management UI: `http://localhost:15672` (`gojudge`/`gojudge`) — handy to watch jobs flow through the queue.

## Docs

- [High-level idea](docs/go-judge-high-level-idea.md) — what this project is and why
- [Implementation plan](docs/go-judge-implementation-plan.md) — detailed phase-by-phase plan with input/output/acceptance criteria ([HTML version](docs/go-judge-implementation-plan.html))

## Architecture (current)

```
Frontend → Backend API → Postgres
              |
              | publish
              v
          RabbitMQ
              |
              | consume
              v
        Judge Worker
              |
              | gRPC
              v
        Code Runner Service
```

## Tech stack

- **Backend:** Go, chi, pgx/database/sql, golang-migrate
- **Communication:** gRPC (Backend Worker ↔ Code Runner), RabbitMQ (Backend API ↔ Worker)
- **Frontend:** React, Vite, TypeScript
- **Infra:** Docker Compose, PostgreSQL, RabbitMQ

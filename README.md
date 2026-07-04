# go-judge

A local LeetCode-style judge system built with Go to practice Go syntax, REST APIs, gRPC, message queues, and worker-based async processing.

## Status

🚧 Not started yet — currently on Phase 0 (project setup). See the implementation plan for progress.

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
- **Communication:** gRPC (Backend ↔ Code Runner), RabbitMQ (Backend ↔ Worker)
- **Frontend:** React, Vite, TypeScript
- **Infra:** Docker Compose, PostgreSQL, RabbitMQ

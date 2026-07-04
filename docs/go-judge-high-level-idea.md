# go-judge — High-Level Idea

## 1. Project Overview

**go-judge** is a local coding judge system for practicing Go syntax while learning backend architecture concepts.

The project is inspired by a simplified LeetCode-style judge system, but it is intentionally scoped for local development and learning.

The main goal is to build a system where a user can:

1. Open a simple frontend.
2. Choose a Go problem.
3. Write and submit Go code.
4. Let the backend create a submission.
5. Process the submission asynchronously.
6. Run server-side tests.
7. Return a verdict such as `PASSED`, `FAILED`, or `ERROR`.

This project is designed to help practice:

- Go syntax and idioms
- REST API design
- Backend service structure
- gRPC communication
- Message queue processing
- Worker jobs
- Docker Compose
- PostgreSQL
- Code execution and testing
- Basic distributed system thinking

---

## 2. Core Idea

The application works like a small local judge system.

A user submits Go code for a problem. The system stores the submission, sends a judge job to a message queue, and a worker processes the job. The worker calls a Code Runner service through gRPC. The Code Runner runs Go tests and returns the result. The worker then updates the submission result in the database.

High-level flow:

```text
Frontend
   |
   | REST API
   v
Backend API
   |
   | Publish judge job
   v
Message Queue
   |
   | Consume job
   v
Judge Worker
   |
   | gRPC
   v
Code Runner Service
   |
   | go test
   v
Test Result
```

---

## 3. Why This Project?

This project is useful because it combines Go language practice with real backend architecture.

Instead of only learning syntax through isolated examples, each Go concept can become a real coding problem.

Example problems:

- Implement `Sum(nums []int) int`
- Reverse a string
- Count word frequency using a map
- Implement a stack using a struct
- Use an interface for shape area calculation
- Use goroutines to process data concurrently
- Use channels to coordinate workers

At the same time, the system itself gives practice with backend engineering:

- API layer
- Database schema
- Queue producer
- Queue consumer
- gRPC client/server
- Worker retry
- Timeout handling
- Dockerized local development

---

## 4. System Components

### 4.1 Frontend

The frontend should be simple.

Recommended stack:

- React
- Vite
- TypeScript
- Monaco Editor, optional

Main pages:

```text
/problems
/problems/:id
/submissions/:id
```

Frontend responsibilities:

- Show list of problems
- Show problem detail
- Display starter code
- Allow user to write code
- Submit code to backend
- Poll submission result
- Display verdict and test output

Example UI:

```text
Problem: Sum Slice

Description:
Implement the Sum function.

Editor:
--------------------------------
func Sum(nums []int) int {
    // your code here
}
--------------------------------

[Submit]

Verdict:
PENDING / RUNNING / PASSED / FAILED / ERROR
```

---

### 4.2 Backend API

The Backend API is the main entry point for the frontend.

Recommended stack:

- Go
- Chi / Gin / Echo
- PostgreSQL
- sqlc or GORM
- RabbitMQ client

Responsibilities:

- Provide problem APIs
- Receive code submissions
- Store submissions in PostgreSQL
- Publish judge jobs to the message queue
- Provide submission status APIs

Suggested REST APIs:

```http
GET /problems
GET /problems/:id
POST /submissions
GET /submissions/:id
```

Example submit request:

```json
{
  "problemId": "sum-slice",
  "code": "func Sum(nums []int) int { ... }"
}
```

Example submit response:

```json
{
  "submissionId": "sub_123",
  "status": "PENDING"
}
```

---

### 4.3 Message Queue

The message queue decouples the API from the judge process.

Recommended queue for MVP:

- RabbitMQ

Queue name:

```text
judge.submission.created
```

Message example:

```json
{
  "submissionId": "sub_123",
  "problemId": "sum-slice"
}
```

Concepts to learn:

- Producer
- Consumer
- Ack / Nack
- Retry
- Dead-letter queue
- Idempotent job handling
- At-least-once delivery

---

### 4.4 Judge Worker

The Judge Worker consumes messages from the queue and processes submissions.

Responsibilities:

- Consume judge jobs
- Load submission from database
- Mark submission as `RUNNING`
- Call Code Runner through gRPC
- Receive test result
- Update submission with final verdict
- Handle retry and failure cases

Worker flow:

```text
1. Receive message from queue
2. Parse submissionId
3. Load submission from database
4. Update status to RUNNING
5. Call Code Runner service
6. Receive result
7. Update status to PASSED / FAILED / ERROR
8. Ack message
```

Important concepts:

- Graceful shutdown
- Context timeout
- Retry on transient error
- Dead-letter queue
- Idempotency
- Concurrency limit

---

### 4.5 Code Runner Service

The Code Runner Service is responsible for running submitted Go code against server-side tests.

Recommended communication:

- gRPC

Responsibilities:

- Receive code and problem ID
- Load problem test cases
- Generate temporary Go files
- Run `go test`
- Capture output
- Return result to worker

Example gRPC contract:

```proto
service CodeRunnerService {
  rpc RunTests(RunTestsRequest) returns (RunTestsResponse);
}

message RunTestsRequest {
  string problem_id = 1;
  string code = 2;
}

message RunTestsResponse {
  bool passed = 1;
  string output = 2;
  int32 total_tests = 3;
  int32 passed_tests = 4;
  string error = 5;
}
```

The runner can generate files like:

```text
/tmp/go-judge/submission_xxx/main.go
/tmp/go-judge/submission_xxx/main_test.go
```

Then execute:

```bash
go test ./...
```

---

## 5. Domain Model

### Problem

A problem represents a coding challenge.

Fields:

```text
id
title
description
difficulty
starter_code
test_code
created_at
updated_at
```

Example:

```text
id: sum-slice
title: Sum Slice
difficulty: EASY
```

---

### Submission

A submission represents user code submitted for a problem.

Fields:

```text
id
problem_id
code
status
output
total_tests
passed_tests
created_at
updated_at
```

Possible statuses:

```text
PENDING
RUNNING
PASSED
FAILED
ERROR
```

---

### Judge Job

A judge job is a queue message used to process a submission asynchronously.

Fields:

```text
submission_id
problem_id
created_at
retry_count
```

---

## 6. Database Schema Draft

```sql
CREATE TABLE problems (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    difficulty TEXT NOT NULL,
    starter_code TEXT NOT NULL,
    test_code TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE submissions (
    id TEXT PRIMARY KEY,
    problem_id TEXT NOT NULL REFERENCES problems(id),
    code TEXT NOT NULL,
    status TEXT NOT NULL,
    output TEXT,
    total_tests INT DEFAULT 0,
    passed_tests INT DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);
```

---

## 7. Example Problem

### Problem: Sum Slice

Description:

```text
Implement a function that returns the sum of all numbers in a slice.
```

Starter code:

```go
func Sum(nums []int) int {
    return 0
}
```

Server-side test:

```go
func TestSum(t *testing.T) {
    got := Sum([]int{1, 2, 3})
    want := 6

    if got != want {
        t.Errorf("got %d, want %d", got, want)
    }
}
```

Expected accepted solution:

```go
func Sum(nums []int) int {
    total := 0

    for _, n := range nums {
        total += n
    }

    return total
}
```

---

## 8. MVP Scope

The first MVP should be intentionally small.

### MVP Features

- Show problem list
- Show problem detail
- Submit Go code
- Store submission
- Run tests
- Show verdict

### MVP Architecture

For the first version, the API can directly run the code without microservices.

```text
Frontend
   |
   v
Backend API
   |
   v
PostgreSQL
   |
   v
Local go test
```

This makes the first milestone easier.

After the MVP works, split the system into services.

---

## 9. Suggested Development Phases

### Phase 1: Monolith Judge

Goal:

Build the simplest working judge.

Components:

```text
Frontend
Backend API
PostgreSQL
```

Features:

- Create problems manually in database
- Submit code
- Run `go test` directly inside Backend API
- Return result

Learning focus:

- Go HTTP server
- Go project structure
- PostgreSQL
- File handling
- `os/exec`
- Basic testing

---

### Phase 2: Extract Code Runner Service

Goal:

Move code execution to a separate service.

New architecture:

```text
Backend API
   |
   | gRPC
   v
Code Runner Service
```

Learning focus:

- gRPC
- Protocol Buffers
- Service boundary
- Timeout
- Error handling
- Service-to-service communication

---

### Phase 3: Add Message Queue and Worker

Goal:

Make judging asynchronous.

New architecture:

```text
Backend API
   |
   | Publish message
   v
RabbitMQ
   |
   | Consume message
   v
Judge Worker
   |
   | gRPC
   v
Code Runner Service
```

Learning focus:

- Producer / consumer
- Worker job
- Async processing
- Ack / Nack
- Retry
- Dead-letter queue

---

### Phase 4: Improve Reliability

Goal:

Make the system more robust.

Features:

- Retry failed jobs
- Dead-letter queue
- Idempotent submission processing
- Worker graceful shutdown
- Context timeout
- Submission timeout
- Better error states

Learning focus:

- Reliability
- Failure handling
- Distributed system basics
- Message processing guarantees

---

### Phase 5: Add Docker Sandbox

Goal:

Run submitted code more safely.

Features:

- Run code inside a temporary Docker container
- Limit execution time
- Limit memory
- Disable network access
- Clean up temporary files

Learning focus:

- Security basics
- Process isolation
- Docker runtime
- Resource limits

---

### Phase 6: Add Realtime Result

Goal:

Improve frontend experience.

Options:

- Polling first
- Server-Sent Events later
- WebSocket later

Recommended progression:

```text
Polling -> SSE -> WebSocket
```

Learning focus:

- Realtime communication
- Event-driven updates
- Frontend state management

---

## 10. Recommended Tech Stack

### Frontend

```text
React
Vite
TypeScript
Monaco Editor
```

### Backend API

```text
Go
Chi or Gin
PostgreSQL
sqlc or GORM
RabbitMQ client
```

### Worker

```text
Go
RabbitMQ consumer
gRPC client
PostgreSQL
```

### Code Runner

```text
Go
gRPC server
os/exec
temporary file handling
Docker sandbox later
```

### Infrastructure

```text
Docker Compose
PostgreSQL
RabbitMQ
```

---

## 11. Local Docker Compose Components

Initial full local setup:

```text
frontend
backend-api
judge-worker
code-runner
postgres
rabbitmq
```

Optional later:

```text
prometheus
grafana
jaeger
redis
```

---

## 12. Learning Outcomes

After building this project, you should understand:

### Go Language

- Functions
- Slices and maps
- Structs
- Interfaces
- Errors
- Context
- Goroutines
- Channels
- File handling
- Command execution
- Testing

### Backend Engineering

- REST APIs
- DTOs
- Validation
- Repository pattern
- Database migration
- Service boundaries
- Error handling

### Microservices

- gRPC communication
- Message queue
- Worker jobs
- Async processing
- Retry and DLQ
- Idempotency
- Graceful shutdown

### Infrastructure

- Docker
- Docker Compose
- PostgreSQL
- RabbitMQ
- Local service orchestration

---

## 13. Future Ideas

Possible improvements after MVP:

- User login
- Submission history
- Hidden test cases
- Problem categories
- Difficulty levels
- Learning roadmap
- Go syntax lessons
- Admin page for creating problems
- Leaderboard
- Contest mode
- Multiple language support
- Metrics with Prometheus
- Tracing with OpenTelemetry
- Logs dashboard
- Rate limiting
- Plagiarism detection
- AI explanation for failed tests

---

## 14. Suggested Repository Name

```text
go-judge
```

Suggested description:

```text
A local coding judge system for practicing Go syntax and learning backend architecture.
```

---

## 15. One-Line Vision

```text
go-judge is a local LeetCode-style judge system built with Go to practice Go syntax, REST APIs, gRPC, message queues, and worker-based asynchronous processing.
```

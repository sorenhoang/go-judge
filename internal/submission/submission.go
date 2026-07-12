package submission

import (
	"errors"
	"time"
)

var ErrNotFound = errors.New("submission not found")

type Status string

const (
	StatusPending Status = "PENDING"
	StatusRunning Status = "RUNNING"
	StatusPassed  Status = "PASSED"
	StatusFailed  Status = "FAILED"
	StatusError   Status = "ERROR"
)

type Submission struct {
	ID          string    `json:"id"`
	ProblemID   string    `json:"problem_id"`
	Code        string    `json:"code"`
	Status      Status    `json:"status"`
	Output      *string   `json:"output"`
	TotalTests  int       `json:"total_tests"`
	PassedTests int       `json:"passed_tests"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

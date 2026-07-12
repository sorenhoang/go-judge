package submission

import "context"

type Repository interface {
	Create(ctx context.Context, s Submission) error
	GetByID(ctx context.Context, id string) (Submission, error)
	UpdateResult(ctx context.Context, id string, status Status, output string, totalTests int, passedTests int) error
}

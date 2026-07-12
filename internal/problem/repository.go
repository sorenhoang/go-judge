package problem

import (
	"context"
)

type Repository interface {
	List(ctx context.Context) ([]Problem, error)
	Get(ctx context.Context, id string) (Problem, error)
}

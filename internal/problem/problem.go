package problem

import (
	"errors"
	"time"
)

var ErrNotFound = errors.New("problem not found")

type Problem struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Difficulty  string    `json:"difficulty"`
	StarterCode string    `json:"starter_code"`
	TestCode    string    `json:"-"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

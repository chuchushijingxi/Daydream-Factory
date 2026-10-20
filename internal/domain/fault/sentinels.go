package fault

import (
	"errors"
)

var (
	ErrUnsupportedFormat = errors.New("unsupported script format")
	ErrInvalidDocument   = errors.New("invalid script document")
	ErrConflict          = errors.New("CONFLICT")
	ErrNotFound          = errors.New("NOT_FOUND")
	ErrInvalid           = errors.New("INVALID_INPUT")
	ErrCancelled         = errors.New("CANCELLED")
	ErrBudget            = errors.New("BUDGET_EXHAUSTED")
	ErrCapacity          = errors.New("CAPACITY_EXCEEDED")
	ErrModelUnavailable  = errors.New("MODEL_UNAVAILABLE")
)

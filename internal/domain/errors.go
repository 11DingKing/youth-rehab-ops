package domain

import (
	"errors"
	"fmt"
)

var (
	ErrValidation      = errors.New("validation failed")
	ErrUnauthenticated = errors.New("authentication required")
	ErrForbidden       = errors.New("operation forbidden")
	ErrNotFound        = errors.New("record not found")
	ErrConflict        = errors.New("state conflict")
	ErrExpired         = errors.New("record expired")
	ErrUnavailable     = errors.New("dependency unavailable")
)

type FieldError struct {
	Field   string
	Problem string
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Problem)
}

func (e *FieldError) Unwrap() error { return ErrValidation }

type ConflictError struct {
	Entity   string
	Expected int64
	Actual   int64
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s version conflict: expected %d, actual %d", e.Entity, e.Expected, e.Actual)
}

func (e *ConflictError) Unwrap() error { return ErrConflict }

func WrapUnavailable(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w: %v", operation, ErrUnavailable, err)
}

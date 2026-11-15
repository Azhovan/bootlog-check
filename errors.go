package bootlog

import (
	"fmt"
)

// ErrorCategory is an immutable errors.Is target. Concrete errors support
// errors.As and unwrap their cause to preserve I/O and context error identity.
type ErrorCategory string

func (e ErrorCategory) Error() string { return string(e) }

const (
	ErrInvalidInput  ErrorCategory = "invalid input"
	ErrInvalidRules  ErrorCategory = "invalid rules"
	ErrLimit         ErrorCategory = "limit exceeded"
	ErrCanceled      ErrorCategory = "operation canceled"
	ErrInvalidReport ErrorCategory = "invalid report"
)

type InvalidInputError struct {
	Line   int64
	Field  string
	Detail string
	Err    error
}

func (e *InvalidInputError) Error() string {
	return describeError("invalid input", e.Line, e.Field, e.Detail, e.Err)
}
func (e *InvalidInputError) Unwrap() error        { return e.Err }
func (e *InvalidInputError) Is(target error) bool { return target == ErrInvalidInput }

type InvalidRulesError struct {
	Field  string
	Detail string
	Err    error
}

func (e *InvalidRulesError) Error() string {
	return describeError("invalid rules", 0, e.Field, e.Detail, e.Err)
}
func (e *InvalidRulesError) Unwrap() error        { return e.Err }
func (e *InvalidRulesError) Is(target error) bool { return target == ErrInvalidRules }

type LimitError struct {
	Resource string
	Limit    int64
	Err      error
}

func (e *LimitError) Error() string {
	return describeError("limit exceeded", 0, e.Resource, fmt.Sprintf("maximum %d", e.Limit), e.Err)
}
func (e *LimitError) Unwrap() error        { return e.Err }
func (e *LimitError) Is(target error) bool { return target == ErrLimit }

type CancellationError struct{ Err error }

func (e *CancellationError) Error() string {
	return describeError("operation canceled", 0, "", "", e.Err)
}
func (e *CancellationError) Unwrap() error        { return e.Err }
func (e *CancellationError) Is(target error) bool { return target == ErrCanceled }

// InvalidReportError distinguishes a malformed caller-built result from a valid
// report containing failed checks. It is intended for public report writers.
type InvalidReportError struct {
	Field  string
	Detail string
	Err    error
}

func (e *InvalidReportError) Error() string {
	return describeError("invalid report", 0, e.Field, e.Detail, e.Err)
}
func (e *InvalidReportError) Unwrap() error        { return e.Err }
func (e *InvalidReportError) Is(target error) bool { return target == ErrInvalidReport }

func describeError(category string, line int64, field, detail string, cause error) string {
	s := category
	if line != 0 {
		s += fmt.Sprintf(" at line %d", line)
	}
	if field != "" {
		s += " (" + field + ")"
	}
	if detail != "" {
		s += ": " + detail
	}
	if cause != nil {
		s += ": " + cause.Error()
	}
	return s
}

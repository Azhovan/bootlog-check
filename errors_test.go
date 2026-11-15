package bootlog_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp/syntax"
	"strings"
	"testing"

	bootlog "github.com/firmdura/bootlog-check"
)

func TestErrorCategoriesAndCauses(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		category  error
		cause     error
		as        func(error) bool
		fragments []string
	}{
		{"input", &bootlog.InvalidInputError{Line: 3, Field: "timestamp_ns", Detail: "cannot read", Err: io.ErrUnexpectedEOF}, bootlog.ErrInvalidInput, io.ErrUnexpectedEOF,
			func(err error) bool {
				var typed *bootlog.InvalidInputError
				return errors.As(err, &typed) && typed.Line == 3 && typed.Field == "timestamp_ns"
			},
			[]string{"invalid input", "3", "timestamp_ns", "cannot read", "unexpected EOF"}},
		{"rules", &bootlog.InvalidRulesError{Field: "boot_start.pattern", Detail: "cannot read", Err: io.ErrClosedPipe}, bootlog.ErrInvalidRules, io.ErrClosedPipe,
			func(err error) bool {
				var typed *bootlog.InvalidRulesError
				return errors.As(err, &typed) && typed.Field == "boot_start.pattern"
			},
			[]string{"invalid rules", "boot_start.pattern", "cannot read", "closed pipe"}},
		{"limit", &bootlog.LimitError{Resource: "input bytes", Limit: 19, Err: io.ErrUnexpectedEOF}, bootlog.ErrLimit, io.ErrUnexpectedEOF,
			func(err error) bool {
				var typed *bootlog.LimitError
				return errors.As(err, &typed) && typed.Resource == "input bytes" && typed.Limit == 19
			},
			[]string{"limit exceeded", "input bytes", "19", "unexpected EOF"}},
		{"cancellation", &bootlog.CancellationError{Err: context.Canceled}, bootlog.ErrCanceled, context.Canceled,
			func(err error) bool { var typed *bootlog.CancellationError; return errors.As(err, &typed) },
			[]string{"operation canceled", "context canceled"}},
		{"deadline cancellation", &bootlog.CancellationError{Err: context.DeadlineExceeded}, bootlog.ErrCanceled, context.DeadlineExceeded,
			func(err error) bool { var typed *bootlog.CancellationError; return errors.As(err, &typed) },
			[]string{"operation canceled", "deadline exceeded"}},
		{"report", &bootlog.InvalidReportError{Field: "summary", Detail: "wrong count", Err: io.ErrShortWrite}, bootlog.ErrInvalidReport, io.ErrShortWrite,
			func(err error) bool {
				var typed *bootlog.InvalidReportError
				return errors.As(err, &typed) && typed.Field == "summary"
			},
			[]string{"invalid report", "summary", "wrong count", "short write"}},
	}
	categories := []error{bootlog.ErrInvalidInput, bootlog.ErrInvalidRules, bootlog.ErrLimit, bootlog.ErrCanceled, bootlog.ErrInvalidReport}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, err := range []error{test.err, fmt.Errorf("outer operation: %w", test.err)} {
				if !errors.Is(err, test.category) || !errors.Is(err, test.cause) || !test.as(err) {
					t.Fatalf("error identity lost through wrapping: %v", err)
				}
				for _, category := range categories {
					if category != test.category && errors.Is(err, category) {
						t.Errorf("%v incorrectly matches category %v", err, category)
					}
				}
				for _, fragment := range test.fragments {
					if !strings.Contains(err.Error(), fragment) {
						t.Errorf("error %q lacks diagnostic %q", err.Error(), fragment)
					}
				}
			}
			if errors.Unwrap(test.err) != test.cause {
				t.Errorf("Unwrap() = %v, want identical cause %v", errors.Unwrap(test.err), test.cause)
			}
		})
	}
}

func TestErrorsWithoutUnderlyingCause(t *testing.T) {
	tests := []struct{ err, category error }{
		{&bootlog.InvalidInputError{}, bootlog.ErrInvalidInput},
		{&bootlog.InvalidRulesError{}, bootlog.ErrInvalidRules},
		{&bootlog.LimitError{}, bootlog.ErrLimit},
		{&bootlog.CancellationError{}, bootlog.ErrCanceled},
		{&bootlog.InvalidReportError{}, bootlog.ErrInvalidReport},
	}
	for _, test := range tests {
		if !errors.Is(test.err, test.category) || errors.Unwrap(test.err) != nil || test.err.Error() == "" {
			t.Errorf("invalid nil-cause behavior: %#v", test.err)
		}
	}
}

func TestConfigValidationPreservesRegexpError(t *testing.T) {
	config := validationConfig()
	config.BootStart.Pattern = "("
	err := config.Validate()
	var ruleError *bootlog.InvalidRulesError
	var syntaxError *syntax.Error
	if !errors.Is(err, bootlog.ErrInvalidRules) || !errors.As(err, &ruleError) || !errors.As(err, &syntaxError) {
		t.Fatalf("regexp cause lost from config validation error: %v", err)
	}
	if ruleError.Err != syntaxError {
		t.Fatalf("underlying regexp error identity changed: %#v", ruleError.Err)
	}
}

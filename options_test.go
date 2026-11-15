package bootlog_test

import (
	"errors"
	"math"
	"testing"

	bootlog "github.com/firmdura/bootlog-check"
)

func TestParseOptionsNormalize(t *testing.T) {
	defaults := bootlog.ParseOptions{MaxInputBytes: 16 << 20, MaxLines: 100_000, MaxLineBytes: 64 << 10}
	tests := []struct {
		name string
		in   bootlog.ParseOptions
		want bootlog.ParseOptions
	}{
		{"defaults", bootlog.ParseOptions{}, defaults},
		{"input override", bootlog.ParseOptions{MaxInputBytes: 1}, bootlog.ParseOptions{MaxInputBytes: 1, MaxLines: 100_000, MaxLineBytes: 64 << 10}},
		{"line override", bootlog.ParseOptions{MaxLines: 1}, bootlog.ParseOptions{MaxInputBytes: 16 << 20, MaxLines: 1, MaxLineBytes: 64 << 10}},
		{"line bytes override", bootlog.ParseOptions{MaxLineBytes: 1}, bootlog.ParseOptions{MaxInputBytes: 16 << 20, MaxLines: 100_000, MaxLineBytes: 1}},
		{"all overrides", bootlog.ParseOptions{MaxInputBytes: 7, MaxLines: 9, MaxLineBytes: 11}, bootlog.ParseOptions{MaxInputBytes: 7, MaxLines: 9, MaxLineBytes: 11}},
		{"maximum overrides", bootlog.ParseOptions{MaxInputBytes: math.MaxInt64, MaxLines: math.MaxInt64, MaxLineBytes: math.MaxInt64}, bootlog.ParseOptions{MaxInputBytes: math.MaxInt64, MaxLines: math.MaxInt64, MaxLineBytes: math.MaxInt64}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := test.in
			got, err := input.Normalize()
			if err != nil || got != test.want {
				t.Fatalf("Normalize() = %#v, %v; want %#v, nil", got, err, test.want)
			}
			if input != test.in {
				t.Fatalf("Normalize mutated caller options: %#v", input)
			}
			second, err := got.Normalize()
			if err != nil || second != got {
				t.Fatalf("normalization changed already normalized options: %#v, %v", second, err)
			}
		})
	}
}

func TestParseOptionsNormalizeRejectsNegatives(t *testing.T) {
	tests := []struct {
		field string
		in    bootlog.ParseOptions
	}{
		{"max_input_bytes", bootlog.ParseOptions{MaxInputBytes: -1}},
		{"max_lines", bootlog.ParseOptions{MaxLines: -1}},
		{"max_line_bytes", bootlog.ParseOptions{MaxLineBytes: math.MinInt64}},
	}
	for _, test := range tests {
		t.Run(test.field, func(t *testing.T) {
			_, err := test.in.Normalize()
			if !errors.Is(err, bootlog.ErrInvalidInput) {
				t.Fatalf("Normalize() error = %v, want ErrInvalidInput", err)
			}
			var typed *bootlog.InvalidInputError
			if !errors.As(err, &typed) || typed.Field != test.field {
				t.Fatalf("Normalize() error = %#v, want InvalidInputError field %q", err, test.field)
			}
		})
	}
}

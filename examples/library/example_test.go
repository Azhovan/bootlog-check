package main_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/firmdura/bootlog-check"
	"github.com/firmdura/bootlog-check/report"
)

func Example_errors() {
	_, err := bootlog.ParseLog(context.Background(), strings.NewReader("ready\n"),
		bootlog.ParseOptions{MaxInputBytes: 5})
	fmt.Println("limit:", errors.Is(err, bootlog.ErrLimit))
	var limit *bootlog.LimitError
	if errors.As(err, &limit) {
		fmt.Println(limit.Resource, limit.Limit)
	}

	_, err = bootlog.ParseLog(context.Background(), strings.NewReader("[1.x] READY\n"), bootlog.ParseOptions{})
	var invalid *bootlog.InvalidInputError
	if errors.As(err, &invalid) {
		fmt.Println("invalid input:", errors.Is(err, bootlog.ErrInvalidInput), "line:", invalid.Line)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = bootlog.ParseLog(ctx, strings.NewReader(""), bootlog.ParseOptions{})
	fmt.Println("canceled:", errors.Is(err, bootlog.ErrCanceled), errors.Is(err, context.Canceled))
	// Output:
	// limit: true
	// max_input_bytes 5
	// invalid input: true line: 1
	// canceled: true true
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func Example_writerError() {
	config := bootlog.Config{
		SchemaVersion: 1,
		BootStart:     bootlog.BootStartConfig{Pattern: "^BOOT$"},
		Forbidden:     []bootlog.ForbiddenConfig{{ID: "panic", Pattern: "PANIC"}},
	}
	rules, err := bootlog.CompileRules(config)
	if err != nil {
		panic(err)
	}
	// An empty capture is valid input with a missing-start failed check.
	result, err := bootlog.Evaluate(bootlog.Log{}, rules)
	if err != nil {
		panic(err)
	}
	fmt.Println("failed checks:", result.Summary.Failed)
	err = report.Write(failedWriter{}, result, report.JSON)
	fmt.Println("writer cause:", errors.Is(err, io.ErrClosedPipe))
	// Output:
	// failed checks: 1
	// writer cause: true
}

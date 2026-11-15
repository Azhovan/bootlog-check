// Command library demonstrates the public API with a small synthetic capture.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/firmdura/bootlog-check"
	"github.com/firmdura/bootlog-check/report"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	// These in-memory readers need no closure. Callers using os.Open must close
	// their files, and callers using buffered writers must check Flush too.
	config, err := bootlog.ParseRules(strings.NewReader(`{
		"schema_version": 1,
		"boot_start": {"pattern": "^BOOT$"},
		"milestones": [{"id": "ready", "pattern": "^READY$", "within_ms": 1000}],
		"forbidden": [{"id": "panic", "pattern": "PANIC"}]
	}`))
	if err != nil {
		return err
	}
	rules, err := bootlog.CompileRules(config)
	if err != nil {
		return err
	}
	// Reuse rules for other captures, including concurrent evaluations.
	log, err := bootlog.ParseLog(context.Background(),
		strings.NewReader("bootloader preamble\n[1] BOOT\n[2] READY\n"), bootlog.ParseOptions{})
	if err != nil {
		return err
	}
	result, err := bootlog.Evaluate(log, rules)
	if err != nil {
		return err
	}
	// Failed checks are report data. Render the complete report before applying
	// this example program's own failure policy.
	if err := report.Write(os.Stdout, result, report.Text); err != nil {
		return err
	}
	if result.Summary.Failed != 0 {
		return fmt.Errorf("capture has %d failed checks", result.Summary.Failed)
	}
	return nil
}

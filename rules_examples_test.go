package bootlog_test

import (
	"fmt"
	"strings"

	bootlog "github.com/firmdura/bootlog-check"
)

func ExampleParseRules() {
	cfg, err := bootlog.ParseRules(strings.NewReader(`{
		"schema_version": 1,
		"boot_start": {"pattern": "^START$"},
		"milestones": [{"id": "ready", "pattern": "^READY$", "within_ms": 100}],
		"forbidden": [{"id": "panic", "pattern": "(?i)panic"}]
	}`))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(cfg.SchemaVersion, cfg.Milestones[0].ID, *cfg.Milestones[0].WithinMS)
	// Output: 1 ready 100
}

func ExampleCompileRules() {
	cfg := bootlog.Config{
		SchemaVersion: 1,
		BootStart:     bootlog.BootStartConfig{Pattern: "^START$"},
		Forbidden:     []bootlog.ForbiddenConfig{{ID: "panic", Pattern: "(?i)panic"}},
	}
	rules, err := bootlog.CompileRules(cfg)
	if err != nil {
		fmt.Println(err)
		return
	}
	// Rules own their configuration and can be shared across evaluations.
	cfg.Forbidden[0].Pattern = "START"
	start := int64(0)
	report, err := bootlog.Evaluate(bootlog.Log{Records: []bootlog.Record{
		{Line: 1, TimestampNS: &start, Message: "START"},
	}}, rules)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(report.Summary.Failed, report.Checks[1].Reason)
	// Output: 0 absent
}

func ExampleEvaluate() {
	deadline := int64(100)
	rules, err := bootlog.CompileRules(bootlog.Config{
		SchemaVersion: 1,
		BootStart:     bootlog.BootStartConfig{Pattern: "^START$"},
		Milestones:    []bootlog.MilestoneConfig{{ID: "ready", Pattern: "^READY$", WithinMS: &deadline}},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	start, ready := int64(2_000_000_000), int64(2_100_000_000)
	report, err := bootlog.Evaluate(bootlog.Log{Records: []bootlog.Record{
		{Line: 1, Message: "preamble"},
		{Line: 2, TimestampNS: &start, Message: "START"},
		{Line: 3, TimestampNS: &ready, Message: "READY"},
	}}, rules)
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, check := range report.Checks {
		fmt.Println(check.ID, check.Status, check.Reason, *check.ElapsedNS)
	}
	fmt.Println(report.Scope)
	// Output:
	// boot_start passed matched 0
	// ready passed matched 100000000
	// Results describe only the supplied capture; a pass does not prove capture completeness or device health.
}

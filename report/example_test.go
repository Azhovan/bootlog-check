package report_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/firmdura/bootlog-check"
	"github.com/firmdura/bootlog-check/report"
)

func ExampleWrite() {
	log, err := bootlog.ParseLog(context.Background(), strings.NewReader("[1] BOOT\n[2] READY\n"), bootlog.ParseOptions{})
	if err != nil {
		panic(err)
	}
	rules, err := bootlog.CompileRules(bootlog.Config{
		SchemaVersion: 1,
		BootStart:     bootlog.BootStartConfig{Pattern: "^BOOT$"},
		Milestones:    []bootlog.MilestoneConfig{{ID: "ready", Pattern: "^READY$"}},
	})
	if err != nil {
		panic(err)
	}
	result, err := bootlog.Evaluate(log, rules)
	if err != nil {
		panic(err)
	}
	var output bytes.Buffer
	if err := report.Write(&output, result, report.Text); err != nil {
		panic(err)
	}
	fmt.Print(output.String())
	// Output:
	// bootlog-check: PASS
	// summary: total=2 passed=2 failed=0 not_evaluated=0
	// schema_version: 1
	// timing_basis: boot_start (elapsed_ns = witness timestamp_ns - origin timestamp_ns)
	// origin: line=1 timestamp_ns=1000000000 message="BOOT"
	// [passed] boot_start (start): reason=matched within_ms=none elapsed_ns=0 match_count=1
	//   evidence: line=1 timestamp_ns=1000000000 message="BOOT"
	// [passed] ready (milestone): reason=matched within_ms=none elapsed_ns=1000000000 match_count=1
	//   evidence: line=2 timestamp_ns=2000000000 message="READY"
	// scope: Results describe only the supplied capture; a pass does not prove capture completeness or device health.
}

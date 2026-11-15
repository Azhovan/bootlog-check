// Package testfixture supplies small, independently authored inputs and expected
// models. It never calls production parsers, compilers, or evaluators. Every
// helper returns fresh mutable storage so one test cannot change another's data.
package testfixture

import "github.com/firmdura/bootlog-check"

// Int64 returns a fresh pointer, including for a meaningful zero timestamp.
func Int64(value int64) *int64 { return &value }

func Timed(line, timestampNS int64, message string) bootlog.Record {
	return bootlog.Record{Line: line, TimestampNS: Int64(timestampNS), Message: message}
}

func Untimed(line int64, message string) bootlog.Record {
	return bootlog.Record{Line: line, Message: message}
}

// LogText contains a preamble, a skipped empty physical line, an inclusive
// deadline witness, an untimed presence-only witness, and a duplicate milestone.
func LogText() string {
	return "[ OK ] serial\n\n[1.000000000] Linux version test\n[4] NETWORK_READY\nAPP_READY\n[4.000000000] NETWORK_READY\n"
}

// Log is the independently written logical counterpart of LogText.
func Log() bootlog.Log {
	return bootlog.Log{Records: []bootlog.Record{
		Untimed(1, "[ OK ] serial"),
		Timed(3, 1_000_000_000, "Linux version test"),
		Timed(4, 4_000_000_000, "NETWORK_READY"),
		Untimed(5, "APP_READY"),
		Timed(6, 4_000_000_000, "NETWORK_READY"),
	}}
}

// Config and RulesJSON describe the same rules without deriving either from the
// other. The second milestone intentionally has no deadline.
func Config() bootlog.Config {
	return bootlog.Config{
		SchemaVersion: 1,
		BootStart:     bootlog.BootStartConfig{Pattern: "^Linux version "},
		Milestones: []bootlog.MilestoneConfig{
			{ID: "network_ready", Pattern: "^NETWORK_READY$", WithinMS: Int64(3000)},
			{ID: "application_ready", Pattern: "^APP_READY$"},
		},
		Forbidden: []bootlog.ForbiddenConfig{{ID: "kernel_panic", Pattern: "(?i)kernel panic"}},
	}
}

func RulesJSON() string {
	return `{"schema_version":1,"boot_start":{"pattern":"^Linux version "},"milestones":[{"id":"network_ready","pattern":"^NETWORK_READY$","within_ms":3000},{"id":"application_ready","pattern":"^APP_READY$"}],"forbidden":[{"id":"kernel_panic","pattern":"(?i)kernel panic"}]}`
}

// PassingReport is the expected result for Log and Config. The duplicate network
// witness increases the count while preserving the first evidence.
func PassingReport() bootlog.Report {
	return bootlog.Report{
		SchemaVersion: 1,
		TimingBasis:   "boot_start",
		Origin:        &bootlog.Evidence{Line: 3, TimestampNS: Int64(1_000_000_000), Message: "Linux version test"},
		Checks: []bootlog.CheckResult{
			{ID: "boot_start", Kind: bootlog.KindStart, Status: bootlog.StatusPassed, Reason: bootlog.ReasonMatched, ElapsedNS: Int64(0), MatchCount: 1, Evidence: &bootlog.Evidence{Line: 3, TimestampNS: Int64(1_000_000_000), Message: "Linux version test"}},
			{ID: "network_ready", Kind: bootlog.KindMilestone, Status: bootlog.StatusPassed, Reason: bootlog.ReasonMatched, WithinMS: Int64(3000), ElapsedNS: Int64(3_000_000_000), MatchCount: 2, Evidence: &bootlog.Evidence{Line: 4, TimestampNS: Int64(4_000_000_000), Message: "NETWORK_READY"}},
			{ID: "application_ready", Kind: bootlog.KindMilestone, Status: bootlog.StatusPassed, Reason: bootlog.ReasonMatched, MatchCount: 1, Evidence: &bootlog.Evidence{Line: 5, Message: "APP_READY"}},
			{ID: "kernel_panic", Kind: bootlog.KindForbidden, Status: bootlog.StatusPassed, Reason: bootlog.ReasonAbsent},
		},
		Summary: bootlog.Summary{Total: 4, Passed: 4},
		Scope:   "Results describe only the supplied capture; a pass does not prove capture completeness or device health.",
	}
}

// FailingReport exercises all post-origin failure reasons, preamble evidence,
// signed elapsed values, untimed evidence, and preserved Unicode/control data.
func FailingReport() bootlog.Report {
	return bootlog.Report{
		SchemaVersion: 1,
		TimingBasis:   "boot_start",
		Origin:        &bootlog.Evidence{Line: 3, TimestampNS: Int64(2_000_000_000), Message: "Linux version test"},
		Checks: []bootlog.CheckResult{
			{ID: "boot_start", Kind: bootlog.KindStart, Status: bootlog.StatusPassed, Reason: bootlog.ReasonMatched, ElapsedNS: Int64(0), MatchCount: 1, Evidence: &bootlog.Evidence{Line: 3, TimestampNS: Int64(2_000_000_000), Message: "Linux version test"}},
			{ID: "network_ready", Kind: bootlog.KindMilestone, Status: bootlog.StatusFailed, Reason: bootlog.ReasonMissing, WithinMS: Int64(3000)},
			{ID: "application_ready", Kind: bootlog.KindMilestone, Status: bootlog.StatusFailed, Reason: bootlog.ReasonLate, WithinMS: Int64(5000), ElapsedNS: Int64(5_000_000_001), MatchCount: 2, Evidence: &bootlog.Evidence{Line: 5, TimestampNS: Int64(7_000_000_001), Message: "APP_READY"}},
			{ID: "serial_ready", Kind: bootlog.KindMilestone, Status: bootlog.StatusFailed, Reason: bootlog.ReasonUntimed, WithinMS: Int64(1), MatchCount: 1, Evidence: &bootlog.Evidence{Line: 4, Message: "SERIAL_READY"}},
			{ID: "kernel_panic", Kind: bootlog.KindForbidden, Status: bootlog.StatusFailed, Reason: bootlog.ReasonForbiddenMatch, ElapsedNS: Int64(-1_000_000_000), MatchCount: 2, Evidence: &bootlog.Evidence{Line: 1, TimestampNS: Int64(1_000_000_000), Message: "kernel panic in preamble"}},
			{ID: "fault", Kind: bootlog.KindForbidden, Status: bootlog.StatusFailed, Reason: bootlog.ReasonForbiddenMatch, MatchCount: 1, Evidence: &bootlog.Evidence{Line: 6, Message: "fault:\x1b[31m失敗\x00<&>"}},
		},
		Summary: bootlog.Summary{Total: 6, Passed: 1, Failed: 5},
		Scope:   "Results describe only the supplied capture; a pass does not prove capture completeness or device health.",
	}
}

// MissingOriginReport keeps every configured milestone as not_evaluated and
// still evaluates forbidden rules across the supplied capture.
func MissingOriginReport() bootlog.Report {
	return bootlog.Report{
		SchemaVersion: 1,
		TimingBasis:   "boot_start",
		Checks: []bootlog.CheckResult{
			{ID: "boot_start", Kind: bootlog.KindStart, Status: bootlog.StatusFailed, Reason: bootlog.ReasonMissing},
			{ID: "network_ready", Kind: bootlog.KindMilestone, Status: bootlog.StatusNotEvaluated, Reason: bootlog.ReasonNoBootStart, WithinMS: Int64(3000)},
			{ID: "application_ready", Kind: bootlog.KindMilestone, Status: bootlog.StatusNotEvaluated, Reason: bootlog.ReasonNoBootStart},
			{ID: "kernel_panic", Kind: bootlog.KindForbidden, Status: bootlog.StatusFailed, Reason: bootlog.ReasonForbiddenMatch, MatchCount: 1, Evidence: &bootlog.Evidence{Line: 2, TimestampNS: Int64(0), Message: "kernel panic"}},
			{ID: "watchdog", Kind: bootlog.KindForbidden, Status: bootlog.StatusPassed, Reason: bootlog.ReasonAbsent},
		},
		Summary: bootlog.Summary{Total: 5, Passed: 1, Failed: 2, NotEvaluated: 2},
		Scope:   "Results describe only the supplied capture; a pass does not prove capture completeness or device health.",
	}
}

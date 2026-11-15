package bootlog_test

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"

	bootlog "github.com/firmdura/bootlog-check"
	"github.com/firmdura/bootlog-check/internal/testfixture"
)

func evaluatedRules(t *testing.T, log bootlog.Log, cfg bootlog.Config) bootlog.Report {
	t.Helper()
	set, err := bootlog.CompileRules(cfg)
	if err != nil {
		t.Fatal(err)
	}
	report, err := bootlog.Evaluate(log, set)
	if err != nil {
		t.Fatal(err)
	}
	if err := report.Validate(); err != nil {
		t.Fatalf("invalid generated report: %v", err)
	}
	return report
}

func TestEvaluateCompleteReports(t *testing.T) {
	failingConfig := testfixture.Config()
	failingConfig.Milestones[1].WithinMS = testfixture.Int64(5000)
	failingConfig.Milestones = append(failingConfig.Milestones, bootlog.MilestoneConfig{ID: "serial_ready", Pattern: "^SERIAL_READY$", WithinMS: testfixture.Int64(1)})
	failingConfig.Forbidden = append(failingConfig.Forbidden, bootlog.ForbiddenConfig{ID: "fault", Pattern: "^fault:"})
	missingConfig := testfixture.Config()
	missingConfig.Forbidden = append(missingConfig.Forbidden, bootlog.ForbiddenConfig{ID: "watchdog", Pattern: "WATCHDOG"})
	cases := []struct {
		name string
		cfg  bootlog.Config
		log  bootlog.Log
		want bootlog.Report
	}{
		{"passing", testfixture.Config(), testfixture.Log(), testfixture.PassingReport()},
		{"all failures", failingConfig, bootlog.Log{Records: []bootlog.Record{
			testfixture.Timed(1, 1_000_000_000, "kernel panic in preamble"),
			testfixture.Timed(2, 1_000_000_000, "NETWORK_READY"),
			testfixture.Timed(3, 2_000_000_000, "Linux version test"),
			testfixture.Untimed(4, "SERIAL_READY"),
			testfixture.Timed(5, 7_000_000_001, "APP_READY"),
			testfixture.Untimed(6, "fault:\x1b[31m失敗\x00<&>"),
			testfixture.Timed(7, 8_000_000_000, "APP_READY"),
			testfixture.Timed(8, 9_000_000_000, "kernel panic after readiness"),
		}}, testfixture.FailingReport()},
		{"missing origin", missingConfig, bootlog.Log{Records: []bootlog.Record{
			testfixture.Untimed(1, "APP_READY"), testfixture.Timed(2, 0, "kernel panic"), testfixture.Timed(3, 1, "NETWORK_READY"),
		}}, testfixture.MissingOriginReport()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluatedRules(t, tc.log, tc.cfg)
			if !reflect.DeepEqual(got, tc.want) {
				a, _ := json.MarshalIndent(got, "", "  ")
				b, _ := json.MarshalIndent(tc.want, "", "  ")
				t.Fatalf("got %s\nwant %s", a, b)
			}
		})
	}
}

func TestEvaluateEmptyLog(t *testing.T) {
	r := evaluatedRules(t, bootlog.Log{}, testfixture.Config())
	want := []bootlog.CheckResult{
		{ID: "boot_start", Kind: bootlog.KindStart, Status: bootlog.StatusFailed, Reason: bootlog.ReasonMissing},
		{ID: "network_ready", Kind: bootlog.KindMilestone, Status: bootlog.StatusNotEvaluated, Reason: bootlog.ReasonNoBootStart, WithinMS: testfixture.Int64(3000)},
		{ID: "application_ready", Kind: bootlog.KindMilestone, Status: bootlog.StatusNotEvaluated, Reason: bootlog.ReasonNoBootStart},
		{ID: "kernel_panic", Kind: bootlog.KindForbidden, Status: bootlog.StatusPassed, Reason: bootlog.ReasonAbsent},
	}
	if r.Origin != nil || !reflect.DeepEqual(r.Checks, want) || r.Summary != (bootlog.Summary{Total: 4, Passed: 1, Failed: 1, NotEvaluated: 2}) {
		t.Fatalf("empty: %#v", r)
	}
}

func TestEvaluateDeadlineBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		origin, elapsed, deadline int64
		status                    bootlog.Status
		reason                    bootlog.Reason
	}{
		{"minus one", 7_000_000_000, 999_999, 1, bootlog.StatusPassed, bootlog.ReasonMatched},
		{"equal", 7_000_000_000, 1_000_000, 1, bootlog.StatusPassed, bootlog.ReasonMatched},
		{"plus one", 7_000_000_000, 1_000_001, 1, bootlog.StatusFailed, bootlog.ReasonLate},
		{"zero equal", 42, 0, 0, bootlog.StatusPassed, bootlog.ReasonMatched},
		{"zero late", 42, 1, 0, bootlog.StatusFailed, bootlog.ReasonLate},
		{"near max origin", math.MaxInt64 - 10, 10, 1, bootlog.StatusPassed, bootlog.ReasonMatched},
		{"maximum deadline", 0, bootlog.MaxWithinMS * bootlog.NanosecondsPerMS, bootlog.MaxWithinMS, bootlog.StatusPassed, bootlog.ReasonMatched},
		{"maximum timestamp late", 0, math.MaxInt64, bootlog.MaxWithinMS, bootlog.StatusFailed, bootlog.ReasonLate},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := bootlog.Config{SchemaVersion: 1, BootStart: bootlog.BootStartConfig{Pattern: "^START$"}, Milestones: []bootlog.MilestoneConfig{{ID: "ready", Pattern: "^READY$", WithinMS: testfixture.Int64(tc.deadline)}}}
			log := bootlog.Log{Records: []bootlog.Record{testfixture.Timed(2, tc.origin, "START"), testfixture.Timed(5, tc.origin+tc.elapsed, "READY")}}
			r := evaluatedRules(t, log, cfg)
			want := bootlog.CheckResult{ID: "ready", Kind: bootlog.KindMilestone, Status: tc.status, Reason: tc.reason, WithinMS: testfixture.Int64(tc.deadline), ElapsedNS: testfixture.Int64(tc.elapsed), MatchCount: 1, Evidence: &bootlog.Evidence{Line: 5, TimestampNS: testfixture.Int64(tc.origin + tc.elapsed), Message: "READY"}}
			if !reflect.DeepEqual(r.Checks[1], want) {
				t.Fatalf("got %#v, want %#v", r.Checks[1], want)
			}
		})
	}
}

func TestEvaluatePhysicalEligibilityAndFirstWitness(t *testing.T) {
	cfg := bootlog.Config{SchemaVersion: 1, BootStart: bootlog.BootStartConfig{Pattern: "^START"}, Milestones: []bootlog.MilestoneConfig{
		{ID: "late_in_config", Pattern: "^SECOND$", WithinMS: testfixture.Int64(2)},
		{ID: "early_in_log", Pattern: "^FIRST$", WithinMS: testfixture.Int64(1)},
		{ID: "preonly", Pattern: "^PRE$"},
		{ID: "start_line", Pattern: "START", WithinMS: testfixture.Int64(0)},
		{ID: "untimed_first", Pattern: "READY", WithinMS: testfixture.Int64(1)},
		{ID: "presence", Pattern: "READY"},
	}, Forbidden: []bootlog.ForbiddenConfig{{ID: "start_forbidden", Pattern: "START"}, {ID: "after", Pattern: "BAD"}}}
	log := bootlog.Log{Records: []bootlog.Record{
		testfixture.Timed(1, 5_000_000, "PRE"),
		testfixture.Timed(2, 5_000_000, "READY"),
		testfixture.Timed(3, 5_000_000, "START START"),
		testfixture.Untimed(4, "READY READY"),
		testfixture.Timed(5, 6_000_000, "FIRST"),
		testfixture.Timed(6, 6_000_000, "READY"),
		testfixture.Timed(7, 7_000_000, "SECOND"),
		testfixture.Timed(8, 8_000_000, "BAD BAD"),
		testfixture.Untimed(9, "BAD"),
	}}
	r := evaluatedRules(t, log, cfg)
	wants := []bootlog.CheckResult{
		{ID: "late_in_config", Kind: bootlog.KindMilestone, Status: bootlog.StatusPassed, Reason: bootlog.ReasonMatched, WithinMS: testfixture.Int64(2), ElapsedNS: testfixture.Int64(2_000_000), MatchCount: 1, Evidence: &bootlog.Evidence{Line: 7, TimestampNS: testfixture.Int64(7_000_000), Message: "SECOND"}},
		{ID: "early_in_log", Kind: bootlog.KindMilestone, Status: bootlog.StatusPassed, Reason: bootlog.ReasonMatched, WithinMS: testfixture.Int64(1), ElapsedNS: testfixture.Int64(1_000_000), MatchCount: 1, Evidence: &bootlog.Evidence{Line: 5, TimestampNS: testfixture.Int64(6_000_000), Message: "FIRST"}},
		{ID: "preonly", Kind: bootlog.KindMilestone, Status: bootlog.StatusFailed, Reason: bootlog.ReasonMissing},
		{ID: "start_line", Kind: bootlog.KindMilestone, Status: bootlog.StatusPassed, Reason: bootlog.ReasonMatched, WithinMS: testfixture.Int64(0), ElapsedNS: testfixture.Int64(0), MatchCount: 1, Evidence: &bootlog.Evidence{Line: 3, TimestampNS: testfixture.Int64(5_000_000), Message: "START START"}},
		{ID: "untimed_first", Kind: bootlog.KindMilestone, Status: bootlog.StatusFailed, Reason: bootlog.ReasonUntimed, WithinMS: testfixture.Int64(1), MatchCount: 2, Evidence: &bootlog.Evidence{Line: 4, Message: "READY READY"}},
		{ID: "presence", Kind: bootlog.KindMilestone, Status: bootlog.StatusPassed, Reason: bootlog.ReasonMatched, MatchCount: 2, Evidence: &bootlog.Evidence{Line: 4, Message: "READY READY"}},
		{ID: "start_forbidden", Kind: bootlog.KindForbidden, Status: bootlog.StatusFailed, Reason: bootlog.ReasonForbiddenMatch, ElapsedNS: testfixture.Int64(0), MatchCount: 1, Evidence: &bootlog.Evidence{Line: 3, TimestampNS: testfixture.Int64(5_000_000), Message: "START START"}},
		{ID: "after", Kind: bootlog.KindForbidden, Status: bootlog.StatusFailed, Reason: bootlog.ReasonForbiddenMatch, ElapsedNS: testfixture.Int64(3_000_000), MatchCount: 2, Evidence: &bootlog.Evidence{Line: 8, TimestampNS: testfixture.Int64(8_000_000), Message: "BAD BAD"}},
	}
	if !reflect.DeepEqual(r.Checks[1:], wants) {
		t.Fatalf("physical eligibility/first witness: %#v", r.Checks)
	}
	if r.Summary != (bootlog.Summary{Total: 9, Passed: 5, Failed: 4}) {
		t.Fatalf("summary: %#v", r.Summary)
	}
}

func TestEvaluateForbiddenPreambleAndExtremeElapsed(t *testing.T) {
	cfg := bootlog.Config{SchemaVersion: 1, BootStart: bootlog.BootStartConfig{Pattern: "START"}, Forbidden: []bootlog.ForbiddenConfig{{ID: "timed", Pattern: "BAD"}, {ID: "untimed", Pattern: "FAULT"}}}
	log := bootlog.Log{Records: []bootlog.Record{testfixture.Untimed(1, "FAULT"), testfixture.Timed(2, 0, "BAD"), testfixture.Timed(3, math.MaxInt64, "START"), testfixture.Timed(4, math.MaxInt64, "BAD FAULT")}}
	r := evaluatedRules(t, log, cfg)
	if r.Checks[1].MatchCount != 2 || *r.Checks[1].ElapsedNS != -math.MaxInt64 || r.Checks[1].Evidence.Line != 2 {
		t.Fatalf("negative elapsed: %#v", r.Checks[1])
	}
	if r.Checks[2].MatchCount != 2 || r.Checks[2].ElapsedNS != nil || r.Checks[2].Evidence.Line != 1 {
		t.Fatalf("untimed preamble: %#v", r.Checks[2])
	}
}

func TestEvaluateMatchingMessageOnly(t *testing.T) {
	cfg := bootlog.Config{SchemaVersion: 1, BootStart: bootlog.BootStartConfig{Pattern: "^START$"}, Forbidden: []bootlog.ForbiddenConfig{
		{ID: "case_sensitive", Pattern: "BAD"}, {ID: "case_flag", Pattern: "(?i)BAD"},
		{ID: "substring", Pattern: "ad"}, {ID: "anchored", Pattern: "^bad$"},
		{ID: "across_records", Pattern: "bad.*NEXT"}, {ID: "timestamp_prefix", Pattern: `^\[`},
		{ID: "control", Pattern: "\x1b\\[31m"},
	}}
	log := bootlog.Log{Records: []bootlog.Record{testfixture.Timed(1, 0, "START"), testfixture.Timed(2, 1, "bad bad"), testfixture.Timed(3, 2, "NEXT"), testfixture.Untimed(4, "\x1b[31m")}}
	r := evaluatedRules(t, log, cfg)
	for i, want := range []int64{0, 1, 1, 0, 0, 0, 1} {
		if r.Checks[i+1].MatchCount != want {
			t.Errorf("%s count=%d want %d", r.Checks[i+1].ID, r.Checks[i+1].MatchCount, want)
		}
	}
}

func TestEvaluateRejectsInvalidInputsWithoutReport(t *testing.T) {
	set, err := bootlog.CompileRules(testfixture.Config())
	if err != nil {
		t.Fatal(err)
	}
	for name, records := range map[string][]bootlog.Record{
		"untimed start":           {testfixture.Untimed(1, "Linux version test")},
		"two timed starts":        {testfixture.Timed(1, 0, "Linux version a"), testfixture.Timed(2, 0, "Linux version b")},
		"timed then untimed":      {testfixture.Timed(1, 0, "Linux version a"), testfixture.Untimed(2, "Linux version b")},
		"untimed then timed":      {testfixture.Untimed(1, "Linux version a"), testfixture.Timed(2, 0, "Linux version b")},
		"after readiness":         {testfixture.Timed(1, 0, "Linux version a"), testfixture.Timed(2, 1, "NETWORK_READY"), testfixture.Untimed(3, "APP_READY"), testfixture.Timed(4, 2, "Linux version b")},
		"negative timestamp":      {testfixture.Timed(1, -1, "anything")},
		"zero line":               {testfixture.Timed(0, 0, "anything")},
		"negative line":           {testfixture.Untimed(-1, "anything")},
		"repeated line":           {testfixture.Untimed(1, "a"), testfixture.Untimed(1, "b")},
		"line decrease":           {testfixture.Untimed(2, "a"), testfixture.Untimed(1, "b")},
		"timestamp decrease":      {testfixture.Timed(1, 2, "a"), testfixture.Timed(2, 1, "b")},
		"invalid UTF8":            {testfixture.Untimed(1, string([]byte{0xff}))},
		"invalid after readiness": {testfixture.Timed(1, 2, "Linux version a"), testfixture.Timed(2, 3, "NETWORK_READY"), testfixture.Timed(3, 4, "APP_READY"), testfixture.Timed(4, 1, "late bad record")},
	} {
		t.Run(name, func(t *testing.T) {
			r, err := bootlog.Evaluate(bootlog.Log{Records: records}, set)
			var typed *bootlog.InvalidInputError
			if !errors.Is(err, bootlog.ErrInvalidInput) || !errors.As(err, &typed) || !reflect.DeepEqual(r, bootlog.Report{}) {
				t.Fatalf("got %#v, %v", r, err)
			}
		})
	}
	for _, set := range []*bootlog.RuleSet{nil, {}} {
		r, err := bootlog.Evaluate(testfixture.Log(), set)
		if !errors.Is(err, bootlog.ErrInvalidRules) || !reflect.DeepEqual(r, bootlog.Report{}) {
			t.Fatalf("invalid ruleset: %#v, %v", r, err)
		}
	}
}

func TestEvaluateDoesNotApplyParserCaps(t *testing.T) {
	cfg := bootlog.Config{SchemaVersion: 1, BootStart: bootlog.BootStartConfig{Pattern: "^START$"}, Forbidden: []bootlog.ForbiddenConfig{{ID: "bad", Pattern: "BAD"}}}
	// A caller-built record can exceed all parser byte limits; physical record
	// count can also exceed MaxLines. The evaluator checks structural validity.
	log := bootlog.Log{Records: make([]bootlog.Record, bootlog.DefaultMaxLines+1)}
	log.Records[0] = testfixture.Timed(1, 0, "START")
	for i := 1; i < len(log.Records); i++ {
		log.Records[i] = testfixture.Untimed(int64(i+1), "x")
	}
	log.Records[len(log.Records)-1].Message = strings.Repeat("x", int(bootlog.DefaultMaxInputBytes)+1)
	r := evaluatedRules(t, log, cfg)
	if r.Summary.Failed != 0 {
		t.Fatalf("caller-built large log failed: %#v", r.Summary)
	}
}

func TestRuleSetAndEvaluationImmutabilityAndConcurrency(t *testing.T) {
	cfg := testfixture.Config()
	log := testfixture.Log()
	cfgBefore, _ := json.Marshal(cfg)
	logBefore, _ := json.Marshal(log)
	set, err := bootlog.CompileRules(cfg)
	if err != nil {
		t.Fatal(err)
	}
	first, err := bootlog.Evaluate(log, set)
	if err != nil {
		t.Fatal(err)
	}
	cfgAfter, _ := json.Marshal(cfg)
	logAfter, _ := json.Marshal(log)
	if string(cfgBefore) != string(cfgAfter) || string(logBefore) != string(logAfter) {
		t.Fatal("compile/evaluate mutated inputs")
	}
	// All exported storage remains caller-owned. Neither input edits nor report
	// edits may alter compiled state or a later result.
	*cfg.Milestones[0].WithinMS = 0
	cfg.Milestones[0].ID = "changed"
	cfg.Milestones[0].Pattern = "NEVER"
	cfg.Forbidden[0].ID = "changed"
	cfg.Forbidden[0].Pattern = "NETWORK"
	cfg.BootStart.Pattern = "NEVER"
	*first.Checks[1].WithinMS = 0
	*first.Checks[1].Evidence.TimestampNS = 42
	*first.Origin.TimestampNS = 42
	*first.Checks[0].Evidence.TimestampNS = 43
	first.Checks[2].Evidence.Message = "changed"
	logAfter, _ = json.Marshal(log)
	if string(logBefore) != string(logAfter) {
		t.Fatal("report aliases input timestamps")
	}
	want := testfixture.PassingReport()
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 20 {
				got, err := bootlog.Evaluate(log, set)
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Errorf("shared ruleset changed: %#v, %v", got, err)
					return
				}
				// A caller may mutate its own report concurrently with evaluations.
				*got.Checks[1].WithinMS = 42
				*got.Origin.TimestampNS = 43
			}
		})
	}
	wg.Wait()
}

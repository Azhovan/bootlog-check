package bootlog_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	bootlog "github.com/firmdura/bootlog-check"
)

func ns(value int64) *int64 { return &value }

// These small expectations are authored directly, without an evaluator.
func basicReport() bootlog.Report {
	return bootlog.Report{
		SchemaVersion: 1, TimingBasis: bootlog.TimingBasis, Scope: bootlog.CaptureScope,
		Origin: &bootlog.Evidence{Line: 2, TimestampNS: ns(1_000_000_000), Message: "START"},
		Checks: []bootlog.CheckResult{
			{ID: "boot_start", Kind: bootlog.KindStart, Status: bootlog.StatusPassed, Reason: bootlog.ReasonMatched, ElapsedNS: ns(0), MatchCount: 1, Evidence: &bootlog.Evidence{Line: 2, TimestampNS: ns(1_000_000_000), Message: "START"}},
			{ID: "ready", Kind: bootlog.KindMilestone, Status: bootlog.StatusPassed, Reason: bootlog.ReasonMatched, WithinMS: ns(500), ElapsedNS: ns(500_000_000), MatchCount: 2, Evidence: &bootlog.Evidence{Line: 4, TimestampNS: ns(1_500_000_000), Message: "READY"}},
			{ID: "panic", Kind: bootlog.KindForbidden, Status: bootlog.StatusPassed, Reason: bootlog.ReasonAbsent},
		},
		Summary: bootlog.Summary{Total: 3, Passed: 3},
	}
}

func TestReportValidStates(t *testing.T) {
	cases := []struct {
		name   string
		change func(*bootlog.Report)
	}{
		{"deadline equality", func(r *bootlog.Report) {}},
		{"late by one ns", func(r *bootlog.Report) {
			r.Checks[1].Evidence.TimestampNS = ns(1_500_000_001)
			r.Checks[1].ElapsedNS = ns(500_000_001)
			r.Checks[1].Status = bootlog.StatusFailed
			r.Checks[1].Reason = bootlog.ReasonLate
			r.Summary.Passed--
			r.Summary.Failed++
		}},
		{"timed milestone untimed first witness", func(r *bootlog.Report) {
			r.Checks[1].Evidence.TimestampNS = nil
			r.Checks[1].ElapsedNS = nil
			r.Checks[1].Status = bootlog.StatusFailed
			r.Checks[1].Reason = bootlog.ReasonUntimed
			r.Summary.Passed--
			r.Summary.Failed++
		}},
		{"presence only untimed", func(r *bootlog.Report) {
			r.Checks[1].WithinMS = nil
			r.Checks[1].Evidence.TimestampNS = nil
			r.Checks[1].ElapsedNS = nil
		}},
		{"presence only timed", func(r *bootlog.Report) { r.Checks[1].WithinMS = nil }},
		{"missing milestone", func(r *bootlog.Report) {
			r.Checks[1].Evidence = nil
			r.Checks[1].ElapsedNS = nil
			r.Checks[1].MatchCount = 0
			r.Checks[1].Status = bootlog.StatusFailed
			r.Checks[1].Reason = bootlog.ReasonMissing
			r.Summary.Passed--
			r.Summary.Failed++
		}},
		{"forbidden before origin", func(r *bootlog.Report) {
			r.Checks[2].Evidence = &bootlog.Evidence{Line: 1, TimestampNS: ns(0), Message: "panic\x00\x1b"}
			r.Checks[2].ElapsedNS = ns(-1_000_000_000)
			r.Checks[2].MatchCount = 1
			r.Checks[2].Status = bootlog.StatusFailed
			r.Checks[2].Reason = bootlog.ReasonForbiddenMatch
			r.Summary.Passed--
			r.Summary.Failed++
		}},
		{"forbidden untimed", func(r *bootlog.Report) {
			r.Checks[2].Evidence = &bootlog.Evidence{Line: 1, Message: "panic"}
			r.Checks[2].MatchCount = 2
			r.Checks[2].Status = bootlog.StatusFailed
			r.Checks[2].Reason = bootlog.ReasonForbiddenMatch
			r.Summary.Passed--
			r.Summary.Failed++
		}},
		{"missing origin still evaluates forbidden", func(r *bootlog.Report) {
			r.Origin = nil
			r.Checks[0] = bootlog.CheckResult{ID: "boot_start", Kind: bootlog.KindStart, Status: bootlog.StatusFailed, Reason: bootlog.ReasonMissing}
			r.Checks[1] = bootlog.CheckResult{ID: "ready", Kind: bootlog.KindMilestone, Status: bootlog.StatusNotEvaluated, Reason: bootlog.ReasonNoBootStart, WithinMS: ns(500)}
			r.Checks[2] = bootlog.CheckResult{ID: "panic", Kind: bootlog.KindForbidden, Status: bootlog.StatusFailed, Reason: bootlog.ReasonForbiddenMatch, MatchCount: 1, Evidence: &bootlog.Evidence{Line: 1, TimestampNS: ns(99), Message: "panic"}}
			r.Summary = bootlog.Summary{Total: 3, Failed: 2, NotEvaluated: 1}
		}},
		{"same record satisfies start milestone and forbidden", func(r *bootlog.Report) {
			r.Checks[1].Evidence = r.Origin
			r.Checks[1].ElapsedNS = ns(0)
			r.Checks[1].WithinMS = ns(0)
			r.Checks[2].Evidence = r.Origin
			r.Checks[2].ElapsedNS = ns(0)
			r.Checks[2].MatchCount = 1
			r.Checks[2].Status = bootlog.StatusFailed
			r.Checks[2].Reason = bootlog.ReasonForbiddenMatch
			r.Summary.Passed--
			r.Summary.Failed++
		}},
		{"maximum timestamp and negative elapsed", func(r *bootlog.Report) {
			r.Origin.TimestampNS = ns(math.MaxInt64)
			r.Checks[0].Evidence.TimestampNS = ns(math.MaxInt64)
			r.Checks[1].Evidence.TimestampNS = ns(math.MaxInt64)
			r.Checks[1].ElapsedNS = ns(0)
			r.Checks[1].WithinMS = ns(bootlog.MaxWithinMS)
			r.Checks[2].Evidence = &bootlog.Evidence{Line: 1, TimestampNS: ns(0), Message: "panic"}
			r.Checks[2].ElapsedNS = ns(-math.MaxInt64)
			r.Checks[2].MatchCount = 1
			r.Checks[2].Status = bootlog.StatusFailed
			r.Checks[2].Reason = bootlog.ReasonForbiddenMatch
			r.Summary.Passed--
			r.Summary.Failed++
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := basicReport()
			tc.change(&r)
			if err := r.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReportRejectsInconsistentModels(t *testing.T) {
	cases := []struct {
		name   string
		change func(*bootlog.Report)
	}{
		{"version", func(r *bootlog.Report) { r.SchemaVersion = 2 }},
		{"basis", func(r *bootlog.Report) { r.TimingBasis = "host clock" }},
		{"claim", func(r *bootlog.Report) { r.Scope = "device is healthy" }},
		{"empty checks", func(r *bootlog.Report) { r.Checks = nil }},
		{"no user check", func(r *bootlog.Report) { r.Checks = r.Checks[:1] }},
		{"too many checks", func(r *bootlog.Report) { r.Checks = make([]bootlog.CheckResult, 130) }},
		{"missing first start", func(r *bootlog.Report) { r.Checks[0], r.Checks[1] = r.Checks[1], r.Checks[0] }},
		{"wrong start id", func(r *bootlog.Report) { r.Checks[0].ID = "start" }},
		{"wrong start kind", func(r *bootlog.Report) { r.Checks[0].Kind = bootlog.KindMilestone }},
		{"duplicate start", func(r *bootlog.Report) { r.Checks[1].ID = "boot_start" }},
		{"second start kind", func(r *bootlog.Report) { r.Checks[1].Kind = bootlog.KindStart }},
		{"unknown kind", func(r *bootlog.Report) { r.Checks[1].Kind = "other" }},
		{"duplicate ID", func(r *bootlog.Report) { r.Checks[2].ID = "ready" }},
		{"invalid ID", func(r *bootlog.Report) { r.Checks[1].ID = "1bad" }},
		{"ID too long", func(r *bootlog.Report) { r.Checks[1].ID = strings.Repeat("x", 65) }},
		{"wrong ordering", func(r *bootlog.Report) { r.Checks[1], r.Checks[2] = r.Checks[2], r.Checks[1] }},
		{"unknown status", func(r *bootlog.Report) { r.Checks[1].Status = "success" }},
		{"unknown reason", func(r *bootlog.Report) { r.Checks[1].Reason = "success" }},
		{"wrong summary total", func(r *bootlog.Report) { r.Summary.Total++ }},
		{"wrong summary passed", func(r *bootlog.Report) { r.Summary.Passed-- }},
		{"wrong summary failed", func(r *bootlog.Report) { r.Summary.Failed++ }},
		{"wrong summary skipped", func(r *bootlog.Report) { r.Summary.NotEvaluated++ }},
		{"negative deadline", func(r *bootlog.Report) { r.Checks[1].WithinMS = ns(-1) }},
		{"overflow deadline", func(r *bootlog.Report) { r.Checks[1].WithinMS = ns(bootlog.MaxWithinMS + 1) }},
		{"start deadline", func(r *bootlog.Report) { r.Checks[0].WithinMS = ns(0) }},
		{"forbidden deadline", func(r *bootlog.Report) { r.Checks[2].WithinMS = ns(0) }},
		{"negative count", func(r *bootlog.Report) { r.Checks[1].MatchCount = -1 }},
		{"zero count with evidence", func(r *bootlog.Report) { r.Checks[1].MatchCount = 0 }},
		{"positive count without evidence", func(r *bootlog.Report) { r.Checks[2].MatchCount = 1 }},
		{"duplicate start matches", func(r *bootlog.Report) { r.Checks[0].MatchCount = 2 }},
		{"no origin with start witness", func(r *bootlog.Report) { r.Origin = nil; r.Checks[0].ElapsedNS = nil; r.Checks[1].ElapsedNS = nil }},
		{"untimed origin", func(r *bootlog.Report) { r.Origin.TimestampNS = nil }},
		{"negative origin time", func(r *bootlog.Report) { r.Origin.TimestampNS = ns(-1) }},
		{"invalid origin line", func(r *bootlog.Report) { r.Origin.Line = 0 }},
		{"invalid origin text", func(r *bootlog.Report) { r.Origin.Message = "\xff" }},
		{"origin mismatch", func(r *bootlog.Report) { r.Origin.Message = "different" }},
		{"invalid witness line", func(r *bootlog.Report) { r.Checks[1].Evidence.Line = -1 }},
		{"invalid witness text", func(r *bootlog.Report) { r.Checks[1].Evidence.Message = "\xff" }},
		{"negative witness time", func(r *bootlog.Report) { r.Checks[1].Evidence.TimestampNS = ns(-1) }},
		{"wrong elapsed", func(r *bootlog.Report) { r.Checks[1].ElapsedNS = ns(499_999_999) }},
		{"missing elapsed", func(r *bootlog.Report) { r.Checks[1].ElapsedNS = nil }},
		{"fabricated elapsed", func(r *bootlog.Report) { r.Checks[2].ElapsedNS = ns(0) }},
		{"untimed with elapsed", func(r *bootlog.Report) { r.Checks[1].Evidence.TimestampNS = nil }},
		{"milestone before origin", func(r *bootlog.Report) { r.Checks[1].Evidence.Line = 1 }},
		{"milestone negative elapsed", func(r *bootlog.Report) {
			r.Checks[1].Evidence.TimestampNS = ns(0)
			r.Checks[1].ElapsedNS = ns(-1_000_000_000)
		}},
		{"late marked passed", func(r *bootlog.Report) { r.Checks[1].WithinMS = ns(499) }},
		{"untimed marked passed", func(r *bootlog.Report) { r.Checks[1].Evidence.TimestampNS = nil; r.Checks[1].ElapsedNS = nil }},
		{"missing milestone marked passed", func(r *bootlog.Report) {
			r.Checks[1].Evidence = nil
			r.Checks[1].ElapsedNS = nil
			r.Checks[1].MatchCount = 0
		}},
		{"not evaluated with origin", func(r *bootlog.Report) {
			r.Checks[1].Status = bootlog.StatusNotEvaluated
			r.Checks[1].Reason = bootlog.ReasonNoBootStart
		}},
		{"conflicting shared evidence", func(r *bootlog.Report) { r.Checks[1].Evidence.Line = 2 }},
		{"forbidden clock reversal", func(r *bootlog.Report) {
			r.Checks[2] = bootlog.CheckResult{ID: "panic", Kind: bootlog.KindForbidden, Status: bootlog.StatusFailed, Reason: bootlog.ReasonForbiddenMatch, MatchCount: 1, Evidence: &bootlog.Evidence{Line: 5, TimestampNS: ns(1_100_000_000), Message: "panic"}, ElapsedNS: ns(100_000_000)}
			r.Summary.Passed--
			r.Summary.Failed++
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := basicReport()
			tc.change(&r)
			err := r.Validate()
			var typed *bootlog.InvalidReportError
			if !errors.Is(err, bootlog.ErrInvalidReport) || !errors.As(err, &typed) || typed.Field == "" {
				t.Fatalf("expected report validation error, got %v", err)
			}
		})
	}
}

func TestMissingOriginRequiresUnevaluatedMilestones(t *testing.T) {
	r := basicReport()
	r.Origin = nil
	r.Checks[0] = bootlog.CheckResult{ID: "boot_start", Kind: bootlog.KindStart, Status: bootlog.StatusFailed, Reason: bootlog.ReasonMissing}
	r.Checks[1] = bootlog.CheckResult{ID: "ready", Kind: bootlog.KindMilestone, Status: bootlog.StatusFailed, Reason: bootlog.ReasonMissing}
	r.Summary = bootlog.Summary{Total: 3, Failed: 2, Passed: 1}
	if !errors.Is(r.Validate(), bootlog.ErrInvalidReport) {
		t.Fatal("missing origin must make milestones not_evaluated")
	}
	r.Checks[1].Status, r.Checks[1].Reason = bootlog.StatusNotEvaluated, bootlog.ReasonNoBootStart
	r.Checks[1].MatchCount = 1
	r.Checks[1].Evidence = &bootlog.Evidence{Line: 3, Message: "ready"}
	if !errors.Is(r.Validate(), bootlog.ErrInvalidReport) {
		t.Fatal("unevaluated milestone cannot retain a witness")
	}
}

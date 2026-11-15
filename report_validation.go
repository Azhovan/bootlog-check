package bootlog

import (
	"fmt"
	"sort"
	"unicode/utf8"
)

// Validate checks report structure and internal consistency. It cannot prove
// source-dependent facts such as matching counts, first-witness selection, or
// configuration order within a kind; these are the evaluator's responsibility.
// Valid failed checks are not errors. Writers must validate before writing.
func (r Report) Validate() error {
	invalid := func(field, detail string) error { return &InvalidReportError{Field: field, Detail: detail} }
	if r.SchemaVersion != SchemaVersion {
		return invalid("schema_version", "must equal 1")
	}
	if r.TimingBasis != TimingBasis {
		return invalid("timing_basis", "must equal boot_start")
	}
	if r.Scope != CaptureScope {
		return invalid("scope", "must contain the schema's capture limitation")
	}
	if len(r.Checks) < 2 || len(r.Checks) > MaxUserRules+1 {
		return invalid("checks", "must contain a start and 1 through 128 user checks")
	}
	if r.Origin != nil {
		if err := validateEvidence(r.Origin, "origin"); err != nil {
			return err
		}
		if r.Origin.TimestampNS == nil {
			return invalid("origin.timestamp_ns", "must be present")
		}
	}
	ids := make(map[string]bool, len(r.Checks))
	evidenceByLine := make(map[int64]*Evidence, len(r.Checks)+1)
	if r.Origin != nil {
		evidenceByLine[r.Origin.Line] = r.Origin
	}
	var summary Summary
	seenForbidden := false
	for i, check := range r.Checks {
		field := fmt.Sprintf("checks[%d]", i)
		if !validID(check.ID) || ids[check.ID] {
			return invalid(field+".id", "must be a unique valid ID")
		}
		ids[check.ID] = true
		if i == 0 {
			if check.ID != BootStartID || check.Kind != KindStart {
				return invalid(field, "first check must be boot_start of kind start")
			}
		} else {
			if check.ID == BootStartID {
				return invalid(field+".id", "boot_start is reserved")
			}
			switch check.Kind {
			case KindMilestone:
				if seenForbidden {
					return invalid(field+".kind", "milestones must precede forbidden checks")
				}
			case KindForbidden:
				seenForbidden = true
			default:
				return invalid(field+".kind", "must be milestone or forbidden")
			}
		}
		if check.WithinMS != nil && (check.Kind != KindMilestone || *check.WithinMS < 0 || *check.WithinMS > MaxWithinMS) {
			return invalid(field+".within_ms", "only milestones accept a nonnegative deadline fitting int64 nanoseconds")
		}
		if check.MatchCount < 0 || (check.MatchCount == 0) != (check.Evidence == nil) {
			return invalid(field+".match_count", "must be nonnegative, with evidence exactly when positive")
		}
		if check.Evidence != nil {
			if err := validateEvidence(check.Evidence, field+".evidence"); err != nil {
				return err
			}
			if previous, ok := evidenceByLine[check.Evidence.Line]; ok && !sameEvidence(previous, check.Evidence) {
				return invalid(field+".evidence", "same physical line must describe the same record")
			}
			evidenceByLine[check.Evidence.Line] = check.Evidence
		}
		if r.Origin != nil && check.Evidence != nil && check.Evidence.TimestampNS != nil {
			want := *check.Evidence.TimestampNS - *r.Origin.TimestampNS // nonnegative int64 operands cannot overflow their signed difference
			if check.ElapsedNS == nil || *check.ElapsedNS != want {
				return invalid(field+".elapsed_ns", "must equal witness timestamp minus origin timestamp")
			}
		} else if check.ElapsedNS != nil {
			return invalid(field+".elapsed_ns", "must be null without both timestamps")
		}
		if err := validateCheckState(check, r.Origin, field); err != nil {
			return err
		}
		summary.Total++
		switch check.Status {
		case StatusPassed:
			summary.Passed++
		case StatusFailed:
			summary.Failed++
		case StatusNotEvaluated:
			summary.NotEvaluated++
		default:
			return invalid(field+".status", "unknown status")
		}
	}
	if r.Summary != summary {
		return invalid("summary", "counts must exactly match check statuses")
	}
	// Evidence is a subset of one nondecreasing-timestamp log. This also checks
	// forbidden witnesses on either side of the origin and shared evidence lines.
	lines := make([]int64, 0, len(evidenceByLine))
	for line := range evidenceByLine {
		lines = append(lines, line)
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i] < lines[j] })
	var previousTime int64
	for _, line := range lines {
		timestamp := evidenceByLine[line].TimestampNS
		if timestamp != nil {
			if *timestamp < previousTime {
				return invalid("checks.evidence.timestamp_ns", "must be nondecreasing in physical line order")
			}
			previousTime = *timestamp
		}
	}
	return nil
}

func validateCheckState(check CheckResult, origin *Evidence, field string) error {
	status, reason := StatusPassed, ReasonMatched
	invalid := func(detail string) error { return &InvalidReportError{Field: field, Detail: detail} }
	switch check.Kind {
	case KindStart:
		if origin == nil {
			status, reason = StatusFailed, ReasonMissing
			if check.MatchCount != 0 {
				return invalid("missing origin cannot have a start witness")
			}
		} else if check.MatchCount != 1 || !sameEvidence(check.Evidence, origin) {
			return invalid("start must have exactly one witness identical to origin")
		}
	case KindMilestone:
		if origin == nil {
			status, reason = StatusNotEvaluated, ReasonNoBootStart
			if check.MatchCount != 0 {
				return invalid("unevaluated milestone cannot have matches")
			}
		} else if check.MatchCount == 0 {
			status, reason = StatusFailed, ReasonMissing
		} else {
			if check.Evidence.Line < origin.Line || (check.ElapsedNS != nil && *check.ElapsedNS < 0) {
				return invalid("milestone witness cannot precede origin")
			}
			if check.WithinMS != nil {
				if check.Evidence.TimestampNS == nil {
					status, reason = StatusFailed, ReasonUntimed
				} else if *check.ElapsedNS > *check.WithinMS*NanosecondsPerMS {
					status, reason = StatusFailed, ReasonLate
				}
			}
		}
	case KindForbidden:
		reason = ReasonAbsent
		if check.MatchCount > 0 {
			status, reason = StatusFailed, ReasonForbiddenMatch
		}
	}
	if check.Status != status || check.Reason != reason {
		return invalid(fmt.Sprintf("state must be %s/%s for its evidence and deadline", status, reason))
	}
	return nil
}

func validateEvidence(e *Evidence, field string) error {
	if e.Line <= 0 {
		return &InvalidReportError{Field: field + ".line", Detail: "must be positive"}
	}
	if e.TimestampNS != nil && *e.TimestampNS < 0 {
		return &InvalidReportError{Field: field + ".timestamp_ns", Detail: "must be nonnegative"}
	}
	if !utf8.ValidString(e.Message) {
		return &InvalidReportError{Field: field + ".message", Detail: "must be valid UTF-8"}
	}
	return nil
}

func sameEvidence(a, b *Evidence) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.Line != b.Line || a.Message != b.Message || (a.TimestampNS == nil) != (b.TimestampNS == nil) {
		return false
	}
	return a.TimestampNS == nil || *a.TimestampNS == *b.TimestampNS
}

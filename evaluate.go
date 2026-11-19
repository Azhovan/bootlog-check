package bootlog

import (
	"fmt"
	"regexp"
)

// Evaluate checks the complete log against immutable compiled rules. It validates
// caller-built records without applying parser caps and does not mutate either
// input. Evaluation is synchronous, takes no context, and is finite and bounded
// by the validated log and rule limits. A failed check is a Report, not an error.
// An invalid input or ruleset returns an error and no partial report.
func Evaluate(log Log, rules *RuleSet) (Report, error) {
	if rules == nil || rules.patterns.bootStart == nil {
		return Report{}, &InvalidRulesError{Detail: "ruleset must be constructed with CompileRules"}
	}
	if err := log.Validate(); err != nil {
		return Report{}, err
	}
	originIndex := -1
	for i, record := range log.Records {
		if !rules.patterns.bootStart.MatchString(record.Message) {
			continue
		}
		if originIndex >= 0 {
			return Report{}, &InvalidInputError{
				Line: record.Line, Field: "boot_start",
				Detail: fmt.Sprintf("must match exactly one record; first match at line %d", log.Records[originIndex].Line),
			}
		}
		originIndex = i
	}
	if originIndex >= 0 && log.Records[originIndex].TimestampNS == nil {
		return Report{}, &InvalidInputError{Line: log.Records[originIndex].Line, Field: "boot_start", Detail: "must have a timestamp"}
	}

	report := Report{SchemaVersion: SchemaVersion, TimingBasis: TimingBasis, Scope: CaptureScope,
		Checks: make([]CheckResult, 0, 1+len(rules.config.Milestones)+len(rules.config.Forbidden))}
	start := CheckResult{ID: BootStartID, Kind: KindStart, Status: StatusFailed, Reason: ReasonMissing}
	if originIndex >= 0 {
		report.Origin = ruleEvidence(log.Records[originIndex])
		start.Status, start.Reason = StatusPassed, ReasonMatched
		start.MatchCount, start.Evidence = 1, ruleEvidence(log.Records[originIndex])
		zero := int64(0)
		start.ElapsedNS = &zero
	}
	report.Checks = append(report.Checks, start)
	for i, rule := range rules.config.Milestones {
		check := CheckResult{ID: rule.ID, Kind: KindMilestone, WithinMS: ruleIntCopy(rule.WithinMS)}
		if originIndex < 0 {
			check.Status, check.Reason = StatusNotEvaluated, ReasonNoBootStart
		} else {
			check.Evidence, check.MatchCount = ruleMatches(log.Records[originIndex:], rules.patterns.milestones[i])
			check.ElapsedNS = ruleElapsed(report.Origin, check.Evidence)
			switch {
			case check.Evidence == nil:
				check.Status, check.Reason = StatusFailed, ReasonMissing
			case rule.WithinMS != nil && check.ElapsedNS == nil:
				check.Status, check.Reason = StatusFailed, ReasonUntimed
			case rule.WithinMS != nil && *check.ElapsedNS > *rule.WithinMS*NanosecondsPerMS:
				check.Status, check.Reason = StatusFailed, ReasonLate
			default:
				check.Status, check.Reason = StatusPassed, ReasonMatched
			}
		}
		report.Checks = append(report.Checks, check)
	}
	for i, rule := range rules.config.Forbidden {
		check := CheckResult{ID: rule.ID, Kind: KindForbidden, Status: StatusPassed, Reason: ReasonAbsent}
		check.Evidence, check.MatchCount = ruleMatches(log.Records, rules.patterns.forbidden[i])
		check.ElapsedNS = ruleElapsed(report.Origin, check.Evidence)
		if check.MatchCount > 0 {
			check.Status, check.Reason = StatusFailed, ReasonForbiddenMatch
		}
		report.Checks = append(report.Checks, check)
	}
	report.Summary.Total = len(report.Checks)
	for _, check := range report.Checks {
		switch check.Status {
		case StatusPassed:
			report.Summary.Passed++
		case StatusFailed:
			report.Summary.Failed++
		case StatusNotEvaluated:
			report.Summary.NotEvaluated++
		}
	}
	return report, nil
}

func ruleEvidence(record Record) *Evidence {
	return &Evidence{Line: record.Line, TimestampNS: ruleIntCopy(record.TimestampNS), Message: record.Message}
}

func ruleMatches(records []Record, pattern *regexp.Regexp) (*Evidence, int64) {
	var first *Evidence
	var count int64
	for _, record := range records {
		if pattern.MatchString(record.Message) {
			count++
			if first == nil {
				first = ruleEvidence(record)
			}
		}
	}
	return first, count
}

func ruleElapsed(origin, witness *Evidence) *int64 {
	if origin == nil || witness == nil || witness.TimestampNS == nil {
		return nil
	}
	// Valid timestamps are between zero and MaxInt64, so signed differences fit.
	elapsed := *witness.TimestampNS - *origin.TimestampNS
	return &elapsed
}

package bootlog

import "encoding/json"

const (
	SchemaVersion = 1
	BootStartID   = "boot_start"

	// TimingBasis identifies elapsed times as relative to the unique timed start.
	TimingBasis = "boot_start"
	// CaptureScope is included verbatim in every report, including failed reports.
	CaptureScope = "Results describe only the supplied capture; a pass does not prove capture completeness or device health."
)

// Record is one retained message with its original 1-based physical line number.
// TimestampNS is nil for an untimestamped message. Zero is a real timestamp.
type Record struct {
	Line        int64  `json:"line"`
	TimestampNS *int64 `json:"timestamp_ns"`
	Message     string `json:"message"`
}

// Log retains records in physical order. It carries no source path or raw-byte
// counts, so equivalent LF and CRLF captures have identical logical records.
type Log struct {
	Records []Record `json:"records"`
}

// MarshalJSON represents an empty log as an empty array, never null.
func (l Log) MarshalJSON() ([]byte, error) {
	type wire Log
	v := wire(l)
	if v.Records == nil {
		v.Records = []Record{}
	}
	return json.Marshal(v)
}

// Config is the versioned rule document. Parsed documents impose stricter
// presence/type checks than encoding/json alone; use ParseRules when available.
type Config struct {
	SchemaVersion int               `json:"schema_version"`
	BootStart     BootStartConfig   `json:"boot_start"`
	Milestones    []MilestoneConfig `json:"milestones"`
	Forbidden     []ForbiddenConfig `json:"forbidden"`
}

type BootStartConfig struct {
	Pattern string `json:"pattern"`
}

type MilestoneConfig struct {
	ID       string `json:"id"`
	Pattern  string `json:"pattern"`
	WithinMS *int64 `json:"within_ms,omitempty"`
}

type ForbiddenConfig struct {
	ID      string `json:"id"`
	Pattern string `json:"pattern"`
}

// MarshalJSON writes absent optional rule lists as [], and omits a nil deadline
// because an explicit null deadline is not valid rule-document syntax.
func (c Config) MarshalJSON() ([]byte, error) {
	type wire Config
	v := wire(c)
	if v.Milestones == nil {
		v.Milestones = []MilestoneConfig{}
	}
	if v.Forbidden == nil {
		v.Forbidden = []ForbiddenConfig{}
	}
	return json.Marshal(v)
}

type CheckKind string

const (
	KindStart     CheckKind = "start"
	KindMilestone CheckKind = "milestone"
	KindForbidden CheckKind = "forbidden"
)

type Status string

const (
	StatusPassed       Status = "passed"
	StatusFailed       Status = "failed"
	StatusNotEvaluated Status = "not_evaluated"
)

type Reason string

const (
	ReasonMatched        Reason = "matched"
	ReasonAbsent         Reason = "absent"
	ReasonMissing        Reason = "missing"
	ReasonNoBootStart    Reason = "no_boot_start"
	ReasonUntimed        Reason = "untimed"
	ReasonLate           Reason = "late"
	ReasonForbiddenMatch Reason = "forbidden_match"
)

// Evidence is the first matching record, not a copy of the full input log.
type Evidence struct {
	Line        int64  `json:"line"`
	TimestampNS *int64 `json:"timestamp_ns"`
	Message     string `json:"message"`
}

// CheckResult describes one configured check. All nullable report fields are
// emitted explicitly as null. WithinMS remains milliseconds; times are ns.
type CheckResult struct {
	ID         string    `json:"id"`
	Kind       CheckKind `json:"kind"`
	Status     Status    `json:"status"`
	Reason     Reason    `json:"reason"`
	WithinMS   *int64    `json:"within_ms"`
	ElapsedNS  *int64    `json:"elapsed_ns"`
	MatchCount int64     `json:"match_count"`
	Evidence   *Evidence `json:"evidence"`
}

type Summary struct {
	Total        int `json:"total"`
	Passed       int `json:"passed"`
	Failed       int `json:"failed"`
	NotEvaluated int `json:"not_evaluated"`
}

// Report is the complete schema-version-1 result. Checks are ordered start,
// milestones in configuration order, then forbidden rules in configuration
// order. A valid report passes exactly when Summary.Failed == 0.
type Report struct {
	SchemaVersion int           `json:"schema_version"`
	TimingBasis   string        `json:"timing_basis"`
	Origin        *Evidence     `json:"origin"`
	Checks        []CheckResult `json:"checks"`
	Summary       Summary       `json:"summary"`
	Scope         string        `json:"scope"`
}

// MarshalJSON preserves the schema's [] representation even for an empty
// caller-built model. Call Validate before rendering; an empty report is invalid.
func (r Report) MarshalJSON() ([]byte, error) {
	type wire Report
	v := wire(r)
	if v.Checks == nil {
		v.Checks = []CheckResult{}
	}
	return json.Marshal(v)
}

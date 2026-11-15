package bootlog_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/firmdura/bootlog-check"
	"github.com/firmdura/bootlog-check/internal/testfixture"
)

func TestReportSchemaGolden(t *testing.T) {
	cases := []struct {
		name   string
		report bootlog.Report
		ids    []string
	}{
		{"passing", testfixture.PassingReport(), []string{"boot_start", "network_ready", "application_ready", "kernel_panic"}},
		{"failing", testfixture.FailingReport(), []string{"boot_start", "network_ready", "application_ready", "serial_ready", "kernel_panic", "fault"}},
		{"missing-origin", testfixture.MissingOriginReport(), []string{"boot_start", "network_ready", "application_ready", "kernel_panic", "watchdog"}},
	}
	statuses := map[string]bool{}
	reasons := map[string]bool{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.report.Validate(); err != nil {
				t.Fatalf("fixture report is invalid: %v", err)
			}
			golden, err := os.ReadFile("testdata/reports/" + tc.name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var compact bytes.Buffer
			if err := json.Compact(&compact, golden); err != nil {
				t.Fatalf("invalid independent golden JSON: %v", err)
			}
			got, err := json.Marshal(tc.report)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, compact.Bytes()) {
				t.Fatalf("schema mismatch\ngot:  %s\nwant: %s", got, compact.Bytes())
			}
			var decoded bootlog.Report
			if err := json.Unmarshal(golden, &decoded); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(decoded, tc.report) {
				t.Fatalf("golden model mismatch\ngot: %#v\nwant: %#v", decoded, tc.report)
			}
			if err := decoded.Validate(); err != nil {
				t.Fatalf("golden report invalid: %v", err)
			}
			ids := make([]string, len(decoded.Checks))
			counts := bootlog.Summary{Total: len(decoded.Checks)}
			for i, check := range decoded.Checks {
				ids[i] = check.ID
				statuses[string(check.Status)] = true
				reasons[string(check.Reason)] = true
				switch string(check.Status) {
				case "passed":
					counts.Passed++
				case "failed":
					counts.Failed++
				case "not_evaluated":
					counts.NotEvaluated++
				default:
					t.Fatalf("unknown status %q", check.Status)
				}
			}
			if !reflect.DeepEqual(ids, tc.ids) {
				t.Fatalf("check order = %v, want %v", ids, tc.ids)
			}
			if counts != decoded.Summary {
				t.Fatalf("summary = %+v, independently counted %+v", decoded.Summary, counts)
			}
		})
	}
	if want := map[string]bool{"passed": true, "failed": true, "not_evaluated": true}; !reflect.DeepEqual(statuses, want) {
		t.Fatalf("goldens cover statuses %v, want %v", statuses, want)
	}
	if want := map[string]bool{"matched": true, "absent": true, "missing": true, "no_boot_start": true, "untimed": true, "late": true, "forbidden_match": true}; !reflect.DeepEqual(reasons, want) {
		t.Fatalf("goldens cover reasons %v, want %v", reasons, want)
	}
}

func TestFrozenSchemaEnums(t *testing.T) {
	if bootlog.SchemaVersion != 1 || bootlog.BootStartID != "boot_start" || bootlog.TimingBasis != "boot_start" {
		t.Fatal("schema version or origin identity changed")
	}
	for _, tc := range []struct{ got, want string }{
		{string(bootlog.KindStart), "start"}, {string(bootlog.KindMilestone), "milestone"}, {string(bootlog.KindForbidden), "forbidden"},
		{string(bootlog.StatusPassed), "passed"}, {string(bootlog.StatusFailed), "failed"}, {string(bootlog.StatusNotEvaluated), "not_evaluated"},
		{string(bootlog.ReasonMatched), "matched"}, {string(bootlog.ReasonAbsent), "absent"}, {string(bootlog.ReasonMissing), "missing"},
		{string(bootlog.ReasonNoBootStart), "no_boot_start"}, {string(bootlog.ReasonUntimed), "untimed"}, {string(bootlog.ReasonLate), "late"},
		{string(bootlog.ReasonForbiddenMatch), "forbidden_match"},
		{bootlog.CaptureScope, "Results describe only the supplied capture; a pass does not prove capture completeness or device health."},
	} {
		if tc.got != tc.want {
			t.Errorf("enum = %q, want %q", tc.got, tc.want)
		}
	}
}

func TestModelJSONAbsence(t *testing.T) {
	cases := []struct {
		name  string
		model any
		want  string
	}{
		{"nil records", bootlog.Log{}, `{"records":[]}`},
		{"empty records", bootlog.Log{Records: []bootlog.Record{}}, `{"records":[]}`},
		{"untimed record", testfixture.Untimed(7, "message"), `{"line":7,"timestamp_ns":null,"message":"message"}`},
		{"zero timestamp", testfixture.Timed(7, 0, "message"), `{"line":7,"timestamp_ns":0,"message":"message"}`},
		{"untimed evidence", bootlog.Evidence{Line: 7, Message: "message"}, `{"line":7,"timestamp_ns":null,"message":"message"}`},
		{"empty optional config lists", bootlog.Config{SchemaVersion: 1, BootStart: bootlog.BootStartConfig{Pattern: "start"}}, `{"schema_version":1,"boot_start":{"pattern":"start"},"milestones":[],"forbidden":[]}`},
		{"nil config deadline omitted", bootlog.MilestoneConfig{ID: "ready", Pattern: "ready"}, `{"id":"ready","pattern":"ready"}`},
		{"zero config deadline retained", bootlog.MilestoneConfig{ID: "ready", Pattern: "ready", WithinMS: testfixture.Int64(0)}, `{"id":"ready","pattern":"ready","within_ms":0}`},
		{"absent report fields explicit", bootlog.CheckResult{ID: "missing", Kind: bootlog.KindMilestone, Status: bootlog.StatusFailed, Reason: bootlog.ReasonMissing}, `{"id":"missing","kind":"milestone","status":"failed","reason":"missing","within_ms":null,"elapsed_ns":null,"match_count":0,"evidence":null}`},
		{"nil report checks", bootlog.Report{}, `{"schema_version":0,"timing_basis":"","origin":null,"checks":[],"summary":{"total":0,"passed":0,"failed":0,"not_evaluated":0},"scope":""}`},
		{"empty report checks", bootlog.Report{Checks: []bootlog.CheckResult{}}, `{"schema_version":0,"timing_basis":"","origin":null,"checks":[],"summary":{"total":0,"passed":0,"failed":0,"not_evaluated":0},"scope":""}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.model)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %s\nwant %s", got, tc.want)
			}
		})
	}
}

func TestReportIntegerUnitsRemainExact(t *testing.T) {
	// MaxWithinMS is intentionally written as an independent literal here.
	if bootlog.MaxWithinMS != 9_223_372_036_854 || bootlog.NanosecondsPerMS != 1_000_000 {
		t.Fatal("deadline unit constants changed")
	}
	r := bootlog.Report{
		SchemaVersion: 1, TimingBasis: "boot_start",
		Origin: &bootlog.Evidence{Line: 1, TimestampNS: testfixture.Int64(0), Message: "start"},
		Checks: []bootlog.CheckResult{
			{ID: "boot_start", Kind: bootlog.KindStart, Status: bootlog.StatusPassed, Reason: bootlog.ReasonMatched, ElapsedNS: testfixture.Int64(0), MatchCount: 1, Evidence: &bootlog.Evidence{Line: 1, TimestampNS: testfixture.Int64(0), Message: "start"}},
			{ID: "deadline", Kind: bootlog.KindMilestone, Status: bootlog.StatusPassed, Reason: bootlog.ReasonMatched, WithinMS: testfixture.Int64(9_223_372_036_854), ElapsedNS: testfixture.Int64(9_223_372_036_854_000_000), MatchCount: 1, Evidence: &bootlog.Evidence{Line: 2, TimestampNS: testfixture.Int64(9_223_372_036_854_000_000), Message: "deadline"}},
			{ID: "presence", Kind: bootlog.KindMilestone, Status: bootlog.StatusPassed, Reason: bootlog.ReasonMatched, ElapsedNS: testfixture.Int64(math.MaxInt64), MatchCount: 1, Evidence: &bootlog.Evidence{Line: 3, TimestampNS: testfixture.Int64(math.MaxInt64), Message: "presence"}},
		},
		Summary: bootlog.Summary{Total: 3, Passed: 3}, Scope: bootlog.CaptureScope,
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, literal := range []string{`"within_ms":9223372036854`, `"elapsed_ns":9223372036854000000`, `"timestamp_ns":9223372036854775807`} {
		if !bytes.Contains(b, []byte(literal)) {
			t.Fatalf("missing exact integer %s in %s", literal, b)
		}
	}
	var roundTrip bootlog.Report
	if err := json.Unmarshal(b, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(roundTrip, r) {
		t.Fatal("integer values changed during JSON round trip")
	}

	// Nonnegative timestamps can produce the full supported negative difference.
	r.Origin = &bootlog.Evidence{Line: 3, TimestampNS: testfixture.Int64(math.MaxInt64), Message: "start"}
	r.Checks = []bootlog.CheckResult{
		{ID: "boot_start", Kind: bootlog.KindStart, Status: bootlog.StatusPassed, Reason: bootlog.ReasonMatched, ElapsedNS: testfixture.Int64(0), MatchCount: 1, Evidence: &bootlog.Evidence{Line: 3, TimestampNS: testfixture.Int64(math.MaxInt64), Message: "start"}},
		{ID: "preamble", Kind: bootlog.KindForbidden, Status: bootlog.StatusFailed, Reason: bootlog.ReasonForbiddenMatch, ElapsedNS: testfixture.Int64(-math.MaxInt64), MatchCount: 1, Evidence: &bootlog.Evidence{Line: 1, TimestampNS: testfixture.Int64(0), Message: "forbidden"}},
	}
	r.Summary = bootlog.Summary{Total: 2, Passed: 1, Failed: 1}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	b, err = json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"elapsed_ns":-9223372036854775807`)) {
		t.Fatalf("negative elapsed rounded: %s", b)
	}
}

func TestIndependentFixtureInputs(t *testing.T) {
	var decoded bootlog.Config
	if err := json.Unmarshal([]byte(testfixture.RulesJSON()), &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, testfixture.Config()) {
		t.Fatal("independent document and config disagree")
	}
	if err := decoded.Validate(); err != nil {
		t.Fatal(err)
	}
	log := testfixture.Log()
	if err := log.Validate(); err != nil {
		t.Fatal(err)
	}
	if want := []int64{1, 3, 4, 5, 6}; !reflect.DeepEqual([]int64{log.Records[0].Line, log.Records[1].Line, log.Records[2].Line, log.Records[3].Line, log.Records[4].Line}, want) {
		t.Fatal("physical line numbers changed")
	}
	if want := "[ OK ] serial\n\n[1.000000000] Linux version test\n[4] NETWORK_READY\nAPP_READY\n[4.000000000] NETWORK_READY\n"; testfixture.LogText() != want {
		t.Fatal("independent log input changed")
	}
	if strings.Count(testfixture.LogText(), "\n") != 6 {
		t.Fatal("physical line count changed")
	}

	// Mutation of one helper result must never change later calls.
	log.Records[0].Message = "changed"
	*log.Records[1].TimestampNS = 123
	config := testfixture.Config()
	config.Milestones[0].ID = "changed"
	*config.Milestones[0].WithinMS = 123
	report := testfixture.PassingReport()
	report.Checks[0].Evidence.Message = "changed"
	*report.Origin.TimestampNS = 123
	*report.Checks[1].WithinMS = 123
	if testfixture.Log().Records[0].Message != "[ OK ] serial" || *testfixture.Log().Records[1].TimestampNS != 1_000_000_000 {
		t.Fatal("log fixture shares mutable state")
	}
	if testfixture.Config().Milestones[0].ID != "network_ready" || *testfixture.Config().Milestones[0].WithinMS != 3000 {
		t.Fatal("config fixture shares mutable state")
	}
	fresh := testfixture.PassingReport()
	if fresh.Checks[0].Evidence.Message != "Linux version test" || *fresh.Origin.TimestampNS != 1_000_000_000 || *fresh.Checks[1].WithinMS != 3000 {
		t.Fatal("report fixture shares mutable state")
	}
	for _, fixture := range []func() bootlog.Report{testfixture.PassingReport, testfixture.FailingReport, testfixture.MissingOriginReport} {
		if !reflect.DeepEqual(fixture(), fixture()) {
			t.Fatal("report fixture is nondeterministic")
		}
	}
}

func ExampleRecord() {
	zero := int64(0)
	for _, record := range []bootlog.Record{
		{Line: 1, Message: "preamble"},
		{Line: 2, TimestampNS: &zero, Message: "ready"},
	} {
		encoded, err := json.Marshal(record)
		if err != nil {
			panic(err)
		}
		fmt.Println(string(encoded))
	}
	// Output:
	// {"line":1,"timestamp_ns":null,"message":"preamble"}
	// {"line":2,"timestamp_ns":0,"message":"ready"}
}

func ExampleConfig() {
	config := bootlog.Config{
		SchemaVersion: 1,
		BootStart:     bootlog.BootStartConfig{Pattern: "^START$"},
		Milestones:    []bootlog.MilestoneConfig{{ID: "ready", Pattern: "^READY$"}},
	}
	if err := config.Validate(); err != nil {
		panic(err)
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(encoded))
	// Output:
	// {"schema_version":1,"boot_start":{"pattern":"^START$"},"milestones":[{"id":"ready","pattern":"^READY$"}],"forbidden":[]}
}

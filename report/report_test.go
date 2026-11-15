package report_test

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode"

	"github.com/firmdura/bootlog-check"
	"github.com/firmdura/bootlog-check/report"
)

var formats = []report.Format{report.Text, report.JSON, report.JUnit}

func ptr(n int64) *int64 { return &n }

func witness(line int64, timestamp *int64, message string) *bootlog.Evidence {
	return &bootlog.Evidence{Line: line, TimestampNS: timestamp, Message: message}
}

// These expected reports are authored without the evaluator or renderer. The
// golden files are separate literal expectations, never updated by these tests.
func passing() bootlog.Report {
	return bootlog.Report{
		SchemaVersion: 1, TimingBasis: "boot_start", Scope: bootlog.CaptureScope,
		Origin: witness(2, ptr(1_000_000), "BOOT"),
		Checks: []bootlog.CheckResult{
			{ID: "boot_start", Kind: bootlog.KindStart, Status: bootlog.StatusPassed, Reason: bootlog.ReasonMatched, ElapsedNS: ptr(0), MatchCount: 1, Evidence: witness(2, ptr(1_000_000), "BOOT")},
			{ID: "ready", Kind: bootlog.KindMilestone, Status: bootlog.StatusPassed, Reason: bootlog.ReasonMatched, WithinMS: ptr(1), ElapsedNS: ptr(1_000_000), MatchCount: 2, Evidence: witness(3, ptr(2_000_000), "READY")},
			{ID: "panic", Kind: bootlog.KindForbidden, Status: bootlog.StatusPassed, Reason: bootlog.ReasonAbsent},
		},
		Summary: bootlog.Summary{Total: 3, Passed: 3},
	}
}

func failing() bootlog.Report {
	return bootlog.Report{
		SchemaVersion: 1, TimingBasis: "boot_start", Scope: bootlog.CaptureScope,
		Origin: witness(2, ptr(1_000_000), "BOOT"),
		Checks: []bootlog.CheckResult{
			{ID: "boot_start", Kind: bootlog.KindStart, Status: bootlog.StatusPassed, Reason: bootlog.ReasonMatched, ElapsedNS: ptr(0), MatchCount: 1, Evidence: witness(2, ptr(1_000_000), "BOOT")},
			{ID: "late", Kind: bootlog.KindMilestone, Status: bootlog.StatusFailed, Reason: bootlog.ReasonLate, WithinMS: ptr(2), ElapsedNS: ptr(2_000_001), MatchCount: 1, Evidence: witness(4, ptr(3_000_001), "LATE")},
			{ID: "untimed", Kind: bootlog.KindMilestone, Status: bootlog.StatusFailed, Reason: bootlog.ReasonUntimed, WithinMS: ptr(0), MatchCount: 2, Evidence: witness(3, nil, "UNTIMED")},
			{ID: "missing", Kind: bootlog.KindMilestone, Status: bootlog.StatusFailed, Reason: bootlog.ReasonMissing},
			{ID: "panic", Kind: bootlog.KindForbidden, Status: bootlog.StatusFailed, Reason: bootlog.ReasonForbiddenMatch, ElapsedNS: ptr(-1_000_000), MatchCount: 3, Evidence: witness(1, ptr(0), "PANIC")},
		},
		Summary: bootlog.Summary{Total: 5, Passed: 1, Failed: 4},
	}
}

func missingOrigin() bootlog.Report {
	return bootlog.Report{
		SchemaVersion: 1, TimingBasis: "boot_start", Scope: bootlog.CaptureScope,
		Checks: []bootlog.CheckResult{
			{ID: "boot_start", Kind: bootlog.KindStart, Status: bootlog.StatusFailed, Reason: bootlog.ReasonMissing},
			{ID: "ready", Kind: bootlog.KindMilestone, Status: bootlog.StatusNotEvaluated, Reason: bootlog.ReasonNoBootStart, WithinMS: ptr(0)},
			{ID: "panic", Kind: bootlog.KindForbidden, Status: bootlog.StatusFailed, Reason: bootlog.ReasonForbiddenMatch, MatchCount: 1, Evidence: witness(1, ptr(0), "PANIC")},
			{ID: "safe", Kind: bootlog.KindForbidden, Status: bootlog.StatusPassed, Reason: bootlog.ReasonAbsent},
		},
		Summary: bootlog.Summary{Total: 4, Passed: 1, Failed: 2, NotEvaluated: 1},
	}
}

func render(t *testing.T, r bootlog.Report, format report.Format) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := report.Write(&out, r, format); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestGoldensAndSemantics(t *testing.T) {
	for name, fixture := range map[string]func() bootlog.Report{"pass": passing, "fail": failing, "missing-origin": missingOrigin} {
		t.Run(name, func(t *testing.T) {
			for _, format := range formats {
				t.Run(string(format), func(t *testing.T) {
					r := fixture()
					got := render(t, r, format)
					want, err := os.ReadFile("testdata/" + name + "." + string(format))
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got, want) {
						t.Fatalf("golden mismatch\ngot:\n%s\nwant:\n%s", got, want)
					}
					assertSemantics(t, got, r, format)
					if !bytes.Equal(got, render(t, fixture(), format)) {
						t.Fatal("repeat render changed bytes")
					}
				})
			}
		})
	}
}

func assertSemantics(t *testing.T, data []byte, r bootlog.Report, format report.Format) {
	t.Helper()
	switch format {
	case report.JSON:
		var decoded bootlog.Report
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(decoded, r) {
			t.Fatalf("JSON changed values: %#v", decoded)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		requireFields(t, fields, "schema_version timing_basis origin checks summary scope")
		if r.Origin == nil && string(fields["origin"]) != "null" {
			t.Fatal("missing origin must be explicit null")
		}
		var checks []map[string]json.RawMessage
		var summary map[string]json.RawMessage
		if err := json.Unmarshal(fields["summary"], &summary); err != nil {
			t.Fatal(err)
		}
		requireFields(t, summary, "total passed failed not_evaluated")
		if err := json.Unmarshal(fields["checks"], &checks); err != nil {
			t.Fatal(err)
		}
		for i, c := range checks {
			requireFields(t, c, "id kind status reason within_ms elapsed_ns match_count evidence")
			for key, absent := range map[string]bool{"within_ms": r.Checks[i].WithinMS == nil, "elapsed_ns": r.Checks[i].ElapsedNS == nil, "evidence": r.Checks[i].Evidence == nil} {
				if absent && string(c[key]) != "null" {
					t.Fatalf("check %d %s is not null", i, key)
				}
			}
			if r.Checks[i].Evidence != nil {
				var e map[string]json.RawMessage
				if err := json.Unmarshal(c["evidence"], &e); err != nil {
					t.Fatal(err)
				}
				requireFields(t, e, "line timestamp_ns message")
			}
		}
	case report.JUnit:
		assertJUnit(t, data, r)
	case report.Text:
		headline := "bootlog-check: PASS\n"
		if r.Summary.Failed != 0 {
			headline = "bootlog-check: FAIL\n"
		}
		if !bytes.HasPrefix(data, []byte(headline)) {
			t.Fatalf("wrong result headline: %s", data)
		}
		assertOverview(t, string(data), r)
		for _, c := range r.Checks {
			if !bytes.Contains(data, []byte("["+string(c.Status)+"] "+c.ID+" ("+string(c.Kind)+")")) {
				t.Errorf("missing check %s", c.ID)
			}
			assertCheckDetails(t, string(data), c)
		}
	}
}

func assertOverview(t *testing.T, output string, r bootlog.Report) {
	t.Helper()
	for _, want := range []string{
		r.Scope, "timing_basis: boot_start", "schema_version: 1",
		fmt.Sprintf("total=%d passed=%d failed=%d not_evaluated=%d", r.Summary.Total, r.Summary.Passed, r.Summary.Failed, r.Summary.NotEvaluated),
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing overview value %q", want)
		}
	}
	if r.Origin == nil {
		if !strings.Contains(output, "origin: none") {
			t.Fatal("missing null origin")
		}
	} else if !strings.Contains(output, fmt.Sprintf("origin: line=%d timestamp_ns=%d message=%s", r.Origin.Line, *r.Origin.TimestampNS, strconv.Quote(r.Origin.Message))) {
		t.Fatal("origin lost values")
	}
}

func assertCheckDetails(t *testing.T, output string, c bootlog.CheckResult) {
	t.Helper()
	for key, value := range map[string]*int64{"within_ms": c.WithinMS, "elapsed_ns": c.ElapsedNS} {
		want := "none"
		if value != nil {
			want = strconv.FormatInt(*value, 10)
		}
		if !strings.Contains(output, key+"="+want+" ") {
			t.Fatalf("missing %s=%s", key, want)
		}
	}
	if c.Evidence != nil {
		want := "none"
		if c.Evidence.TimestampNS != nil {
			want = strconv.FormatInt(*c.Evidence.TimestampNS, 10)
		}
		if !strings.Contains(output, fmt.Sprintf("evidence: line=%d timestamp_ns=%s message=%s", c.Evidence.Line, want, strconv.Quote(c.Evidence.Message))) {
			t.Fatal("evidence lost values")
		}
	} else if !strings.Contains(output, "evidence: none") {
		t.Fatal("missing absent evidence")
	}
}

func requireFields(t *testing.T, fields map[string]json.RawMessage, names string) {
	t.Helper()
	keys := strings.Fields(names)
	if len(fields) != len(keys) {
		t.Fatalf("unexpected fields: %v", fields)
	}
	for _, key := range keys {
		if _, ok := fields[key]; !ok {
			t.Fatalf("missing field %s", key)
		}
	}
}

func assertJUnit(t *testing.T, data []byte, r bootlog.Report) {
	t.Helper()
	var suite struct {
		XMLName  xml.Name
		Name     string `xml:"name,attr"`
		Tests    int    `xml:"tests,attr"`
		Failures int    `xml:"failures,attr"`
		Skipped  int    `xml:"skipped,attr"`
		Errors   int    `xml:"errors,attr"`
		Output   string `xml:"system-out"`
		Cases    []struct {
			Name    string `xml:"name,attr"`
			Class   string `xml:"classname,attr"`
			Output  string `xml:"system-out"`
			Failure *struct {
				Message string `xml:"message,attr"`
				Text    string `xml:",chardata"`
			} `xml:"failure"`
			Skipped *struct {
				Message string `xml:"message,attr"`
				Text    string `xml:",chardata"`
			} `xml:"skipped"`
		} `xml:"testcase"`
	}
	if err := xml.Unmarshal(data, &suite); err != nil {
		t.Fatal(err)
	}
	if suite.XMLName.Local != "testsuite" || suite.Name != "bootlog-check" || suite.Tests != len(r.Checks) || suite.Failures != r.Summary.Failed || suite.Skipped != r.Summary.NotEvaluated || suite.Errors != 0 || len(suite.Cases) != len(r.Checks) {
		t.Fatalf("incorrect JUnit suite: %+v", suite)
	}
	assertOverview(t, suite.Output, r)
	failures, skipped := 0, 0
	for i, c := range suite.Cases {
		want := r.Checks[i]
		assertCheckDetails(t, c.Output, want)
		if c.Name != want.ID || c.Class != "bootlog."+string(want.Kind) || !strings.Contains(c.Output, "reason="+string(want.Reason)) || !strings.Contains(c.Output, fmt.Sprintf("match_count=%d", want.MatchCount)) {
			t.Fatalf("wrong testcase %d: %+v", i, c)
		}
		if (c.Failure != nil) != (want.Status == bootlog.StatusFailed) || (c.Skipped != nil) != (want.Status == bootlog.StatusNotEvaluated) {
			t.Fatalf("wrong status elements for %s", want.ID)
		}
		if c.Failure != nil {
			failures++
			if c.Failure.Message != string(want.Reason) || c.Failure.Text != c.Output {
				t.Fatal("failure lost details")
			}
		}
		if c.Skipped != nil {
			skipped++
			if c.Skipped.Message != string(want.Reason) || c.Skipped.Text != c.Output {
				t.Fatal("skipped lost details")
			}
		}
		if want.Evidence != nil {
			if !strings.Contains(c.Output, "message="+strconv.Quote(want.Evidence.Message)) || !strings.Contains(c.Output, fmt.Sprintf("line=%d ", want.Evidence.Line)) {
				t.Fatal("evidence lost")
			}
		} else if !strings.Contains(c.Output, "evidence: none") {
			t.Fatal("missing absent evidence")
		}
	}
	if failures != suite.Failures || skipped != suite.Skipped {
		t.Fatal("counts disagree with children")
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if start, ok := token.(xml.StartElement); ok {
			for _, a := range start.Attr {
				if a.Name.Local == "time" || a.Name.Local == "timestamp" {
					t.Fatal("misleading timing attribute")
				}
			}
		}
	}
}

func TestControlsAndUnicode(t *testing.T) {
	message := "<>&\"' \\literal\\n\n\r\t\x1b[31m\x00\x01\x7f\u0085\u009b\u202e\u2028\ufffe\uffff 雪🙂"
	r := passing()
	r.Origin.Message = message
	r.Checks[0].Evidence.Message = message
	for _, f := range formats {
		t.Run(string(f), func(t *testing.T) {
			data := render(t, r, f)
			assertSemantics(t, data, r, f)
			if f != report.JSON {
				for _, ch := range string(data) {
					if ch != '\n' && (unicode.IsControl(ch) || ch == '\u202e' || ch == '\u2028' || ch == '\ufffe' || ch == '\uffff') {
						t.Fatalf("raw unsafe character %U", ch)
					}
				}
				for _, escape := range []string{`\\literal\\n`, `\n\r\t\x1b`, `\x00\x01\x7f`, `\u0085\u009b`, `\u202e\u2028\ufffe\uffff`, "雪🙂"} {
					if !bytes.Contains(data, []byte(escape)) {
						t.Errorf("missing printable %s in %s", escape, data)
					}
				}
			}
		})
	}
	if r.Origin.Message != message || r.Checks[0].Evidence.Message != message {
		t.Fatal("message mutated")
	}
}

func TestIntegerExtremesAndPresenceOnly(t *testing.T) {
	r := passing()
	r.Origin.TimestampNS = ptr(0)
	r.Checks[0].Evidence.TimestampNS = ptr(0)
	r.Checks[1].WithinMS = ptr(bootlog.MaxWithinMS)
	r.Checks[1].Evidence.TimestampNS = ptr(bootlog.MaxWithinMS * 1_000_000)
	r.Checks[1].ElapsedNS = ptr(bootlog.MaxWithinMS * 1_000_000)
	r.Checks[1].MatchCount = math.MaxInt64
	r.Checks[1].Evidence.Line = math.MaxInt64
	for _, f := range formats {
		assertSemantics(t, render(t, r, f), r, f)
	}
	r.Checks[1].WithinMS = nil
	r.Checks[1].Evidence.TimestampNS = ptr(math.MaxInt64)
	r.Checks[1].ElapsedNS = ptr(math.MaxInt64)
	for _, f := range formats {
		assertSemantics(t, render(t, r, f), r, f)
	}
	r.Checks[1].Evidence.TimestampNS = nil
	r.Checks[1].ElapsedNS = nil
	for _, f := range formats {
		assertSemantics(t, render(t, r, f), r, f)
	}
}

func TestMaximumChecksAndSharedWitness(t *testing.T) {
	r := passing()
	r.Checks = r.Checks[:1]
	for i := 0; i < 128; i++ {
		r.Checks = append(r.Checks, bootlog.CheckResult{ID: fmt.Sprintf("ready%d", i), Kind: bootlog.KindMilestone, Status: bootlog.StatusPassed, Reason: bootlog.ReasonMatched, WithinMS: ptr(0), ElapsedNS: ptr(0), MatchCount: 1, Evidence: r.Origin})
	}
	r.Summary = bootlog.Summary{Total: 129, Passed: 129}
	for _, f := range formats {
		assertSemantics(t, render(t, r, f), r, f)
	}
}

type observingWriter struct {
	calls  int
	closed bool
}

func (w *observingWriter) Write(p []byte) (int, error) { w.calls++; return len(p), nil }
func (w *observingWriter) Close() error                { w.closed = true; return nil }

func TestInvalidBeforeWriting(t *testing.T) {
	mutations := map[string]func(*bootlog.Report){
		"schema":        func(r *bootlog.Report) { r.SchemaVersion = 2 },
		"scope":         func(r *bootlog.Report) { r.Scope = "other" },
		"counts":        func(r *bootlog.Report) { r.Summary.Total++ },
		"status":        func(r *bootlog.Report) { r.Checks[1].Status = bootlog.StatusFailed },
		"reason":        func(r *bootlog.Report) { r.Checks[1].Reason = bootlog.ReasonMissing },
		"utf8":          func(r *bootlog.Report) { r.Checks[1].Evidence.Message = "\xff" },
		"origin":        func(r *bootlog.Report) { r.Origin = nil },
		"duplicate":     func(r *bootlog.Report) { r.Checks[1].ID = "boot_start" },
		"order":         func(r *bootlog.Report) { r.Checks[1], r.Checks[2] = r.Checks[2], r.Checks[1] },
		"line conflict": func(r *bootlog.Report) { r.Checks[1].Evidence.Line = 2 },
		"elapsed":       func(r *bootlog.Report) { r.Checks[1].ElapsedNS = nil },
		"empty":         func(r *bootlog.Report) { *r = bootlog.Report{} },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			for _, f := range formats {
				r := passing()
				mutate(&r)
				w := &observingWriter{}
				err := report.Write(w, r, f)
				var invalid *bootlog.InvalidReportError
				if !errors.Is(err, bootlog.ErrInvalidReport) || !errors.As(err, &invalid) || w.calls != 0 || w.closed {
					t.Fatalf("format=%s error=%v writer=%+v", f, err, w)
				}
			}
		})
	}
	for _, f := range []report.Format{"", "yaml", "TEXT"} {
		w := &observingWriter{}
		if report.Write(w, passing(), f) == nil || w.calls != 0 {
			t.Fatal("invalid format emitted bytes")
		}
	}
	for _, f := range formats {
		if report.Write(nil, passing(), f) == nil {
			t.Fatal("nil writer accepted")
		}
		w := &observingWriter{}
		if err := report.Write(w, passing(), f); err != nil || w.closed || w.calls == 0 {
			t.Fatalf("writer ownership: %v %+v", err, w)
		}
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestWriteFailures(t *testing.T) {
	sentinel := errors.New("destination failed")
	for _, f := range formats {
		for _, tc := range []struct {
			name   string
			writer writerFunc
			want   error
		}{
			{"short", func(p []byte) (int, error) { return len(p) - 1, nil }, io.ErrShortWrite},
			{"zero", func([]byte) (int, error) { return 0, nil }, io.ErrShortWrite},
			{"failed", func([]byte) (int, error) { return 0, sentinel }, sentinel},
			{"partial failed", func(p []byte) (int, error) { return len(p) / 2, sentinel }, sentinel},
			{"full failed", func(p []byte) (int, error) { return len(p), sentinel }, sentinel},
		} {
			t.Run(string(f)+"/"+tc.name, func(t *testing.T) {
				if err := report.Write(tc.writer, passing(), f); !errors.Is(err, tc.want) {
					t.Fatalf("got %v, want %v", err, tc.want)
				}
			})
		}
	}
}

func TestConcurrentDeterministicNoMutation(t *testing.T) {
	r := failing()
	before, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, f := range formats {
		want := render(t, r, f)
		for i := 0; i < 10; i++ {
			wg.Go(func() {
				if got := render(t, r, f); !bytes.Equal(got, want) {
					t.Error("concurrent output changed")
				}
			})
		}
	}
	wg.Wait()
	after, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("input mutated")
	}
}

func TestLFCRLFEquivalentReports(t *testing.T) {
	// Independently written captures and model expectation. No host information
	// or terminator representation enters any rendered format.
	lf := "preamble\n[0.001] BOOT\n[0.002] READY\n[0.003] READY\n"
	crlf := "preamble\r\n[0.001] BOOT\r\n[0.002] READY\r\n[0.003] READY\r\n"
	rules, err := bootlog.CompileRules(bootlog.Config{SchemaVersion: 1, BootStart: bootlog.BootStartConfig{Pattern: "^BOOT$"}, Milestones: []bootlog.MilestoneConfig{{ID: "ready", Pattern: "^READY$", WithinMS: ptr(1)}}, Forbidden: []bootlog.ForbiddenConfig{{ID: "panic", Pattern: "PANIC"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{lf, crlf} {
		log, err := bootlog.ParseLog(context.Background(), strings.NewReader(input), bootlog.ParseOptions{})
		if err != nil {
			t.Fatal(err)
		}
		r, err := bootlog.Evaluate(log, rules)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(r, passing()) {
			t.Fatalf("unexpected evaluation: %+v", r)
		}
		for _, f := range formats {
			if !bytes.Equal(render(t, r, f), render(t, passing(), f)) {
				t.Fatal("logical equivalent output changed")
			}
		}
	}
}

// Package replayproof checks the synthetic replay oracle without importing any
// production parser, evaluator, model, or renderer. Both the demo and tests use it.
package replayproof

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
)

// Case is one hand-authored expectation from examples/replay/expected.json.
type Case struct {
	Name      string          `json:"name"`
	Exit      int             `json:"exit"`
	ErrorLine int64           `json:"error_line,omitempty"`
	Report    json.RawMessage `json:"report"`
}

type evidence struct {
	Line        int64  `json:"line"`
	TimestampNS *int64 `json:"timestamp_ns"`
	Message     string `json:"message"`
}

type check struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	Status     string    `json:"status"`
	Reason     string    `json:"reason"`
	WithinMS   *int64    `json:"within_ms"`
	ElapsedNS  *int64    `json:"elapsed_ns"`
	MatchCount int64     `json:"match_count"`
	Evidence   *evidence `json:"evidence"`
}

type expectedReport struct {
	SchemaVersion int       `json:"schema_version"`
	TimingBasis   string    `json:"timing_basis"`
	Origin        *evidence `json:"origin"`
	Checks        []check   `json:"checks"`
	Summary       struct {
		Total        int `json:"total"`
		Passed       int `json:"passed"`
		Failed       int `json:"failed"`
		NotEvaluated int `json:"not_evaluated"`
	} `json:"summary"`
	Scope string `json:"scope"`
}

func Load(path string) ([]Case, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []Case
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cases); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("oracle has trailing content: %v", err)
	}
	if len(cases) != 7 {
		return nil, fmt.Errorf("oracle has %d scenarios; want 7", len(cases))
	}
	return cases, nil
}

// Verify requires a complete report matching the independent oracle. Exit-2
// cases instead require zero report bytes. The caller checks exit and stderr.
func Verify(c Case, format string, data []byte) error {
	if c.Exit == 2 {
		if len(data) != 0 {
			return fmt.Errorf("invalid input emitted report bytes: %q", data)
		}
		return nil
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		return fmt.Errorf("report is empty or lacks its final newline")
	}
	var expected expectedReport
	if err := json.Unmarshal(c.Report, &expected); err != nil {
		return fmt.Errorf("decode oracle: %w", err)
	}
	switch format {
	case "json":
		got, err := document(data)
		if err != nil {
			return fmt.Errorf("decode JSON report: %w", err)
		}
		want, err := document(c.Report)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(got, want) {
			return fmt.Errorf("JSON differs from hand-authored oracle\ngot: %s\nwant: %s", data, c.Report)
		}
		return nil
	case "text":
		var want strings.Builder
		want.WriteString(overview(expected))
		for _, check := range expected.Checks {
			want.WriteString(details(check))
		}
		fmt.Fprintf(&want, "scope: %s\n", expected.Scope)
		if string(data) != want.String() {
			return fmt.Errorf("text differs from expected fields\ngot: %s\nwant: %s", data, want.String())
		}
		return nil
	case "junit":
		return verifyJUnit(data, expected)
	default:
		return fmt.Errorf("unknown format %q", format)
	}
}

// UseNumber preserves exact integers and comparison also catches absent/null
// fields, extra keys, wrong types, or extra documents in JSON output.
func document(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON content: %v", err)
	}
	return value, nil
}

type issue struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Text    string `xml:",chardata"`
}

type testcase struct {
	Name      string     `xml:"name,attr"`
	Classname string     `xml:"classname,attr"`
	Attrs     []xml.Attr `xml:",any,attr"`
	Failures  []issue    `xml:"failure"`
	Skipped   []issue    `xml:"skipped"`
	Errors    []issue    `xml:"error"`
	Output    []string   `xml:"system-out"`
}

func verifyJUnit(data []byte, want expectedReport) error {
	var suite struct {
		XMLName  xml.Name   `xml:"testsuite"`
		Name     string     `xml:"name,attr"`
		Tests    int        `xml:"tests,attr"`
		Failures int        `xml:"failures,attr"`
		Skipped  int        `xml:"skipped,attr"`
		Errors   int        `xml:"errors,attr"`
		Attrs    []xml.Attr `xml:",any,attr"`
		Cases    []testcase `xml:"testcase"`
		Output   []string   `xml:"system-out"`
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&suite); err != nil {
		return fmt.Errorf("decode JUnit: %w", err)
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if space, ok := token.(xml.CharData); !ok || strings.TrimSpace(string(space)) != "" {
			return fmt.Errorf("trailing XML content")
		}
	}
	if suite.Name != "bootlog-check" || suite.Tests != want.Summary.Total || suite.Failures != want.Summary.Failed || suite.Skipped != want.Summary.NotEvaluated || suite.Errors != 0 || len(suite.Cases) != want.Summary.Total {
		return fmt.Errorf("JUnit suite counts/name differ from oracle: %+v", suite)
	}
	if len(suite.Attrs) != 0 || !reflect.DeepEqual(suite.Output, []string{overview(want) + "scope: " + want.Scope + "\n"}) {
		return fmt.Errorf("JUnit suite has unexpected attributes or incomplete overview")
	}
	failures, skipped := 0, 0
	for i, expected := range want.Checks {
		got := suite.Cases[i]
		if got.Name != expected.ID || got.Classname != "bootlog."+expected.Kind || len(got.Attrs) != 0 || len(got.Errors) != 0 || !reflect.DeepEqual(got.Output, []string{details(expected)}) {
			return fmt.Errorf("JUnit testcase %d identity/evidence differs: %+v", i, got)
		}
		wantFailures, wantSkipped := 0, 0
		if expected.Status == "failed" {
			wantFailures = 1
		}
		if expected.Status == "not_evaluated" {
			wantSkipped = 1
		}
		if len(got.Failures) != wantFailures || len(got.Skipped) != wantSkipped {
			return fmt.Errorf("JUnit testcase %s failure/skipped mapping differs", expected.ID)
		}
		for _, failure := range got.Failures {
			if failure.Message != expected.Reason || failure.Type != expected.Reason || failure.Text != details(expected) {
				return fmt.Errorf("JUnit testcase %s failure reason/evidence differs", expected.ID)
			}
		}
		for _, skip := range got.Skipped {
			if skip.Message != expected.Reason || skip.Text != details(expected) {
				return fmt.Errorf("JUnit testcase %s skipped reason/evidence differs", expected.ID)
			}
		}
		failures += len(got.Failures)
		skipped += len(got.Skipped)
	}
	if failures != suite.Failures || skipped != suite.Skipped {
		return fmt.Errorf("JUnit counts do not recount actual children")
	}
	return nil
}

func overview(r expectedReport) string {
	result := "PASS"
	if r.Summary.Failed != 0 {
		result = "FAIL"
	}
	return fmt.Sprintf("bootlog-check: %s\nsummary: total=%d passed=%d failed=%d not_evaluated=%d\nschema_version: %d\ntiming_basis: %s (elapsed_ns = witness timestamp_ns - origin timestamp_ns)\norigin: %s\n",
		result, r.Summary.Total, r.Summary.Passed, r.Summary.Failed, r.Summary.NotEvaluated, r.SchemaVersion, r.TimingBasis, evidenceText(r.Origin))
}

func details(c check) string {
	return fmt.Sprintf("[%s] %s (%s): reason=%s within_ms=%s elapsed_ns=%s match_count=%d\n  evidence: %s\n",
		c.Status, c.ID, c.Kind, c.Reason, number(c.WithinMS), number(c.ElapsedNS), c.MatchCount, evidenceText(c.Evidence))
}

func number(n *int64) string {
	if n == nil {
		return "none"
	}
	return strconv.FormatInt(*n, 10)
}

func evidenceText(e *evidence) string {
	if e == nil {
		return "none"
	}
	return fmt.Sprintf("line=%d timestamp_ns=%s message=%s", e.Line, number(e.TimestampNS), strconv.Quote(e.Message))
}

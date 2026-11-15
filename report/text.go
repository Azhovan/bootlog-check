package report

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/firmdura/bootlog-check"
)

func textReport(r bootlog.Report) []byte {
	var out strings.Builder
	writeOverview(&out, r)
	for _, check := range r.Checks {
		out.WriteString(checkDetails(check))
	}
	fmt.Fprintf(&out, "scope: %s\n", r.Scope)
	return []byte(out.String())
}

func writeOverview(out *strings.Builder, r bootlog.Report) {
	result := "PASS"
	if r.Summary.Failed != 0 {
		result = "FAIL"
	}
	fmt.Fprintf(out, "bootlog-check: %s\n", result)
	fmt.Fprintf(out, "summary: total=%d passed=%d failed=%d not_evaluated=%d\n",
		r.Summary.Total, r.Summary.Passed, r.Summary.Failed, r.Summary.NotEvaluated)
	fmt.Fprintf(out, "schema_version: %d\n", r.SchemaVersion)
	fmt.Fprintf(out, "timing_basis: %s (elapsed_ns = witness timestamp_ns - origin timestamp_ns)\n", r.TimingBasis)
	fmt.Fprintf(out, "origin: %s\n", evidenceText(r.Origin))
}

func checkDetails(check bootlog.CheckResult) string {
	return fmt.Sprintf("[%s] %s (%s): reason=%s within_ms=%s elapsed_ns=%s match_count=%d\n  evidence: %s\n",
		check.Status, check.ID, check.Kind, check.Reason, numberText(check.WithinMS),
		numberText(check.ElapsedNS), check.MatchCount, evidenceText(check.Evidence))
}

func numberText(value *int64) string {
	if value == nil {
		return "none"
	}
	return strconv.FormatInt(*value, 10)
}

func evidenceText(e *bootlog.Evidence) string {
	if e == nil {
		return "none"
	}
	// Quote keeps printable Unicode and makes controls, line breaks, backslashes,
	// and nonprinting Unicode explicit. Its output is also safe XML 1.0 text.
	return fmt.Sprintf("line=%d timestamp_ns=%s message=%s", e.Line,
		numberText(e.TimestampNS), strconv.Quote(e.Message))
}

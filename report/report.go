// Package report renders complete saved-log check results as text, JSON, or
// a conservative JUnit XML subset. Messages follow the source log's data policy.
package report

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/firmdura/bootlog-check"
)

// Format selects a deterministic report representation. The zero value is invalid.
type Format string

const (
	Text  Format = "text"
	JSON  Format = "json"
	JUnit Format = "junit"
)

// Write validates result before emitting bytes, then writes a complete report.
// Failed checks are valid report data, not rendering errors. Invalid formats or
// reports emit nothing; downstream write errors can leave partial output.
// Write preserves writer error identity, including on incomplete writes. An
// incomplete write with no accompanying writer error returns io.ErrShortWrite.
// Write never closes w or mutates result. The caller must not concurrently modify
// result or its referenced storage while Write is running.
func Write(w io.Writer, result bootlog.Report, format Format) error {
	if format != Text && format != JSON && format != JUnit {
		return fmt.Errorf("report: unsupported format %q", format)
	}
	if err := result.Validate(); err != nil {
		return err
	}
	if w == nil {
		return fmt.Errorf("report: nil writer")
	}
	var data []byte
	var err error
	switch format {
	case Text:
		data = textReport(result)
	case JSON:
		data, err = json.MarshalIndent(result, "", "  ")
		data = append(data, '\n')
	case JUnit:
		data, err = junitReport(result)
	}
	if err != nil {
		return fmt.Errorf("report: encode %s: %w", format, err)
	}
	n, err := w.Write(data)
	if err != nil {
		return fmt.Errorf("report: write %s: %w", format, err)
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return nil
}

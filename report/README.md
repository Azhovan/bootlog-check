# Writing reports

Import `github.com/firmdura/bootlog-check/report` and choose a format when calling
`report.Write`:

```go
err := report.Write(writer, result, report.Text)
```

Use `report.JSON` or `report.JUnit` in place of `report.Text` for those formats.
The [runnable example](example_test.go) parses a saved log, evaluates it, and
writes a report. The caller is responsible for closing or flushing the writer.

Before writing any bytes, `report.Write` calls `bootlog.Report.Validate`.
An invalid report returns an error that matches `bootlog.ErrInvalidReport` with
`errors.Is`; details are available as `*bootlog.InvalidReportError`. An unsupported format, including an empty format,
or a nil writer interface also returns an error. A valid report containing failed
checks is written successfully.

Report writers do not modify the result or add dates, host paths, processing
times, or capture byte counts. Concurrent calls may share a result if no one is
modifying it. Callers must synchronize access when sharing a destination writer.

## Information in every format

Reports use UTF-8, end with a newline, and are deterministic: the same logical
result produces the same bytes. Checks appear in this order: boot start,
milestones in configuration order, then forbidden rules in configuration order.
Every configured check appears, including milestones that could not be evaluated
because boot start was missing.

All formats include the schema version, summary counts, boot-start origin and
timing basis, reasons, deadlines in integer milliseconds, elapsed times in integer
nanoseconds, match counts, first matching evidence, and a statement explaining
the limits of the result. The origin is the boot-start record, whose timestamp
is the starting point for elapsed-time calculations.

Elapsed times come from the log's clock, not the time taken to run the checker.
Forbidden evidence before boot start can have a negative elapsed time. Missing
timestamps remain absent; they do not become zero.

Reports include the first matching message for each check, its original line
number, and its timestamp if available. They do not include the full raw log.
Because matched messages may contain the same sensitive information as the source
log, handle and share reports under the same data policy.

## Text

A text report begins with `bootlog-check: PASS` or `bootlog-check: FAIL`, followed
by counts, timing information, each check, and the statement of scope. The word
`none` indicates an absent numeric value, evidence record, or origin. An actual
zero is printed as `0`. Status labels are `passed`, `failed`, and `not_evaluated`.

Evidence messages use double-quoted Go string syntax (`strconv.Quote`). Printable
Unicode stays readable. Quotes and literal backslashes are escaped. Line breaks,
carriage returns, tabs, ESC, NUL, other controls, and nonprinting Unicode appear
as printable escapes such as `\n`, `\r`, `\t`, `\x1b`, `\x00`, or `\u0085`.
An actual newline therefore appears as `\n`, while a literal backslash followed
by `n` appears as `\n`. You can recover the original message with
`strconv.Unquote`; these display escapes do not change the text used for matching.

## JSON

JSON follows [schema version 1](../docs/schema-v1.md). All nullable fields appear
explicitly as `null` when absent, and empty model arrays appear as `[]`. A valid
v1 report always has at least two checks.

Reports use standard `encoding/json` escaping and two-space indentation. Decoding
the JSON preserves the original valid Unicode and control characters. Nanosecond
values are signed 64-bit integers; consumers must preserve that precision rather
than converting them through floating-point values. The `schema_version` field
identifies the serialized structure. Unsupported report versions are rejected.

## JUnit XML

JUnit output uses a conservative XML 1.0 subset with one `testsuite` named
`bootlog-check`:

| Element or attribute | Meaning |
| --- | --- |
| `tests`, `failures`, `skipped` | Total, failed, and not-evaluated check counts |
| `errors="0"` | Operational errors do not produce reports |
| `testcase name` | Stable check ID, including `boot_start` |
| `testcase classname` | `bootlog.start`, `bootlog.milestone`, or `bootlog.forbidden` |
| `failure` | One per failed check, with the reason in `message` and `type` and full check details in its text |
| `skipped` | One per not-evaluated check, with the reason in `message` and full check details in its text |
| Testcase `system-out` | Status, reason, deadline, elapsed time, count, and first evidence, including for passing checks |
| Suite `system-out` | Summary with passed count, schema version, origin, timing basis, and scope |

Passing cases have neither `failure` nor `skipped`. Reports omit `time` and
`timestamp` attributes so boot times cannot be mistaken for test execution times.
Messages use the same reversible quoted representation as text reports, followed
by XML escaping. Characters forbidden in XML 1.0, such as NUL, U+FFFE, and U+FFFF,
appear as printable escapes. They are never silently removed or replaced. After
decoding the XML, use `strconv.Unquote` on a quoted message to recover its contents.

This subset is intended for ordinary CI report readers, but compatibility with
every JUnit importer is not guaranteed. The examples in [`testdata`](testdata)
show all three formats. They are hand-written expected reports, not evidence of
real boots.

## Output failures

If a writer reports fewer bytes than requested without an error, `Write` returns
`io.ErrShortWrite`. Any error supplied by the writer retains its identity for
`errors.Is` and `errors.As`, including after partial or full writes. A write failure can leave a partial report, which callers must
treat as incomplete. The report writer never closes the destination.

The CLI returns exit code 2 for rendering failures. A complete report with failed
checks returns exit code 1.

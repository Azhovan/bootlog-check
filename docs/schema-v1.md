# Go API and report schema, version 1

The root package is `bootlog`, imported as `github.com/firmdura/bootlog-check`.
This reference describes the types shared by the parser, evaluator, and report
writers, along with their JSON representations. The
[behavior contract](v1-contract.md) defines how these components behave.

## Public model

A `Record` represents one log record and identifies its source line. It has `Line int64`,
`TimestampNS *int64`, and `Message string`. A nil `TimestampNS` means the record
has no timestamp. A `Log` contains `Records []Record` in file order.

Line numbers are positive and strictly increasing; gaps account for skipped
empty lines. Timestamps are nonnegative and must stay the same or increase across
all timestamped records, including any preamble before boot start.
`Log.Validate()` checks these properties and valid UTF-8 without applying the
parser's size limits. If you build a log directly, you are responsible for its
memory and input-size bounds.

`ParseOptions` has independent `int64` fields `MaxInputBytes`, `MaxLines`, and
`MaxLineBytes`. `Normalize()` returns a copy in which zero values are replaced
by the defaults: 16 MiB, 100,000 lines, and 64 KiB per line, respectively. A negative
value returns an error in the `ErrInvalidInput` category.

The caller owns the source reader and is responsible for closing it. The parser
checks cancellation between reads, but cannot interrupt a reader already blocked
in `Read`.

`Config` has `SchemaVersion int`, `BootStart BootStartConfig`,
`Milestones []MilestoneConfig`, and `Forbidden []ForbiddenConfig`.
`BootStartConfig` has `Pattern string`; the other rule types also have `ID string`.
Only `MilestoneConfig` has `WithinMS *int64`. A nil deadline checks only whether
the milestone appears. A zero deadline requires the first eligible matching record
to have exactly the boot-start timestamp.

`Config.Validate()` checks required values, IDs, limits, deadlines, and patterns.
`CompileRules` performs this validation and retains the compiled expressions
without compiling them twice. You do not need to call `Validate` before
`CompileRules`.

The maximum deadline is 9,223,372,036,854 milliseconds, whose conversion to
9,223,372,036,854,000,000 nanoseconds fits in int64. Configuration is limited to
128 user rules and 4,096 UTF-8 bytes per pattern. The rules document is limited to
1 MiB. These values are exported constants in `options.go`.

Nil rule slices in Go mean empty lists. In a JSON rule file, optional arrays may
be omitted, but explicit null arrays are invalid. Marshaling a `Config` writes
both empty lists as `[]` and omits a nil `within_ms`. Supplying `within_ms: null`
in rule JSON is invalid.

`ParseRules` also checks that required fields are present, rejects duplicate
keys, enforces field types and null restrictions, validates UTF-8, and requires
exactly one complete JSON document. Calling `encoding/json.Unmarshal` directly
does not provide those checks and is not a substitute for `ParseRules`.

`RuleSet` exposes no fields or mutable accessors. Its zero value is invalid. The
compiler copies the configuration, including the values referenced by deadline
pointers and the contents of rule slices, and keeps its compiled expressions
private. Concurrent evaluations can safely share a `RuleSet`. The evaluator
rejects nil and zero-value rulesets and validates caller-built logs without
reapplying the parser's default limits.

`InvalidInputError`, `InvalidRulesError`, `LimitError`, `CancellationError`, and
`InvalidReportError` are pointer error types for `errors.As`. Their immutable
`ErrorCategory` constants are `ErrInvalidInput`, `ErrInvalidRules`, `ErrLimit`,
`ErrCanceled`, and `ErrInvalidReport`, usable with `errors.Is`. A concrete error's
`Err` field is its unwrapped cause, preserving errors such as `io.ErrUnexpectedEOF`,
`context.Canceled`, and `context.DeadlineExceeded`. Limit and cancellation are
distinct error categories. If you need an error to match more than one category,
wrap it explicitly. A failed check is data in a valid `Report`, not an
`InvalidReportError`.

## Report JSON

Every serialized report contains all six fields in this order:

| Field | Type and meaning |
| --- | --- |
| `schema_version` | Integer `1` |
| `timing_basis` | String `"boot_start"`, identifying the intended basis for elapsed times; this label remains present when boot start is missing |
| `origin` | The boot-start evidence object, or explicit `null` when boot start is missing |
| `checks` | Array containing boot start, milestones in configuration order, then forbidden rules in configuration order |
| `summary` | Object with integer `total`, `passed`, `failed`, and `not_evaluated` counts |
| `scope` | The fixed statement below explaining what a result can establish |

`scope` is exactly:

```text
Results describe only the supplied capture; a pass does not prove capture completeness or device health.
```

Every check has these fields in this order, with no omitted fields:

| Field | Type and meaning |
| --- | --- |
| `id` | Valid unique rule ID; first check is reserved `boot_start` |
| `kind` | `start`, `milestone`, or `forbidden` |
| `status` | `passed`, `failed`, or `not_evaluated` |
| `reason` | One of the seven fixed reason strings below |
| `within_ms` | Nonnegative integer milliseconds for a deadline, otherwise `null` |
| `elapsed_ns` | Signed integer: first evidence timestamp minus boot-start timestamp, or `null` if either is unavailable |
| `match_count` | Nonnegative integer matching-record count; one record counts once per rule |
| `evidence` | An Evidence object for the first matching record, or `null` if there is none |

An Evidence object contains `line` (a positive integer), `timestamp_ns` (a
nonnegative int64 or `null`), and `message` (the exact valid Unicode message).
The first matching record is sometimes called the witness. The `origin` is the
same evidence as the passed boot-start check.

A positive match count requires evidence; a zero count requires that evidence be
absent. When boot start and evidence both have timestamps, elapsed time is their
signed difference. Otherwise it is null. Forbidden evidence before boot start
can have a negative elapsed time. Milestone evidence cannot precede boot start.

| Kind and condition | Status | Reason |
| --- | --- | --- |
| Start found exactly once, timed | `passed` | `matched` |
| Start missing | `failed` | `missing` |
| Milestone present, presence-only or within inclusive deadline | `passed` | `matched` |
| Milestone absent with origin present | `failed` | `missing` |
| Milestone has a deadline but its first match has no timestamp | `failed` | `untimed` |
| Milestone first match is beyond its deadline | `failed` | `late` |
| Any milestone without origin | `not_evaluated` | `no_boot_start` |
| Forbidden absent anywhere in capture | `passed` | `absent` |
| Forbidden present anywhere in capture | `failed` | `forbidden_match` |

A report contains 2 to 129 checks. There is exactly one boot-start check, and it
appears first with no deadline. If boot start is found, this check has one
timestamped evidence record and elapsed time zero. Otherwise, it has no evidence.

When boot start is missing, every milestone has status `not_evaluated`, reason
`no_boot_start`, a count of zero, and null evidence and elapsed time. Forbidden
checks are still evaluated. Summary counts must equal the number of checks with
each status. Overall success means `summary.failed == 0`.

`Report.Validate()` checks these relationships, verifies that references to the
same line have consistent evidence, and checks timestamp order among the evidence
records. It cannot verify match counts or first matches against the original log,
or check the order of milestones and forbidden rules against the configuration.
The evaluator's tests cover those properties.

JSON numbers are written as integers. Consumers must preserve int64 precision
rather than converting values through floating point. The schema includes no
paths, host-clock readings, processing durations, raw-byte counts, or line-ending
styles.

Marshaling `Log`, `Config`, and `Report` converts nil lists to `[]`; nil evidence
and report timing fields remain explicit `null`. An empty `Log` is valid. An
empty `Config` or `Report` can be marshaled, but is invalid. Marshaling methods do
not validate their input, so report writers must call `Validate` before writing
any bytes.

The independently written [expected reports](../testdata/reports) cover passing
checks, mixed failures, and missing boot start with skipped milestones. Tests
execute these examples against the evaluator and report writers.
`internal/testfixture` provides equivalent Go values and helpers for small,
deterministic logs and configurations. These are test examples, not real QEMU
captures.

## Public functions

```go
ParseLog(ctx context.Context, r io.Reader, opts ParseOptions) (Log, error)
ParseRules(r io.Reader) (Config, error)
CompileRules(cfg Config) (*RuleSet, error)
Evaluate(log Log, rules *RuleSet) (Report, error)
```

`Evaluate` runs synchronously and takes no context. Its work is finite and
bounded by the validated log and rule count. It changes neither input. Callers
retain responsibility for their readers. The core library does not print, exit
the process, run host commands, or access the network.

The public package `github.com/firmdura/bootlog-check/report` imports `bootlog`
and provides `Write(io.Writer, bootlog.Report, Format) error`. Its string-backed
`Format` constants are `Text = "text"`, `JSON = "json"`, and `JUnit = "junit"`.
The core package never imports `report`.

Report writers validate before writing, return errors for short or failed writes,
and never close the caller's writer. Matched messages require the same data
handling as the source log. Text and XML escape unsafe control characters; JSON
uses standard escaping that preserves the underlying Unicode. The CLI uses
`internal/cli` with a thin `cmd/bootlog-check/main.go` entry point.

## Toolchain and verification

The project pins Go **1.27.4** in `.go-version`; `go.mod` declares Go **1.27.0**
as the minimum. The pinned version was checked against the
[official Go download feed](https://go.dev/dl/?mode=json) when the toolchain was
selected.

Install the matching platform archive from [Go downloads](https://go.dev/dl/) and
verify its published SHA-256 before adding its `bin` directory to PATH. The
initial implementation used the darwin/arm64 archive with SHA-256
`91a44a2b8a716600ad94a4f143e156116620db8828086a6e3602291be9bd0bb4`.

```sh
go version
./scripts/verify.sh
go test -race ./...
```

The verification script checks formatting without changing files, runs `go vet`
and tests, and builds all delivered packages, including `bin/bootlog-check`.
It disables toolchain and dependency downloads, so verification runs offline once
Go is installed. No root access, firmware, Docker, hardware, or QEMU is needed.
If your sandbox cannot write to the default Go build cache, set `GOCACHE` to a
writable directory. There are no third-party runtime dependencies.

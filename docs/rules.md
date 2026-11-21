# Rules and saved-log evaluation

To check a saved log, read its JSON rules with
`ParseRules(io.Reader) (Config, error)`, then compile them with
`CompileRules(Config) (*RuleSet, error)`. Compilation validates the configuration,
compiles each pattern once, and copies the rule slices and deadline values.
You do not need to call `Config.Validate` first. Parsing also validates patterns,
so a successfully parsed configuration is ready to compile.

Pass the log and compiled rules to `Evaluate(Log, *RuleSet) (Report, error)`.
You can reuse a compiled `RuleSet` for different logs and for concurrent calls.
The library never closes a reader; that remains the caller's responsibility.

The [sample rules](../testdata/rules/sample.json) check a Linux banner, two
milestones with deadlines, a milestone that can match a message without a
timestamp, and two forbidden patterns. The
[runnable examples](../rules_examples_test.go) demonstrate parsing, compilation,
and evaluation from a separate Go package. See the [API and schema reference](schema-v1.md)
for report fields and the [behavior contract](v1-contract.md) for the full v1.0
requirements.

## Writing a rule file

```json
{
  "schema_version": 1,
  "boot_start": {"pattern": "^Linux version "},
  "milestones": [
    {"id": "ready", "pattern": "^APP_READY$", "within_ms": 5000}
  ],
  "forbidden": [
    {"id": "panic", "pattern": "(?i)kernel panic"}
  ]
}
```

The `boot_start` pattern is required, along with at least one milestone or
forbidden rule. Each milestone and forbidden rule needs an `id` and a `pattern`.
You may omit either optional array; supplying `null` instead of an array is
invalid.

A milestone without `within_ms` checks only whether its message appears. To add a
deadline, supply a nonnegative JSON integer. A value of `0` requires the first
eligible matching record to have a timestamp equal to boot start. Null,
fractional, and exponent forms are invalid.

Only schema version 1 is accepted, and field names are case-sensitive. The parser
rejects duplicate keys, unknown fields, extra JSON after the document, incorrect
types, invalid UTF-8, and unpaired Unicode surrogate escapes. It does not repair
or normalize malformed rules.

The complete document, including whitespace, must fit within 1 MiB. The two arrays
may contain at most 128 rules in total. Patterns must contain 1 to 4,096 UTF-8 bytes.
IDs must match `[A-Za-z][A-Za-z0-9_.-]{0,63}`, be unique across both arrays, and must
not be the reserved ID `boot_start`. The largest deadline is
9,223,372,036,854 ms, which can be converted to signed 64-bit nanoseconds without
overflow.

If two rules use the same ID, the error names both fields. For example,
`invalid rules (forbidden[0].id): duplicates milestones[1].id` means the first
forbidden rule and the second milestone share an ID. Give one of them a
different ID.

## Matching messages

Patterns use Go's `regexp` syntax. They are case-sensitive unless the pattern
contains a flag such as `(?i)`. A pattern searches within each message; add `^`
and `$` if the entire message must match. Timestamp prefixes are removed before
matching, but control characters and Unicode remain unchanged.

Patterns are not globs and cannot match across records. Invalid regular
expressions and expressions that match the empty string are rejected. A record
can match several rules. If a pattern occurs several times in one record, that
record still counts only once for that rule.

## Boot start and the first match

The evaluator searches the whole log for the boot-start pattern. Exactly one
matching record with a timestamp establishes the timing origin: the timestamp
from which elapsed times are measured. Two matching records make the input
invalid, even if their timestamps are equal or one has no timestamp. A single
boot-start match without a timestamp is also invalid. In these cases, evaluation
returns an error and no report.

If there is no boot-start match, the report contains a failed `boot_start` check
with reason `missing`. Each milestone has status `not_evaluated`, reason
`no_boot_start`, a match count of zero, and no evidence. Forbidden rules are still
checked against every record. An empty log produces the same missing-start
results.

Milestones consider only the boot-start record and later records in file order.
Earlier matches never count, even if they have the same timestamp as boot start.
For each milestone, the first eligible matching record is kept as evidence. This
record is also called the witness. All eligible matching records contribute to
the count, but later matches never replace the first evidence.

The boot-start record itself can satisfy a milestone with elapsed time zero.
Milestones do not need to appear in the order listed in the configuration; that
order controls only their order in the report.

## Deadlines and forbidden messages

Each deadline is measured independently from the boot-start timestamp. A match
exactly at the deadline passes; a match one nanosecond later fails with reason
`late`. A zero deadline requires the first eligible matching record to have a timestamp
equal to boot start.

A milestone with no matching record fails with `missing`. A milestone without a
deadline passes whenever it has an eligible match, whether or not that match has
a timestamp. With a deadline, however,
an untimestamped first match fails with `untimed`, even if a later match has a
timestamp. That later match increases the count but does not replace the evidence.

Forbidden rules inspect the entire capture, including the preamble, records
without timestamps, and messages after all milestones have passed. Any match
fails with `forbidden_match`. If no record matches, the check passes with `absent`. The report retains
the first matching record and the total number of matching records.

Elapsed time is the evidence timestamp minus the boot-start timestamp. Forbidden
evidence before boot start can therefore have a negative elapsed time; evidence
at boot start has elapsed time zero. If either timestamp is unavailable, elapsed
time is `null`.

## Reports and errors

Reports list the boot-start check first, then milestones in configuration order,
then forbidden rules in configuration order. Every check appears, including
failed checks and milestones that could not be evaluated. The overall result
passes when `Summary.Failed == 0`.

A failed check is a valid result, so `Evaluate` returns its report with a nil error.
Invalid rules, invalid records, repeated or untimestamped boot-start matches, and
input-reading failures return errors rather than a usable partial configuration
or report.

Use `errors.Is(err, bootlog.ErrInvalidRules)` for malformed rules or a nil or
zero-value `RuleSet`, `errors.Is(err, bootlog.ErrInvalidInput)` for invalid logs or
boot-start matches, and `errors.Is(err, bootlog.ErrLimit)` for an oversized rule
document. The concrete `InvalidRulesError`, `InvalidInputError`, and `LimitError`
types support `errors.As`. Underlying reader errors remain available through
`errors.Is` and error unwrapping. Violations of rule-count, pattern, and deadline
limits are invalid-rules errors.

## Validation and safe reuse

Configurations built directly in Go receive the same semantic validation as
parsed configurations. Logs built directly in Go must have positive, strictly
increasing line numbers, valid UTF-8 messages, and nonnegative timestamps that
stay the same or increase across all timestamped records. Validation includes
records after readiness.

`Evaluate` checks the log's structure without reapplying the parser's default
size limits. This preserves any larger limits you chose when parsing. If you
build a log directly, you are responsible for its memory and input-size bounds.

Evaluation runs synchronously, accepts no context, and performs no I/O. Its work
is bounded by the validated log and the rule limit. It changes neither input and
makes separate copies of output evidence and deadlines. A compiled `RuleSet` is
safe to share across concurrent evaluations. Callers must not modify a log while
another goroutine is evaluating it.

A report describes only the supplied capture. It cannot establish that the
capture is complete, that source timestamps reflect real elapsed time, that no
later failure occurred outside the file, or that the device is healthy. Missing
or late evidence describes an observation, not its cause.

# Parsing saved logs

Use `bootlog.ParseLog(ctx, reader, options)` to read a saved UTF-8 log into a
`bootlog.Log`. The parser reads to the end of the file and keeps records in their
original order. It does not apply rules or decide whether a boot passed. An
empty log is valid input to the parser.

The [behavior contract](v1-contract.md) defines the complete requirements, and
the [API reference](schema-v1.md) describes the public types. The
[runnable examples](../parser_example_test.go) show how to call the parser.

## Lines and message text

The parser accepts LF and CRLF line endings, as well as a final line without a
line ending. It removes the LF and, if present, the single CR immediately before
it. Other CR characters remain part of the message, including a lone CR at the
end of the file. A final line ending does not create an extra empty line.

Empty lines do not produce records, but they still count toward the line and
input-byte limits. Lines containing only whitespace produce records without
timestamps and retain their original text. Each record includes its original
line number, starting at 1, so an empty line leaves a gap in the record numbers.

Invalid UTF-8 is rejected. Valid control characters, including NUL and ANSI escape
sequences, are preserved for rule matching. The parser does not make messages
safe to display; the report writers escape unsafe characters when rendering them.

## Timestamps

A timestamped line must start in column one with the following prefix:

1. An opening `[` followed by zero or more ASCII spaces or tabs.
2. One or more ASCII decimal digits for a nonnegative number of seconds.
3. An optional decimal point followed by 1 to 9 fractional digits.
4. A closing `]` immediately after the number.

After the closing bracket, the parser removes at most one separating space or
tab. Everything that remains is the message, unchanged. Leading zeros in the
number are accepted. A prefix with no message still produces a timestamped record
with an empty message.

| Saved line | Timestamp in nanoseconds | Message |
| --- | --- | --- |
| `[    1.250000] APP_READY` | `1250000000` | `APP_READY` |
| `[0.000000001]` | `1` | empty |
| `[1]  ready` | `1000000000` | one leading space, then `ready` |
| `[ OK ] ready` | null | `[ OK ] ready` |
| ` [1] ready` | null | one leading space, then `[1] ready` |

Timestamps are converted to integer nanoseconds with overflow checks, so there is
no floating-point rounding. The largest accepted value is
`[9223372036.854775807]`, or `9223372036854775807` nanoseconds. Signs, exponents,
missing digits, more than nine fractional digits, and overflow are invalid. For
example, `[+1]`, `[.5]`, `[1.]`, `[1e3]`, `[1 ]`, and `[1` are rejected.

The parser treats a line as an attempted timestamp when it starts with `[` and
the first character after any spaces or tabs is an ASCII digit, `+`, `-`, or `.`.
If that prefix is malformed, parsing fails. It is not silently kept as an ordinary
message. Other bracketed lines, including `[ OK ] ready` and `[`, are ordinary
records without timestamps.

A record without a timestamp has `TimestampNS == nil`. The parser never fills in
a missing timestamp from a neighboring record.

## One clock for the whole capture

Timestamps must stay the same or increase throughout the file, including any
preamble before the boot-start message. A decreasing timestamp is invalid, even
before a Linux banner or after an application-ready message. Empty lines and
records without timestamps do not reset the previous timestamp.

All timestamped lines must therefore use the same clock. You can keep untimestamped
bootloader text alongside timestamped kernel messages, but the parser does not
combine different clocks, reorder records, split multiple boots, or reset the
starting point for timing. The evaluator identifies that starting point from the
boot-start rule.

A ready marker does not stop parsing: the complete file is validated. Successfully
parsing a log does not establish that the capture is complete or the device is
healthy.

## Limits and memory use

Each `ParseOptions` limit is independent. A zero value selects its default; a
negative value is invalid.

| Option | Default | What it counts |
| --- | --- | --- |
| `MaxInputBytes` | 16 MiB | All input bytes, including LF and CRLF line endings |
| `MaxLines` | 100,000 | All lines, including empty ones |
| `MaxLineBytes` | 64 KiB | Bytes in one line, including its timestamp prefix but excluding LF or CRLF |

Input exactly at a limit is accepted; exceeding it fails. These are byte counts,
so a multibyte UTF-8 character counts as all of its bytes. A lone final CR counts
toward line length, while the CR in CRLF does not. LF and CRLF versions of the same
log produce identical records if both fit the limits, but the CRLF version uses
more input bytes.

For example, `ParseOptions{MaxInputBytes: 32 << 20}` raises only the input-byte
limit to 32 MiB. The line-count and line-length limits keep their defaults.
Larger limits allow larger allocations, so callers should choose them to suit
the resources available. The parser limits reads and line-buffer growth before
retaining oversized input.

The returned log is held in memory. Memory use grows with the accepted message
bytes and record count, plus parsing buffers and allocation overhead. These limits
are not an exact heap-size budget, and the parser is not a constant-memory
streaming API.

## Errors, readers, and cancellation

A parsing failure returns a zero-value `Log`, never a usable partial log. Use
`errors.Is` to identify an error category and `errors.As` to inspect its details:

| Category | Concrete error | Examples |
| --- | --- | --- |
| `ErrInvalidInput` | `*InvalidInputError` | Malformed timestamp, decreasing time, invalid UTF-8, negative option, reader failure |
| `ErrLimit` | `*LimitError` | Input-byte, line-count, or line-length limit exceeded |
| `ErrCanceled` | `*CancellationError` | Canceled context or expired deadline |

Reader errors other than EOF cause parsing to fail, even after valid content has
been read. The underlying cause is preserved, so a call such as
`errors.Is(err, io.ErrUnexpectedEOF)` can identify it. Cancellation errors
similarly preserve `context.Canceled` or `context.DeadlineExceeded`.

A `LimitError` identifies the resource and its configured limit. An
`InvalidInputError` includes a line number when the problem belongs to a particular
line. Inspect these categories and fields instead of relying on exact diagnostic
wording.

Pass a nonnil context and reader. The caller is responsible for closing the reader
when necessary; `ParseLog` never closes it. Cancellation is checked between reads.
It cannot interrupt an arbitrary `io.Reader` already blocked in `Read`. If you need
interruptible I/O, provide a reader that can be unblocked, for example through its
own deadline or a way to close it.

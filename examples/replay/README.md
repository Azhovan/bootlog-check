# Replaying synthetic logs offline

These small logs were written by hand to exercise specific rules and edge cases.
They were not captured from a device or a past failure. No bootloader or firmware
was run to create them, and the U-Boot banner is only an illustration. For logs
from actual Linux boots, see the separate [QEMU example](../qemu/README.md).

From the repository root, with the Go version in `.go-version` installed:

```sh
./scripts/demo.sh
go test ./integration/...
./scripts/verify.sh
```

The demo needs only Go and a POSIX shell. It disables toolchain and module
downloads, builds the CLI and a report checker that uses only the standard
library, then runs all seven logs in text, JSON, and JUnit format. It prints
21 verified outcomes.

Some scenarios are expected to return exit code 1 or 2; the demo captures and
checks those codes. It stops with a nonzero status if an exit code, diagnostic,
report structure, or report field differs from the expected result. JSON numbers
are decoded without losing integer precision. JUnit is parsed as XML, and its
testcase, failure, and skipped counts are checked against its contents. Text and
JUnit evidence are compared with the same independently written expectations.

The checker in `integration/internal/replayproof` imports no production code.
Its reference file, `expected.json`, specifies every expected field. Those values
were derived from the log lines and integer arithmetic before running
`bootlog-check`, rather than generated from the evaluator's output.

Integration tests also pass the same inputs through the public parser, rule
compiler, evaluator, and report writer, then compare the output byte for byte
with the CLI. Each format is checked in four runs: twice with the original input,
once with CRLF line endings, and once without a final newline. All four outputs
must agree. Invalid input must produce no usable partial library log or report.

## Expected findings

Every valid report has five checks in this order: `boot_start`, `network_ready`,
`application_ready`, `console_ready`, and `kernel_panic`. Line numbers in the
evidence still account for the empty line at line 2.

In `pass.log`, boot start is line 4 at 1,000,000,000 ns. This is the origin from
which elapsed times are measured. The first eligible network match is line 6,
with elapsed time 1,000,000,000 ns, exactly its 1000 ms deadline. The earlier
match on line 3 does not count because it comes before boot start.

The first application match is line 7, with elapsed time 2,000,000,000 ns,
exactly its 2000 ms deadline. There are two eligible application matches.
`CONSOLE_READY` on line 5 passes because its rule requires only presence;
its timestamp is null.

| Log | Exit | Expected finding | Passed / failed / not evaluated |
| --- | --- | --- | --- |
| `pass.log` | 0 | All checks pass; forbidden count is zero | 5 / 0 / 0 |
| `missing.log` | 1 | Application is `missing`, with count zero and null evidence and elapsed time | 4 / 1 / 0 |
| `forbidden.log` | 1 | `forbidden_match`, count 2; first line 10 at 4,500,000,000 ns, elapsed 3,500,000,000 ns | 4 / 1 / 0 |
| `late.log` | 1 | Application `late` by 1 ns; first line 7 at 3,000,000,001 ns, elapsed 2,000,000,001 ns, count 2 | 4 / 1 / 0 |
| `missing-origin.log` | 1 | Start `missing`; all three milestones `no_boot_start`; forbidden still fails on line 9 with null elapsed | 0 / 2 / 3 |
| `untimed-first.log` | 1 | Application is `untimed`, with first match on line 7 and count 2; the later timestamped match does not change the result | 4 / 1 / 0 |
| `invalid-trailing.log` | 2 | Malformed exponent prefix at line 10 after readiness; empty stdout, diagnostic on stderr | No report |

The forbidden messages appear after readiness and contain XML metacharacters.
The checks verify that the first matching message survives report escaping and
parsing unchanged. They also verify that a malformed timestamp after readiness
causes the whole input to be rejected. Together, these cases show that processing
continues through the end of the file.

To inspect one report, run the following commands from the repository root.
They build the CLI in a temporary directory, capture the expected exit code 1,
and display the JSON report before removing the directory:

```sh
replay_dir=$(mktemp -d /tmp/bootlog-replay-manual.XXXXXX)
go build -o "$replay_dir/bootlog-check" ./cmd/bootlog-check
replay_status=0
"$replay_dir/bootlog-check" --rules examples/replay/rules.json --format json \
  examples/replay/forbidden.log > "$replay_dir/forbidden.json" || replay_status=$?
printf 'exit=%s (expected 1)\n' "$replay_status"
cat "$replay_dir/forbidden.json"
test "$replay_status" -eq 1
rm -rf "$replay_dir"
```

The automated demo removes its `/tmp/bootlog-replay.*` binaries and reports when
it finishes, fails, or receives SIGINT or SIGTERM. It writes no output into the
tracked source tree. The manual example's final command removes its temporary
directory; if interrupted, run
`rm -rf "$replay_dir"` in the same shell. General verification separately builds
the ignored `bin/bootlog-check`, which can be removed with `rm -f bin/bootlog-check`.

The synthetic timestamps exercise exact deadline boundaries; they do not predict
real boot performance. A passing replay describes only the saved evidence
provided. It does not establish capture completeness, device health, hardware
behavior, or the accuracy of source timestamps. The demo does not capture live
logs, access hardware or the network, start Docker, or boot QEMU.

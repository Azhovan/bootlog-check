# CI and local verification

Run the built executable in CI and use its exit code to decide whether the job
passes. Save that code along with the reports. The example below is tested
against the real executable by
[`TestCIShellExample`](../examples/library/ci_test.go).

## Check a saved capture and keep artifacts

Build the command first. `go run` may change the program's exit code, so call
the executable directly when its result determines whether a CI job passes.

```sh
go build -o ./bin/bootlog-check ./cmd/bootlog-check
```

The following POSIX shell script saves a report in each format, plus any error
messages. Set `RULES` and `LOG` to the paths of your rules file and saved log
before running it. Keep both files unchanged while the script runs, because each
command reads them again. The script exits with code 0 if every check passes, 1
if any check fails, or 2 if any invocation encounters an operational error. Code
2 takes precedence over code 1, so an error cannot be hidden by a completed
failed check.

```sh
set -eu
: "${RULES:?set RULES to the rules file}"
: "${LOG:?set LOG to the saved log}"
if mkdir -p artifacts; then :; else exit 2; fi
bootlog_status=0
collect_report() {
  if ./bin/bootlog-check --rules "$RULES" --format "$1" "$LOG"; then
    bootlog_result=0
  else
    bootlog_result=$?
  fi
  case "$bootlog_result" in
    0) ;;
    1) if [ "$bootlog_status" -eq 0 ]; then bootlog_status=1; fi ;;
    *) bootlog_status=2 ;;
  esac
  return 0
}
for format in text json junit; do
  case "$format" in
    text) extension=txt ;;
    json) extension=json ;;
    junit) extension=xml ;;
  esac
  if collect_report "$format" \
      >"artifacts/report.$extension" 2>"artifacts/$format.stderr"; then
    :
  else
    bootlog_status=2
  fi
done
exit "$bootlog_status"
```

A failed check produces a complete report and exits with code 1. Invalid input,
invalid rules, or another operational error produces code 2. Errors before
report writing leave an empty report file and an error message in the
corresponding `.stderr` file. A write error can leave a partial report. The
script also returns 2 if a command returns an unexpected exit code or a shell
redirection fails.

A report file's existence does not establish that the check passed or that the
file is complete. Preserve the exit code: `|| true` discards a failure, and a
pipeline can hide it behind the result of a later command.

In GitHub Actions, put the script in the step that runs the check. Leave
`continue-on-error` disabled so a failed check fails the job. Add a later step
with `always()` to upload the reports even when the check fails:

```yaml
- name: Keep reports and diagnostics
  if: ${{ always() }}
  uses: actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7.0.2
  with:
    name: bootlog-reports
    path: artifacts/
    if-no-files-found: error
    retention-days: 7
```

Uploading artifacts does not change the earlier step's failed result. If several
jobs in a matrix upload reports, give each artifact a different name. GitHub
makes the files available for download; uploading JUnit XML alone does not
create a test-results view. bootlog-check does not include a custom GitHub
Action or a reporting service. The [official artifact
guide](https://docs.github.com/en/actions/tutorials/store-and-share-data) and
[status-function
reference](https://docs.github.com/en/actions/reference/workflows-and-actions/expressions#always)
explain how artifact uploads and step conditions work. Reports contain matched
log messages, so apply the source capture's access controls and retention policy
to uploaded artifacts.

## Repository checks

Install the Go version pinned in `.go-version`. Check your downloaded archive
against its SHA-256 checksum on [Go downloads](https://go.dev/dl/), then add
Go's `bin` directory to your `PATH`. Routine checks use only the standard
library and run offline after setup. Go needs a writable build cache; set
`GOCACHE` to a writable directory if its default location is unavailable.

```sh
export GOTOOLCHAIN=local
export GOPROXY=off
export GOSUMDB=off
./scripts/verify.sh
go test -race ./...
go test . -run='^$' -fuzz='^FuzzParseLog$' -fuzztime=30s -fuzzminimizetime=5s -parallel=2 -timeout=2m
go test . -run='^$' -fuzz='^FuzzParseRules$' -fuzztime=30s -fuzzminimizetime=5s -parallel=2 -timeout=2m
```

`verify.sh` checks Go formatting without changing files, runs `go vet` and all
package tests, and builds the CLI. The tests include the public examples and
checks that launch the real executable and verify its exit codes.

Run race checks on a supported native host with cgo enabled. Linux also requires
an installed C compiler. See the [Go race-detector
requirements](https://go.dev/doc/articles/race_detector#Requirements). The two
fuzz commands limit the worker count, fuzzing duration, time spent minimizing
failures, and total run time. Each 30-second run is a brief check; it cannot
cover every possible input. See [Go fuzzing](https://go.dev/doc/security/fuzz/).

To check that the command compiles for all four supported platforms, build each
one with cgo disabled:

```sh
mkdir -p bin
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  target_os=${target%/*}
  target_arch=${target#*/}
  CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" \
    go build -o "bin/bootlog-check-$target_os-$target_arch" ./cmd/bootlog-check
done
```

Keep `CGO_ENABLED=0` local to these build commands; race checks need cgo. A
successful cross-build establishes that the code compiles for that platform, but
does not show that the executable runs there.

The repository's CI workflow runs native tests and race checks on Linux and
macOS. On Linux it also runs the two brief fuzz checks and all four
cross-builds. The workflow runs on pushes to `main` and can be started manually.
It pins the Go version and official GitHub Actions to specific versions or
commits and requests read-only repository permissions. To confirm that CI passed
for a particular commit, check its [Actions
run](https://github.com/firmdura/bootlog-check/actions); a local check does not
establish that CI ran.

To run the [offline replay example](../examples/replay/README.md), use:

```sh
./scripts/demo.sh
go test ./integration/...
```

The example compares seven synthetic scenarios in all three formats with
independently written expected results. Its integration tests also run as part
of `go test ./...` and `verify.sh`. Those checks do not download firmware, run
QEMU or Docker, require root access, or use device hardware.

The separate [QEMU/Linux example](../examples/qemu/README.md) documents six
actual Linux boots and CLI reports checked independently against the saved logs.
It also includes timeout and cleanup tests and specifies the prerequisites
needed to reproduce the boots. Cross-builds and synthetic replay alone do not
provide evidence from an actual boot.

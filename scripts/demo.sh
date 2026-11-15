#!/bin/sh
set -eu

cd "$(dirname "$0")/.."
export GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off

# Keep builds and reports outside the checkout, including on failure.
workdir=$(mktemp -d /tmp/bootlog-replay.XXXXXX)
trap 'rm -rf "$workdir"' 0
trap 'exit 130' INT
trap 'exit 143' TERM

go build -o "$workdir/bootlog-check" ./cmd/bootlog-check
go build -o "$workdir/check" ./integration/check
expected=examples/replay/expected.json
"$workdir/check" list "$expected" > "$workdir/cases"

while read -r scenario expected_exit; do
  for format in text json junit; do
    actual_exit=0
    "$workdir/bootlog-check" --rules examples/replay/rules.json --format "$format" \
      "examples/replay/$scenario.log" > "$workdir/report" 2> "$workdir/stderr" || actual_exit=$?
    if [ "$actual_exit" -ne "$expected_exit" ]; then
      printf '%s/%s: expected exit %s, got %s\n' "$scenario" "$format" "$expected_exit" "$actual_exit" >&2
      cat "$workdir/stderr" >&2
      exit 1
    fi
    if [ "$expected_exit" -eq 2 ]; then
      if [ ! -s "$workdir/stderr" ]; then
        printf '%s/%s: missing operational diagnostic\n' "$scenario" "$format" >&2
        exit 1
      fi
    elif [ -s "$workdir/stderr" ]; then
      cat "$workdir/stderr" >&2
      exit 1
    fi
    "$workdir/check" verify "$expected" "$scenario" "$format" "$workdir/report"
    printf '%s/%s: exit %s verified\n' "$scenario" "$format" "$actual_exit"
  done
done < "$workdir/cases"

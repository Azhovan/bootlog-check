#!/bin/sh
set -eu

cd "$(dirname "$0")/.."

# Normal verification must not download a toolchain or dependencies.
export GOTOOLCHAIN=local
export GOPROXY=off
export GOSUMDB=off

unformatted=$(find . -name '*.go' -not -path './.git/*' -exec gofmt -l {} +)
if [ -n "$unformatted" ]; then
  printf 'Go files need formatting:\n%s\n' "$unformatted" >&2
  exit 1
fi

go vet ./...
go test ./...

go build ./...
mkdir -p bin
go build -o ./bin/bootlog-check ./cmd/bootlog-check

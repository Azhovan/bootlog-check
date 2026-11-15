package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/firmdura/bootlog-check/internal/cli"
)

func main() {
	// Report a broken output pipe as exit 2 instead of SIGPIPE termination.
	signal.Ignore(syscall.SIGPIPE)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Run(ctx, os.Args[1:], cli.Streams{Stdout: os.Stdout, Stderr: os.Stderr})
	stop()
	os.Exit(code)
}

package integration_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	bootlog "github.com/firmdura/bootlog-check"
	"github.com/firmdura/bootlog-check/integration/internal/replayproof"
	"github.com/firmdura/bootlog-check/report"
)

// The expected fields are authored in examples/replay/expected.json from the
// physical fixture lines and integer arithmetic, never captured product output.
func TestReplay(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	fixtures := filepath.Join(root, "examples", "replay")
	cases, err := replayproof.Load(filepath.Join(fixtures, "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	rulesPath := filepath.Join(fixtures, "rules.json")
	rulesData, err := os.ReadFile(rulesPath)
	if err != nil {
		t.Fatal(err)
	}
	config, err := bootlog.ParseRules(bytes.NewReader(rulesData))
	if err != nil {
		t.Fatal(err)
	}
	rules, err := bootlog.CompileRules(config)
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "bootlog-check")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/bootlog-check")
	build.Dir = root
	build.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build real CLI: %v\n%s", err, output)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			original, err := os.ReadFile(filepath.Join(fixtures, c.Name+".log"))
			if err != nil {
				t.Fatal(err)
			}
			var first = make(map[report.Format][]byte)
			for _, variant := range []struct {
				name string
				data []byte
			}{
				{"LF", original},
				{"repeat", original},
				{"CRLF", bytes.ReplaceAll(original, []byte("\n"), []byte("\r\n"))},
				{"unterminated", bytes.TrimSuffix(original, []byte("\n"))},
			} {
				t.Run(variant.name, func(t *testing.T) {
					logPath := filepath.Join(t.TempDir(), "synthetic capture.log")
					if err := os.WriteFile(logPath, variant.data, 0600); err != nil {
						t.Fatal(err)
					}
					log, parseErr := bootlog.ParseLog(context.Background(), bytes.NewReader(variant.data), bootlog.ParseOptions{})
					var result bootlog.Report
					if c.Exit == 2 {
						var invalid *bootlog.InvalidInputError
						if !errors.Is(parseErr, bootlog.ErrInvalidInput) || !errors.As(parseErr, &invalid) || invalid.Line != c.ErrorLine || len(log.Records) != 0 {
							t.Fatalf("want invalid input at line %d and no partial log; got log=%+v err=%v", c.ErrorLine, log, parseErr)
						}
					} else {
						if parseErr != nil {
							t.Fatal(parseErr)
						}
						result, err = bootlog.Evaluate(log, rules)
						if err != nil {
							t.Fatal(err)
						}
					}
					for _, format := range []report.Format{report.Text, report.JSON, report.JUnit} {
						t.Run(string(format), func(t *testing.T) {
							var library bytes.Buffer
							if c.Exit != 2 {
								if err := report.Write(&library, result, format); err != nil {
									t.Fatal(err)
								}
								if err := replayproof.Verify(c, string(format), library.Bytes()); err != nil {
									t.Fatalf("composed public library: %v", err)
								}
							}
							code, stdout, stderr := runCLI(t, binary, "--rules", rulesPath, "--format", string(format), logPath)
							if code != c.Exit || (c.Exit == 2) != (len(stderr) > 0) {
								t.Fatalf("exit=%d stderr=%q; want exit=%d and diagnostic only for exit 2", code, stderr, c.Exit)
							}
							if err := replayproof.Verify(c, string(format), stdout); err != nil {
								t.Fatal(err)
							}
							if !bytes.Equal(stdout, library.Bytes()) {
								t.Fatal("CLI and public library composition differ")
							}
							if variant.name == "LF" {
								first[format] = bytes.Clone(stdout)
							} else if !bytes.Equal(stdout, first[format]) {
								t.Fatal("repeated, LF, CRLF, or unterminated replay differs")
							}
						})
					}
				})
			}
		})
	}
}

func runCLI(t *testing.T, binary string, args ...string) (int, []byte, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if ctx.Err() != nil {
		t.Fatalf("CLI timed out: %v", ctx.Err())
	}
	if err == nil {
		return 0, stdout.Bytes(), stderr.Bytes()
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatal(fmt.Errorf("launch CLI: %w", err))
	}
	return exit.ExitCode(), stdout.Bytes(), stderr.Bytes()
}

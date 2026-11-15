package main_test

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCIShellExample executes the documented shell, so its exit and artifact
// guarantees are checked against the real command rather than a mock checker.
func TestCIShellExample(t *testing.T) {
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile(filepath.Join(repo, "docs", "ci.md"))
	if err != nil {
		t.Fatal(err)
	}
	var script string
	for _, block := range strings.Split(string(doc), "```sh\n")[1:] {
		body, _, ok := strings.Cut(block, "```")
		if ok && strings.Contains(body, "collect_report()") {
			if script != "" {
				t.Fatal("multiple CI collection examples")
			}
			script = body
		}
	}
	if script == "" {
		t.Fatal("CI collection example missing")
	}
	binary := filepath.Join(t.TempDir(), "bootlog-check")
	build := exec.Command("go", "build", "-o", binary, "./cmd/bootlog-check")
	build.Dir = repo
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	const rules = `{"schema_version":1,"boot_start":{"pattern":"^BOOT$"},"milestones":[{"id":"ready","pattern":"^READY$","within_ms":1000}],"forbidden":[{"id":"panic","pattern":"PANIC"}]}`
	cases := []struct {
		name         string
		log          string
		wantExit     int
		wantFailed   int
		badRedirect  bool
		badDirectory bool
	}{
		{name: "pass", log: "[1] BOOT\n[2] READY\n", wantExit: 0},
		{name: "failed checks", log: "[1] BOOT\n[2.001] READY\n", wantExit: 1, wantFailed: 1},
		{name: "operational error", log: "[1] BOOT\n[2] READY\n[2.x] broken\n", wantExit: 2},
		{name: "redirection error", log: "[1] BOOT\n[2] READY\n", wantExit: 2, badRedirect: true},
		{name: "redirection error with failed checks", log: "[1] BOOT\n[2.001] READY\n", wantExit: 2, wantFailed: 1, badRedirect: true},
		{name: "artifact directory error", log: "[1] BOOT\n[2] READY\n", wantExit: 2, badDirectory: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "bin"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(binary, filepath.Join(dir, "bin", "bootlog-check")); err != nil {
				t.Fatal(err)
			}
			for name, contents := range map[string]string{"rules.json": rules, "boot.log": tc.log, "collect.sh": script} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.badRedirect {
				if err := os.MkdirAll(filepath.Join(dir, "artifacts", "report.txt"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if tc.badDirectory {
				if err := os.WriteFile(filepath.Join(dir, "artifacts"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("sh", "collect.sh")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "RULES=rules.json", "LOG=boot.log")
			output, err := cmd.CombinedOutput()
			exit := 0
			if err != nil {
				if failure, ok := err.(*exec.ExitError); ok {
					exit = failure.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			if exit != tc.wantExit {
				t.Fatalf("exit = %d, want %d: %s", exit, tc.wantExit, output)
			}
			if tc.badDirectory {
				return
			}
			for _, format := range []struct{ name, extension string }{{"text", "txt"}, {"json", "json"}, {"junit", "xml"}} {
				if tc.badRedirect && format.name == "text" {
					continue
				}
				data, err := os.ReadFile(filepath.Join(dir, "artifacts", "report."+format.extension))
				if err != nil {
					t.Fatal(err)
				}
				diagnostic, err := os.ReadFile(filepath.Join(dir, "artifacts", format.name+".stderr"))
				if err != nil {
					t.Fatal(err)
				}
				if tc.wantExit == 2 && !tc.badRedirect {
					if len(data) != 0 || len(diagnostic) == 0 {
						t.Fatalf("%s: operational error requires empty report and diagnostic", format.name)
					}
					continue
				}
				if len(diagnostic) != 0 {
					t.Fatalf("%s: unexpected diagnostic: %s", format.name, diagnostic)
				}
				switch format.name {
				case "text":
					headline := "bootlog-check: PASS\n"
					if tc.wantFailed != 0 {
						headline = "bootlog-check: FAIL\n"
					}
					if !strings.HasPrefix(string(data), headline) || !strings.Contains(string(data), "scope: Results describe only the supplied capture;") {
						t.Fatalf("incomplete or wrong text report: %s", data)
					}
				case "json":
					var result struct {
						Checks  []json.RawMessage           `json:"checks"`
						Summary struct{ Total, Failed int } `json:"summary"`
					}
					if err := json.Unmarshal(data, &result); err != nil || len(result.Checks) != 3 || result.Summary.Total != 3 || result.Summary.Failed != tc.wantFailed {
						t.Fatalf("incomplete or wrong JSON report: %s (%v)", data, err)
					}
				case "junit":
					var result struct {
						Tests    int        `xml:"tests,attr"`
						Failures int        `xml:"failures,attr"`
						Cases    []struct{} `xml:"testcase"`
					}
					if err := xml.Unmarshal(data, &result); err != nil || result.Tests != 3 || len(result.Cases) != 3 || result.Failures != tc.wantFailed {
						t.Fatalf("incomplete or wrong JUnit report: %s (%v)", data, err)
					}
				}
			}
		})
	}
}

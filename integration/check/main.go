// Command check is the demo's independent report verifier, not a product CLI.
package main

import (
	"fmt"
	"os"

	"github.com/firmdura/bootlog-check/integration/internal/replayproof"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 2 && len(args) != 5 {
		return fmt.Errorf("usage: check list EXPECTED | check verify EXPECTED SCENARIO FORMAT REPORT")
	}
	cases, err := replayproof.Load(args[1])
	if err != nil {
		return err
	}
	if len(args) == 2 && args[0] == "list" {
		for _, c := range cases {
			fmt.Printf("%s %d\n", c.Name, c.Exit)
		}
		return nil
	}
	if len(args) == 5 && args[0] == "verify" {
		for _, c := range cases {
			if c.Name == args[2] {
				data, err := os.ReadFile(args[4])
				if err != nil {
					return err
				}
				return replayproof.Verify(c, args[3], data)
			}
		}
	}
	return fmt.Errorf("unknown command or scenario")
}

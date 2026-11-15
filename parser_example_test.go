package bootlog_test

import (
	"context"
	"errors"
	"fmt"
	"strings"

	bootlog "github.com/firmdura/bootlog-check"
)

func ExampleParseLog() {
	saved := "U-Boot 2026.01\n\n" +
		"[    0.001] Linux version demo\n" +
		"[1.250000001]\t APP_READY\n" +
		"[ OK ] console"
	log, err := bootlog.ParseLog(context.Background(), strings.NewReader(saved), bootlog.ParseOptions{})
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, record := range log.Records {
		if record.TimestampNS == nil {
			fmt.Printf("line %d: untimed %q\n", record.Line, record.Message)
		} else {
			fmt.Printf("line %d: %d ns %q\n", record.Line, *record.TimestampNS, record.Message)
		}
	}
	// Output:
	// line 1: untimed "U-Boot 2026.01"
	// line 3: 1000000 ns "Linux version demo"
	// line 4: 1250000001 ns " APP_READY"
	// line 5: untimed "[ OK ] console"
}

func ExampleParseLog_limits() {
	// Other zero-valued limits keep their defaults. The newline counts toward
	// MaxInputBytes, so the extra byte after "ready\n" exceeds this allowance.
	log, err := bootlog.ParseLog(context.Background(), strings.NewReader("ready\nx"), bootlog.ParseOptions{
		MaxInputBytes: 6,
	})
	fmt.Println("limit exceeded:", errors.Is(err, bootlog.ErrLimit))
	var limit *bootlog.LimitError
	if errors.As(err, &limit) {
		fmt.Printf("%s: %d\n", limit.Resource, limit.Limit)
	}
	fmt.Println("returned records:", len(log.Records))
	// Output:
	// limit exceeded: true
	// max_input_bytes: 6
	// returned records: 0
}

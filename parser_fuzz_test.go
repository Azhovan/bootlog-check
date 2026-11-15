package bootlog_test

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"

	bootlog "github.com/firmdura/bootlog-check"
)

func FuzzParseLog(f *testing.F) {
	for _, seed := range []string{
		"", "\n\r\n \t", "[0] start\n[1.000000001] ready\n", "[ OK ] ready\n",
		"[9223372036.854775807]\n", "[9223372036.854775808]\n", "[-1]\n", "[0.0000000000]",
		"[2] preamble\nuntimed\n[1] start", "\x1b[31m\x00世界\r", "[0] ready\n\xff",
		strings.Repeat("x", 256) + "\r\n", strings.Repeat("\n", 65), strings.Repeat("x\n", 2049),
	} {
		f.Add([]byte(seed), uint8(7))
	}
	f.Fuzz(func(t *testing.T, raw []byte, chunk uint8) {
		if len(raw) > 8192 {
			t.Skip()
		}
		opts := bootlog.ParseOptions{MaxInputBytes: 4096, MaxLines: 64, MaxLineBytes: 256}
		log, err := bootlog.ParseLog(context.Background(), bytes.NewReader(raw), opts)
		fragmented, fragmentedErr := bootlog.ParseLog(context.Background(), &parserChunkReader{input: raw, chunk: int(chunk) + 1}, opts)
		if (err == nil) != (fragmentedErr == nil) {
			t.Fatalf("read boundaries changed success: %v versus %v", err, fragmentedErr)
		}
		if err != nil {
			if !reflect.DeepEqual(log, bootlog.Log{}) || !reflect.DeepEqual(fragmented, bootlog.Log{}) {
				t.Fatal("failure returned partial records")
			}
			return
		}
		if err := log.Validate(); err != nil {
			t.Fatalf("successful model is invalid: %v", err)
		}
		if !reflect.DeepEqual(log, fragmented) {
			t.Fatal("read boundaries changed records")
		}
		if len(raw) > int(opts.MaxInputBytes) || len(log.Records) > int(opts.MaxLines) {
			t.Fatal("successful result exceeded input or record bounds")
		}
		for _, record := range log.Records {
			if record.Line > opts.MaxLines || len(record.Message) > int(opts.MaxLineBytes) {
				t.Fatal("successful record exceeded line bounds")
			}
		}
	})
}

// The result is intentionally retained through each iteration. Allocation bytes
// grow with accepted text and records; no constant-memory claim is made.
func BenchmarkParseLogRetained(b *testing.B) {
	for _, lines := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprint(lines), func(b *testing.B) {
			input := strings.Repeat("[1.000000001] "+strings.Repeat("x", 80)+"\n", lines)
			b.ReportAllocs()
			b.SetBytes(int64(len(input)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				log, err := bootlog.ParseLog(context.Background(), strings.NewReader(input), bootlog.ParseOptions{})
				if err != nil || len(log.Records) != lines {
					b.Fatalf("ParseLog: %v", err)
				}
				runtime.KeepAlive(log)
			}
		})
	}
}

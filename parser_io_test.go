package bootlog_test

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	bootlog "github.com/firmdura/bootlog-check"
)

type parserChunkReader struct {
	input []byte
	chunk int
	err   error
}

func (r *parserChunkReader) Read(p []byte) (int, error) {
	if len(p) > r.chunk {
		p = p[:r.chunk]
	}
	n := copy(p, r.input)
	r.input = r.input[n:]
	if len(r.input) == 0 {
		if r.err != nil {
			return n, r.err
		}
		return n, io.EOF
	}
	return n, nil
}

type parserReadFunc func([]byte) (int, error)

func (f parserReadFunc) Read(p []byte) (int, error) { return f(p) }

type parserReadCloser struct {
	io.Reader
	closed bool
}

func (r *parserReadCloser) Close() error { r.closed = true; return nil }

func TestParseLogReadBoundaries(t *testing.T) {
	input := "\r\nU-Boot ü世界\r\n[0.000000001] Linux version\r\n[1.25]\tAPP_READY🙂"
	want := bootlog.Log{Records: []bootlog.Record{
		{Line: 2, Message: "U-Boot ü世界"},
		{Line: 3, TimestampNS: parserNS(1), Message: "Linux version"},
		{Line: 4, TimestampNS: parserNS(1_250_000_000), Message: "APP_READY🙂"},
	}}
	for chunk := 1; chunk <= len(input)+1; chunk++ {
		got, err := bootlog.ParseLog(context.Background(), &parserChunkReader{input: []byte(input), chunk: chunk}, bootlog.ParseOptions{})
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("chunk %d: %#v, %v; want %#v", chunk, got, err, want)
		}
	}
	for _, reader := range []io.Reader{iotest.OneByteReader(strings.NewReader(input)), iotest.HalfReader(strings.NewReader(input)), iotest.DataErrReader(strings.NewReader(input))} {
		got, err := bootlog.ParseLog(context.Background(), reader, bootlog.ParseOptions{})
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("%T: %#v, %v", reader, got, err)
		}
	}
}

func TestParseLogReaderErrors(t *testing.T) {
	marker := errors.New("capture read failed")
	for _, cause := range []error{marker, io.ErrUnexpectedEOF} {
		for _, input := range []string{"", "preamble", "[0] start\n[1] APP_READY\n", "[0] start\n[1] APP_READY\ntail"} {
			for _, chunk := range []int{1, 4096} {
				log, err := bootlog.ParseLog(context.Background(), &parserChunkReader{input: []byte(input), chunk: chunk, err: cause}, bootlog.ParseOptions{})
				assertParserFailure(t, log, err, bootlog.ErrInvalidInput)
				if !errors.Is(err, cause) {
					t.Fatalf("reader cause was lost: %v", err)
				}
			}
		}
	}
	// The reader reaches the exact input allowance, then fails instead of EOF.
	log, err := bootlog.ParseLog(context.Background(), io.MultiReader(strings.NewReader("ok\n"), iotest.ErrReader(marker)), bootlog.ParseOptions{MaxInputBytes: 3})
	assertParserFailure(t, log, err, marker)
}

func TestParseLogCancellation(t *testing.T) {
	t.Run("before read", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		reader := parserReadFunc(func([]byte) (int, error) { t.Fatal("Read called after cancellation"); return 0, nil })
		log, err := bootlog.ParseLog(ctx, reader, bootlog.ParseOptions{})
		assertParserFailure(t, log, err, bootlog.ErrCanceled)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation cause was lost: %v", err)
		}
		var canceled *bootlog.CancellationError
		if !errors.As(err, &canceled) {
			t.Fatalf("missing cancellation type: %v", err)
		}
	})
	t.Run("deadline", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
		defer cancel()
		log, err := bootlog.ParseLog(ctx, strings.NewReader("[0] start"), bootlog.ParseOptions{})
		assertParserFailure(t, log, err, context.DeadlineExceeded)
	})
	for _, terminal := range []error{nil, io.EOF} {
		t.Run("during read", func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			reader := parserReadFunc(func(p []byte) (int, error) {
				calls++
				if calls > 1 {
					t.Fatal("another Read after cancellation")
				}
				cancel()
				return copy(p, "[0] start\n[1] APP_READY\n"), terminal
			})
			log, err := bootlog.ParseLog(ctx, reader, bootlog.ParseOptions{})
			assertParserFailure(t, log, err, bootlog.ErrCanceled)
		})
	}
}

func TestParseLogReaderOwnership(t *testing.T) {
	for _, input := range []string{"[0] start", "[+1] invalid", strings.Repeat("x", 10)} {
		reader := &parserReadCloser{Reader: strings.NewReader(input)}
		_, _ = bootlog.ParseLog(context.Background(), reader, bootlog.ParseOptions{MaxLineBytes: 9})
		if reader.closed {
			t.Fatal("ParseLog closed caller-owned reader")
		}
	}
}

func TestParseLogNoProgress(t *testing.T) {
	calls := 0
	reader := parserReadFunc(func([]byte) (int, error) {
		calls++
		if calls > 100 {
			t.Fatal("unbounded empty reads")
		}
		return 0, nil
	})
	log, err := bootlog.ParseLog(context.Background(), reader, bootlog.ParseOptions{})
	assertParserFailure(t, log, err, io.ErrNoProgress)
	// A transient empty read is permitted by io.Reader and is not EOF.
	log, err = bootlog.ParseLog(context.Background(), io.MultiReader(&parserTransientEmptyReader{}, strings.NewReader("x")), bootlog.ParseOptions{})
	if err != nil || len(log.Records) != 1 || log.Records[0].Message != "x" {
		t.Fatalf("transient empty read: %#v, %v", log, err)
	}
}

type parserTransientEmptyReader struct{ read bool }

func (r *parserTransientEmptyReader) Read([]byte) (int, error) {
	if r.read {
		return 0, io.EOF
	}
	r.read = true
	return 0, nil
}

func TestParseLogStopsReadingAtLimits(t *testing.T) {
	for _, test := range []struct {
		opts      bootlog.ParseOptions
		byteValue byte
		maxRead   int
	}{
		{bootlog.ParseOptions{MaxInputBytes: 7}, 'x', 8},
		{bootlog.ParseOptions{MaxLineBytes: 7}, 'x', 4096},
		{bootlog.ParseOptions{MaxLines: 7}, '\n', 4096},
	} {
		read := 0
		reader := parserReadFunc(func(p []byte) (int, error) {
			read += len(p)
			if read > test.maxRead {
				t.Fatalf("read beyond bounded allowance: %d", read)
			}
			for i := range p {
				p[i] = test.byteValue
			}
			return len(p), nil
		})
		log, err := bootlog.ParseLog(context.Background(), reader, test.opts)
		assertParserFailure(t, log, err, bootlog.ErrLimit)
	}
}

func TestParseLogInvalidArguments(t *testing.T) {
	for _, opts := range []bootlog.ParseOptions{{MaxInputBytes: -1}, {MaxLines: -1}, {MaxLineBytes: -1}} {
		log, err := bootlog.ParseLog(context.Background(), strings.NewReader(""), opts)
		assertParserFailure(t, log, err, bootlog.ErrInvalidInput)
	}
	log, err := bootlog.ParseLog(nil, strings.NewReader(""), bootlog.ParseOptions{})
	assertParserFailure(t, log, err, bootlog.ErrInvalidInput)
	log, err = bootlog.ParseLog(context.Background(), nil, bootlog.ParseOptions{})
	assertParserFailure(t, log, err, bootlog.ErrInvalidInput)
	for _, count := range []int{-1, 4097} {
		reader := parserReadFunc(func([]byte) (int, error) { return count, nil })
		log, err := bootlog.ParseLog(context.Background(), reader, bootlog.ParseOptions{})
		assertParserFailure(t, log, err, bootlog.ErrInvalidInput)
	}
}

func TestParseLogBootFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/parser/mixed-boot.log")
	if err != nil {
		t.Fatal(err)
	}
	want := bootlog.Log{Records: []bootlog.Record{
		{Line: 1, Message: "U-Boot 2024.01 (synthetic parser fixture)"},
		{Line: 2, Message: "DRAM:  512 MiB"},
		{Line: 4, Message: "Starting kernel ..."},
		{Line: 6, TimestampNS: parserNS(0), Message: "Booting Linux on physical CPU 0x0"},
		{Line: 7, TimestampNS: parserNS(0), Message: "Linux version fixture"},
		{Line: 8, TimestampNS: parserNS(125_000_000), Message: "printk: console [ttyAMA0] enabled"},
		{Line: 9, Message: "[ OK ] Started fixture service"},
		{Line: 10, TimestampNS: parserNS(1_000_000_001), Message: "NETWORK_READY"},
		{Line: 11, TimestampNS: parserNS(1_250_000_000), Message: "APP_READY"},
		{Line: 12, TimestampNS: parserNS(1_500_000_000), Message: "post-ready diagnostic"},
	}}
	for _, input := range []string{string(raw), strings.ReplaceAll(string(raw), "\n", "\r\n")} {
		log, err := bootlog.ParseLog(context.Background(), strings.NewReader(input), bootlog.ParseOptions{})
		if err != nil || !reflect.DeepEqual(log, want) {
			t.Fatalf("fixture: %#v, %v; want %#v", log, err, want)
		}
	}
}

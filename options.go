package bootlog

const (
	DefaultMaxInputBytes int64 = 16 << 20
	DefaultMaxLines      int64 = 100_000
	DefaultMaxLineBytes  int64 = 64 << 10
	MaxRulesBytes        int64 = 1 << 20
	MaxUserRules               = 128
	MaxPatternBytes            = 4096
	NanosecondsPerMS     int64 = 1_000_000
	MaxWithinMS          int64 = (1<<63 - 1) / NanosecondsPerMS
)

// ParseOptions bounds retained input. Zero selects the corresponding default;
// negatives are invalid. Limits are independent and need not have equal ratios.
type ParseOptions struct {
	MaxInputBytes int64
	MaxLines      int64
	MaxLineBytes  int64
}

// Normalize validates options and returns a copy with zero values defaulted.
func (o ParseOptions) Normalize() (ParseOptions, error) {
	fields := []struct {
		name     string
		value    *int64
		fallback int64
	}{
		{"max_input_bytes", &o.MaxInputBytes, DefaultMaxInputBytes},
		{"max_lines", &o.MaxLines, DefaultMaxLines},
		{"max_line_bytes", &o.MaxLineBytes, DefaultMaxLineBytes},
	}
	for _, f := range fields {
		if *f.value < 0 {
			return ParseOptions{}, &InvalidInputError{Field: f.name, Detail: "must not be negative"}
		}
		if *f.value == 0 {
			*f.value = f.fallback
		}
	}
	return o, nil
}

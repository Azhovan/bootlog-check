package bootlog

// parseLogTimestamp distinguishes ordinary bracketed text from timestamp-looking
// prefixes. Only ASCII spaces/tabs are padding and only ASCII digits are numeric.
func parseLogTimestamp(line []byte, lineNumber int64) (*int64, []byte, error) {
	if line[0] != '[' {
		return nil, line, nil
	}
	i := 1
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	if i == len(line) || !(logDigit(line[i]) || line[i] == '+' || line[i] == '-' || line[i] == '.') {
		return nil, line, nil
	}
	invalid := func(detail string) (*int64, []byte, error) {
		return nil, nil, &InvalidInputError{Line: lineNumber, Field: "timestamp_ns", Detail: detail}
	}
	if !logDigit(line[i]) {
		return invalid("timestamp must start with a nonnegative decimal integer")
	}
	const second int64 = 1_000_000_000
	const maxNS int64 = 1<<63 - 1
	var seconds int64
	for i < len(line) && logDigit(line[i]) {
		digit := int64(line[i] - '0')
		if seconds > (maxNS/second-digit)/10 {
			return invalid("timestamp overflows int64 nanoseconds")
		}
		seconds = seconds*10 + digit
		i++
	}
	var fraction int64
	if i < len(line) && line[i] == '.' {
		i++
		start := i
		for i < len(line) && logDigit(line[i]) {
			if i-start == 9 {
				return invalid("timestamp fraction must have 1 through 9 digits")
			}
			fraction = fraction*10 + int64(line[i]-'0')
			i++
		}
		if i == start {
			return invalid("timestamp fraction must have 1 through 9 digits")
		}
		for digits := i - start; digits < 9; digits++ {
			fraction *= 10
		}
	}
	if i == len(line) || line[i] != ']' {
		return invalid("timestamp must end with a closing bracket immediately after the number")
	}
	if seconds == maxNS/second && fraction > maxNS%second {
		return invalid("timestamp overflows int64 nanoseconds")
	}
	timestamp := seconds*second + fraction
	i++
	if i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return &timestamp, line[i:], nil
}

func logDigit(b byte) bool { return b >= '0' && b <= '9' }

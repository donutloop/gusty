package lang

// Level is a diagnostic severity.
type Level string

const (
	LevelInfo    Level = "info"
	LevelWarning Level = "warning"
	LevelError   Level = "error"
)

// Diagnostic is a JSON-serializable compiler diagnostic.
type Diagnostic struct {
	Level      Level  `json:"level"`
	Span       Span   `json:"span"`
	Msg        string `json:"msg"`
	Suggestion string `json:"suggestion,omitempty"`
}

func (d Diagnostic) Error() string {
	if d.Span.IsZero() {
		return string(d.Level) + ": " + d.Msg
	}
	return string(d.Level) + " at " + spanStr(d.Span) + ": " + d.Msg
}

// spanStr renders a span as "line:col".
func spanStr(s Span) string {
	return itoa(s.Line) + ":" + itoa(s.Col)
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [32]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

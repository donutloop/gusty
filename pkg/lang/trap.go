package lang

import (
	"fmt"
	"strings"
)

// TrapError is a program's exception, named by class and message, that the compiler itself
// has to talk about. Two things reach for it: a constant fold that sees the operation would
// trap (`1 // 0`, `math.sqrt(-1)`, `(-8) ** (1/3)`), and a refusal that has to say which
// CPython class the program would have raised had the fold been able to answer.
//
// It is called TrapError rather than the backend's old name because nothing evaluates the
// program any more to raise it: the compiled backend emits the trap into the module, and the
// target raises it at run time with the same class and message (ADR 0211's failure classes,
// ADR 0214's machine-readable exception). ADR 0302 retired the second execution path; this
// type is what survived it, because the *class name* is language surface, not backend surface.
type TrapError struct {
	Msg     string
	ExnType string
	ExnMsg  string
	// Traceback is the frame stack the trap was reported through. The compiled backend builds
	// one: the program carries a shadow stack of its own, the raise copies it, and rt_die prints
	// it (Gap K.8, ADR 0354). ParseTrapFrames is the host reading that report back, so the CLI's
	// `traceback` and `frames` members are the same frames the terminal saw, in the same order.
	Traceback []Frame `json:"traceback,omitempty"`
}

// Frame is one entry of a trap's frame stack: the function, and the position in the source.
//
// Col is zero for every frame the compiled runtime reports: the shadow stack stores the line
// the call is written on, which is the position a traceback is read for, and a column the
// compiler does not track would be a number the report invented.
type Frame struct {
	Name string `json:"name"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
}

// frameLineRe is the shape rt_die writes a frame: the caption the reference trained everyone
// to read, with the file the module names, the line, and the function the program wrote.
//
// ParseTrapFrames reads the frames back that ParseTrapReport skips: the same transcript, the
// class read by the one door and the history read by this one, so the two can never tell two
// stories about one run (ADR 0214's machine-readable exception, extended by ADR 0354).
func ParseTrapFrames(stderr string) []Frame {
	var out []Frame
	for _, raw := range strings.Split(stderr, "\n") {
		l := strings.TrimSpace(raw)
		if !strings.HasPrefix(l, "File \"") {
			continue
		}
		// File "prog", line 5, in outer
		rest := l[len("File \""):]
		q := strings.Index(rest, "\"")
		if q < 0 {
			continue
		}
		rest = rest[q+1:]
		const lineCaption = ", line "
		i := strings.Index(rest, lineCaption)
		if i < 0 {
			continue
		}
		num := rest[i+len(lineCaption):]
		j := strings.Index(num, ", in ")
		if j < 0 {
			continue
		}
		line := 0
		for _, c := range num[:j] {
			if c < '0' || c > '9' {
				line = -1
				break
			}
			line = line*10 + int(c-'0')
		}
		if line < 0 {
			continue
		}
		out = append(out, Frame{Name: num[j+len(", in "):], Line: line})
	}
	return out
}

func (e *TrapError) Error() string { return "program traps: " + e.Msg }

// trapError builds a TrapError carrying a typed exception (class name + message). It is the
// only way a fold reports "the reference raises here", so the class the program's `except`
// matches on is never lost between the fold and the diagnostic (ADR 0214).
func trapError(exnType, msg string) *TrapError {
	return &TrapError{Msg: msg, ExnType: exnType, ExnMsg: msg}
}

// ParseTrapReport reads the report a trapped program wrote to its stderr and returns the line
// that carries the exception, plus its class and message split apart.
//
// The runtime owns the words (rt_die writes CPython's own `Class: message` sentence, preceded by
// a `Traceback (most recent call last):` header and the frames it has); this is the host's half
// of reading them back out, so the CLI's machine path can hand an agent the class as data rather
// than making it scrape prose (ADR 0214). The last line that looks like an exception is the
// answer, because the frames above it are the program's history, not its failure.
//
// ok=false means the transcript holds no exception report at all — a program that exited
// non-zero for some other reason. Callers must not invent a class in that case.
func ParseTrapReport(stderr string) (line, class, message string) {
	for _, raw := range strings.Split(stderr, "\n") {
		l := strings.TrimSpace(raw)
		if l == "" || strings.HasPrefix(l, "Traceback (most recent call last):") || strings.HasPrefix(l, "File ") || strings.HasPrefix(l, "  File ") {
			continue
		}
		cls, msg, split := strings.Cut(l, ": ")
		if !split || !looksLikeExceptionName(cls) {
			continue
		}
		line, class, message = l, cls, msg
	}
	if class == "" {
		return "", "", ""
	}
	return line, class, message
}

// looksLikeExceptionName guards ParseTrapReport against reading an ordinary printed line as an
// exception: only an identifier-shaped, class-named word before the colon is a class.
func looksLikeExceptionName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_' && i > 0:
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z' && i > 0:
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// RenderTraceback prints the frame stack the host has, in the shape Python trained everyone to
// read: a header, one `File "prog", line N, in NAME` per frame, and the class and message last.
//
// It lives with TrapError rather than with the engine that used to build the stack, because the
// words belong to the report and not to the thing that raised it — the CLI prints this, and a
// traceback the host can assemble from a runtime report prints through the same door (ADR 0231).
// The frames themselves are Gap K.8's to supply.
func (e *TrapError) RenderTraceback() string {
	if len(e.Traceback) == 0 {
		return ""
	}
	out := "Traceback (most recent call last):\n"
	for _, f := range e.Traceback {
		out += fmt.Sprintf("  File \"prog\", line %d, in %s\n", f.Line, f.Name)
	}
	// Python's last line is `ValueError: boom`, and so is ours: the class is what an
	// `except IndexError:` matched on, so dropping it would make the report less specific than the
	// raise that produced it.
	if e.ExnType != "" && e.Msg != e.ExnType {
		out += e.ExnType + ": " + e.Msg
	} else {
		out += e.Msg
	}
	return out
}

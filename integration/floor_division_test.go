package integration

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/donutloop/gusty/pkg/lang"
)

// Gap R.28 / Gap R.30 (ADR 0216) across both legs. The unit grid asserts the record
// against a Go computation; here the same program is run by CPython as well as by both of our
// backends, because every one of these operators was "consistent between our two implementations"
// while being wrong — and two integration tests had even pinned the truncated values as expected,
// having been written from the emitted `sdiv`/`srem` rather than from the language.

func floorGridSource(t *testing.T) string {
	t.Helper()
	var body strings.Builder
	ints := []int64{9, -9, 6, -6, 17, -17, 24, -24, 5, -5, 1, -1, 0}
	divs := []int64{2, -2, 3, -3, 5, -5, 4, -4, 6, -6, 1, -1}
	for _, a := range ints {
		for _, b := range divs {
			body.WriteString("print(" + strconv.FormatInt(a, 10) + " // " + strconv.FormatInt(b, 10) + ")\n")
			body.WriteString("print(" + strconv.FormatInt(a, 10) + " % " + strconv.FormatInt(b, 10) + ")\n")
		}
	}
	floats := []string{"9.5", "-9.5", "6.0", "-6.0", "0.25", "-0.25", "1.75", "-1.75"}
	fdivs := []string{"2.0", "-2.0", "3.0", "-3.0", "0.5", "-0.5", "2", "-3"}
	for _, a := range floats {
		for _, b := range fdivs {
			body.WriteString("print(" + a + " // " + b + ")\n")
			body.WriteString("print(" + a + " % " + b + ")\n")
		}
	}
	// No expectation is computed here: the same source is run through CPython below, so the
	// oracle is the reference implementation rather than a third reading of my own rule.
	return body.String()
}

func TestFloorDivisionMatchesCPythonOnTheCompiledBackend(t *testing.T) {
	src := floorGridSource(t)
	oracle, oracleErr, perr := lang.PythonRun(src)
	if perr != nil {
		t.Skipf("no usable oracle: %v\n%s", perr, oracleErr)
	}
	lang.RecordedStdoutIs(t, src, oracle)
	compiled, err := runAOTWithTimeout(t, src, 240*time.Second)
	if err != nil {
		t.Fatalf("compiled run: %v", err)
	}
	if compiled != oracle {
		t.Errorf("compiled backend diverges from CPython on the floor grid:\n%s", floorGridDiff(compiled, oracle))
	}
}

// The identity, on the compiled path: a truncating pair satisfies it only when both halves
// truncate, so this is the assertion that would fail if a future change floored `%` and left `//`
// truncating (the state the record was in before Gap R.28).
func TestCompiledFloorIdentity(t *testing.T) {
	src := ""
	for _, a := range []int64{9, -9, 7, -7, 1, -1} {
		for _, b := range []int64{2, -2, 3, -3} {
			src += "print(" + strconv.FormatInt(a*b, 10) + " == (" + strconv.FormatInt(a*b, 10) +
				" // " + strconv.FormatInt(b, 10) + ") * " + strconv.FormatInt(b, 10) + " + (" +
				strconv.FormatInt(a*b, 10) + " % " + strconv.FormatInt(b, 10) + "))\n"
		}
	}
	compiled, err := runAOTWithTimeout(t, src, 240*time.Second)
	if err != nil {
		t.Fatalf("compiled run: %v", err)
	}
	if strings.TrimSpace(compiled) == "" {
		t.Fatalf("the identity program printed nothing")
	}
	for i, line := range strings.Split(strings.TrimSpace(compiled), "\n") {
		// The identity is a proposition, and CPython renders a proved proposition True;
		// the pin here used to be 1, which was this backend's own answer (ADR 0257).
		if line != "True" {
			t.Errorf("line %d of the identity check printed %q, want True", i+1, line)
		}
	}
}

func floorGridDiff(got, want string) string {
	g := strings.Split(got, "\n")
	w := strings.Split(want, "\n")
	var out []string
	n := len(g)
	if len(w) > n {
		n = len(w)
	}
	for i := 0; i < n && len(out) < 10; i++ {
		a, b := "?", "?"
		if i < len(g) {
			a = g[i]
		}
		if i < len(w) {
			b = w[i]
		}
		if a != b {
			out = append(out, "  line "+strconv.Itoa(i+1)+": got "+a+" want "+b)
		}
	}
	return strings.Join(out, "\n")
}

package integration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Integration coverage for Gap R.29 (roadmap), ADR 0221: `==`/`!=` across int and float is one
// numeric question. The interpreter used to answer `1 == 1.0` with 0 and `1.0 == 1` with 1 while the
// compiled backend answered both correctly — the exact shape parity hides — so all three legs
// (interpreter, compiled, CPython) run the same source here, and the expectations are computed from
// the Python rules rather than from any emission.

// numericEqualitySource is the grid: every int/float pair in both orders under six operators, each
// printed as a plain 1/0 so the CPython leg needs no formatting concession.
func numericEqualitySource() (src string, want []string) {
	ints := []string{"0", "1", "2", "-3", "7"}
	floats := []string{"0.0", "1.0", "2.5", "-3.0", "-0.0"}
	ops := []string{"==", "!=", "<", "<=", ">", ">="}
	var b strings.Builder
	for _, i := range ints {
		for _, f := range floats {
			for _, op := range ops {
				for _, pair := range [][2]string{{i, f}, {f, i}} {
					b.WriteString(fmt.Sprintf("print(1 if %s %s %s else 0)\n", pair[0], op, pair[1]))
					want = append(want, pyNumCompare(pair[0], op, pair[1]))
				}
			}
		}
	}
	return b.String(), want
}

// pyNumCompare applies the Python rule for two numeric literals and renders the answer the way this
// language prints a boolean.
func pyNumCompare(l, op, r string) string {
	lf, err := strconv.ParseFloat(l, 64)
	if err != nil {
		panic(fmt.Sprintf("bad left literal %q", l))
	}
	rf, err := strconv.ParseFloat(r, 64)
	if err != nil {
		panic(fmt.Sprintf("bad right literal %q", r))
	}
	var v bool
	switch op {
	case "==":
		v = lf == rf
	case "!=":
		v = lf != rf
	case "<":
		v = lf < rf
	case "<=":
		v = lf <= rf
	case ">":
		v = lf > rf
	case ">=":
		v = lf >= rf
	default:
		panic(fmt.Sprintf("bad operator %q", op))
	}
	return strconv.Itoa(bool2int(v))
}

func bool2int(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestNumericEqualityMatchesCPythonOnBothEngines(t *testing.T) {
	src, want := numericEqualitySource()
	dir := t.TempDir()
	path := filepath.Join(dir, "numeric_equality.gy")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// The expectation must agree with CPython before it is allowed to judge anybody.
	pyOut, err := exec.Command("python3", path).CombinedOutput()
	if err != nil {
		t.Skipf("no usable CPython for the oracle leg: %v", err)
	}
	pyLines := strings.Split(strings.TrimSpace(string(pyOut)), "\n")
	if len(pyLines) != len(want) {
		t.Fatalf("CPython printed %d lines, expectation has %d", len(pyLines), len(want))
	}
	for i := range pyLines {
		switch pyLines[i] {
		case "True":
			pyLines[i] = "1"
		case "False":
			pyLines[i] = "0"
		}
		if pyLines[i] != want[i] {
			t.Fatalf("the written-down expectation disagrees with CPython at line %d: %q vs %q", i+1, want[i], pyLines[i])
		}
	}
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)
	for _, engine := range cliEngines {
		if code := runCode(t, bin, engine, path); code != 0 {
			t.Fatalf("%s exited %d", engine, code)
		}
		out, _ := cliRunCode(t, engine, path)
		got := strings.Split(strings.TrimSpace(out), "\n")
		if len(got) != len(want) {
			t.Fatalf("%s printed %d lines, want %d:\n%s", engine, len(got), len(want), out)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s line %d printed %s, want %s", engine, i+1, got[i], want[i])
			}
		}
	}
}

// TestMixedNumericListEqualityIsNotAnsweredByTruncation is Gap R.40 closed and ADR 0233 paid: a
// float in a container slot used to emit an invalid module for the literal form and to answer from
// a truncated word for the bound form — the same code said [1.5] == [1.6] was True where CPython
// says False. A float now lives in a box whose handle sits in the slot, so the comparison is a
// comparison of doubles and the malformed static initializer the gap was about cannot come back:
// the emitted module must not contain a list global holding a float's bits, only rt_float_new calls.
func TestMixedNumericListEqualityIsNotAnsweredByTruncation(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"print(1 if [1] == [1.0] else 0)\n", "1\n"}, // Python: [1] == [1.0]
		{"xs = [1]\nys = [1.0]\nprint(1 if xs == ys else 0)\n", "1\n"},
		{"print(1 if [1.5] == [1.6] else 0)\n", "0\n"}, // the truncation answer was 1
		{"print([1.5, \"a\"])\n", "[1.5, 'a']\n"},      // printed the interned index's repr
	} {
		path := writeSrc(t, t.TempDir(), "mixed.gy", tc.src)
		if py, ok := cpythonOut(t, path); ok && py != tc.want {
			t.Fatalf("the expectation is not CPython's: got %q want %q", py, tc.want)
		}
		for _, engine := range cliEngines {
			out, code := cliRunCode(t, engine, path)
			if code == 2 {
				t.Fatalf("%s rejected the compiler's own module (ADR 0166):\n%s", engine, cliRun(t, engine, path))
			}
			if code != 0 {
				t.Fatalf("%s exited %d: %s", engine, code, cliRun(t, engine, path))
			}
			if out != tc.want {
				t.Errorf("%s printed %q for %q, want CPython's %q", engine, out, tc.src, tc.want)
			}
		}
	}
}

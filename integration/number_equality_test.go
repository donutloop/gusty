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
	for _, engine := range []string{"--interp", "--aot"} {
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

// TestMixedNumericListEqualityRefusesRatherThanTruncates is Gap R.40 closed (ADR 0226). This test used
// to pin two defects: a *literal* list holding a float emitted an invalid module
// (`@.lst2 = private global {i32, [1 x i32]} { i32 1, [1 x i32] [@env_store = ...`, llc: "expected
// type") so the user got a toolchain rejection for an ordinary program; and the *bound* form "worked",
// which this cycle established was truncation -- the same code said [1.5] == [1.6] was True where
// CPython says False. Both halves now refuse with something to act on; the answers belong to L11.6, and
// integration/programs/probe_float_container_equality.gy is the debt row that keeps them visible.
func TestMixedNumericListEqualityRefusesRatherThanTruncates(t *testing.T) {
	for _, src := range []string{
		"print(1 if [1] == [1.0] else 0)\n",
		"xs = [1]\nys = [1.0]\nprint(1 if xs == ys else 0)\n",
		"print(1 if [1.5] == [1.6] else 0)\n",
	} {
		path := writeSrc(t, t.TempDir(), "mixed.gy", src)
		out, code := cliRunCode(t, "--aot", path)
		if code == 2 {
			t.Fatalf("%q rejected the compiler's own module (ADR 0166):\n%s", src, cliRun(t, "--aot", path))
		}
		if code == 0 {
			t.Fatalf("%q compiled and printed %q; a float in a container slot must refuse until L11.6, not answer from a truncated word", src, out)
		}
		msg := cliRun(t, "--aot", path)
		if !strings.Contains(msg, "float") || !strings.Contains(msg, "container") {
			t.Fatalf("%q refused without naming the kind and the slot:\n%s", src, msg)
		}
	}
	// The artifact assertion, flipped: the malformed initializer this gap was about must no longer be
	// emittable at all -- the refusal happens before anything reaches the module.
	ir, irc := cliRunCode(t, "--emit-llvm", "print(1 if [1] == [1.0] else 0)\n")
	if irc == 0 && strings.Contains(ir, "[1 x i32] [@") {
		t.Fatalf("the malformed list initializer is still being emitted:\n%s", ir)
	}
}

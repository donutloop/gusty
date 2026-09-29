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

// TestMixedNumericListEqualityIsAPinnedCompiledDebt records the part of this rule the compiled backend
// still gets wrong in the worst of the two available ways: a *literal* list containing a float emits
// an invalid module (`@.lst2 = private global {i32, [1 x i32]} { i32 1, [1 x i32] [@env_store = ...`,
// llc: "expected type"), so the user sees a toolchain rejection (exit 2) instead of an answer or a
// refusal — the Gap K.10 / ADR 0166 class. The same comparison through variables is correct, which is
// why this is a codegen emitter bug and not a semantics one. Delete the exit-2 assertion when the
// module verifies; roadmap Gap R.40.
func TestMixedNumericListEqualityIsAPinnedCompiledDebt(t *testing.T) {
	dir := t.TempDir()
	literal := filepath.Join(dir, "mixed_literal.gy")
	if err := os.WriteFile(literal, []byte("print(1 if [1] == [1.0] else 0)\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)
	// Through variables, the compiled backend already answers this correctly.
	bound := filepath.Join(dir, "mixed_bound.gy")
	if err := os.WriteFile(bound, []byte("xs = [1]\nys = [1.0]\nprint(1 if xs == ys else 0)\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if code := runCode(t, bin, "--interp", bound); code != 0 {
		t.Fatalf("interp exited %d; the interpreter answers the bound form correctly", code)
	}
	if code := runCode(t, bin, "--aot", bound); code != 0 {
		t.Fatalf("compiled bound form exited %d; it is expected to work", code)
	}
	out, code := cliRunCode(t, "--aot", literal)
	if code != 2 {
		t.Fatalf("the literal form exited %d, want the recorded toolchain rejection (2):\n%s", code, out)
	}
	// Assert the artifact, not the tool's prose: the emitted module is what is broken, and this is
	// the line llc refuses — an int-typed array element position holding a `@` with no name.
	ir, irc := cliRunCode(t, "--emit-llvm", "print(1 if [1] == [1.0] else 0)\n")
	if irc != 0 {
		t.Fatalf("--emit-llvm exited %d:\n%s", irc, ir)
	}
	if !strings.Contains(ir, "[1 x i32] [@") {
		t.Fatalf("the emitted module no longer contains the malformed list initializer this gap is about:\n%s", ir)
	}
}

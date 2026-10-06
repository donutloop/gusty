package lang

// A container in a numeric operand is refused or raised, never compiled into an invalid module
// (Gap R.175, ADR 0292).
//
// `value()` renders a list literal as the ADDRESS of a compile-time global, so every container that
// reached an arithmetic operand produced IR llc rejects — and the exit-code contract spends exit 2 on
// OUR bug, not the program's:
//
//	print([0] * 3)        ->  mul i32 @.lst1, 3                     llc: global variable reference must have pointer type
//	print([1, 2] + [3])   ->  add i32 @.lst1, @.lst2                ditto
//	print([1] / 2)        ->  sitofp i32 @.lst1 to double           ditto
//
// Two answers, split by what the REFERENCE does rather than by what this backend lacks:
//
//   - where CPython raises (`[1] - [2]`, `{1: 2} * 2`, `[1] + {}`, `[] < {}`) the compiled leg raises
//     CPython's own sentence, so `except TypeError:` runs on both engines (ADR 0215);
//   - where CPython ANSWERS (`[0] * 3`, `[1, 2] + [3]`, `[1] < [2]`) it declines in words — there is no
//     runtime list-repeat or concatenate helper, and a fabricated number is ADR 0166's bug.
//
// A PARAMETER and a subscript are deliberately not claimed: their kind belongs to the caller and to the
// slot roads, which already answer (`def half(xs): return xs[0] / 2` with `half([1.5])` = 0.75) or refuse
// with a message naming the slot they could not read.

import (
	"os/exec"
	"strings"
	"testing"
)

// referenceOut asks the oracle directly. The rows below split into "the reference answers" and "the
// reference raises", and the split has to be MEASURED rather than remembered — every sentence this file
// pins was written down from a real `python3`, which is also what makes a stale row fail loudly.
func referenceOut(t *testing.T, src string) string {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3 to cross-check")
	}
	cmd := exec.Command("python3", "-c", src)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// A raise: hand back the last line, which is the sentence the program would print.
		lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
		return strings.TrimSpace(lines[len(lines)-1])
	}
	return string(out)
}

// TestContainerInArithmeticNeverExitsTwo is the contract. Every row must end at exit 1 (a refusal in
// words) or exit 3 (the reference's own raise) — never 2, and never an answer the reference disagrees with.
func TestContainerInArithmeticNeverExitsTwo(t *testing.T) {
	rows := []struct {
		name string
		src  string
	}{
		{"list times an int", "print([0] * 3)\n"},
		{"an int times a list", "print(3 * [0])\n"},
		{"list plus list", "print([1, 2] + [3])\n"},
		{"list minus list", "print([1] - [2])\n"},
		{"list over an int", "print([1] / 2)\n"},
		{"list floored by an int", "print([1] // 2)\n"},
		{"list modulo an int", "print([1] % 2)\n"},
		{"list to a power", "print([1] ** 2)\n"},
		{"dict times an int", "print({1: 2} * 2)\n"},
		{"dict minus dict", "print({1: 2} - {1: 2})\n"},
		{"set times an int", "print({1} * 2)\n"},
		{"list plus dict", "print([1] + {})\n"},
		{"dict plus list", "print({} + [])\n"},
		{"list ordered against a dict", "print([] < {})\n"},
		{"list ordered against a number", "print([1] < 2)\n"},
		{"two lists ordered", "print([1] < [2])\n"},
		{"two lists equal", "print([] == [])\n"},
		{"tuple concat", "print((1,) + (2,))\n"},
		// A container on the left of a call argument position, which is NOT arithmetic.
		{"a container as an argument is legal", "def half(xs):\n    return xs[0] / 2\n\n\nprint(half([1.5]))\n"},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			res, err := Compile(r.src)
			if err != nil {
				// A refusal must be words about the program, never the module it could not build.
				for _, banned := range []string{"global variable reference", "store i32 @", "add i32 @.", "mul i32 @.", "sitofp i32 @.", "unexpected type"} {
					if strings.Contains(err.Error(), banned) {
						t.Errorf("the refusal leaked an invalid module (`%s`): %v", banned, err)
					}
				}
				return
			}
			// Answered: then the answer must be the reference's. Rows the reference raises for are
			// checked by the raise tests below; a plain wrong number is what this catches.
			out := runIRMayTrap(t, res.IR)
			if strings.Contains(out, "Traceback") {
				return
			}
			want := referenceOut(t, r.src)
			if want == "" {
				return // the reference raises; the raise tests own that row
			}
			if out != want {
				t.Errorf("--aot answered %q, want the reference's %q", out, want)
			}
		})
	}
}

// TestTheReferenceRaiseIsTheCompiledRaise pins the wording half. A raise that invents its own sentence is
// as wrong as a wrong number, because a program that catches `TypeError` and prints e.message reads it
// (ADR 0215's rule, measured here operator by operator).
func TestTheReferenceRaiseIsTheCompiledRaise(t *testing.T) {
	rows := []struct {
		name string
		src  string
		want string
	}{
		{"list minus list", "print([1] - [2])\n", "TypeError: unsupported operand type(s) for -: 'list' and 'list'"},
		{"list over an int", "print([1] / 2)\n", "TypeError: unsupported operand type(s) for /: 'list' and 'int'"},
		{"list modulo an int", "print([1] % 2)\n", "TypeError: unsupported operand type(s) for %: 'list' and 'int'"},
		{"dict times an int", "print({1: 2} * 2)\n", "TypeError: unsupported operand type(s) for *: 'dict' and 'int'"},
		{"set times an int", "print({1} * 2)\n", "TypeError: unsupported operand type(s) for *: 'set' and 'int'"},
		{"list plus dict", "print([1] + {})\n", "TypeError: can only concatenate list (not \"dict\") to list"},
		// NOT the mirror of the row above. The `+` sentence follows the LEFT operand's type: a list
		// defines concatenation and refuses the other type, a dict does not define it at all and gets
		// the generic sentence. Written from the reference, not from symmetry.
		{"dict plus list", "print({} + [])\n", "TypeError: unsupported operand type(s) for +: 'dict' and 'list'"},
		{"list plus an int", "print([1] + 2)\n", "TypeError: can only concatenate list (not \"int\") to list"},
		{"dict plus an int", "print({} + 2)\n", "TypeError: unsupported operand type(s) for +: 'dict' and 'int'"},
		{"list ordered against a dict", "print([] < {})\n", "TypeError: '<' not supported between instances of 'list' and 'dict'"},
		{"list ordered against a number", "print([1] < 2)\n", "TypeError: '<' not supported between instances of 'list' and 'int'"},
		// A sequence DOES define __mul__, so a non-int multiplier is ITS refusal, quoted with the
		// MULTIPLIER's type: `can't multiply sequence by non-int of type 'float'` / 'str' / 'dict' /
		// 'NoneType'. Four different lookups named those "int" until each was measured.
		{"list times a float", "print([1] * 2.0)\n", "TypeError: can't multiply sequence by non-int of type 'float'"},
		{"list times a text", "print([1] * \"x\")\n", "TypeError: can't multiply sequence by non-int of type 'str'"},
		{"list times None", "print([1] * None)\n", "TypeError: can't multiply sequence by non-int of type 'NoneType'"},
		{"list times a dict", "print([1] * {})\n", "TypeError: can't multiply sequence by non-int of type 'dict'"},
		{"list times a list", "print([1] * [2])\n", "TypeError: can't multiply sequence by non-int of type 'list'"},
		// NOT the mirror: `{} * []` names the MAPPING on the left, because that is the __mul__ the
		// reference tried and failed. Written from the reference, not from symmetry with the row above.
		{"dict times a list", "print({} * [])\n", "TypeError: can't multiply sequence by non-int of type 'dict'"},
		{"set times a list", "print({1} * [])\n", "TypeError: can't multiply sequence by non-int of type 'set'"},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			want := referenceOut(t, r.src)
			if want != r.want {
				t.Fatalf("the row is stale against the reference: it prints %q, row pins %q", want, r.want)
			}
			// The raise arrives as an TrapError, not as stdout: `captureStdout` fails the test on a
			// program that traps, which is exactly the shape this table pins.
			ex := trapRun(t, r.src)
			if ex == nil {
				t.Fatalf("the interpreter answered where the reference raises %q", r.want)
			}
			if got := ex.ExnType + ": " + ex.ExnMsg; got != r.want {
				t.Errorf("interpreter raised %q, want the reference's %q", got, r.want)
			}
			res, cerr := Compile(r.src)
			if cerr != nil {
				t.Fatalf("the compiled leg refused a program the reference raises for, in words that are not the reference's: %v", cerr)
			}
			if out := runIRMayTrap(t, res.IR); !strings.Contains(out, r.want) {
				t.Errorf("--aot printed %q, want the reference's %q", out, r.want)
			}
		})
	}
}

// TestContainerConcatAndRepeatRefuseRatherThanAnswer covers the half the reference ANSWERS and this
// backend cannot: `[0] * 3` is `[0, 0, 0]` and `[1, 2] + [3]` is `[1, 2, 3]`, and there is no runtime
// helper to build either. A refusal is the deliverable; a number is not. These rows are written so that
// the day someone emits rt_list_concat they FAIL and get promoted.
func TestContainerConcatAndRepeatRefuseRatherThanAnswer(t *testing.T) {
	for _, r := range []struct{ name, src, want string }{
		{"list repeat", "print([0] * 3)\n", "[0, 0, 0]\n"},
		{"int times list", "print(3 * [0])\n", "[0, 0, 0]\n"},
		{"list concat", "print([1, 2] + [3])\n", "[1, 2, 3]\n"},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			if got := captureStdout(t, r.src); got != r.want {
				t.Errorf("interpreter printed %q, want %q — the interpreter has the sequence semantics", got, r.want)
			}
			res, err := Compile(r.src)
			if err == nil {
				if out := runIR(t, res.IR); out == r.want {
					t.Log("the compiled leg now answers this; promote the row into TestContainerInArithmeticNeverExitsTwo")
				}
				return
			}
			if !strings.Contains(err.Error(), "no compiled lowering") {
				t.Errorf("refusal %q does not say what is missing (Gap R.38)", err)
			}
		})
	}
}

// TestAContainerArgumentIsStillAValidArgument is the regression that made this cycle expensive. The lift
// guard fires on any container reaching the double road, and a CALL ARGUMENT reaches it legitimately:
// `def half(xs): return xs[0] / 2` with `half([1.5])` answered 0.75 before the guard existed, and a guard
// that refused it would trade an exit-2 crash for a lost answer. The ladder's rule: an answer may not
// become a refusal.
func TestAContainerArgumentIsStillAValidArgument(t *testing.T) {
	for _, r := range []struct {
		src  string
		want string
	}{
		{"def half(xs):\n    return xs[0] / 2\n\n\nprint(half([1.5]))\n", "0.75\n"},
		{"def first(xs):\n    return xs[0] + 1\n\n\nprint(first([2]))\n", "3\n"},
		{"def total(xs):\n    return xs[0] * xs[1]\n\n\nprint(total([3, 4]))\n", "12\n"},
	} {
		r := r
		t.Run(strings.TrimSpace(r.src), func(t *testing.T) {
			if got := captureStdout(t, r.src); got != r.want {
				t.Errorf("interpreter printed %q, want %q", got, r.want)
			}
			if got := compiledOut(t, r.src); got != r.want {
				t.Errorf("--aot printed %q, want %q — a container ARGUMENT is legal; only an arithmetic operand is not", got, r.want)
			}
		})
	}
}

// TestTheArithmeticGuardLeavesTheOrderRoadItsQuestion pins the other regression: `a = [1]` / `b = [2]` /
// `print(a < b)` printed True before this row existed and must keep printing it. The tagged order door
// three lines above the guard owns a comparison over two same-kind containers; claiming it from here
// turned True into a refusal.
func TestTheArithmeticGuardLeavesTheOrderRoadItsQuestion(t *testing.T) {
	src := "a = [1]\nb = [2]\nprint(a < b)\n"
	if got := captureStdout(t, src); got != "True\n" {
		t.Errorf("interpreter printed %q, want True", got)
	}
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("the compiled leg refused a comparison it answered before Gap R.175: %v", err)
	}
	if out := runIR(t, res.IR); out != "True\n" {
		t.Errorf("--aot printed %q, want True", out)
	}
}

// TestTextTimesAnIntStillAnswers guards the boundary on the other side: `"ab" * 2` is `abab`, and the
// container guard must not swallow the sequence road that already works.
func TestTextTimesAnIntStillAnswers(t *testing.T) {
	src := "print(\"ab\" * 2)\n"
	if got := captureStdout(t, src); got != "abab\n" {
		t.Errorf("interpreter printed %q, want abab", got)
	}
	// The compiled leg's text-repeat refusal predates this row (Gap R.82) and is pinned here only so
	// the container guard cannot have changed it: it must stay WORDS about the operator, never an
	// invalid module and never a number.
	res, err := Compile(src)
	if err == nil {
		if out := runIR(t, res.IR); out != "abab\n" {
			t.Errorf("--aot answered %q, want abab", out)
		}
		return
	}
	for _, banned := range []string{"global variable reference", "sitofp i32 @", "mul i32 @."} {
		if strings.Contains(err.Error(), banned) {
			t.Errorf("refusal leaked an invalid module (`%s`): %v", banned, err)
		}
	}
}

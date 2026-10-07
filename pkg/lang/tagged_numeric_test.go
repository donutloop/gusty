package lang

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// A number the object describes (roadmap L11.1's open clause, Gap R.88).
//
// ADR 0243 taught the compiled backend that an element the literal still describes is that number
// for arithmetic — `xs[0] + 1`. This is the other half of that sentence: the **index** the program
// computes. `xs = [1.5, "a"]; i = 0; print(xs[i] + 1)` answers 2.5 in CPython and on the record;
// the compiled backend refused with "this context needs a single static kind", and worse, the one
// shape that did reach the float arm emitted
//
//	%t2 = fdiv double , %t1
//
// — an empty operand, which `llc` rejects, so an ordinary question about an ordinary list exited 2
// (ADR 0166 counts that as the compiler's bug, not the program's).
//
// The door here lowers the read to the (payload, tag) pair the writer left beside the slot and asks
// the tag which arithmetic to emit: a float slot unboxes, an int or bool slot converts, and a slot
// holding anything else raises the TypeError CPython raises, named per kind and per operator — the
// same shape `lenOfTaggedSlot` already uses to say `object of type 'int' has no len()`.

func TestTaggedNumericUseMatchesCPythonOnBothLegs(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			"the exit-2 program: true division of a slot read through a computed index",
			"xs = [10, 4]\ni = 0\nprint(xs[i] / 4)\n", "2.5\n",
		},
		{
			"a float slot added to a number",
			"xs = [1.5, \"a\"]\ni = 0\nprint(xs[i] + 1)\n", "2.5\n",
		},
		{
			"a float slot multiplied, the other slot",
			"xs = [1.5, 2.5]\ni = 1\nprint(xs[i] * 2)\n", "5.0\n",
		},
		{
			"a float slot through a loop index",
			"xs = [1.5, 2.5]\nfor i in [0, 1]:\n    print(xs[i] * 2)\n", "3.0\n5.0\n",
		},
		{
			"an int slot divided, which is a float answer in Python",
			"xs = [10, \"a\"]\ni = 0\nprint(xs[i] / 4)\n", "2.5\n",
		},
		{
			"a bool slot is the number it is",
			"xs = [True, 1]\ni = 0\nprint(xs[i] + 1)\n", "2\n",
		},
		{
			"a float slot ordered against a number",
			"xs = [1.5, \"a\"]\ni = 0\nprint(1 if xs[i] > 1.0 else 0)\nprint(1 if xs[i] < 1.0 else 0)\n", "1\n0\n",
		},
		{
			"an int slot ordered against a number through a loop index",
			"xs = [1, \"a\"]\nfor i in [0]:\n    print(1 if xs[i] > 2 else 0)\n    print(1 if xs[i] <= 1 else 0)\n", "0\n1\n",
		},
		{
			"two slots of the same container, both read through an index",
			"xs = [1.5, 2.5]\ni = 0\nj = 1\nprint(xs[i] + xs[j])\n", "4.0\n",
		},
		{
			"the result accumulates",
			"xs = [1.5, \"a\"]\ntotal = 0.0\nfor i in [0, 0]:\n    total = total + xs[i]\nprint(total)\n", "3.0\n",
		},
		{
			// Negation is its own operator, so it has its own door: spelling `-xs[i]` as `0 - xs[i]`
			// would raise the binary TypeError for a program that never wrote a binary minus.
			"a float slot negated",
			"xs = [1.5, \"a\"]\ni = 0\nprint(-xs[i])\n", "-1.5\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%q refused: %v", tc.src, err)
			}
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("AOT ran %q, want CPython's %q\nsrc: %s", out, tc.want, tc.src)
			}
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("interpreter printed %q, want CPython's %q\nsrc: %s", out, tc.want, tc.src)
			}
		})
	}
}

// The slot holds something with no number in it. The compiled backend has to raise what the oracle
// raises — the class, the sentence, and the per-operator difference between `"a" + 1` and `"a" - 1`,
// which is the whole reason this is a door per operator rather than one helper for arithmetic.
func TestTaggedNumericUseTrapsWherePythonTraps(t *testing.T) {
	for _, tc := range []struct {
		name    string
		src     string
		class   string
		message string
		// aotOnly marks a row whose interpreter half is a separate, already-recorded defect: the
		// compiled path is still checked, so the new door cannot regress, and the record's wrong
		// answer is not laundered into a pass (roadmap Gap R.89).
		aotOnly bool
	}{
		{
			"text in the slot, added",
			"xs = [\"a\", 1.5]\ni = 0\nprint(xs[i] + 1)\n",
			"TypeError", "can only concatenate str (not \"int\") to str", false,
		},
		{
			"text in the slot, subtracted",
			"xs = [\"a\", 1.5]\ni = 0\nprint(xs[i] - 1)\n",
			"TypeError", "unsupported operand type(s) for -: 'str' and 'int'", false,
		},
		{
			"None in the slot",
			"xs = [None, 1.5]\ni = 0\nprint(xs[i] - 1)\n",
			"TypeError", "unsupported operand type(s) for -: 'NoneType' and 'int'", false,
		},
		{
			"text in the slot, divided",
			"xs = [\"a\", 1.5]\ni = 0\nprint(xs[i] / 2)\n",
			"TypeError", "unsupported operand type(s) for /: 'str' and 'int'", false,
		},
		{
			"a container in the slot, subtracted",
			"xs = [[1], 1.5]\ni = 0\nprint(xs[i] - 1)\n",
			"TypeError", "unsupported operand type(s) for -: 'list' and 'int'", false,
		},
		{
			"text ordered against a number",
			"xs = [\"a\", 1]\ni = 0\nprint(1 if xs[i] > 1 else 0)\n",
			"TypeError", "'>' not supported between instances of 'str' and 'int'", false,
		},
		{
			// The order the two type names appear in is the order the operands were written: CPython
			// says 'int' and 'str' here, and a message built from (slot, other) rather than
			// (left, right) would have it the other way round.
			"a number ordered against text in the slot",
			"xs = [1, \"a\"]\ni = 1\nprint(1 if 1 > xs[i] else 0)\n",
			"TypeError", "'>' not supported between instances of 'int' and 'str'", false,
		},
		{
			// Unary minus has its own sentence in CPython, which is why the negation is its own door rather
			// than a binary minus in a mask. Both halves now ask the question: the compiled one reads the
			// kind off the expression and the literal, the record reads it off the object. What this row
			// pinned was Gap R.89's `aotOnly` — the record answering a garbage number because `-` had
			// never consulted a tag at all — and ADR 0266 paid it, so the row is no longer compiled-only.
			"text in the slot negated",
			"xs = [1.5, \"a\"]\ni = 1\nprint(-xs[i])\n",
			"TypeError", "bad operand type for unary -: 'str'", false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.aotOnly {
				ee := trapRun(t, tc.src)
				if ee.ExnType != tc.class {
					t.Errorf("interpreter raised %q, want %q (msg %q)", ee.ExnType, tc.class, ee.ExnMsg)
				}
				if ee.ExnMsg != tc.message {
					t.Errorf("interpreter message =\n  %q\nwant\n  %q", ee.ExnMsg, tc.message)
				}
			}
			// The compiled program has to die with the same sentence, on stderr, inside a traceback.
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the oracle traps on: %v", err)
			}
			out := runIRMayTrap(t, res.IR)
			if !strings.Contains(out, "Traceback (most recent call last):") {
				t.Errorf("the compiled program printed no traceback:\n%s", out)
			}
			if !strings.Contains(out, tc.message) {
				t.Errorf("compiled message missing %q:\n%s", tc.message, out)
			}
			if strings.Contains(out, "codegen:") {
				t.Errorf("the compiled backend refused what the oracle traps on:\n%s", out)
			}
		})
	}
}

// runIRMayTrap is runIR for the programs that are *supposed* to die: lli exits non-zero, and what it
// wrote — traceback and all — is the answer under test rather than a failure of the harness.
func runIRMayTrap(t *testing.T, ir string) string {
	t.Helper()
	tmp, err := os.CreateTemp("", "trap-*.ll")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tmp.WriteString(ir); err != nil {
		t.Fatal(err)
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	bc := tmp.Name() + ".bc"
	defer os.Remove(bc)
	if out, err := exec.Command("llvm-as-20", tmp.Name(), "-o", bc).CombinedOutput(); err != nil {
		t.Fatalf("llvm-as: %v\n%s", err, out)
	}
	out, _ := exec.Command("lli-20", bc).CombinedOutput()
	return string(out)
}

// What the door still will not answer. Each row is the compiler naming a shape rather than answering
// with a word that means something else — and none of them is allowed to be an exit 2, which the
// integration table asserts against the CLI.
func TestTaggedNumericUseRefusesWhatItCannotProve(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			// `xs[i] * 2` on a container that can hold text or a list is repetition in Python, not a
			// TypeError, and this backend has no repetition yet (Gap R.33) — so the door declines
			// rather than raising a trap CPython would not have raised.
			"repetition-shaped multiplication over a container that holds text",
			"xs = [\"ab\", 1]\ni = 0\nprint(xs[i] * 2)\n",
			"single static kind",
		},
		{
			// A "mixed" family — ints here, floats there, both from the same literal — makes the
			// *result's* kind a run-time question, and the float path always answers a double. That
			// question is the tagged value word owed here (Gap R.82).
			"a container whose slots are ints on one side and floats on the other",
			"xs = [1, 2.5]\ni = 0\nj = 1\nprint(xs[i] + xs[j])\n",
			"single static kind",
		},
		{
			// Two slot reads where the container can also hold text: `xs[i] + xs[j]` is concatenation
			// for one pair of indices and a TypeError for another, and a door that only emits raises
			// would answer the first wrongly (Gap R.82).
			"two slot reads of a container that can hold text",
			"xs = [1.5, 2.5, \"a\"]\ni = 0\nj = 1\nprint(xs[i] + xs[j])\n",
			"single static kind",
		},
		{
			"a slot read of a set has no positions at all",
			"sa = {1, 2}\ni = 0\nprint(sa[i] + 1)\n",
			"index",
		},
		{
			// An int slot negated is still an int, and only the float arms can ask the tag — which would
			// answer -1.0 where CPython answers -1. The result's kind is the run-time question Gap R.82
			// owes, so this shape stays a refusal rather than a near miss.
			"an int slot negated",
			"xs = [1, \"a\"]\ni = 0\nprint(-xs[i])\n",
			"single static kind",
		},
		{
			// `//`, `%` and `**` of an int slot have to stay ints — printing 3.0 where CPython prints
			// 3 is a wrong answer, not a near miss — so the door declines anything whose result kind it
			// cannot settle, and the integer arms refuse as they always have. The float versions
			// (`xs = [7.5, "a"]; xs[i] // 2` → 3.0) are answered, and pinned above.
			"floor division of an int slot",
			"xs = [7, \"a\"]\ni = 0\nprint(xs[i] // 2)\n",
			"single static kind",
		},
		{
			"modulo of an int slot",
			"xs = [7, \"a\"]\ni = 0\nprint(xs[i] % 3)\n",
			"single static kind",
		},
		{
			"a power of an int slot",
			"xs = [3, \"a\"]\ni = 0\nprint(xs[i] ** 2)\n",
			"single static kind",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.src)
			if err == nil {
				out := captureStdout(t, tc.src)
				t.Fatalf("%q compiled; want a refusal (the record printed %q)\nsrc: %s", tc.src, out, tc.src)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("refused with %q, want it to mention %q", err.Error(), tc.want)
			}
			if strings.Contains(err.Error(), "LLVM ERROR") || strings.Contains(err.Error(), "verifier") {
				t.Errorf("failed as an IR problem instead of a front-end refusal: %v", err)
			}
		})
	}
}

// The read emits its bounds check before the arithmetic, so `xs[i]` out of range is still an
// IndexError and not a phi that reads a word out of the object.
func TestTaggedNumericUseKeepsTheBoundsCheck(t *testing.T) {
	ir := compileOrFatal(t, "xs = [1.5, 2.5]\ni = 0\nprint(xs[i] + 1)\n")
	for _, want := range []string{
		"call i32 @rt_get_elem(",
		"call i32 @rt_tag_of(",
		"call double @rt_float_of(",
		"sitofp i32",
	} {
		if !strings.Contains(ir, want) {
			t.Errorf("module is missing %q\nnumeric lift: %v", want, irLinesContaining(ir, "rt_get_elem"))
		}
	}
}

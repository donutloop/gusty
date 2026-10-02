package lang

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// slot_division_test.go — the true division of a slot **no literal describes** (roadmap L11.1's open
// clause Gap R.96; ADR 0253).
//
// ADR 0252 taught `<`, `<=`, `>`, `>=` to ask the object which kind a slot holds, and left the
// arithmetic sentence unfinished, because `xs[0] + 1` cannot know whether it answers `4` or `4.5`
// before the module exists. One operator does not have that problem: **true division is a float
// whatever arrives**, so the result's kind — the only thing the module has to know in advance — is
// settled. What the shape was doing instead:
//
//	xs = []
//	xs.append(3)
//	print(xs[0] / 4)   # CPython 0.75 · --interp 0.75 · --aot printed 0.0 with exit 0
//
// `0.0` is ADR 0249's empty-operand `fdiv` wearing a disguise: the float arm had no operand to lift,
// substituted a literal, and the program printed a number-shaped lie. A wrong answer printed with exit
// 0 is the worst class this repository recognises, so the arm now asks the tag the ordering door asks
// — `rt_float_of` for a float slot, `sitofp` for an int or bool, and CPython's own sentence per other
// kind — and an operand that still cannot be lifted is *refused* rather than substituted.
//
// The trap wording is why the guard lives inside the arms: `3 / 0` says `division by zero` and
// `1.5 / 0` says `float division by zero`, and with one operand's kind only in the object, a guard
// after the merge could only guess which sentence the program's `except` is matching.
//
// Both engines below, CPython in the sibling integration file: parity rows, the traps that must be
// raised rather than refused, the refusals that remain (each naming the half that is missing), and an
// IR row that fails if the module stops branching on the tag or starts inventing an operand again.

func TestTrueDivisionOfAnUnliteralisedSlotAnswersInBothEngines(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		// ---- the program Gap R.96 measured, and its float twin.
		{"an appended int slot divided", "xs = []\nxs.append(3)\nprint(xs[0] / 4)\n", "0.75\n"},
		{"an appended float slot divided", "xs = []\nxs.append(1.5)\nprint(xs[0] / 2)\n", "0.75\n"},
		{"an appended int slot divided evenly", "xs = []\nxs.append(6)\nprint(xs[0] / 4)\n", "1.5\n"},
		{"a bool slot is the number it is", "xs = []\nxs.append(True)\nprint(xs[0] / 2)\n", "0.5\n"},
		{"a negative slot", "xs = []\nxs.append(-6)\nprint(xs[0] / 4)\n", "-1.5\n"},
		// ---- the read may sit on either side of the operator.
		{"the slot is the divisor", "xs = []\nxs.append(4)\nprint(6 / xs[0])\n", "1.5\n"},
		{"the slot divided by a float literal", "xs = []\nxs.append(6)\nprint(xs[0] / 4.0)\n", "1.5\n"},
		{"the slot divided by a settled variable", "xs = []\nxs.append(6)\nd = 4.0\nprint(xs[0] / d)\n", "1.5\n"},
		{"a settled variable over the slot", "xs = []\nxs.append(4)\nd = 6.0\nprint(d / xs[0])\n", "1.5\n"},
		{"a bool literal as the divisor", "xs = []\nxs.append(4)\nprint(xs[0] / True)\n", "4.0\n"},
		// ---- one container, several kinds: the arm that runs is a run-time question per slot.
		{
			"an int slot and a float slot in one built container",
			"xs = []\nxs.append(6)\nxs.append(3.0)\nprint(xs[0] / 4)\nprint(xs[1] / 4)\n", "1.5\n0.75\n",
		},
		{
			"a loop-built container",
			"xs = []\nfor i in [3, 6]:\n    xs.append(i * 2)\nprint(xs[0] / 4)\nprint(xs[1] / 4)\n", "1.5\n3.0\n",
		},
		// ---- the same door through the doors that were already open.
		{"a slot read through an index the program computes", "xs = []\nxs.append(5)\ni = 0\nprint(xs[i] / 4)\n", "1.25\n"},
		{"a dict slot by key", "d = {}\nd[\"a\"] = 4\nprint(d[\"a\"] / 2)\n", "2.0\n"},
		{"a slot one level below", "xs = []\nxs.append([4])\nprint(xs[0][0] / 2)\n", "2.0\n"},
		{"three levels down", "xs = []\nxs.append([[4]])\nprint(xs[0][0][0] / 2)\n", "2.0\n"},
		{"a dict slot of a built container, by key", "xs = []\nxs.append({\"k\": 5})\nprint(xs[0][\"k\"] / 2)\n", "2.5\n"},
		{"a nested dict read twice", "d = {}\nd[\"a\"] = {\"k\": 4}\nprint(d[\"a\"][\"k\"] / 2)\n", "2.0\n"},
		// ---- the positions a division is used in.
		{"bound to a name and printed twice", "xs = []\nxs.append(6)\ny = xs[0] / 4\nprint(y)\nprint(y * 2)\n", "1.5\n3.0\n"},
		{"in a condition", "xs = []\nxs.append(6)\nif xs[0] / 4 > 1:\n    print(\"big\")\nelse:\n    print(\"small\")\n", "big\n"},
		{"in a while head", "xs = []\nxs.append(6)\ni = 0\nwhile xs[0] / 4 > i:\n    print(i)\n    i = i + 1\n", "0\n1\n"},
		{"in a float accumulation", "xs = []\nxs.append(6)\nf = 0.0\nf += xs[0] / 4\nprint(f)\n", "1.5\n"},
		{"in an f-string", "xs = []\nxs.append(6)\nprint(f\"got {xs[0] / 4}\")\n", "got 1.5\n"},
		{"under a builtin", "xs = []\nxs.append(-6)\nprint(abs(xs[0] / 4))\n", "1.5\n"},
		{"negated", "xs = []\nxs.append(4)\nprint(-(xs[0] / 2))\n", "-2.0\n"},
		{"in a value position beside another operand", "xs = []\nxs.append(4)\nprint(len(\"ab\") + xs[0] / 2)\n", "4.0\n"},
		{"appended to a container as its own kind", "xs = []\nxs.append(3)\nys = []\nys.append(xs[0] / 2)\nprint(ys[0])\n", "1.5\n"},
		// ---- the operators whose result kind the compiler can settle without the object: an int slot
		// stays an int, and the door leaves them to the path that already answers them (ADR 0249).
		{"an int slot raised to a power", "xs = []\nxs.append(6)\nprint(xs[0] ** 2)\n", "36\n"},
		{"an int slot floor-divided", "xs = []\nxs.append(7)\nprint(xs[0] // 2)\n", "3\n"},
		// ---- the shapes the older doors already owned, pinned so this one cannot take them over.
		{"a literal container still divides from the literal", "xs = [10, 4]\nprint(xs[0] / 4)\n", "2.5\n"},
		{"a loop variable over a built container divides", "xs = []\nxs.append(6)\nfor v in xs:\n    print(v / 4)\n", "1.5\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%s (%q): refused: %v", tc.name, tc.src, err)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("%s: AOT ran %q, want CPython's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
			}
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("%s: interpreter printed %q, want CPython's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
			}
		})
	}
}

// TestTrueDivisionOfAnUnliteralisedSlotTrapsLikeCPython pins the other half of the claim. A slot whose
// kind the object reports does not merely fail to divide — it *raises*, with CPython's wording and the
// slot's own kind named, and the zero trap keeps the wording that belongs to the operand kinds the arm
// actually lifted. Before this door the compiled leg printed `0.0` for the numeric shapes and a
// truncated number for the rest; a printed lie is what these rows exist to catch.
func TestTrueDivisionOfAnUnliteralisedSlotTrapsLikeCPython(t *testing.T) {
	for _, tc := range []struct{ name, src, class, message string }{
		// ---- the kinds with no number in them: one raise each, naming the kind the slot really holds.
		{
			"a text slot divided",
			"xs = []\nxs.append(\"a\")\nprint(xs[0] / 2)\n",
			"TypeError", "unsupported operand type(s) for /: 'str' and 'int'",
		},
		{
			"a None slot divided",
			"xs = []\nxs.append(None)\nprint(xs[0] / 2)\n",
			"TypeError", "unsupported operand type(s) for /: 'NoneType' and 'int'",
		},
		{
			"a list slot divided",
			"xs = []\nxs.append([1, 2])\nprint(xs[0] / 2)\n",
			"TypeError", "unsupported operand type(s) for /: 'list' and 'int'",
		},
		{
			"a dict slot divided",
			"xs = []\nxs.append({\"k\": 1})\nprint(xs[0] / 2)\n",
			"TypeError", "unsupported operand type(s) for /: 'dict' and 'int'",
		},
		{
			"a set slot divided",
			"xs = []\nxs.append({1, 2})\nprint(xs[0] / 2)\n",
			"TypeError", "unsupported operand type(s) for /: 'set' and 'int'",
		},
		{
			"the divisor side names its own kind when the read sits on the left",
			"xs = []\nxs.append(\"a\")\nprint(2 / xs[0])\n",
			"TypeError", "unsupported operand type(s) for /: 'int' and 'str'",
		},
		{
			"a text slot divided by a settled variable",
			"xs = []\nxs.append(\"a\")\nd = 2.0\nprint(xs[0] / d)\n",
			"TypeError", "unsupported operand type(s) for /: 'str' and 'float'",
		},
		{
			"a slot one level below raises too",
			"xs = []\nxs.append([\"a\"])\nprint(xs[0][0] / 2)\n",
			"TypeError", "unsupported operand type(s) for /: 'str' and 'int'",
		},
		// ---- the zero trap, whose wording is a fact about the two operand *kinds*.
		{
			"an int slot over zero answers the plain sentence",
			"xs = []\nxs.append(0)\nprint(3 / xs[0])\n",
			"ZeroDivisionError", "division by zero",
		},
		{
			"a float slot over zero answers the float sentence",
			"xs = []\nxs.append(0.0)\nprint(3 / xs[0])\n",
			"ZeroDivisionError", "float division by zero",
		},
		{
			"a zero literal divisor beside an int slot",
			"xs = []\nxs.append(3)\nprint(xs[0] / 0)\n",
			"ZeroDivisionError", "division by zero",
		},
		{
			"a zero literal divisor beside a float slot",
			"xs = []\nxs.append(3.5)\nprint(xs[0] / 0)\n",
			"ZeroDivisionError", "float division by zero",
		},
		{
			"a float literal zero makes it the float sentence either way",
			"xs = []\nxs.append(3)\nprint(xs[0] / 0.0)\n",
			"ZeroDivisionError", "float division by zero",
		},
		{
			"a settled float variable holding zero",
			"xs = []\nxs.append(3)\nd = 0.0\nprint(xs[0] / d)\n",
			"ZeroDivisionError", "float division by zero",
		},
		// ---- the traps the read carries, which this door must not lose.
		{
			"the bounds check still fires",
			"xs = []\nxs.append([1, 2])\nprint(xs[0][9] / 1)\n",
			"IndexError", "index out of range",
		},
		{
			"a missing key still raises its own KeyError",
			"d = {}\nd[\"a\"] = 4\nprint(d[\"z\"] / 2)\n",
			"KeyError", "key not found",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ee := trapRun(t, tc.src)
			if ee.ExnType != tc.class {
				t.Errorf("interpreter raised %q, want %q (msg %q)", ee.ExnType, tc.class, ee.ExnMsg)
			}
			if ee.ExnMsg != tc.message {
				t.Errorf("interpreter message =\n  %q\nwant\n  %q", ee.ExnMsg, tc.message)
			}
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
		})
	}
}

// TestTrueDivisionOfAnUnliteralisedSlotIsCatchable pins that each raise is a trap and not a crash: the
// program can catch it, which is the difference between exit 3 and exit 1 and the reason the arm may
// not be a refusal (ADR 0166's exit-class contract, Gap R.37's rule).
func TestTrueDivisionOfAnUnliteralisedSlotIsCatchable(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"a text slot's TypeError",
			"xs = []\nxs.append(\"a\")\ntry:\n    print(xs[0] / 2)\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"the zero trap's ZeroDivisionError",
			"xs = []\nxs.append(0)\ntry:\n    print(1 / xs[0])\nexcept ZeroDivisionError:\n    print(\"no\")\n",
			"no\n",
		},
		{
			"a trap in one slot does not stop the next",
			"xs = []\nxs.append(\"a\")\nxs.append(6)\ntry:\n    print(xs[0] / 2)\nexcept TypeError:\n    print(xs[1] / 4)\n",
			"1.5\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("refused: %v", err)
			}
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("AOT ran %q, want %q", out, tc.want)
			}
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("interpreter printed %q, want %q", out, tc.want)
			}
		})
	}
}

// TestTrueDivisionOfAnUnliteralisedSlotRefusesWhatItCannotName pins the shapes this door still declines,
// each by naming the half that is missing. There are two reasons a program is turned away. The first is
// the gate every other object-reported door has: the raise sentence names the *other* operand's type,
// so the compiler has to be able to name it too — two sides the object would describe is a table the
// compiler would be inventing (Gap R.97's shape, one operator over). The second is the domain: the arms
// answer with a `double`, and a context that stores an `i32` word cannot hold it — printing, a float
// binding, a comparison and a condition can, a call argument, `str()`'s argument and a container slot
// written by key cannot. Those refusals are the tagged value word's, and each row says so.
func TestTrueDivisionOfAnUnliteralisedSlotRefusesWhatItCannotName(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"two built slots divided by each other",
			"xs = []\nxs.append(6)\nys = []\nys.append(4)\nprint(xs[0] / ys[0])\n",
			"has no number the compiled backend can lift",
		},
		{
			"a built slot divided by a slot read through a computed index",
			"xs = []\nxs.append(6)\ni = 0\nprint(xs[i] / xs[0])\n",
			"has no number the compiled backend can lift",
		},
		{
			// `**` is not `/`: an int slot answers 36 and a float slot 2.25, so the result's kind is a
			// fact about the data and the door steps aside for the path that can prove it.
			"a float slot raised to a power",
			"xs = []\nxs.append(1.5)\nprint(xs[0] ** 2)\n",
			"needs a single static kind",
		},
		{
			"the double has no word to travel in to a call",
			"def f(a, b):\n    return b\n\nxs = []\nxs.append(6)\nprint(f(1, xs[0] / 2))\n",
			"this context stores an i32 word",
		},
		{
			"the double has no word to travel in to str()",
			"xs = []\nxs.append(6)\nprint(str(xs[0] / 2))\n",
			"this context stores an i32 word",
		},
		{
			"the double has no word to travel in to a dict slot",
			"xs = []\nxs.append(6)\nd = {}\nd[\"k\"] = xs[0] / 2\nprint(d[\"k\"])\n",
			"this context stores an i32 word",
		},
		{
			"the double has no word to travel in to an int variable",
			"xs = []\nxs.append(6)\nn = 0\nn += xs[0] / 2\nprint(n)\n",
			"writes a double into the i32 slot",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("%s (%q): compiled; want a refusal", tc.name, tc.src)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s refused with %q, want it to mention %q", tc.name, err.Error(), tc.want)
			}
			for _, bad := range []string{"LLVM ERROR", "verifier", "Instruction does not dominate"} {
				if strings.Contains(err.Error(), bad) {
					t.Errorf("%s failed as an IR problem instead of a front-end refusal: %v", tc.name, err)
				}
			}
		})
	}
}

// TestTrueDivisionInsideAComprehensionIsFiledNotFixed pins the three shapes the door still loses when
// the division is the *element* of a comprehension over a container the program built. They are not
// expectations to keep: each row records what every engine answers *today*, and the row fails the day
// the compiler catches up.
//
// The two float rows share a root: the comprehension appends the element through the static path, which
// never asks the numeric door — so the double is either truncated to the i32 word the slot keeps
// (`[xs[0] / 2]` → `[2]`, roadmap Gap R.99) or, when the element raises, the guard's blocks land between
// the loop header and the increment the induction `phi` names as its back edge, and `llc` rejects the
// module (roadmap Gap R.100 — ADR 0224's lesson, one door over).
func TestTrueDivisionInsideAComprehensionIsFiledNotFixed(t *testing.T) {
	for _, tc := range []struct {
		name, src, aotWant, oracle string
		gap                        string
		aotExit                    int
	}{
		{
			"a float element of a comprehension over a built container",
			"xs = []\nxs.append(6)\nprint([xs[0] / 2])\n", "[2]\n", "[3.0]\n", "roadmap Gap R.99", 0,
		},
		{
			"a float element computed from the loop variable",
			"xs = []\nxs.append(6)\nprint([v / 2 for v in xs])\n",
			// the module llc rejects: the guard's `fdiv.ok` block is the real back edge
			"PHI node entries do not match predecessors", "[3.0]\n", "roadmap Gap R.100", 2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if tc.aotExit == 2 {
				if err != nil {
					t.Fatalf("refused: %v (%s owes this shape an answer, not a refusal)", err, tc.gap)
				}
				// The module is the failure here, so the module is what gets checked: `llvm-as` is the
				// verifier the CLI's exit-2 class is produced by, and the row's needle is its complaint.
				tmp, ferr := os.CreateTemp("", "gapr100-*.ll")
				if ferr != nil {
					t.Fatal(ferr)
				}
				defer os.Remove(tmp.Name())
				tmp.WriteString(res.IR)
				tmp.Close()
				defer os.Remove(tmp.Name() + ".bc")
				out, verr := exec.Command("llvm-as-20", tmp.Name(), "-o", tmp.Name()+".bc").CombinedOutput()
				if verr == nil {
					t.Fatalf("the module verified; %s owes it a fix, and this row is the pin", tc.gap)
				}
				if !strings.Contains(string(out), tc.aotWant) {
					t.Errorf("%s: the verifier said %q, want the recorded %q (%s)", tc.name, string(out), tc.aotWant, tc.gap)
				}
				return
			}
			if err != nil {
				t.Fatalf("refused: %v (%s owes this shape an answer, not a refusal)", err, tc.gap)
			}
			out := runIR(t, res.IR)
			if out != tc.aotWant {
				t.Errorf("%s: the compiled leg prints %q, the row recorded %q — if it now prints the oracle's "+
					"answer (%s), delete this row and close %s", tc.name, out, tc.aotWant, tc.oracle, tc.gap)
			}
			if got := captureStdout(t, tc.src); got != tc.oracle {
				t.Errorf("%s: the interpreter prints %q, want the oracle's %q", tc.name, got, tc.oracle)
			}
		})
	}
}

// TestTrueDivisionOfTwoUnliteralisedSlotsIsFiledNotFixed pins the division of two slots the object would
// have to describe — the same gate Gap R.97 records for the ordering, one operator over. The refuse is
// honest (the raise sentence names the *other* operand's type, and with both sides in the object that is
// one branch per pair of kinds), but it is a refusal of a program CPython answers, so it is recorded
// rather than praised, and the row fails when the door grows the pair.
func TestTrueDivisionOfTwoUnliteralisedSlotsIsFiledNotFixed(t *testing.T) {
	for _, tc := range []struct{ name, src, aotWant, oracle, gap string }{
		{
			"two built slots",
			"xs = []\nxs.append(6)\nys = []\nys.append(4)\nprint(xs[0] / ys[0])\n",
			"has no number the compiled backend can lift", "1.5\n", "roadmap Gap R.101",
		},
		{
			"a built slot and a slot read through a computed index",
			"xs = []\nxs.append(6)\ni = 0\nprint(xs[i] / xs[0])\n",
			"has no number the compiled backend can lift", "1.0\n", "roadmap Gap R.101",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := captureStdout(t, tc.src); got != tc.oracle {
				t.Errorf("%s: the interpreter prints %q, want the oracle's %q", tc.name, got, tc.oracle)
			}
			_, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("%s compiled; %s owes it an answer, and this row is the pin", tc.name, tc.gap)
			}
			if !strings.Contains(err.Error(), tc.aotWant) {
				t.Errorf("%s refused with %q, want the recorded %q (%s)", tc.name, err.Error(), tc.aotWant, tc.gap)
			}
		})
	}
}

// TestTrueDivisionOfAnUnliteralisedSlotBranchesOnTheTag is the IR half: the module must carry the tests
// on the tag, one CPython sentence per kind the writers can leave beside a payload, and — the half this
// cycle exists to keep — no instruction with an operand the compiler invented. Gap R.96 was exactly an
// invented `0.0`, so the assertion is not only about `llc` rejecting the module: a substituted operand
// that *does* verify is the bug that printed `0.0` with exit 0.
func TestTrueDivisionOfAnUnliteralisedSlotBranchesOnTheTag(t *testing.T) {
	src := "xs = []\nxs.append(3)\nxs.append(1.5)\nxs.append(\"a\")\nxs.append(None)\nxs.append([1])\nprint(xs[0] / 2)\nprint(xs[1] / 2)\nprint(6 / xs[0])\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	for _, want := range []string{
		// the arms are chosen by the tag the object carries
		"= icmp eq i32 %t", "br i1",
		// a float slot unboxes, an int or bool slot converts, and the divide is real
		"call double @rt_float_of(", "= sitofp i32", "fdiv double",
		// and the raise arm is one sentence per kind, not one sentence for everything
		"unsupported operand type(s) for /: 'str' and 'int'",
		"unsupported operand type(s) for /: 'NoneType' and 'int'",
		"unsupported operand type(s) for /: 'list' and 'int'",
		"unsupported operand type(s) for /: 'dict' and 'int'",
		"unsupported operand type(s) for /: 'set' and 'int'",
		// both ZeroDivisionError wordings, because the guard is inside the arms
		"float division by zero", "division by zero",
	} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("the module does not contain %q:\n%s", want, res.IR)
		}
	}
	for i, ln := range strings.Split(res.IR, "\n") {
		if strings.Contains(ln, "i32 @.") {
			t.Errorf("line %d keeps a global in a value position: %s", i+1, ln)
		}
		// Gap R.96's own shape: an fdiv whose operand the compiler could not lift.
		if strings.Contains(ln, "fdiv double ,") || strings.Contains(ln, "fdiv double  ") {
			t.Errorf("line %d divides with an operand that is not there: %s", i+1, ln)
		}
	}
}

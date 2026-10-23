package lang

import (
	"strings"
	"testing"
)

// Roadmap Gap R.385 / ADR 0405 — the ternary whose arm is a container.
//
// `print([1, 2] if 1 else [9])` is a program the reference answers with `[1, 2]`, and the backend had two
// roads for a container: the module-level road (`containerValue`, which builds the literal once, names it
// with a module global, and prints it at the door) and the call-argument road (`containerOperand`, which
// hands the handle to a function as a word). Neither road had the ternary ARM as a shape it could name, so
// the arm fell to the number road, which renders a container literal as the ADDRESS of its compile-time
// global and hands that to printf in an `i32` slot:
//
//	%t1 = call i32 (i8*, ...) @printf(i8* getelementptr(…@.fmt1…), i32 @.lst1)
//	llc-20: error: global variable reference must have pointer type
//
// — ADR 0166's exit-2 class: a compiler bug wearing a module the assembler refuses. Two shapes were worse
// than the crash, because they were silent: `print({"a": 1} if 1 else {"b": 2})` printed `0` and
// `print((1, 2) if 1 else (3, 4, 5))` printed `0`, the handle of the object printed as a number (ADR 0188's
// mistake, ADR 0192's mistake, one construct later).
//
// What the row decided: an arm is a program the test runs, not an operand of the instruction that picks
// (Gap R.340, ADR 0374), so each arm's builder runs INSIDE the block the branch enters and the merge phis
// the two handles; a test the source wrote is not a run-time question, so the arm that runs is the answer
// and the other arm is not in the program at all. The print door chooses its printer from the kind the arms
// AGREE on — and an arm pair that agrees on no kind is refused in words at exit 1 rather than emitted
// (ADR 0166 again), because a handle printed as a number is the answer the reader cannot tell from a right
// one.
//
// The two questions an arm answers — may this be built as a container, and which printer renders it — are
// read through `isContainerArm`/`staticContainerArmKind` in heapargs.go, so the road that builds and the
// door that prints cannot disagree (ADR 0232's decide-and-build pair). The comprehension spelling is the
// arm shape that needed them: a bare `[v for v in xs]` is settled by the comprehension door, so the shared
// predicate had never been asked to call it a container.

// ternaryContainerAnswered is the family this row answers: every program prints the reference's own bytes,
// pinned on the record leg (the golden's stdout) and on the compiled leg (the linked module run).
func ternaryContainerAnswered() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{"a list arm at the print door", "print([1, 2] if 1 else [9])\n", "[1, 2]\n"},
		{"a list arm whose test is a comparison", "print([1, 2] if 1 == 1 else [9])\n", "[1, 2]\n"},
		// The row's own shape, and the program ADR 0403 left in the owed table of lambda_answer_test.go.
		{"the arm the reference evaluates", "print([1, 2] if (lambda v: v)(1) else [9])\n", "[1, 2]\n"},
		{"the arm that builds the container itself", "print((lambda: [1, 2])() if 1 else [9])\n", "[1, 2]\n"},
		{"a dict arm at the print door", "print({\"a\": 1} if 1 else {\"b\": 2})\n", "{'a': 1}\n"},
		// A test the source wrote is not a run-time question, so the arm that runs is the answer and the kind
		// the pair would have had to agree on is not asked (ADR 0374 at the container arm).
		{"a dict arm with a number where no arm runs", "print({\"a\": 1} if 1 else 7)\n", "{'a': 1}\n"},
		{"a list arm with a number where no arm runs", "print([1, 2] if 1 else 7)\n", "[1, 2]\n"},
		{"the arm a written test rejected is not in the module", "print(7 if 1 else [1, 2])\n", "7\n"},
		{"a set arm at the print door", "print({1, 2} if 1 else {3})\n", "{1, 2}\n"},
		{"a tuple arm at the print door", "print((1, 2) if 1 else (3, 4, 5))\n", "(1, 2)\n"},
		{"a comprehension arm", "print([v for v in [1, 2, 3] if v > 1] if 1 else [9])\n", "[2, 3]\n"},
		{"a comprehension arm whose elements carry their own kinds", "print([v for v in [1, \"a\", True]] if 1 else [9])\n", "[1, 'a', True]\n"},
		{"a comprehension arm of containers", "print([[1] for _ in [1, 2]] if 1 else [9])\n", "[[1], [1]]\n"},
		{"a dict comprehension arm", "print({v: 1 for v in [1, 2]} if 1 else {9: 0})\n", "{1: 1, 2: 1}\n"},
		{"a set comprehension arm", "print({v for v in [1, 2]} if 1 else {9})\n", "{1, 2}\n"},
		{"arms bound and read by position", "arms = [1, 2] if 1 else [9]\nprint(arms[0], arms[1])\n", "1 2\n"},
		{"an arm handed to a function", "def head(xs):\n    return xs[0]\n\nprint(head([1, 2] if 1 else [9]))\n", "1\n"},
		{"a dict arm handed to a function", "def head(d):\n    return d[\"a\"]\n\nprint(head({\"a\": 7} if 1 else {\"a\": 0}))\n", "7\n"},
		{"an arm handed to the ordering builtin", "print(sorted([2, 1] if 1 else [9]))\n", "[1, 2]\n"},
		{"an arm asked a membership question", "print(1 in ([1, 2] if 1 else [9]))\n", "True\n"},
		{"a chain of ternaries with container arms", "print([1] if 1 else [2] if 0 else [3])\n", "[1]\n"},
		// The two-branch road: the test is a name, so both arms exist as blocks and the merge phis them.
		{"a run-time test takes the hot arm then the cold one", "def hot():\n    print(\"hot\")\n    return [1, 2]\n\ndef cold():\n    print(\"cold\")\n    return [9]\n\nt = 1 < 2\nprint(hot() if t else cold())\nt = 1 > 2\nprint(hot() if t else cold())\n", "hot\n[1, 2]\ncold\n[9]\n"},
		{"a comprehension arm chosen by a run-time test", "t = 1 < 2\nprint([v for v in [1, 2, 3] if v > t] if t else [9])\n", "[2, 3]\n"},
		{"a dict comprehension arm chosen by a run-time test", "t = len(\"ab\") == 2\nprint({v: v * 2 for v in [1, 2]} if t else {\"z\": 0})\n", "{1: 2, 2: 4}\n"},
		// The control (ADR 0273): an arm that is a number keeps the number road. The pass must not touch a
		// program whose arms answer a word, and the row's claim is only meaningful beside this row.
		{"the control: an arm that is a number", "print(1 if 1 else [9])\n", "1\n"},
	}
}

// TestAContainerInATernaryArmPrintsWhatTheReferencePrints is the row's answer table. Each row is judged on
// the record leg — the golden's stdout, whose bytes the case must match, so coverage cannot be deleted by
// deleting a record — and on the compiled leg, the linked module run beside it.
func TestAContainerInATernaryArmPrintsWhatTheReferencePrints(t *testing.T) {
	t.Parallel()
	for _, tc := range ternaryContainerAnswered() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			goldenStdoutIs(t, strings.TrimSuffix(tc.src, "\n"), tc.want)
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the reference answers (%v): %s", err, tc.src)
			}
			if got := runIR(t, res.IR); got != tc.want {
				t.Errorf("compiled: stdout %q, want the reference's %q\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}

// TestTheUntakenContainerArmBuildsNothing pins ADR 0374's rule at the container arm: the arm the test
// rejected is not run, so nothing of it reaches the program — no print, and no element written into the
// object that was built for the other arm.
func TestTheUntakenContainerArmBuildsNothing(t *testing.T) {
	t.Parallel()
	tracing := "def hot():\n    print(\"hot\")\n    return [1, 2]\n\ndef cold():\n    print(\"cold\")\n    return [9]\n\nt = 1 < 2\nprint(hot() if t else cold())\n"
	if got := compiledRunIsTheBackend(t, tracing); got != "hot\n[1, 2]\n" {
		t.Fatalf("the arm the test rejected was run: %q", got)
	}
	// The fold: a test the source wrote leaves the other arm out of the module, not merely unexecuted.
	folded := "print([1, 2] if 1 else [9])\n"
	res, err := Compile(folded)
	if err != nil {
		t.Fatalf("the compiled backend refused a program the reference answers (%v)", err)
	}
	if strings.Contains(res.IR, "i32 9") {
		t.Fatalf("the arm a written test rejected is still in the module:\n%s", res.IR)
	}
}

// TestTheContainerArmsTravelInAWordAndNotAsAGlobal is the row's IR claim, pinned separately from the answer
// because it is the shape a later "optimisation" would restore: `i32 @.lst1` in an argument slot is the
// instruction llc-20 refuses at exit 2 (ADR 0188, ADR 0192), and a module that prints the right bytes while
// carrying it is a module one edit away from crashing.
func TestTheContainerArmsTravelInAWordAndNotAsAGlobal(t *testing.T) {
	t.Parallel()
	for _, tc := range ternaryContainerAnswered() {
		tc := tc
		if tc.name == "the control: an arm that is a number" {
			continue // no container in the taken arm, so nothing to travel
		}
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the reference answers (%v): %s", err, tc.src)
			}
			if _, err := VerifyModuleIR(res.IR, 0); err != nil {
				t.Fatalf("the emitted module does not verify: %v\n%s", err, res.IR)
			}
			for _, line := range strings.Split(res.IR, "\n") {
				if !strings.Contains(line, "@printf(") {
					continue
				}
				for _, folded := range []string{"@.lst", "@.set", "@.dict"} {
					if strings.Contains(line, folded) {
						t.Fatalf("a folded container global reached the print call where a word belongs (%s):\n%s", folded, line)
					}
				}
			}
		})
	}
	// The two-branch road's own shape: the arms are built in the blocks the branch enters and the merge
	// phis the handles, so the printer is handed a register.
	branching := "t = 1 < 2\nprint([1, 2] if t else [9])\n"
	res, err := Compile(branching)
	if err != nil {
		t.Fatalf("the compiled backend refused a program the reference answers (%v)", err)
	}
	for _, want := range []string{"br i1", "rt_set_elem(i32", "call void @rt_print_list(i32 %"} {
		if !strings.Contains(res.IR, want) {
			t.Fatalf("the arms are not built in the branch they belong to — the module carries no %s:\n%s", want, res.IR)
		}
	}
}

// TestAPairOfContainerArmsWithNoKindToNameIsRefusedInWords is ADR 0166 held to the ternary: the arms of
// `[1, 2] if t else {"a": 1}` are two objects of two kinds, the print door cannot choose one printer from
// a kind the compiler does not have, and the alternative to refusing is the wrong answer at exit 0 (or the
// module at exit 2 this row just removed). The sentence has to name the program and the road.
func TestAPairOfContainerArmsWithNoKindToNameIsRefusedInWords(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{"a list arm and a dict arm", "t = 1 < 2\nprint([1, 2] if t else {\"a\": 1})\n"},
		{"a set arm and a list arm", "t = 1 < 2\nprint({1, 2} if t else [9])\n"},
		{"a list arm and a number arm", "t = 1 < 2\nprint([1, 2] if t else 7)\n"},
		{"a number arm and a list arm", "t = 1 < 2\nprint(7 if t else [1, 2])\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msg := compiledRefusalOf(tc.src)
			if msg == "" {
				t.Fatalf("the pair compiled — the door chose a printer for two kinds; refuse it by name\n%s", tc.src)
			}
			if !strings.Contains(msg, "arms") || !strings.Contains(msg, "Gap R.385") {
				t.Fatalf("the refusal does not name the arms or the road:\n%s", msg)
			}
			for _, leak := range []string{"*lang.", "*ir.", "%!s(", "%v", "0x"} {
				if strings.Contains(msg, leak) {
					t.Fatalf("the refusal leaks the compiler's own spelling %q:\n%s", leak, msg)
				}
			}
		})
	}
}

// ternaryContainerOwedRow is one shape of the family the row measured and does NOT own, with the answer it
// holds today written beside the reference's. Pinned, not filed-and-forgotten: a row that stops being true
// because the program now answers the reference's bytes FAILS, and the case has to be deleted rather than
// left to certify a road that no longer exists.
//
//   - "rejects" — the module never reaches the run time: `llc-20` refuses it, and `today` holds the message.
//   - "refuses" — the backend refuses in words, and `today` holds the sentence.
type ternaryContainerOwedRow struct {
	name  string
	src   string
	want  string // the reference's own bytes, read off `python3`
	mode  string
	today string
	gap   string
}

func ternaryContainerOwed() []ternaryContainerOwedRow {
	return []ternaryContainerOwedRow{
		// Gap R.393 — the print door has the arms; the doors that ASK the arm a question do not. Each of
		// these reads the container ternary as a word, and either hands the assembler the folded global or
		// declines with a sentence about literals.
		{
			name:  "str() of a container ternary",
			src:   "print(str([1, 2] if 1 else [9]))\n",
			want:  "[1, 2]\n",
			mode:  "rejects",
			today: "global variable reference must have pointer type",
			gap:   "Gap R.393",
		},
		{
			name:  "a loop over a container ternary",
			src:   "for v in ([1, 2] if 1 else [9]):\n    print(v)\n",
			want:  "1\n2\n",
			mode:  "rejects",
			today: "global variable reference must have pointer type",
			gap:   "Gap R.393",
		},
		{
			name:  "a def that returns a container ternary",
			src:   "def f():\n    return [1, 2] if 1 else [9]\n\nprint(f())\n",
			want:  "[1, 2]\n",
			mode:  "rejects",
			today: "global variable reference must have pointer type",
			gap:   "Gap R.393",
		},
		{
			name:  "a container ternary in a % format",
			src:   "print(\"%s\" % ([1, 2] if 1 else [9]))\n",
			want:  "[1, 2]\n",
			mode:  "rejects",
			today: "global variable reference must have pointer type",
			gap:   "Gap R.393",
		},
		{
			name:  "a container ternary in an interpolation",
			src:   "print(f\"{[1, 2] if 1 else [9]}\")\n",
			want:  "[1, 2]\n",
			mode:  "rejects",
			today: "global variable reference must have pointer type",
			gap:   "Gap R.393",
		},
		{
			name:  "the length of a container ternary",
			src:   "print(len([1, 2] if 1 else [9]))\n",
			want:  "2\n",
			mode:  "refuses",
			today: "len requires an inline list/dict/set literal",
			gap:   "Gap R.393",
		},
		{
			name:  "a position in a container ternary",
			src:   "print(([1, 2] if 1 else [9])[0])\n",
			want:  "1\n",
			mode:  "refuses",
			today: "index requires an inline list/dict/set literal",
			gap:   "Gap R.393",
		},
		{
			name:  "a position in a bound tuple arm",
			src:   "xs = (1, 2) if 1 else (3,)\nprint(xs[0], len(xs))\n",
			want:  "1 2\n",
			mode:  "refuses",
			today: "index of a non-literal variable",
			gap:   "Gap R.393",
		},
		// Gap R.394 — the pair this row refuses. The reference prints the taken arm whatever the arms' kinds
		// are, so the answer exists; what is missing is a printer that asks the object, which is the tag road
		// Gap R.231/ADR 0333 built for the answer of a call.
		{
			name:  "a list arm and a dict arm, printed",
			src:   "t = 1 < 2\nprint([1, 2] if t else {\"a\": 1})\n",
			want:  "[1, 2]\n",
			mode:  "refuses",
			today: "cannot choose one printer",
			gap:   "Gap R.394",
		},
		{
			name:  "a list arm and a number arm, printed",
			src:   "t = 1 < 2\nprint([1, 2] if t else 7)\n",
			want:  "[1, 2]\n",
			mode:  "refuses",
			today: "cannot choose one printer",
			gap:   "Gap R.394",
		},
		{
			name:  "a number arm and a list arm, printed",
			src:   "t = 1 < 2\nprint(7 if t else [1, 2])\n",
			want:  "7\n",
			mode:  "refuses",
			today: "cannot choose one printer",
			gap:   "Gap R.394",
		},
		{
			name:  "a text arm and a list arm, printed",
			src:   "t = 1 < 2\nprint(\"text\" if t else [9])\n",
			want:  "text\n",
			mode:  "refuses",
			today: "cannot choose one printer",
			gap:   "Gap R.394",
		},
	}
}

// TestWhatTheContainerTernaryStillOwesPinsTheDivergence holds the shapes this cycle measured and left on the
// tracker, each with the reference's answer beside the one the backend gives today.
func TestWhatTheContainerTernaryStillOwesPinsTheDivergence(t *testing.T) {
	t.Parallel()
	for _, tc := range ternaryContainerOwed() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			switch tc.mode {
			case "rejects":
				got, err := RunSource(tc.src)
				if err == nil {
					t.Fatalf("%s: the module no longer dies in `llc` — it printed %q (the reference prints %q); when the "+
						"two agree, DELETE this row and move the program into the answered table\n%s", tc.gap, got, tc.want, tc.src)
				}
				if !strings.Contains(err.Error(), tc.today) {
					t.Fatalf("%s: the module rejection changed from %q to:\n%v", tc.gap, tc.today, err)
				}
			case "refuses":
				msg := compiledRefusalOf(tc.src)
				if msg == "" {
					if out, err := RunSource(tc.src); err == nil && out == tc.want {
						t.Fatalf("%s: the program now prints the reference's %q — DELETE this row\n%s", tc.gap, tc.want, tc.src)
					}
					t.Fatalf("%s: the program compiled — if it now prints %q, DELETE this row\n%s", tc.gap, tc.want, tc.src)
				}
				if !strings.Contains(msg, tc.today) {
					t.Fatalf("%s: the refusal changed from %q to:\n%s", tc.gap, tc.today, msg)
				}
			default:
				t.Fatalf("%s: unknown owed mode %q", tc.gap, tc.mode)
			}
		})
	}
}

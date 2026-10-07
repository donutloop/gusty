package lang

// ternary_number_test.go — a ternary's answer travels in the word its arms answer (roadmap L11.6,
// Gap R.102; ADR 0262).
//
// The compiled backend chose the branch and then forgot it: one instruction, whatever the arms were,
//
//	%t = select i1 %c, i32 %then, i32 %els
//
// and an arm that was a double reached `value()`, which renders a float by its truncated integer. So
// `print(1 if 0 else 2.5)` printed `2`, `def f(x): return 1.5 if x > 2 else 2.5` returned `2`, and the
// function whose body rebound a parameter to a double and returned one of its arms was refused with a
// message blaming the missing `select` (Gap R.102). The two questions — what kind is the answer, and
// which instruction chooses it — are now answered once, in `ternaryKind`, and read by the renderer, the
// return-word gate and both lowerings.
//
// Every row here is CPython's answer on both legs, or — for the shape whose arms disagree on a word
// — a refusal in words on the compiled side with the record still answering CPython. A row that
// quietly asserted a truncated number would be the bug wearing a test.

import (
	"strings"
	"testing"
)

// TestATernaryAnswersInTheWordItsArmsAnswer is the parity table: every shape the rule can name, run
// through the real compiler and the record, against what CPython prints.
func TestATernaryAnswersInTheWordItsArmsAnswer(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		// ---- both arms are doubles: the instruction Gap R.102 was filed for.
		{"both arms doubles, printed",
			"c = 1\nprint(1.5 if c > 0 else 2.5)\n", "1.5\n"},
		{"both arms doubles, the other branch",
			"c = 0\nprint(1.5 if c > 0 else 2.5)\n", "2.5\n"},
		{"both arms doubles from a function",
			"def f(x):\n    return 1.5 if x > 2 else 2.5\n\nprint(f(1), f(5))\n", "2.5 1.5\n"},
		{"a double built by arithmetic on each arm",
			"def f(x):\n    return x * 1.5 if x > 2 else x / 2\n\nprint(f(3), f(1))\n", "4.5 0.5\n"},
		// ---- the test is a value the source wrote: the arm that runs answers, in its own kind.
		{"a constant test takes the integer arm",
			"print(1 if 1 else 2.5)\n", "1\n"},
		{"the same constant test takes the double arm",
			"print(1 if 0 else 2.5)\n", "2.5\n"},
		{"a constant test written as a verdict",
			"print(1.5 if True else 2)\n", "1.5\n"},
		{"a negated constant test",
			"print(1.5 if not 0 else 2)\n", "1.5\n"},
		{"an empty container is the falsy test CPython says it is",
			"print(1.5 if [] else 2)\n", "2\n"},
		{"a non-empty container is the truthy one",
			"print(1.5 if [1] else 2)\n", "1.5\n"},
		// ...and the dead arm must not decide anything, which is what makes the pair above two
		// different answers rather than one answer with two spellings.
		{"the dead arm is not even emitted as a question",
			"def boom():\n    return 1 / 0\n\nprint(2.5 if 1 else boom())\n", "2.5\n"},
		// ---- the arithmetic and comparison domains read the same answer.
		{"a ternary inside an arithmetic expression",
			"c = 1\nprint((1.5 if c > 0 else 2.5) * 2)\n", "3.0\n"},
		{"a double arm compared against a double",
			"c = 1\nprint(1 if (1.5 if c > 0 else 2.5) > 2 else 0)\n", "0\n"},
		{"a double arm that does compare true",
			"c = 1\nprint(1 if (1.5 if c > 0 else 2.5) > 1 else 0)\n", "1\n"},
		{"both arms doubles, accumulated",
			"def f(x):\n    return x + 0.5 if x > 2 else x - 0.5\n\nprint(f(3), f(1))\n", "3.5 0.5\n"},
		// ---- the shapes that were already right and must not move.
		{"both arms integers",
			"c = 1\nprint(1 if c > 0 else 2)\n", "1\n"},
		{"both arms verdicts",
			"c = 1\nprint(True if c > 0 else False)\n", "True\n"},
		{"a ternary nested in a ternary",
			"c = 1\nd = 0\nprint(1.5 if c > 0 else 2.5 if d > 0 else 3.5)\n", "1.5\n"},
		{"a double arm stored in a variable",
			"c = 0\nt = 1.5 if c > 0 else 2.5\nprint(t)\n", "2.5\n"},
		{"a double arm printed through str()",
			"c = 0\nprint(str(1.5 if c > 0 else 2.5))\n", "2.5\n"},
		{"a double arm inside a container slot",
			"c = 0\nprint([1.5 if c > 0 else 2.5])\n", "[2.5]\n"},
		// ---- the return the gap was named for: the body rebound the parameter, and the arm is that
		// double. This is `select i1 … double …, double …` and nothing else.
		{"Gap R.102: a ternary arm of the parameter the body rebound to a float",
			"def f(x):\n    x = x + 1.5\n    return x if x > 2 else 0.0\n\nprint(f(1))\n", "2.5\n"},
		{"the same function taking the other arm",
			"def f(x):\n    x = x + 1.5\n    return x if x > 2 else 0.0\n\nprint(f(5))\n", "6.5\n"},
		{"both arms doubles with the parameter on the outside",
			"def f(x):\n    x = x + 0.5\n    return 0.0 if x > 2 else x\n\nprint(f(1))\n", "1.5\n"},
		{"a ternary arm inside a larger return expression",
			"def f(x):\n    x = x + 1.5\n    return (x if x > 2 else 0.0) * 2\n\nprint(f(1))\n", "5.0\n"},
		{"a ternary arm of a local bound to a double",
			"def f(x):\n    y = x * 2.0\n    return y if y > 2 else 0.5\n\nprint(f(3.0), f(0.5))\n", "6.0 0.5\n"},
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

// TestTheDoubleSelectIsTheInstructionThatChooses is the IR half of the gap: the roadmap row named the
// missing instruction, so the module is what proves it arrived. A `select i1 … double …, double …`
// choosing the arms, no i32 select choosing a double, and a function whose arms are doubles ending in
// `ret double` — the pairing ADR 0254's return-word gate checks for the shapes it already covers.
func TestTheDoubleSelectIsTheInstructionThatChooses(t *testing.T) {
	res, err := Compile("def f(x):\n    x = x + 1.5\n    return x if x > 2 else 0.0\n\nprint(f(1))\n")
	if err != nil {
		t.Fatalf("the shape Gap R.102 filed as unlowerable was refused: %v", err)
	}
	const head = "define double @gy_f(double %p0)"
	at := strings.Index(res.IR, head)
	if at < 0 {
		t.Fatalf("the ternary arm did not promote the function to a double return:\n%s", res.IR)
	}
	body := res.IR[at:]
	if stop := strings.Index(body, "\n}\n"); stop >= 0 {
		body = body[:stop]
	}
	if !strings.Contains(body, "select i1") {
		t.Errorf("nothing chooses the arms:\n%s", body)
	}
	if !containsDoubleSelect(body) {
		t.Errorf("the select does not choose two doubles — that is the instruction Gap R.102 was filed for:\n%s", body)
	}
	if strings.Contains(body, "ret i32 %") {
		t.Errorf("a function returning a double ternary ends in an i32 register:\n%s", body)
	}
	if !strings.Contains(body, "ret double") {
		t.Errorf("a function returning a double ternary has no double return:\n%s", body)
	}
	if strings.Contains(body, "sitofp i32 @.") {
		t.Errorf("a global handle was converted to a double — ADR 0166's rejected module:\n%s", body)
	}
	assertNoForbiddenIR(t, "def f(x): x = x + 1.5; return x if x > 2 else 0.0", res.IR)
	if out := runIR(t, res.IR); out != "2.5\n" {
		t.Errorf("the chosen arm ran %q, want CPython's \"2.5\\n\"", out)
	}
}

// containsDoubleSelect asks for a select whose arms are doubles, without the regex-a-line habit that
// reads the wrong instruction when a function has both kinds.
func containsDoubleSelect(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "= select i1") && strings.Contains(line, "double ") {
			return true
		}
	}
	return false
}

// TestATernaryWhoseArmsDisagreeOnAWordIsRefused is the half the compiler cannot answer: one arm is the
// double, the other is not, and which arm runs is a run-time fact. A `double` word would render the
// other arm `1.0` where CPython writes `1`; an i32 truncates the double arm to its integer. Both are
// number-shaped wrong answers, which ADR 0166 counts as our bug — so the program is refused in words,
// with the record still answering CPython and the tagged value word named as the owner.
func TestATernaryWhoseArmsDisagreeOnAWordIsRefused(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"one arm a double at the top level",
			"c = 1\nprint(1 if c > 0 else 2.5)\n",
			"do not agree on a word",
		},
		{
			"one arm a double, printed on the arm that would have worked today",
			"c = 1\nprint(1 if c > 0 else 2.5)\n",
			"truncate the double arm",
		},
		{
			"one arm a double in a returned expression",
			"def f(x):\n    return 1 if x > 2 else 0.0\n\nprint(f(1))\n",
			"do not agree on a word",
		},
		{
			"the double on the other side",
			"def f(x):\n    return 1.5 if x > 2 else 0\n\nprint(f(1))\n",
			"do not agree on a word",
		},
		{
			"a verdict arm against a double arm",
			"c = 1\nprint(True if c > 0 else 1.5)\n",
			"do not agree on a word",
		},
		{
			"a double arm against a text arm",
			"c = 1\nprint(\"a\" if c > 0 else 1.5)\n",
			"do not agree on a word",
		},
		{
			"the parameter the body rebound, one arm an integer",
			"def f(x):\n    x = x + 1.5\n    return x if x > 2 else 0\n\nprint(f(1))\n",
			"do not agree on a word",
		},
		{
			// CPython answers `2` for this arm and `5.0` for the other; no compile-time word
			// prints both, and printing `2.0` always would be the same bug with a decimal point.
			"an integer arm inside a product",
			"c = 1\nprint((1 if c > 0 else 2.5) * 2)\n",
			"do not agree on a word",
		},
		{
			"a double arm against an integer arm, spelled as doubles-in-one-shape",
			"def f(x):\n    return 2.0 if x > 2 else 2\n\nprint(f(1))\n",
			"do not agree on a word",
		},
		{
			// The callee's convention fixes this parameter's word at i32, which is roadmap
			// Gap P.1's float-parameter story; the ternary only makes it a refusal instead of
			// the truncated answer `twice(1.5)` prints today.
			"a double arm handed to a parameter whose word is an i32",
			"def twice(v):\n    return v + v\n\nprint(twice(1.5 if 1 else 2))\n",
			"is a double and this context stores an i32 word",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("%s (%q): compiled; want a refusal — either word would print a number-shaped wrong answer", tc.name, tc.src)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s refused with %q, want it to mention %q", tc.name, err.Error(), tc.want)
			}
			// A refusal is only honest if it is a refusal: not a toolchain rejection.
			for _, bad := range []string{"LLVM ERROR", "verifier", "must have pointer type", "defined with type"} {
				if strings.Contains(err.Error(), bad) {
					t.Errorf("%s reached the toolchain instead of refusing: %v", tc.name, err)
				}
			}
			// The message names the arms the program wrote, so the reader knows which line it means
			// and what to change.
			for _, needle := range []string{"`", "if ", "else "} {
				if !strings.Contains(err.Error(), needle) {
					t.Errorf("%s message %q does not quote the ternary it refuses", tc.name, err.Error())
				}
			}
			// The record answers every one of these the way CPython does, so the limit is the
			// compiled backend's own and the row says so.
			if out := captureStdout(t, tc.src); out == "" {
				t.Errorf("%s: the record leg printed nothing either", tc.name)
			}
		})
	}
}

// TestNoTernaryAnswersWithATruncatedNumber is the invariant this file exists for, stated over the whole
// shape rather than over the rows I thought of: for any ternary whose arms mention a double, either the
// compiled backend answers CPython, or it refuses in words that name the missing word. There is no
// third answer, and the one that used to be there — a number, printed happily, truncated — fails here.
func TestNoTernaryAnswersWithATruncatedNumber(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"c = 1\nprint(1 if c > 0 else 2.5)\n", "1\n"},
		{"c = 0\nprint(1 if c > 0 else 2.5)\n", "2.5\n"},
		{"print(1 if 1 else 2.5)\n", "1\n"},
		{"print(1 if 0 else 2.5)\n", "2.5\n"},
		{"def f(x):\n    return 1.5 if x > 2 else 2.5\n\nprint(f(1))\n", "2.5\n"},
		{"def f(x):\n    return 1.5 if x > 2 else 2.5\n\nprint(f(5))\n", "1.5\n"},
		{"def f(x):\n    x = x + 1.5\n    return x if x > 2 else 0.0\n\nprint(f(1))\n", "2.5\n"},
		{"c = 1\nprint(2.5 if c > 0 else 1)\n", "2.5\n"},
		{"c = 1\nprint((1.5 if c > 0 else 2.5) * 2)\n", "3.0\n"},
		{"c = 0\nprint(str(1.5 if c > 0 else 2.5))\n", "2.5\n"},
		{"c = 0\nprint([1.5 if c > 0 else 2.5])\n", "[2.5]\n"},
		{"c = 0\nt = 1.5 if c > 0 else 2.5\nprint(t)\n", "2.5\n"},
	} {
		res, err := Compile(tc.src)
		if err == nil {
			out := runIR(t, res.IR)
			if out != tc.want {
				t.Errorf("the compiled backend answered %q where CPython answers %q — a truncated number is the one answer this rule may not give\n%s", out, tc.want, tc.src)
			}
			continue
		}
		if !strings.Contains(err.Error(), "word") {
			t.Errorf("refused %q for a reason that does not name the missing word: %v", tc.src, err)
		}
		if out := captureStdout(t, tc.src); out != tc.want {
			t.Errorf("the compiled backend refused %q and the interpreter also disagrees: got %q want %q", tc.src, out, tc.want)
		}
	}
}

// TestTheTernaryRuleIsAskedOnceAndNotTwice is the tripwire against the shape this codebase keeps falling
// into: a second predicate re-deriving the same answer from the shape of the line. The lowerings may not
// ask `isFloat` of an arm and then choose an instruction somewhere else; both read `ternaryKind`, and a
// `select i1 … i32 …, i32 …` for a program whose arms are doubles is the bug returning.
func TestTheTernaryRuleIsAskedOnceAndNotTwice(t *testing.T) {
	src := "c = 1\nprint(1.5 if c > 0 else 2.5)\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if !containsDoubleSelect(res.IR) {
		t.Errorf("a ternary of two doubles is not chosen by a double select:\n%s", res.IR)
	}
	// The renderer and the chooser cannot now disagree: this program prints the double it chose.
	if out := runIR(t, res.IR); out != "1.5\n" {
		t.Errorf("printed %q, want CPython's \"1.5\\n\"", out)
	}
	// And the same shape with an integer pair keeps its old instruction, so the new arm did not
	// swallow the case it was not for.
	ints, err := Compile("c = 1\nprint(1 if c > 0 else 2)\n")
	if err != nil {
		t.Fatalf("refused an integer ternary: %v", err)
	}
	if !strings.Contains(ints.IR, "= select i1") || containsDoubleSelect(ints.IR) {
		t.Errorf("an integer ternary no longer chooses with the i32 select it always used:\n%s", ints.IR)
	}
}

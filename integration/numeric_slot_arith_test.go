package integration

// integration/numeric_slot_arith_test.go — the number use of a slot whose kind only the run time can
// describe, at the CLI, against the reference, on both engines (roadmap L11.1's last clause; ADR 0265).
//
// The shape is a list of lists built by `append`, with arithmetic on what comes out of the inner one.
// Until this door the compiled leg refused it at compile time — "cannot reach into xs's slots" — because
// the answer's kind (`int` for `xs[0][0] + 1`, `float` for `xs[0][0] * 2`) lives in the object, and the
// object is only built when the program runs.
//
// Every row here is the *same source* run three ways: CPython, `gustyc --file <path> --interp`, and
// `gustyc --file <path> -aot`. The legs are forced explicitly — a bare `--file` is the interpreter's
// default, and `-aot` written after the path becomes the flag's value rather than the compiled leg.
// Exit 2 — the contract's "the compiler is broken" code — fails any row here, including the trap table,
// where a half-finished implementation is exactly what reaches for it (ADR 0166).

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// cpythonPlainOut is the reference's answer for a source it can run as written — no import prelude,
// because this shape is ordinary Python.
func cpythonPlainOut(t *testing.T, dir, src string) (string, bool) {
	t.Helper()
	py := os.Getenv("GUSTY_PYTHON")
	if py == "" {
		py = "python3"
	}
	if _, err := exec.LookPath(py); err != nil {
		t.Skipf("no %s to act as the oracle (set GUSTY_PYTHON)", py)
	}
	path := writeSrc(t, dir, "plain_twin.py", src)
	cmd := exec.Command(py, path)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err == nil
}

func TestTheNumberUseOfARunTimeSlotAnswersLikeTheReferenceAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"the row's own shape: a list built by append, a number out of the inner one",
			"xs = []\nxs.append([7, 8])\nprint(xs[0][0] + 1)\n", "8\n",
		},
		{
			// The answer's kind is the answer's own: the same source with a float in the slot answers a
			// float, and no instruction was rewritten to get there.
			"the float slot keeps the float",
			"xs = []\nxs.append([7.5, 8])\nprint(xs[0][0] * 2)\n", "15.0\n",
		},
		{"the negation", "xs = []\nxs.append([7, 8])\nprint(-xs[0][0])\n", "-7\n"},
		{"subtraction", "xs = []\nxs.append([7, 8])\nprint(xs[0][1] - 3)\n", "5\n"},
		{"both operands are slots", "xs = []\nxs.append([7, 8])\nprint(xs[0][0] + xs[0][1])\n", "15\n"},
		{"a bool is a number", "xs = []\nxs.append([True, 2])\nprint(xs[0][0] + 1)\n", "2\n"},
		{"a dict value", "d = {}\nd[\"k\"] = 40\nprint(d[\"k\"] + 2)\n", "42\n"},
		{"int beside float, both directions",
			"xs = []\nxs.append([7, 8.5])\nprint(xs[0][0] + 1)\nprint(xs[0][1] + 1)\n", "8\n9.5\n"},
		{"three levels deep",
			"xs = []\nxs.append([[7, 8]])\nprint(xs[0][0][1] - 1)\n", "7\n"},
		{"true division keeps its own door",
			"xs = []\nxs.append([7, 8])\nprint(xs[0][0] / 2)\n", "3.5\n"},
		{
			// The same arithmetic bound to a name before it is printed. The road was opened at the printer
			// and stopped one statement short of it; the binding stores the pair, so the name carries the
			// tag its own expression produced (roadmap Gap R.138, ADR 0266).
			"the answer bound to a name first",
			"xs = []\nxs.append([7, 8])\nn = xs[0][0] * 2\nprint(n)\n", "14\n"},
		{
			// And the bound answer keeps the kind the slot gave it — a float stays a float through the
			// name, which is the whole point of carrying the tag with the payload.
			"the float answer bound to a name keeps the float",
			"xs = []\nxs.append([7.5, 8])\nm = xs[0][0] * 2\nprint(m)\n", "15.0\n"},
		{"the negation bound to a name",
			"xs = []\nxs.append([7, 8])\nk = -xs[0][0]\nprint(k)\n", "-7\n"},
		{"two slots summed into a name",
			"xs = []\nxs.append([7, 8])\nt = xs[0][0] + xs[0][1]\nprint(t)\n", "15\n"},
		{"a dict value bound to a name",
			"d = {}\nd[\"k\"] = 40\nn = d[\"k\"] + 2\nprint(n)\n", "42\n"},
		{
			// A name bound twice — a plain number first, the run-time answer second — takes the second
			// binding's kind, the way every other tracked binding in this backend does (ADR 0172).
			"a name rebound from a plain number to a run-time answer",
			"xs = []\nxs.append([7, 8])\nn = 0\nn = xs[0][0] * 2\nprint(n)\n", "14\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "numeric_slot.gy", tc.src)
			if py, ok := cpythonPlainOut(t, dir, tc.src); !ok || py != tc.want {
				t.Fatalf("the expectation is not the reference's: python said %q (ok %v), the row says %q\nsrc: %s", py, ok, tc.want, tc.src)
			}
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliRunCode(t, engine, "--file", gy)
				if code == 2 {
					t.Fatalf("%s: the compiler's own module was rejected (ADR 0166):\n%s", engine, out)
				}
				if code != 0 || out != tc.want {
					t.Errorf("%s: exit %d, stdout %q, want the reference's %q\nsrc: %s", engine, code, out, tc.want, tc.src)
				}
			}
		})
	}
}

// TestTheNumberUseOfARunTimeSlotRaisesLikeTheReferenceAtTheCLI is the exit-code half: the shapes the
// reference *stops* on must raise — exit 3, the runtime-error class — with the reference's own sentence,
// and must be catchable, because a raise the program cannot reach is a different thing wearing the same
// words (ADR 0228).
func TestTheNumberUseOfARunTimeSlotRaisesLikeTheReferenceAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"the negation of a slot that holds text",
			"xs = []\nxs.append(\"hi\")\nprint(-xs[0])\n",
			"TypeError: bad operand type for unary -: 'str'",
		},
		{
			// The kind inside the quotes is the one the object carries, so the two operands of one
			// subtraction are named separately — and `sub` really is the symbol in the sentence.
			"a text under subtraction",
			"xs = []\nxs.append(\"hi\")\nprint(xs[0] - 1)\n",
			"TypeError: unsupported operand type(s) for -: 'str' and 'int'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "numeric_slot_trap.gy", tc.src)
			if py, ok := cpythonPlainOut(t, dir, tc.src); ok || !strings.Contains(py, tc.want) {
				t.Fatalf("the reference was expected to stop with %q, said %q (ok %v)", tc.want, py, ok)
			}
			// Only the compiled leg is pinned here, and that is not an oversight: the interpreted leg
			// answers these two shapes with a *number* at exit 0 (Gap R.137, filed the day this door
			// shipped — negating a text reaches the int evaluator with the interned index inside it),
			// and a parity test for a divergence that is open is a test that documents the bug as if it
			// were the spec. The interpreter's exact answer is pinned per leg in
			// integration/conformance_cases.go, where an open divergence belongs.
			out, code := cliReport(t, "--aot", "--file", gy)
			if code == 2 {
				t.Fatalf("--aot: exit 2 for a program the reference raises on (ADR 0166):\n%s", out)
			}
			if code != 3 {
				t.Errorf("--aot: exit %d, want 3 (the runtime-error class)\n%s", code, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("--aot raised with %q, want the reference's %q", out, tc.want)
			}
		})
	}
}

// TestTheNumberUseOfARunTimeSlotIsCatchableAtTheCLI: the raise leaves through the emitted raise door, so
// `except TypeError:` and `except OverflowError:` reach it — the `except` clause is the only way this
// file asserts the exact sentence on the compiled leg, and the reason the helper fills a buffer and
// returns a status instead of raising inside itself.
func TestTheNumberUseOfARunTimeSlotIsCatchableAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"typeError caught",
			"xs = []\nxs.append(\"hi\")\ntry:\n    print(-xs[0])\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"overflowError caught",
			"xs = []\nxs.append([7, 8])\ntry:\n    print(xs[0][0] * 1000000000)\nexcept OverflowError:\n    print(\"caught\")\n",
			"caught\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "numeric_slot_catch.gy", tc.src)
			out, code := cliRunCode(t, "--aot", "--file", gy)
			if code == 2 {
				t.Fatalf("--aot: exit 2 (ADR 0166):\n%s", out)
			}
			if code != 0 || out != tc.want {
				t.Errorf("--aot: exit %d, stdout %q, want %q", code, out, tc.want)
			}
		})
	}
}

// TestTheNumberDoorStaysShutWhereTheReferenceWouldAnswerTextOrAContainer is the gate at the CLI. The
// reference *answers* these — a joined text, a repeated list — and this backend builds neither from a
// slot (Gap R.82), so the door must not open: the program keeps the compile-time refusal it always had
// (exit 1, named) rather than get a TypeError the reference never raises.
func TestTheNumberDoorStaysShutWhereTheReferenceWouldAnswerTextOrAContainer(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"text stored in a container, used with +",
			"xs = []\nxs.append(\"hi\")\nprint(xs[0] + 1)\n",
			"concatenating a string with a value that is not a string",
		},
		{
			// One text in one container closes `+` and `*` for the whole program: coarse, and coarse
			// means refusing more, never answering wrongly (ADR 0265).
			"text in another container closes the door",
			"xs = []\nxs.append([7, 8])\nys = []\nys.append(\"word\")\nprint(xs[0][0] + 1)\n",
			"cannot reach into",
		},
		{
			// A value with no spelling — the result of a call — is not a literal the notebook can read,
			// and an absent level in the kind tree is refused rather than assumed.
			"a value the compiler cannot see through",
			"def f():\n    return [7, 8]\n\nxs = []\nxs.append(f())\nprint(xs[0][0] + 1)\n",
			"cannot reach into",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "numeric_slot_gate.gy", tc.src)
			out, code := cliReport(t, "--aot", "--file", gy)
			if code == 2 {
				t.Fatalf("exit 2 where the front end should refuse (ADR 0166):\n%s", out)
			}
			if code != 1 {
				t.Errorf("exit %d, want 1 (a program this backend declines to build)\n%s", code, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("refused without naming the missing half: wanted %q in\n%s", tc.want, out)
			}
		})
	}
}

// TestTheCorpusProgramPrintsWhatTheLedgerSays runs the registered conformance file through both engines
// and checks it against the reference: programs/numeric_slot_arith.gy is `oracle: match`, which is a
// claim about all three engines, and this is where it is checked rather than asserted.
func TestTheCorpusProgramPrintsWhatTheLedgerSays(t *testing.T) {
	src, err := os.ReadFile("programs/numeric_slot_arith.gy")
	if err != nil {
		t.Fatalf("the registered corpus file is missing: %v", err)
	}
	want := "8\n5\n-7\n15\n15.0\n8.5\n42\n14\n6.5\n"
	dir := t.TempDir()
	gy := writeSrc(t, dir, "numeric_slot_arith.gy", string(src))
	if py, ok := cpythonPlainOut(t, dir, string(src)); !ok || py != want {
		t.Fatalf("the ledger's expectation is not the reference's: %q (ok %v)", py, ok)
	}
	for _, engine := range []string{"--interp", "--aot"} {
		out, code := cliRunCode(t, engine, "--file", gy)
		if code == 2 {
			t.Fatalf("%s: exit 2 (ADR 0166):\n%s", engine, out)
		}
		if code != 0 || out != want {
			t.Errorf("%s: exit %d, stdout %q, want %q", engine, code, out, want)
		}
	}
}

// TestTheNumberDoorAnswersThePrintPositionAndRefusesTheRest is the door's edge, written down rather than
// hoped away. The pair road is taken where the print dispatch asks for a value *and* where a name is
// bound to one — ADR 0266 paid the binding, and its row answers in the parity table above. What the
// ordinary numeric road still refuses is the pair handed across a call, and the pair used as a number.
// Both are exit 1 with the missing half named — the interpreter answers the reference in each case, so
// each is a filed row (roadmap Gaps R.139 and R.140), not a parity claim.
func TestTheNumberDoorAnswersThePrintPositionAndRefusesTheRest(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"the answer handed to a function",
			"def twice(v):\n    return v * 2\n\nxs = []\nxs.append([7, 8])\nprint(twice(xs[0][0]))\n",
			"cannot reach into",
		},
		{
			// The name the pair was bound to, used as a number: the value slot means nothing without its
			// tag, and this context has no word to carry the pair in (Gap R.140).
			"the bound name used as a number",
			"xs = []\nxs.append([7, 8])\nn = xs[0][0] * 2\nprint(n + 1)\n",
			"using it as a number needs a tagged value",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "numeric_slot_edge.gy", tc.src)
			want, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Fatalf("the reference was expected to answer this program, said %q", want)
			}
			// The interpreted leg is the reference's answer; the compiled leg declines to build it.
			out, code := cliRunCode(t, "--interp", "--file", gy)
			if code == 2 {
				t.Fatalf("--interp: exit 2 (ADR 0166):\n%s", out)
			}
			if code != 0 || out != want {
				t.Errorf("--interp: exit %d, stdout %q, want the reference's %q", code, out, want)
			}
			out, code = cliReport(t, "--aot", "--file", gy)
			if code == 2 {
				t.Fatalf("--aot: exit 2 where the front end should refuse (ADR 0166):\n%s", out)
			}
			if code != 1 || !strings.Contains(out, tc.want) {
				t.Errorf("--aot: exit %d, want the refusal naming %q, said:\n%s", code, tc.want, out)
			}
		})
	}
}

// TestTheWholeNumberAnswerBeyondTheCompiledIntWordRaisesAndIsCatchable pins the one arithmetic the
// compiled word cannot hold. The reference answers 7000000000 and so does the interpreted leg, whose ints
// are int64; the compiled int is 32 bits, and the arm checks before the truncation, because out of the
// word `fptosi` is poison rather than a wrong number (ADR 0264's lesson, applied at this door by ADR
// 0265). The decision that would make the two agree — a wider int word — is roadmap L12.12's to make.
func TestTheWholeNumberAnswerBeyondTheCompiledIntWordRaisesAndIsCatchable(t *testing.T) {
	const src = "xs = []\nxs.append([7, 8])\nprint(xs[0][0] * 1000000000)\n"
	const sentence = "OverflowError: the whole number the arithmetic would answer is beyond the word this backend's int holds (roadmap L12.12)"
	dir := t.TempDir()
	gy := writeSrc(t, dir, "int_word.gy", src)
	if py, ok := cpythonPlainOut(t, dir, src); !ok || py != "7000000000\n" {
		t.Fatalf("the reference is expected to answer 7000000000, said %q (ok %v)", py, ok)
	}
	out, code := cliRunCode(t, "--interp", "--file", gy)
	if code != 0 || out != "7000000000\n" {
		t.Errorf("--interp: exit %d, stdout %q, want the reference's 7000000000", code, out)
	}
	out, code = cliReport(t, "--aot", "--file", gy)
	if code == 2 {
		t.Fatalf("--aot: exit 2 (ADR 0166):\n%s", out)
	}
	if code != 3 || !strings.Contains(out, sentence) {
		t.Errorf("--aot: exit %d, want 3 with the guard's sentence, said:\n%s", code, out)
	}
	// And the trap is a raise the program can reach, not a stop the program has to obey.
	const caught = "xs = []\nxs.append([7, 8])\ntry:\n    print(xs[0][0] * 1000000000)\nexcept OverflowError:\n    print(\"caught\")\n"
	gy2 := writeSrc(t, dir, "int_word_caught.gy", caught)
	out, code = cliRunCode(t, "--aot", "--file", gy2)
	if code != 0 || out != "caught\n" {
		t.Errorf("the OverflowError is not catchable: exit %d, stdout %q", code, out)
	}
}

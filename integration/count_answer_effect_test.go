package integration

// integration/count_answer_effect_test.go — the count door PERFORMS an element whose answer is a
// container, at the CLI, against a live reference (roadmap **Gap R.534**, **ADR 0460**).
//
// The program that filed the row is two lines of the most ordinary kind:
//
//	def probe():
//	    print("built")
//	    return [1]
//	print(len([probe()]))
//
// `python3` writes `built` and then `1`. This toolchain wrote `1`, at the exit code of success, because
// the length door read the count off the literal's SPELLING and asked the one-word value road for the
// element — which refuses a call that hands back a container (**Gap R.67**'s sentence) — and the door
// treated that refusal as "the element has nothing to do". A raise written inside the callee was skipped
// the same way, which is what makes this the queue's priority-1 class: not a lost line of output, a
// program answered that the reference never lets finish.
//
// The fix is the door, not a new road: `lowerLiteralElementForEffect` asks `containerOperand` — the same
// `*Call` arm **ADR 0333** grew for `print([build(), 3])` — and drops the handle, because a position that
// keeps no word has no reason to repeat a refusal that is about a word.
//
// Every expectation below is what a live `python3` printed for the same source inside the case, never what
// this file remembers (**Gap N**'s standing rule), and exit 2 — **ADR 0166**'s code for a compiler that
// broke — fails every row here, including the owed ones.

import (
	"strings"
	"testing"
)

// countAnswerEffectSources is the answered family at this altitude: every position the length door
// answers from a spelling, with an element that is a call handing back a container, a dict, a set, a
// tuple, a method's answer, a forwarding body, or a body whose slots the answer scan cannot describe.
func countAnswerEffectSources() []string {
	listProbe := "def probe():\n    print(\"built\")\n\n    return [1]\n"
	return []string{
		listProbe + "print(len([probe()]))\n",
		listProbe + "print(len([probe(), 2]))\n",
		listProbe + "print(len([probe(), probe()]))\n",
		listProbe + "print(len([[probe()], 2]))\n",
		listProbe + "print(len([[[probe()]]]))\n",
		listProbe + "print(len({\"a\": probe()}))\n",
		listProbe + "print(len({\"a\": probe(), \"b\": 2}))\n",
		listProbe + "print(len([{\"k\": probe()}]))\n",
		listProbe + "print(len((probe(), 1)))\n",
		listProbe + "print(len([probe(), [7][0]]))\n",
		listProbe + "n = len([probe()])\nprint(n)\n",
		listProbe + "if len([probe()]) > 0:\n    print(\"yes\")\n\nelse:\n    print(\"no\")\n",
		listProbe + "print(f\"n={len([probe()])}\")\n",
		listProbe + "for i in range(2):\n    print(len([probe()]))\n",
		listProbe + "def h():\n    print(\"h\")\n\n    return len([probe(), 1])\n\nprint(h())\n",
		listProbe + "print(len([probe()]) + len([probe()]))\n",
		listProbe + "print(len([probe()]))\nprint([probe()])\n",
		listProbe + "print(len(sorted([probe()])))\n",
		listProbe + "print(len([]))\nprint(len([probe()]))\n",
		// The answer's kind is the callee's, and the door performs the call whatever it is — including the
		// tuple **ADR 0457** declines at the SLOTS door, because this position has no slot to lie about.
		"def probe():\n    print(\"built\")\n\n    return {\"k\": 1}\n\nprint(len([probe()]))\n",
		"def probe():\n    print(\"built\")\n\n    return {1}\n\nprint(len([probe()]))\n",
		"def probe():\n    print(\"built\")\n\n    return (1, 2)\n\nprint(len([probe()]))\n",
		// A body the scan can prove but not describe, and a body that only forwards another call's answer.
		"def g(x):\n    print(\"g\")\n\n    z = 5\n\n    return [x, z]\n\nprint(len([g(1)]))\n",
		"def inner():\n    print(\"in\")\n\n    return [1, 2]\n\ndef f():\n    return inner()\n\nprint(len([f()]))\n",
		// A method's container answer, and the mutator's void beside it — the road ADR 0458 opened, asked of
		// the count door.
		"class C:\n    def m(self):\n        print(\"m\")\n\n        return [1]\n\nc = C()\nprint(len([c.m()]))\n",
		"def b():\n    return [1, 2]\n\ndef probe():\n    print(\"built\")\n\n    return [3]\n\nprint(len([probe(), b()]))\n",
		// The negative control beside every row: a container of spellings costs the module nothing, and the
		// answer is the digit the spelling states. (A dict literal with TEXT keys is a heap object by design —
		// ADR 0232's key tags — so the control is written with the containers the fold really does answer.)
		"print(len([1, 2, 3]))\nprint(len([[1, 2], [3]]))\n",
	}
}

func TestTheCountDoorPerformsAContainerAnsweringElementAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, src := range countAnswerEffectSources() {
		src := src
		t.Run(strings.ReplaceAll(strings.SplitN(src, "\n", 2)[0], " ", "_"), func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, src)
			stdout, stderr, code := cliStreams(t, "--eval", src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program (exit 2 — ADR 0166's code):\n%s\n%s", stderr, stdout)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: the reference answers this program\nstdout %q\nstderr %q", code, stdout, stderr)
			}
			if strings.TrimRight(stdout, "\n") != strings.TrimRight(want, "\n") {
				t.Fatalf("printed %q, the reference prints %q", stdout, want)
			}
		})
	}
}

// The accident half — the one that makes this the priority-1 class rather than a lost line. Each of these
// programs raises in the reference and never reaches the count; the compiled leg must run the callee, raise
// the reference's own class and message, keep the count OFF stdout, and stay out of exit 2.
func TestTheCountDoorsContainerAnsweringElementRaisesAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, printed, class, message string }{
		{
			"def probe():\n    xs = [1]\n\n    y = xs[7]\n\n    return [y]\n\nprint(len([probe()]))\n",
			"", "IndexError", "list index out of range",
		},
		{
			"def g(x):\n    z = 5\n\n    raise ValueError(\"boom\")\n\n    return [x, z]\n\nprint(len([g(1)]))\n",
			"", "ValueError", "boom",
		},
		{
			"def probe():\n    print(\"built\")\n\n    raise ValueError(\"boom\")\n\n    return [1]\n\nprint(len([probe()]))\n",
			"built\n", "ValueError", "boom",
		},
		{
			"def boom():\n    raise ValueError(\"first\")\n\n    return [1]\n\ndef other():\n    raise ValueError(\"second\")\n\n    return [2]\n\nprint(len([boom(), other()]))\n",
			"", "ValueError", "first",
		},
		{
			"def probe():\n    x = 1 // 0\n\n    return [x]\n\nprint(len([probe()]))\n",
			"", "ZeroDivisionError", "integer division or modulo by zero",
		},
	} {
		tc := tc
		t.Run(strings.ReplaceAll(strings.SplitN(tc.src, "\n", 2)[0], " ", "_"), func(t *testing.T) {
			t.Parallel()
			// The reference agrees that the program never finishes.
			pyOut, pyOK := cpythonPlainOut(t, t.TempDir(), tc.src)
			if pyOK {
				t.Fatalf("the reference ran this to completion (%q); the row is filed because it raises", pyOut)
			}
			stdout, stderr, code := cliStreams(t, "--eval", tc.src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program (exit 2):\n%s\n%s", stderr, stdout)
			}
			if code != 3 {
				t.Fatalf("exit %d, want the trap class 3; stdout %q stderr %q", code, stdout, stderr)
			}
			if stdout != tc.printed {
				t.Fatalf("the program printed %q before the raise, want %q — a count printed here is a program answered that the reference never finishes", stdout, tc.printed)
			}
			if !strings.Contains(stderr, tc.class+": "+tc.message) {
				t.Fatalf("the raise is not the reference's %s: %s:\n%s", tc.class, tc.message, stderr)
			}
			if strings.Contains(stdout, "Traceback") {
				t.Fatalf("the trap report leaked into the program's stdout: %q", stdout)
			}
		})
	}
}

// The raises are the program's, so the program can catch them — the half a host-side refusal could never
// pass, and the reason a "fix" that turns a wrong answer into a compile-time refusal is not a fix.
func TestTheCountDoorsContainerAnsweringRaisesAreCatchableAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ head, call, class, want string }{
		{
			"def probe():\n    xs = [1]\n\n    y = xs[7]\n\n    return [y]\n\n",
			"print(len([probe()]))", "IndexError", "caught list index out of range",
		},
		{
			"def g(x):\n    z = 5\n\n    raise ValueError(\"boom\")\n\n    return [x, z]\n\n",
			"print(len([g(1)]))", "ValueError", "caught boom",
		},
		{
			"def probe():\n    print(\"built\")\n\n    raise ValueError(\"boom\")\n\n    return [1]\n\n",
			"print(len([probe()]))", "ValueError", "built\ncaught boom",
		},
	} {
		tc := tc
		t.Run(tc.class+"/"+strings.ReplaceAll(strings.SplitN(tc.call, "(", 2)[0], " ", "_"), func(t *testing.T) {
			t.Parallel()
			src := tc.head + "try:\n    " + tc.call + "\nexcept " + tc.class + " as e:\n    print(\"caught\", e)\n"
			want := pythonOutput(t, src)
			stdout, stderr, code := cliStreams(t, "--eval", src)
			if code != 0 {
				t.Fatalf("exit %d, want 0 — the raise is the program's to catch: %q %q", code, stdout, stderr)
			}
			if strings.TrimRight(stdout, "\n") != strings.TrimRight(want, "\n") {
				t.Fatalf("printed %q, the reference prints %q", stdout, want)
			}
			if !strings.Contains(stdout, tc.want) {
				t.Fatalf("the handler did not print %q: %q", tc.want, stdout)
			}
		})
	}
}

// Every answered shape must survive LLVM's own module verifier, and the module must carry the callee's
// call — the claim that the element was PERFORMED is a claim about the emitted instructions, not only
// about stdout (**ADR 0188**'s discipline: what a door promises, the module is shown doing).
func TestTheCountDoorAnsweringModulesVerifyAndCarryTheCall(t *testing.T) {
	t.Parallel()
	for _, src := range countAnswerEffectSources() {
		src := src
		t.Run(strings.ReplaceAll(strings.SplitN(src, "\n", 2)[0], " ", "_"), func(t *testing.T) {
			t.Parallel()
			ir, stderr, code := cliStreams(t, "--emit-llvm", src)
			if code != 0 {
				t.Fatalf("--emit-llvm failed (%d): %s", code, stderr)
			}
			if !strings.Contains(ir, "define i32 @main()") {
				t.Fatalf("no `main` in the module:\n%s", ir)
			}
			if strings.Contains(src, "print(len([probe()]))") || strings.Contains(src, "print(len(sorted([probe()])))") {
				if !strings.Contains(ir, "call i32 @gy_probe(") {
					t.Fatalf("the count door skipped the element's call — the module carries no call to `probe`:\n%s", ir)
				}
			}
			if strings.Contains(src, "print(len([1, 2, 3]))") {
				at := strings.Index(ir, "define i32 @main()")
				body := ir[at:]
				if end := strings.Index(body, "\n}"); end >= 0 {
					body = body[:end]
				}
				for _, forbidden := range []string{"call i32 @rt_alloc(", "call i32 @rt_set_elem("} {
					if strings.Contains(body, forbidden) {
						t.Fatalf("a container of spellings now builds an object (%s):\n%s", forbidden, body)
					}
				}
			}
			out, stderr, vcode := cliStreams(t, "--verify-llvm", src)
			if vcode != 0 {
				t.Fatalf("the module verifier rejected a program this row answers (%d): %s\n%s", vcode, stderr, out)
			}
		})
	}
}

// What the door still cannot reach, pinned with today's bytes at the CLI so the shape cannot quietly
// start answering (or quietly stop being measured) — the pair-argument element (**Gap R.542**) and the
// float-answering argument that reaches `llc` with a double in an `i32` slot (**Gap R.543**, identical on
// the build before this commit).
func TestWhatTheCountDoorStillSkipsAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
		owes            string
	}{
		{
			"an element whose argument is a container answer is still not performed",
			"def k():\n    print(\"k\")\n\n    return [9]\n\ndef f(x):\n    print(\"f\")\n\n    return [1]\n\nprint(len([f(k())]))\n",
			"1\n", "Gap R.542",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, tc.src)
			if strings.TrimRight(want, "\n") == strings.TrimRight(tc.want, "\n") {
				t.Fatalf("the reference now prints what this row pins (%q) — the row is paid and must move", tc.want)
			}
			stdout, stderr, code := cliStreams(t, "--eval", tc.src)
			if code == 2 {
				t.Fatalf("exit 2 on a program this row pins as an ANSWER (%s):\n%s\n%s", tc.owes, stderr, stdout)
			}
			if code != 0 || stdout != tc.want {
				t.Fatalf("%q printed %q at exit %d, want today's %q — the row that owns it is %s (the reference: %q)\n%s",
					tc.src, stdout, code, tc.want, tc.owes, want, stderr)
			}
		})
	}
	t.Run("an element whose argument answers a float still dies in the assembler", func(t *testing.T) {
		t.Parallel()
		src := "def f(x):\n    print(\"f\")\n\n    return [x]\n\ndef g():\n    return 2.5\n\nprint(len([f(g())]))\n"
		stdout, stderr, code := cliStreams(t, "--eval", src)
		if code != 2 {
			t.Fatalf("exit %d where this row is filed for the assembler's rejection (Gap R.543, exit 2 is the code ADR 0166 reserves for the compiler): stdout %q stderr %q", code, stdout, stderr)
		}
		if !strings.Contains(stderr, "double") {
			t.Fatalf("the assembler failed for a reason this row does not pin (Gap R.543):\n%s", stderr)
		}
	})
}

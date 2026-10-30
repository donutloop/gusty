package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// A slice whose base has no slots — a number, a verdict, the void — judged by a caller who sees only an exit
// code, stdout and the traceback the runtime wrote (roadmap **Gap R.559**, ADR 0468).
//
// ADR 0466 gave the slice road its base question for the two containers that have no positions. The bases with
// no slots at all were left on the old road, where the copy door refused the program:
//
//	$ gustyc --eval 'print(1[0:1])'
//	gustyc: jit: codegen: cannot slice 1: the copy is a list whose slots are the base's payloads with their
//	tags, and the base is not a list the records describe …            # exit 1
//	$ python3 -c 'print(1[0:1])'
//	TypeError: 'int' object is not subscriptable                       # exit 1, and the program's own
//
// The two lines above differ in more than their words: one is the compiler declining to build a program, the
// other is the program answering the runtime's question. A caller that branches on the exit code cannot tell a
// language that refuses `1[0:1]` from a language that raises on it, and the second is what CPython says the
// language DOES. So the expectations below are read out of a live CPython for each source (Gap N's standing
// rule) and demanded of the toolchain: exit 3, nothing on stdout, the reference's class and sentence on stderr
// under a frame naming the line, and — for the shapes the program catches — exit 0 with the handler's bytes.
//
// The history rows are kept too. Every one of these shapes reached `rt_slice` with something in its argument
// slot that was not a handle, which is how a slice road ends with `llc` rejecting the module at **exit 2** —
// the code the contract reserves for a compiler bug (ADR 0166). A row that regresses to exit 2 fails as loudly
// as one that regresses to a wrong answer.

// sliceScalarCLIRaises are the programs the reference traps by naming a kind. Nothing is asserted about the
// sentence in this file: it is read out of CPython for each source and demanded of the CLI.
var sliceScalarCLIRaises = []struct {
	name string
	src  string
}{
	// ---- the numbers, spelled ----
	{"a slice of an int literal", "print(1[0:1])"},
	{"a slice of an int the program parenthesised", "print((1)[0:1])"},
	{"a slice of a float literal", "print(1.5[0:1])"},
	{"a float sliced by nothing at all", "print(1.5[:])"},
	{"a float sliced by a step alone", "print(1.5[::3])"},
	// ---- the verdicts: an int where a position is asked, a verdict where the object is asked ----
	{"a slice of a verdict written True", "print(True[0:1])"},
	{"a slice of a verdict written False, by a step", "print(False[::2])"},
	{"a verdict sliced by text bounds", "print(True[\"a\":\"b\"])"},
	{"a verdict the program computed", "flag = 1 == 1\nprint(flag[0:1])"},
	// ---- the void ----
	{"a slice of None", "print(None[0:1])"},
	{"the void sliced by a step alone", "print(None[::])"},
	{"the void sliced by a float bound", "print(None[1.5:2])"},
	{"the void handed back by a binding", "o = None\nprint(o[0:1])"},
	// ---- the same objects in a name: the kind is the binding's, not the spelling's ----
	{"a slice of a name the program bound to a verdict", "b = True\nprint(b[0:1])"},
	{"a slice of a name the program bound to a float", "f = 1.5\nprint(f[:])"},
	{"a slice of a name the program bound to a computed float", "half = 1 / 2\nprint(half[0:1])"},
	// ---- the read the compiler already folds: an element of a literal, sliced ----
	{"a slice of the element of a literal list", "print([1, 2][0][0:1])"},
	{"a slice of the element of a literal tuple", "print((1, 2)[0][0:1])"},
	// ---- the base answers before any bound does ----
	{"text bounds over an int, which the bounds door never gets to complain about", "print(1[\"a\":\"b\"])"},
	{"a step of zero over an int, which is never strided", "print(1[0:1:0])"},
	{"a list bound over a float", "print(1.5[[1]:2])"},
	// ---- the raise arrives before the print, in a statement with two arguments ----
	{"the first of two printed slices raises and nothing is printed", "print(1[0:1], 2)"},
	{"the first statement raises and the second never runs", "print(1[0:1])\nprint(2[0:1])"},
	{"a raise from inside a function body", "def f():\n    return 1[0:1]\n\nprint(f())"},
	// ---- the counterweights: the bases the road serves, asked at the same altitude ----
	{"a list sliced by the same text bounds an int refuses on", "xs = [1, 2]\nprint(xs[\"a\":\"b\"])"},
	{"a set still says the object, not the bounds", "print({1, 2}[1.5:2])"},
	{"a dict still hashes the slice and misses", "print({1: 2}[1.5:2])"},
}

// sliceScalarCLICatchRows are the same raises as the program's own: the handler runs, the exit is 0, and the
// bytes are the reference's.
var sliceScalarCLICatchRows = []struct {
	name string
	src  string
}{
	{"except TypeError over an int slice", "try:\n    print(1[0:1])\nexcept TypeError:\n    print(\"caught\")"},
	{"except TypeError over a verdict slice", "b = True\ntry:\n    print(b[0:1])\nexcept TypeError:\n    print(\"caught\")"},
	{"except TypeError over the void", "try:\n    print(None[0:1])\nexcept TypeError:\n    print(\"caught\")"},
	{"the caught message is the raise's argument", "try:\n    print(1[0:1])\nexcept TypeError as e:\n    print(e)"},
	{"the caught message of a verdict", "try:\n    print(True[0:1])\nexcept TypeError as e:\n    print(e)"},
	{"two refusals, each caught by its own arm", "try:\n    print(1[0:1])\nexcept TypeError:\n    print(\"no slots\")\n\ntry:\n    print(None[0:1])\nexcept TypeError:\n    print(\"still no slots\")\n\nprint(\"done\")"},
	{"a handled refusal and a real slice", "try:\n    print(None[0:1])\nexcept TypeError:\n    print([1, 2][0:1])"},
	{"the program goes on after a handled refusal", "try:\n    print(1.5[0:1])\nexcept TypeError:\n    print(\"missed\")\n\nprint([1, 2, 3][1:])"},
	{"a refusal inside a function, caught outside it", "def f():\n    return True[0:1]\n\ntry:\n    print(f())\nexcept TypeError:\n    print(\"from the body\")"},
}

// sliceScalarCLIAnswers are the doors this row must not lean on: the bases the slice road serves still answer,
// at exit 0, byte for byte with CPython.
var sliceScalarCLIAnswers = []struct {
	name string
	src  string
}{
	{"a list still slices", "print([1, 2][0:1])"},
	{"a text still slices by counting characters", "print(\"abcd\"[1:3])"},
	{"a tuple still slices", "print((1, 2)[0:1])"},
	{"a name bound to a list still slices", "xs = [1, 2]\nprint(xs[0:2])"},
	{"a name that held a float and now holds a list still slices", "xs = 1.5\nxs = [1, 2]\nprint(xs[0:1])"},
	{"a verdict bound is still a position", "print([1, 2][True:2])"},
	{"a name bound to a float still does arithmetic", "f = 1.5\nprint(f + 1)"},
	{"the void still prints", "o = None\nprint(o)"},
}

// sliceScalarCLIRefusals are the shapes whose kind no record names, which the toolchain declines in words. The
// reference traps them; the row's claim is that the toolchain says so rather than answering wrongly or crashing
// (ADR 0273's rule that a gate may not invent a kind, ADR 0166's rule about exit 2).
var sliceScalarCLIRefusals = []struct {
	name string
	src  string
	gap  string
}{
	{
		"a name the program bound to an int has no kind record",
		"n = 1\nprint(n[0:1])",
		"Gap R.560",
	},
	{
		"a name a container bound and a scalar rebound",
		"xs = [1, 2]\nxs = 1.5\nprint(xs[0:1])",
		"Gap R.561",
	},
	{
		"the answer of a call has no scalar kind record",
		"def m(v):\n    return v\n\nprint(m(1)[0:1])",
		"Gap R.562",
	},
	{
		"a slot of a list the program named",
		"xs = [1, 2]\nprint(xs[0][0:1])",
		"Gap R.562",
	},
	{
		"a position read of a slice that raises",
		"print(None[0:1][0])",
		"Gap R.556",
	},
}

// TestASliceOfAValueWithNoSlotsRaisesThroughTheCLI is the row as a caller sees it: exit 3, nothing on stdout,
// and the reference's own class and sentence on stderr under a frame that names the line that raised (ADR 0211's
// exit-code contract, ADR 0354's traceback frame, Gap N's reference leg).
func TestASliceOfAValueWithNoSlotsRaisesThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range sliceScalarCLIRaises {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			refOut, refErr, rerr := lang.PythonRun(tt.src + "\n")
			if rerr == nil {
				t.Fatalf("the reference ANSWERS %q (%q); this row is filed as a raise — re-measure it before trusting the table", tt.src, refOut)
			}
			want := lastTracebackLine(refErr)
			if want == "" {
				t.Fatalf("cannot read the reference's trap for %q:\n%s", tt.src, refErr)
			}
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("exit 2 — the code the contract reserves for a compiler bug (ADR 0166):\n%s", stderr)
			}
			if code == 0 {
				t.Fatalf("exit 0 and stdout %q where the reference raises %s; a number printed where the program raises is the defect this row exists to stop", stdout, want)
			}
			if code != 3 {
				t.Fatalf("exit %d, want 3 (the uncaught-exception exit):\nstdout %q\nstderr %q", code, stdout, stderr)
			}
			if out := strings.TrimSpace(stdout); out != "" {
				t.Fatalf("%q printed %q on the way to the raise; the reference prints nothing", tt.src, out)
			}
			if strings.Contains(stderr, "codegen:") {
				t.Fatalf("the program was refused by the compiler rather than raised by the runtime (want %s):\n%s", want, stderr)
			}
			if !strings.Contains(stderr, want) {
				t.Fatalf("%q did not raise %q:\n%s", tt.src, want, stderr)
			}
			if !strings.Contains(stderr, "Traceback (most recent call last):") || !strings.Contains(stderr, "line ") {
				t.Fatalf("%q raised without a traceback frame naming the site:\n%s", tt.src, stderr)
			}
		})
	}
}

// TestAScalarBaseTrapIsCatchableThroughTheCLI is the raise as a value the program can take: exit 0 and the
// handler's bytes, line for line with CPython's.
func TestAScalarBaseTrapIsCatchableThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range sliceScalarCLICatchRows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			want := strings.TrimSpace(pythonOutput(t, tt.src+"\n"))
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("exit 2 — the code the contract reserves for a compiler bug (ADR 0166):\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: the reference runs this program to completion\nstdout %q\nstderr %q", code, stdout, stderr)
			}
			if got := strings.TrimSpace(stdout); got != want {
				t.Fatalf("%q printed %q, the reference prints %q", tt.src, got, want)
			}
		})
	}
}

// TestTheServedBasesStillAnswerThroughTheCLI is the ADR 0273 half at this altitude: the gate is a raise for the
// bases the language refuses, and silence for the ones it serves. A row here that starts exiting 3 is the gate
// reading the base where it should have read nothing.
func TestTheServedBasesStillAnswerThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range sliceScalarCLIAnswers {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			want := strings.TrimSpace(pythonOutput(t, tt.src+"\n"))
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("exit 2 — the code the contract reserves for a compiler bug (ADR 0166):\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0; the reference prints %q\nstdout %q\nstderr %q", code, want, stdout, stderr)
			}
			if got := strings.TrimSpace(stdout); got != want {
				t.Fatalf("%q printed %q, the reference prints %q", tt.src, got, want)
			}
		})
	}
}

// TestWhatTheScalarBaseDoorStillCannotReachRefusesThroughTheCLI keeps the honest residue visible to a machine
// reader: these programs raise in the reference and are DECLINED here, at exit 1 with a `codegen:` sentence —
// never answered wrongly (exit 0) and never with the compiler's own bug code (exit 2).
func TestWhatTheScalarBaseDoorStillCannotReachRefusesThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range sliceScalarCLIRefusals {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("exit 2 — the code the contract reserves for a compiler bug (ADR 0166):\n%s", stderr)
			}
			if code == 0 {
				t.Fatalf("exit 0 and stdout %q; this shape is filed as owed (%s) and an answer here has not been checked against the reference", stdout, tt.gap)
			}
			if code != 1 {
				t.Fatalf("exit %d, want 1 (the front-end refusal exit): %s\nstdout %q\nstderr %q", code, tt.gap, stdout, stderr)
			}
			if !strings.Contains(stderr, "codegen:") {
				t.Fatalf("the refusal does not read as a compiler refusal (%s owns this shape):\n%s", tt.gap, stderr)
			}
			// A refusal that leaks the compiler's own AST text is a refusal nobody can act on (Gap R.232's
			// disease). This one still does for shapes the road has not been given; the row records which.
			t.Logf("filed under %s; refusal: %s", tt.gap, strings.TrimSpace(lastLine(stderr)))
		})
	}
}

// TestTheScalarBaseGateHandsNoGlobalToTheCopyHelper is the history pinned at this altitude: a slice of a base
// with no slots reached `rt_slice` with something in the handle slot that was not a handle, which is how the
// road ended with `llc` turning the module down (ADR 0166). The gate replaces the read with a raise, so the
// module carries the raise and never carries the copy.
func TestTheScalarBaseGateHandsNoGlobalToTheCopyHelper(t *testing.T) {
	t.Parallel()
	for _, tt := range sliceScalarCLIRaises {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := lang.Compile(tt.src)
			if err != nil {
				t.Fatalf("the program was refused by the compiler: %v", err)
			}
			if strings.Contains(res.IR, "ret i32 @.") || strings.Contains(res.IR, "@rt_slice(i32 @.") {
				t.Fatalf("a global sits in a value position — the shape that made `llc` reject the module (ADR 0166):\n%s",
					bodyAroundGlobalReturn(res.IR))
			}
			if strings.Contains(res.IR, "call i32 @rt_slice(") {
				t.Fatalf("the slice road built a copy of a base that has no slots to copy:\n%s", bodyAroundGlobalReturn(res.IR))
			}
			if !strings.Contains(res.IR, "@exn_code") {
				t.Fatalf("the module carries no exception state, so nothing raised:\n%s", bodyAroundGlobalReturn(res.IR))
			}
		})
	}
}

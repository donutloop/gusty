package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// The slice road asks what its BASE is, judged by a caller who sees only an exit code, stdout and the
// traceback the runtime wrote (roadmap Gap R.234, ADR 0466).
//
// The shape at this altitude used to be a refusal that read like a missing feature and a family of shapes
// that read like a compiler bug. `print({1, 2}[0:1])` handed a set literal's i32 global to a helper whose
// argument is an i32 register, which `llc` rejected: **exit 2**, the code the contract reserves for a
// compiler bug (ADR 0166). `print({1: 2}[0:1])` reached the same helper, and before ADR 0335's gate the two
// bound spellings (`s = {1, 2}` / `print(s[0:1])`, `d = {1: 2}` / `print(d[0:1])`) printed the list `[1]` at
// the **exit code of success** — a copy of a dict, printed as a list, believed.
//
// The reference answers none of those with a list and none of them with a refusal:
//
//	$ python3 -c 'print({1, 2}[0:1])'
//	TypeError: 'set' object is not subscriptable
//	$ python3 -c 'print({1: 2}[0:1])'
//	KeyError: slice(0, 1, None)
//
// so every row below derives its expectation from a live CPython rather than from this file's memory (Gap N's
// standing rule): the class, the sentence and the bytes the handler prints are the reference's, and the CLI is
// what is measured against them. A refusal that arrives where the reference raises is the wrong-answer class
// one step away, which is why `codegen:` on stderr fails a row as loudly as a wrong exit code does.

// sliceBaseCLIRaises are the programs the reference traps. Nothing is asserted about the sentence here — it is
// read out of CPython for each source and demanded of the toolchain.
var sliceBaseCLIRaises = []struct {
	name string
	src  string
}{
	{"a slice of a set literal", "print({1, 2}[0:1])"},
	{"a slice of a set kept in a name", "s = {1, 2}\nprint(s[0:1])"},
	{"a set sliced by nothing at all", "print({1, 2}[:])"},
	{"a set sliced by a step alone", "print({1, 2}[::2])"},
	{"a set sliced by a float bound, which is the object's complaint not the bounds'", "print({1, 2}[1.5:2])"},
	{"a set sliced by a list bound, which never reaches hashing", "print({1, 2}[[1]:2])"},
	{"a set sliced by text bounds", "print({1, 2}[\"a\":\"b\"])"},
	{"a set filled by an unpacking", "a, b = {1, 2}, 0\nprint(a[0:1])"},
	{"a set handed back by a call", "def m():\n    return {1, 2}\n\nprint(m()[0:1])"},
	{"a set handed back by a call and then bound", "def m():\n    return {1, 2}\n\ns = m()\nprint(s[0:1])"},
	{"a slice of a dict literal", "print({1: 2}[0:1])"},
	{"a slice of a dict kept in a name", "d = {1: 2}\nprint(d[0:1])"},
	{"a dict sliced by nothing at all", "print({1: 2}[:])"},
	{"a dict sliced by None bounds", "print({1: 2}[None:None])"},
	{"a dict sliced by a float and a bool", "print({1: 2}[1.5:True])"},
	{"a dict sliced by bool bounds", "print({1: 2}[False:True])"},
	{"a dict keyed by bools, sliced by bools", "print({True: 2}[True:False])"},
	{"a dict sliced by text bounds", "print({1: 2}[\"a\":\"b\"])"},
	{"a dict sliced by a step of zero, which it only hashes", "print({1: 2}[0:1:0])"},
	{"a dict sliced by a parenthesised bound", "print({1: 2}[(1, \"a\"):2])"},
	{"a dict whose values are lists is still not a sequence", "d = {\"k\": [1, 2, 3]}\nprint(d[0:1])"},
	{"a dict handed back by a call", "def m():\n    return {1: 2}\n\nprint(m()[0:1])"},
	{"a dict sliced by a list bound", "print({1: 2}[[1]:2])"},
	{"a dict sliced by an unhashable high bound", "d = {1: 2}\nprint(d[0:[1]])"},
	{"a dict sliced by a dict bound", "print({1: 2}[{1: 2}:1])"},
	{"a dict sliced by a set bound", "print({1: 2}[{1}:1])"},
	{"an unhashable member of a tuple bound names the member", "print({1: 2}[(1, [2]):1])"},
	{"a dict raised over, with the raise arriving before any print", "d = {1: 2}\nprint(d[0:1])\nprint(d)"},
	{"the second of two slices, where the first one raises", "s = {1, 2}\nd = {1: 2}\nprint(s[0:1], d[0:1])"},
	// The base that counts keeps the bounds door: the same text bounds a set refuses with the object's own
	// sentence, a list answers with `slice indices must be integers …` (ADR 0336), and the gate must not have
	// swallowed that row on the way.
	{"a list sliced by the same bounds a dict misses on", "xs = [1, 2]\nprint(xs[\"a\":\"b\"])"},
	{"a list sliced by a list bound", "xs = [1, 2]\nprint(xs[[1]:2])"},
}

// sliceBaseCLICatchRows are the same raises as language values: the program's `except` takes them, and the
// bytes the handler prints are compared to the reference's bytes.
var sliceBaseCLICatchRows = []struct {
	name string
	src  string
}{
	{"except TypeError over a set slice", "s = {1, 2}\ntry:\n    print(s[0:1])\nexcept TypeError:\n    print(\"caught\")"},
	{"except KeyError over a dict slice", "d = {1: 2}\ntry:\n    print(d[0:1])\nexcept KeyError:\n    print(\"caught\")"},
	{"except TypeError over an unhashable bound", "d = {1: 2}\ntry:\n    print(d[[1]:2])\nexcept TypeError:\n    print(\"caught\")"},
	{"the caught KeyError's own str", "d = {1: 2}\ntry:\n    print(d[0:1])\nexcept KeyError as err:\n    print(err)"},
	{"the program goes on after a handled miss", "d = {1: 2}\ntry:\n    print(d[0:1])\nexcept KeyError:\n    print(\"missed\")\nprint(d[1])"},
	{"a handled set slice and a real list slice", "s = {1, 2}\nxs = [1, 2, 3]\ntry:\n    print(s[0:1])\nexcept TypeError:\n    print(xs[0:2])"},
	{"a base that changed kind answers, and does not raise", "d = {1: 2}\nd = [1, 2]\nprint(d[0:1])"},
	{"a dict read by a real key is untouched", "d = {1: 2}\nprint(d[1])"},
}

// TestASliceOfASetOrADictRaisesThroughTheCLI is the row as a caller sees it: exit 3 (the uncaught-exception
// exit), nothing on stdout, and the reference's own class and sentence on stderr, under a frame that names the
// line that raised (ADR 0211's exit-code contract, ADR 0354's traceback frame, Gap N's reference leg).
func TestASliceOfASetOrADictRaisesThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range sliceBaseCLIRaises {
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
				t.Fatalf("exit 0 and stdout %q where the reference raises %s; a copy of a set or a dict printed as a list is the defect this row exists to stop", stdout, want)
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
			// The raise names its site: the frame line is what makes the sentence traceable to the
			// statement, and it is the half Gap K.8 owed (ADR 0354).
			if !strings.Contains(stderr, "Traceback (most recent call last):") || !strings.Contains(stderr, "line ") {
				t.Fatalf("%q raised without a traceback frame naming the site:\n%s", tt.src, stderr)
			}
		})
	}
}

// TestASliceBaseTrapIsCatchableThroughTheCLI is the raise as a value the program can take: exit 0 and the
// handler's bytes, line for line with CPython's.
func TestASliceBaseTrapIsCatchableThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range sliceBaseCLICatchRows {
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

// TestTheSliceBaseGateHandsNoGlobalToTheCopyHelper is the row's history pinned at this altitude: every one of
// these shapes reached `rt_slice` with a module-level literal in its argument slot and `llc` turned the module
// down. The gate replaces the read with a raise, so the module must carry the raise AND must not carry the copy
// of a base the language refuses to slice (ADR 0166, ADR 0335's gate).
func TestTheSliceBaseGateHandsNoGlobalToTheCopyHelper(t *testing.T) {
	t.Parallel()
	for _, tt := range sliceBaseCLIRaises {
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
				t.Fatalf("the slice road built a copy of a base it raises for:\n%s", bodyAroundGlobalReturn(res.IR))
			}
			if !strings.Contains(res.IR, "@exn_code") {
				t.Fatalf("the module carries no exception state, so nothing raised:\n%s", bodyAroundGlobalReturn(res.IR))
			}
		})
	}
}

// lastTracebackLine is the reference's own `Class: message` — the line a caller reads. CPython also writes
// `SyntaxWarning` lines for a subscript over a set literal (`perhaps you missed a comma?`), and those are the
// interpreter's opinion of the source, not the program's trap, so a line that carries a warning, a frame or an
// indentation gutter is not a candidate.
func lastTracebackLine(stderr string) string {
	lines := strings.Split(strings.TrimRight(stderr, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "Traceback ("), strings.HasPrefix(line, "File \""):
			continue
		case strings.HasPrefix(lines[i], " "):
			continue // the source echo the traceback prints under the frame
		case strings.Contains(line, "Warning"):
			continue
		}
		colon := strings.Index(line, ": ")
		if colon <= 0 {
			continue
		}
		if head := line[:colon]; strings.ContainsAny(head, " \t'\"") {
			continue
		}
		return line
	}
	return ""
}

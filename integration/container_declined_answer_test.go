package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// A function whose container the answer table declines, through the CLI (roadmap Gap R.261, ADR 0344).
//
// `containerAnswerScan` answers two questions and, before this row, only one of them was read: which
// functions hand back a container, and — separately — whether the table can describe what their slots hold.
// A slot filled by a call, a comprehension or a format is declined, rightly, because such a value can arrive
// as the (payload, tag) pair and a table that printed the pair as a number was Gap R.67's bug:
//
//	def g():
//	    return 5
//
//	def f():
//	    out = {}
//	    out["k"] = g()       # a slot the table cannot follow
//	    return out
//
//	print(f())               # CPython: {'k': 5}
//	                         # this toolchain before the row: 0    at exit 0
//
// `f` was absent from the table, absence was read as "not a container", the ordinary call road handed the
// heap handle to the number printer, and the run printed the allocator's slot at the exit code that means the
// program was answered. Measured off `python3` before the doors were touched: `print(f())` printed `0` over
// each of the three kinds the scan proves — a list, a set and a dict, whose references print `[5]`, `{5}` and
// `{'k': 5}` — a dict filled by a float-returning call printed `0` where the reference prints `{'k': 2.5}`, and
// `for v in f(): print(v)` printed nothing at all where the reference prints `5`; all of them at exit 0.
//
// What the row does is ask each position WHO answers the question it is about to ask: print, str, repr, a
// condition, a binding and a membership test ask the OBJECT, and take the declined answer; an index read, a
// comparison, an arithmetic position and a loop's iterable read a slot back through the notebooks the scan
// writes, and refuse with the missing half named.
//
// Every row below is judged against CPython: the expectation is what `python3` printed for the same source,
// never what this file remembers (Gap N's standing rule).

const declinedPreamble = "def g():\n    return 5\n\ndef f():\n    out = []\n    out.append(g())\n    return out\n"

// declinedAnswered are the positions that ask the OBJECT: print, str, repr, a condition, a binding, a
// membership test. Each now asks the object what it is, which is what `ys = f()` has always done and what the
// rest of this row had no excuse for not doing.
var declinedAnswered = []struct {
	src  string
	want string
}{
	{declinedPreamble + "\nprint(f())\n", "[5]"},
	{declinedPreamble + "\nprint(str(f()))\n", "[5]"},
	{declinedPreamble + "\nprint(repr(f()))\n", "[5]"},
	{declinedPreamble + "\nif f():\n    print(\"yes\")\n", "yes\n"},
	{declinedPreamble + "\nprint(not f())\n", "False"},
	// The binding road, which answered before the row: asserted so a widening of the doors above cannot
	// quietly move the answer off the road that has been right about this object all along.
	{declinedPreamble + "\nys = f()\nprint(ys)\n", "[5]"},
	{declinedPreamble + "\nys = f()\nprint(len(ys))\n", "1"},
	{declinedPreamble + "\nys = f()\nfor v in ys:\n    print(v)\n", "5\n"},
	{declinedPreamble + "\nprint(5 in f())\n", "True"},
	// The other two kinds the scan proves the same way — and the two whose wrong answers were measured:
	// a dict whose VALUE slot is filled by a call printed the handle as `0`, and filled by a float-returning
	// call printed `0` too, both at exit 0 (roadmap Gap R.261).
	{"def g():\n    return 5\n\ndef f():\n    out = {}\n    out[\"k\"] = g()\n    return out\n\nprint(f())\n", "{'k': 5}"},
	{"def g():\n    return 2.5\n\ndef f():\n    out = {}\n    out[\"k\"] = g()\n    return out\n\nprint(f())\n", "{'k': 2.5}"},
	{"def g():\n    return 5\n\ndef f():\n    out = set()\n    out.add(g())\n    return out\n\nprint(f())\n", "{5}"},
	{"def g():\n    return 5\n\ndef f():\n    out = {}\n    out[\"k\"] = g()\n    return out\n\nprint(str(f()))\n", "{'k': 5}"},
	{"def g():\n    return 5\n\ndef f():\n    out = {}\n    out[\"k\"] = g()\n    return out\n\nprint(\"k\" in f())\n", "True"},
	// A slot filled by a call that answers a container: the object inside the object.
	{"def g():\n    return [1, 2]\n\ndef f():\n    out = []\n    out.append(g())\n    return out\n\nprint(f())\n", "[[1, 2]]"},
	// The described answer, unchanged: what the table can follow keeps the road ADR 0333 built for it.
	{"def build():\n    return [1, 2]\n\nprint(build())\n", "[1, 2]"},
	{"def build():\n    return [1, 2]\n\nfor v in build():\n    print(v)\n", "1\n2\n"},
	{"def build():\n    return {\"a\": 1}\n\nprint(build())\n", "{'a': 1}"},
	{"def f(n):\n    return [n, n * 2]\n\nprint(f(3))\n", "[3, 6]"},
}

// TestTheDeclinedContainerAnswerAnswersThroughTheCLI is the answering half: reference and CLI on the same
// source, byte for byte. Exit 2 — the code ADR 0166 reserves for a compiler bug — is forbidden on every row.
func TestTheDeclinedContainerAnswerAnswersThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range declinedAnswered {
		name := strings.ReplaceAll(strings.TrimSpace(tt.src), "\n", "; ")
		if len(name) > 70 {
			name = name[:70]
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, tt.src)
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: the reference answers this program\nstderr %q", code, stderr)
			}
			if got := strings.TrimSpace(stdout); got != strings.TrimSpace(want) {
				t.Fatalf("printed %q, the reference prints %q", got, strings.TrimSpace(want))
			}
		})
	}
}

// TestTheDeclinedContainerAnswerIsRefusedThroughTheCLI covers the positions that read a slot back through
// the notebooks this table writes — arithmetic, an element, an equality, an f-string field, a loop's iterable,
// an index read. The reference answers each of them; this toolchain refuses, with the missing half named, at
// the code that means "the program asked for what the language does not have" (ADR 0166, ADR 0006). Before
// the row each printed a number the program never wrote — or nothing at all — at exit 0.
func TestTheDeclinedContainerAnswerIsRefusedThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		src  string
	}{
		{"arithmetic", declinedPreamble + "\nprint(f() + 1)\n"},
		{"an element of a list", declinedPreamble + "\nprint([f(), 9])\n"},
		{"an equality", declinedPreamble + "\nprint(f() == [5])\n"},
		{"an f-string field", declinedPreamble + "\nprint(f\"{f()}\")\n"},
		// The loop's iterable is the row's loudest case: the toolchain printed NOTHING at exit 0 for a
		// program whose reference prints `5`, because the loop road walked an answer whose element notebook
		// was never written and found a count of its own to believe.
		{"a loop's iterable", declinedPreamble + "\nfor v in f():\n    print(v)\n"},
		{"a loop over a dict answer", "def g():\n    return 5\n\ndef f():\n    out = {}\n    out[\"k\"] = g()\n    return out\n\nfor k in f():\n    print(k)\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 0 {
				t.Fatalf("%q printed %q at exit 0 — a heap handle is not an answer", tt.name, stdout)
			}
			if code == 2 {
				t.Fatalf("%q reached the compiler-bug code:\n%s", tt.name, stderr)
			}
			if stdout != "" {
				t.Fatalf("%q printed %q before refusing; the refusal is the whole answer\n%s", tt.name, stdout, stderr)
			}
			if !strings.Contains(stderr, "hands back a container whose slots this backend will not describe") {
				t.Fatalf("%q refused with somebody else's sentence:\n%s", tt.name, stderr)
			}
			// The sentence names the function the source wrote and tells the reader what to do next; it
			// does not print the emitter's own machinery (ADR 0166, and Gap R.232's shape for a refusal).
			if !strings.Contains(stderr, `"f"`) || !strings.Contains(stderr, "bind it to a name") {
				t.Fatalf("%q's refusal names neither the function nor the way around it:\n%s", tt.name, stderr)
			}
			for _, leak := range []string{"%!s(", "*lang.", "__cans", "0x"} {
				if strings.Contains(stderr, leak) {
					t.Fatalf("%q's refusal leaks %s:\n%s", tt.name, leak, stderr)
				}
			}
		})
	}
}

// TestTheDeclinedContainerAnswerModulesVerify asks the claim of the artifact rather than of the run: the
// answer arrives as a rooted slot, the container is filled by the tagged append, and every module the row
// produces — answering or refusing — is LLVM the toolchain accepts (AGENTS.md's verification rule).
func TestTheDeclinedContainerAnswerModulesVerify(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, tt := range []struct {
		name string
		src  string
		want []string
	}{
		{"print asks the object", declinedPreamble + "\nprint(f())\n",
			[]string{"call i32 @gy_f(", "call i32 @gy_g(", "@rt_append_tagged(", "rt_str_of_container("}},
		{"a condition asks the object", declinedPreamble + "\nif f():\n    print(\"yes\")\n",
			[]string{"call i32 @gy_f(", "@rt_append_tagged("}},
		{"a binding asks the object", declinedPreamble + "\nys = f()\nprint(ys)\n",
			[]string{"call i32 @gy_f(", "@rt_append_tagged("}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := lang.Compile(tt.src)
			if err != nil {
				t.Fatalf("%s: the compiled leg refused a program the reference answers: %v", tt.name, err)
			}
			for _, want := range tt.want {
				if !strings.Contains(res.IR, want) {
					t.Fatalf("%s: the module does not ask %s:\n%s", tt.name, want, res.IR)
				}
			}
			irPath := filepath.Join(dir, strings.ReplaceAll(tt.name, " ", "_")+".ll")
			if err := os.WriteFile(irPath, []byte(res.IR), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
			llc := "llc-20"
			if _, err := exec.LookPath(llc); err != nil {
				llc = "llc"
			}
			out, err := exec.Command(llc, "-filetype=null", irPath).CombinedOutput()
			if err != nil {
				t.Fatalf("%s: %s rejected the module: %v\n%s\nIR:\n%s", tt.name, llc, err, out, res.IR)
			}
		})
	}
}

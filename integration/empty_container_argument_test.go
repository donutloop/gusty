package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// empty_container_argument_test.go is the CLI half of roadmap Gap R.369 / ADR 0394.
//
// An empty container spelled as a CALL — `list()`, `set()`, `dict()` — stored in a container literal and
// read back into a call argument used to arrive as an integer. The literal store wrote the slot with a tag
// that named no container, the pair scan refused to call a `Call` a self-describing slot and closed the
// parameter, and the ordinary road handed the callee one word: the heap handle. The row program printed `3`
// and took the `then` arm where CPython prints `[]` and takes the `else`, at the exit code of success.
//
// Each row is asked of CPython first — the table's own expectation is checked against the reference inside
// the case, so a row cannot drift into pinning this backend — and then of both compiled legs, with exit 2
// (the compiler refusing its own module) forbidden.

var emptyContainerArgumentCLIPrograms = []struct{ name, src, want string }{
	// The three spellings, at the two positions the row owns: the print door and the truth door, asked of
	// the same entry so a fix that opens only one of them cannot pass.
	{
		name: "the dict entry spelled as list()",
		src:  "def f(v):\n    print(v)\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {\"k\": list()}\nf(d[\"k\"])\n",
		want: "[]\nf\n",
	},
	{
		name: "the dict entry spelled as set()",
		src:  "def f(v):\n    print(v)\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {\"k\": set()}\nf(d[\"k\"])\n",
		want: "set()\nf\n",
	},
	{
		name: "the dict entry spelled as dict()",
		src:  "def f(v):\n    print(v)\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {\"k\": dict()}\nf(d[\"k\"])\n",
		want: "{}\nf\n",
	},
	{
		// The list side of the same store: a member, not an entry. `xs = [set()]` was refused outright
		// before this row, with Gap R.82's mixed-list sentence, because the member carried no kind for the
		// scan to read.
		name: "the list member spelled as set()",
		src:  "def f(v):\n    print(v)\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nxs = [set(), [1]]\nf(xs[0])\nf(xs[1])\n",
		want: "set()\nf\n[1]\nt\n",
	},
	{
		name: "the list member spelled as dict()",
		src:  "def f(v):\n    print(v)\n\nxs = [dict()]\nf(xs[0])\n",
		want: "{}\n",
	},
	// Nesting: the scan's question is asked at every depth of the literal.
	{
		name: "a nested list element spelled as a call",
		src:  "def f(v):\n    print(v)\n\nxs = [[list()], [1, 2]]\nf(xs[0])\nf(xs[1])\n",
		want: "[[]]\n[1, 2]\n",
	},
	{
		name: "a nested dict value spelled as a call",
		src:  "def f(v):\n    print(v)\n\nd = {\"k\": {\"m\": set()}}\nf(d[\"k\"])\n",
		want: "{'m': set()}\n",
	},
	{
		name: "a dict entry whose value is a list holding the call spelling",
		src:  "def f(v):\n    print(v)\n\nd = {\"k\": [list()]}\nf(d[\"k\"])\n",
		want: "[[]]\n",
	},
	// The whole family in one dict: one entry's spelling no longer decides the answer for the literal.
	{
		name: "three spellings and the ordinary entries of one dict",
		src:  "def f(v):\n    print(v)\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {\"a\": list(), \"b\": set(), \"c\": dict(), \"i\": 5, \"j\": \"hi\", \"l\": None}\nf(d[\"a\"])\nf(d[\"b\"])\nf(d[\"c\"])\nf(d[\"i\"])\nf(d[\"j\"])\nf(d[\"l\"])\n",
		want: "[]\nf\nset()\nf\n{}\nf\n5\nt\nhi\nt\nNone\nf\n",
	},
	// The roads that never needed the scan keep their answers: a binding, a direct argument, a run-time
	// store. A fix that opened the door by rebuilding one road around `list()` would break these.
	{
		name: "the same value held in a name",
		src:  "def f(v):\n    print(v)\n\nw = list()\nf(w)\n",
		want: "[]\n",
	},
	{
		name: "the same value handed over directly",
		src:  "def f(v):\n    print(v)\n\nf(list())\n",
		want: "[]\n",
	},
	{
		name: "the same value stored after the dict was made",
		src:  "def f(v):\n    print(v)\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {}\nd[\"k\"] = list()\nf(d[\"k\"])\n",
		want: "[]\nf\n",
	},
	// The door is the emitter's `emptyContainerCall`, and that door is shut where the PROGRAM took the name.
	// A user's `def set(): return 1` is an integer wearing the spelling of a set; labelling its payload as a
	// container would be a fact about nothing, so the whole family declines and the program keeps the road it
	// always had.
	{
		name: "a program that defines set owns that spelling",
		src:  "def set():\n    return 1\n\ndef f(v):\n    print(v)\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {\"k\": set()}\nprint(d)\nprint(d[\"k\"])\nf(d[\"k\"])\n",
		want: "{'k': 1}\n1\n1\nt\n",
	},
	{
		// The control on the other side: a parameter of an UNRELATED function is that body's own binding and
		// registers no definition under the name. A shadowing gate stricter than the door it copies would
		// close this family's parameter for a spelling the store goes on labelling a container.
		name: "an unrelated parameter named list does not take the name",
		src:  "def g(list):\n    return list\n\ny = g(4)\n\ndef f(v):\n    print(v)\n\nd = {\"k\": list()}\nf(d[\"k\"])\n",
		want: "[]\n",
	},
}

func TestTheEmptyContainerSpelledAsACallCrossesAtTheCLI(t *testing.T) {
	for _, tc := range emptyContainerArgumentCLIPrograms {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			py, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Fatalf("the reference failed to answer this program: src: %s", tc.src)
			}
			if py != tc.want {
				t.Fatalf("this table's expectation is not the reference's: table %q, CPython %q\nsrc: %s", tc.want, py, tc.src)
			}
			path := writeSrc(t, t.TempDir(), "empty_container_argument.gy", tc.src)
			for _, engine := range []string{"--jit", "--aot"} {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("%s: the compiled leg rejected the compiler's own module (ADR 0166):\n%s", engine, cliRun(t, engine, path))
				}
				if code != 0 {
					t.Fatalf("%s: exit %d, want 0: %s", engine, code, cliRun(t, engine, path))
				}
				if out != tc.want {
					t.Errorf("%s: printed %q, want CPython's %q\nsrc: %s", engine, out, tc.want, tc.src)
				}
			}
		})
	}
}

func TestTheEmptyContainerArgumentModulesVerify(t *testing.T) {
	for _, tc := range emptyContainerArgumentCLIPrograms {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := lang.Compile(tc.src)
			if err != nil {
				t.Fatalf("refused: %v", err)
			}
			v, verr := lang.VerifyModuleIR(res.IR, 0)
			if verr != nil || !v.OK {
				t.Fatalf("module does not verify: %v %v", v.Errors, verr)
			}
		})
	}
}

// The conformance program: the whole family in one file, line for line what CPython prints.
const emptyContainerArgumentProgram = "empty_container_argument"

func TestTheEmptyContainerArgumentProgramMatchesTheReference(t *testing.T) {
	t.Parallel()
	src := readProgramSrc(emptyContainerArgumentProgram)
	want := pythonOutput(t, src)
	if strings.TrimSpace(want) == "" {
		t.Fatal("the reference printed nothing for the conformance program")
	}
	path := writeSrc(t, t.TempDir(), emptyContainerArgumentProgram+".gy", src)
	for _, engine := range []string{"--jit", "--aot"} {
		out, code := cliRunCode(t, engine, path)
		if code != 0 {
			t.Fatalf("%s: exit %d, want 0: %s", engine, code, cliRun(t, engine, path))
		}
		if out != want {
			t.Errorf("%s printed %q, the reference prints %q", engine, out, want)
		}
	}
}

func TestTheEmptyContainerArgumentProgramIsOnTheRecord(t *testing.T) {
	t.Parallel()
	src := readProgramSrc(emptyContainerArgumentProgram)
	// A missing record is a failure, not a skip: the record leg is one of the two witnesses this feature is
	// judged on, and coverage cannot be allowed to disappear by a record going away.
	if !lang.HasGoldenAnswer(src) {
		t.Fatal("the conformance program is not on the record — record it from the reference with tools/recmerge")
	}
	// The record's bytes are compared to the reference, and the compiled leg to the record, by
	// `TestTheEmptyContainerCallArgumentAgreesWithTheRecord` in `pkg/lang`, where the record is read.
}

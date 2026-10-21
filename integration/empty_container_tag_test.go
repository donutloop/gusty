package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// empty_container_tag_test.go is the CLI half of roadmap Gap R.360 / ADR 0391.
//
// `d = {None: set()}` / `print(d[None])` printed `3` at the exit code that means the program was answered;
// `print([set(), list(), dict()])` printed `[1, 2, 3]`. The objects were right — the LABEL beside each
// handle in its slot said "integer", and every door downstream of that label believed it.
//
// Every row here is judged against `python3` run inside the case, on both compiled legs, and no row is
// allowed to reach exit 2 — ADR 0166's class, which a container handed to a number printer is exactly the
// way into.

var emptyContainerCLIPrograms = []struct {
	name, src, want string
}{
	{"a slot of a list literal", "xs = [set()]\nprint(xs[0])\n", "set()\n"},
	{"a slot spelled list", "xs = [list()]\nprint(xs[0])\n", "[]\n"},
	{"a slot spelled dict", "xs = [dict()]\nprint(xs[0])\n", "{}\n"},
	{"a dict entry", "d = {None: set()}\nprint(d[None])\n", "set()\n"},
	{"a dict entry spelled list", "d = {None: list()}\nprint(d[None])\n", "[]\n"},
	{"printed whole", "print([set(), list(), dict()])\n", "[set(), [], {}]\n"},
	{"printed among other kinds", "print([1, list(), None, \"a\", {1: 2}])\n", "[1, [], None, 'a', {1: 2}]\n"},
	{"a set beside a number", "xs = [set(), 1]\nprint(xs)\n", "[set(), 1]\n"},
	{"a dict value printed whole", "print({\"k\": set()})\n", "{'k': set()}\n"},
	{"an append of an empty set", "xs = []\nxs.append(set())\nprint(xs)\n", "[set()]\n"},
	{"an entry assigned from the empty call", "d = {}\nd[\"k\"] = set()\nprint(d)\n", "{'k': set()}\n"},
	{"the truth of a read slot", "print(bool([set()][0]))\n", "False\n"},
	{"the truth of comprehension items", "print([bool(x) for x in [set(), list()]])\n", "[False, False]\n"},
	{"a condition on a dict entry", "d = {None: set()}\nif d[None]:\n    print(\"t\")\nelse:\n    print(\"f\")\n", "f\n"},
	{"a ternary test on a dict entry", "d = {None: set()}\nprint(\"y\" if d[None] else \"n\")\n", "n\n"},
	{"a filter that keeps the empties", "print([x for x in [set(), list()] if not x])\n", "[set(), []]\n"},
	{"a loop over the slot", "xs = [set()]\nfor v in xs:\n    print(v)\n", "set()\n"},
	{"the count of a container slot", "xs = [set()]\nprint(len(xs[0]))\n", "0\n"},
	{"the count of a list slot", "xs = [list()]\nprint(len(xs[0]))\n", "0\n"},
	{"two built lists of sets are equal", "print([set()] == [set()])\n", "True\n"},
	{"a slot equals the spelling it came from", "xs = [set()]\nprint(xs[0] == set())\n", "True\n"},
	{"nested one level deeper", "print([[set()], [list()]])\n", "[[set()], [[]]]\n"},
	{"a dict of lists of sets", "print({\"a\": [set()]})\n", "{'a': [set()]}\n"},
	{"bound from a slot", "d = {\"a\": set()}\ns = d[\"a\"]\nprint(s)\n", "set()\n"},
	{"str of a slot", "d = {\"a\": set()}\nprint(str(d[\"a\"]))\n", "set()\n"},
	{"a name built first", "s = set()\nxs = [s]\nprint(xs[0])\n", "set()\n"},
	{"a comprehension over empty containers", "print([x for x in [set()]])\n", "[set()]\n"},
	{"a list of the loop variable", "print([[x] for x in [set(), list()]])\n", "[[set()], [[]]]\n"},
	// The controls: what the road answered before, and still answers.
	{"an all-integer list still folds", "print([1, 2])\n", "[1, 2]\n"},
	{"an integer slot still reads", "xs = [1, 2]\nprint(xs[0])\n", "1\n"},
	{"the empty set printed whole", "print(set())\n", "set()\n"},
	{"set members still print", "print({1, 2})\n", "{1, 2}\n"},
	{"a shadowing definition still wins", "def set():\n    return 1\n\nxs = [set()]\nprint(xs)\n", "[1]\n"},
}

func TestAnEmptyContainerWrittenAsACallAnswersTheReferenceAtTheCLI(t *testing.T) {
	for _, tc := range emptyContainerCLIPrograms {
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
			path := writeSrc(t, t.TempDir(), "empty_container_tag.gy", tc.src)
			for _, engine := range []string{"--jit", "--aot"} {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("%s: the compiled leg rejected the compiler's own module (ADR 0166):\n%s",
						engine, cliRun(t, engine, path))
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

// Every module this row produces has to verify: the shape that broke was a container's handle reaching a
// number printer, and a tag written beside the wrong payload is the same class of mistake one word later.
func TestTheEmptyContainerModulesVerify(t *testing.T) {
	for _, tc := range emptyContainerCLIPrograms {
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

// The refusals, at the CLI: exit 1 with the missing half named, never exit 2, and never the compiler's own
// machinery in the sentence. `{set(): 1}` is the case this row decided by hand — the reference RAISES
// `TypeError: unhashable type: 'set'`, this backend REFUSES the way it refuses a container key spelled as
// a literal, and Gap R.81 owns the day both spellings raise.
func TestWhatTheEmptyContainerCallRefusesAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{"a container key spelled as a call", "print({set(): 1})\n", "cannot hold an empty set"},
		{"a list key spelled as a call", "print({list(): 1})\n", "cannot hold an empty list"},
		{"a dict key spelled as a call", "print({dict(): 1})\n", "cannot hold an empty dict"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			gy := writeSrc(t, dir, "empty_container_refuse.gy", tc.src)
			out, code := cliRunMerged(t, "--file", gy)
			if code == 2 {
				t.Fatalf("exit 2 is a compiler bug, not a refusal:\n%s", out)
			}
			if code != 1 {
				t.Fatalf("exit %d, want 1:\n%s", code, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("refusal %q does not name %q", out, tc.want)
			}
			for _, leak := range []string{"*lang.", "%!s(", "0x"} {
				if strings.Contains(out, leak) {
					t.Fatalf("the refusal leaks %q: %s", leak, out)
				}
			}
		})
	}
}

// The record leg at the CLI: no row of this table passes on this backend's own say-so.
func TestTheEmptyContainerProgramsAreOnTheRecordAtTheCLI(t *testing.T) {
	asked := 0
	for _, tc := range emptyContainerCLIPrograms {
		t.Run(tc.name, func(t *testing.T) {
			if !lang.HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge, never from this backend", tc.src)
			}
			asked++
		})
	}
	if asked == 0 {
		t.Fatal("not one of this row's programs is on the record: the record leg is not being walked")
	}
}

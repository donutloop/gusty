package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// empty_container_decide_test.go is the CLI half of roadmap Gap R.364 / ADR 0392.
//
// `print(len(set()))` stopped the build with `len requires an inline list/dict/set literal`,
// `print(list() == [])` with `comparing a container with an empty list needs a tagged value`, and two more
// answered at the exit code that means the program ran: `print(set() == set())` printed `False` and
// `print(abs(list()))` printed `0`. Every one of them is a door that decides an expression's kind from its
// SPELLING rather than from the object — while the same value written as a literal, or bound to a name
// first, answered correctly on the very same road.
//
// At the terminal each row either prints CPython byte for byte, raises CPython's class and sentence at
// exit 3, or refuses with the missing half named — never exit 2, and never a number wearing a handle.

var emptyContainerDecideCLIPrograms = []struct {
	name, src, want string
}{
	{"len of an empty list call", "print(len(list()))\n", "0\n"},
	{"len of an empty set call", "print(len(set()))\n", "0\n"},
	{"len of an empty dict call", "print(len(dict()))\n", "0\n"},
	{"two lengths added", "print(len(list()) + len(set()))\n", "0\n"},
	{"a filled set by name", "s = set()\ns.add(1)\nprint(len(s))\n", "1\n"},
	{"an empty list equals the literal", "print(list() == [])\n", "True\n"},
	{"the literal equals an empty list", "print([] == list())\n", "True\n"},
	{"two empty sets are equal", "print(set() == set())\n", "True\n"},
	{"an empty dict equals the literal", "print(dict() == {})\n", "True\n"},
	{"inequality answers the other way", "print(list() != [])\n", "False\n"},
	{"an empty set is not a filled one", "print(set() == {1})\n", "False\n"},
	{"an empty list is not a filled one", "print(list() == [1])\n", "False\n"},
	{"lists of empty sets compare by content", "print([set()] == [set()])\n", "True\n"},
	{"membership by content", "print(list() in [[]])\n", "True\n"},
	{"sorting an empty set", "print(sorted(set()))\n", "[]\n"},
	{"sorting an empty list", "print(sorted(list()))\n", "[]\n"},
	{"concatenation on the right", "print(list() + [1])\n", "[1]\n"},
	{"concatenation on the left", "print([1] + list())\n", "[1]\n"},
	{"repeating an empty list", "print(list() * 2)\n", "[]\n"},
	// Controls: what the road answered before, and still answers.
	{"a literal length", "print(len([1, 2]))\n", "2\n"},
	{"a literal equality", "print([1] == [1])\n", "True\n"},
	{"a named set still counts", "xs = set()\nprint(len(xs))\n", "0\n"},
	{"a literal sum still folds", "print(sum([1, 2]))\n", "3\n"},
}

func TestTheDecidingDoorsAnswerTheEmptyContainerCallAtTheCLI(t *testing.T) {
	for _, tc := range emptyContainerDecideCLIPrograms {
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
			path := writeSrc(t, t.TempDir(), "empty_container_decide.gy", tc.src)
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

// The traps, at the terminal: the exit class is the program's raise (3), not the compiler's refusal (1)
// and not a broken module (2), and the sentence is CPython's word for word — `abs` names the operation and
// the unary minus names its own, which is the distinction ADR 0271 and ADR 0266 exist to keep.
func TestTheDecidingDoorsRaiseAtTheCLIForAnEmptyContainerCall(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{"abs of an empty list", "print(abs(list()))\n", "TypeError: bad operand type for abs(): 'list'"},
		{"abs of an empty set", "print(abs(set()))\n", "TypeError: bad operand type for abs(): 'set'"},
		{"abs of an empty dict", "print(abs(dict()))\n", "TypeError: bad operand type for abs(): 'dict'"},
		{"negating an empty list", "print(-list())\n", "TypeError: bad operand type for unary -: 'list'"},
		{"negating an empty set", "print(-set())\n", "TypeError: bad operand type for unary -: 'set'"},
		{"negating an empty dict", "print(-dict())\n", "TypeError: bad operand type for unary -: 'dict'"},
		{"abs of a list literal, as it always did", "print(abs([1]))\n", "TypeError: bad operand type for abs(): 'list'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			gy := writeSrc(t, dir, "empty_container_trap.gy", tc.src)
			out, code := cliRunMerged(t, "--file", gy)
			if code == 2 {
				t.Fatalf("exit 2 is a compiler bug, not the program's raise:\n%s", out)
			}
			if code != 3 {
				t.Fatalf("exit %d, want 3 (the program's own raise):\n%s", code, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("the raise reads %q, want it to contain %q", out, tc.want)
			}
		})
	}
	// …and each raise is catchable, which a refusal can never be.
	const catching = "try:\n    abs(list())\nexcept TypeError:\n    print(\"caught\")\nprint(\"after\")\n"
	dir := t.TempDir()
	gy := writeSrc(t, dir, "empty_container_catch.gy", catching)
	out, code := cliRunMerged(t, "--file", gy)
	if code != 0 || out != "caught\nafter\n" {
		t.Fatalf("the raise was not catchable: exit %d, %q", code, out)
	}
	if want := pythonOutput(t, catching); want != "caught\nafter\n" {
		t.Fatalf("the reference reads %q, want %q", want, "caught\nafter\n")
	}
}

func TestTheEmptyContainerDecideModulesVerify(t *testing.T) {
	for _, tc := range emptyContainerDecideCLIPrograms {
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

func TestTheEmptyContainerDecideProgramsAreOnTheRecordAtTheCLI(t *testing.T) {
	asked := 0
	for _, tc := range emptyContainerDecideCLIPrograms {
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

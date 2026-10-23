package integration

import (
	"regexp"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// integration/pop_tag_test.go — the REFERENCE leg of **ADR 0410** (roadmap Gap R.411): what the LINKED
// PROGRAM prints when a `list.pop()` takes an element out of a list whose slots the compiler cannot read.
//
// The row's claim is small enough to say in one line — a slot is a payload AND the tag that says what the
// payload means, and the door that removes the slot has to move both and hand both back — and wide enough
// that every reader of the answer has to be asked on its own: the printer, `str`, the truth road, a binding,
// a function's parameter, a list literal, a tuple literal, a set, a dict value, a sum, an ordering. So this
// file is not one program and its eight expectations; it is the same removal asked of eighteen programs,
// each of them checked against the pinned CPython INSIDE the case, so a table whose expectation drifted fails
// here rather than certifying the drift.
//
// Three things are checked that no test in this repository checked before:
//
//   - the TAIL of a popped list, printed and read by position, which is what the shift actually leaves
//     behind (`[2, 'a']`, not `[2, 0]`) — the answer nobody asked because `pop` was believed to be about the
//     element it returns;
//   - the two IndexError sentences, which are two programs' errors and not one: `pop from empty list` for the
//     empty list and `pop index out of range` for the rest. This backend had one, and it was the wrong one;
//   - that a raise is a raise: exit **3** with CPython's class and sentence, and never exit **1** (a refusal
//     from the compiler) and never exit **2** (ADR 0166 reserves that for a module `llc` rejected).
//
// The measurement, with a build of the parent commit `0f798e8` beside a build of this one, over the programs
// these two files carry: **25 of them moved to the reference's answer and none moved away from it.** The rows
// that still do not agree are pinned below rather than dropped — two refusals this row does not own (a
// module-level container reached from a body is **Gap R.35**; a container method over a name is the fold that
// owns `insert`/`clear`/`copy`) and three answers this row files instead of hiding (**Gap R.412**,
// **Gap R.413**, **Gap R.414**).

func popCLIFamily() []struct {
	name string
	src  string
	want string
} {
	return []struct {
		name string
		src  string
		want string
	}{
		{"the row's own program", "xs = [1, 2, \"a\"]\nxs.pop(0)\nprint(xs)\n", "[2, 'a']\n"},
		{"the shift read by position", "xs = [1, 2, \"a\"]\nxs.pop(0)\nprint(len(xs))\nprint(xs[0])\nprint(xs[1])\n", "2\n2\na\n"},
		{"a pop from the middle", "xs = [\"a\", \"b\", 1]\nxs.pop(1)\nprint(xs)\n", "['a', 1]\n"},
		{"a text's answer", "xs = [\"a\", 1]\nprint(xs.pop(0))\nprint(xs)\n", "a\n[1]\n"},
		{"a double's answer", "xs = [2.5, \"a\"]\nprint(xs.pop(0))\n", "2.5\n"},
		{"a verdict's answer", "xs = [True, \"a\"]\nprint(xs.pop(0))\n", "True\n"},
		{"the void's answer", "xs = [None, 1]\nprint(xs.pop(0))\n", "None\n"},
		{"a container's answer", "xs = [[1, \"a\"], 2]\nprint(xs.pop(0))\n", "[1, 'a']\n"},
		{"a dict's answer", "xs = [{\"a\": 1}, 2]\nprint(xs.pop(0))\n", "{'a': 1}\n"},
		{"every kind, in order", "xs = [1, \"a\", 2.5, True, None]\nprint(xs.pop())\nprint(xs)\n", "None\n[1, 'a', 2.5, True]\n"},
		{"drained from the front", "xs = [1, 2, \"a\"]\nprint(xs.pop(0))\nprint(xs.pop(0))\nprint(xs)\n", "1\n2\n['a']\n"},
		{"drained from the front in a loop", "xs = [1, \"a\", 2, \"b\"]\nwhile xs:\n    print(xs.pop(0))\n", "1\na\n2\nb\n"},
		{"the answer through str()", "xs = [1, \"a\"]\nprint(str(xs.pop(0)))\n", "1\n"},
		{"the answer beside a prefix", "xs = [\"a\", 1]\nprint(\"v:\", xs.pop(0))\n", "v: a\n"},
		{"the answer as a condition", "xs = [2.5, \"a\"]\nif xs.pop(0):\n    print(\"t\")\nelse:\n    print(\"f\")\nprint(xs)\n", "t\n['a']\n"},
		{"the answer bound to a name", "xs = [1, \"a\"]\nn = xs.pop(1)\nprint(n)\nprint(xs)\n", "a\n[1]\n"},
		{"the answer compared by content", "xs = [\"a\", 1]\nprint(xs.pop(0) == \"a\")\nprint(xs.pop(0) == 1)\n", "True\nTrue\n"},
		{"the answer as an argument", "xs = [\"a\", 1]\ndef f(x):\n    return x\nprint(f(xs.pop(0)))\n", "a\n"},
		{"the answer in a list literal", "xs = [1, \"a\"]\nys = [xs.pop(0), xs.pop(0)]\nprint(ys)\n", "[1, 'a']\n"},
		{"the answer in a tuple literal", "xs = [\"a\", 1]\nzs = (xs.pop(0), xs.pop(0))\nprint(zs[0])\nprint(zs[1])\n", "a\n1\n"},
		{"the answer in a set literal", "xs = [1, \"a\"]\nprint({xs.pop(0)})\n", "{1}\n"},
		{"the answer as a dict value", "xs = [1, \"a\"]\nprint({\"k\": xs.pop(0)})\n", "{'k': 1}\n"},
		{"the answer in a product", "xs = [2.5, \"a\"]\nprint(xs.pop(0) * 2)\n", "5.0\n"},
		{"the answer in a sum", "xs = [2.5, 1]\nprint(xs.pop(0) + xs.pop(0))\n", "3.5\n"},
		{"the answer ordered", "xs = [1, \"a\"]\nprint(xs.pop(0) > 1)\n", "False\n"},
		{"the default index", "xs = [1, 2, 3]\nprint(xs.pop())\nprint(xs)\n", "3\n[1, 2]\n"},
		{"a negative index", "xs = [1, 2, \"a\"]\nprint(xs.pop(-1))\nprint(xs)\n", "a\n[1, 2]\n"},
		{"a list of doubles", "xs = [1.5, 2.5]\nprint(xs.pop(0))\n", "1.5\n"},
		{"a list of ints keeps its own road", "xs = [7, 8, 9]\nprint(xs.pop(0) + 1)\nprint(xs)\n", "8\n[8, 9]\n"},
		{"a list of texts keeps its printer", "xs = [\"a\", \"b\"]\nprint(xs.pop(0))\nprint(xs)\n", "a\n['b']\n"},
		{"the same list under two names", "xs = [1, \"a\"]\nys = xs\nprint(ys.pop(0), xs)\n", "1 ['a']\n"},
		{"a pop inside a method", "class T:\n    def m(self):\n        xs = [1, \"a\"]\n        print(xs.pop(1))\nT().m()\n", "a\n"},
		{"the neighbour that appends", "xs = [1, 2, \"a\"]\nxs.append(3)\nprint(xs)\n", "[1, 2, 'a', 3]\n"},
	}
}

// TestThePoppedSlotFamilyAnswersLikeTheReference is the reference leg: every program above is asked of the
// pinned CPython INSIDE the case, so this file cannot certify a stale expectation, and the linked program then
// has to print the same bytes at exit 0 on every CLI engine.
func TestThePoppedSlotFamilyAnswersLikeTheReference(t *testing.T) {
	t.Parallel()
	for _, tc := range popCLIFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "pop_tag.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want != tc.want {
				t.Fatalf("the reference no longer prints %q for this program (prints %q) — this table's expectation went stale\n%s",
					tc.want, want, tc.src)
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code == 2 {
					t.Fatalf("%s rejected the compiler's own module (ADR 0166 reserves exit 2 for a compiler bug):\n%s\n%s",
						engine, stderr, tc.src)
				}
				if code != 0 {
					t.Fatalf("%s exited %d on a program the reference prints %q — the pop road refused it:\n%s",
						engine, code, want, stderr)
				}
				if stdout != want {
					t.Fatalf("%s printed %q, CPython printed %q\n%s", engine, stdout, want, tc.src)
				}
			}
		})
	}
}

var popErrLine = regexp.MustCompile(`(?m)^([A-Za-z]+Error): (.*)$`)

// cpythonTrap asks the reference a program it refuses, and answers the class and the sentence of the last
// traceback line — the two facts the compiled raise has to match.
func cpythonTrap(t *testing.T, path string) (class, message string, ok bool) {
	t.Helper()
	out, err := oracleCommand(t, path).CombinedOutput()
	if err == nil {
		return "", "", false
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if m := popErrLine.FindStringSubmatch(strings.TrimSpace(lines[i])); m != nil {
			return m[1], m[2], true
		}
	}
	return "", "", false
}

// popCLITraps are the bounds the reference raises over, through the linked binary. The two IndexError
// sentences are two different programs' errors, and the empty-list one is the sentence this backend had
// wrong; the TypeErrors are the sums whose pair the removal handed over.
func popCLITraps() []struct {
	name string
	src  string
} {
	return []struct {
		name string
		src  string
	}{
		{"the empty list", "xs = []\nprint(xs.pop())\n"},
		{"the empty list, indexed", "xs = []\nprint(xs.pop(0))\n"},
		{"past the end", "xs = [1]\nprint(xs.pop(5))\n"},
		{"a mixed list past the end", "xs = [1, \"a\", 2.5]\nprint(xs.pop(9))\n"},
		{"behind the front", "xs = [1, 2, \"a\"]\nprint(xs.pop(-4))\n"},
		{"a mixed list behind the front", "xs = [1, \"a\"]\nprint(xs.pop(-3))\n"},
		{"a text answer against a number", "xs = [\"a\", 1]\nprint(1 + xs.pop(0))\n"},
		{"two answers of different kinds", "xs = [1, \"a\"]\nprint(xs.pop(0) + xs.pop(0))\n"},
	}
}

// TestThePopTrapAtTheCLIIsTheReferenceTrap pins the raise family at the CLI: exit 3 (the program ran and
// trapped), never 1 (which would be the compiler refusing), never 2 (ADR 0166), and the reference's own class
// and sentence on stderr.
func TestThePopTrapAtTheCLIIsTheReferenceTrap(t *testing.T) {
	t.Parallel()
	for _, tc := range popCLITraps() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "pop_tag_trap.gy", tc.src)
			class, message, ok := cpythonTrap(t, path)
			if !ok {
				t.Skip("the reference answered this program — the row is stale, move it into popCLIFamily()")
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code == 2 {
					t.Fatalf("%s reached the assembler with a broken module (ADR 0166):\n%s\n%s", engine, stderr, tc.src)
				}
				if code == 1 {
					t.Fatalf("%s refused in words what the reference raises: exit 1 with %q\nreference: %s: %s\n%s",
						engine, strings.TrimSpace(stderr), class, message, tc.src)
				}
				if code != 3 {
					t.Fatalf("%s exited %d on a program the reference traps with %s: %s; stdout %q stderr %q",
						engine, code, class, message, stdout, stderr)
				}
				if !strings.Contains(stderr, class+": "+message) {
					t.Fatalf("%s raises %q, the reference raises %q\n%s", engine, strings.TrimSpace(stderr), class+": "+message, tc.src)
				}
			}
		})
	}
}

// TestThePopTrapIsCatchableAtTheCLI is the raise's other half at this altitude: the program's own handler
// catches it, so it is a raise and not a compiler error wearing a raise's clothes.
func TestThePopTrapIsCatchableAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"the empty list", "try:\n    xs = []\n    xs.pop()\nexcept IndexError as e:\n    print(\"caught\", e)\n", "caught pop from empty list\n"},
		{"past the end", "xs = [1, \"a\"]\nprint(1)\ntry:\n    xs.pop(7)\nexcept IndexError as e:\n    print(\"caught\", e)\nprint(xs)\n", "1\ncaught pop index out of range\n[1, 'a']\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "pop_tag_catch.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want != tc.want {
				t.Fatalf("the reference prints %q, this table pins %q\n%s", want, tc.want, tc.src)
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code != 0 {
					t.Fatalf("%s exited %d on a program whose own handler catches the raise:\n%s", engine, code, stderr)
				}
				if stdout != want {
					t.Fatalf("%s printed %q, CPython printed %q\n%s", engine, stdout, want, tc.src)
				}
			}
		})
	}
}

// popCLIOwed names the shapes this row leaves standing, with the sentence they carry today, the row that owns
// the fix, and the bytes the reference prints beside them. A row that stops being true FAILS, and the case is
// DELETED rather than edited.
func popCLIOwed() []struct {
	name, gap, src, mustMatch, want string
} {
	return []struct {
		name, gap, src, mustMatch, want string
	}{
		{
			"a tuple's pop", "L11.3 / ADR 0361",
			"ys = (1, 2, \"a\")\nprint(ys.pop(0))\n",
			"`pop()` would write into a tuple", "",
		},
		{
			"a dict's pop", "Gap R.174",
			"d = {\"a\": 1}\nprint(d.pop(\"a\"))\n",
			"pop() on \"d\", which the program bound to a container", "1\n",
		},
		{
			"a module-level container reached from a body", "Gap R.35",
			"xs = [1, \"a\"]\ndef g():\n    return xs.pop(1)\nprint(g())\n",
			"bound at module level", "a\n",
		},
		{
			"the container method over a name", "the container-method fold (insert/clear/copy)",
			"xs = [1, 2, \"a\"]\nxs.insert(0, \"z\")\nprint(xs)\n",
			"insert() on \"xs\", which the program bound to a container", "['z', 1, 2, 'a']\n",
		},
	}
}

// TestWhatThePopRowStillOwesAtTheCLI pins the refusals beside the reference's answer, on every engine, with
// exit 2 forbidden and the sentence required to name the row's subject.
func TestWhatThePopRowStillOwesAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range popCLIOwed() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "pop_tag_owed.gy", tc.src)
			if tc.want != "" {
				want, ok := cpythonOut(t, path)
				if !ok {
					t.Skip("no live python3 to ask")
				}
				if want != tc.want {
					t.Fatalf("the reference no longer prints %q (prints %q) — this row's premise went stale\n%s", tc.want, want, tc.src)
				}
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code == 0 && stdout == tc.want {
					t.Fatalf("%s closed %s: the program now prints the reference's bytes — delete this row and move it into the answered table\n%s",
						engine, tc.gap, tc.src)
				}
				if code == 2 {
					t.Fatalf("%s reached the assembler with a broken module (ADR 0166): %s\n%s", engine, stderr, tc.src)
				}
				if !strings.Contains(stderr, tc.mustMatch) {
					t.Fatalf("%s: %s's refusal reads %q, which does not carry %q\n%s", engine, tc.gap, stderr, tc.mustMatch, tc.src)
				}
			}
		})
	}
}

// TestThePopRefusalsAreMachineReadable is the agentic face: a program the pop road declines exits **1** with a
// `capability` member naming a code, its suggestion and its roadmap rows — never a bare sentence an agent has
// to pattern-match by hand.
func TestThePopRefusalsAreMachineReadable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, code string }{
		{"a tuple's pop", "ys = (1, 2, \"a\")\nprint(ys.pop(0))\n", "lower.unsupported.tuple-is-immutable"},
		{"a dict's pop", "d = {\"a\": 1}\nprint(d.pop(\"a\"))\n", "lower.unsupported.container-method-over-a-name"},
		{"the container method over a name", "xs = [1, 2, \"a\"]\nxs.insert(0, \"z\")\nprint(xs)\n", "lower.unsupported.container-method-over-a-name"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "pop_tag_json.gy", tc.src)
			out, code := cliRunCode(t, "--json", path)
			if code != 1 {
				t.Fatalf("a refused program exited %d, want 1 (ADR 0006): %s", code, out)
			}
			for _, frag := range []string{`"ok":false`, `"exit":1`, `"phase":"codegen"`, `"code":"` + tc.code + `"`, `"suggestion":`, `"rows":`} {
				if !strings.Contains(out, frag) {
					t.Fatalf("the JSON refusal carries no %s:\n%s", frag, out)
				}
			}
			if strings.Contains(out, "%!s(") || strings.Contains(out, "*lang.") {
				t.Fatalf("the JSON refusal prints the compiler's own types at the agent:\n%s", out)
			}
		})
	}
}

// TestWhatThePopRowAnswersWrongAtTheCLI is the reference leg of the honest half: the shapes this backend
// answers at the exit code of SUCCESS with the wrong bytes. Each row pins the reference's answer beside the
// compiled one and names the row that owns the fix; when the program starts printing the reference's bytes the
// row is DELETED, not edited.
func TestWhatThePopRowAnswersWrongAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, r := range []struct{ name, src, gap, python, compiled string }{
		{
			name:     "the popped slot compared with None",
			src:      "xs = [None, 1]\nprint(xs.pop(0) is None)\n",
			gap:      "Gap R.412",
			python:   "True\n",
			compiled: "False\n",
		},
		{
			name:     "a body whose answer is the popped slot",
			src:      "def m():\n    xs = [1, \"a\"]\n    return xs.pop(1)\n\nprint(m())\n",
			gap:      "Gap R.414",
			python:   "a\n",
			compiled: "0\n",
		},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "pop_tag_wrong.gy", r.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want != r.python {
				t.Fatalf("the reference no longer prints %q (prints %q) — this row's premise went stale\n%s", r.python, want, r.src)
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code == 2 {
					t.Fatalf("%s reached the assembler with a broken module (ADR 0166):\n%s\n%s", engine, stderr, r.src)
				}
				if stdout == want && code == 0 {
					t.Fatalf("%s closed %s: the program prints %q — delete this row and move the program into the answered table\n%s",
						engine, r.gap, want, r.src)
				}
				if stdout != r.compiled {
					t.Fatalf("%s: the wrong answer changed shape — prints %q, this row pins %q (the reference prints %q)\n%s",
						engine, stdout, r.compiled, want, r.src)
				}
			}
		})
	}
}

// TestThePoppedSlotFamilyIsOnTheRecord is ADR 0302's rule at this altitude: every program this row answers has
// an answer on record, written from the reference by tools/recmerge and cross-checked against the compiled
// leg — never written by this backend.
func TestThePoppedSlotFamilyIsOnTheRecord(t *testing.T) {
	t.Parallel()
	onRecord := 0
	for _, tc := range popCLIFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if !lang.HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge, never from this backend", tc.src)
			}
			onRecord++
		})
	}
	if onRecord == 0 {
		t.Fatal("not one popped-slot program is on the record: the record leg is not being walked")
	}
}

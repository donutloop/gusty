package integration

// rebind_kind_test.go — the CLI witness for a container variable's kind being decided by its LATEST
// binding (roadmap Gap Q.1, owner L11.1; ADR 0341, door `settleContainerKind` in `pkg/lang/rebind_kind.go`).
//
// The compiled leg alone decided a name's container kind in six roads, and the list-literal road cleared
// nothing. `d = {1: 10}` / `d = [3, 4]` therefore printed `{3: 4, 0: 0}` and `d[0]` printed `0`, both at
// the exit code that means the program ran: the dict's notebook outlived the dict.
//
// Three classes at the CLI, the same split ADR 0340's rows use:
//   - answers, asserted against the reference's own stdout and not merely against the previous build;
//   - traps, asserted at the reference's exit class (3) with the words the LAST binding earns, and
//     catchable by the program's own `except`;
//   - and the refusals this row does not own, asserted at exit 1 with exit 2 forbidden (ADR 0166).

import (
	"strings"
	"testing"
)

func TestAReboundContainerAtTheCLIMatchesCPython(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"a dict rebound to a list prints the list", "d = {1: 10}\nd = [3, 4]\nprint(d)\n", "[3, 4]\n"},
		{"its first slot", "d = {1: 10}\nd = [3, 4]\nprint(d[0])\n", "3\n"},
		{"its second slot", "d = {1: 10}\nd = [3, 4]\nprint(d[1])\n", "4\n"},
		{"its last slot from the end", "d = {1: 10}\nd = [3, 4]\nprint(d[-1])\n", "4\n"},
		{"its length", "d = {1: 10}\nd = [3, 4]\nprint(len(d))\n", "2\n"},
		{"membership in it", "d = {1: 10}\nd = [3, 4]\nprint(3 in d)\n", "True\n"},
		{"a loop over it", "d = {1: 10}\nd = [3, 4]\nfor v in d:\n    print(v)\n", "3\n4\n"},
		{"str of it", "d = {1: 10}\nd = [3, 4]\nprint(str(d))\n", "[3, 4]\n"},
		{"repr of it", "d = {1: 10}\nd = [3, 4]\nprint(repr(d))\n", "[3, 4]\n"},
		{"appending to it", "d = {1: 10}\nd = [3, 4]\nd.append(9)\nprint(d)\n", "[3, 4, 9]\n"},
		{"a copy taken from it", "d = {1: 10}\nd = [3, 4]\nprint(d[0:2])\n", "[3, 4]\n"},
		{"a walk of it by index", "d = {1: 10}\nd = [3, 1, 2]\ni = 0\nwhile i < len(d):\n    print(d[i])\n    i = i + 1\n", "3\n1\n2\n"},
		{"sorting it", "d = {1: 10}\nd = [3, 1, 2]\nd.sort()\nprint(d)\n", "[1, 2, 3]\n"},
		{"a mixed list after a dict", "d = {1: 10}\nd = [3, \"a\"]\nprint(d)\n", "[3, 'a']\n"},
		{"a slot of that mixed list", "d = {1: 10}\nd = [1, \"a\"]\nprint(d[1])\n", "a\n"},
		{"a text-keyed dict rebound to a list", "d = {\"a\": 1}\nd = [7, 8]\nprint(d[0])\n", "7\n"},
		{"a mixed dict rebound to a list", "d = {1: \"x\"}\nd = [2, 3]\nprint(d)\n", "[2, 3]\n"},
		{"a slot of the list that replaced a mixed dict", "d = {1: \"x\"}\nd = [2, 3]\nprint(d[0])\n", "2\n"},
		{"a comprehension over it", "d = {1: 10}\nd = [1, 2]\nprint([v * 2 for v in d])\n", "[2, 4]\n"},
		{"a sorted copy of it", "d = {1: 10}\nd = [3, 1, 2]\nprint(sorted(d))\n", "[1, 2, 3]\n"},
		{"it is a list to a call", "def first(xs):\n    return xs[0]\nd = {1: 10}\nd = [7, 8]\nprint(first(d))\n", "7\n"},
		{"a set rebound to a list", "s = {1, 2}\ns = [5, 6]\nprint(s)\n", "[5, 6]\n"},
		{"a slot of that list", "s = {1, 2}\ns = [5, 6]\nprint(s[0])\n", "5\n"},
		{"appending to that list", "s = {1, 2}\ns = [5, 6]\ns.append(7)\nprint(s)\n", "[5, 6, 7]\n"},
		{"a mixed set rebound to a list", "s = {1, \"a\"}\ns = [2, 3]\nprint(s)\n", "[2, 3]\n"},
		{"a list rebound to a dict", "xs = [3, 4]\nxs = {1: 10}\nprint(xs)\n", "{1: 10}\n"},
		{"an entry of that dict", "xs = [3, 4]\nxs = {1: 10}\nprint(xs[1])\n", "10\n"},
		{"the length of that dict", "xs = [3, 4]\nxs = {1: 10}\nprint(len(xs))\n", "1\n"},
		{"a loop over that dict's keys", "xs = [3, 4]\nxs = {1: 10}\nfor k in xs:\n    print(k)\n", "1\n"},
		{"a list rebound to a set", "xs = [3, 4]\nxs = {7}\nprint(xs)\n", "{7}\n"},
		{"membership in that set", "xs = [3, 4]\nxs = {7}\nprint(7 in xs)\n", "True\n"},
		{"a dict rebound to a set", "d = {1: 10}\nd = {3, 4}\nprint(d)\n", "{3, 4}\n"},
		{"membership in the set that replaced a dict", "d = {1: 10}\nd = {3, 4}\nprint(3 in d)\n", "True\n"},
		{"the length of the set that replaced a dict", "d = {1: 10}\nd = {3, 4}\nprint(len(d))\n", "2\n"},
		{"three bindings, the last one answers", "x = [1, 2]\nx = {1: 2}\nx = [7, 8]\nprint(x[0])\n", "7\n"},
		{"three bindings printed", "x = [1, 2]\nx = {1: 2}\nx = [7, 8]\nprint(x)\n", "[7, 8]\n"},
		{"three bindings, a set last", "x = [1, 2]\nx = {1: 2}\nx = {5, 6}\nprint(5 in x)\n", "True\n"},
		{"the detour through a number", "x = {1: 2}\nx = 5\nx = [7, 8]\nprint(x[0])\n", "7\n"},
		{"an alias of a name that is never read again", "a = {1: 2}\nb = a\nb = [7, 8]\nprint(b[0])\n", "7\n"},
		{"the same decision inside a function", "def f():\n    d = {1: 10}\n    d = [3, 4]\n    print(d[0])\n    print(d)\n\nf()\n", "3\n[3, 4]\n"},
		{"a text list rebound to a number list", "xs = [\"a\"]\nxs = [1, 2]\nprint(xs)\n", "[1, 2]\n"},
		{"a number list rebound to a text list", "xs = [1, 2]\nxs = [\"a\"]\nprint(xs)\n", "['a']\n"},
		{"appending after a same-kind rebinding", "xs = [\"a\"]\nxs = [1, 2]\nxs.append(3)\nprint(xs)\n", "[1, 2, 3]\n"},
		{"a text-keyed dict rebound to a number-keyed one", "d = {\"a\": 1}\nd = {2: 3}\nprint(d)\n", "{2: 3}\n"},
		{"a text set rebound to a number set", "s = {\"a\"}\ns = {1}\nprint(s)\n", "{1}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "rebind_kind.gy", tc.src)
			if py, ok := cpythonOut(t, path); ok && py != tc.want {
				t.Fatalf("the expectation is not CPython's: got %q want %q", py, tc.want)
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("%s rejected the compiler's own module (ADR 0166 / exit-code contract):\n%s", engine, cliRun(t, engine, path))
				}
				if code != 0 {
					t.Fatalf("%s exited %d: %s", engine, code, cliRun(t, engine, path))
				}
				if out != tc.want {
					t.Fatalf("%s printed %q, want CPython's %q", engine, out, tc.want)
				}
			}
		})
	}
}

func TestAReboundContainerRaisesTheWordsTheLastBindingEarns(t *testing.T) {
	for _, tc := range []struct {
		name, src, msg string
	}{
		{
			"a position past the list that replaced a dict",
			"d = {1: 10}\nd = [3, 4]\nprint(d[9])\n",
			"IndexError: list index out of range",
		},
		{
			"a text key into the list that replaced a dict",
			"d = {1: 10}\nd = [3, 4]\nprint(d[\"a\"])\n",
			"TypeError: list indices must be integers or slices, not str",
		},
		{
			"a position past the list that replaced a set",
			"s = {1, 2}\ns = [5]\nprint(s[3])\n",
			"IndexError: list index out of range",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "rebind_trap.gy", tc.src)
			py, pyCode := oracleTrap(t, path)
			if pyCode == 0 || !strings.Contains(py, tc.msg) {
				t.Fatalf("the oracle does not raise what the table claims: exit %d, %q", pyCode, py)
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 0 {
					t.Errorf("%s exited 0 on a program the oracle dies on: stdout=%q", engine, out)
				}
				if code == 2 {
					t.Errorf("%s exited 2 (compiler bug, ADR 0166) on a program the oracle dies on:\n%s", engine, out)
				}
				combined := cliRun(t, engine, path)
				if !strings.Contains(combined, "Traceback (most recent call last):") || !strings.Contains(combined, tc.msg) {
					t.Errorf("%s did not raise %q:\n%s", engine, tc.msg, combined)
				}
			}
		})
	}
}

func TestAReboundContainersTrapsStayCatchable(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"the bounds trap is caught",
			"d = {1: 10}\nd = [3, 4]\ntry:\n    print(d[9])\nexcept IndexError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"the wrong-key trap is caught",
			"d = {1: 10}\nd = [3, 4]\ntry:\n    print(d[\"a\"])\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"after the arm the name is still the list",
			"d = {1: 10}\nd = [3, 4]\ntry:\n    print(d[9])\nexcept IndexError:\n    print(d[0])\n",
			"3\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "rebind_catch.gy", tc.src)
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code != 0 || out != tc.want {
					t.Fatalf("%s printed %q exit %d, want %q (%s)", engine, out, code, tc.want, cliRun(t, engine, path))
				}
			}
		})
	}
}

// TestTheAliasedContainerIsADebtRowNotAnAnswer pins the divergence the rebinding family walks into that
// this row does not own: `b = a` copies the handle, so the two names hold ONE object, and the rebinding
// `b = [7, 8]` frees the object `a` still names. The reference prints "7\n7\n2\n" at exit 0; both the
// commit before ADR 0341 and this one die with `KeyError: 1` at exit 3 (roadmap Gap R.257). It is pinned
// as a debt, with the reference's answer beside it, because a wrong answer at exit 0 and a death at exit 3
// are both worse than the refusal this row is careful about — and because the fix belongs to the handle
// copy, not to the kind claim ADR 0341 settled.
func TestTheAliasedContainerIsADebtRowNotAnAnswer(t *testing.T) {
	const src = "y = {1: 2}\ny = 5\ny = [7, 8]\nprint(y[0])\na = {1: 2}\nb = a\nb = [7, 8]\nprint(b[0])\nprint(a[1])\n"
	path := writeSrc(t, t.TempDir(), "rebind_alias_debt.gy", src)
	if py, ok := cpythonOut(t, path); ok && py != "7\n7\n2\n" {
		t.Fatalf("the reference's answer this row writes into Gap R.257 changed: %q", py)
	}
	for _, engine := range cliEngines {
		combined := cliRun(t, engine, path)
		if !strings.Contains(combined, "KeyError: 1") {
			t.Errorf("%s no longer dies the way Gap R.257 records (%s); if the alias now answers, promote the program to the answered table, the record and the conformance corpus", engine, combined)
		}
	}
}

// TestWhatAReboundContainerStillOwesAtTheCLI pins the two refusals the family still meets. Neither is the
// kind claim's defect — both reproduce on a program with a single binding — so they are pinned as refusals
// with the reference's answer written into the roadmap row that owns them (Gap R.107, Gap R.256), and
// never as answers this row promised.
func TestWhatAReboundContainerStillOwesAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src, phrase string }{
		{
			"max over a container variable (Gap R.107)",
			"d = {1: 10}\nd = [3, 1, 2]\nprint(max(d))\n",
			"max requires an inline list/set/dict literal",
		},
		{
			"a set asked for a position (Gap R.256)",
			"d = {1: 10}\nd = {3, 4}\nprint(d[0])\n",
			"index of a non-literal variable",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "rebind_owed.gy", tc.src)
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 0 {
					t.Fatalf("%s answered %q at exit 0; this shape is owed by the roadmap row the name cites", engine, out)
				}
				if code == 2 {
					t.Fatalf("%s exited 2 on a refusal (ADR 0166): %s", engine, out)
				}
				combined := cliRun(t, engine, path)
				if !strings.Contains(combined, tc.phrase) {
					t.Fatalf("%s refused without naming %q:\n%s", engine, tc.phrase, combined)
				}
			}
		})
	}
}

// TestASingleBindingContainerIsNotMovedByTheDoor is the negative control: the programs that always answered
// must not have moved, including the slot read ADR 0241 licenses from a literal the name was bound to once.
func TestASingleBindingContainerIsNotMovedByTheDoor(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"a literal list's slot", "xs = [1, 2]\nprint(xs[0])\n", "1\n"},
		{"a literal list printed", "xs = [1, 2]\nprint(xs)\n", "[1, 2]\n"},
		{"a literal dict's entry", "d = {1: 10}\nprint(d[1])\n", "10\n"},
		{"a literal set's membership", "s = {1, 2}\nprint(1 in s)\n", "True\n"},
		{"a mixed list printed", "xs = [1, \"a\", None]\nprint(xs)\n", "[1, 'a', None]\n"},
		{"a nested literal read", "xs = [[7, 8]]\nprint(xs[0][1])\n", "8\n"},
		{"a list grown by append", "xs = []\nxs.append(3)\nxs.append(4)\nprint(xs)\n", "[3, 4]\n"},
		{"a dict filled by assignment", "d = {}\nd[\"a\"] = 1\nprint(d)\n", "{'a': 1}\n"},
		{"a loop over a literal list", "for v in [1, 2, 3]:\n    print(v)\n", "1\n2\n3\n"},
		{"a sorted literal", "print(sorted([3, 1, 2]))\n", "[1, 2, 3]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "rebind_single.gy", tc.src)
			if py, ok := cpythonOut(t, path); ok && py != tc.want {
				t.Fatalf("the expectation is not CPython's: got %q want %q", py, tc.want)
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code != 0 || out != tc.want {
					t.Fatalf("%s printed %q exit %d, want CPython's %q (%s)", engine, out, code, tc.want, cliRun(t, engine, path))
				}
			}
		})
	}
}

// integration/numeric_slot_door_scope_test.go — the reference leg of the number door's scope (roadmap
// Gap R.191, the slot-read road of L11.1; ADR 0322).
//
// The record leg in pkg/lang pins what this compiler answers; this file asks CPython, which is the only
// witness that says whether opening the door was right. The defect was a refusal — a program the reference
// prints, answered at exit 1 by a sentence blaming a container the program never mutated — so the rows
// below are the ones that must now PRINT, and the ones beside them are the rows that must keep refusing:
// the door exists because `xs[0] + "a"` is a joined text in the reference and an unbuildable value here
// (Gap R.82), and a door that opened too wide would raise where the reference returns a value.
//
// Exit 2 is the contract's compiler-bug code (ADR 0166) and appears in none of these runs.

package integration

import (
	"strings"
	"testing"
)

// textElsewhere is the line that used to close the number door for the whole module. Nothing in the
// programs below reads `w`, and the arithmetic is over another container's slots.
const textElsewhere = "w = []\nw.append(\"a text\")\n"

// TestCLIAgentATextInOneContainerLeavesAnotherNumberDoorOpen is the answering family against CPython: the
// same programs print, on the compiled leg, what the reference prints.
func TestCLIAgentATextInOneContainerLeavesAnotherNumberDoorOpen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, tc := range []struct{ name, src string }{
		{"the row's own shape", textElsewhere + "nested = []\nnested.append([7, 8])\nd = nested[0][0] * 2\nprint([d])\n"},
		{"the slot, printed by its own line", textElsewhere + "nested = []\nnested.append([7, 8])\nprint(nested[0][0] * 2)\n"},
		{"the slot added to", textElsewhere + "nested = []\nnested.append([7, 8])\nprint(nested[0][0] + 1)\n"},
		{"the other slot subtracted from", textElsewhere + "nested = []\nnested.append([7, 8])\nprint(nested[0][1] - 3)\n"},
		{"the slot negated", textElsewhere + "nested = []\nnested.append([7, 8])\nprint(-nested[0][0])\n"},
		{"a float slot keeps its float", textElsewhere + "xs = []\nxs.append([7.5, 8])\nprint(xs[0][0] * 2)\n"},
		{"a dict value written by assignment", textElsewhere + "d = {}\nd[\"k\"] = 40\nprint(d[\"k\"] + 2)\n"},
		{"two slots out of two built rows", textElsewhere + "rows = []\nrows.append([1, 2])\nrows.append([3, 4])\nprint(rows[0][0] + rows[1][1])\n"},
		{"three levels down", textElsewhere + "outer = []\nouter.append([[7, 8]])\nprint(outer[0][0][0] + 1)\n"},
		{"two text containers beside one numeric one", "w = []\nw.append(\"a text\")\nxs = []\nxs.append([7, 8])\nys = []\nys.append(\"more\")\nprint(xs[0][0] + 1)\n"},
		{"the text container is a dict", textElsewhere + "notes = {}\nnotes[\"title\"] = \"a text\"\nxs = []\nxs.append([6])\nprint(xs[0][0] * 7)\n"},
		{"the answer bound, printed, and asked again", textElsewhere + "xs = []\nxs.append([7, 8])\nn = xs[0][0] * 2\nprint(n)\nprint(n + 1)\nprint(f\"{n}\")\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, dir, "door_scope.gy", tc.src)
			ref, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Skipf("the reference could not answer this program: %s", tc.src)
			}
			got, code := cliRunMerged(t, "--file", path)
			if code == 2 {
				t.Fatalf("exit 2 — the module the door was emitted into was rejected (ADR 0166):\n%s", got)
			}
			if code != 0 {
				t.Fatalf("the compiled leg refused a program the reference prints (exit %d):\n reference: %q\n compiled: %q", code, ref, got)
			}
			requireReferenceAgreement(t, tc.src, ReferenceAgreement{
				Python: ref, Compiled: got, Code: code,
			}, "roadmap Gap R.191 (a container that holds a text closed another container's number door)",
				"a text stored in one container, arithmetic over another container's slots")
		})
	}
}

// TestCLIAgentTheDoorScopeRowsThatBelongToAnotherRowAreFiledNotFixed keeps the two refusals this cycle
// measured beside its own, so nobody re-derives them and nobody mistakes them for this row's: both refuse
// on the commit before the block was re-keyed exactly as they refuse after it. The first is Gap R.101 — `/`
// with a slot on each side, the one operator whose door admits only one operand read from the object. The
// second is the loop-variable family (Gaps R.91, R.95): `v` is bound per iteration, and the arithmetic
// asked of `v[0]` needs the tag the loop variable does not carry. Both are refusals in words at exit 1, and
// the reference's answer is written into the row so a future cycle that lands the real fix breaks this
// table rather than quietly passing.
func TestCLIAgentTheDoorScopeRowsThatBelongToAnotherRowAreFiledNotFixed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, tc := range []struct {
		name, src, owner, wantReference, wantCompiled string
	}{
		{
			name:          "true division with a slot on each side",
			src:           "tags = set()\ntags.add(\"x\")\nxs = []\nxs.append([6, 2])\nprint(xs[0][0] / xs[0][1])\n",
			owner:         "roadmap Gap R.101 (ADR 0253's one-side gate)",
			wantReference: "3.0\n",
			wantCompiled:  "no number the compiled backend can lift",
		},
		{
			name:          "a slot of a slot read through a loop variable",
			src:           textElsewhere + "xs = []\nxs.append([7, 8])\nfor v in xs:\n    print(v[0] + 1)\n",
			owner:         "roadmap Gaps R.91 and R.95 (the loop variable carries no pair into the read)",
			wantReference: "8\n",
			wantCompiled:  "index of a non-literal variable",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, dir, "door_scope_filed.gy", tc.src)
			ref, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok || ref != tc.wantReference {
				t.Fatalf("the reference's answer moved (want %q, got %q, ok %v) — the row this table files is no longer the one it filed", tc.wantReference, ref, ok)
			}
			out, code := cliReport(t, "--aot", "--file", path)
			if code == 2 {
				t.Fatalf("exit 2 on a program the reference answers — %s: %s", tc.owner, out)
			}
			if code == 0 {
				t.Fatalf("this shape now compiles and runs — %s is paid, and this table has to go:\n%s", tc.owner, out)
			}
			if !strings.Contains(out, tc.wantCompiled) {
				t.Errorf("the refusal moved away from %q (owed by %s):\n%s", tc.wantCompiled, tc.owner, out)
			}
		})
	}
}

// TestCLIAgentTheNumberDoorStaysShutWhereAnAliasCanReachTheText is the half that must not open: the block
// travels with the object, so a text that can reach the container the read walks keeps the program on the
// ordinary road — which refuses it in words, at exit 1, rather than raising a TypeError the reference never
// raises. Each row is a program the reference answers with a TEXT (a joined string, a repeated container),
// which is exactly what this backend cannot build from a slot (Gap R.82).
func TestCLIAgentTheNumberDoorStaysShutWhereAnAliasCanReachTheText(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, tc := range []struct{ name, src, want string }{
		{
			"a text stored in the container the read walks",
			"xs = []\nxs.append([7, 8])\nxs.append(\"word\")\nprint(xs[0][0] + 1)\n",
			"cannot reach into",
		},
		{
			"a text stored through an alias of it",
			"xs = []\nxs.append([7, 8])\nys = xs\nys.append(\"word\")\nprint(xs[1] * 2)\n",
			"single static kind",
		},
		{
			"a value the compiler cannot see through",
			"def f():\n    return [7, 8]\n\nxs = []\nxs.append(f())\nprint(xs[0][0] + 1)\n",
			"cannot reach into",
		},
		{
			"a text stored where the read goes, one level up",
			"xs = []\nxs.append(\"hi\")\nprint(xs[0] + 1)\n",
			"concatenating a string with a value that is not a string",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, dir, "door_scope_shut.gy", tc.src)
			out, code := cliReport(t, "--aot", "--file", path)
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

// TestCLIAgentTheRegisteredDoorScopeProbePrintsWhatTheReferencePrints runs the corpus file the conformance
// matrix registers. `oracle: match` for that file is a claim about the compiled leg and the reference, and
// this is where it is checked rather than asserted.
func TestCLIAgentTheRegisteredDoorScopeProbePrintsWhatTheReferencePrints(t *testing.T) {
	t.Parallel()
	src := readProgram(t, "probe_a_text_in_one_container_leaves_another_number_door_open.gy")
	want := "[14]\n8\n5\n15.0\n-7\n42\n5\n2\n"
	dir := t.TempDir()
	gy := writeSrc(t, dir, "probe_a_text_in_one_container_leaves_another_number_door_open.gy", src)
	if py, ok := cpythonPlainOut(t, dir, src); !ok || py != want {
		t.Fatalf("the expectation this file carries is not the reference's: %q (ok %v)", py, ok)
	}
	for _, engine := range cliEngines {
		out, code := cliRunCode(t, engine, "--file", gy)
		if code == 2 {
			t.Fatalf("%s: exit 2 (ADR 0166):\n%s", engine, out)
		}
		if code != 0 || out != want {
			t.Errorf("%s: exit %d, stdout %q, want %q", engine, code, out, want)
		}
	}
}

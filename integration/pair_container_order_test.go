// integration/pair_container_order_test.go — the CLI half of the fold that orders containers (roadmap
// Gap R.197, filed measuring ADR 0316 and paid by ADR 0318).
//
// The record leg (`pkg/lang/pair_container_order_test.go`) holds these programs against the answer on
// record; this file asks CPython, which is the only witness that says what a fold over a container IS. The
// defect Gap R.197 filed was not a crash: two different containers in one `min` raised the reference's own
// `TypeError: '<' not supported between instances of 'list' and 'list'` at exit 3 where the reference
// answers `[1, 2]`, and a container folded against a number answered the container's ELEMENT COUNT at exit 0
// where the reference raises. Both are wrong answers, and only the reference can tell you which row is which.
//
// What the door still declines stays a refusal in words at exit 1 and never the contract's exit 2 (ADR 0166),
// and what the door answers by deciding something the program never wrote is filed, not asserted — the
// relational operators over two built containers are Gap R.97's row, measured here as Gap R.201.

package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// The containers a fold is asked to order, spelled the way the door meets them: built by the program and
// read back through a name, so the fold's own line does not say which kind arrives.
const (
	foldPairCLILists = "xs = []\nxs.append([1, 2])\nxs.append([3])\na = xs[0]\nb = xs[1]\n"
	foldPairCLISets  = "xs = []\nxs.append({1, 2})\nxs.append({1, 2, 3})\na = xs[0]\nb = xs[1]\n"
	foldPairCLIEmpty = "xs = []\nxs.append([])\nxs.append([1, 2])\na = xs[0]\nb = xs[1]\n"
	foldPairCLIMix   = "xs = []\nxs.append([1])\nxs.append({1})\na = xs[0]\nb = xs[1]\n"
	foldPairCLIClash = "xs = []\nxs.append([\"a\"])\nxs.append([1])\na = xs[0]\nb = xs[1]\n"
)

// TestCLIAgentAFoldOfContainersAgreesWithCPython is the reference leg of the answer table: a LIST orders
// lexicographically, element by element, the shorter list the lesser when every shared element is equal; a
// SET orders by the subset operator, and the pair that is neither one's subset orders NEITHER way without
// raising, so the incumbent survives exactly as the reference's `min` keeps it.
func TestCLIAgentAFoldOfContainersAgreesWithCPython(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src string }{
		{"two built lists, min", foldPairCLILists + "print(min(a, b))\n"},
		{"two built lists, max", foldPairCLILists + "print(max(a, b))\n"},
		{"one list asked against itself", foldPairCLILists + "print(min(a, a))\n"},
		{"two built sets, min", foldPairCLISets + "print(min(a, b))\n"},
		{"two built sets, max", foldPairCLISets + "print(max(a, b))\n"},
		{"the empty list is the lesser list", foldPairCLIEmpty + "print(min(a, b))\n"},
		{"the empty list read by max", foldPairCLIEmpty + "print(max(a, b))\n"},
		{"two empty lists spelled as calls", "print(min(list(), list()))\n"},
		{"a list asked whether it is the other list", foldPairCLILists + "print(a == b)\n"},
		{"the winner bound, printed, and asked back", foldPairCLILists + "m = min(a, b)\nprint(m)\nprint(m == [1, 2])\n"},
		{"the winner fills an f-string field", foldPairCLILists + "m = min(a, b)\nprint(f\"{m}\")\n"},
		{"the winner folded again by a second fold", foldPairCLILists + "m = min(a, b)\nn = max(m, b)\nprint(n)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "foldpair.gy", tc.src)
			ref, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Skipf("the reference could not answer this program: %s", tc.src)
			}
			got, code := cliRunMerged(t, "--file", path)
			if code == 2 {
				t.Fatalf("exit 2 — LLVM rejected the module the ordering was emitted into (ADR 0166):\n%s", got)
			}
			if code != 0 {
				t.Fatalf("the compiled run exited %d on a program the reference prints:\n reference: %q\n compiled: %q", code, ref, got)
			}
			requireReferenceAgreement(t, tc.src, ReferenceAgreement{
				Python: ref, Compiled: got, Code: code,
			}, "roadmap Gap R.197 (the fold asked to order a container the program built)",
				"a min/max over containers the program built, ordered element by element")
		})
	}
}

// TestCLIAgentAFoldOfContainersRaisesWhatCPythonRaises is the reference leg of the trap table, and the half
// that pays the row's second arm: each of these used to ANSWER at the exit code of success — the container's
// element count, standing in for its contents — and the reference raises for all of them. The sentence names
// the two kinds in the order the failing comparison had them, which inside two lists means the ELEMENTS'
// kinds: `min(["a"], [1])` names 'int' and 'str' because the reference asks the later argument against the
// earlier one.
func TestCLIAgentAFoldOfContainersRaisesWhatCPythonRaises(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src, sentence string }{
		{"a list folded against a number (min)", foldPairCLILists + "print(min(a, 3))\n", "TypeError: '<' not supported between instances of 'int' and 'list'"},
		{"a list folded against a number (max)", foldPairCLILists + "print(max(a, 3))\n", "TypeError: '>' not supported between instances of 'int' and 'list'"},
		{"a set folded against a number", foldPairCLISets + "print(min(a, 3))\n", "TypeError: '<' not supported between instances of 'int' and 'set'"},
		{"a list folded against a text", foldPairCLILists + "print(min(a, \"a\"))\n", "TypeError: '<' not supported between instances of 'str' and 'list'"},
		{"a list folded against a set names both containers", foldPairCLIMix + "print(min(a, b))\n", "TypeError: '<' not supported between instances of 'set' and 'list'"},
		{"two dicts raise", "d = {}\ne = {}\nprint(min(d, e))\n", "TypeError: '<' not supported between instances of 'dict' and 'dict'"},
		{"a dict against itself has no ordering either", "d = {}\nprint(min(d, d))\n", "TypeError: '<' not supported between instances of 'dict' and 'dict'"},
		{"the failure inside two lists names the elements", foldPairCLIClash + "print(min(a, b))\n", "TypeError: '<' not supported between instances of 'int' and 'str'"},
		{"the same failure seen by max names '>'", foldPairCLIClash + "print(max(a, b))\n", "TypeError: '>' not supported between instances of 'int' and 'str'"},
		{"an empty set spelled as a call", "print(min(set(), 3))\n", "TypeError: '<' not supported between instances of 'int' and 'set'"},
		{"an empty list spelled as a call", "print(min(list(), 3))\n", "TypeError: '<' not supported between instances of 'int' and 'list'"},
		{"a number against a set spelled as a call", "print(min(3, {4}))\n", "TypeError: '<' not supported between instances of 'set' and 'int'"},
		{"an empty list against a text", "print(min(list(), \"a\"))\n", "TypeError: '<' not supported between instances of 'str' and 'list'"},
		{"an empty set against an empty list", "print(min(set(), list()))\n", "TypeError: '<' not supported between instances of 'list' and 'set'"},
		{"a number among the lists a min folds", "print(min(3, [1], [2]))\n", "TypeError: '<' not supported between instances of 'list' and 'int'"},
		{"the lists a min folds, with a number last", "print(min([1], [2], 3))\n", "TypeError: '<' not supported between instances of 'int' and 'list'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "foldpair_trap.gy", tc.src)
			if py, ok := cpythonPlainOut(t, dir, tc.src); ok {
				t.Fatalf("the reference answered %q, expected the trap\nsrc: %s", py, tc.src)
			}
			out, code := cliRunMerged(t, "--file", path)
			if code == 2 {
				t.Fatalf("exit 2 — LLVM rejected the module this raise was emitted into (ADR 0166):\n%s", out)
			}
			if code != 3 {
				t.Fatalf("exit %d, want the trap exit 3 — the answer Gap R.197 measured was an answer at exit 0, "+
					"the container's element count where the reference raises:\n%s", code, out)
			}
			if !strings.Contains(out, tc.sentence) {
				t.Errorf("the compiled raise did not say %q:\n%s", tc.sentence, out)
			}
		})
	}
}

// TestCLIAgentAFoldOfContainersTrapReachesTheProgramsOwnArm is the half that makes a compiled raise a raise:
// in a linked binary, the handler runs, the program continues, and the exit code is success.
func TestCLIAgentAFoldOfContainersTrapReachesTheProgramsOwnArm(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src, want string }{
		{"the container-against-a-number raise caught",
			foldPairCLILists + "try:\n    print(min(a, 3))\nexcept TypeError:\n    print(\"caught\")\nprint(\"carries-on\")\n", "caught\ncarries-on\n"},
		{"the fold still answers after a caught raise",
			foldPairCLILists + "try:\n    print(min(a, 3))\nexcept TypeError:\n    print(\"caught\")\nprint(min(a, b))\n", "caught\n[1, 2]\n"},
		{"the element-that-does-not-order raise caught",
			foldPairCLIClash + "try:\n    print(min(a, b))\nexcept TypeError:\n    print(\"caught\")\nprint(a)\n", "caught\n['a']\n"},
		{"the dict raise caught", "d = {}\ntry:\n    print(min(d, d))\nexcept TypeError:\n    print(\"caught\")\n", "caught\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "foldpair_catch.gy", tc.src)
			ref, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Fatalf("the reference could not answer a program whose raise must be catchable: %s", tc.src)
			}
			if strings.TrimRight(ref, "\n") != strings.TrimRight(tc.want, "\n") {
				t.Fatalf("the case's expectation drifted from the reference: it prints %q, the case says %q", ref, tc.want)
			}
			out, code := cliRunMerged(t, "--file", path)
			if code != 0 {
				t.Fatalf("a caught raise left the linked binary at exit %d:\n%s", code, out)
			}
			if strings.TrimRight(out, "\n") != strings.TrimRight(tc.want, "\n") {
				t.Fatalf("compiled printed %q, want %q", out, tc.want)
			}
		})
	}
}

// TestCLIAgentWhatTheFoldOfContainersStillRefusesIsStillRefusedInWords keeps the honest half at the CLI. The
// first four rows are Gap R.198's untaggable shapes — a container WRITTEN among the folded values, which the
// door will not label — and the last four are the fold's answer used where ONE word is kept (Gap R.146) or
// read by a builtin that asks the compiler to see inside the value. None of them is an answer, and none of
// them is allowed to spend the exit-code contract's "the compiler is broken" code (ADR 0166).
func TestCLIAgentWhatTheFoldOfContainersStillRefusesIsStillRefusedInWords(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src, want string }{
		{"two literal containers", "print(min([1], [2]))\n", "container among values"},
		{"an answer folded against a literal container", foldPairCLILists + "print(min([min(a, b)], b))\n", "container among values"},
		{"an empty list spelled as a call beside a literal one", "print(min(list(), [1]))\n", "container among values"},
		{"an empty set spelled as a call beside a literal one", "print(min(set(), {1}))\n", "container among values"},
		{"the winner as a list element", foldPairCLILists + "print([min(a, b)])\n", "one word"},
		{"the winner asked against a literal list", foldPairCLILists + "print(min(a, b) == [1, 2])\n", "one word"},
		{"the winner measured by len", foldPairCLILists + "print(len(min(a, b)))\n", "len requires an inline list/dict/set literal"},
		{"the winner subscripted", foldPairCLILists + "m = min(a, b)\nprint(m[0])\n", "index of a non-literal variable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "foldpair_refuse.gy", tc.src)
			out, code := cliRunMerged(t, "--file", path)
			if code == 2 {
				t.Fatalf("exit 2 — LLVM rejected the module gusty emitted (ADR 0166):\n%s", out)
			}
			if code != 1 {
				t.Fatalf("exit %d, want 1 (a program this backend declines to build):\n%s", code, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("the refusal does not name the missing half (%q):\n%s", tc.want, out)
			}
			noteCompiledGap(t, tc.src, out)
		})
	}
}

// TestTheContainerOrderProbeIsOnRecord is the loud version of ADR 0302's missing-record rule for the program
// the conformance matrix promotes with this row: a deleted record fails a test instead of quietly skipping
// the case, and the compiled leg must print the reference's BYTES — the answer Gap R.197's debt row recorded
// beside the raise is now the answer both legs print (roadmap Gap R.197, ADR 0318).
func TestTheContainerOrderProbeIsOnRecord(t *testing.T) {
	src := readProgram(t, "probe_a_fold_orders_two_built_containers.gy")
	if !lang.HasGoldenAnswer(src) {
		t.Fatal("the probe this cycle promotes out of the debt list has no record — every corpus case that reads " +
			"expectations from the record fails on it, starting with TestGCCorpusCollectsAndAgrees")
	}
	res, err := lang.JIT(src, 0)
	if err != nil {
		t.Fatalf("the compiled backend refused a program the reference prints: %v", err)
	}
	if res.Code != 0 {
		t.Fatalf("the probe ended at exit %d: %s", res.Code, res.Stderr)
	}
	ref, ok := cpythonPlainOut(t, t.TempDir(), src)
	if !ok {
		t.Fatalf("the reference could not answer the probe")
	}
	if res.Output != ref {
		t.Fatalf("the compiled leg prints other bytes than the reference's:\n got %q\nwant %q", res.Output, ref)
	}
}

// TestCLIAgentTheRelationalOrderingOfContainersIsFiledNotFixed is the row this cycle measured and did NOT
// fix, written down rather than quietly left out. The fold got its element-wise ordering; the RELATIONAL
// OPERATORS did not, and they are a different road with its own owner:
//
//   - `a < b` over two containers the program BUILT answers True in the reference and raises the reference's
//     own sentence compiled — and the sentence names kinds neither operand is (`'list' and 'str'` for two
//     lists). A raise where the reference answered a value, in a diagnostic that misdescribes the program:
//     roadmap Gap R.201, the half Gap R.97 owns for `<`.
//   - `True < [1]` and `None < [1]` name 'int' for an operand that is a verdict and for one that is None, and
//     `"a" < [1]` names 'int' for a text — the cross-kind sentence is built from a table that reads a
//     container's operand as a number. A wrong sentence is a wrong answer (ADR 0271): roadmap Gap R.202.
//
// Both classes are asserted as the reference answers them, so the row cannot drift, and as the compiled leg
// answers them today, so the defect cannot silently "pass" by moving somewhere else.
func TestCLIAgentTheRelationalOrderingOfContainersIsFiledNotFixed(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src, reference, compiled string }{
		{
			"two built lists ordered by <",
			foldPairCLILists + "print(a < b)\n", "True\n",
			"TypeError: '<' not supported between instances of 'list' and 'str'",
		},
		{
			"two built lists ordered by >=",
			foldPairCLILists + "print(a >= b)\n", "False\n",
			"TypeError: '>=' not supported between instances of 'list' and 'str'",
		},
		{
			"two built sets ordered by <",
			"xs = []\nxs.append({1})\nxs.append({1, 2})\na = xs[0]\nb = xs[1]\nprint(a < b)\n", "True\n",
			"TypeError: '<' not supported between instances of 'set' and 'str'",
		},
		{
			"a verdict against a list names 'bool'",
			"print(True < [1])\n",
			"TypeError: '<' not supported between instances of 'bool' and 'list'", "TypeError: '<' not supported between instances of 'int' and 'list'",
		},
		{
			"a None against a list names 'NoneType'",
			"print(None < [1])\n",
			"TypeError: '<' not supported between instances of 'NoneType' and 'list'", "TypeError: '<' not supported between instances of 'int' and 'list'",
		},
		{
			"a text against a list names 'str'",
			"print(\"a\" < [1])\n",
			"TypeError: '<' not supported between instances of 'str' and 'list'", "TypeError: '<' not supported between instances of 'int' and 'list'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "foldpair_relation.gy", tc.src)
			out, code := cliRunMerged(t, "--file", path)
			if code == 2 {
				t.Fatalf("exit 2 — LLVM rejected the module this ordering was emitted into (ADR 0166):\n%s", out)
			}
			// The reference's side of the row first: where the row records an ANSWER the reference must
			// still give it, and where it records a SENTENCE the reference must still refuse to answer.
			ref, refAnswered := cpythonPlainOut(t, dir, tc.src)
			if tc.reference != "" && !strings.HasPrefix(tc.reference, "TypeError") {
				if !refAnswered {
					t.Fatalf("the reference stopped answering a row that records its answer %q:\nsrc: %s", tc.reference, tc.src)
				}
				if strings.TrimRight(ref, "\n") != strings.TrimRight(tc.reference, "\n") {
					t.Fatalf("the reference's answer for this filed row drifted: it prints %q, the row records %q", ref, tc.reference)
				}
			} else if refAnswered {
				t.Fatalf("the reference answered %q for a row that records its raise %q", ref, tc.reference)
			}
			if !strings.Contains(out, tc.compiled) {
				t.Fatalf("the compiled raise is no longer the one this row files:\n got %q\nwant %q", out, tc.compiled)
			}
			if code == 0 {
				t.Fatalf("this shape answered at exit 0, which means the filed row (Gap R.201/R.202) is paid and "+
					"must be promoted, not re-asserted:\n%s", out)
			}
			if code != 3 {
				t.Fatalf("exit %d, want the trap exit 3 — a refusal here would be the compiler's opinion, not the "+
					"reference's raise:\n%s", code, out)
			}
			noteCompiledGap(t, tc.src, out)
		})
	}
}

// TestCLIAgentTheCyclicContainerFoldIsFiledNotFixed is Gap R.203 through the CLI — the exit-code half of the row
// whose answer half lives in `pkg/lang/pair_container_order_test.go`. A program whose fold walks a container that
// contains itself dies at **exit 2**, the code the exit-code contract reserves for a compiler that is actually
// broken (ADR 0166), where the reference prints `[[...]]`. It is asserted here rather than in the unit process
// because the death takes the process with it: the JIT run inside a test binary exits 2 itself, which is how this
// row was discovered to be untestable that way (ADR 0317's lesson, aimed at a crash this time).
//
// Before ADR 0318 the fold answered `0` at exit 0 for `print(min(xs, xs))` — a silent wrong answer that hid the
// printer's hole. The walk picks the right winner now and the program dies one statement later, in code this row
// does not own: the ordering's own arm is the honest one, which is why the depth-guard row records the reference's
// `RecursionError` beside the `TypeError` the compiled leg raises instead of claiming they agree.
func TestCLIAgentTheCyclicContainerFoldIsFiledNotFixed(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name      string
		src       string
		refAnswer string // what CPython prints, or "" when the reference traps
		refTrap   string
		compiled  string // what the compiled binary does TODAY
		code      int
	}{
		{
			name:      "a cyclic list printed leaves exit 2",
			src:       "xs = []\nxs.append(xs)\nprint(xs)\nprint(\"after\")\n",
			refAnswer: "[[...]]\nafter\n",
			compiled:  "",
			code:      2,
		},
		{
			name:      "the same list folded against itself leaves exit 2",
			src:       "xs = []\nxs.append(xs)\nprint(min(xs, xs))\nprint(\"after\")\n",
			refAnswer: "[[...]]\nafter\n",
			compiled:  "",
			code:      2,
		},
		{
			name:     "two independently cyclic containers hit the ordering's depth guard",
			src:      "xs = []\nxs.append(xs)\nys = []\nys.append(ys)\nprint(min(xs, ys))\n",
			refTrap:  "RecursionError",
			compiled: "TypeError: '<' not supported between instances of 'list' and 'list'",
			code:     3,
		},
		{
			name:     "a cyclic list against a plain one names the kinds that failed",
			src:      "xs = []\nxs.append(1)\nys = []\nys.append(xs)\nys.append(ys)\nprint(min(ys[0], ys[1]))\n",
			refTrap:  "TypeError: '<' not supported between instances of 'list' and 'int'",
			compiled: "TypeError: '<' not supported between instances of 'list' and 'int'",
			code:     3,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "foldpair_cyclic.gy", tc.src)
			ref, refAnswered := cpythonPlainOut(t, dir, tc.src)
			if tc.refAnswer != "" {
				if !refAnswered {
					t.Fatalf("the reference stopped answering this filed row: %s", tc.src)
				}
				if ref != tc.refAnswer {
					t.Fatalf("the reference's answer for this filed row drifted: %q, want %q", ref, tc.refAnswer)
				}
			} else if refAnswered {
				t.Fatalf("the reference answered %q where this row records its trap %s", ref, tc.refTrap)
			}
			out, code := cliRunMerged(t, "--file", path)
			if code != tc.code {
				t.Fatalf("the linked binary exited %d, want the filed exit %d (%s) — if this shape now answers, "+
					"delete this row and promote the program into the parity tables (roadmap Gap R.203):\n%s",
					code, tc.code, tc.compiled, out)
			}
			if tc.compiled != "" && !strings.Contains(out, tc.compiled) {
				t.Fatalf("the compiled raise is no longer the filed one:\n got %q\nwant %q", out, tc.compiled)
			}
			noteCompiledGap(t, tc.src, out)
		})
	}
}

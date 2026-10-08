package integration

// integration/pair_fold_test.go — the CLI half of `min`, `max` and `sum` over a value whose kind the run
// time chose (roadmap L11.1, Gap R.146's "`min(n, 3)`" and "a literal `sum`/`min`/`max` folds into a static
// array"; ADR 0316).
//
// The record leg (`pkg/lang/pair_fold_test.go`) holds these programs against the answer on record; this file
// asks CPython, which is the witness a fold especially needs: a fold HANDS BACK one of the values it was
// given, so the answer's kind is the winner's kind, and the failure mode this door replaced was not a crash
// but a plausible number — `max([1, 2.5])` answering `2` (Gap R.104), `min(2.5, 3)` answering `2.5` by
// accident while `max(2.5, 3)` answered `2.5` by truncation. Only the reference says which of those is the
// language's answer.
//
// The traps are the other half of the row: a fold of two values with no ordering is a run-time event that
// leaves through the program's own `except TypeError:` with CPython's sentence (candidate first, incumbent
// second — the order the failing comparison had), and what the door declines stays a refusal at exit 1 and
// never the compiler's exit 2 (ADR 0166).

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// The six slot kinds a fold can be asked about, spelled the way the door meets them: a container the program
// built, and a name read out of it.
const (
	foldCLIIntSlot   = "xs = []\nxs.append(7)\nn = xs[0]\n"
	foldCLINegSlot   = "xs = []\nxs.append(-7)\nn = xs[0]\n"
	foldCLIFloatSlot = "xs = []\nxs.append(2.5)\nn = xs[0]\n"
	foldCLITextSlot  = "xs = []\nxs.append(\"a\")\nn = xs[0]\n"
	foldCLINoneSlot  = "xs = []\nxs.append(None)\nn = xs[0]\n"
	foldCLIBoolSlot  = "xs = []\nxs.append(True)\nn = xs[0]\n"
)

// TestCLIAgentTheFoldBuiltinsAnswerThePairAgreesWithCPython is the reference leg of the answer table: the
// varargs spelling, the container-literal spelling, `sum` as the left fold over `+` seeded with the integer
// 0, and the answer bound to a name and read back by the positions that ask the tag.
func TestCLIAgentTheFoldBuiltinsAnswerThePairAgreesWithCPython(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src string }{
		// the varargs spelling, an int slot
		{"an int slot — min of the slot and a literal", foldCLIIntSlot + "print(min(n, 3))\n"},
		{"an int slot — max of the slot and a literal", foldCLIIntSlot + "print(max(n, 3))\n"},
		{"an int slot — the literal on the left", foldCLIIntSlot + "print(min(3, n))\n"},
		{"an int slot — the slot on the left of a bigger literal", foldCLIIntSlot + "print(max(3, n))\n"},
		{"an int slot — the slot against itself", foldCLIIntSlot + "print(min(n, n))\n"},
		{"an int slot — three candidates", foldCLIIntSlot + "print(min(n, 3, 1))\n"},
		{"an int slot — a double it beats", foldCLIIntSlot + "print(max(n, 2.5))\n"},
		{"an int slot — a double that beats it", foldCLIIntSlot + "print(min(n, 2.5))\n"},
		{"an int slot — beside a verdict it loses to", foldCLIIntSlot + "print(min(n, True))\n"},
		{"a negative slot", foldCLINegSlot + "print(min(n, 3))\n"},
		{"a negative slot — max", foldCLINegSlot + "print(max(n, 3))\n"},
		// the winner's kind is the winner's own — the four rows that used to truncate
		{"a float slot — min keeps the float", foldCLIFloatSlot + "print(min(n, 3))\n"},
		{"a float slot — max returns the INTEGER", foldCLIFloatSlot + "print(max(n, 3))\n"},
		{"a float slot — max keeps the float", foldCLIFloatSlot + "print(max(n, 1))\n"},
		{"a float slot — the slot against itself", foldCLIFloatSlot + "print(min(n, n))\n"},
		{"a float slot — a literal it loses to", foldCLIFloatSlot + "print(max(n, 3.5))\n"},
		{"a verdict slot — min keeps the verdict", foldCLIBoolSlot + "print(min(n, 3))\n"},
		{"a verdict slot — max returns the number", foldCLIBoolSlot + "print(max(n, 3))\n"},
		{"a verdict slot — the tie keeps the incumbent", foldCLIBoolSlot + "print(max(n, 1))\n"},
		// texts order by content, not by the intern table's arrival order (ADR 0248's rule, one door later)
		{"a text slot — min of two texts", foldCLITextSlot + "print(min(n, \"b\"))\n"},
		{"a text slot — max of two texts", foldCLITextSlot + "print(max(n, \"b\"))\n"},
		{"a text slot — the tie keeps the incumbent", foldCLITextSlot + "print(max(n, n))\n"},
		// sum is the left fold over + seeded with the integer 0
		{"an int slot — sum of one", foldCLIIntSlot + "print(sum([n]))\n"},
		{"an int slot — sum of two", foldCLIIntSlot + "print(sum([n, 1]))\n"},
		{"an int slot — sum with the literal first", foldCLIIntSlot + "print(sum([1, n]))\n"},
		{"an int slot — sum of the slot twice", foldCLIIntSlot + "print(sum([n, n]))\n"},
		{"a float slot — sum keeps the float", foldCLIFloatSlot + "print(sum([n]))\n"},
		{"a float slot — sum with an int beside it", foldCLIFloatSlot + "print(sum([n, 1]))\n"},
		{"a verdict slot — sum answers a number", foldCLIBoolSlot + "print(sum([n]))\n"},
		{"a verdict slot — sum of two", foldCLIBoolSlot + "print(sum([n, 1]))\n"},
		// the container-literal spelling
		{"an int slot — min over a literal list", foldCLIIntSlot + "print(min([n, 3]))\n"},
		{"an int slot — max over a literal list", foldCLIIntSlot + "print(max([n, 3]))\n"},
		{"a float slot — min over a literal list", foldCLIFloatSlot + "print(min([n, 3]))\n"},
		{"a float slot — max over a literal list keeps the winner's kind", foldCLIFloatSlot + "print(max([n, 2.5]))\n"},
		{"a text slot — min over a literal list", foldCLITextSlot + "print(min([n, \"b\"]))\n"},
		// the answer bound to a name, then read back
		{"a bound min prints", foldCLIIntSlot + "m = min(n, 3)\nprint(m)\n"},
		{"a bound max keeps the winner's float", foldCLIFloatSlot + "m = max(n, 1)\nprint(m)\n"},
		{"a bound sum prints", foldCLIIntSlot + "m = sum([n, 1])\nprint(m)\n"},
		{"a bound min enters a list literal", foldCLIIntSlot + "m = min(n, 3)\nprint([m])\n"},
		{"a bound min is measured by len", foldCLIIntSlot + "m = min(n, 3)\nprint(len([m, 1]))\n"},
		{"a bound min is asked for membership", foldCLIIntSlot + "m = min(n, 3)\nprint(3 in [m])\n"},
		{"a bound min renders through str", foldCLIIntSlot + "m = min(n, 3)\nprint(str(m))\n"},
		{"a bound min renders through repr", foldCLIFloatSlot + "m = max(n, 1)\nprint(repr(m))\n"},
		{"a bound min fills an f-string field", foldCLIFloatSlot + "m = min(n, 3)\nprint(f\"{m}\")\n"},
		{"a bound min is folded again by a second fold", foldCLIIntSlot + "m = min(n, 3)\nprint(max(m, 5))\n"},
		{"a bound sum of a float slot", foldCLIFloatSlot + "m = sum([n, 1])\nprint(m)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "pairfold.gy", tc.src)
			ref, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Skipf("the reference could not answer this program: %s", tc.src)
			}
			got, code := cliRunMerged(t, "--file", path)
			if code == 1 && refusesHonestly(got) {
				requireReferenceTrapOrHonestRefusal(t, tc.src, "", got, code,
					"roadmap L11.1 (the tagged value word) and Gap R.146 (the fold's positions)",
					"the winner's kind is a run-time fact and this position kept one word for it")
				return
			}
			if code != 0 {
				t.Fatalf("the compiled run exited %d on a program the reference prints:\n reference: %q\n compiled: %q", code, ref, got)
			}
			requireReferenceAgreement(t, tc.src, ReferenceAgreement{
				Python: ref, Compiled: got, Code: code,
			}, "roadmap L11.1 (the tagged value word, at the built-in folds)",
				"the answer of a min/max/sum whose winner the run time chose")
		})
	}
}

// TestCLIAgentAFoldOfValuesWithNoOrderingRaisesWhatCPythonRaises is the trap half through the CLI: the tags
// decide it, the exit is the trap exit, and the sentence is the reference's with the candidate named first
// and the incumbent second.
func TestCLIAgentAFoldOfValuesWithNoOrderingRaisesWhatCPythonRaises(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src, sentence string }{
		{"a number folded against a text (min)", foldCLIIntSlot + "print(min(n, \"a\"))\n", "TypeError: '<' not supported between instances of 'str' and 'int'"},
		{"a number folded against a text (max)", foldCLIIntSlot + "print(max(n, \"a\"))\n", "TypeError: '>' not supported between instances of 'str' and 'int'"},
		{"a number folded against None (min)", foldCLIIntSlot + "print(min(n, None))\n", "TypeError: '<' not supported between instances of 'NoneType' and 'int'"},
		{"a number folded against None (max)", foldCLIIntSlot + "print(max(n, None))\n", "TypeError: '>' not supported between instances of 'NoneType' and 'int'"},
		{"a float folded against a text", foldCLIFloatSlot + "print(max(n, \"a\"))\n", "TypeError: '>' not supported between instances of 'str' and 'float'"},
		{"a float folded against None", foldCLIFloatSlot + "print(min(n, None))\n", "TypeError: '<' not supported between instances of 'NoneType' and 'float'"},
		{"a verdict folded against a text names 'bool'", foldCLIBoolSlot + "print(min(n, \"a\"))\n", "TypeError: '<' not supported between instances of 'str' and 'bool'"},
		{"a verdict folded against None names 'bool'", foldCLIBoolSlot + "print(max(n, None))\n", "TypeError: '>' not supported between instances of 'NoneType' and 'bool'"},
		{"a text folded against a number", foldCLITextSlot + "print(min(n, 3))\n", "TypeError: '<' not supported between instances of 'int' and 'str'"},
		{"a text folded against a double", foldCLITextSlot + "print(max(n, 2.5))\n", "TypeError: '>' not supported between instances of 'float' and 'str'"},
		{"None folded against None", foldCLINoneSlot + "print(min(n, None))\n", "TypeError: '<' not supported between instances of 'NoneType' and 'NoneType'"},
		{"None folded against a number", foldCLINoneSlot + "print(max(n, 3))\n", "TypeError: '>' not supported between instances of 'int' and 'NoneType'"},
		{"a container slot folded against a number", "xs = []\nxs.append([1, 2])\nn = xs[0]\nprint(min(n, 3))\n", "TypeError: '<' not supported between instances of 'int' and 'list'"},
		{"a dict slot folded against a number", "xs = []\nxs.append({\"k\": 1})\nn = xs[0]\nprint(max(n, 3))\n", "TypeError: '>' not supported between instances of 'int' and 'dict'"},
		{"a set slot folded against a text", "xs = []\nxs.append({1})\nn = xs[0]\nprint(min(n, \"a\"))\n", "TypeError: '<' not supported between instances of 'str' and 'set'"},
		{"a sum of a number and a text", foldCLIIntSlot + "print(sum([n, \"a\"]))\n", "TypeError: unsupported operand type(s) for +: 'int' and 'str'"},
		{"a sum of a number and None", foldCLIIntSlot + "print(sum([n, None]))\n", "TypeError: unsupported operand type(s) for +: 'int' and 'NoneType'"},
		{"a sum of a float and a text", foldCLIFloatSlot + "print(sum([n, \"a\"]))\n", "TypeError: unsupported operand type(s) for +: 'float' and 'str'"},
		{"a sum of a number and a container slot", "xs = []\nxs.append(7)\nxs.append([1])\nn = xs[0]\nm = xs[1]\nprint(sum([n, m]))\n", "TypeError: unsupported operand type(s) for +: 'int' and 'list'"},
		{"a mixed container's fold raises on the kind that has no ordering", "xs = []\nxs.append(3)\nxs.append(\"a\")\nprint(min(xs[0], xs[1]))\n", "TypeError: '<' not supported between instances of 'str' and 'int'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "pairfold_trap.gy", tc.src)
			if py, ok := cpythonPlainOut(t, dir, tc.src); ok {
				t.Fatalf("the reference answered %q, expected the trap\nsrc: %s", py, tc.src)
			}
			out, code := cliRunMerged(t, "--file", path)
			if code == 2 {
				t.Fatalf("exit 2 — LLVM rejected the module this raise was emitted into (ADR 0166):\n%s", out)
			}
			if code != 3 {
				t.Fatalf("exit %d, want the trap exit 3 (ADR 0166):\n%s", code, out)
			}
			if !strings.Contains(out, tc.sentence) {
				t.Errorf("the compiled raise did not say %q:\n%s", tc.sentence, out)
			}
		})
	}
}

// TestCLIAgentAFoldTrapReachesTheProgramsOwnArm is the half that makes a compiled raise a raise: through the
// CLI, in a linked binary, the handler runs, the program continues, and the exit code is success.
func TestCLIAgentAFoldTrapReachesTheProgramsOwnArm(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src, want string }{
		{"min caught", foldCLIIntSlot + "try:\n    print(min(n, \"a\"))\nexcept TypeError:\n    print(\"caught-the-ordering\")\n", "caught-the-ordering\n"},
		{"max caught", foldCLINoneSlot + "try:\n    print(max(n, 1))\nexcept TypeError:\n    print(\"caught-the-none-ordering\")\n", "caught-the-none-ordering\n"},
		{"the sum's raise caught", foldCLIIntSlot + "try:\n    print(sum([n, \"a\"]))\nexcept TypeError:\n    print(\"caught-the-sum-of-text\")\n", "caught-the-sum-of-text\n"},
		{"a caught fold leaves the name usable", foldCLIIntSlot + "ok = 0\ntry:\n    ok = max(n, 3)\nexcept TypeError:\n    ok = 1\nprint(ok)\n", "7\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "pairfold_catch.gy", tc.src)
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

// TestCLIAgentWhatTheFoldDoorStillRefusesIsStillRefusedInWords keeps the honest half at the CLI: the positions
// that keep ONE word for a fold's answer, the fold over a set literal, and a container literal as a fold
// operand each stay refusals that name the missing half — at exit 1, never at the compiler's exit 2.
func TestCLIAgentWhatTheFoldDoorStillRefusesIsStillRefusedInWords(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src, want string }{
		{"a fold answer as an arithmetic operand", foldCLIIntSlot + "print(min(n, 3) + 1)\n", "one word"},
		{"a fold answer as an abs operand", foldCLIIntSlot + "print(abs(min(n, 3)))\n", "one word"},
		{"a bound fold answer as an arithmetic operand", foldCLIIntSlot + "m = min(n, 3)\nprint(m + 1)\n", "the answer of a fold the built-in chose"},
		{"a bound fold answer in an ordering", foldCLIIntSlot + "m = min(n, 3)\nprint(m > 1)\n", "the answer of a fold the built-in chose"},
		{"a fold over a set literal", foldCLIIntSlot + "print(min({n, 3}))\n", "one word"},
		{"a container literal as a fold operand", foldCLIIntSlot + "print(sum([n, [1]]))\n", "sum adds numbers"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "pairfold_refuse.gy", tc.src)
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

// TestCLIAgentAFoldOfTwoBuiltContainersIsFiledNotFixed is the debt row's live half. The conformance ledger
// pins `programs/probe_a_fold_orders_two_built_containers.gy` with the compiled leg's partial stdout and
// `exit status 3`; the sentence the raise writes, and the fact that the REFERENCE answers those three lines
// at all, are asserted here. A raise where the reference answered a value is the wrong-answer class — the
// same class Gap R.97 files for `<` — so this is a filed defect, not a refusal, and it is the residue the
// fold door leaves (roadmap Gap R.197, filed measuring ADR 0316).
func TestCLIAgentAFoldOfTwoBuiltContainersIsFiledNotFixed(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src, partial, sentence, reference string }{
		{
			"two built lists, ordered lexicographically by the reference",
			"xs = []\nxs.append([1, 2])\nxs.append([3])\na = xs[0]\nb = xs[1]\nprint(min(a, b))\n",
			"", "TypeError: '<' not supported between instances of 'list' and 'list'", "[1, 2]\n",
		},
		{
			"two built sets, ordered by the subset operator by the reference",
			"xs = []\nxs.append({1})\nxs.append({2})\na = xs[0]\nb = xs[1]\nprint(max(a, b))\n",
			"", "TypeError: '>' not supported between instances of 'set' and 'set'", "{1}\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "pairfold_container_order.gy", tc.src)
			ref, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Skipf("the reference declined to answer a program this row files as its own answer: %s", tc.src)
			}
			if strings.TrimRight(ref, "\n") != strings.TrimRight(tc.reference, "\n") {
				t.Fatalf("the case's recorded reference answer drifted: it prints %q, the row says %q", ref, tc.reference)
			}
			out, code := cliRunMerged(t, "--file", path)
			if code == 2 {
				t.Fatalf("exit 2 — LLVM rejected the module this raise was emitted into (ADR 0166):\n%s", out)
			}
			if code != 3 {
				t.Fatalf("exit %d, want the trap exit 3 — the reference ANSWERS this program, so an answer at exit 0 "+
					"would be a wrong answer and a refusal would be a different claim (ADR 0166):\n%s", code, out)
			}
			if tc.partial != "" && !strings.HasPrefix(out, tc.partial) {
				t.Errorf("the compiled leg stopped printing before the raise: got %q, want the prefix %q", out, tc.partial)
			}
			if !strings.Contains(out, tc.sentence) {
				t.Errorf("the compiled raise did not say %q:\n%s", tc.sentence, out)
			}
			noteCompiledGap(t, tc.src, out)
		})
	}
}

// TestTheFoldProbeIsOnRecord is the loud version of ADR 0302's missing-record rule for the program the
// conformance matrix registers: a deleted record fails a test instead of skipping the row (ADR 0311's
// precedent, ADR 0306's), and the compiled leg must print the reference's bytes and not merely its own.
func TestTheFoldProbeIsOnRecord(t *testing.T) {
	src := readProgram(t, "probe_the_fold_builtins_answer_the_pair.gy")
	if !lang.HasGoldenAnswer(src) {
		t.Fatal("the probe the matrix registers has no record — every corpus case that reads expectations " +
			"from the record fails on it, starting with TestGCCorpusCollectsAndAgrees")
	}
	res, err := lang.JIT(src, 0)
	if err != nil {
		t.Fatalf("the compiled backend refused a program the reference prints: %v", err)
	}
	ref, ok := cpythonPlainOut(t, t.TempDir(), src)
	if !ok {
		t.Fatalf("the reference could not answer the probe")
	}
	if res.Output != ref {
		t.Fatalf("the compiled leg prints other bytes than the reference's:\n got %q\nwant %q", res.Output, ref)
	}
}

// TestTheFoldDoorDoesNotRouteAProgramThatWasAlreadyAnswered is the cheap gate at the CLI: a fold of nothing
// but literals, and a fold over a container of nothing but literals, are answered by the constant road, and
// landing this door must not have rerouted one of them.
func TestTheFoldDoorDoesNotRouteAProgramThatWasAlreadyAnswered(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src, want string }{
		{"a literal min", "print(min(2.5, 3))\n", "2.5\n"},
		{"a literal max", "print(max(2.5, 3))\n", "3\n"},
		{"a literal sum over a list", "print(sum([1, 2, 3]))\n", "6\n"},
		{"a literal sum over floats", "print(sum([1.5, 2.5]))\n", "4.0\n"},
		{"a literal min over a list", "print(min([3, 1, 2]))\n", "1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "pairfold_const.gy", tc.src)
			out, code := cliRunMerged(t, "--file", path)
			if code != 0 {
				t.Fatalf("the constant fold road exited %d on a program it has always answered:\n%s", code, out)
			}
			if out != tc.want {
				t.Fatalf("the constant fold road changed its answer to %q, want %q", out, tc.want)
			}
			res, err := lang.Compile(tc.src)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			for _, absent := range []string{"@rt_pair_fold", "@rt_fold_bad", "@rt.num.msg"} {
				if strings.Contains(res.IR, absent) {
					t.Errorf("a fold the compiler can settle started carrying %q (ADR 0309's gate on the fold door)", absent)
				}
			}
		})
	}
}

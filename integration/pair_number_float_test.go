package integration

// integration/pair_number_float_test.go — the CLI half of a pair-bound name entering the DOUBLE domain
// (roadmap L11.1, Gap R.148; ADR 0303's binding, ADR 0304's arithmetic door, ADR 0253 and ADR 0252's
// per-kind raises).
//
// `pkg/lang/pair_number_float_test.go` checks these programs against the record the retired engine left;
// this file asks whether CPython agrees, which is the question a record cannot answer (ADR 0302's two
// ledgers: the record keeps the language measurable, the reference defines it).
//
// The shape Gap R.148 was filed with is the asymmetry: `print(n / 4)` and `print(n > 2.5)` refused while
// `print(2.5 - n)` answered, and the naive route was worse than the refusal — storing a `double` into the
// i32 slot a tagged name owns is a module `llc` rejects, which ADR 0166 counts as the compiler's own bug.
// What the door has to be is a branch on the tag: the float arm unboxes, the int/bool arm converts, and
// every other kind raises the reference's own sentence — with BOTH operand types in source order for an
// ordering, which is why the pair walks the tagged ordering door instead of a bare lift.

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

const numFloatSlotCLI = "xs = []\nxs.append(7)\nn = xs[0]\n"

// TestCLIDoubleDomainOfASlotBoundNameAgreesWithCPython is the row: the reference and the compiled artifact
// run the same program in the same case, and a disagreement becomes a debt row rather than an expectation.
func TestCLIDoubleDomainOfASlotBoundNameAgreesWithCPython(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src string }{
		{"true division", numFloatSlotCLI + "print(n / 4)\n"},
		{"division by a float literal", numFloatSlotCLI + "print(n / 2.5)\n"},
		{"ordering against a float", numFloatSlotCLI + "print(n > 2.5)\n"},
		{"equality against a float", numFloatSlotCLI + "print(n == 2.5)\n"},
		{"addition with a float", numFloatSlotCLI + "print(n + 2.5)\n"},
		{"product with a float", numFloatSlotCLI + "print(n * 2.5)\n"},
		{"arithmetic over the name, then a float", numFloatSlotCLI + "print(n - 1 + 0.5)\n"},
		{"the float on the left", "xs = []\nxs.append(7)\nn = xs[0]\nprint(2.5 - n)\n"},
		{"an ordering against an int literal", numFloatSlotCLI + "print(n > 7)\n"},
		{"the int literal on the left", "xs = []\nxs.append(7)\nn = xs[0]\nprint(7 > n)\n"},
		{"a reversed ordering", numFloatSlotCLI + "print(n >= 7)\n"},
		{"a comparison the condition asks", numFloatSlotCLI + "if n > 2.5:\n    print(\"big\")\n"},
		{"the double reaches a builtin", numFloatSlotCLI + "print(round(n / 4))\n"},
		// abs of a pair is the signless call of the arithmetic door: a number out whatever the slot holds, so a
		// float slot's magnitude keeps its .0 and a negative slot's answers the positive (ADR 0309).
		{"abs of a negative slot", "xs = []\nxs.append(-7)\nn = xs[0]\nprint(abs(n))\n"},
		{"abs of a negative float slot", "xs = []\nxs.append(-2.5)\nn = xs[0]\nprint(abs(n))\n"},
		{"a float slot keeps its float", "xs = []\nxs.append(7.5)\nn = xs[0]\nprint(n / 2)\n"},
		{"a float slot ordered against an int", "xs = []\nxs.append(7.5)\nn = xs[0]\nprint(n > 7)\n"},
		{"a negative slot divided", "xs = []\nxs.append(-8)\nn = xs[0]\nprint(n / 4)\n"},
		{"a bool slot divided", "xs = []\nxs.append(True)\nn = xs[0]\nprint(n / 2)\n"},
		{"a dict slot by key", "d = {}\nd[\"k\"] = 9\nn = d[\"k\"]\nprint(n / 3)\n"},
		{"a slot read by a computed position", "xs = []\nxs.append(7)\nxs.append(9)\ni = 1\nn = xs[i]\nprint(n / 3)\n"},
		{"two levels down", "xs = []\nxs.append([7, 8])\nn = xs[0][1]\nprint(n / 2)\n"},
		{"the loop variable divided", "xs = []\nxs.append(7)\nfor v in xs:\n    print(v / 2)\n"},
		{"the loop variable ordered", "xs = []\nxs.append(5)\nfor v in xs:\n    print(v > 3)\n"},
		{"a while head over the ordering", "xs = []\nxs.append(5)\nn = xs[0]\nwhile n > 3:\n    n = n - 1\nprint(n)\n"},
		{"the answer bound and used again", numFloatSlotCLI + "h = n / 2\nprint(h)\nprint(h + 1)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "pairfloat.gy", tc.src)
			ref, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Skipf("the reference could not answer this program: %s", tc.src)
			}
			got, code := cliRunMerged(t, "--file", path)
			if code == 1 && refusesHonestly(got) {
				requireReferenceTrapOrHonestRefusal(t, tc.src, "", got, code,
					"roadmap L11.1 (the tagged value word) and Gap R.148 (a pair-bound name in the float domain)",
					"the operand's kind is a run-time fact and this position kept one word for it")
				return
			}
			if code != 0 {
				t.Fatalf("the compiled run exited %d on a program the reference prints:\n reference: %q\n compiled: %q", code, ref, got)
			}
			requireReferenceAgreement(t, tc.src, ReferenceAgreement{
				Python: ref, Compiled: got, Code: code,
			}, "roadmap L11.1 (the tagged value word, at the float door)",
				"a name bound from a container slot, used where the reference wants a double")
		})
	}
}

// TestCLIDoubleDomainOfASlotBoundNameRaisesTheKindItHolds is why this door branches on the tag instead of
// calling the lift: `rt_lift_num` unboxes a float and `sitofp`s everything else, so a text slot would answer
// the interned index of "a" as a number, at exit 0. Class and message are compared against the reference, and
// the ordering rows check BOTH types in source order — the sentence is what an `except` matches on.
func TestCLIDoubleDomainOfASlotBoundNameRaisesTheKindItHolds(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ src, trap string }{
		{"xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(n / 4)\n", "TypeError: unsupported operand type(s) for /: 'str' and 'int'"},
		{"xs = []\nxs.append(None)\nn = xs[0]\nprint(n / 4)\n", "TypeError: unsupported operand type(s) for /: 'NoneType' and 'int'"},
		{"xs = []\nxs.append([1])\nn = xs[0]\nprint(n / 4)\n", "TypeError: unsupported operand type(s) for /: 'list' and 'int'"},
		{"xs = []\nxs.append({\"k\": 1})\nn = xs[0]\nprint(n / 4)\n", "TypeError: unsupported operand type(s) for /: 'dict' and 'int'"},
		{"xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(n > 2.5)\n", "TypeError: '>' not supported between instances of 'str' and 'float'"},
		{"xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(n > 7)\n", "TypeError: '>' not supported between instances of 'str' and 'int'"},
		{"xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(7 > n)\n", "TypeError: '>' not supported between instances of 'int' and 'str'"},
		{"xs = []\nxs.append(None)\nn = xs[0]\nprint(n >= 1)\n", "TypeError: '>=' not supported between instances of 'NoneType' and 'int'"},
		{"xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(n + 2.5)\n", "TypeError: can only concatenate str (not \"float\") to str"},
		{numFloatSlotCLI + "print(n / 0)\n", "ZeroDivisionError: division by zero"},
		{numFloatSlotCLI + "print(n / 0.0)\n", "ZeroDivisionError: float division by zero"},
	} {
		path := writeSrc(t, dir, "pairfloat_trap.gy", tc.src)
		got, code := cliRunMerged(t, "--file", path)
		requireReferenceTrapOrHonestRefusal(t, tc.src, tc.trap, got, code,
			"roadmap L11.1 (the tagged value word), ADR 0253 (the per-kind raise) and Gap R.148",
			"the operand's kind is a run-time fact")
		if code == 2 {
			t.Errorf("exit 2 — LLVM rejected the module gusty emitted (ADR 0166):\n%s", got)
		}
	}
}

// TestCLIDoubleDomainTrapOfASlotBoundNameIsCatchable: the raise is inside the arm that knows what it lifted and
// leaves through the open handler, so the program's own `except` runs (ADR 0228).
func TestCLIDoubleDomainTrapOfASlotBoundNameIsCatchable(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src, want string }{
		{
			"a TypeError arm runs on a slot's division",
			"xs = []\nxs.append(\"a\")\nn = xs[0]\ntry:\n    print(n / 4)\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"a TypeError arm runs on a slot's ordering",
			"xs = []\nxs.append(\"a\")\nn = xs[0]\ntry:\n    print(n > 7)\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"a ZeroDivisionError arm runs on a slot's division",
			numFloatSlotCLI + "try:\n    print(n / 0)\nexcept ZeroDivisionError:\n    print(\"caught\")\n",
			"caught\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "pairfloat_catch.gy", tc.src)
			got, code := cliRunMerged(t, "--file", path)
			if code != 0 || !strings.Contains(got, tc.want) {
				t.Fatalf("the arm the program wrote did not run (exit %d, out %q), want %q", code, got, tc.want)
			}
		})
	}
}

// TestCLIDoubleDomainOfASlotBoundNameNeverEscapesAsAnExitTwo: Gap R.148 was filed partly because the naive
// route — storing a double into a tagged name's i32 slot — is exactly the module llc rejects. None of these
// shapes may reach the toolchain-rejection class, answered, trapped or refused.
func TestCLIDoubleDomainOfASlotBoundNameNeverEscapesAsAnExitTwo(t *testing.T) {
	dir := t.TempDir()
	for _, src := range []string{
		numFloatSlotCLI + "print(n / 4)\n",
		numFloatSlotCLI + "print(n > 7)\n",
		numFloatSlotCLI + "print(n > 2.5)\n",
		numFloatSlotCLI + "print(n + 2.5)\n",
		numFloatSlotCLI + "print(n - 1 + 0.5)\n",
		"xs = []\nxs.append(7.5)\nn = xs[0]\nprint(n > 7)\n",
		"xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(n / 4)\n",
		numFloatSlotCLI + "print(float(n))\n",
		numFloatSlotCLI + "print(sum([n]))\n",
		// `print(f"{n - 1}")` answers since ADR 0307 — an f-string field asks the module's ONE
		// tag-reading printer — and is pinned against the reference in pair_fstring_test.go.
	} {
		path := writeSrc(t, dir, "pairfloat_noexit2.gy", src)
		if out, code := cliRunCode(t, "--file", path); code == 2 {
			t.Errorf("exit 2 — LLVM rejected the module gusty emitted (ADR 0166):\n%s", out)
		}
	}
}

// TestCompiledDoubleDomainOfASlotBoundNameBranchesOnTheTag is the module-shape half: the answer comes from
// arms the tag selects, the float arm unboxes through the one reader of a float box, and no empty-operand
// float instruction — ADR 0253's blacklist — stands in for an operand the compiler never read.
func TestCompiledDoubleDomainOfASlotBoundNameBranchesOnTheTag(t *testing.T) {
	for _, src := range []string{
		numFloatSlotCLI + "print(n / 4)\n",
		numFloatSlotCLI + "print(n > 7)\n",
	} {
		res, err := lang.Compile(src)
		if err != nil {
			t.Fatalf("compile %q: %v", src, err)
		}
		for _, forbidden := range []string{"fdiv double ,", "fadd double ,", "fmul double ,", "fsub double ,"} {
			if strings.Contains(res.IR, forbidden) {
				t.Errorf("%s: the module substituted an operand it never read (%s) — ADR 0253's blacklist", src, forbidden)
			}
		}
		if !strings.Contains(res.IR, "_n_tag") {
			t.Errorf("%s: the module read the name without its tag, so the arm is a guess", src)
		}
	}
}

// TestTheDoubleDomainStillRefusesTheOneWordPositions keeps the remaining half of Gap R.146/R.148 visible: a
// `float()` argument, a `sum` element and a `min` argument each still keep one word for the value, and refuse
// in words that name the origin — never a loop the program does not contain (Gap R.38). An `abs` operand and an
// f-string field were on this table and both answer now (ADR 0306, ADR 0307).
func TestTheDoubleDomainStillRefusesTheOneWordPositions(t *testing.T) {
	dir := t.TempDir()
	for _, src := range []string{
		numFloatSlotCLI + "print(float(n))\n",
		numFloatSlotCLI + "print(sum([n]))\n",
		// `print([n])` answers since ADR 0306 — the heap builder writes the element's payload and its
		// tag — and is pinned in pair_container_test.go against the reference instead.
		numFloatSlotCLI + "print(min(n, 3))\n",
		// `print(f"{n - 1}")` answers since ADR 0307 — an f-string field asks the module's ONE
		// tag-reading printer — and is pinned against the reference in pair_fstring_test.go.
	} {
		path := writeSrc(t, dir, "pairfloat_refuse.gy", src)
		got, code := cliRunMerged(t, "--file", path)
		if code == 0 {
			t.Fatalf("the compiler answered a position that keeps one word for its operand:\n%s", got)
		}
		if !refusesHonestly(got) {
			t.Errorf("the refusal is not actionable:\n%s", got)
		}
		if strings.Contains(got, "loop") {
			t.Errorf("the refusal blames a loop for a program that has none:\n%s", got)
		}
		noteCompiledGap(t, src, got)
	}
}

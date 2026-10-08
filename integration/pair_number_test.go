package integration

// integration/pair_number_test.go — the CLI half of a name bound from a container slot being used as a
// NUMBER (roadmap L11.1, Gap R.146's arithmetic positions; ADR 0303's pair binding, ADR 0265's door).
//
// `pkg/lang/pair_number_test.go` checks these programs against the record the retired engine left; this
// file asks the question a record cannot answer, which is whether CPython agrees. The distinction is
// ADR 0302's two-ledger rule: the record keeps the language's answers measurable, and the reference is
// still the only thing that defines them. Where they disagree the reference wins and the row goes to
// `testdata/cpython-debt.json` with the roadmap row that owns the fix.
//
// The line this file walks is ADR 0265's: the operators that answer a number or raise (`-`, `-x`, `//`)
// take the tagged door whatever the slot holds, and `rt_num_arith` raises CPython's own sentence per kind;
// the operators the reference can answer with something that is NOT a number (`+`, `*`, `%` in its printf
// form) stay behind the compiler's number proof, and refuse in words. Nothing in here may spend exit 2 —
// ADR 0166 reserves that class for the compiler's own failures.

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

const pairNumberBuiltInt = "xs = []\nxs.append(7)\nn = xs[0]\n"

// TestCLIArithmeticOfASlotBoundNameAgreesWithCPython is the row: the reference and the compiled artifact
// run in the same case, and a disagreement becomes a debt row rather than an expectation.
func TestCLIArithmeticOfASlotBoundNameAgreesWithCPython(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src string }{
		{"subtraction", pairNumberBuiltInt + "print(n - 1)\n"},
		{"the subtrahend is the slot", "xs = []\nxs.append(7)\nprint(10 - xs[0])\n"},
		{"negation", pairNumberBuiltInt + "print(-n)\n"},
		{"floor division", pairNumberBuiltInt + "print(n // 2)\n"},
		{"abs of the slot", pairNumberBuiltInt + "print(abs(n))\n"},
		{"abs of a negative slot", "xs = []\nxs.append(-4)\nn = xs[0]\nprint(abs(n))\n"},
		{"abs of a bool slot", "xs = []\nxs.append(True)\nn = xs[0]\nprint(abs(n))\n"},
		{"abs bound and printed again", pairNumberBuiltInt + "y = abs(n)\nprint(y)\n"},
		{"a float slot keeps its float", "xs = []\nxs.append(2.5)\nn = xs[0]\nprint(n - 1)\n"},
		{"a float slot negated", "xs = []\nxs.append(2.5)\nn = xs[0]\nprint(-n)\n"},
		{"a negative slot", "xs = []\nxs.append(-4)\nn = xs[0]\nprint(n - 1)\n"},
		{"a bool slot is the number it is", "xs = []\nxs.append(True)\nn = xs[0]\nprint(n - 1)\n"},
		{"a dict slot by key", "d = {}\nd[\"k\"] = 9\nn = d[\"k\"]\nprint(n - 1)\n"},
		{"a computed position", "xs = []\nxs.append(7)\nxs.append(2)\ni = 1\nn = xs[i]\nprint(n - 1)\n"},
		{"two levels down", "xs = []\nxs.append([7, 8])\nn = xs[0][1]\nprint(n - 1)\n"},
		{"the loop variable", "xs = []\nxs.append(7)\nfor v in xs:\n    print(v - 1)\n"},
		{"the loop variable negated", "xs = []\nxs.append(7)\nfor v in xs:\n    print(-v)\n"},
		{"the answer bound and used again", pairNumberBuiltInt + "m = n - 1\nprint(m)\nprint(m * 2)\n"},
		{"two slots, one sum", "xs = []\nxs.append(7)\nxs.append(8)\na = xs[0]\nb = xs[1]\nprint(b - a)\n"},
		{"the number gates a condition", pairNumberBuiltInt + "if n - 1:\n    print(\"truthy\")\n"},
		{"the number reaches a builtin", pairNumberBuiltInt + "print(len(str(n - 1)))\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "pairnum.gy", tc.src)
			ref, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Skipf("the reference could not answer this program: %s", tc.src)
			}
			got, code := cliRunMerged(t, "--file", path)
			if code == 1 && refusesHonestly(got) {
				requireReferenceTrapOrHonestRefusal(t, tc.src, "", got, code,
					"roadmap L11.1 (the tagged value word) and Gap R.146 (the positions that keep one word)",
					"the operand's kind is a run-time fact and this position kept one word for it")
				return
			}
			if code != 0 {
				t.Fatalf("the compiled run exited %d on a program the reference prints:\n reference: %q\n compiled: %q", code, ref, got)
			}
			requireReferenceAgreement(t, tc.src, ReferenceAgreement{
				Python: ref, Compiled: got, Code: code,
			}, "roadmap L11.1 (the tagged value word, at the arithmetic door)",
				"arithmetic on a name bound from a container slot")
		})
	}
}

// TestCLIArithmeticOfASlotBoundNameRaisesRatherThanAnswersWrongly is the safety half of widening the door:
// a slot that holds a text, a None or a container has to RAISE the reference's class and message, because
// the alternative is summing the payload's bits — a number where the reference has a string, at exit 0.
func TestCLIArithmeticOfASlotBoundNameRaisesRatherThanAnswersWrongly(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ src, trap string }{
		{"xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(n - 1)\n", "TypeError: unsupported operand type(s) for -: 'str' and 'int'"},
		{"xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(-n)\n", "TypeError: bad operand type for unary -: 'str'"},
		{"xs = []\nxs.append(None)\nn = xs[0]\nprint(-n)\n", "TypeError: bad operand type for unary -: 'NoneType'"},
		{"xs = []\nxs.append([1])\nn = xs[0]\nprint(-n)\n", "TypeError: bad operand type for unary -: 'list'"},
		{"xs = []\nxs.append({\"k\": 1})\nn = xs[0]\nprint(-n)\n", "TypeError: bad operand type for unary -: 'dict'"},
		{"xs = []\nxs.append({1})\nn = xs[0]\nprint(n - 1)\n", "TypeError: unsupported operand type(s) for -: 'set' and 'int'"},
		{pairNumberBuiltInt + "print(n // 0)\n", "ZeroDivisionError: integer division or modulo by zero"},
	} {
		path := writeSrc(t, dir, "pairnum_trap.gy", tc.src)
		got, code := cliRunMerged(t, "--file", path)
		requireReferenceTrapOrHonestRefusal(t, tc.src, tc.trap, got, code,
			"roadmap L11.1 (the tagged value word) and ADR 0265 (the door that raises per kind)",
			"the operand's kind is a run-time fact")
		if code == 2 {
			t.Errorf("exit 2 — LLVM rejected the module gusty emitted (ADR 0166):\n%s", got)
		}
	}
}

// TestCLIArithmeticOfASlotBoundNameIsCatchable: the raise leaves through the door the program can reach, so
// the `except` arm the programmer wrote runs (ADR 0228's rule for every trap this compiler emits).
func TestCLIArithmeticOfASlotBoundNameIsCatchable(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src, want string }{
		{
			"a TypeError arm runs",
			"xs = []\nxs.append(\"a\")\nn = xs[0]\ntry:\n    print(n - 1)\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"a ZeroDivisionError arm runs on a slot's floor division",
			pairNumberBuiltInt + "try:\n    print(n // 0)\nexcept ZeroDivisionError:\n    print(\"caught\")\n",
			"caught\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "pairnum_catch.gy", tc.src)
			got, code := cliRunMerged(t, "--file", path)
			if code != 0 || !strings.Contains(got, tc.want) {
				t.Fatalf("the arm the program wrote did not run (exit %d, out %q), want %q", code, got, tc.want)
			}
		})
	}
}

// TestCLIArithmeticOfASlotBoundNameNeverEscapesAsAnExitTwo: none of these shapes — answered, trapped or
// refused — may reach the toolchain-rejection class.
func TestCLIArithmeticOfASlotBoundNameNeverEscapesAsAnExitTwo(t *testing.T) {
	dir := t.TempDir()
	for _, src := range []string{
		pairNumberBuiltInt + "print(n - 1)\n",
		pairNumberBuiltInt + "print(-n)\n",
		pairNumberBuiltInt + "print(n // 2)\n",
		pairNumberBuiltInt + "print(n + 1)\n",
		pairNumberBuiltInt + "print(n % 3)\n",
		pairNumberBuiltInt + "print(abs(n))\n",
		pairNumberBuiltInt + "print([n])\n",
		pairNumberBuiltInt + "print(n - 1 + 0.5)\n",
		"xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(n - 1)\n",
		"xs = []\nxs.append(7)\nfor v in xs:\n    print(v - 1)\n",
	} {
		path := writeSrc(t, dir, "pairnum_noexit2.gy", src)
		if out, code := cliRunCode(t, "--file", path); code == 2 {
			t.Errorf("exit 2 — LLVM rejected the module gusty emitted (ADR 0166):\n%s", out)
		}
	}
}

// TestCompiledArithmeticOfASlotBoundNameAsksTheTaggedDoor is the module-shape half through the CLI: the sum
// is done by the door that branches on the tag, and the raise leaves through the emitted door rather than a
// helper that called `abort` for itself (ADR 0265's IR contract).
func TestCompiledArithmeticOfASlotBoundNameAsksTheTaggedDoor(t *testing.T) {
	res, err := lang.Compile(pairNumberBuiltInt + "print(n - 1)\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, want := range []string{"@rt_num_arith", "@rt_lift_num"} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("the module answers arithmetic on a slot-bound name without %s", want)
		}
	}
	if strings.Contains(res.IR, "fdiv double ,") || strings.Contains(res.IR, "fadd double ,") {
		t.Errorf("the module substituted an operand it never read (ADR 0253's empty-operand fdiv):\n%s", head(res.IR, 30))
	}
}

// head is the IR listing an IR-shape failure quotes: enough lines to see what the module did, without
// drowning the report in a module that is thousands of lines long.
func head(s string, lines int) string {
	out := strings.SplitN(s, "\n", lines+1)
	if len(out) > lines {
		out = out[:lines]
	}
	return strings.Join(out, "\n")
}

// TestTheOneWordPositionsStillRefuseInWordsThatNameTheOrigin keeps the soft landings soft *and* honest: the
// positions this cycle did not open still answer with a sentence naming the missing half and its owner, and
// never with a story about a loop the program does not contain (Gap R.38).
func TestTheOneWordPositionsStillRefuseInWordsThatNameTheOrigin(t *testing.T) {
	dir := t.TempDir()
	for _, src := range []string{
		pairNumberBuiltInt + "print(n + 1)\n",
		pairNumberBuiltInt + "print(n * 2)\n",
		pairNumberBuiltInt + "print(n % 3)\n",
		// `print(abs(n))` was on this table and answers since ADR 0309: the signless call takes the
		// operand's kind from the tag. `print([n])` was on this table too and is answered since ADR 0306: a container element is no
		// longer a position that keeps one word. It lives in pair_container_test.go's answer table.
		// `print(min(n, 3))` was on this table a third time, and it answers since ADR 0316 — a fold hands back
		// one of the values it was given, and the door writes the winner's payload beside the winner's tag. It
		// lives in pair_fold_test.go, together with the fold shapes this table still refuses (an answer used as
		// an arithmetic or `abs` operand, in pair_binding_test.go's table beside this one).
	} {
		path := writeSrc(t, dir, "pairnum_refuse.gy", src)
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

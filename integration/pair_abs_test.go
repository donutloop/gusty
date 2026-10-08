package integration

// integration/pair_abs_test.go — the CLI half of the signless call asking the tag (roadmap L11.1,
// Gap R.146's positions that keep one word for a whole value; ADR 0309).
//
// pkg/lang/pair_abs_test.go checks these programs against the record; this file asks the question a record
// cannot answer for a shape it has never seen: whether CPython agrees. That question is not ceremony. `abs`
// of a pair-bound name is a CALL whose operand's kind the compiler cannot see, and the failure mode of a
// one-word reading is a plausible number where the reference has a string — a text slot's payload is its
// `@str_tab` index, a float's its box handle — taken by a magnitude and printed at the exit code of success,
// which `compiled refusals this run` never counts.
//
// The raise rows are the other half of the CLI comparison, and they are why this file exists beside the pkg
// one. The reference answers a magnitude or raises `bad operand type for abs(): 'str'`; the signless call's
// sentence is NOT the unary minus's, and only the reference can tell those two apart (ADR 0271).

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

const absCLISlot = "xs = []\nxs.append(7)\nn = xs[0]\n"

// TestCLIAbsAgreesWithCPython runs the reference and the compiled artifact over the same program.
func TestCLIAbsAgreesWithCPython(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src string }{
		{"an int slot", absCLISlot + "print(abs(n))\n"},
		{"a negative slot", "xs = []\nxs.append(-8)\nn = xs[0]\nprint(abs(n))\n"},
		{"a float slot", "xs = []\nxs.append(-2.5)\nn = xs[0]\nprint(abs(n))\n"},
		{"a positive float slot keeps its .0", "xs = []\nxs.append(2.5)\nn = xs[0]\nprint(abs(n))\n"},
		{"a bool slot is the number it is", "xs = []\nxs.append(True)\nn = xs[0]\nprint(abs(n))\n"},
		{"a zero slot", "xs = []\nxs.append(0)\nn = xs[0]\nprint(abs(n))\n"},
		{"a negative zero answers zero", "xs = []\nxs.append(-0.0)\nn = xs[0]\nprint(abs(n))\n"},
		{"the answer is bound and printed", absCLISlot + "y = abs(n)\nprint(y)\n"},
		{"the answer is bound and measured", absCLISlot + "y = abs(n)\nprint(len([y]))\n"},
		{"a slot of the arithmetic over it", "xs = []\nxs.append([7, 8])\nn = xs[0][0] * 2\nprint(abs(n))\n"},
		{"the sign already gone", "xs = []\nxs.append([7, 8])\nn = xs[0][0] * -2\nprint(abs(n))\n"},
		{"a dict slot", "d = {}\nd[\"k\"] = -9\nn = d[\"k\"]\nprint(abs(n))\n"},
		{"the loop variable", "xs = []\nxs.append(-5)\nfor v in xs:\n    print(abs(v))\n"},
		{"a text slot raises", "xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(abs(n))\n"},
		{"a None slot raises", "xs = []\nxs.append(None)\nn = xs[0]\nprint(abs(n))\n"},
		{"a list slot raises", "xs = []\nxs.append([1])\nn = xs[0]\nprint(abs(n))\n"},
		{"a dict slot raises", "xs = []\nxs.append({\"k\": 1})\nn = xs[0]\nprint(abs(n))\n"},
		{"a set slot raises", "xs = []\nxs.append({1})\nn = xs[0]\nprint(abs(n))\n"},
		{"the minus keeps its own sentence", "xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(-n)\n"},
		{"a caught raise runs the program's arm", "xs = []\nxs.append(\"a\")\nn = xs[0]\ntry:\n    print(abs(n))\nexcept TypeError:\n    print(\"caught\")\n"},
		{"abs of a literal", "print(abs(-8))\n"},
		{"abs of a plain variable", "x = -7\nprint(abs(x))\n"},
		// The two rows that used to sit in the refusal table below: the double door still cannot read a pair
		// (the six rows that stayed there), but a signless call *inside* a fold's argument can be lifted, and
		// so these two programs answer now.
		{"a min argument", absCLISlot + "print(min(abs(n), 3))\n"},
		{"a sum element", absCLISlot + "print(sum([abs(n)]))\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "pairabs.gy", tc.src)
			ref, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Skipf("the reference could not answer this program: %s", tc.src)
			}
			got, code := cliRunMerged(t, "--file", path)
			if code == 1 && refusesHonestly(got) {
				requireReferenceTrapOrHonestRefusal(t, tc.src, "", got, code,
					"roadmap L11.1 (the tagged value word, at the signless call) and Gap R.146",
					"the operand's kind is a run-time fact and this position kept one word for it")
				return
			}
			if code == 2 {
				t.Fatalf("exit 2 — LLVM rejected the module gusty emitted (ADR 0166):\n%s", got)
			}
			if code != 0 && code != 1 {
				t.Fatalf("the compiled run exited %d:\n reference: %q\n compiled: %q", code, ref, got)
			}
			requireReferenceAgreement(t, tc.src, ReferenceAgreement{
				Python: ref, Compiled: got, Code: code,
			}, "roadmap L11.1 (the tagged value word, at the signless call)",
				"a name bound from a container slot handed to abs")
		})
	}
}

// TestCLIAbsStillRefusesInWordsThatNameTheOrigin keeps the signless call's refusals honest: each names the
// value it cannot read, the missing half, and who owes it — and none of them spends the contract's forbidden
// exit 2. One of these rows is a compiler crash this cycle found and closed: `round(abs(n) / 2)` had the
// double door lift its own pair-shaped sibling, and the two doors recursed until the process ran out of
// stack; the position refuses now (ADR 0166's rule about who owns exit 2).
func TestCLIAbsStillRefusesInWordsThatNameTheOrigin(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src string }{
		{"an abs operand of a sum", absCLISlot + "print(abs(n) + 1)\n"},
		{"an abs operand of a product", absCLISlot + "print(abs(n) * 2)\n"},
		{"an abs operand of a modulo", absCLISlot + "print(abs(n) % 3)\n"},
		{"an abs operand of a true quotient", absCLISlot + "print(round(abs(n) / 2))\n"},
		{"abs of a negation", absCLISlot + "print(abs(-n))\n"},
		{"two abs answers added", absCLISlot + "m = xs[0]\nprint(abs(n) + abs(m))\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "pairabs_refuse.gy", tc.src)
			out, code := cliRunMerged(t, "--file", path)
			if code == 2 {
				t.Fatalf("exit 2 — LLVM rejected the module gusty emitted (ADR 0166):\n%s", out)
			}
			if code != 1 {
				t.Fatalf("exit %d, want 1 (a program this backend declines to build):\n%s", code, out)
			}
			if !refusesHonestly(out) {
				t.Errorf("the refusal is not actionable:\n%s", out)
			}
			if strings.Contains(out, "loop") {
				t.Errorf("the refusal blames a loop for a program that has none:\n%s", out)
			}
			noteCompiledGap(t, tc.src, out)
		})
	}
}

// TestTheSignlessCallProbeIsOnRecord asks the whole probe program through the record, which is the one
// way a registered conformance program's answer becomes something the suite can check: the GC corpus,
// every `--aot` comparison and the drift ledger all read their expectations from the record, and ADR
// 0302 made a missing entry a failure rather than a skip. A source first asked after the engine was
// retired is recorded from the reference and cross-checked by the matrix (`meta.added_after_0302`) —
// the bytes below are CPython's, and the row's `oracle: match` plus the ledger are what keep that from
// being a self-certifying recording (ADR 0309).
func TestTheSignlessCallProbeIsOnRecord(t *testing.T) {
	src := readProgram(t, "probe_the_signless_call_answers_for_a_pair_bound_name.gy")
	want := "7\n7\n8\n2.5\n2.5\n1\n0\n14\n14\n9\n5\n"
	if !lang.HasGoldenAnswer(src) {
		t.Fatal("the probe the matrix registers has no record — every corpus case that reads expectations " +
			"from the record fails on it, starting with TestGCCorpusCollectsAndAgrees")
	}
	if got := runCompiled(t, src); got != want {
		t.Fatalf("the record does not hold the reference's bytes:\n got %q\nwant %q", got, want)
	}
	res, err := lang.JIT(src, 0)
	if err != nil {
		t.Fatalf("the compiled backend refused a program the reference prints: %v", err)
	}
	if res.Output != want {
		t.Fatalf("the compiled leg prints other bytes than the record's:\n got %q\nwant %q", res.Output, want)
	}
}

package integration

// integration/pair_fstring_test.go — the CLI half of an f-string field asking the tag (roadmap L11.1,
// Gap R.146's rendering positions; ADR 0307).
//
// `pkg/lang/pair_fstring_test.go` checks these programs against the record; this file asks the question a
// record cannot answer for a shape that has never been answered: whether CPython agrees. That question is
// not ceremony here. Every row prints a value whose kind the compiler could not see, through a road whose
// failure mode is a plausible number where the reference has a string — a text field printing its `@str_tab`
// index, a float field printing its box handle — and only the reference can tell `2.5` from `140737488355328`.
//
// The conversion rows are here for the same reason. `f"{x!r}"` used to print NOTHING at exit 0 for any field
// that was not a literal, and a wrong answer with the exit code of success is not counted by
// `compiled refusals this run`, so a suite that only pinned refusals would never have noticed (Gap R.192).

import (
	"strings"
	"testing"
)

const fsCLISlot = "xs = []\nxs.append(7)\nn = xs[0]\n"

// TestCLIAFStringFieldAgreesWithCPython runs the reference and the compiled artifact over the same program;
// a disagreement becomes a debt row rather than an expectation someone grows used to.
func TestCLIAFStringFieldAgreesWithCPython(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src string }{
		{"an int slot", fsCLISlot + "print(f\"{n}\")\n"},
		{"the field inside text", fsCLISlot + "print(f\"[{n}]\")\n"},
		{"the field twice", fsCLISlot + "print(f\"{n}-{n}\")\n"},
		{"a text slot", "sb = []\nsb.append(\"a\")\nn = sb[0]\nprint(f\"{n}\")\n"},
		{"a text slot between literals", "sb = []\nsb.append(\"a\")\nn = sb[0]\nprint(f\"x{n}y\")\n"},
		{"a float slot", "xs = []\nxs.append(2.5)\nn = xs[0]\nprint(f\"{n}\")\n"},
		{"a None slot", "xs = []\nxs.append(None)\nn = xs[0]\nprint(f\"{n}\")\n"},
		{"a bool slot", "xs = []\nxs.append(True)\nn = xs[0]\nprint(f\"{n}\")\n"},
		{"a container slot", "xs = []\nxs.append([1, 2])\nn = xs[0]\nprint(f\"{n}\")\n"},
		{"a dict slot by key", "d = {}\nd[\"k\"] = 9\nn = d[\"k\"]\nprint(f\"{n}\")\n"},
		{"arithmetic over a slot", "xs = []\nxs.append([7, 8])\nn = xs[0][0] * 2\nprint(f\"{n}\")\n"},
		{"arithmetic inside the field", fsCLISlot + "print(f\"{n - 1}\")\n"},
		{"a negation inside the field", fsCLISlot + "print(f\"{-n}\")\n"},
		{"a floored quotient in the field", fsCLISlot + "print(f\"{n // 2}\")\n"},
		{"a true quotient in the field", fsCLISlot + "print(f\"{n / 2}\")\n"},
		{"a verdict in the field", fsCLISlot + "print(f\"{n == 7}\")\n"},
		{"a builtin over the field", fsCLISlot + "print(f\"{round(n / 2)}\")\n"},
		{"the loop variable", "xs = []\nxs.append(5)\nfor v in xs:\n    print(f\"v={v}\")\n"},
		{"repr of a text variable", "s = \"a\"\nprint(f\"{s!r}\")\n"},
		{"str of a text variable", "s = \"a\"\nprint(f\"{s!s}\")\n"},
		{"repr of an int variable", "x = 7\nprint(f\"{x!r}\")\n"},
		{"repr of a float variable", "x = 2.5\nprint(f\"{x!r}\")\n"},
		{"repr of a text slot read", "sb = []\nsb.append(\"a\")\nn = sb[0]\nprint(f\"{n!r}\")\n"},
		{"repr of a text literal field", "print(f\"{'a'!r}\")\n"},
		{"repr inside brackets", "s = \"hi\"\nprint(f\"[{s!r}]\")\n"},
		{"a format spec over a literal", "print(f\"{3.5:.2f}\")\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "pairfstr.gy", tc.src)
			ref, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Skipf("the reference could not answer this program: %s", tc.src)
			}
			got, code := cliRunMerged(t, "--file", path)
			if code == 1 && refusesHonestly(got) {
				requireReferenceTrapOrHonestRefusal(t, tc.src, "", got, code,
					"roadmap L11.1 (the tagged value word, at the f-string field) and Gap R.146",
					"the field's kind is a run-time fact and this position kept one word for it")
				return
			}
			if code == 2 {
				t.Fatalf("exit 2 — LLVM rejected the module gusty emitted (ADR 0166):\n%s", got)
			}
			if code != 0 {
				t.Fatalf("the compiled run exited %d on a program the reference prints:\n reference: %q\n compiled: %q", code, ref, got)
			}
			requireReferenceAgreement(t, tc.src, ReferenceAgreement{
				Python: ref, Compiled: got, Code: code,
			}, "roadmap L11.1 (the tagged value word, at the f-string field)",
				"a name bound from a container slot interpolated into an f-string")
		})
	}
}

// TestCLIAFStringFieldStillRefusesInWordsThatNameTheOrigin keeps the field's refusals honest: each names the
// value it cannot read, the missing half, and who owes it — and never blames a loop the program does not
// contain (Gap R.38's rule), never spends exit 2 (ADR 0166's rule).
func TestCLIAFStringFieldStillRefusesInWordsThatNameTheOrigin(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src string }{
		{"a product the door will not prove", fsCLISlot + "print(f\"{n + 1}\")\n"},
		{"a modulo over a slot", fsCLISlot + "print(f\"{n % 3}\")\n"},
		{"a format spec over a slot", fsCLISlot + "print(f\"{n:>.2f}\")\n"},
		{"an f-string used as a value", fsCLISlot + "s = f\"x{n}\"\nprint(s)\n"},
		{"an f-string concatenated", fsCLISlot + "print(f\"{n}\" + f\"{n}\")\n"},
		{"a method on an f-string", fsCLISlot + "print(f\"v={n}\".upper())\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "pairfstr_refuse.gy", tc.src)
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

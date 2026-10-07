package integration

// integration/pair_container_test.go — the CLI half of a pair-bound name used as a CONTAINER element
// (roadmap L11.1, Gap R.146; ADR 0303's pair binding, ADR 0187's payload-never-without-its-tag).
//
// `pkg/lang/pair_container_test.go` checks these programs against the record the retired engine left; this
// file asks the question a record cannot answer for a shape that has never been answered: whether CPython
// agrees. That question is not ceremony here. Every answer in this cycle is a container printing a slot
// whose kind the compiler could not see, and the failure mode that makes this interesting is the silent one
// — an interned string index or a float box's handle printed as an integer at exit 0 (Gap R.38's family).
// Only the reference can tell `[2.5]` from `[140737488355328]`, so the reference is what each row reads.
//
// A refusal is allowed and is still checked: it has to name the missing capability and the roadmap row that
// owns it, and it may never spend exit 2 (ADR 0166 reserves that class for the compiler's own failures).

import (
	"strings"
	"testing"
)

const pairContainerBuilt = "xs = []\nxs.append(7)\nn = xs[0]\n"

// TestCLIAPairBoundNameInAContainerAgreesWithCPython is the row Gap R.146 named, at the CLI: the reference
// and the compiled artifact run the same program, and a disagreement becomes a debt row rather than an
// expectation someone grows used to.
func TestCLIAPairBoundNameInAContainerAgreesWithCPython(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src string }{
		{"the element the pair road bound", pairContainerBuilt + "print([n])\n"},
		{"the pair first", pairContainerBuilt + "print([n, 1])\n"},
		{"the pair last", pairContainerBuilt + "print([1, n])\n"},
		{"the pair twice", pairContainerBuilt + "print([n, n])\n"},
		{"every kind in one literal", pairContainerBuilt + "print([n, \"x\", 2, None])\n"},
		{"a text slot", "xs = []\nxs.append(\"a\")\nn = xs[0]\nprint([n])\n"},
		{"a float slot", "xs = []\nxs.append(2.5)\nn = xs[0]\nprint([n])\n"},
		{"a None slot", "xs = []\nxs.append(None)\nn = xs[0]\nprint([n])\n"},
		{"a bool slot", "xs = []\nxs.append(True)\nn = xs[0]\nprint([n])\n"},
		{"a container slot, nested", "xs = []\nxs.append([1, 2])\nn = xs[0]\nprint([n])\n"},
		{"two pair-bound names", "xs = []\nxs.append(7)\nxs.append(8)\na = xs[0]\nb = xs[1]\nprint([a, b])\n"},
		{"arithmetic over a slot", "xs = []\nxs.append([7, 8])\nn = xs[0][0] * 2\nprint([n])\n"},
		{"the loop variable", "xs = []\nxs.append(7)\nfor v in xs:\n    print([v])\n"},
		{"a dict slot by key", "d = {}\nd[\"k\"] = 9\nn = d[\"k\"]\nprint([n])\n"},
		{"bound and printed", pairContainerBuilt + "y = [n]\nprint(y)\n"},
		{"bound with a literal element", pairContainerBuilt + "y = [n, 1]\nprint(y)\n"},
		{"bound and measured", pairContainerBuilt + "y = [n]\nprint(len(y))\n"},
		{"bound and subscripted", pairContainerBuilt + "y = [n]\nprint(y[0])\n"},
		{"bound, read back, and used as a number", pairContainerBuilt + "y = [n]\nprint(y[0] + 1)\n"},
		{"bound and asked for membership", pairContainerBuilt + "y = [n]\nprint(7 in y)\n"},
		{"bound and appended to", pairContainerBuilt + "y = [n]\ny.append(8)\nprint(y)\n"},
		{"bound and looped over", pairContainerBuilt + "y = [n]\nfor v in y:\n    print(v)\n"},
		{"bound and rendered by str", pairContainerBuilt + "y = [n]\nprint(str(y))\n"},
		{"a text slot rendered", "xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(str([n]))\n"},
		{"a text slot compared", "xs = []\nxs.append(\"a\")\nn = xs[0]\nprint([n] == [\"a\"])\n"},
		{"a float slot bound and printed", "xs = []\nxs.append(2.5)\nn = xs[0]\ny = [n]\nprint(y)\n"},
		{"repr of the bound list", "xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(repr([n]))\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "paircont.gy", tc.src)
			ref, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Skipf("the reference could not answer this program: %s", tc.src)
			}
			got, code := cliRunMerged(t, "--file", path)
			if code == 1 && refusesHonestly(got) {
				requireReferenceTrapOrHonestRefusal(t, tc.src, "", got, code,
					"roadmap L11.1 (the tagged value word) and Gap R.146 (the positions that keep one word)",
					"the element's kind is a run-time fact and this position kept one word for it")
				return
			}
			if code != 0 {
				t.Fatalf("the compiled run exited %d on a program the reference prints:\n reference: %q\n compiled: %q", code, ref, got)
			}
			requireReferenceAgreement(t, tc.src, ReferenceAgreement{
				Python: ref, Compiled: got, Code: code,
			}, "roadmap L11.1 (the tagged value word, at the container element door)",
				"a name bound from a container slot used as a container element")
		})
	}
}

// TestCLIAPairBoundNameStillRefusesTheContainerPositionsThatKeepOneWord is the honest half at the CLI,
// narrowed by ADR 0310: the dict entry and the set member went through the same door the list element did
// (their builders take the tag as an `i32`, and a register is an `i32`), so what is left of the original table
// is the literal a builtin folds into a static array — an array with no tag storage at all. Each refusal has
// to say so in the words that name the missing capability rather than in three words a person cannot act on.
func TestCLIAPairBoundNameStillRefusesTheContainerPositionsThatKeepOneWord(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src string }{
		{"sum over a literal", pairContainerBuilt + "print(sum([n]))\n"},
		{"min over a literal", pairContainerBuilt + "print(min([n, 3]))\n"},
		{"max over a literal", pairContainerBuilt + "print(max([n, 3]))\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "paircont_refuse.gy", tc.src)
			out, code := cliRunMerged(t, "--file", path)
			if code == 2 {
				t.Fatalf("exit 2 — LLVM rejected the module gusty emitted (ADR 0166):\n%s", out)
			}
			if code != 1 {
				t.Fatalf("exit %d, want 1 (a program this backend declines to build):\n%s", code, out)
			}
			for _, want := range []string{"(payload, tag) pair", "one word", "roadmap L11.1"} {
				if !strings.Contains(out, want) {
					t.Errorf("refusal does not name the missing half (%q):\n%s", want, out)
				}
			}
			noteCompiledGap(t, tc.src, out)
		})
	}
}

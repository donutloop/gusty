package integration

// integration/str_pair_test.go — the CLI half of `str(v)` / `repr(v)` over a value whose kind the
// program decides: the compiled artifact's bytes against CPython's, in both directions of the pair, with
// the trap classes and the refusal contract kept separate (roadmap L11.1 / L11.2, Gap R.171 / Gap R.146).
//
// `pkg/lang/str_pair_test.go` checks the same programs against the retired engine's record; this file
// asks the thing a record cannot answer, which is whether the reference agrees. That distinction is the
// point of ADR 0302's two ledgers: the record keeps the language's answers measurable, and CPython is
// still the only thing that defines them. Where the two disagree, the reference wins and
// `testdata/cpython-debt.json` holds the row.
//
// The rendering runs through the module's one tag-reading printer (`rt_str_of_value` →
// `rt_print_mixed_value` with the output pointed at a capture buffer), which is why `print(v)`, `str(v)`
// and `repr(v)` cannot drift apart on one value — the failure mode ADR 0258 was written for, when two
// number formatters disagreed about a container and `str([1, 2])` answered `0`.

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// TestCLIStrAndReprOfARuntimeValueAnswerLikeCPython is the row: each shape is run by the reference and by
// the compiled artifact in the same case, and a disagreement is not allowed to become an expectation —
// it goes on the reference-debt ledger with the roadmap row that owns it.
func TestCLIStrAndReprOfARuntimeValueAnswerLikeCPython(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src string }{
		{"int slot, str", "xs = []\nxs.append(3)\nprint(str(xs[0]))\n"},
		{"text slot, str", "xs = []\nxs.append(\"a\")\nprint(str(xs[0]))\n"},
		{"text slot, repr", "xs = []\nxs.append(\"a\")\nprint(repr(xs[0]))\n"},
		{"float slot keeps its float", "xs = []\nxs.append(2.5)\nprint(str(xs[0]))\n"},
		{"None slot", "xs = []\nxs.append(None)\nprint(str(xs[0]))\n"},
		{"bool slot", "xs = []\nxs.append(True)\nprint(str(xs[0]), repr(xs[0]))\n"},
		{"container slot renders itself", "xs = []\nxs.append([1, 2])\nprint(str(xs[0]))\n"},
		{"dict slot renders itself", "xs = []\nxs.append({\"k\": 5})\nprint(str(xs[0]))\n"},
		{
			"four kinds in one container, both halves",
			"xs = []\nxs.append(3)\nxs.append(\"a\")\nxs.append([1, 2])\nxs.append(None)\n" +
				"print(str(xs[0]), str(xs[1]), str(xs[2]), str(xs[3]))\n" +
				"print(repr(xs[0]), repr(xs[1]), repr(xs[2]), repr(xs[3]))\n",
		},
		{"computed position", "xs = []\nxs.append(7)\nxs.append(\"b\")\ni = 1\nprint(str(xs[i]))\n"},
		{"two levels down", "xs = []\nxs.append([4, \"c\"])\nprint(str(xs[0][1]))\n"},
		{"dict value by key", "d = {}\nd[\"k\"] = \"v\"\nprint(str(d[\"k\"]), repr(d[\"k\"]))\n"},
		{"a name bound from a slot", "xs = []\nxs.append(3)\nxs.append(\"a\")\nn = xs[1]\nprint(str(n), repr(n))\n"},
		{"loop variable", "xs = []\nxs.append(1)\nxs.append(\"b\")\nfor v in xs:\n    print(str(v))\n"},
		{"the answer is usable as text", "xs = []\nxs.append(\"a\")\ns = str(xs[0])\nprint(s + \"b\")\n"},
		{"nested in a container", "xs = []\nxs.append(3)\nys = [str(xs[0])]\nprint(ys)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "strpair.gy", tc.src)
			ref, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Skipf("the reference could not answer this program: %s", tc.src)
			}
			got, code := cliRunMerged(t, "--file", path)
			if code == 1 && refusesHonestly(got) {
				// Refusing is the honest alternative to answering wrongly, but only when the sentence
				// names the missing door — and it is a gap, counted, not an answer.
				requireReferenceTrapOrHonestRefusal(t, tc.src, "", got, code,
					"roadmap L11.1 (the tagged value word, at the str/repr door) and L11.2 (the renderer)",
					"the value's kind is a run-time fact and this position kept one word for it")
				return
			}
			if code != 0 {
				t.Fatalf("the compiled run exited %d on a program the reference prints:\n reference: %q\n compiled: %q", code, ref, got)
			}
			requireReferenceAgreement(t, tc.src, ReferenceAgreement{
				Python: ref, Compiled: got, Code: code,
			}, "roadmap L11.1 (the tagged value word, at the str/repr door)",
				"str()/repr() of a slot read, a slot-bound name, or a loop element")
		})
	}
}

// TestCLIStrOfASlotInheritsTheTrapsTheReadCarries: the same door also has to keep the traps the read
// itself raises — an out-of-range position and a missing key — rather than turn them into a rendering
// failure or an empty string.
func TestCLIStrOfASlotInheritsTheTrapsTheReadCarries(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ src, trap string }{
		{"xs = [1]\nprint(str(xs[5]))\n", "IndexError: index out of range"},
		{"d = {}\nprint(str(d[\"z\"]))\n", "KeyError: 'z'"},
	} {
		path := writeSrc(t, dir, "strpair_trap.gy", tc.src)
		got, code := cliRunMerged(t, "--file", path)
		requireReferenceTrapOrHonestRefusal(t, tc.src, tc.trap, got, code,
			"roadmap L11.1 (the tagged value word) and Gap R.189 (naming the key)",
			"the read behind the rendering is a run-time question")
	}
}

// TestCLIStrOfASlotNeverEscapesAsAnExitTwo is the ADR 0166 half: none of these shapes may spend the
// toolchain-rejection class, which is the one class that blames the compiler for a program.
func TestCLIStrOfASlotNeverEscapesAsAnExitTwo(t *testing.T) {
	dir := t.TempDir()
	for _, src := range []string{
		"xs = []\nxs.append(3)\nprint(str(xs[0]))\n",
		"xs = []\nxs.append(\"a\")\nprint(repr(xs[0]))\n",
		"xs = []\nxs.append(3)\nn = xs[0]\nprint(str(n))\n",
		"xs = []\nxs.append(3)\nn = xs[0]\nprint(n + 1)\n",
		"d = {}\nd[\"k\"] = \"v\"\nprint(repr(d[\"k\"]))\n",
	} {
		path := writeSrc(t, dir, "noexit2.gy", src)
		if out, code := cliRunCode(t, "--file", path); code == 2 {
			t.Errorf("exit 2 — LLVM rejected the module gusty emitted (ADR 0166):\n%s", out)
		}
	}
}

// TestCompiledStrOfASlotCarriesOnePrinterNotTwo is the module-shape half through the CLI: the answer is
// rendered by the tag-reading door. A module that answers these shapes while carrying a second renderer is
// the shape ADR 0258 exists to keep dead.
func TestCompiledStrOfASlotCarriesOnePrinterNotTwo(t *testing.T) {
	src := "xs = []\nxs.append(3)\nxs.append(\"a\")\nprint(str(xs[1]), repr(xs[1]))\n"
	res, err := lang.Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, want := range []string{"@rt_str_of_value", "@rt_print_mixed_value"} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("the module does not render the slot through the shared printer (%s missing)", want)
		}
	}
	if !strings.Contains(res.IR, "rt_tag_of") && !strings.Contains(res.IR, "_tag") {
		t.Errorf("the module read a slot without its tag, so the printer is guessing the kind")
	}
}

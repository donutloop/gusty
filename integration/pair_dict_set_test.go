package integration

// integration/pair_dict_set_test.go — the CLI half of a pair-bound name used as a DICT ENTRY and a SET
// MEMBER (roadmap L11.1, Gap R.146's positions that keep one word for a whole value; ADR 0310, ADR 0306's
// list element, ADR 0232's tagged dict/set slots).
//
// `pkg/lang/pair_dict_set_test.go` checks these programs against the record the retired engine left; this
// file asks the question a record cannot answer for a shape it has never seen — whether CPython agrees. For
// this cycle that question is not ceremony: every answer is a container printing a key, a value or a member
// whose kind the compiler could not see, and the failure mode that makes the row interesting is the silent
// one. A text slot's payload is its `@str_tab` index and a float slot's is its box handle, so only the
// reference can tell `{'k': 'a'}` from `{'k': 0}` and `{2.5}` from `{140737488355328}` — at the exit code of
// success, which `compiled refusals this run` never counts.
//
// The raise rows are the second reason this file exists beside the pkg one. A dict key and a set member are
// the two container positions that have to answer *can this value be a key*, and with the kind in a register
// only the run time can answer it; the reference raises `TypeError: unhashable type: 'list'` and the
// compiled leg has to raise the same sentence, at the trap exit, catchable by the program's own
// `except TypeError:` (ADR 0166's exit classes, ADR 0228's typed raise). A refusal is allowed too, and has
// to name the missing half and the row that owes it.

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

const pairDictCLISlot = "xs = []\nxs.append(7)\nn = xs[0]\n"

// TestCLIAgentPairBoundNameInADictOrSetAgreesWithCPython runs the reference and the compiled artifact over
// the same program: a dict entry on either side of the pair, a set member, and every position that reads the
// container back.
func TestCLIAgentPairBoundNameInADictOrSetAgreesWithCPython(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src string }{
		// the dict entry
		{"a value from an int slot", pairDictCLISlot + "print({\"k\": n})\n"},
		{"a value from a text slot", "xs = []\nxs.append(\"a\")\nn = xs[0]\nprint({\"k\": n})\n"},
		{"a value from a float slot", "xs = []\nxs.append(2.5)\nn = xs[0]\nprint({\"k\": n})\n"},
		{"a value from a None slot", "xs = []\nxs.append(None)\nn = xs[0]\nprint({\"k\": n})\n"},
		{"a value from a verdict slot", "xs = []\nxs.append(True)\nn = xs[0]\nprint({\"k\": n})\n"},
		{"a value from a container slot", "xs = []\nxs.append([1, 2])\nn = xs[0]\nprint({\"k\": n})\n"},
		{"a value the arithmetic over a slot produced", "xs = []\nxs.append([7, 8])\nn = xs[0][0] * 2\nprint({\"k\": n})\n"},
		{"a value read out of a dict by key", "d = {}\nd[\"k\"] = 9\nn = d[\"k\"]\nprint({\"v\": n})\n"},
		{"a key from an int slot", pairDictCLISlot + "print({n: \"v\"})\n"},
		{"a key from a text slot", "xs = []\nxs.append(\"a\")\nn = xs[0]\nprint({n: 1})\n"},
		{"a key from a float slot", "xs = []\nxs.append(2.5)\nn = xs[0]\nprint({n: 1})\n"},
		{"a key from a None slot", "xs = []\nxs.append(None)\nn = xs[0]\nprint({n: 1})\n"},
		{"a key from a verdict slot", "xs = []\nxs.append(True)\nn = xs[0]\nprint({n: 1})\n"},
		{"key and value are both pairs", "xs = []\nxs.append(7)\nxs.append(8)\na = xs[0]\nb = xs[1]\nprint({a: b})\n"},
		{"the pair beside literal entries", pairDictCLISlot + "print({\"a\": n, \"b\": 2})\n"},
		{"a value that is a list holding the pair", pairDictCLISlot + "print({\"k\": [n]})\n"},
		{"a list element that is a dict holding the pair", pairDictCLISlot + "print([{\"k\": n}])\n"},
		{"a dict inside a dict", pairDictCLISlot + "print({\"a\": {\"b\": n}})\n"},
		{"the loop variable as a value", "xs = []\nxs.append(7)\nfor v in xs:\n    print({\"k\": v})\n"},
		{"the loop variable as a key", "xs = []\nxs.append(7)\nfor v in xs:\n    print({v: \"x\"})\n"},
		{"a dict literal handed to a function", pairDictCLISlot + "def show(d):\n    print(d)\nshow({\"k\": n})\n"},
		// the dict bound to a name, and everything that reads it back
		{"bound and printed", pairDictCLISlot + "d2 = {\"k\": n}\nprint(d2)\n"},
		{"bound and measured", pairDictCLISlot + "d2 = {\"k\": n}\nprint(len(d2))\n"},
		{"bound and looked up", pairDictCLISlot + "d2 = {\"k\": n}\nprint(d2[\"k\"])\n"},
		{"looked up and used as a number", pairDictCLISlot + "d2 = {\"k\": n}\nprint(d2[\"k\"] + 1)\n"},
		{"a pair key asked for its membership", pairDictCLISlot + "d2 = {n: \"v\"}\nprint(n in d2)\n"},
		{"looked up through the pair it was keyed by", pairDictCLISlot + "d2 = {n: \"v\"}\nprint(d2[n])\n"},
		{"a pair key beside a literal key", pairDictCLISlot + "d2 = {n: \"v\", 1: \"w\"}\nprint(d2[7])\n"},
		{"bound from a text slot and rendered by str", "xs = []\nxs.append(\"a\")\nn = xs[0]\nd2 = {\"k\": n}\nprint(str(d2))\n"},
		{"rendered by repr", "xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(repr({\"k\": n}))\n"},
		{"compared with a literal dict", "xs = []\nxs.append(\"a\")\nn = xs[0]\nprint({\"k\": n} == {\"k\": \"a\"})\n"},
		{"compared unequal", "xs = []\nxs.append(\"a\")\nn = xs[0]\nprint({\"k\": n} != {\"k\": \"b\"})\n"},
		{"keys looped over", pairDictCLISlot + "d2 = {n: \"v\"}\nfor k in d2:\n    print(k)\n"},
		{"the value read back inside the key loop", pairDictCLISlot + "d2 = {\"k\": n}\nfor k in d2:\n    print(k, d2[k])\n"},
		{"grown by a literal key after binding", pairDictCLISlot + "d2 = {\"k\": n}\nd2[\"j\"] = 2\nprint(d2)\n"},
		// the set member
		{"a member from an int slot", pairDictCLISlot + "print({n})\n"},
		{"a member from a text slot", "xs = []\nxs.append(\"a\")\nn = xs[0]\nprint({n})\n"},
		{"a member from a float slot", "xs = []\nxs.append(2.5)\nn = xs[0]\nprint({n})\n"},
		{"a member from a None slot", "xs = []\nxs.append(None)\nn = xs[0]\nprint({n})\n"},
		{"a member from a verdict slot", "xs = []\nxs.append(True)\nn = xs[0]\nprint({n})\n"},
		{"the loop variable as a member", "xs = []\nxs.append(7)\nfor v in xs:\n    print({v})\n"},
		{"two equal pairs are one member", pairDictCLISlot + "xs.append(7)\nm = xs[1]\nprint({n, m})\n"},
		{"a bound set printed", pairDictCLISlot + "s2 = {n}\nprint(s2)\n"},
		{"a bound set measured", pairDictCLISlot + "s2 = {n}\nprint(len(s2))\n"},
		{"a bound set asked for membership", pairDictCLISlot + "s2 = {n}\nprint(7 in s2)\n"},
		{"a bound text set asked for membership", "xs = []\nxs.append(\"a\")\nn = xs[0]\ns2 = {n}\nprint(\"a\" in s2)\n"},
		{"a bound set added to", pairDictCLISlot + "s2 = {n}\ns2.add(7)\nprint(len(s2))\n"},
		{"a bound set looped over", pairDictCLISlot + "s2 = {n}\nfor v in s2:\n    print(v)\n"},
		{"a set rendered by str", "xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(str({n}))\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "pairdict.gy", tc.src)
			ref, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Skipf("the reference could not answer this program: %s", tc.src)
			}
			got, code := cliRunMerged(t, "--file", path)
			if code == 1 && refusesHonestly(got) {
				requireReferenceTrapOrHonestRefusal(t, tc.src, "", got, code,
					"roadmap L11.1 (the tagged value word) and Gap R.146 (the positions that keep one word)",
					"the entry's kind is a run-time fact and this position kept one word for it")
				return
			}
			if code != 0 {
				t.Fatalf("the compiled run exited %d on a program the reference prints:\n reference: %q\n compiled: %q", code, ref, got)
			}
			requireReferenceAgreement(t, tc.src, ReferenceAgreement{
				Python: ref, Compiled: got, Code: code,
			}, "roadmap L11.1 (the tagged value word, at the dict entry and the set member)",
				"a name bound from a container slot stored as a dict key, a dict value or a set member")
		})
	}
}

// TestCLIAgentPairBoundKeyOrMemberRaisesWhatCPythonRaises is the hashing half, at the CLI: the reference
// raises for a key or a member that cannot be one, and the compiled leg raises the same sentence, at the
// trap exit, never at the exit code of success and never as the compiler's own exit 2.
func TestCLIAgentPairBoundKeyOrMemberRaisesWhatCPythonRaises(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src, sentence string }{
		{
			"a list slot cannot be a key",
			"xs = []\nxs.append([1, 2])\nn = xs[0]\nprint({n: 1})\n",
			"TypeError: unhashable type: 'list'",
		},
		{
			"a dict slot cannot be a key",
			"xs = []\nxs.append({\"a\": 1})\nn = xs[0]\nprint({n: 1})\n",
			"TypeError: unhashable type: 'dict'",
		},
		{
			"a set slot cannot be a key",
			"xs = []\nxs.append({1})\nn = xs[0]\nprint({n: 1})\n",
			"TypeError: unhashable type: 'set'",
		},
		{
			"a list slot cannot be a member",
			"xs = []\nxs.append([1, 2])\nn = xs[0]\nprint({n})\n",
			"TypeError: unhashable type: 'list'",
		},
		{
			"a dict slot cannot be a member",
			"xs = []\nxs.append({\"a\": 1})\nn = xs[0]\nprint({n})\n",
			"TypeError: unhashable type: 'dict'",
		},
		{
			"a set slot cannot be a member",
			"xs = []\nxs.append({1})\nn = xs[0]\nprint({n})\n",
			"TypeError: unhashable type: 'set'",
		},
		{
			"a bound dict raises for its unhashable key",
			"xs = []\nxs.append([1, 2])\nn = xs[0]\nd2 = {n: 1}\nprint(d2)\n",
			"TypeError: unhashable type: 'list'",
		},
		{
			"a bound set raises for its unhashable member",
			"xs = []\nxs.append([1, 2])\nn = xs[0]\ns2 = {n}\nprint(s2)\n",
			"TypeError: unhashable type: 'list'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "pairdict_trap.gy", tc.src)
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

// TestCLIAgentUnhashablePairRaiseIsCaughtAtTheCLI is the trap's machine-visible half: an uncaught trap exits
// 3 and a caught one runs the program's arm at exit 0, which is the difference between a typed raise and a
// message printed on the way to an arbitrary exit.
func TestCLIAgentUnhashablePairRaiseIsCaughtAtTheCLI(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src, want string }{
		{
			"a caught key raise runs the arm",
			"xs = []\nxs.append([1, 2])\nn = xs[0]\ntry:\n    print({n: 1})\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"a caught member raise runs the arm",
			"xs = []\nxs.append([1, 2])\nn = xs[0]\ntry:\n    print({n})\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "pairdict_catch.gy", tc.src)
			ref, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok || ref != tc.want {
				t.Fatalf("the reference said %q (ok %v), want %q", ref, ok, tc.want)
			}
			out, code := cliRunMerged(t, "--file", path)
			if code != 0 {
				t.Fatalf("exit %d, want 0 — the arm did not run:\n%s", code, out)
			}
			if out != tc.want {
				t.Errorf("stdout %q, want %q", out, tc.want)
			}
		})
	}
}

// TestThePairBoundDictAndSetProbeIsOnRecord asks the whole probe program through the record, which is the one
// way a registered conformance program's answer becomes something the suite can check: the GC corpus, every
// compiled comparison and the drift ledger all read their expectations from the record, and ADR 0302 made a
// missing entry a failure rather than a skip. A source first asked after the engine was retired is recorded
// from the reference and cross-checked by the matrix (`meta.added_after_0302`) — the bytes below are
// CPython's, and the row's `oracle: match` plus the ledger are what keep that from being a self-certifying
// recording (ADR 0310, ADR 0306's precedent).
func TestThePairBoundDictAndSetProbeIsOnRecord(t *testing.T) {
	src := readProgram(t, "probe_pair_bound_dict_entry_and_set_member.gy")
	want := "{'v': 7}\n{'v': 'a'}\n{'v': 2.5}\n{'v': None}\n{'v': True}\n{'v': [1, 2]}\n{7: 'k'}\n{'a': 1}\n{2.5: 1}\n{None: 1}\n{7: 7}\n{'a': 7, 'b': 2}\n{'k': 7}\n1\n7\n8\nTrue\nv\n7\n{7}\n{'a'}\n{2.5}\n{None}\n{True}\n1\nTrue\nTrue\n{'pair': [7, 'a', 2.5], 'nested': {'inner': 'a'}}\ncaught-the-list-key\ncaught-the-list-member\n"
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

// TestCLIAgentPairBoundDictAndSetStillRefuseInWordsThatNameTheOrigin is the honest half at the CLI. Three
// classes keep their refusal: a pair handed across a call into a body whose parameter is not a pair, a value
// that is not a pair at all (`d["k"] = xs[0] / 2`), which the ordinary road is asked about first and refuses
// in words, and a fold the door declines to label — a set literal, whose element set and order the source
// does not fix, or a container literal as an operand (Gap R.198). The three rows that used to open this
// table — `sum([n])`, `min([n, 3])`, `max([n, 3])` — answer since ADR 0316 and live in
// `pair_fold_test.go`; the MUTATION roads themselves — `xs.append(n)`, `s.add(n)`, `xs[i] = v`,
// `d[k] = v` — are paid by ADR 0311 and compared with the reference in `pair_mutation_test.go`.
// Each refusal names the value's origin, the missing half and the roadmap row; exit 2 stays forbidden.
func TestCLIAgentPairBoundDictAndSetStillRefuseInWordsThatNameTheOrigin(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name, src string
		must      []string
	}{
		{"a pair handed through a parameter into a dict", pairDictCLISlot + "def build(k):\n    return {\"k\": k}\nprint(build(n))\n", nil},
		{"a fold over a set literal", pairDictCLISlot + "print(min({n, 3}))\n", nil},
		// A container literal is an operand the fold door will not label; the sentence is the ordinary sum
		// road's own, and it names what the element IS because there the kind is a fact the compiler can see
		// (Gap R.198, ADR 0316).
		{"a container literal as a fold operand", pairDictCLISlot + "print(sum([n, [1]]))\n",
			[]string{"sum adds numbers", "list literal is a container", "Python raises TypeError"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "pairdict_refuse.gy", tc.src)
			out, code := cliRunMerged(t, "--file", path)
			if code == 2 {
				t.Fatalf("exit 2 — LLVM rejected the module gusty emitted (ADR 0166):\n%s", out)
			}
			if code != 1 {
				t.Fatalf("exit %d, want 1 (a program this backend declines to build):\n%s", code, out)
			}
			wants := tc.must
			if wants == nil {
				wants = []string{"(payload, tag) pair", "one word", "roadmap L11.1"}
			}
			for _, want := range wants {
				if !strings.Contains(out, want) {
					t.Errorf("the refusal does not name the missing half (%q):\n%s", want, out)
				}
			}
			noteCompiledGap(t, tc.src, out)
		})
	}
}

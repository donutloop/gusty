// Roadmap Gap R.288 / ADR 0437 — a refusal names the rule it is missing, and never offers a way to run
// that this toolchain retired.
//
//	d = {[1]: "x"}
//	print(d[[1]])     # TypeError: unhashable type: 'list'      the reference
//	                  # refused, in words — and the words ended "…or run it interpreted"
//
// The declining was honest: a compiled dict cannot be keyed by a container, because an entry filed under
// a container is filed under the handle the allocator handed out, and the rule that would ask two
// containers with the same elements whether they are the same key — a hash — is the half this backend
// does not have. The sentence beside it was not: `ADR 0302` deleted the AST interpreter, the `--interp`
// flag's answer is a usage error, and an agent reading a refusal and told to "run it interpreted" goes
// looking for something that is not there. It was also unguardable: the witness guard reads the tests, the
// CLI, the conformance registry and the agent-facing documents, and these strings are emitted by
// `pkg/lang` (Gap R.193 named exactly that hole).
//
// Two rulings, and each door now says which one it is standing in:
//
//   - the KEY door — `a compiled container cannot hold %s as a key yet: a dict finds an entry by hashing
//     its key, and this backend has no hashing rule for a container …`; and
//   - the ELEMENT door — `a compiled container cannot hold %s yet: a slot of a compiled container carries
//     one word and the tag that says what that word is, and this element nests deeper than any tag the
//     compiler writes …`.
//
// The element door walks into what it is declining, so an element whose trouble is a dict inside it is
// refused for want of the hash and not for want of a tag (`print([{{[1]: 2}}])` names `[1]`), which is the
// difference between two true sentences and the one the reader can act on (**ADR 0249**, **ADR 0232**).
//
// What this file pins:
//
//   - the key door's sentence at every key spelling that has no hash — a list, a dict, a set, an
//     unhashable tuple, the empty container spelled as a call, a dict buried in an element, and the same
//     shape through a comprehension;
//   - the element door's sentence at the shapes that reach it, and that the door is not simply the key
//     door wearing a hat (the two sentences are different, and each row asserts its own);
//   - what no refusal may say again — an offer to run the program the retired way, the retired flag, the
//     retired entry points, and the compiler's own type names;
//   - that the door did not widen: every container key that HAS a hash still answers, and the nested
//     elements that answered before still answer, on the compiled leg, against the record, and against
//     CPython at the CLI (`integration/refusal_names_the_missing_rule_test.go`).
package lang

import (
	"regexp"
	"strings"
	"testing"
)

// The phrases a compiled refusal may never contain are DATA, not code, for the reason the witness guard
// gives for its own list: a restatement in the source would be eaten by the guard that enforces them, and a
// test that quotes a banned phrase to forbid it has quietly become the thing it tests. They live in
// testdata/witness-emitter-banned-phrases.txt (the offers to run this toolchain retired) and
// testdata/witness-banned-phrases.txt (the engine claims), and the same two lists are what
// `witness_emitter_test.go` asks of every string literal in `pkg/lang` and `cmd/gustyc`. One list, asked at
// two moments: when the sentence is compiled, and when the tree is tested.
func refusalBannedPatterns(t *testing.T) []*regexp.Regexp {
	t.Helper()
	patterns, _, _ := witnessEmitterLists(t)
	return patterns
}

// assertRefusalSentence compiles a program that must be refused, and asks its sentence the three
// questions ADR 0249 and ADR 0437 settle: does it name the rule that is missing, does it name the row that
// owns it, and does it keep its mouth shut about everything else.
func assertRefusalSentence(t *testing.T, src, wantSub, row string) string {
	t.Helper()
	out, refused := compiledOutOrRefusal(t, src)
	if !refused {
		t.Fatalf("%q answered %q at exit 0, where the backend must refuse: %s\nsrc: %s", src, out, row, src)
	}
	if !strings.Contains(out, wantSub) {
		t.Errorf("%q refused without naming %q (%s):\n%s", src, wantSub, row, out)
	}
	if !strings.Contains(out, "roadmap") {
		t.Errorf("%q refused without naming the roadmap row that owns the missing half (%s):\n%s", src, row, out)
	}
	for _, re := range refusalBannedPatterns(t) {
		if re.MatchString(out) {
			t.Errorf("%q refused with a sentence that promises an engine this toolchain retired (%s):\n%s", src, re.String(), out)
		}
	}
	// The emitter's own machinery in a reader's sentence is the same class of defect (Gap R.232), and the
	// doubled prefix is what this row found at the comprehension road: a sentence about the toolchain that
	// is not true.
	for _, leak := range []string{"*lang.", "%!s(", "codegen: codegen:"} {
		if strings.Contains(out, leak) {
			t.Errorf("%q refused by naming the compiler's own machinery (%q) — %s:\n%s", src, leak, row, out)
		}
	}
	return out
}

// keyDoorRows are the dict keys that have no hash. Each was read off CPython beside being written here:
// every one of them is `TypeError: unhashable type: '<inner>'` there, and a refusal here, and the only
// question this row decides is whether the refusal says WHICH rule it is missing.
var keyDoorRows = []struct {
	name   string
	src    string
	says   string // the fragment of the sentence that names the hashing rule
	quotes string
}{
	{
		name:   "the row's own program, an untaggable key",
		src:    "d = {[1]: \"x\"}\nprint(d[[1]])\n",
		says:   "as a key yet: a dict finds an entry by hashing its key",
		quotes: "a list literal",
	},
	{
		name:   "a literal dict under a list key",
		src:    "print({[1, 2]: 3})\n",
		says:   "no hashing rule for a container",
		quotes: "a list literal",
	},
	{
		name:   "a dict key that is a dict",
		src:    "print({{1: 2}: 3})\n",
		says:   "no hashing rule for a container",
		quotes: "a dict literal",
	},
	{
		name:   "a dict key that is a set",
		src:    "print({{1}: 3})\n",
		says:   "no hashing rule for a container",
		quotes: "a set literal",
	},
	{
		name:   "the empty container spelled as a call",
		src:    "print({set(): 1})\n",
		says:   "no hashing rule for a container",
		quotes: "an empty set",
	},
	{
		name:   "a list spelled as a call",
		src:    "print({list(): 1})\n",
		says:   "no hashing rule for a container",
		quotes: "an empty list",
	},
	{
		name:   "a dict spelled as a call",
		src:    "print({dict(): 1})\n",
		says:   "no hashing rule for a container",
		quotes: "an empty dict",
	},
	{
		name:   "an unhashable tuple inherits its element's lack of a hash",
		src:    "print({(1, [2]): 3})\n",
		says:   "no hashing rule for a container",
		quotes: "a tuple literal",
	},
	{
		name:   "a container key one level inside a value",
		src:    "d = {\"a\": {[1]: 2}}\nprint(d)\n",
		says:   "no hashing rule for a container",
		quotes: "a list literal",
	},
	{
		name:   "a container key inside a container element",
		src:    "print([{{[1]: 2}} for x in range(1)])\n",
		says:   "no hashing rule for a container",
		quotes: "a list literal",
	},
	{
		name:   "a container key carried by a comprehension's element",
		src:    "print([{[1]: 2} for x in range(1)])\n",
		says:   "no hashing rule for a container",
		quotes: "a list literal",
	},
	{
		name:   "a container key inside a set comprehension's element",
		src:    "print({{[1]: 2} for x in range(1)})\n",
		says:   "no hashing rule for a container",
		quotes: "a list literal",
	},
}

func TestAKeyWithNoHashNamesTheHashingRuleItMisses(t *testing.T) {
	t.Parallel()
	for _, tc := range keyDoorRows {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := assertRefusalSentence(t, tc.src, tc.says, "Gap R.288, ADR 0437")
			if !strings.Contains(out, tc.quotes) {
				t.Errorf("%q refused without quoting the key it declined (%q) — the sentence must name the object the reader has to change (%s):\n%s", tc.src, tc.quotes, "Gap R.288", out)
			}
			if strings.Contains(out, "a slot of a compiled container carries one word") {
				t.Errorf("%q was refused by the ELEMENT door although the missing rule is the key's hash (%s):\n%s", tc.src, "Gap R.288", out)
			}
		})
	}
}

// elementDoorRows are the elements that nest deeper than any tag the compiler writes. They are the sibling
// shape, and they must not be refused with the key's sentence: the trouble is a slot that would hold a
// handle, not a bucket the entry could never be found in.
var elementDoorRows = []struct {
	name string
	src  string
}{
	{"a slot read that is a list, in a comprehension", "d = {1: [2]}\nprint([[d[1]] for k in d])\n"},
	{"a slot read beside a number", "d = {1: [2]}\nprint([[d[1], 1] for k in d])\n"},
	// A CALL's answer as a comprehension's element left this table with **ADR 0457** (roadmap **Gap R.530**):
	// the whole-program scan knows the callee's answer, so the element road builds the object once and stores
	// the handle beside the callee's kind. It is pinned as an answer below.
	{"a dict slot read, in a comprehension's element", "d = {\"k\": [1, \"a\"]}\nprint([[d[\"k\"]] for x in range(1)])\n"},
}

func TestAnElementThatNestsTooDeepNamesTheTagRuleItMisses(t *testing.T) {
	t.Parallel()
	for _, tc := range elementDoorRows {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := assertRefusalSentence(t, tc.src, "a slot of a compiled container carries one word", "Gap R.288, ADR 0437")
			if !strings.Contains(out, "printed by handle and compared by address") {
				t.Errorf("%q refused without saying what the wrong answer would look like (%s):\n%s", tc.src, "Gap R.288", out)
			}
			if strings.Contains(out, "as a key yet") {
				t.Errorf("%q was refused by the KEY door although nothing here is a key (%s):\n%s", tc.src, "Gap R.288", out)
			}
		})
	}
}

// TestTheContainerDoorsStillSayWhatTheyAnsweredBefore is the control that keeps the door from widening: the
// keys that have a hash, and the elements that were answered, answer exactly as they did — on the compiled
// leg and against the record. `tools/recmerge` put the four sources on the record from the reference, and
// refused the refusals below, which have no answer to record.
func TestTheContainerDoorsStillSayWhatTheyAnsweredBefore(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{"a tuple key has a hash and answers", "d = {(1, 2): \"x\"}\nprint(d[(1, 2)])\n", "x\n"},
		{"a tuple key prints in a dict", "print({(1, 2): 3})\n", "{(1, 2): 3}\n"},
		{"a text key answers", "d = {\"a\": 1}\nprint(d[\"a\"])\n", "1\n"},
		{"a container element one level deep prints", "print([[1, [2]], 1])\n", "[[1, [2]], 1]\n"},
		{"two levels of containers print", "print([[[1, 2], [3]], 1])\n", "[[[1, 2], [3]], 1]\n"},
		{"a dict in a list prints", "print([{\"a\": 1}, 2])\n", "[{'a': 1}, 2]\n"},
		{"a dict whose value is a mixed list prints", "print([[{\"a\": [1]}], 1])\n", "[[{'a': [1]}], 1]\n"},
		{"a comprehension over a literal of containers", "xs = [[1, 2], [3]]\nprint([y for y in xs])\n", "[[1, 2], [3]]\n"},
		// The shape **ADR 0457** took off the element door's list: the element is a call, the call answers a
		// container, and the comprehension's element builds the object once (roadmap **Gap R.530**).
		{"a call's answer, in a comprehension's element", "def f():\n    return [1]\nprint([[f()] for x in range(1)])\n", "[[[1]]]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, tc.src)
			if refused {
				t.Fatalf("%q refused (%s), where the answer is %q — the door was widened: %s", tc.src, out, tc.want, tc.name)
			}
			if out != tc.want {
				t.Errorf("%q printed %q, want %q (%s)", tc.src, out, tc.want, "Gap R.288's control")
			}
			recorded, err := runGoldenStdout(t, tc.src)
			if err != nil {
				t.Fatalf("%q is not on the record: %v — record it from the reference with tools/recmerge", tc.src, err)
			}
			if strings.TrimRight(recorded, "\n") != strings.TrimRight(tc.want, "\n") {
				t.Errorf("%q: the record says %q, this row expects %q", tc.src, recorded, tc.want)
			}
		})
	}
}

// TestTheEmitterSentencesNameNoSecondWayToRun asks the whole refusal family the question the guard asks, but
// through the door that produces the sentence rather than the file that stores it: a program per shape,
// compiled, and the answer checked for the offer ADR 0437 removed.
func TestTheEmitterSentencesNameNoSecondWayToRun(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"d = {[1]: \"x\"}\nprint(d[[1]])\n",
		"print({set(): 1})\n",
		"print({(1, [2]): 3})\n",
		"d = {1: [2]}\nprint([[d[1]] for k in d])\n",
		"print([{[1]: 2} for x in range(1)])\n",
		"print([{{[1]: 2}} for x in range(1)])\n",
	} {
		src := src
		t.Run(strings.ReplaceAll(src, "\n", " / "), func(t *testing.T) {
			t.Parallel()
			_, err := Compile(src)
			if err == nil {
				t.Fatalf("%q compiled where a refusal is owed", src)
			}
			// The sentence the `--json` payload carries is the same string the human reads: one door, one
			// sentence, no second copy for the machine to disagree with (AGENTS.md § the machine path).
			msg := err.Error()
			for _, re := range refusalBannedPatterns(t) {
				if re.MatchString(msg) {
					t.Errorf("%q's refusal promises an engine this toolchain retired (%s):\n%s", src, re.String(), msg)
				}
			}
			if !strings.Contains(msg, "roadmap") {
				t.Errorf("%q's refusal names no roadmap row:\n%s", src, msg)
			}
		})
	}
}

package lang

import (
	"os"
	"strings"
	"testing"
)

// A dict is a key → value mapping, and this is the file that holds the interpreter to it (roadmap
// Gaps R.118 and R.120, ADR 0260).
//
// The interpreter grew a dict by appending: `o.elems = append(o.elems, key)` for the literal, the
// comprehension and the copy alike. A key written twice therefore made two entries, both printed and
// both counted, while the compiled backend — whose fold deduplicates keys and whose runtime
// `rt_dict_put_tagged` updates in place — answered CPython's line all along. That arrangement is the
// reason this file exists: the two backends disagreed, so a parity test could have caught it, and
// none did, because nothing had ever built a dict with a repeated key.
//
// The rules the rows keep, all of them CPython's and taken from `python3` on the same source:
//   - a repeated key takes the new value and keeps the entry's position (`{"a": 1, "b": 2, "a": 3}`
//     is `{'a': 3, 'b': 2}` — the order is first insertion, not last write);
//   - the key that survives is the one written first (`{1: 'a', True: 'b'}` prints `{1: 'b'}`);
//   - 1, True and 1.0 are one key, because a bool and a float compare to the number they are
//     (ADR 0259's `dictKeyEq`), so `{1: 1, True: 2}` is one entry, not two.
func TestADictWithARepeatedKeyIsOneEntry(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		// The plain literal, one key per type the language has.
		{"print({\"a\": 1, \"a\": 2})\n", "{'a': 2}\n"},
		{"print({1: \"a\", 1: \"b\"})\n", "{1: 'b'}\n"},
		{"print({None: 1, None: 2})\n", "{None: 2}\n"},
		// Position is first insertion, not last write — the order the program was written in.
		{"print({\"a\": 1, \"b\": 2, \"a\": 3})\n", "{'a': 3, 'b': 2}\n"},
		// A key whose kind the comparison has to see through: bool and float are the numbers
		// they behave like, so all three of these are one entry.
		{"print({1: 1, True: 2})\n", "{1: 2}\n"},
		{"print({True: 1, 1: 2})\n", "{True: 2}\n"},
		{"print({1.0: \"a\", 1: \"b\"})\n", "{1.0: 'b'}\n"},
		{"print({1: \"a\", 1.0: \"b\"})\n", "{1: 'b'}\n"},
		// What the rest of the program then sees: one slot, the last value, and no ghost to count.
		{"d = {\"a\": 1, \"a\": 2}\nprint(len(d))\n", "1\n"},
		{"d = {\"a\": 1, \"a\": 2}\nprint(d[\"a\"])\n", "2\n"},
		{"d = {\"a\": 1, \"a\": 2}\nprint(\"a\" in d)\n", "True\n"},
		{"d = {1: 1, 1: 2}\nfor k in d:\n    print(k)\n", "1\n"},
		// Item assignment always walked this rule; the literal now walks the same one, so the two
		// ways of building the same dict cannot give two different containers.
		{"d = {\"a\": 1}\nd[\"a\"] = 2\nprint(d, len(d))\n", "{'a': 2} 1\n"},
		{"d = {}\nd[True] = 1\nd[1] = 2\nprint(d, len(d))\n", "{True: 2} 1\n"},
	} {
		name := strings.ReplaceAll(strings.Split(tc.src, "\n")[0], " ", "_")
		if got := captureStdout(t, tc.src); got != tc.want {
			t.Errorf("interpreter %s = %q, want %q", name, got, tc.want)
		}
		code, out := negBuildRun(t, "dict_key_"+name, tc.src)
		if code != 0 {
			t.Errorf("compiled %s exited %d: %s", name, code, out)
			continue
		}
		if out != tc.want {
			t.Errorf("compiled %s = %q, want %q", name, out, tc.want)
		}
	}
}

// The comprehension is the shape that started the row (roadmap Gap R.118): the loop writes the same
// key on two iterations and the dict grew a second entry under it.
func TestADictComprehensionPutsItsEntries(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"d = {1: 2 for x in [1, 2]}\nprint(d)\nprint(len(d))\n", "{1: 2}\n1\n"},
		{"d = {1: 2 for x in [1, 2, 3]}\nprint(d)\n", "{1: 2}\n"},
		{"d = {x: x * 2 for x in [1, 1, 2]}\nprint(d)\nprint(len(d))\n", "{1: 2, 2: 4}\n2\n"},
		{"d = {1: 1 for x in [1]}\nd[1] = 9\nprint(d)\n", "{1: 9}\n"},
		// A filtered comprehension may skip the item that would have written the first value; the
		// entry that survives is the one the loop actually ran.
		{"d = {1: x for x in [1, 2] if x > 1}\nprint(d)\n", "{1: 2}\n"},
	} {
		name := strings.ReplaceAll(strings.Split(tc.src, "\n")[0], " ", "_")
		if got := captureStdout(t, tc.src); got != tc.want {
			t.Errorf("interpreter %s = %q, want %q", name, got, tc.want)
		}
		code, out := negBuildRun(t, "dict_comp_"+name, tc.src)
		if code != 0 {
			t.Errorf("compiled %s exited %d: %s", name, code, out)
			continue
		}
		if out != tc.want {
			t.Errorf("compiled %s = %q, want %q", name, out, tc.want)
		}
	}
}

// The compiled path's dict-comprehension builder walks integer keys, so the text spelling of the same
// comprehension is a refusal it makes in words while the interpreter answers — filed as roadmap Gap
// R.123 rather than skipped, and pinned per leg in the conformance ledger. The int spelling two rows up
// is parity surface; the difference between the two is what the compiled builder can spell into a
// compile-time global, not what a dict means.
func TestADictComprehensionWithATextKeyIsRefusedByCodegenOnly(t *testing.T) {
	const src = "d = {\"k\": v for v in [1, 2, 3]}\nprint(d)\nprint(d[\"k\"], len(d))\n"
	const want = "{'k': 3}\n3 1\n"
	if got := captureStdout(t, src); got != want {
		t.Errorf("interpreter = %q, want %q (ADR 0260 put the entry; the interpreter owes CPython's line)", got, want)
	}
	_, err := Compile(src)
	if err == nil {
		t.Fatalf("the text-key comprehension compiled; the ledger row for Gap R.123 pins a refusal, so a new answer has to move that row first")
	}
	if !strings.Contains(err.Error(), "comprehension key must be constant") {
		t.Errorf("refused with %q, want it to name the constant key its builder needs", err.Error())
	}
}

// The copy is the third builder, and the reason the rule lives in one helper rather than three: a copy
// that spliced two arrays could carry a duplicate entry out of a container that ever had one.
func TestDictCopyIsBuiltByPuttingToo(t *testing.T) {
	if got, want := captureStdout(t, "d = {\"a\": 1, \"b\": 2, \"a\": 3}\nc = dict(d)\nprint(c)\nprint(len(c))\n"), "{'a': 3, 'b': 2}\n2\n"; got != want {
		t.Errorf("interpreter dict() copy = %q, want %q", got, want)
	}
}

// The tripwire: a dict grown from any of the three builders asks one door. Appending is the condition
// that made the defect, so a builder that grows `dvals` on its own is what this fails — the behaviour
// rows above would pass again the day someone re-introduces the second copy of the rule.
func TestEveryDictBuilderWalksTheOneDoor(t *testing.T) {
	src, err := os.ReadFile("jit.go")
	if err != nil {
		t.Fatalf("read jit.go: %v", err)
	}
	source := string(src)
	door := "func (e *Evaluator) dictPut(o *obj, key, val int64)"
	if !strings.Contains(source, door) {
		t.Fatalf("jit.go lost its dict-building door (%s)", door)
	}
	for _, want := range []string{
		"e.dictPut(o, e.slotVal(k, kv), e.slotVal(n.Vals[i], vv))",        // the literal
		"e.dictPut(ro, e.slotVal(c.Keys[0], k), e.slotVal(c.Vals[0], v))", // the comprehension
		"e.dictPut(o, key, val)",                 // item assignment
		"e.dictPut(dst, o.elems[i], o.dvals[i])", // the dict() copy
	} {
		if !strings.Contains(source, want) {
			t.Errorf("a dict builder stopped walking the door: %s is missing from jit.go", want)
		}
	}
	if n := strings.Count(source, "dvals = append"); n != 1 {
		t.Errorf("jit.go has %d places that append dict values; exactly one is allowed, inside dictPut", n)
	}
	if n := strings.Count(source, "elems = append(o.elems, e.slotVal(k, kv)"); n != 0 {
		t.Errorf("the dict literal grew its keys by append again — the condition Gap R.120 measured")
	}

}

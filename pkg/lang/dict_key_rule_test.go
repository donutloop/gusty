package lang

import (
	"sort"
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

// The tripwire: a dict grown by any of the builders asks one door. Appending is the condition that
// made the defect (roadmap Gap R.120), so a builder that grows a dict's slots on its own is what this
// fails — the behaviour rows above would pass again the day someone re-introduces the second copy of
// the rule.
//
// The case used to read the retired engine's Go source and count its `dictPut` callers. What it
// actually holds is a property of the artifact, so that is what it reads now: the module the compiler
// emitted. Every dict growth in it is a call to `rt_dict_put` or its tagged twin, and nothing outside
// those two definitions writes a slot in a dict's data array. The builders — literal, comprehension,
// item assignment, `dict()` copy — are all present in the one program below, so a builder that grew
// its own array would show up here as a store the module cannot account for.
func TestEveryDictBuilderWalksTheOneDoor(t *testing.T) {
	const src = "d = {1: 2, 2: 3}\n" +
		"d[3] = 4\n" +
		"e = {x: x * 2 for x in [5, 6]}\n" +
		"print(d)\nprint(len(e))\n"
	mod := compileSrc(t, src)

	door := "define internal void @rt_dict_put(i32 %h, i32 %k, i32 %v) {"
	taggedDoor := "define internal void @rt_dict_put_tagged(i32 %h, i32 %k, i32 %v, i32 %kt, i32 %vt) {"
	for _, want := range []string{door, taggedDoor} {
		if !strings.Contains(mod, want) {
			t.Fatalf("the module lost its dict-building door (%s)", want)
		}
	}
	// Every builder reaches for a door: the literal takes the plain one, the tagged one carries the
	// elements' kinds, and the comprehension reaches through whichever its keys spell.
	if n := strings.Count(mod, "call void @rt_dict_put(") + strings.Count(mod, "call void @rt_dict_put_tagged("); n < 3 {
		t.Errorf("the module grows dicts in %d places through the door; the literal, the assignment and the comprehension must all go through it", n)
	}
	if _, err := Compile("d = {1: 2}\nprint(dict(d))\n"); err == nil || !strings.Contains(err.Error(), "copies are not supported") {
		t.Errorf("the copy builder answered instead of refusing; it must not have a growth route of its own (got %v)", err)
	}

	// The two halves of "one door": nothing the compiler emitted for the *program* writes a slot in a
	// container's data array (it has to ask a runtime helper), and among the dict helpers only the two
	// doors do. This is the exact shape of the defect — a second copy of the growth rule, splicing slots
	// instead of asking the one that checks for an existing key first — and the shape the behaviour rows
	// above would not catch, because a splice can print the right answer right up to the day a key
	// repeats.
	if offenders := slotStoresOutsideRuntime(mod); len(offenders) != 0 {
		t.Errorf("the module writes container slots from code the runtime does not own (%s) — the growth rule has a second copy (Gap R.120)",
			strings.Join(offenders, ", "))
	}
	if offenders := dictSlotWriters(mod); len(offenders) != 2 {
		t.Errorf("%d dict helpers write data slots, want exactly the two doors (rt_dict_put, rt_dict_put_tagged): %s",
			len(offenders), strings.Join(offenders, ", "))
	}
}

// slotStoresOutsideRuntime names the functions where the compiler's own code (not a runtime helper)
// stores into a container's slot array. Program code may not: it must call rt_dict_put, rt_list_append
// or their twins, which are the places that keep a key from being added twice.
func slotStoresOutsideRuntime(mod string) []string {
	var out []string
	for name, body := range moduleFunctions(mod) {
		if strings.HasPrefix(strings.TrimPrefix(name, "@"), "rt_") {
			continue
		}
		if storesIntoSlotArray(body) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// dictSlotWriters names the dict helpers that write data slots. Exactly the two doors may.
func dictSlotWriters(mod string) []string {
	var out []string
	for name, body := range moduleFunctions(mod) {
		if !strings.HasPrefix(strings.TrimPrefix(name, "@"), "rt_dict_") {
			continue
		}
		if storesIntoSlotArray(body) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// moduleFunctions splits the textual module into function name to body. Names keep their `@`.
func moduleFunctions(mod string) map[string]string {
	funcs := map[string]string{}
	var cur string
	var body strings.Builder
	for _, line := range strings.Split(mod, "\n") {
		if strings.HasPrefix(line, "define ") {
			if cur != "" {
				funcs[cur] = body.String()
			}
			cur = ""
			for _, f := range strings.Fields(line) {
				if strings.HasPrefix(f, "@") {
					cur = strings.TrimSuffix(f, "(")
					break
				}
			}
			body.Reset()
			continue
		}
		if cur != "" {
			body.WriteString(line)
			body.WriteString("\n")
		}
	}
	if cur != "" {
		funcs[cur] = body.String()
	}
	return funcs
}

// storesIntoSlotArray reports whether a body stores into an element of the [256 x i32] slot array —
// the array a dict keeps its interleaved keys and values in, and the one thing nothing but the growth
// door may touch.
func storesIntoSlotArray(body string) bool {
	slotRegs := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if !strings.Contains(t, "getelementptr [256 x i32]") {
			continue
		}
		if name, _, ok := strings.Cut(t, " = "); ok && strings.HasPrefix(name, "%") {
			slotRegs[name] = true
		}
	}
	if len(slotRegs) == 0 {
		return false
	}
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "store ") {
			continue
		}
		for reg := range slotRegs {
			if strings.HasSuffix(t, reg) {
				return true
			}
		}
	}
	return false
}

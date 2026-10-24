// Roadmap Gap R.426 / ADR 0417 — `sorted` over a DICT sorts the dict's KEYS, which means it walks the
// entry array at stride two and stops at the entry count, because a dict's length word counts ENTRIES
// while its array holds two words per entry.
//
//	d = {"a": 1, "b": 2}
//	print(sorted(d))                 # ['a', 'b']       the reference
//	                                 # ['a', 1]   ← this backend, before — at exit 0
//	print(sorted(d, reverse=True))   # ['b', 'a']       the reference
//	                                 # [1, 'a']  ← this backend, before — at exit 0
//
// `sorted` took the container road it shares with a list: copy the payload array, sort the copy. A list's
// array is its elements, so that road is right there and wrong here — a dict's array is
// [key, value, key, value, …] and its `len` is 2, so the copy read the first TWO words, handed the sort a
// text key, a number and (for the reversed case) them flipped, and printed both at **exit 0**. One key and
// one value were presented as the two items of a sort over a two-entry dict. In the same program
// `for k in d` walked the keys correctly, and `list(d)` / `max(d)` / `sum(d)` all refused with their own
// sentences — so the lie belonged to this one door (**Gap R.246**'s iterable arm, **Gap R.350**'s stride note
// inside `rt_gc`, which had already been taught the same 2-words-per-entry fact and answered it).
//
// The door now asks the dict its keys through `@rt_dict_keys`, which strides by two and stops at
// `rt_dict_len`, and sorts THAT list. The comparator and the printed kind of a key are answered by the same
// record of the keys, in one place, so the order a key gets and the way it prints cannot drift (**ADR 0232**'s
// slot-describes-itself rule, **ADR 0232**'s one-table rule applied to a sort).
//
// What this file pins:
//
//   - the answers, on the compiled leg and against the record, for the shapes the row measured: text keys,
//     whole-number keys, keys that arrive unordered, `reverse=` by name and by position, the empty dict at
//     both doors, a dict filled by item stores, a container or a list sitting in a value slot, a verdict key,
//     the result assigned to a name and printed from there;
//   - the sorted/walk twins: the keys `for k in d` steps over are the keys `sorted(d)` prints, item for item;
//   - the shape of the walk in the module — `@rt_dict_keys` strides by `mul i32 %i, 2`, stops at
//     `@rt_dict_len`, and is the only thing whose handle reaches `@rt_sort`; the positional word read that
//     caused this row is not in the module at all;
//   - what the door refuses, and in what words, where the keys' kinds are not on record (**Gap R.429**).
package lang

import (
	"regexp"
	"strings"
	"testing"
)

// sortedDictAnswered is the family this row pays: a dict is handed to `sorted` and the answer is the
// reference's — its keys, in the reference's order.
func sortedDictAnswered() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		// The two programs the row was filed with.
		{"the row's own program", "d = {\"a\": 1, \"b\": 2}\nprint(sorted(d))\n", "['a', 'b']\n"},
		{"the row's program, reversed", "d = {\"a\": 1, \"b\": 2}\nprint(sorted(d, reverse=True))\n", "['b', 'a']\n"},

		// The keys arrive in an order the sort has to undo.
		{"keys arrive unordered", "d = {\"b\": 2, \"a\": 1, \"c\": 3}\nprint(sorted(d))\n", "['a', 'b', 'c']\n"},
		{"many keys, by name", "print(sorted({\"pear\": 1, \"apple\": 2, \"fig\": 3, \"banana\": 4}))\n", "['apple', 'banana', 'fig', 'pear']\n"},
		{"many keys, reversed", "print(sorted({\"pear\": 1, \"apple\": 2, \"fig\": 3, \"banana\": 4}, reverse=True))\n", "['pear', 'fig', 'banana', 'apple']\n"},
		{"whole-number keys", "print(sorted({3: \"c\", 1: \"a\", 2: \"b\"}))\n", "[1, 2, 3]\n"},
		{"whole-number keys reversed", "print(sorted({3: \"c\", 1: \"a\", 2: \"b\"}, reverse=True))\n", "[3, 2, 1]\n"},
		{"a repeated key keeps one entry", "print(sorted({\"b\": 1, \"a\": 2, \"b\": 3}))\n", "['a', 'b']\n"},

		// The empty dict, at both doors the row reaches.
		{"the empty literal", "print(sorted({}))\n", "[]\n"},
		{"the empty dict behind a name", "d = {}\nprint(sorted(d))\n", "[]\n"},

		// A dict the program built itself, key by key: the answer comes from the record of the keys, not
		// from the literal the binding left behind, which by then says nothing.
		{"a dict built by item stores", "g = {}\ng[\"q\"] = 1\ng[\"p\"] = 2\nprint(sorted(g))\n", "['p', 'q']\n"},

		// What a slot holds must not join the sort: the value is not a key.
		{"a dict inside a value slot", "print(sorted({\"a\": {\"n\": 1}, \"b\": 2}))\n", "['a', 'b']\n"},
		{"a list inside a value slot", "print(sorted({\"b\": [1], \"a\": [2]}))\n", "['a', 'b']\n"},
		{"the values keep their entries", "d = {\"b\": 2, \"a\": 1}\nprint(d[\"b\"])\nprint(sorted(d))\nprint(len(d))\n", "2\n['a', 'b']\n2\n"},

		// Kinds that are not plain numbers and not texts, and whose printing belongs to the slot.
		{"a verdict key", "print(sorted({True: 1, False: 0}))\n", "[False, True]\n"},
		{"one key, no comparison to perform", "print(sorted({None: 1}))\n", "[None]\n"},

		// The result is a list, and the program is free to keep it.
		{"the sorted keys kept under a name", "d = {\"b\": 2, \"a\": 1}\nxs = sorted(d)\nprint(xs)\nprint(len(xs))\nprint(xs[0])\n", "['a', 'b']\n2\na\n"},
		{"a literal sorted under a name", "xs = sorted({\"b\": 2, \"a\": 1})\nprint(xs)\n", "['a', 'b']\n"},
		{"whole keys kept under a name", "xs = sorted({3: \"c\", 1: \"a\"})\nprint(xs)\nprint(xs[0])\n", "[1, 3]\n1\n"},
		{"verdict keys kept under a name", "xs = sorted({True: 1, False: 0})\nprint(xs)\n", "[False, True]\n"},

		// A key the program named rather than wrote, and the front end has the text of.
		{"keys held under names", "a = \"b\"\nb = \"a\"\nprint(sorted({a: 1, b: 2}))\n", "['a', 'b']\n"},

		// Two dicts in one program: neither borrows the other's keys.
		{"two dicts, two sorts", "a = {\"b\": 2, \"a\": 1}\nc = {2: \"x\", 1: \"y\"}\nprint(sorted(a))\nprint(sorted(c))\n", "['a', 'b']\n[1, 2]\n"},
		{"an alias sorted twice", "d = {\"b\": 2, \"a\": 1}\ne = d\nprint(sorted(e))\nprint(sorted(d))\n", "['a', 'b']\n['a', 'b']\n"},
	}
}

// TestSortedOverADictAnswersItsKeys is the row's answer on both witness legs: the compiled leg prints the
// reference's bytes and the record leg has an answer for the same source. A MISSING record fails the case —
// an answer may not be certified by the backend that earned it (ADR 0302).
func TestSortedOverADictAnswersItsKeys(t *testing.T) {
	t.Parallel()
	for _, tc := range sortedDictAnswered() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the sort door refused a program the reference prints %q: %v\n%s", tc.want, err, tc.src)
			}
			if got != tc.want {
				t.Fatalf("printed %q, want %q\n%s", got, tc.want, tc.src)
			}
			if !HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge\n%s", tc.src, tc.src)
			}
			recorded, err := runGoldenStdout(t, tc.src)
			if err != nil {
				t.Fatalf("the record leg could not answer this source: %v\n%s", err, tc.src)
			}
			if recorded != tc.want {
				t.Fatalf("the record reads %q for this source and the compiled leg printed %q\n%s", recorded, got, tc.src)
			}
		})
	}
}

// TestTheSortedKeysAndTheWalkOverTheSameDictAgree is the row's twin claim: the keys the `for` road steps over
// and the keys this door sorts are the same keys. The `for` road's own arm over `sorted(d)` is still refused
// (**Gap R.247**'s dict half), so the twin is asked the way the two doors that DO answer can be asked: every
// key the walk produces is one the sort produced, and the two counts are one count.
func TestTheSortedKeysAndTheWalkOverTheSameDictAgree(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			"every walked key is a sorted key",
			"d = {\"b\": 2, \"a\": 1, \"c\": 3}\nxs = sorted(d)\nfor k in d:\n    print(k in xs)\nprint(len(xs))\n",
			"True\nTrue\nTrue\n3\n",
		},
		{
			"the walk keeps its own order, the sort keeps its own",
			"d = {\"b\": 2, \"a\": 1, \"c\": 3}\nprint(sorted(d))\nfor k in d:\n    print(k)\n",
			"['a', 'b', 'c']\nb\na\nc\n",
		},
		{
			"a whole-numbered twin",
			"d = {3: \"c\", 1: \"a\", 2: \"b\"}\nxs = sorted(d)\nfor k in d:\n    print(k in xs)\nprint(len(xs))\n",
			"True\nTrue\nTrue\n3\n",
		},
		{
			"sorting a dict leaves the dict alone",
			"d = {\"b\": 2, \"a\": 1}\nprint(sorted(d))\nprint(sorted(d))\nfor k in d:\n    print(k)\nprint(d[\"b\"])\n",
			"['a', 'b']\n['a', 'b']\nb\na\n2\n",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the twin program was refused, where the reference prints %q: %v\n%s", tc.want, err, tc.src)
			}
			if got != tc.want {
				t.Fatalf("printed %q, want %q\n%s", got, tc.want, tc.src)
			}
		})
	}
}

// TestTheOtherContainersStillSortTheirOwnElements is the row's control. This door shares `@rt_sort` with the
// list and the set, and a fix that reads a dict by its keys while reading a LIST by its elements is only
// correct if the list and the set still answer: they were never wrong.
func TestTheOtherContainersStillSortTheirOwnElements(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{"an inline list", "print(sorted([3, 1, 2]))\n", "[1, 2, 3]\n"},
		{"a list under a name, the name kept", "ys = [3, 1, 2]\nxs = sorted(ys)\nprint(xs)\nprint(ys)\n", "[1, 2, 3]\n[3, 1, 2]\n"},
		{"a text list under a name", "ys = [\"b\", \"a\"]\nxs = sorted(ys)\nprint(xs)\n", "['a', 'b']\n"},
		{"the dict's own questions, unsorted", "d = {\"b\": 2, \"a\": 1}\nprint(d)\nprint(len(d))\nprint(\"a\" in d)\n", "{'b': 2, 'a': 1}\n2\nTrue\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the list/set sort road was refused: %v\n%s", err, tc.src)
			}
			if got != tc.want {
				t.Fatalf("printed %q, want %q\n%s", got, tc.want, tc.src)
			}
		})
	}
}

// TestWhatSortingASetStillOwesIsFiledNotFixed pins the neighbour this row's control case measured. A SET is
// neither a list nor a dict, and the door that reads a dict by its keys does not reach it: `sorted(<set>)`
// answers the set's own insertion order, at **exit 0**, where the reference answers the sorted sequence.
// Byte-identical on the commit before this row's door (`rt_sort` is never reached for a set), which is what
// keeps it a row of its own rather than a footnote to **Gap R.426**.
func TestWhatSortingASetStillOwesIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, owed string
	}{
		{"a set under a name", "t = {\"c\", \"a\", \"b\"}\nprint(sorted(t))\n", "['a', 'b', 'c']\n"},
		{"a set reversed", "t = {\"c\", \"a\", \"b\"}\nprint(sorted(t, reverse=True))\n", "['c', 'b', 'a']\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the set sort was refused where it answers in words: %v\n%s", err, tc.src)
			}
			if got == tc.owed {
				t.Fatalf("roadmap Gap R.430 is pinned as owed and the door now answers %q — promote this case into the answered family and close the row\n%s", got, tc.src)
			}
		})
	}
}

// The two calls the door is allowed to leave behind, and the shape of the walk it must not change.
var (
	dictKeysCallRe = regexp.MustCompile(`(?m)^\s*%?[^=\s]*\s*=?\s*call i32 @rt_dict_keys\(i32 [^)]+\)\s*$`)
	dictKeysDefRe  = regexp.MustCompile(`(?m)^define internal i32 @rt_dict_keys\(i32 %h, i32 %kt\) \{$`)
	rtSortCallRe   = regexp.MustCompile(`(?m)^\s*call void @rt_sort\(i32 ([^,]+), i32 \d+\)\s*$`)
)

// TestTheKeyCopyWalksTheDictByItsEntries reads the door out of the module. The stride and the bound are the
// whole row: read them from the emitted blocks rather than from this file's prose, so a later edit to the
// door that quietly goes back to `len` words at stride one fails here instead of printing [1, 'a'].
func TestTheKeyCopyWalksTheDictByItsEntries(t *testing.T) {
	t.Parallel()
	ir := mustCompileIR(t, "d = {\"b\": 2, \"a\": 1}\nprint(sorted(d))\n")
	if !dictKeysDefRe.MatchString(ir) {
		t.Fatalf("the module does not define the key door this row built:\n%s", ir)
	}
	if n := len(dictKeysDefRe.FindAllString(ir, -1)); n != 1 {
		t.Fatalf("@rt_dict_keys is defined %d times; one door, one definition", n)
	}
	body := dictKeysBody(ir)
	for _, want := range []string{
		"%w = mul i32 %i, 2",            // the stride: two words per entry
		"%c = icmp slt i32 %i, %n",      // the bound: entries, not words
		"call i32 @rt_dict_len(i32 %h)", // and the bound comes from the dict, not from the source
		"call void @rt_set_elem(i32 %nh, i32 %i, i32 %k)",
		"call void @rt_tag_elem(i32 %nh, i32 %i, i32 %tg)",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the key door does not walk the entry array as claimed (%s):\n%s", want, body)
		}
	}
	if strings.Contains(body, "@rt_get_elem(i32 %h, i32 %i)") {
		t.Fatalf("the key door reads the array through the LIST element door — that is the stride this row paid:\n%s", body)
	}
	if !dictKeysCallRe.MatchString(ir) {
		t.Fatalf("the sort does not build its input through the key door:\n%s", ir)
	}
	// The handle the sort receives is the handle the key door returned, and nothing else.
	def := dictKeysCallTarget(ir)
	if def == "" {
		t.Fatalf("could not read the key door's answer register from the module:\n%s", ir)
	}
	for _, m := range rtSortCallRe.FindAllStringSubmatch(ir, -1) {
		if strings.TrimSpace(m[1]) != def {
			t.Fatalf("@rt_sort is handed %q, not the key copy %q", m[1], def)
		}
	}
}

// dictKeysBody returns the `define` block of the key door, so a claim about the walk is a claim about that
// block and not about any other block that happens to mention `mul i32 %i, 2`.
func dictKeysBody(ir string) string {
	start := dictKeysDefRe.FindStringIndex(ir)
	if start == nil {
		return ""
	}
	rest := ir[start[0]:]
	if end := strings.Index(rest[1:], "\n}\n"); end >= 0 {
		return rest[:end+3]
	}
	return rest
}

// dictKeysCallTarget returns the register the program's call to the key door writes its answer into.
func dictKeysCallTarget(ir string) string {
	for _, line := range strings.Split(ir, "\n") {
		if !strings.Contains(line, "call i32 @rt_dict_keys(") {
			continue
		}
		i := strings.Index(line, "=")
		if i < 0 {
			continue
		}
		return strings.TrimSpace(line[:i])
	}
	return ""
}

// TestWhatSortingADictRefusesInWords keeps the row's other half: where the keys' kinds are not on record the
// door says so, in a sentence that names what is missing and the row that owns it. The reference has an
// answer for each of these (it asks the key its own __lt__ at run time); a compiled answer here would be a
// guess, and this contract ranks a guessed answer worst (**Gap R.429**).
func TestWhatSortingADictRefusesInWords(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			"a text key and a number key together",
			"print(sorted({1: \"x\", \"b\": 2}))\n",
			"of more than one kind",
		},
		{
			"a fractional key",
			"print(sorted({1.5: \"x\", 2.5: \"y\"}))\n",
			"the box table rather than in its slot",
		},
		{
			"a key the front end holds no kind for",
			"x = 3\ny = 1\nprint(sorted({x: 1, y: 2}))\n",
			"is refused",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msg := compileErr(t, tc.src)
			if msg == "" {
				t.Fatalf("the door answered a program whose keys it cannot name; want a refusal containing %q\n%s", tc.want, tc.src)
			}
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("refused with %q, want a sentence containing %q\n%s", msg, tc.want, tc.src)
			}
			if !strings.Contains(msg, "Gap R.429") {
				t.Fatalf("the refusal does not name the row that owns the rest: %q\n%s", msg, tc.src)
			}
		})
	}
}

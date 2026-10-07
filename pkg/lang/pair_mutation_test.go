package lang

// pkg/lang/pair_mutation_test.go — a pair-bound name written into a container by a STATEMENT (roadmap
// L11.1, Gap R.146's mutation roads; ADR 0311, ADR 0310's dict entry and set member, ADR 0306's list
// element, ADR 0187's rule that a payload is never written without its tag).
//
// `xs.append(n)`, `s.add(n)`, `xs[i] = v` and `d[k] = v` ask the same question the literal builders ask —
// what word, and with what tag — from the other side of the semicolon: they change a container the program
// already built rather than build one. Each already had a tagged door (`rt_append_tagged`,
// `rt_set_add_tagged`, `rt_dict_put_tagged`, `rt_put_elem`+`rt_tag_elem`) because a heterogeneous literal
// needed one (ADR 0232), and each was being fed by `heapElemKind`, which asks a name for a spelling and so
// has nothing to say about a value whose kind lives in `_n_tag`.
//
// Two things had to be true at once, and the second is why the first is not simply "call the tagged door":
//
//   - the pair's payload and tag are stored together, and the container stops claiming one kind, because
//     the kind is a register now (`promotePairMixed`, ADR 0232's promotion by other means);
//   - every value that is NOT a pair is still asked what it is by the ordinary road first. Deleting that
//     question is how `d["k"] = xs[0] / 2` became a printed `3.0` by accident and `xs[0] = xs[0] / 2` a
//     silent `[2]` — a double's payload stored where its meaning lives in a word nobody read. The refusal
//     is the answer this backend gives when it cannot carry a kind, and a row that stops refusing without
//     answering has not been paid, it has been lost.
import (
	"strings"
	"testing"
)

// TestAPairBoundNameIsAppendedToAListByWayOfItsTag pays the append road: the statement that grows a list with a value whose kind the compiler cannot state.
func TestAPairBoundNameIsAppendedToAListByWayOfItsTag(t *testing.T) {
	for _, tc := range []struct{ name, pre, body, want string }{
		{"a verdict slot — appended, and the list printed", "xs = []\nxs.append(True)\nn = xs[0]\n", "ys = []\nys.append(n)\nprint(ys)\n", "[True]\n"},
		{"a verdict slot — appended beside a literal element", "xs = []\nxs.append(True)\nn = xs[0]\n", "ys = []\nys.append(n)\nys.append(1)\nprint(ys)\n", "[True, 1]\n"},
		{"a verdict slot — appended, and the list measured", "xs = []\nxs.append(True)\nn = xs[0]\n", "ys = []\nys.append(n)\nprint(len(ys))\n", "1\n"},
		{"a verdict slot — appended, and the list asked for membership", "xs = []\nxs.append(True)\nn = xs[0]\n", "ys = []\nys.append(n)\nprint(7 in ys)\n", "False\n"},
		{"a verdict slot — appended, and the list asked for membership", "xs = []\nxs.append(True)\nn = xs[0]\n", "ys = []\nys.append(n)\nfor v in ys:\n    print(v)\n", "True\n"},
		{"a verdict slot — appended, and the list rendered by str", "xs = []\nxs.append(True)\nn = xs[0]\n", "ys = []\nys.append(n)\nprint(str(ys))\n", "[True]\n"},
		{"a float slot — appended, and the list printed", "xs = []\nxs.append(2.5)\nn = xs[0]\n", "ys = []\nys.append(n)\nprint(ys)\n", "[2.5]\n"},
		{"a float slot — appended beside a literal element", "xs = []\nxs.append(2.5)\nn = xs[0]\n", "ys = []\nys.append(n)\nys.append(1)\nprint(ys)\n", "[2.5, 1]\n"},
		{"a float slot — appended, and the list measured", "xs = []\nxs.append(2.5)\nn = xs[0]\n", "ys = []\nys.append(n)\nprint(len(ys))\n", "1\n"},
		{"a float slot — appended, and the list asked for membership", "xs = []\nxs.append(2.5)\nn = xs[0]\n", "ys = []\nys.append(n)\nprint(7 in ys)\n", "False\n"},
		{"a float slot — appended, and the list asked for membership", "xs = []\nxs.append(2.5)\nn = xs[0]\n", "ys = []\nys.append(n)\nfor v in ys:\n    print(v)\n", "2.5\n"},
		{"a float slot — appended, and the list rendered by str", "xs = []\nxs.append(2.5)\nn = xs[0]\n", "ys = []\nys.append(n)\nprint(str(ys))\n", "[2.5]\n"},
		{"an int slot — appended, and the list printed", "xs = []\nxs.append(7)\nn = xs[0]\n", "ys = []\nys.append(n)\nprint(ys)\n", "[7]\n"},
		{"an int slot — appended beside a literal element", "xs = []\nxs.append(7)\nn = xs[0]\n", "ys = []\nys.append(n)\nys.append(1)\nprint(ys)\n", "[7, 1]\n"},
		{"an int slot — appended, and the list measured", "xs = []\nxs.append(7)\nn = xs[0]\n", "ys = []\nys.append(n)\nprint(len(ys))\n", "1\n"},
		{"an int slot — appended, and the list asked for membership", "xs = []\nxs.append(7)\nn = xs[0]\n", "ys = []\nys.append(n)\nprint(7 in ys)\n", "True\n"},
		{"an int slot — appended, and the list asked for membership", "xs = []\nxs.append(7)\nn = xs[0]\n", "ys = []\nys.append(n)\nfor v in ys:\n    print(v)\n", "7\n"},
		{"an int slot — appended, and the list rendered by str", "xs = []\nxs.append(7)\nn = xs[0]\n", "ys = []\nys.append(n)\nprint(str(ys))\n", "[7]\n"},
		{"a container slot — appended, and the list printed", "xs = []\nxs.append([1, 2])\nn = xs[0]\n", "ys = []\nys.append(n)\nprint(ys)\n", "[[1, 2]]\n"},
		{"a container slot — appended beside a literal element", "xs = []\nxs.append([1, 2])\nn = xs[0]\n", "ys = []\nys.append(n)\nys.append(1)\nprint(ys)\n", "[[1, 2], 1]\n"},
		{"a container slot — appended, and the list measured", "xs = []\nxs.append([1, 2])\nn = xs[0]\n", "ys = []\nys.append(n)\nprint(len(ys))\n", "1\n"},
		{"a container slot — appended, and the list asked for membership", "xs = []\nxs.append([1, 2])\nn = xs[0]\n", "ys = []\nys.append(n)\nprint(7 in ys)\n", "False\n"},
		{"a container slot — appended, and the list asked for membership", "xs = []\nxs.append([1, 2])\nn = xs[0]\n", "ys = []\nys.append(n)\nfor v in ys:\n    print(v)\n", "[1, 2]\n"},
		{"a container slot — appended, and the list rendered by str", "xs = []\nxs.append([1, 2])\nn = xs[0]\n", "ys = []\nys.append(n)\nprint(str(ys))\n", "[[1, 2]]\n"},
		{"a None slot — appended, and the list printed", "xs = []\nxs.append(None)\nn = xs[0]\n", "ys = []\nys.append(n)\nprint(ys)\n", "[None]\n"},
		{"a None slot — appended beside a literal element", "xs = []\nxs.append(None)\nn = xs[0]\n", "ys = []\nys.append(n)\nys.append(1)\nprint(ys)\n", "[None, 1]\n"},
		{"a None slot — appended, and the list measured", "xs = []\nxs.append(None)\nn = xs[0]\n", "ys = []\nys.append(n)\nprint(len(ys))\n", "1\n"},
		{"a None slot — appended, and the list asked for membership", "xs = []\nxs.append(None)\nn = xs[0]\n", "ys = []\nys.append(n)\nprint(7 in ys)\n", "False\n"},
		{"a None slot — appended, and the list asked for membership", "xs = []\nxs.append(None)\nn = xs[0]\n", "ys = []\nys.append(n)\nfor v in ys:\n    print(v)\n", "None\n"},
		{"a None slot — appended, and the list rendered by str", "xs = []\nxs.append(None)\nn = xs[0]\n", "ys = []\nys.append(n)\nprint(str(ys))\n", "[None]\n"},
		{"a text slot — appended, and the list printed", "xs = []\nxs.append(\"a\")\nn = xs[0]\n", "ys = []\nys.append(n)\nprint(ys)\n", "['a']\n"},
		{"a text slot — appended beside a literal element", "xs = []\nxs.append(\"a\")\nn = xs[0]\n", "ys = []\nys.append(n)\nys.append(1)\nprint(ys)\n", "['a', 1]\n"},
		{"a text slot — appended, and the list measured", "xs = []\nxs.append(\"a\")\nn = xs[0]\n", "ys = []\nys.append(n)\nprint(len(ys))\n", "1\n"},
		{"a text slot — appended, and the list asked for membership", "xs = []\nxs.append(\"a\")\nn = xs[0]\n", "ys = []\nys.append(n)\nprint(7 in ys)\n", "False\n"},
		{"a text slot — appended, and the list asked for membership", "xs = []\nxs.append(\"a\")\nn = xs[0]\n", "ys = []\nys.append(n)\nfor v in ys:\n    print(v)\n", "a\n"},
		{"a text slot — appended, and the list rendered by str", "xs = []\nxs.append(\"a\")\nn = xs[0]\n", "ys = []\nys.append(n)\nprint(str(ys))\n", "['a']\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.pre + tc.body
			RecordedStdoutIs(t, src, tc.want)
			res, err := JIT(src, 0)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the reference answers: %v", err)
			}
			if got := strings.TrimSuffix(res.Output, "\n"); got != strings.TrimSuffix(tc.want, "\n") {
				t.Fatalf("compiled printed %q, want %q", got, strings.TrimSuffix(tc.want, "\n"))
			}
		})
	}
}

// TestAPairBoundNameIsWrittenIntoAListSlotByWayOfItsTag pays the element-assignment road: one slot written, the tag written with it (ADR 0187), and the list promoted because its elements no longer share a kind.
func TestAPairBoundNameIsWrittenIntoAListSlotByWayOfItsTag(t *testing.T) {
	for _, tc := range []struct{ name, pre, body, want string }{
		{"a verdict slot — written over an element the literal spelled", "xs = []\nxs.append(True)\nn = xs[0]\n", "ys = [0]\nys[0] = n\nprint(ys)\n", "[True]\n"},
		{"a verdict slot — written into a list that holds others", "xs = []\nxs.append(True)\nn = xs[0]\n", "ys = [1, 2]\nys[1] = n\nprint(ys)\n", "[1, True]\n"},
		{"a float slot — written over an element the literal spelled", "xs = []\nxs.append(2.5)\nn = xs[0]\n", "ys = [0]\nys[0] = n\nprint(ys)\n", "[2.5]\n"},
		{"a float slot — written into a list that holds others", "xs = []\nxs.append(2.5)\nn = xs[0]\n", "ys = [1, 2]\nys[1] = n\nprint(ys)\n", "[1, 2.5]\n"},
		{"an int slot — written over an element the literal spelled", "xs = []\nxs.append(7)\nn = xs[0]\n", "ys = [0]\nys[0] = n\nprint(ys)\n", "[7]\n"},
		{"an int slot — written into a list that holds others", "xs = []\nxs.append(7)\nn = xs[0]\n", "ys = [1, 2]\nys[1] = n\nprint(ys)\n", "[1, 7]\n"},
		{"a container slot — written over an element the literal spelled", "xs = []\nxs.append([1, 2])\nn = xs[0]\n", "ys = [0]\nys[0] = n\nprint(ys)\n", "[[1, 2]]\n"},
		{"a container slot — written into a list that holds others", "xs = []\nxs.append([1, 2])\nn = xs[0]\n", "ys = [1, 2]\nys[1] = n\nprint(ys)\n", "[1, [1, 2]]\n"},
		{"a None slot — written over an element the literal spelled", "xs = []\nxs.append(None)\nn = xs[0]\n", "ys = [0]\nys[0] = n\nprint(ys)\n", "[None]\n"},
		{"a None slot — written into a list that holds others", "xs = []\nxs.append(None)\nn = xs[0]\n", "ys = [1, 2]\nys[1] = n\nprint(ys)\n", "[1, None]\n"},
		{"a text slot — written over an element the literal spelled", "xs = []\nxs.append(\"a\")\nn = xs[0]\n", "ys = [0]\nys[0] = n\nprint(ys)\n", "['a']\n"},
		{"a text slot — written into a list that holds others", "xs = []\nxs.append(\"a\")\nn = xs[0]\n", "ys = [1, 2]\nys[1] = n\nprint(ys)\n", "[1, 'a']\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.pre + tc.body
			RecordedStdoutIs(t, src, tc.want)
			res, err := JIT(src, 0)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the reference answers: %v", err)
			}
			if got := strings.TrimSuffix(res.Output, "\n"); got != strings.TrimSuffix(tc.want, "\n") {
				t.Fatalf("compiled printed %q, want %q", got, strings.TrimSuffix(tc.want, "\n"))
			}
		})
	}
}

// TestAPairBoundNameIsAddedToASetByWayOfItsTag pays the set's mutator: adding and tagging are one call, because a member added without its tag dedups against the payload alone.
func TestAPairBoundNameIsAddedToASetByWayOfItsTag(t *testing.T) {
	for _, tc := range []struct{ name, pre, body, want string }{
		{"a verdict slot — added, and the set asked for membership", "xs = []\nxs.append(True)\nn = xs[0]\n", "s2 = set()\ns2.add(n)\nprint(7 in s2)\n", "False\n"},
		{"a verdict slot — added, and the set asked for membership", "xs = []\nxs.append(True)\nn = xs[0]\n", "s2 = set()\ns2.add(n)\nfor v in s2:\n    print(v)\n", "True\n"},
		{"a verdict slot — added to a set that holds one already", "xs = []\nxs.append(True)\nn = xs[0]\n", "s2 = {1}\ns2.add(n)\nprint(s2)\n", "{1}\n"},
		{"a verdict slot — added, and the set printed", "xs = []\nxs.append(True)\nn = xs[0]\n", "s2 = set()\ns2.add(n)\nprint(s2)\n", "{True}\n"},
		{"a verdict slot — added, and the set measured", "xs = []\nxs.append(True)\nn = xs[0]\n", "s2 = set()\ns2.add(n)\nprint(len(s2))\n", "1\n"},
		{"a float slot — added, and the set asked for membership", "xs = []\nxs.append(2.5)\nn = xs[0]\n", "s2 = set()\ns2.add(n)\nprint(7 in s2)\n", "False\n"},
		{"a float slot — added, and the set asked for membership", "xs = []\nxs.append(2.5)\nn = xs[0]\n", "s2 = set()\ns2.add(n)\nfor v in s2:\n    print(v)\n", "2.5\n"},
		{"a float slot — added to a set that holds one already", "xs = []\nxs.append(2.5)\nn = xs[0]\n", "s2 = {1}\ns2.add(n)\nprint(s2)\n", "{1, 2.5}\n"},
		{"a float slot — added, and the set printed", "xs = []\nxs.append(2.5)\nn = xs[0]\n", "s2 = set()\ns2.add(n)\nprint(s2)\n", "{2.5}\n"},
		{"a float slot — added, and the set measured", "xs = []\nxs.append(2.5)\nn = xs[0]\n", "s2 = set()\ns2.add(n)\nprint(len(s2))\n", "1\n"},
		{"an int slot — added, and the set asked for membership", "xs = []\nxs.append(7)\nn = xs[0]\n", "s2 = set()\ns2.add(n)\nprint(7 in s2)\n", "True\n"},
		{"an int slot — added, and the set asked for membership", "xs = []\nxs.append(7)\nn = xs[0]\n", "s2 = set()\ns2.add(n)\nfor v in s2:\n    print(v)\n", "7\n"},
		{"an int slot — added to a set that holds one already", "xs = []\nxs.append(7)\nn = xs[0]\n", "s2 = {1}\ns2.add(n)\nprint(s2)\n", "{1, 7}\n"},
		{"an int slot — added, and the set printed", "xs = []\nxs.append(7)\nn = xs[0]\n", "s2 = set()\ns2.add(n)\nprint(s2)\n", "{7}\n"},
		{"an int slot — added, and the set measured", "xs = []\nxs.append(7)\nn = xs[0]\n", "s2 = set()\ns2.add(n)\nprint(len(s2))\n", "1\n"},
		{"a None slot — added, and the set asked for membership", "xs = []\nxs.append(None)\nn = xs[0]\n", "s2 = set()\ns2.add(n)\nprint(7 in s2)\n", "False\n"},
		{"a None slot — added, and the set asked for membership", "xs = []\nxs.append(None)\nn = xs[0]\n", "s2 = set()\ns2.add(n)\nfor v in s2:\n    print(v)\n", "None\n"},
		{"a None slot — added, and the set printed", "xs = []\nxs.append(None)\nn = xs[0]\n", "s2 = set()\ns2.add(n)\nprint(s2)\n", "{None}\n"},
		{"a None slot — added, and the set measured", "xs = []\nxs.append(None)\nn = xs[0]\n", "s2 = set()\ns2.add(n)\nprint(len(s2))\n", "1\n"},
		{"a text slot — added, and the set asked for membership", "xs = []\nxs.append(\"a\")\nn = xs[0]\n", "s2 = set()\ns2.add(n)\nprint(7 in s2)\n", "False\n"},
		{"a text slot — added, and the set asked for membership", "xs = []\nxs.append(\"a\")\nn = xs[0]\n", "s2 = set()\ns2.add(n)\nfor v in s2:\n    print(v)\n", "a\n"},
		{"a text slot — added to a set that holds one already", "xs = []\nxs.append(\"a\")\nn = xs[0]\n", "s2 = {1}\ns2.add(n)\nprint(s2)\n", "{1, 'a'}\n"},
		{"a text slot — added, and the set printed", "xs = []\nxs.append(\"a\")\nn = xs[0]\n", "s2 = set()\ns2.add(n)\nprint(s2)\n", "{'a'}\n"},
		{"a text slot — added, and the set measured", "xs = []\nxs.append(\"a\")\nn = xs[0]\n", "s2 = set()\ns2.add(n)\nprint(len(s2))\n", "1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.pre + tc.body
			RecordedStdoutIs(t, src, tc.want)
			res, err := JIT(src, 0)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the reference answers: %v", err)
			}
			if got := strings.TrimSuffix(res.Output, "\n"); got != strings.TrimSuffix(tc.want, "\n") {
				t.Fatalf("compiled printed %q, want %q", got, strings.TrimSuffix(tc.want, "\n"))
			}
		})
	}
}

// TestAPairBoundNameIsWrittenIntoADictEntryByWayOfItsTag pays the dict's setitem road on both sides of the entry: the key and the value each bring their own tag, and the lookup that reads them back compares tags, not payloads.
func TestAPairBoundNameIsWrittenIntoADictEntryByWayOfItsTag(t *testing.T) {
	for _, tc := range []struct{ name, pre, body, want string }{
		{"a verdict slot — written, and the dict printed", "xs = []\nxs.append(True)\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(d2)\n", "{'k': True}\n"},
		{"a verdict slot — written and read back by its literal key", "xs = []\nxs.append(True)\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(d2[\"k\"])\n", "True\n"},
		{"a verdict slot — written, and the dict measured", "xs = []\nxs.append(True)\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(len(d2))\n", "1\n"},
		{"a verdict slot — written, and the dict asked for membership", "xs = []\nxs.append(True)\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(\"k\" in d2)\n", "True\n"},
		{"a verdict slot — written beside a literal entry", "xs = []\nxs.append(True)\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nd2[\"j\"] = 2\nprint(d2)\n", "{'k': True, 'j': 2}\n"},
		{"a verdict slot — used as the key", "xs = []\nxs.append(True)\nn = xs[0]\n", "d2 = {}\nd2[n] = 1\nprint(d2)\n", "{True: 1}\n"},
		{"a verdict slot — used as the key and looked up by the same pair", "xs = []\nxs.append(True)\nn = xs[0]\n", "d2 = {}\nd2[n] = 1\nprint(d2[n])\n", "1\n"},
		{"a verdict slot — used as key and value at once", "xs = []\nxs.append(True)\nn = xs[0]\n", "d2 = {}\nd2[n] = n\nprint(d2)\n", "{True: True}\n"},
		{"a verdict slot — used as key and value beside a literal entry", "xs = []\nxs.append(True)\nn = xs[0]\n", "d2 = {\"a\": 1}\nd2[n] = n\nprint(d2)\n", "{'a': 1, True: True}\n"},
		{"a verdict slot — written, and the dict rendered by str", "xs = []\nxs.append(True)\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(str(d2))\n", "{'k': True}\n"},
		{"a float slot — written, and the dict printed", "xs = []\nxs.append(2.5)\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(d2)\n", "{'k': 2.5}\n"},
		{"a float slot — written and read back by its literal key", "xs = []\nxs.append(2.5)\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(d2[\"k\"])\n", "2.5\n"},
		{"a float slot — written, and the dict measured", "xs = []\nxs.append(2.5)\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(len(d2))\n", "1\n"},
		{"a float slot — written, and the dict asked for membership", "xs = []\nxs.append(2.5)\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(\"k\" in d2)\n", "True\n"},
		{"a float slot — written beside a literal entry", "xs = []\nxs.append(2.5)\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nd2[\"j\"] = 2\nprint(d2)\n", "{'k': 2.5, 'j': 2}\n"},
		{"a float slot — used as the key", "xs = []\nxs.append(2.5)\nn = xs[0]\n", "d2 = {}\nd2[n] = 1\nprint(d2)\n", "{2.5: 1}\n"},
		{"a float slot — used as the key and looked up by the same pair", "xs = []\nxs.append(2.5)\nn = xs[0]\n", "d2 = {}\nd2[n] = 1\nprint(d2[n])\n", "1\n"},
		{"a float slot — used as key and value at once", "xs = []\nxs.append(2.5)\nn = xs[0]\n", "d2 = {}\nd2[n] = n\nprint(d2)\n", "{2.5: 2.5}\n"},
		{"a float slot — used as key and value beside a literal entry", "xs = []\nxs.append(2.5)\nn = xs[0]\n", "d2 = {\"a\": 1}\nd2[n] = n\nprint(d2)\n", "{'a': 1, 2.5: 2.5}\n"},
		{"a float slot — written, and the dict rendered by str", "xs = []\nxs.append(2.5)\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(str(d2))\n", "{'k': 2.5}\n"},
		{"an int slot — written, and the dict printed", "xs = []\nxs.append(7)\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(d2)\n", "{'k': 7}\n"},
		{"an int slot — written and read back by its literal key", "xs = []\nxs.append(7)\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(d2[\"k\"])\n", "7\n"},
		{"an int slot — written, and the dict measured", "xs = []\nxs.append(7)\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(len(d2))\n", "1\n"},
		{"an int slot — written, and the dict asked for membership", "xs = []\nxs.append(7)\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(\"k\" in d2)\n", "True\n"},
		{"an int slot — written beside a literal entry", "xs = []\nxs.append(7)\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nd2[\"j\"] = 2\nprint(d2)\n", "{'k': 7, 'j': 2}\n"},
		{"an int slot — used as the key", "xs = []\nxs.append(7)\nn = xs[0]\n", "d2 = {}\nd2[n] = 1\nprint(d2)\n", "{7: 1}\n"},
		{"an int slot — used as the key and looked up by the same pair", "xs = []\nxs.append(7)\nn = xs[0]\n", "d2 = {}\nd2[n] = 1\nprint(d2[n])\n", "1\n"},
		{"an int slot — used as key and value at once", "xs = []\nxs.append(7)\nn = xs[0]\n", "d2 = {}\nd2[n] = n\nprint(d2)\n", "{7: 7}\n"},
		{"an int slot — used as key and value beside a literal entry", "xs = []\nxs.append(7)\nn = xs[0]\n", "d2 = {\"a\": 1}\nd2[n] = n\nprint(d2)\n", "{'a': 1, 7: 7}\n"},
		{"an int slot — written, and the dict rendered by str", "xs = []\nxs.append(7)\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(str(d2))\n", "{'k': 7}\n"},
		{"a container slot — written, and the dict printed", "xs = []\nxs.append([1, 2])\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(d2)\n", "{'k': [1, 2]}\n"},
		{"a container slot — written and read back by its literal key", "xs = []\nxs.append([1, 2])\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(d2[\"k\"])\n", "[1, 2]\n"},
		{"a container slot — written, and the dict measured", "xs = []\nxs.append([1, 2])\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(len(d2))\n", "1\n"},
		{"a container slot — written, and the dict asked for membership", "xs = []\nxs.append([1, 2])\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(\"k\" in d2)\n", "True\n"},
		{"a container slot — written beside a literal entry", "xs = []\nxs.append([1, 2])\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nd2[\"j\"] = 2\nprint(d2)\n", "{'k': [1, 2], 'j': 2}\n"},
		{"a container slot — written, and the dict rendered by str", "xs = []\nxs.append([1, 2])\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(str(d2))\n", "{'k': [1, 2]}\n"},
		{"a None slot — written, and the dict printed", "xs = []\nxs.append(None)\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(d2)\n", "{'k': None}\n"},
		{"a None slot — written and read back by its literal key", "xs = []\nxs.append(None)\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(d2[\"k\"])\n", "None\n"},
		{"a None slot — written, and the dict measured", "xs = []\nxs.append(None)\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(len(d2))\n", "1\n"},
		{"a None slot — written, and the dict asked for membership", "xs = []\nxs.append(None)\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(\"k\" in d2)\n", "True\n"},
		{"a None slot — written beside a literal entry", "xs = []\nxs.append(None)\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nd2[\"j\"] = 2\nprint(d2)\n", "{'k': None, 'j': 2}\n"},
		{"a None slot — used as the key", "xs = []\nxs.append(None)\nn = xs[0]\n", "d2 = {}\nd2[n] = 1\nprint(d2)\n", "{None: 1}\n"},
		{"a None slot — used as the key and looked up by the same pair", "xs = []\nxs.append(None)\nn = xs[0]\n", "d2 = {}\nd2[n] = 1\nprint(d2[n])\n", "1\n"},
		{"a None slot — used as key and value at once", "xs = []\nxs.append(None)\nn = xs[0]\n", "d2 = {}\nd2[n] = n\nprint(d2)\n", "{None: None}\n"},
		{"a None slot — used as key and value beside a literal entry", "xs = []\nxs.append(None)\nn = xs[0]\n", "d2 = {\"a\": 1}\nd2[n] = n\nprint(d2)\n", "{'a': 1, None: None}\n"},
		{"a None slot — written, and the dict rendered by str", "xs = []\nxs.append(None)\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(str(d2))\n", "{'k': None}\n"},
		{"a text slot — written, and the dict printed", "xs = []\nxs.append(\"a\")\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(d2)\n", "{'k': 'a'}\n"},
		{"a text slot — written and read back by its literal key", "xs = []\nxs.append(\"a\")\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(d2[\"k\"])\n", "a\n"},
		{"a text slot — written, and the dict measured", "xs = []\nxs.append(\"a\")\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(len(d2))\n", "1\n"},
		{"a text slot — written, and the dict asked for membership", "xs = []\nxs.append(\"a\")\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(\"k\" in d2)\n", "True\n"},
		{"a text slot — written beside a literal entry", "xs = []\nxs.append(\"a\")\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nd2[\"j\"] = 2\nprint(d2)\n", "{'k': 'a', 'j': 2}\n"},
		{"a text slot — used as the key", "xs = []\nxs.append(\"a\")\nn = xs[0]\n", "d2 = {}\nd2[n] = 1\nprint(d2)\n", "{'a': 1}\n"},
		{"a text slot — used as the key and looked up by the same pair", "xs = []\nxs.append(\"a\")\nn = xs[0]\n", "d2 = {}\nd2[n] = 1\nprint(d2[n])\n", "1\n"},
		{"a text slot — used as key and value at once", "xs = []\nxs.append(\"a\")\nn = xs[0]\n", "d2 = {}\nd2[n] = n\nprint(d2)\n", "{'a': 'a'}\n"},
		{"a text slot — used as key and value beside a literal entry", "xs = []\nxs.append(\"a\")\nn = xs[0]\n", "d2 = {\"a\": 1}\nd2[n] = n\nprint(d2)\n", "{'a': 'a'}\n"},
		{"a text slot — written, and the dict rendered by str", "xs = []\nxs.append(\"a\")\nn = xs[0]\n", "d2 = {}\nd2[\"k\"] = n\nprint(str(d2))\n", "{'k': 'a'}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.pre + tc.body
			RecordedStdoutIs(t, src, tc.want)
			res, err := JIT(src, 0)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the reference answers: %v", err)
			}
			if got := strings.TrimSuffix(res.Output, "\n"); got != strings.TrimSuffix(tc.want, "\n") {
				t.Fatalf("compiled printed %q, want %q", got, strings.TrimSuffix(tc.want, "\n"))
			}
		})
	}
}

// TestAPairBoundMemberOrKeyThatCannotBeHashedRaisesWhatTheReferenceRaises is the mutation roads' share of
// the hashing question: `s.add(n)` and `d[n] = v` put the value in a bucket, and a payload alone says "yes,
// hashable" for every value in the language — a list's handle is an i32 like any other. The tag is what knows
// (Gap R.81, ADR 0310's literal road, ADR 0271's rule about what a raise says).
func TestAPairBoundMemberOrKeyThatCannotBeHashedRaisesWhatTheReferenceRaises(t *testing.T) {
	for _, tc := range []struct{ name, pre, body, class, message string }{
		{"a container slot — added, and the set asked for membership", "xs = []\nxs.append([1, 2])\nn = xs[0]\n", "s2 = set()\ns2.add(n)\nprint(7 in s2)\n", "TypeError", "unhashable type: 'list'"},
		{"a container slot — added, and the set asked for membership", "xs = []\nxs.append([1, 2])\nn = xs[0]\n", "s2 = set()\ns2.add(n)\nfor v in s2:\n    print(v)\n", "TypeError", "unhashable type: 'list'"},
		{"a container slot — added to a set that holds one already", "xs = []\nxs.append([1, 2])\nn = xs[0]\n", "s2 = {1}\ns2.add(n)\nprint(s2)\n", "TypeError", "unhashable type: 'list'"},
		{"a container slot — used as the key", "xs = []\nxs.append([1, 2])\nn = xs[0]\n", "d2 = {}\nd2[n] = 1\nprint(d2)\n", "TypeError", "unhashable type: 'list'"},
		{"a container slot — used as the key and looked up by the same pair", "xs = []\nxs.append([1, 2])\nn = xs[0]\n", "d2 = {}\nd2[n] = 1\nprint(d2[n])\n", "TypeError", "unhashable type: 'list'"},
		{"a container slot — used as key and value at once", "xs = []\nxs.append([1, 2])\nn = xs[0]\n", "d2 = {}\nd2[n] = n\nprint(d2)\n", "TypeError", "unhashable type: 'list'"},
		{"a container slot — used as key and value beside a literal entry", "xs = []\nxs.append([1, 2])\nn = xs[0]\n", "d2 = {\"a\": 1}\nd2[n] = n\nprint(d2)\n", "TypeError", "unhashable type: 'list'"},
		{"a container slot — added, and the set printed", "xs = []\nxs.append([1, 2])\nn = xs[0]\n", "s2 = set()\ns2.add(n)\nprint(s2)\n", "TypeError", "unhashable type: 'list'"},
		{"a container slot — added, and the set measured", "xs = []\nxs.append([1, 2])\nn = xs[0]\n", "s2 = set()\ns2.add(n)\nprint(len(s2))\n", "TypeError", "unhashable type: 'list'"}} {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.pre + tc.body
			res, err := JIT(src, 0)
			if err != nil {
				t.Fatalf("compiled to nothing where the reference raises: %v", err)
			}
			if !strings.Contains(res.Stderr, tc.class+": "+tc.message) {
				t.Fatalf("raised %q, want %s: %s", firstLine(res.Stderr), tc.class, tc.message)
			}
			if res.Code == 0 {
				t.Fatalf("the trap left at the exit code of success")
			}
			if res.Code == 2 {
				t.Fatalf("exit 2 — LLVM rejected the module this raise was emitted into (ADR 0166)")
			}
		})
	}
}

// TestAMutatedContainerTakesItsWordsFromTheTaggedDoors is the IR half of the decision: each of the four
// roads must go to the call that takes the tag with the payload, with the tag arriving as the register the
// objects wrote rather than a constant this pass guessed, and the container must be told its slots describe
// themselves. The rows fail if a road grows a one-word helper of its own, or if the payload starts being
// stored as a `double`, which is the module `llc` rejects (ADR 0305's lesson).
func TestAMutatedContainerTakesItsWordsFromTheTaggedDoors(t *testing.T) {
	for _, tc := range []struct {
		name, pre, body, wantCall string
		guards                    bool
	}{
		{"append", pairDictIntSlot, "ys = []\nys.append(n)\nprint(ys)\n", "call void @rt_append_tagged(", false},
		{"element assignment", pairDictIntSlot, "ys = [0]\nys[0] = n\nprint(ys)\n", "call void @rt_tag_elem(", false},
		{"set add", pairDictIntSlot, "s2 = set()\ns2.add(n)\nprint(s2)\n", "call void @rt_set_add_tagged(", true},
		{"dict setitem value", pairDictIntSlot, "d2 = {}\nd2[\"k\"] = n\nprint(d2)\n", "call void @rt_dict_put_tagged(", false},
		{"dict setitem key", pairDictIntSlot, "d2 = {}\nd2[n] = 1\nprint(d2)\n", "call void @rt_dict_put_tagged(", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.pre + tc.body)
			if err != nil {
				t.Fatalf("refused a program the reference prints: %v", err)
			}
			if !strings.Contains(res.IR, tc.wantCall) {
				t.Errorf("the mutation never asked %s — it grew a one-word road of its own", tc.wantCall)
			}
			if !strings.Contains(res.IR, "load i32, i32* %_n_tag") {
				t.Error("the tag never came from the name the pair road bound — it was guessed from a spelling the element never had")
			}
			tagged := false
			for _, line := range strings.Split(res.IR, "\n") {
				if strings.Contains(line, tc.wantCall) && strings.Contains(line, "%t") {
					tagged = true
				}
			}
			if !tagged {
				t.Errorf("no tagged call took a register — the tag was a constant, not the word the objects wrote")
			}
			if !strings.Contains(res.IR, "call void @rt_mark_estr(i32 %") {
				t.Errorf("the container was never told its slots describe themselves")
			}
			got := strings.Contains(res.IR, "unhashable type: 'list'")
			if got != tc.guards {
				t.Errorf("the key/member guard is present=%v, want %v", got, tc.guards)
			}
		})
	}
}

// TestWhatTheMutationRoadsStillRefuseIsStillRefusedInWords is this cycle's honest half, and the row it most
// nearly lost. `d["k"] = xs[0] / 2` and `xs[0] = xs[0] / 2` used to refuse because the ordinary value road was
// asked first; the pair road must not answer for a value it is not. A pair handed through a parameter, and a
// literal a builtin folds into a static array, are the same field of Gap R.146 and keep their sentences;
// exit 2 stays forbidden, because a refusal this backend emits is a diagnostic (ADR 0166).
func TestWhatTheMutationRoadsStillRefuseIsStillRefusedInWords(t *testing.T) {
	for _, tc := range []struct{ name, pre, body, wantPhrase string }{
		{"a double written into a dict slot", pairDictIntSlot, "d2 = {}\nd2[\"k\"] = xs[0] / 2\nprint(d2)\n", "stores an i32 word"},
		{"a double written into a list slot", pairDictIntSlot, "ys = [0]\nys[0] = xs[0] / 2\nprint(ys)\n", "stores an i32 word"},
		{"a pair handed through a parameter into a dict", pairDictIntSlot, "def build(k):\n    return {\"k\": k}\nprint(build(n))\n", "roadmap L11.1"},
		{"a sum over a folded literal", pairDictIntSlot, "print(sum([n]))\n", "one word"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.pre + tc.body
			res, err := JIT(src, 0)
			if err == nil {
				t.Fatalf("answered %q where this road does not reach: the value is not a pair this door can carry", res.Output)
			}
			if !strings.Contains(err.Error(), tc.wantPhrase) {
				t.Errorf("the refusal does not name the missing half (%q): %v", tc.wantPhrase, err)
			}
		})
	}
}

// TestTheArithmeticAnswerOfASlotIsAppendedToo is the shape this cycle found already answered by the append
// road's other door: `xs[0] / 2` is a pair the float road built, and `ys.append(...)` hands the list its
// payload and its tag together, so the list prints `[3.5]` where an untagged append would print the number
// `3` — a row worth pinning, because it is the near-miss that shows what the tagged doors are for.
func TestTheArithmeticAnswerOfASlotIsAppendedToo(t *testing.T) {
	for _, tc := range []struct{ name, pre, body, want string }{
		{"the double a slot's half is", pairDictIntSlot, "ys = []\nys.append(xs[0] / 2)\nprint(ys)\n", "[3.5]\n"},
		{"that double beside a literal", pairDictIntSlot, "ys = []\nys.append(xs[0] / 2)\nys.append(1)\nprint(ys)\n", "[3.5, 1]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.pre + tc.body
			RecordedStdoutIs(t, src, tc.want)
			res, err := JIT(src, 0)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the reference answers: %v", err)
			}
			if strings.TrimSuffix(res.Output, "\n") != strings.TrimSuffix(tc.want, "\n") {
				t.Fatalf("compiled printed %q, want %q", res.Output, tc.want)
			}
		})
	}
}

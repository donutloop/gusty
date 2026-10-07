package lang

// pkg/lang/pair_container_test.go — a pair-bound name as a CONTAINER element (roadmap L11.1, Gap R.146;
// ADR 0303's pair binding, ADR 0304's arithmetic door, ADR 0187's payload-never-written-without-its-tag).
//
// A list literal used to be lowered two ways: a static `@.lstN` global when every element's shape is known
// at compile time, a heap object when a payload does not fit an i32 slot or a slot cannot say what it holds.
// A name the pair road bound broke both doors at once — its shape is not known (the object the slot was read
// out of decides at run time) and its word is not one word (payload and tag are two allocas). `print([n])`
// therefore refused with Gap R.146's sentence, while `print(n)` had been answering since ADR 0303.
//
// The fix is the third door opened, not a new runtime: the literal goes to the heap builder, and the element
// writes BOTH of its words — the payload through `rt_set_elem`, the tag through `rt_tag_elem` — taking the tag
// from the name's own alloca instead of from `elemTagFor`'s constant. `rt_tag_elem` takes an i32, and a
// register is an i32, so the runtime needed nothing: what a literal element states as a constant, a pair
// element states as a fact the objects wrote. The literal is also marked self-describing (bit 8) so the
// printer reads the slots instead of the list's single kind — without that bit `["a"]` came back as [0], the
// interned index, which is Gap R.38's wrong-answer family arriving through the door this cycle opened.
//
// The rows below are the shapes that used to refuse. Every answer is the record's, which is CPython's, and
// the trap rows are compared by class AND message: an element whose tag says `str` has to be able to make
// `n + 1` raise the concatenation sentence, not print an interned index.
//
// The CLI comparison against CPython lives in integration/pair_container_test.go.

import (
	"strings"
	"testing"
)

const intSlot = "xs = []\nxs.append(7)\nn = xs[0]\n"

// TestAPairBoundNameEntersAContainerByWayOfItsTag is the row Gap R.146 named: an element of a list literal
// is a position that used to keep one word for its operand, and no longer does.
func TestAPairBoundNameEntersAContainerByWayOfItsTag(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"the element the pair road bound", intSlot + "print([n])\n", "[7]\n"},
		{"the pair first, literal after", intSlot + "print([n, 1])\n", "[7, 1]\n"},
		{"literal before, pair after", intSlot + "print([1, n])\n", "[1, 7]\n"},
		{"the same pair twice", intSlot + "print([n, n])\n", "[7, 7]\n"},
		{
			"the pair beside every other kind",
			intSlot + "print([n, \"x\", 2, None])\n", "[7, 'x', 2, None]\n",
		},
		{
			"a text slot in a literal with numbers",
			"xs = []\nxs.append(\"a\")\nn = xs[0]\nprint([n, 1, 2.5, None])\n", "['a', 1, 2.5, None]\n",
		},
		{"a float slot", "xs = []\nxs.append(2.5)\nn = xs[0]\nprint([n])\n", "[2.5]\n"},
		{"a None slot", "xs = []\nxs.append(None)\nn = xs[0]\nprint([n])\n", "[None]\n"},
		{"a bool slot", "xs = []\nxs.append(True)\nn = xs[0]\nprint([n])\n", "[True]\n"},
		{
			"a container slot, nested",
			"xs = []\nxs.append([1, 2])\nn = xs[0]\nprint([n])\n", "[[1, 2]]\n",
		},
		{
			"two pair-bound names",
			"xs = []\nxs.append(7)\nxs.append(8)\na = xs[0]\nb = xs[1]\nprint([a, b])\n", "[7, 8]\n",
		},
		{
			"the answer of arithmetic over a slot",
			"xs = []\nxs.append([7, 8])\nn = xs[0][0] * 2\nprint([n])\n", "[14]\n",
		},
		{
			"the loop variable as an element",
			"xs = []\nxs.append(7)\nfor v in xs:\n    print([v])\n", "[7]\n",
		},
		{
			"a dict slot by key",
			"d = {}\nd[\"k\"] = 9\nn = d[\"k\"]\nprint([n])\n", "[9]\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			RecordedStdoutIs(t, tc.src, tc.want)
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the reference answers: %v", err)
			}
			if got := strings.TrimSuffix(res.Output, "\n"); got != strings.TrimSuffix(tc.want, "\n") {
				t.Fatalf("compiled printed %q, want %q", got, strings.TrimSuffix(tc.want, "\n"))
			}
		})
	}
}

// TestAPairBoundNameBoundIntoAListKeepsItsTagOnTheObject is the binding half: `y = [n]` is the same element
// question asked by the assignment road, which has its own element loop and its own record of the variable's
// element kinds. Every position that reads the list back has to see the tag the name carried, or it reads the
// payload of a slot it cannot classify — which is how a text element prints its interned index.
func TestAPairBoundNameBoundIntoAListKeepsItsTagOnTheObject(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"bound and printed", intSlot + "y = [n]\nprint(y)\n", "[7]\n"},
		{"bound with a literal element", intSlot + "y = [n, 1]\nprint(y)\n", "[7, 1]\n"},
		{"bound and asked for its length", intSlot + "y = [n]\nprint(len(y))\n", "1\n"},
		{"bound and subscripted", intSlot + "y = [n]\nprint(y[0])\n", "7\n"},
		{"bound, subscripted, and used as a number", intSlot + "y = [n]\nprint(y[0] + 1)\n", "8\n"},
		{"bound and asked for membership", intSlot + "y = [n]\nprint(7 in y)\n", "True\n"},
		{"bound and then appended to", intSlot + "y = [n]\ny.append(8)\nprint(y)\n", "[7, 8]\n"},
		{"bound and looped over", intSlot + "y = [n]\nfor v in y:\n    print(v)\n", "7\n"},
		{"bound and rendered by str", intSlot + "y = [n]\nprint(str(y))\n", "[7]\n"},
		{
			"bound from a text slot and rendered",
			"xs = []\nxs.append(\"a\")\nn = xs[0]\ny = [n]\nprint(str(y))\n", "['a']\n",
		},
		{
			"bound from a text slot and compared",
			"xs = []\nxs.append(\"a\")\nn = xs[0]\nprint([n] == [\"a\"])\n", "True\n",
		},
		{
			"bound from a float slot and printed",
			"xs = []\nxs.append(2.5)\nn = xs[0]\ny = [n]\nprint(y)\n", "[2.5]\n",
		},
		{"repr of the bound list", "xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(repr([n]))\n", "['a']\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			RecordedStdoutIs(t, tc.src, tc.want)
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the reference answers: %v", err)
			}
			if got := strings.TrimSuffix(res.Output, "\n"); got != strings.TrimSuffix(tc.want, "\n") {
				t.Fatalf("compiled printed %q, want %q", got, strings.TrimSuffix(tc.want, "\n"))
			}
		})
	}
}

// TestAPairBoundNameStillRefusesTheContainerPositionsThatTakeOneWord is this cycle's honest half. A dict and a
// set keep their entries in a table the builder fills with one word per key and per value, and the roads that
// bind them ask `heapElemKind` for every element before any tag question is put — so the shape is still a
// refusal, in the words that name the missing capability. `sum`/`min`/`max` over a literal fold the elements
// into a static array, which has the same one-word problem. Each row stays until the pair reaches it; the
// failure mode that has to remain impossible is a dict whose text key prints its interned index at exit 0.
func TestAPairBoundNameStillRefusesTheContainerPositionsThatTakeOneWord(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"a dict value", intSlot + "print({\"k\": n})\n"},
		{"a dict key", intSlot + "print({n: 1})\n"},
		{"a set member", intSlot + "print({n})\n"},
		{"a bound dict", intSlot + "d2 = {\"k\": n}\nprint(d2)\n"},
		{"a bound set", intSlot + "s2 = {n}\nprint(s2)\n"},
		{"sum over a literal", intSlot + "print(sum([n]))\n"},
		{"min over a literal", intSlot + "print(min([n, 3]))\n"},
		{"max over a literal", intSlot + "print(max([n, 3]))\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			CompiledRefusal(t, tc.src, "roadmap L11.1")
		})
	}
}

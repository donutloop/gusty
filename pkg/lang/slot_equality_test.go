package lang

import (
	"strings"
	"testing"
)

// An element of a container whose slots describe themselves is a (payload, tag) pair, and until now
// the tag was carried to the *printer* and dropped at the *comparison*: `print(out[1])` asked the
// object and rendered `a`, while `out[1] == "a"` refused with "this context needs a single static
// kind" — one read, two answers, and the useful question was the one that could not be asked
// (roadmap L11.1, Gap R.79).
//
// The door here is the pair on both sides of `==`/`!=`, and the answer is `rt_payload_eq` — the one
// equality the container printers and lookups already ask, because an equality that ignores a tag
// (or, worse, compares two float *box handles* because the tags happened to match) is not an
// equality at all. Where the other side's kind cannot be proven the program is refused: comparing
// the two bare words is how `xs[0] == f()` came out true because f returned the text whose interned
// index happens to be the number in the slot (ADR 0232's collision, at a new site).

func TestSlotEqualityAnswersOnBothLegs(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{"a mixed slot against text", "xs = [1, \"a\"]\nprint(1 if xs[1] == \"a\" else 0)\n", "1\n"},
		{"the other answer", "xs = [1, \"a\"]\nprint(1 if xs[0] == \"a\" else 0)\n", "0\n"},
		{"two slot reads", "xs = [1, \"a\"]\nprint(1 if xs[0] == xs[1] else 0)\nprint(1 if xs[0] == xs[0] else 0)\n", "0\n1\n"},
		{
			// The soundness row: "zero" interns to a small index, and an untagged compare of the two
			// words has to call the number 0 and that index the same value.
			"payload collision between an int and interned text",
			"xs = [0, \"zero\"]\nprint(1 if xs[0] == xs[1] else 0)\n", "0\n",
		},
		{
			"None is not 0 and not the text None",
			"xs = [None, 0, \"None\"]\nprint(1 if xs[0] == xs[1] else 0)\nprint(1 if xs[0] == xs[2] else 0)\nprint(1 if xs[0] == None else 0)\n",
			"0\n0\n1\n",
		},
		{
			// The wrong answer that started the cycle: both tags said float, so the words compared
			// were two box handles and the numbers never met.
			"a float slot against the number it holds",
			"xs = [1.5, \"a\"]\ny = xs[0]\nprint(1 if y == 1.5 else 0)\nprint(1 if y == 1.6 else 0)\n", "1\n0\n",
		},
		{
			"across the numeric tags, as Python counts",
			"xs = [1.0, \"a\"]\nprint(1 if xs[0] == 1 else 0)\nxs2 = [1, \"a\"]\nprint(1 if xs2[0] == 1.0 else 0)\n",
			"1\n1\n",
		},
		{
			"a slot reached through an index the program computes",
			"xs = [1, \"a\"]\ni = 1\nprint(1 if xs[i] == \"a\" else 0)\nj = -1\nprint(1 if xs[j] == \"a\" else 0)\n",
			"1\n1\n",
		},
		{
			"a container slot against an equal container",
			"xs = []\nxs.append([1, 2])\nprint(1 if xs[0] == [1, 2] else 0)\nprint(1 if xs[0] != [1, 2] else 0)\n",
			"1\n0\n",
		},
		{"a dict entry holding a container", "d = {}\nd[\"k\"] = [1, 2]\nprint(1 if d[\"k\"] == [1, 2] else 0)\n", "1\n"},
		{
			// Gap R.79's own program: a comprehension slot compared with text.
			"a comprehension slot against text",
			"xs = []\nxs.append(1)\nxs.append(\"a\")\nout = [x for x in xs]\nprint(1 if out[1] == \"a\" else 0)\nprint(1 if out[0] == 1 else 0)\n",
			"1\n1\n",
		},
		{
			"an int key and a text key that collide as words",
			"d = {1: \"one\", \"a\": 2}\nprint(1 if d[\"a\"] == 2 else 0)\nprint(1 if d[1] == \"one\" else 0)\n",
			"1\n1\n",
		},
		{"a set slot against an equal set", "xs = [{1, 2}, \"a\"]\nprint(1 if xs[0] == {2, 1} else 0)\n", "1\n"},
		{"in an if condition", "xs = [1, \"a\"]\nif xs[1] == \"a\":\n    print(\"hit\")\nelse:\n    print(\"miss\")\n", "hit\n"},
		{
			"a loop variable against three kinds",
			"xs = [1, \"a\", None, 1.5]\nfor x in xs:\n    print(1 if x == \"a\" else 0, 1 if x == 1 else 0, 1 if x == None else 0)\n",
			"0 1 0\n1 0 0\n0 0 1\n0 0 0\n",
		},
		{"a uniform text container keeps its answer", "xs = [\"a\", \"b\"]\nprint(1 if xs[1] == \"b\" else 0)\n", "1\n"},
		{
			"an int-keyed dict with a container value and a plain one",
			"m = {0: [1, 2], 1: 3}\nprint(1 if m[0] == [1, 2] else 0)\nprint(1 if m[1] == 3 else 0)\n",
			"1\n1\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%q refused: %v", tc.src, err)
			}
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("AOT ran %q, want CPython's %q\nsrc: %s", out, tc.want, tc.src)
			}
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("interpreter printed %q, want CPython's %q\nsrc: %s", out, tc.want, tc.src)
			}
		})
	}
}

// The equality is asked of one helper. Two answers to "are these the same value?" is how the float
// box bug survived: the printer unboxed, the comparison did not (roadmap L11.1, Gap R.79).
func TestSlotEqualityAsksTheOneComparison(t *testing.T) {
	ir := compileOrFatal(t, "xs = [1, \"a\"]\nprint(1 if xs[1] == \"a\" else 0)\n")
	for _, want := range []string{
		"call i32 @rt_get_elem(",
		"call i32 @rt_tag_of(",
		"call i32 @rt_payload_eq(",
	} {
		if !strings.Contains(ir, want) {
			t.Errorf("module is missing %q\npayload/tag reads: %v", want, irLinesContaining(ir, "rt_get_elem"))
		}
	}
	if strings.Contains(ir, "@rt_mixed_eq(") {
		t.Errorf("the word-for-word comparison is back: %v", irLinesContaining(ir, "rt_mixed_eq"))
	}
}

// Two shapes stay refused, and each names the half that is missing rather than answering with a
// coincidence: an ordering comparison needs the tagged operand on the *relational* side too, and a
// comparison against an expression whose kind cannot be proven has no tag to compare with at all.
func TestSlotEqualityRefusesWhatItCannotProve(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			"a call whose two paths return different kinds",
			"def pick(c):\n    return \"z\"\n    return 7\n\nxs = [1, \"a\"]\nprint(1 if xs[0] == pick(1) else 0)\n",
			"needs a value whose kind the compiler can prove",
		},
		// An ordering of a mixed slot used to sit here. It is answered now — the tags pick which pair
		// the comparison was — and is pinned with its traps in slot_ordering_test.go (Gap R.82, ADR 0250).
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("%q compiled; want a refusal\nsrc: %s", tc.src, tc.src)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("refused with %q, want it to mention %q", err.Error(), tc.want)
			}
			if strings.Contains(err.Error(), "LLVM ERROR") || strings.Contains(err.Error(), "verifier") {
				t.Errorf("failed as an IR problem instead of a front-end refusal: %v", err)
			}
		})
	}
}

package lang

// pkg/lang/str_pair_test.go — `str(v)` and `repr(v)` answer for a value whose kind the program, not the
// literal, decides (roadmap L11.1 / L11.2, Gap R.171 / Gap R.146; ADR 0302's single-backend pass).
//
// The compiled backend renders a value by pointing one tag-reading printer at either stdout or a capture
// buffer. `print(xs[i])` had been through that door for a dozen ADRs; `str(xs[i])` had not. It reached
// instead the compile-time str/repr table — an integer, a text, None, a verdict, and the container
// literals — and refused anything the table has no row for, which is precisely the set of values whose
// kind is a run-time fact. Three shapes were refused while `print` of the same value answered:
//
//	xs = []; xs.append(3); xs.append("a")
//	str(xs[0])   # was refused, CPython "3"
//	str(xs[1])   # was refused, CPython "a"
//	repr(xs[1])  # was refused, CPython "'a'"
//	n = xs[1] ; str(n)   # was refused, and `n + 1` blamed a loop the program never wrote (Gap R.38)
//
// Each answers now by asking the same door `print` asks — `rt_str_of_value(payload, tag, quote)` — so
// `print(v)`, `str(v)` and `repr(v)` cannot drift into disagreeing about one value (ADR 0258's rule, which
// is the reason the pair exists at all). The `%quote` flag is the str/repr half of the pair: a text writes
// its characters under `str` and its quoted repr under `repr`, and only the caller knows which context it
// is in.
//
// What stays refused is the same boundary the row names — a position that keeps ONE word for a whole
// value (`n + 1`, `abs(n)`, `[n]`, `min(n, 3)`) — and the refusal now names where the pair came from
// instead of guessing, which is the second thing this file pins.
//
// The CLI-side comparison against CPython lives in integration/str_pair_test.go.

import (
	"strings"
	"testing"
)

const strPairBuilt = "xs = []\nxs.append(3)\nxs.append(\"a\")\nxs.append([1, 2])\nxs.append(None)\n"

// TestStrAndReprOfASlotReadAnswerLikePrintDoes is the row: every value the literal cannot describe, in
// both halves of the rendering pair, checked against the answer the record holds — which is CPython's.
func TestStrAndReprOfASlotReadAnswerLikePrintDoes(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"an int slot, str", "xs = []\nxs.append(3)\nprint(str(xs[0]))\n", "3\n"},
		{"a text slot, str", "xs = []\nxs.append(\"a\")\nprint(str(xs[0]))\n", "a\n"},
		{"a text slot, repr", "xs = []\nxs.append(\"a\")\nprint(repr(xs[0]))\n", "'a'\n"},
		{"an int slot, repr", "xs = []\nxs.append(3)\nprint(repr(xs[0]))\n", "3\n"},
		{"a float slot keeps its float", "xs = []\nxs.append(2.5)\nprint(str(xs[0]))\n", "2.5\n"},
		{"a None slot", "xs = []\nxs.append(None)\nprint(str(xs[0]))\n", "None\n"},
		{"a bool slot says its name", "xs = []\nxs.append(True)\nprint(str(xs[0]))\n", "True\n"},
		{"a bool slot, repr too", "xs = []\nxs.append(False)\nprint(repr(xs[0]))\n", "False\n"},
		{
			"a container slot renders itself, not its handle",
			"xs = []\nxs.append([1, 2])\nprint(str(xs[0]))\n", "[1, 2]\n",
		},
		{
			"a dict slot renders itself",
			"xs = []\nxs.append({\"k\": 5})\nprint(str(xs[0]))\n", "{'k': 5}\n",
		},
		{
			// The mixed container is the whole point: one slot is an int, the next a text, and the
			// tag — not the compiler's guess — decides which printer arm runs for each.
			"four kinds, one container, both halves",
			strPairBuilt + "print(str(xs[0]), str(xs[1]), str(xs[2]), str(xs[3]))\n", "3 a [1, 2] None\n",
		},
		{"repr over the same four", strPairBuilt + "print(repr(xs[0]), repr(xs[1]), repr(xs[2]), repr(xs[3]))\n", "3 'a' [1, 2] None\n"},
		{"a slot read by a computed position", "xs = []\nxs.append(7)\nxs.append(\"b\")\ni = 1\nprint(str(xs[i]))\n", "b\n"},
		{"a slot two levels down", "xs = []\nxs.append([4, \"c\"])\nprint(str(xs[0][1]))\n", "c\n"},
		{"a dict value by key", "d = {}\nd[\"k\"] = \"v\"\nprint(str(d[\"k\"]))\n", "v\n"},
		{"a dict value, repr quotes it", "d = {}\nd[\"k\"] = \"v\"\nprint(repr(d[\"k\"]))\n", "'v'\n"},
		{
			// The name case: `n = xs[i]` binds the pair, and the rendering pair has to read the tag
			// the binding stored rather than refuse the name.
			"a name bound from a slot, str",
			"xs = []\nxs.append(3)\nxs.append(\"a\")\nn = xs[1]\nprint(str(n))\n", "a\n",
		},
		{"a name bound from a slot, repr", "xs = []\nxs.append(3)\nxs.append(\"a\")\nn = xs[1]\nprint(repr(n))\n", "'a'\n"},
		{
			"print and str agree on the same slot",
			strPairBuilt + "print(str(xs[1]))\nprint(xs[1])\n", "a\na\n",
		},
		{
			"the loop variable of a built container",
			"xs = []\nxs.append(1)\nxs.append(\"b\")\nfor v in xs:\n    print(str(v))\n", "1\nb\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The record is the witness for the value; `RecordedStdoutIs` also refuses to let the
			// case's own expectation drift away from what was recorded.
			RecordedStdoutIs(t, tc.src, tc.want)
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the reference answers: %v", err)
			}
			if strings.TrimSuffix(res.Output, "\n") != strings.TrimSuffix(tc.want, "\n") {
				t.Fatalf("compiled printed %q, want %q", res.Output, tc.want)
			}
		})
	}
}

// TestStrOfASlotIsTheSameDoorPrintUses is the shape half: the answer arrives through the module's one
// tag-reading printer, so a second renderer cannot disagree about how many digits a float has or whether
// a text is quoted. A module that answers these shapes without the capture door has quietly grown a
// parallel renderer, which is how `str([1, 2])` came to answer `0` once (ADR 0258).
func TestStrOfASlotIsTheSameDoorPrintUses(t *testing.T) {
	res, err := Compile(strPairBuilt + "print(str(xs[1]), repr(xs[1]))\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, want := range []string{"@rt_str_of_value", "@rt_print_mixed_value"} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("the module does not render through the shared printer (%s missing)", want)
		}
	}
}

// TestThePairBindingWritesBothWords is the binding half of the same claim: `n = xs[0]` out of a container
// the program built stores the payload AND its tag, because a payload alone is a number wearing another
// object's bits (ADR 0185). A regression here is silent — the value prints as an unrelated small integer.
func TestThePairBindingWritesBothWords(t *testing.T) {
	res, err := Compile("xs = []\nxs.append(\"a\")\nn = xs[0]\nprint(str(n))\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(res.IR, "_n_tag") {
		t.Errorf("the binding stored no tag beside the payload; `n` is a bare i32 again:\n%s", firstLines(res.IR, 20))
	}
	if !strings.Contains(res.IR, "store i32") {
		t.Errorf("the binding stored nothing")
	}
}

// TestANumberPositionStillRefusesAndNamesTheTruthIsTheHonestHalf. The pair does not reach a position that
// keeps one word for its operand (`n + 1`), and that remains Gap R.146's. What this cycle changed is the
// sentence: it used to say the value "comes from a loop over a mixed list" for a program with no loop in
// it, which is Gap R.38's defect — a diagnostic describing a program the reader cannot find. The row
// therefore fails if the refusal either ANSWERS (a payload read as a number) or blames a loop.
func TestANumberPositionStillRefusesAndNamesTheTruth(t *testing.T) {
	src := "xs = []\nxs.append(3)\nn = xs[0]\nprint(n + 1)\n"
	if res, err := Compile(src); err == nil {
		t.Fatalf("the compiler answered a position that keeps one word for its operand, emitting:\n%s", firstLines(res.IR, 20))
	}
	_, err := JIT(src, 0)
	if err == nil {
		t.Fatal("expected the number position to be refused")
	}
	msg := err.Error()
	if strings.Contains(msg, "loop") {
		t.Errorf("the refusal blames a loop this program never wrote (Gap R.38): %q", msg)
	}
	for _, need := range []string{"slot", "roadmap L11.1"} {
		if !strings.Contains(msg, need) {
			t.Errorf("the refusal does not name %s: %q", need, msg)
		}
	}
}

// TestAStrRefusalStillNamesItsMissingDoor keeps the ADR 0166 contract on the shapes this cycle did NOT
// reach: a value with no pair at all must refuse in a sentence an agent can act on, not in three words.
func TestAStrRefusalStillNamesItsMissingDoor(t *testing.T) {
	for _, tc := range []struct{ name, src, need string }{
		{"a parameter with no pair", "def f(v):\n    return str(v)\n\nprint(f(None))\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := JIT(tc.src, 0)
			if err == nil {
				return // answered; the answer is checked by the tables above
			}
			msg := err.Error()
			for _, need := range []string{"roadmap", "reference"} {
				if !strings.Contains(msg, need) {
					t.Errorf("refusal names neither the roadmap row nor the reference's behaviour: %q", msg)
				}
			}
		})
	}
}

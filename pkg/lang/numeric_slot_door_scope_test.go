package lang

// pkg/lang/numeric_slot_door_scope_test.go — the number door's block is keyed by the container a store
// WRITES, not by the program (roadmap Gap R.191, the slot-read road of L11.1; ADR 0322).
//
// `xs[0][0] + 1` over a container the program built is answered by the tagged arithmetic door (ADR 0265),
// and that door opens only where a pass can prove the slots the read walks hold numbers — the reference
// ANSWERS `xs[0] + "a"` with a joined text and `[1] * 2` with a repeated list, and this backend builds
// neither from a slot (Gap R.82). The proof used to be one bit for the whole module, so one
// `w.append("a")` — a text in a container the arithmetic never walks — closed the door for every slot
// read in the program. `w = []; w.append("a"); nested = []; nested.append([7, 8]); print([nested[0][0] * 2])`
// printed a refusal at exit 1 where the reference prints `[14]`, and the sentence blamed `nested`:
//
//	index cannot reach into nested's slots: the name was rebound, mutated, or handed to code this pass
//	cannot see …
//
// — a refusal filed against a container the program never mutated, by a pass that had actually seen the
// text land in a different one.
//
// The block now travels with the OBJECT. A store blocks the container it writes and every name that can
// denote it, which is the reason this file asks two different questions:
//
//   - what the door ANSWERS, judged on both legs — the record and the compiled run — because the whole
//     point is that these programs print something, and the only thing that makes that a claim rather
//     than a mood is the reference's own output beside it (integration/numeric_slot_door_scope_test.go);
//   - what the pass KNOWS, asked as the plain Go question it is, because a block that stopped travelling
//     along an alias would answer `wordword`-shaped programs at exit 0 and no emitted-IR assertion can
//     see that coming.

import (
	"strings"
	"testing"
)

// unrelatedText is the line that used to close the door for the module: a text, stored into a container of
// its own, in a program whose arithmetic never walks near it.
const unrelatedText = "w = []\nw.append(\"a text\")\n"

// TestTheNumberDoorIsKeyedByTheContainerTheStoreWrites is the answering family: a container holding a
// text beside a container holding numbers, and the arithmetic over the numbers' slots answering. Every row
// is the reference's own output, and every row used to be a refusal at exit 1.
func TestTheNumberDoorIsKeyedByTheContainerTheStoreWrites(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"a slot two levels down, multiplied", unrelatedText + "nested = []\nnested.append([7, 8])\nprint(nested[0][0] * 2)\n", "14\n"},
		{"the answer bound and printed as a list", unrelatedText + "nested = []\nnested.append([7, 8])\nd = nested[0][0] * 2\nprint([d])\n", "[14]\n"},
		{"a slot two levels down, added to", unrelatedText + "nested = []\nnested.append([7, 8])\nprint(nested[0][0] + 1)\n", "8\n"},
		{"the other slot subtracted from", unrelatedText + "nested = []\nnested.append([7, 8])\nprint(nested[0][1] - 3)\n", "5\n"},
		{"the slot negated", unrelatedText + "nested = []\nnested.append([7, 8])\nprint(-nested[0][0])\n", "-7\n"},
		{"a float slot keeps its float", unrelatedText + "xs = []\nxs.append([7.5, 8])\nprint(xs[0][0] * 2)\n", "15.0\n"},
		{"a dict value written by assignment", unrelatedText + "d = {}\nd[\"k\"] = 40\nprint(d[\"k\"] + 2)\n", "42\n"},
		{"two slots from two rows of a built table", unrelatedText + "rows = []\nrows.append([1, 2])\nrows.append([3, 4])\nprint(rows[0][0] + rows[1][1])\n", "5\n"},
		{"three levels down", unrelatedText + "outer = []\nouter.append([[7, 8]])\nprint(outer[0][0][0] + 1)\n", "8\n"},
		{"two text containers and one numeric one", "w = []\nw.append(\"a text\")\nxs = []\nxs.append([7, 8])\nys = []\nys.append(\"more text\")\nprint(xs[0][0] + 1)\n", "8\n"},
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

// TestTheBlockTravelsWithTheObjectAndNotTheName asks the pass directly, as a function, so a block that
// stops crossing an alias fails a decision row rather than turning a program into a wrong answer. Each
// row names the container the read walks and whether the door may open for it.
func TestTheBlockTravelsWithTheObjectAndNotTheName(t *testing.T) {
	for _, tc := range []struct {
		name   string
		src    string
		read   string
		depth  int
		mayAsk bool
	}{
		{
			name: "a text in the container the read walks closes it",
			src:  "xs = []\nxs.append([7, 8])\nxs.append(\"word\")\nprint(xs[0][0] + 1)\n",
			read: "xs", depth: 2, mayAsk: false,
		},
		{
			name: "a text in a container nobody reads closes nothing",
			src:  "xs = []\nxs.append([7, 8])\nys = []\nys.append(\"word\")\nprint(xs[0][0] + 1)\n",
			read: "xs", depth: 2, mayAsk: true,
		},
		{
			name: "an alias carries the block back to the container it was bound from",
			src:  "xs = []\nxs.append([7, 8])\nys = xs\nys.append(\"word\")\nprint(xs[0][0] + 1)\n",
			read: "xs", depth: 2, mayAsk: false,
		},
		{
			name: "the alias itself is blocked too, in both directions",
			src:  "xs = []\nxs.append([7, 8])\nys = xs\nys.append(\"word\")\nprint(ys[0][0] + 1)\n",
			read: "ys", depth: 2, mayAsk: false,
		},
		{
			name: "an alias is not evidence, only a carrier",
			src:  "xs = []\nxs.append([7, 8])\nys = xs\nys.append([9, 10])\nprint(xs[0][0] + 1)\n",
			read: "xs", depth: 2, mayAsk: true,
		},
		{
			name: "a loop variable names the elements, so it names the container",
			src:  "xs = []\nxs.append([7, 8])\nfor v in xs:\n    v.append(\"a\")\nprint(xs[0][0] + 1)\n",
			read: "xs", depth: 2, mayAsk: false,
		},
		{
			name: "a parameter is the caller's container under the callee's name",
			src:  "def f(p):\n    p.append(\"a\")\n\nxs = []\nxs.append([7, 8])\nf(xs)\nprint(xs[0][0] + 1)\n",
			read: "xs", depth: 2, mayAsk: false,
		},
		{
			name: "a container handed to a callee this pass cannot see is blocked by the call",
			src:  "def f(p):\n    return p\n\nxs = []\nxs.append([7, 8])\nys = xs\nf(xs)\nprint(xs[0][0] + 1)\n",
			read: "xs", depth: 2, mayAsk: true,
		},
		{
			name: "an unknown callee could append to what it was handed",
			src:  "xs = []\nxs.append([7, 8])\nmystery(xs)\nprint(xs[0][0] + 1)\n",
			read: "xs", depth: 2, mayAsk: false,
		},
		{
			name: "a loop variable over an expression the pass cannot name blocks the program",
			src:  "def rows():\n    return [[7, 8]]\n\nfor v in rows():\n    v.append(\"a\")\n\nxs = []\nxs.append([7, 8])\nprint(xs[0][0] + 1)\n",
			read: "xs", depth: 2, mayAsk: false,
		},
		{
			name: "a numeric store through a nameless receiver blocks nothing",
			src:  "xs = []\nxs.append([7, 8])\nxs[0].append(9)\nprint(xs[0][0] + 1)\n",
			read: "xs", depth: 2, mayAsk: true,
		},
		{
			name: "a text through a nameless receiver is the residue the program-wide block keeps",
			src:  "xs = []\nxs.append([7, 8])\nxs[0].append(\"a\")\nprint(xs[0][0] + 1)\n",
			read: "xs", depth: 2, mayAsk: false,
		},
		{
			name: "a text stored two levels down blocks the program, not just the level",
			src:  "xs = []\nxs.append([7, 8])\nxs[0][1] = \"a\"\nprint(xs[0][0] + 1)\n",
			read: "xs", depth: 2, mayAsk: false,
		},
		{
			name: "an augmented assignment extends the container it never names as a receiver",
			src:  "xs = []\nxs.append([7, 8])\nxs += [\"a\"]\nprint(xs[0][0] + 1)\n",
			read: "xs", depth: 2, mayAsk: false,
		},
		{
			name: "a comprehension variable names the container it iterates",
			src:  "xs = []\nxs.append([7, 8])\nys = [v for v in xs]\nzz = []\nzz.append(\"a\")\nprint(xs[0][0] + 1)\n",
			read: "xs", depth: 2, mayAsk: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := parseProgram(tc.src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			c := computeNumericSlotChains(prog)
			if got := c.numeric(tc.read, tc.depth); got != tc.mayAsk {
				t.Fatalf("the door %s for %s at depth %d, want may-ask=%v (levels %v, ok %v, blocked-under %v, program-wide %v)",
					map[bool]string{true: "opened", false: "closed"}[got], tc.read, tc.depth, tc.mayAsk,
					c.depth[tc.read], c.ok[tc.read], c.blockedUnder(tc.read), c.blockAll)
			}
		})
	}
}

// TestTheAliasGuardCanFail is the negative half the repo asks of every gate: break the mechanism and a row
// reddens. Dropping the alias closure has to close the door on nothing, so the block would stop travelling
// and the alias rows would answer where the reference builds a text.
func TestTheAliasGuardCanFail(t *testing.T) {
	src := "xs = []\nxs.append([7, 8])\nys = xs\nys.append(\"word\")\nprint(xs[0][0] + 1)\n"
	prog, err := parseProgram(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	withAliases := computeNumericSlotChains(prog)
	if withAliases.numeric("xs", 2) {
		t.Fatal("the alias already fails to carry the block: this row cannot show anything breaking")
	}
	// The same program, with the union-find taken out of the walk: the store lands on `ys` alone.
	broken := newNumericSlotChains()
	broken.record("xs", &ListLit{Elems: []Expr{&IntLit{Value: 7}, &IntLit{Value: 8}}})
	broken.record("ys", &StrLit{Value: "word"})
	if broken.numeric("xs", 2) {
		return // the block did not travel without the alias closure — the guard has done its job
	}
	t.Fatal("a text stored into an alias left the container the read walks open: the block travels by name alone")
}

// TestTheNumberDoorStillRefusesWhatAnAliasCanFillWithText pins the residue: the door stays shut where the
// reference would answer a joined text or a repeated container, and it says so in words. Exit 2 is the
// contract's compiler-bug code and never an answer here (ADR 0166).
func TestTheNumberDoorStillRefusesWhatAnAliasCanFillWithText(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"text stored in the container the read walks",
			"xs = []\nxs.append(\"hi\")\nprint(xs[0] + 1)\n",
			"concatenating a string with a value that is not a string",
		},
		{
			"a text reaches the same container through an alias",
			"xs = []\nxs.append([7, 8])\nys = xs\nys.append(\"word\")\nprint(xs[1] * 2)\n",
			"single static kind",
		},
		{
			"a value with no spelling",
			"def f():\n    return [7, 8]\n\nxs = []\nxs.append(f())\nprint(xs[0][0] + 1)\n",
			"cannot reach into",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("the door opened for a program the reference answers with a text: %s", tc.src)
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("%q refused with %q, want it to mention %q", tc.src, msg, tc.want)
			}
			if strings.Contains(msg, "LLVM ERROR") || strings.Contains(msg, "verifier") {
				t.Fatalf("%q failed as an IR problem instead of a front-end refusal: %v", tc.src, err)
			}
		})
	}
}

// TestTheNumberDoorIsCarriedOnlyByTheProgramThatAsksIt is the cost half of widening the gate: opening the
// door for one container must not saddle a program that never asks the question with the arithmetic helper
// and its raise formatter (the ADR 0173/0192 rule — a referenced internal function never emitted is the
// module `llc` rejects).
func TestTheNumberDoorIsCarriedOnlyByTheProgramThatAsksIt(t *testing.T) {
	opened := slotIR(t, unrelatedText+"nested = []\nnested.append([7, 8])\nprint(nested[0][0] + 1)\n")
	for _, want := range []string{"@rt_num_arith", "@rt_lift_num", "@rt_print_mixed_value"} {
		if !strings.Contains(opened, want) {
			t.Errorf("the program whose door opened does not carry %s", want)
		}
	}
	quiet := slotIR(t, "print(2 + 3)\nys = []\nys.append(\"word\")\nprint(ys)\n")
	for _, absent := range []string{"@rt_num_arith", "@rt_num_bad", "@rt_lift_num"} {
		if strings.Contains(quiet, absent) {
			t.Errorf("a program that never asks the door carries %s", absent)
		}
	}
}

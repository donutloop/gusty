package lang

import (
	"strings"
	"testing"
)

// The tuple-unpacking target takes the pair road (Gap R.144).
//
// Before: an unpacking's value was decided twice over and neither time by the value. Each
// right-hand element was emitted to get its storage - unboxed where its static type is known -
// and stored to a fresh slot to get its name, and a source that began by reading a
// collection's slots got no slot read at all: `xs = []`, `xs.append([7, 8])`, then `a, b =
// xs[0][0] + 1, xs[0][1] + 2` was refused outright with `index cannot reach into xs's slots:
// it is not a list, tuple or dict`. The same question through an ordinary assignment - `a =
// xs[0][0] + 1` - answers 8, because there the element asks for a pair first (emitCallArgPair,
// emitFoldArgPair, emitIndexSlotReadPair, emitAssignValuePair) and takes the unboxed road only
// once the pair door has declined. So the language had two answers to the same question
// depending on whether the binding wore commas or not.
//
// Both roads are now the same road: assign_pair.go's bindAssignPair, whose steps are the ones
// an ordinary assignment already takes - a pair someone else already carried, a tagged value
// word, the slot read of a freshly computed pair, the value-as-object door, the float box, an
// unboxed store under the type gate - and which emitTupleAssign walks per element, after asking
// the value's shape of the pair emitter for the whole right-hand side so that a `*pa`-shaped
// star target still gets the cells it was written for.
//
// The binding is decided by the VALUE, which is what makes a pair-bound unpacked name answer as
// a pair: rebinding one to a plain int goes by load-and-retype onto the pair's own slot rather
// than opening an i32 name beside the pair one (floatbind.go's rule for every name, recorded at
// Gap R.322 - where it did not, the name read back 0), and the reference is CPython, which
// carries no static type at all: `a, b = xs[0][0], xs[0][1]` then `a = 5` answers 5.

// Every `want` below is the reference's, read off CPython by running the same source: this file
// never invents an answer, and the rows that landed with one (the `0` a rebound pair bound name
// read back, the intern-table index a text slot read back, the `1 0` a whole four-kind unpacking
// read back) are recorded in docs/roadmap-details.md.
var unpackPairAnswers = []struct {
	name string
	src  string
	want string
}{
	{"the slot pair, read by an unpacking", "xs = []\nxs.append([7, 8])\na, b = xs[0][0], xs[0][1]\nprint(a, b)\n", "7 8"},
	{"a slot two containers deep", "outer = []\nouter.append([3, 4])\nxs = []\nxs.append(outer)\na, b = xs[0][0][0], xs[0][0][1]\nprint(a, b)\n", "3 4"},
	{"arithmetic over two slots", "xs = []\nxs.append([7, 8])\na, b = xs[0][0] + 1, xs[0][1] + 2\nprint(a, b)\n", "8 10"},
	{"the two slots cross over", "xs = []\nxs.append([7, 8])\na, b = xs[0][1] + 2, xs[0][0] + 1\nprint(a, b)\n", "10 8"},
	{"a text slot and a number", "t = []\nt.append(\"hi\")\na, b = 1, t[0][0] + \"-\"\nprint(a, b)\n", "1 h-"},
	{"None beside an int", "a, b = 1, None\nprint(a, b)\n", "1 None"},
	{"None first", "a, b = None, True\nprint(a, b)\n", "None True"},
	{"a text slot", "a, b = 1, \"t\"\nprint(a, b)\n", "1 t"},
	{"a float slot", "a, b = 1, 2.5\nprint(a, b)\n", "1 2.5"},
	{"a bool slot", "a, b = 1, False\nprint(a, b)\n", "1 False"},
	{"a text then a float", "a, b = \"a\", 2.0\nprint(a, b)\n", "a 2.0"},
	{"a float then an int", "a, b = 1.5, 2\nprint(a, b)\n", "1.5 2"},
	{"four kinds at once", "a, b, c, d = 1, \"x\", None, True\nprint(a, b, c, d)\n", "1 x None True"},
	{"two plain ints, no pair road", "a, b = 1, 2\nprint(a, b)\n", "1 2"},
	{"two plain texts", "a, b = \"a\", \"b\"\nprint(a, b)\n", "a b"},
	{"a swap of numbers", "a, b = 1, 2\na, b = b, a\nprint(a, b)\n", "2 1"},
	{"a swap beside an untouched name", "c = 0\na, b = 1, 2\na, b = b, a\nprint(a, b, c)\n", "2 1 0"},
	{"a three-way rotate", "a, b, c = 1, 2, 3\na, b, c = c, a, b\nprint(a, b, c)\n", "3 1 2"},
	{"a dict entry", "d = {}\nd[\"k\"] = 5\na, b = 1, d[\"k\"]\nprint(a, b)\n", "1 5"},
	{"the folds", "xs = []\nxs.append([7, 8])\na, b = max(7, 8), min(7, 8)\nprint(a, b)\n", "8 7"},
	{"the folds over slots", "xs = []\nxs.append([7, 8])\na, b = max(xs[0][0], xs[0][1]), min(xs[0][0], xs[0][1])\nprint(a, b)\n", "8 7"},
	{"a pair bound name through a fold", "xs = []\nxs.append([7, 8])\na, b = xs[0][0], xs[0][1]\nprint(max(a, b))\n", "8"},
	{"a pair bound name compared", "xs = []\nxs.append([7, 8])\na, b = xs[0][0], xs[0][1]\nprint(a == 7)\n", "True"},
	{"a pair bound name ordered", "xs = []\nxs.append([7, 8])\na, b = xs[0][0], xs[0][1]\nprint(a < b)\n", "True"},
	{"a pair bound name measured", "xs = []\nxs.append([7, 8])\na, b = xs[0][0], xs[0][1]\nprint(len([a, b]))\n", "2"},
	{"a pair bound name rendered", "xs = []\nxs.append([7, 8])\na, b = xs[0][0], xs[0][1]\nprint(str(a) + \"-\" + str(b))\n", "7-8"},
	{"a pair bound name in an f-string", "xs = []\nxs.append([7, 8])\na, b = xs[0][0], xs[0][1]\nprint(f\"v={a}|{b}\")\n", "v=7|8"},
	{"a pair bound name printed twice", "xs = []\nxs.append([7, 8])\na, b = xs[0][0], xs[0][1]\nprint(a, b, a)\n", "7 8 7"},
	{"rebound to an int", "xs = []\nxs.append([7, 8])\na, b = xs[0][0], xs[0][1]\na = 5\nprint(a, b)\n", "5 8"},
	{"rebound to a text", "xs = []\nxs.append([7, 8])\na, b = xs[0][0], xs[0][1]\na = \"late\"\nprint(a, b)\n", "late 8"},
	{"rebound to a float", "xs = []\nxs.append([7, 7.5])\na, b = xs[0][0], xs[0][1]\na = 2.5\nprint(a, b)\n", "2.5 7.5"},
	{"rebound to None", "xs = []\nxs.append([7, 8])\na, b = xs[0][0], xs[0][1]\na = None\nprint(a, b)\n", "None 8"},
	{"rebound to a bool", "xs = []\nxs.append([7, 8])\na, b = xs[0][0], xs[0][1]\na = True\nprint(a, b)\n", "True 8"},
	{"both names rebound, to different kinds", "xs = []\nxs.append([7, 8])\na, b = xs[0][0], xs[0][1]\na = 5\nb = \"t\"\nprint(a, b)\n", "5 t"},
	{"rebound inside a loop", "xs = []\nxs.append([7, 8])\na, b = xs[0][0], xs[0][1]\nfor i in [0, 1]:\n    a = i\nprint(a, b)\n", "1 8"},
	{"re-unpacked the other way round", "xs = []\nxs.append([7, 8])\na, b = xs[0][0], xs[0][1]\na, b = xs[0][1], xs[0][0]\nprint(a, b)\n", "8 7"},
	{"bound in a function body", "def f():\n    xs = []\n    xs.append([7, 8])\n    a, b = xs[0][0] + 1, xs[0][1] + 2\n    print(a, b)\nf()\n", "8 10"},
	{"bound in a loop body", "xs = []\nxs.append([7, 8])\nfor i in [0]:\n    a, b = xs[0][0] + 1, xs[0][1] + 2\n    print(a, b)\n", "8 10"},
	{"an int answer returned out of a function", "def f():\n    a, b = 1, 2\n    return a\nprint(f())\n", "1"},
	{"a text answer returned out of a function", "def f():\n    a, b = 1, \"z\"\n    return b\nprint(f())\n", "z"},
}

// What still refuses, in words, and why: each of these is a position that keeps ONE word for its
// operand, so the tag the pair road carried has nowhere to go (L11.1's rule), or a construct the
// unpacking has never owned. They are refusals, not wrong answers, and the sentence each one
// prints names where the value it cannot carry came from (Gap R.38's rule).
var unpackPairRefusals = []struct {
	name    string
	src     string
	wantErr string
}{
	{
		name:    "a pair bound name as one operand of an addition",
		src:     "xs = []\nxs.append([7, 8])\na, b = xs[0][0], xs[0][1]\nprint(a + b)\n",
		wantErr: "this position needs a tagged value word",
	},
	{
		name:    "a rebound pair bound name in an addition",
		src:     "xs = []\nxs.append([7, 8])\na, b = xs[0][0], xs[0][1]\na = 5\nprint(a + b)\n",
		wantErr: "this position needs a tagged value word",
	},
	{
		name:    "a call answer as one unpacked element",
		src:     "def k():\n    return 7\nxs = []\nxs.append([7, 8])\na, b = xs[0][0] + 0, xs[0][1] - k()\nprint(a, b)\n",
		wantErr: "index cannot reach into xs's slots",
	},
	{
		name:    "a pair bound name subscripted",
		src:     "xs = []\nxs.append([7, 8])\na, b = xs[0][0], xs[0][1]\nprint(a[0])\n",
		wantErr: "index of a non-literal variable",
	},
	{
		name:    "a pair bound name as a condition",
		src:     "xs = []\nxs.append([7, 8])\na, b = xs[0][0], xs[0][1]\nif a:\n    print(1)\n",
		wantErr: "needs a tagged value word",
	},
	{
		name:    "unpacking from one collection instead of a list of values",
		src:     "xs = []\nxs.append([7, 8])\na, b = xs[0]\nprint(a, b)\n",
		wantErr: "unsupported tuple assignment value",
	},
	{
		name:    "unpacking from a name",
		src:     "t = (1, 2)\na, b = t\nprint(a, b)\n",
		wantErr: "unsupported tuple assignment value",
	},
}

func TestTupleUnpackingBindsEachValueByItsOwnTag(t *testing.T) {
	t.Parallel()
	for _, tc := range unpackPairAnswers {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if out := strings.TrimRight(compiledOut(t, tc.src), "\n"); out != tc.want {
				t.Fatalf("compiled %q printed %q, want %q", tc.src, out, tc.want)
			}
		})
	}
}

// The IR shape the row turns on: an unpacking whose element is a read asks the pair door first,
// so the value that lands in the name carries the tag the objects wrote beside the payload.
func TestTupleUnpackingAsksThePairDoorFirst(t *testing.T) {
	t.Parallel()
	ir := emittedModuleIR(t, "xs = []\nxs.append([7, 8])\na, b = xs[0][0] + 1, xs[0][1] + 2\nprint(a, b)\n")
	for _, want := range []string{
		"rt_get_elem(",     // the slot read the ordinary assignment makes, payload and tag together
		"rt_tag_of(",       // the kind the object says the slot holds
		"rt_num_arith(",    // and the arithmetic it feeds, not a bit operation on a raw word
		"_a_tag", "_b_tag", // the tag travels beside each name
	} {
		if !strings.Contains(ir, want) {
			t.Fatalf("the unpacking never emitted %q:\n%s", want, ir)
		}
	}
}

// A shape no pair door answers keeps the ordinary road's own answer, word for word: the plain
// literal unpacking must not find its way into the pair runtime.
func TestPlainLiteralUnpackingNeverVisitsThePairRoad(t *testing.T) {
	t.Parallel()
	ir := emittedModuleIR(t, "a, b = 1, 2\nprint(a + b)\n")
	for _, banned := range []string{"rt_get_elem(", "rt_tag_of(", "rt_dict_get_tagged(",
		"rt_append_tagged(", "rt_set_elem(", "rt_tag_elem(", "rt_lift_num("} {
		if strings.Contains(ir, banned) {
			t.Fatalf("plain literal unpacking called %s:\n%s", banned, ir)
		}
	}
}

// Unpacking stays simultaneous: the right-hand side is read from the bindings that were there
// before any of the left-hand names was touched. A store-before-load answers `2 2`.
func TestUnpackingStillReadsEveryValueBeforeNamingAny(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, want string }{
		{"a, b = 1, 2\na, b = b, a\nprint(a, b)\n", "2 1"},
		{"a, b, c = 1, 2, 3\na, b, c = c, a, b\nprint(a, b, c)\n", "3 1 2"},
		{"xs = []\nxs.append([7, 8])\na, b = xs[0][0], xs[0][1]\na, b = xs[0][1], xs[0][0]\nprint(a, b)\n", "8 7"},
		{"c = 0\na, b = 1, 2\na, b = b, a\nprint(a, b, c)\n", "2 1 0"},
	} {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if out := strings.TrimRight(compiledOut(t, tc.src), "\n"); out != tc.want {
				t.Fatalf("compiled %q printed %q, want %q", tc.src, out, tc.want)
			}
		})
	}
}

func TestTupleUnpackingRefusalsAreStillRefusals(t *testing.T) {
	t.Parallel()
	for _, tc := range unpackPairRefusals {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("want a refusal naming %q, got a clean compile: %s", tc.wantErr, tc.src)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

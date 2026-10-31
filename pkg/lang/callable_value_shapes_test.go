package lang

import (
	"strings"
	"testing"
)

// Roadmap L11.7 (functions are values that compile) — the four shapes the defunctionalisation pass and
// the callable value doors still decline, filed as **Gap R.590** (a callable RETURNED from a function),
// **Gap R.591** (a callable HELD IN A CONTAINER), **Gap R.592** (a callable HELD IN A FIELD) and
// **Gap R.593** (a callable PARAMETER FORWARDED to another function's callable position).
//
// Each row is measured, not assumed: the program's own answer is what `python3` prints (asserted on the
// CLI leg, `integration/callable_value_shapes_test.go`), the compiled leg's sentence is pinned here, and
// every row fails the day its shape starts answering, so closing a door has to go through this file.
//
// Nothing on this list is an exit-2 shape: a program the compiler cannot build is a refusal (exit 1) and
// a program whose module `llc` rejects is the compiler's own bug (ADR 0166). `compileRefusalText` goes
// through `Compile`, which is the same path the CLI's failure path takes, and `compileAndCheckLLVM` is
// run beside it so a refusal here can never quietly be a verifier failure wearing refusal clothes.
//
// None of these programs is on the record, and none is added by this commit: the record holds an ANSWER,
// and these shapes have none until the door opens. A record entry whose stdout the compiled leg cannot
// reproduce is a divergence, and the drift ledger is the wrong place to park a missing feature.

type callableValueShape struct {
	name string
	src  string
	// marker is the clause of the refusal this row is ABOUT. The sentence is long by house style; a row
	// pins the clause, and the failing test quotes the whole thing.
	marker string
	// row is the roadmap row that owns this shape.
	row string
	// why is what the door is missing, in the row's own words.
	why string
}

// callableReturnedShapes is Gap R.590's half: a `return` whose value is a callable. The callable may be a
// `lambda` the body spells, a `def` the program declared, or either side of a conditional — three spellings
// of "this function hands back a function", and the third is the one that reaches the door last.
var callableReturnedShapes = []callableValueShape{
	{
		name:   "a returned lambda",
		src:    "def make():\n    return lambda x: x * 2\n\nf = make()\nprint(f(3))\n",
		marker: "a lambda used as a value has no compiled representation",
		row:    "Gap R.590",
		why: "a lambda registered at a call site has a define to call; a lambda RETURNED has no site to " +
			"register at, so the value door has no word to hand back — Gap R.150/Gap R.151's door, on the return side",
	},
	{
		name:   "a returned lambda that reads its frame",
		src:    "def make(k):\n    return lambda x: x * k\n\nprint(make(3)(2))\n",
		marker: "a lambda used as a value has no compiled representation",
		row:    "Gap R.590",
		why: "the capture half on top of the value half: the word `k` holds has to travel with the callable " +
			"out of the frame that owns it (ADR 0370's capture parameter, asked of a return)",
	},
	{
		name:   "a returned declared function",
		src:    "def add1(x):\n    return x + 1\n\ndef idf(f):\n    return f\n\nprint(idf(add1)(2))\n",
		marker: "is called but is not a function this backend can build a call to",
		row:    "Gap R.590",
		why: "the callee is a call's answer here, so the call door is asked for a define it cannot name; the " +
			"sentence even reports the callee as `\"\"` — the position that asked has no name to print",
	},
	{
		name:   "a conditional over two declared functions",
		src:    "def add1(x):\n    return x + 1\n\ndef mul2(x):\n    return x * 2\n\ndef pick(n):\n    return add1 if n else mul2\n\nprint(pick(1)(3))\n",
		marker: "names function, which the compiled backend has no value for",
		row:    "Gap R.590",
		why: "a ternary arm that answers a callable needs the (payload, tag) pair ADR 0475 gave a container " +
			"answer, with a callable tag this backend has no kind for",
	},
}

// callableContainerShapes is Gap R.591's half: a callable stored in a list or a dict and called back out.
// The storing door and the calling door are different doors, which is why one program can produce two
// different sentences depending on which end it is written from.
var callableContainerShapes = []callableValueShape{
	{
		name:   "a lambda in a list",
		src:    "ops = [lambda x: x + 1]\nprint(ops[0](1))\n",
		marker: "a lambda used as a value has no compiled representation",
		row:    "Gap R.591",
		why: "the container's slot holds int64, float and the tagged value only (ADR 0368, ADR 0372) — a " +
			"callable has no slot to be written into",
	},
	{
		name:   "a declared function in a list",
		src:    "def add1(x):\n    return x + 1\n\nops = [add1]\nprint(ops[0](1))\n",
		marker: "names function, which the compiled backend has no value for",
		row:    "Gap R.591",
		why: "the name is the callable, and a name in a literal position is read as a value: the " +
			"defunctionalisation pass sees a function reaching a container and withdraws the whole program",
	},
	{
		name:   "a declared function as a dict value",
		src:    "def add1(x):\n    return x + 1\n\ntbl = {\"inc\": add1}\nprint(tbl[\"inc\"](1))\n",
		marker: "names function, which the compiled backend has no value for",
		row:    "Gap R.591",
		why: "same door, dict side — and the dict's slots are where the tag lives, so the missing kind is " +
			"the same missing kind as the list's",
	},
	{
		name:   "two reads of the same list",
		src:    "def add1(x):\n    return x + 1\n\nxs = [add1, add1]\nprint(xs[0](1) + xs[1](2))\n",
		marker: "names function, which the compiled backend has no value for",
		row:    "Gap R.591",
		why: "the shape that shows a per-call substitution cannot work: which define the read names is a " +
			"fact about the run-time element, not about the call site",
	},
}

// callableFieldShapes is Gap R.592's half — and the row's evidence that the refusal is filed at the wrong
// door today: `c.f(1)` is refused by the ATTRIBUTE road (`string-method f on a receiver that is not a
// text`), which is asked before anyone notices that what `f` holds is a function.
var callableFieldShapes = []callableValueShape{
	{
		name:   "a lambda in a field",
		src:    "class C:\n    def __init__(self):\n        self.f = lambda x: x + 1\n\nc = C()\nprint(c.f(1))\n",
		marker: "on a receiver that is not a text the compiler can read",
		row:    "Gap R.592",
		why: "the attribute door answers only a text receiver today (Gap I.2), so the callable never gets " +
			"to be the thing that is refused — the sentence is about the wrong half of the program",
	},
	{
		name:   "a declared function in a field",
		src:    "def add1(x):\n    return x + 1\n\nclass C:\n    def __init__(self):\n        self.f = add1\n\nc = C()\nprint(c.f(1))\n",
		marker: "on a receiver that is not a text the compiler can read",
		row:    "Gap R.592",
		why: "the same misattribution with a declared function: the store (`self.f = add1`) and the call " +
			"(`c.f(1)`) are two doors and neither one is the sentence the reader gets",
	},
}

// callableForwardShapes is Gap R.593's half: a callable parameter handed onward to another function's
// callable position. The pass proves a parameter is never used as a value by asking that every occurrence
// of it is a callee (or the recursion handing it back to itself); a FORWARD is a third occurrence kind,
// equally free of value-hood, and the proof has no clause for it — so the whole program withdraws and the
// call is refused by the emitter instead.
var callableForwardShapes = []callableValueShape{
	{
		name:   "a parameter forwarded one level",
		src:    "def double(x):\n    return x * 2\n\ndef use(f, x):\n    return f(x)\n\ndef outer(g, y):\n    return use(g, y)\n\nprint(outer(double, 3))\n",
		marker: "is not a declared function, so it has no name to substitute",
		row:    "Gap R.593",
		why: "`g` is `outer`'s own parameter: inside the clone minted for `double` it IS `double`, and the " +
			"entry resolver asks for a declared name instead of the clone's entry",
	},
	{
		name:   "a parameter forwarded two levels",
		src:    "def add1(x):\n    return x + 1\n\ndef forward(f, v):\n    return f(v)\n\ndef call_it(g, w):\n    return forward(g, w)\n\nprint(call_it(add1, 4))\n",
		marker: "is not a declared function, so it has no name to substitute",
		row:    "Gap R.593",
		why: "the nested case: `forward`'s site is inside a function that is itself cloned, so its entry " +
			"tuple is derived from another clone's — the pass collects sites from the source, not from clones",
	},
	{
		name:   "a forwarded parameter called twice",
		src:    "def add1(x):\n    return x + 1\n\ndef use(f, x):\n    return f(x)\n\ndef twice(g):\n    return use(g, 1) + use(g, 2)\n\nprint(twice(add1))\n",
		marker: "is not a declared function, so it has no name to substitute",
		row:    "Gap R.593",
		why: "two forwards of one parameter, which is also the shape that shows the rewrite must be keyed " +
			"per call rank and not per function",
	},
}

// callableAnsweredControls are the shapes the pass ALREADY rewrites. They are here because a filed row that
// only pins refusals cannot tell a door from a wall: these three print numbers today, and a widening of the
// pass that costs any of them is a regression this file catches.
var callableAnsweredControls = []struct {
	name string
	src  string
	want string
}{
	{
		name: "a callable argument, called",
		src:  "def add1(x):\n    return x + 1\n\ndef apply_(f, v):\n    return f(v)\n\nprint(apply_(add1, 1))\n",
		want: "2",
	},
	{
		name: "a lambda argument, called",
		src:  "def use(f, x):\n    return f(x)\n\nprint(use(lambda v: v * 7, 3))\n",
		want: "21",
	},
	{
		name: "a callable parameter, called twice",
		src:  "def add1(x):\n    return x + 1\n\ndef twice(f, v):\n    return f(f(v))\n\nprint(twice(add1, 5))\n",
		want: "7",
	},
}

func callableShapes() []callableValueShape {
	var all []callableValueShape
	all = append(all, callableReturnedShapes...)
	all = append(all, callableContainerShapes...)
	all = append(all, callableFieldShapes...)
	all = append(all, callableForwardShapes...)
	return all
}

// TestACallableThatReachesAValuePositionIsRefusedByNamedSentence is the filed leg: every shape's refusal
// says the clause this row claims it says, and no shape reaches `llc`.
func TestACallableThatReachesAValuePositionIsRefusedByNamedSentence(t *testing.T) {
	t.Parallel()
	for _, r := range callableShapes() {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			text := compileRefusalText(t, r.src)
			if !strings.Contains(text, r.marker) {
				t.Fatalf("%s: the refusal does not say %q\n%s\nfull output:\n%s", r.row, r.marker, r.why, text)
			}
			if strings.Contains(text, "exit 2") || strings.Contains(text, "verifier") {
				t.Fatalf("%s: a refusal that names the verifier is the compiler's own bug (ADR 0166): %s", r.row, text)
			}
		})
	}
}

// TestARefusedCallableShapeStillRefusesTheBuild is the exit-code leg: these programs stop the compile at
// exit 1. A shape that starts answering here has opened a door, and the row that files it must be deleted
// in the same commit that made it happen.
func TestARefusedCallableShapeStillRefusesTheBuild(t *testing.T) {
	t.Parallel()
	for _, r := range callableShapes() {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Compile(r.src); err == nil {
				t.Fatalf("%s: this shape compiles now — delete the row and close the half of L11.7 it names\n%s", r.row, r.src)
			}
		})
	}
}

// TestTheCallablePassStillRewritesWhatItRewrote is the wall-and-door control: the three higher-order shapes
// the defunctionalisation pass answers must keep answering, at the same numbers, whatever this round does
// to the four doors above (ADR 0273's rule that a gate must not cost a program that runs).
func TestTheCallablePassStillRewritesWhatItRewrote(t *testing.T) {
	t.Parallel()
	for _, r := range callableAnsweredControls {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			got, trap := runForParity(t, r.src)
			if trap != "" {
				t.Fatalf("the program raised %s; it answers %q today\n%s", trap, r.want, r.src)
			}
			if got != r.want {
				t.Fatalf("compiled answers %q, want %q\n%s", got, r.want, r.src)
			}
		})
	}
}

// TestTheFourCallableValueRowsAreEachReachable keeps the filing honest: four rows, four distinct causes, and
// a row whose shapes all come from one door is one row pretending to be four. `Gap R.592` is the row that
// must NOT share a door with the others — its refusal comes from the attribute road, which is the finding.
func TestTheFourCallableValueRowsAreEachReachable(t *testing.T) {
	t.Parallel()
	byRow := map[string]map[string]bool{}
	for _, r := range callableShapes() {
		if byRow[r.row] == nil {
			byRow[r.row] = map[string]bool{}
		}
		byRow[r.row][r.marker] = true
		if len(r.src) == 0 {
			t.Fatalf("%s/%s: a row with no program is not a measured row", r.row, r.name)
		}
		if r.why == "" {
			t.Fatalf("%s/%s: a row with no cause is a wish", r.row, r.name)
		}
	}
	for _, row := range []string{"Gap R.590", "Gap R.591", "Gap R.592", "Gap R.593"} {
		if len(byRow[row]) == 0 {
			t.Fatalf("%s has no shape pinned by this file", row)
		}
	}
	if len(byRow["Gap R.592"]) != 1 {
		t.Fatalf("Gap R.592 is the row whose point is that ONE door answers for all of them; it now speaks %d sentences", len(byRow["Gap R.592"]))
	}
	if byRow["Gap R.592"]["a lambda used as a value has no compiled representation"] {
		t.Fatalf("Gap R.592's whole finding is that the attribute road answers first; if the value door is now " +
			"reached, that is a different defect and needs its own row")
	}
}

// TestWhatTheCallableValueDoorsStillCannotReachIsFiledNotFixed is the residue table: the shapes this round
// measured, decided it could not reach, and filed instead of absorbing into the row above.
func TestWhatTheCallableValueDoorsStillCannotReachIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	residue := []struct {
		name string
		src  string
		mark string
	}{
		{
			name: "a callable in a ternary arm beside an int",
			src:  "def add1(x):\n    return x + 1\n\ndef pick(n):\n    return add1 if n else 0\n\nprint(pick(1))\n",
			mark: "names function, which the compiled backend has no value for",
		},
		{
			// print of a callable can never match the reference byte for byte: the reference prints the
			// object's ADDRESS. This is a separate debt from the four rows above and must not be closed
			// by them — a row whose definition of done is `print(f)` would be uncloseable.
			name: "print of a callable",
			src:  "def add1(x):\n    return x + 1\n\nprint(add1)\n",
			mark: "names function, which the compiled backend has no value for",
		},
		{
			name: "a callable compared for equality",
			src:  "def add1(x):\n    return x + 1\n\nprint(add1 == add1)\n",
			mark: "names function, which the compiled backend has no value for",
		},
		{
			name: "a callable called through a subscript of a call's answer",
			src:  "def add1(x):\n    return x + 1\n\ndef ops():\n    return [add1]\n\nprint(ops()[0](1))\n",
			mark: "names function, which the compiled backend has no value for",
		},
	}
	for _, r := range residue {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			text := compileRefusalText(t, r.src)
			if !strings.Contains(text, r.mark) {
				t.Fatalf("the filed refusal moved: expected %q\n%s", r.mark, text)
			}
		})
	}
}

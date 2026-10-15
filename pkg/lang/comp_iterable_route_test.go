package lang

// A comprehension walks what the program computes, and it says so when it cannot see it.
//
// `compItems` derives the items a comprehension unrolls at compile time: an inline list literal, or
// `range(start, stop[, step])` with constant bounds. Its dispatcher asked whether the iterable *was a call*
// and, if so, handed it to that derivation — which opens `Args[0]` to read a bound (roadmap Gap R.291,
// ADR 0357). Three shapes, all measured against the binary at HEAD before any of this was written:
//
//	def g():                 # 0 args  → panic: index out of range [0] with length 0   (exit 2)
//	    return [1,2,3]
//	print([x for x in g()])
//
//	def z():                 # 0 args  → the same panic
//	    return {"a": 1}
//	print([k for k in z()])
//
//	def h(n):                # 1 arg   → range(Args[0]), whatever the callee returns
//	    return n + 1
//	print([x for x in h(3)]) # printed [0, 1, 2]; CPython raises TypeError
//
// The third is the interesting one, and the reason the fix is a route and not a nil check. The dispatcher
// believed that a call had a compile-time length because it was a call, so it read the call's OWN argument as
// the range stop. That answered `[x for x in range(3)]` and `[x for x in h(3)]` alike, whichever `h` returned
// — `[n*n]` gave `[0, 1, 2]` where CPython gives `[9]`. An answer that arrives by a rule that reads the wrong
// thing is not an answer; the ladder protects answers, and this was not one.
//
// So: `range(...)` with at least one argument is the only call with a compile-time length, and every other
// call goes to the runtime loop — where it asks the `for` road's three iterable questions before lowering, so
// the shapes `for v in sorted(xs)` has been answering for cycles are answered by `[… for … in sorted(xs)]`
// too. What neither road can prove is refused with the half that is missing named.

import (
	"strings"
	"testing"
)

// compileNeverPanics is the regression the exit-2 shapes deserve: a compiler crash is not a refusal, and no
// message about a construct being unsupported is reachable from a panic.
func compileNeverPanics(t *testing.T, src string) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("compiling panicked (%v) — a crash is not a refusal, and the program below has a right answer:\n%s", r, src)
		}
	}()
	if _, err := Compile(src); err != nil {
		t.Fatalf("the comprehension refused a program the reference answers: %v\n%s", err, src)
	}
}

// TestAComprehensionWalksWhatACallAnswers is the answered half: a call as the iterable, judged against the
// reference through the compiler, including the two shapes that used to panic the toolchain.
func TestAComprehensionWalksWhatACallAnswers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			name: "a_zero_argument_call_answering_a_list",
			src:  "def g():\n    return [1, 2, 3]\nprint([x for x in g()])\n",
			want: "[1, 2, 3]\n",
		},
		{
			name: "a_zero_argument_call_answering_a_dict",
			src:  "def z():\n    return {\"a\": 1}\nprint([k for k in z()])\n",
			want: "['a']\n",
		},
		{
			name: "a_call_reading_its_argument_as_a_list",
			src:  "def f(xs):\n    return [y for y in xs]\nprint(f([1, 2, 3]))\n",
			want: "[1, 2, 3]\n",
		},
		{
			name: "a_call_whose_value_is_a_square",
			src:  "def h(n):\n    return [n * n]\nprint([x for x in h(3)])\n",
			want: "[9]\n",
		},
		{
			name: "sorted_over_a_literal",
			src:  "print([x for x in sorted([3, 1, 2])])\n",
			want: "[1, 2, 3]\n",
		},
		{
			name: "sorted_over_a_variable",
			src:  "xs = [3, 1, 2]\nprint([x for x in sorted(xs)])\n",
			want: "[1, 2, 3]\n",
		},
		{
			name: "a_container_variable",
			src:  "xs = [1, 2, 3]\nprint([x for x in xs])\n",
			want: "[1, 2, 3]\n",
		},
		{
			name: "a_container_variable_with_a_condition",
			src:  "xs = [1, 2, 3]\nprint([x for x in xs if x > 1])\n",
			want: "[2, 3]\n",
		},
		{
			name: "a_range_is_still_unrolled",
			src:  "print([x * 2 for x in range(3)])\n",
			want: "[0, 2, 4]\n",
		},
		{
			name: "a_list_literal_is_still_unrolled",
			src:  "print([x + 1 for x in [1, 2]])\n",
			want: "[2, 3]\n",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			compileNeverPanics(t, tc.src)
			got := compiledOut(t, tc.src)
			if got != tc.want {
				t.Fatalf("the compiled leg printed %q, want %q:\n%s", got, tc.want, tc.src)
			}
			if ref := referenceOut(t, tc.src); ref != tc.want {
				t.Fatalf("the reference prints %q, so this row's expectation is invented:\n%s", ref, tc.src)
			}
		})
	}
}

// The refusals, kept honest: the iterable the backend cannot prove is a container is refused with the missing
// half named, and never walked as an empty object. The second row is the one the route exists for — the
// compiled backend used to print `[0, 1, 2]` for it at exit 0, because the rule read the call's own argument
// as a range stop (the wrong answer class, roadmap Gap R.291).
func TestAComprehensionOverAnUnprovableCallRefusesInsteadOfWalkingNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		src    string
		refuse string
		ref    string // what the reference does, kept in the failure text
	}{
		{
			name:   "a_builtin_the_container_scan_does_not_hold",
			src:    "print([x for x in list([1, 2])])\n",
			refuse: "does not name a container the compiled backend can prove",
			ref:    "[1, 2]",
		},
		{
			name:   "a_call_answering_a_number",
			src:    "def h(n):\n    return n + 1\nprint([x for x in h(3)])\n",
			refuse: "does not name a container the compiled backend can prove",
			ref:    "TypeError: 'int' object is not iterable",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, tc.src)
			if !refused {
				t.Fatalf("%s answered %q where the reference does %q; walking an object the compiler never proved "+
					"is how `[x for x in h(3)]` printed `[0, 1, 2]` (roadmap Gap R.291):\n%s", tc.name, out, tc.ref, tc.src)
			}
			if !strings.Contains(out, tc.refuse) {
				t.Fatalf("%s refused without naming the missing proof (%q): %s", tc.name, tc.refuse, out)
			}
		})
	}
}

// The predicate the route turns on, asked as a function: which calls have a compile-time length. The shapes
// that used to fall into `Args[0]` are the rows that matter, and a gate tested only through programs that
// already reach it proves nothing about the calls it turns away.
func TestOnlyRangeHasACompileTimeLength(t *testing.T) {
	t.Parallel()
	rng := func(args ...Expr) Expr { return &Call{Fn: &Name{Value: "range"}, Args: args} }
	for _, tc := range []struct {
		name string
		iter Expr
		want bool
	}{
		{"range with a stop", rng(&IntLit{Value: 3}), true},
		{"range with start and stop", rng(&IntLit{Value: 1}, &IntLit{Value: 3}), true},
		{"range with a step", rng(&IntLit{Value: 1}, &IntLit{Value: 9}, &IntLit{Value: 2}), true},
		{"range with no argument at all", rng(), false},
		{"range with a name as its stop", rng(&Name{Value: "n"}), true},
		{"another call with an argument", &Call{Fn: &Name{Value: "f"}, Args: []Expr{&IntLit{Value: 3}}}, false},
		{"another call with none", &Call{Fn: &Name{Value: "g"}}, false},
		{"a method-shaped call", &Call{Fn: &Attr{Obj: &Name{Value: "d"}, Name: &Name{Value: "keys"}}, Args: []Expr{&IntLit{Value: 1}}}, false},
		{"not a call at all", &ListLit{Elems: []Expr{&IntLit{Value: 1}}}, false},
		{"nothing", nil, false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compIterIsStaticRange(tc.iter); got != tc.want {
				t.Errorf("compIterIsStaticRange = %v, want %v", got, tc.want)
			}
		})
	}
}

// The zero-argument call is the crash, so it gets the check without a runner in front of it: the derivation
// must refuse it, not index into it.
func TestCompItemsRefusesAZeroArgumentCallRatherThanReadingItsFirstArgument(t *testing.T) {
	t.Parallel()
	g := &irGen{}
	c := &Comp{
		Kind:   CompList,
		Elems:  []Expr{&Name{Value: "x"}},
		ForVar: &Name{Value: "x"},
		Iter:   &Call{Fn: &Name{Value: "z"}},
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("compItems panicked on a zero-argument call (%v) — this is the exit-2 shape of roadmap Gap R.291", r)
		}
	}()
	if _, _, err := g.compItems(c); err == nil {
		t.Fatalf("compItems enumerated items for a call that is not range(); the runtime loop owns this shape")
	}
}

// TestTheRecordLegAnswersEveryWalkedIterable is the record leg of the same route.
//
// The seven programs below are not invented for the occasion: each one is in
// `testdata/interpreter-golden.json` with the answer the retired engine gave, and each one asks a
// comprehension whose iterable is a NAME — a list the program grew with `append`, a set it grew with
// `add`, a dict walked for its keys, a list whose materialisation the mutation forced. That is the
// family the route exists for, and the record shows the engine never refused a single one of them:
// a comprehension over a variable was an ordinary thing to ask.
//
// `runGoldenStdout` compiles the source, runs it, and compares what came out against the record, so a
// case here fails on either half — a compiled answer that moved, or a record that disappeared. The
// mixed-key dict row (`['a', 2]`) and the mixed set row (`[1, 'a']`) are the two that need the tag the
// loop binds beside the payload (ADR 0244's pair, one door over): read the payload alone and they print
// numbers where the record prints a word and a container.
func TestTheRecordLegAnswersEveryWalkedIterable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{"dict keys of two kinds", "d = {1: \"x\", \"k\": 2}\nout = [v for v in d]\nprint(out)\n", "[1, 'k']"},
		{"dict built by puts", "d = {}\nd[\"a\"] = 1\nd[\"b\"] = 2\nout = [k for k in d]\nprint(out)\n", "['a', 'b']"},
		{"dict with a mixed key", "d = {}\nd[\"a\"] = 1\nd[2] = \"b\"\nout = [k for k in d]\nprint(out)\n", "['a', 2]"},
		{"list grown by append", "names = [\"a\", \"b\"]\nnames.append(\"c\")\nout = [n for n in names]\nprint(out)\nprint(out[0])\n", "['a', 'b', 'c']\na"},
		{"set of three kinds", "sa = {1, \"a\", None}\nout = [x for x in sa]\nprint(len(out))\n", "3"},
		{"list materialised by mutation", "xs = [1, 2, 3]\nxs.append(9)\nprint([x * 2 for x in xs])\n", "[2, 4, 6, 18]"},
		{"set grown by add", "sa = set()\nsa.add(1)\nsa.add(\"a\")\nout = [x for x in sa]\nprint(out)\n", "[1, 'a']"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := runGoldenStdout(t, tc.src)
			if err != nil {
				t.Fatalf("%s: the walked iterable did not answer against the record: %v\n%s", tc.name, err, tc.src)
			}
			if strings.TrimSpace(got) != tc.want {
				t.Errorf("%s: the record leg answers %q, the compiled program printed %q\n%s", tc.name, tc.want, got, tc.src)
			}
		})
	}
}

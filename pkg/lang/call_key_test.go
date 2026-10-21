package lang

import (
	"regexp"
	"strings"
	"testing"
)

// call_key_test.go — a `key=` argument is asked what it is, and a key the compiler can call is called
// where the comparison is made (roadmap L11.7 · the ordering builtins' callable argument; closes Gap
// R.249; ADR 0340).
//
// Before this row the option arguments of `sorted` were read by POSITION: the second argument was the
// reverse flag wherever it stood. Measured against the reference, that produced every bad answer the
// contract ranks, in one family of programs:
//
//	print(sorted([3,1,2], key=abs))                # [1, 2, 3]        this backend: refused, blaming reverse=
//	print(sorted([3,1,-2], key=abs, reverse=True)) # [3, -2, 1]       this backend: [-2, 1, 3] AT EXIT 0
//	print(sorted([3,1,2], key=3))                  # TypeError        this backend: [3, 2, 1] AT EXIT 0
//	print(min(3,1,-2, key=abs))                    # -2               this backend: llc rejects %_abs → EXIT 2
//	print(min([3,1,2], key=abs))                   # 1                this backend: refused, "not a container among values"
//
// The three verdicts a door can honestly give are all in this file: CALL (the key is a function, so the
// ordinary call road is handed `key(element)` and answers it exactly as it answers that call anywhere
// else in the program — arity mistakes and `len(3)` included), RAISE (the reference stops the program
// with a TypeError, so this backend raises it with `raiseTo` and the program's own `except TypeError:`
// catches it), and REFUSE (the half that is missing is named: a run-time list's elements, a key that
// answers a float, a lambda, a key whose kind no notebook can read).

// keyedAnswered programs the row newly answers. `want` is what CPython prints for the same source,
// re-measured whenever this file is extended; each row is asked of the compiler, of the JIT, and in
// integration/call_key_test.go of the CLI.
var keyedAnswered = []struct {
	src  string
	want string
}{
	// the five programs above, in the order they were measured
	{"print(sorted([3, 1, 2], key=abs))", "[1, 2, 3]"},
	{"print(sorted([-3, 1, 2], key=abs))", "[1, 2, -3]"},
	{"def f(x):\n    return 0-x\nprint(sorted([3, 1, 2], key=f))", "[3, 2, 1]"},
	{"print(sorted([3, 1, -2], key=abs, reverse=True))", "[3, -2, 1]"},
	{"print(min(3, 1, -2, key=abs))", "1"},
	{"print(max(3, 1, -2, key=abs))", "3"},
	{"print(min([3, 1, 2], key=abs))", "1"},
	{"print(max([3, 1, 2], key=abs))", "3"},
	// the key answers text: the comparator is @rt_elem_gt's text mode, which reads the text and not
	// the intern table's arrival order (ADR 0248's rule, at the ordering door)
	{"print(sorted([\"bbb\", \"a\", \"cc\"], key=len))", "['a', 'cc', 'bbb']"},
	{"print(sorted([\"bbb\", \"a\", \"cc\"], key=len, reverse=True))", "['bbb', 'cc', 'a']"},
	{"print(min([\"aaa\", \"b\", \"cc\"], key=len))", "b"},
	{"print(max([\"aaa\", \"b\", \"cc\"], key=len))", "aaa"},
	{"print(sorted([10, 9, 1], key=str))", "[1, 10, 9]"},
	// STABILITY: equal keys keep the order the program wrote, which is the tie half of the rank
	// formula — count the earlier equal keys, do not compare the elements themselves
	{"print(sorted([2, 1, 2, 1], key=abs))", "[1, 1, 2, 2]"},
	{"print(sorted([3, 3, 1], key=abs))", "[1, 3, 3]"},
	{"print(sorted([1, 1, 1], key=abs))", "[1, 1, 1]"},
	{"print(sorted([3, 1, 2], key=abs, reverse=False))", "[1, 2, 3]"},
	// `key=None` is the reference's IDENTITY key, not a non-callable: the reference runs the program,
	// so the door steps aside and lets the unkeyed road answer it.
	{"print(sorted([3, 1, 2], key=None))", "[1, 2, 3]"},
	{"print(min([3, 1, -2], key=None))", "-2"},
	{"print(sorted([3, 1, 2], key=None, reverse=True))", "[3, 2, 1]"},
	{"for v in sorted([3, 1, 2], key=None):\n    print(v)", "1\n2\n3"},
	{"print(sorted([7], key=abs))", "[7]"},
	{"print(sorted([], key=abs))", "[]"},
	// a parameter with a default is callable with one argument, which is exactly how the reference
	// calls a key — the door asks the arity, it does not guess it
	{"def h(a, b=1):\n    return 0-a\nprint(sorted([3, 1, 2], key=h))", "[3, 2, 1]"},
	// the answer is a list like any other: length, position, sum and a binding all read it
	{"print(len(sorted([3, 1, 2], key=abs)))", "3"},
	{"print(sorted([3, 1, 2], key=abs)[1])", "2"},
	{"print(sorted([3, 1, 2], key=abs)[0] + 1)", "2"},
	{"print(sum(sorted([3, 1, 2], key=abs)))", "6"},
	{"xs = sorted([3, 1, 2], key=abs)\nprint(xs)", "[1, 2, 3]"},
	{"for v in sorted([3, 1, 2], key=abs):\n    print(v)", "1\n2\n3"},
	{"def f(x):\n    return 0-x\nfor v in sorted([3, 1, 2], key=f):\n    print(v)", "3\n2\n1"},
	// A `key=lambda ...` is answered since the defunctionalisation pass gave the lambda a declaration of its
	// own: the key door calls a named callable by the ordinary call road (ADR 0340), and the pass is what
	// supplies the name (roadmap L11.7, ADR 0345).
	{"print(sorted([3, 1, 2], key=lambda x: 0-x))", "[3, 2, 1]"},
	{"print(max([1, 3, 2], key=lambda x: 0-x))", "1"},
	{"print(min([3, 1, 2], key=lambda x: 0-x))", "3"},
	{"for v in sorted([3, 1, 2], key=lambda x: 0-x):\n    print(v)", "3\n2\n1"},
	{"for v in sorted([\"bbb\", \"a\", \"cc\"], key=len):\n    print(v)", "a\ncc\nbbb"},
	{"for v in sorted([3, 1, -2], key=abs, reverse=True):\n    print(v)", "3\n-2\n1"},
	{"for v in sorted([2, 1, 2, 1], key=abs):\n    print(v)", "1\n1\n2\n2"},
	// The builtins whose ONE-argument answer this backend already emits as a word the comparator orders:
	// `round` answers the whole number the reference's one-argument `round` answers, `bool` answers the
	// verdict the payload already is, `chr` answers a text. The door used to refuse all three beside the
	// builtins that really do answer a double or a container (roadmap Gap R.255).
	{"print(sorted([3, 1, 2], key=round))", "[1, 2, 3]"},
	{"print(max([3, 1, 2], key=round))", "3"},
	{"print(min([3, 1, 2], key=round))", "1"},
	{"print(sorted([3, 1, 2], key=bool))", "[3, 1, 2]"}, // every element is truthy: the order is the stable one
	{"print(sorted([0, 3, 0, 1], key=bool))", "[0, 0, 3, 1]"},
	{"print(sorted([3, 1, 2], key=chr))", "[1, 2, 3]"},
	{"print(sorted([3, 1, 2], key=chr, reverse=True))", "[3, 2, 1]"},
	// the raise is catchable, which is the difference between the reference's TypeError and a refusal
	{"try:\n    print(sorted([3, 1, 2], key=3))\nexcept TypeError:\n    print(\"caught\")", "caught"},
	{"try:\n    print(min([3, 1], reverse=True))\nexcept TypeError:\n    print(\"caught\")", "caught"},
}

// keyedRaises are the programs the REFERENCE stops. Each is emitted as a real typed raise, so the
// class and the message are the reference's words and the program's own `except` reaches them
// (ADR 0214, ADR 0228) — a compile-time refusal here would be an answer that cannot be caught.
var keyedRaises = []struct {
	src     string
	class   string
	message string
}{
	{"print(sorted([3, 1, 2], key=3))", "TypeError", "'int' object is not callable"},
	{"print(min([3, 1, 2], key=3))", "TypeError", "'int' object is not callable"},
	{"print(max([3, 1, 2], key=3))", "TypeError", "'int' object is not callable"},
	{"print(sorted([3, 1, 2], key=1.5))", "TypeError", "'float' object is not callable"},
	{"print(sorted([3, 1, 2], key=\"abs\"))", "TypeError", "'str' object is not callable"},
	{"print(sorted([3, 1, 2], key=True))", "TypeError", "'bool' object is not callable"},
	{"xs = [1, 2]\nprint(sorted([3, 1], key=xs))", "TypeError", "'list' object is not callable"},
	{"def z():\n    return 1\nprint(sorted([1, 2], key=z))", "TypeError", "z() takes 0 positional arguments but 1 was given"},
	{"def g(a, b):\n    return a\nprint(sorted([1, 2], key=g))", "TypeError", "g() missing 1 required positional argument: 'b'"},
	{"print(min([3, 1], reverse=True))", "TypeError", "'reverse' is an invalid keyword argument for min()"},
	{"print(max([3, 1], key=abs, reverse=True))", "TypeError", "'reverse' is an invalid keyword argument for max()"},
	{"print(sorted([1], cmp=3))", "TypeError", "'cmp' is an invalid keyword argument for sort()"},
	{"print(min([], key=abs))", "ValueError", "min() iterable argument is empty"},
	{"print(max([], key=abs))", "ValueError", "max() iterable argument is empty"},
}

// keyedRefused are the keys this door cannot call. Every one names what is missing, and each has a
// roadmap row: the run-time list's elements and the float key are the tagged value word's (L11.1,
// Gap R.254, Gap R.255), the unresolvable name is L11.7's. A `key=lambda ...` is no longer one of them —
// the pass that names a lambda argument (ADR 0345) moved it to keyedAnswered above.
var keyedRefused = []struct {
	src   string
	names string
	owns  string
}{
	{
		src:   "xs = [3, 1, 2]\nprint(sorted(xs, key=abs))",
		names: "sorted(xs, key=abs)",
		owns:  "L11.7",
	},
	{
		src:   "def f(x):\n    return x / 2\nprint(sorted([3, 1, 2], key=f))",
		names: "key=f",
		owns:  "Gap R.248",
	},
	{
		src:   "print(sorted([3, 1, 2], key=sum))",
		names: "key=sum",
		owns:  "Gap R.248",
	},
	{
		src:   "print(sorted([3, 1, 2], key=print))",
		names: "key=print",
		owns:  "L11.7",
	},
	{
		src:   "def f(x):\n    return x\nprint(sorted([3, 1, 2], key=f(1)))",
		names: "key=f(1)",
		owns:  "L11.7",
	},
	{
		src:   "print(sorted([3, 1, 2], key=print(1)))",
		names: "key=print(1)",
		owns:  "L11.7",
	},
	{
		src:   "print(sorted([3, 1, 2], key=hash))",
		names: "key=hash",
		owns:  "L11.7",
	},
	// (`print(max([3, 1, 2], key=round))` sat here. `round(x)` with the one argument a key is called
	// with answers an integer in this backend — the two-argument form, which answers a float, is not the
	// call a key makes — so the refusal's premise was stale and the row moved to `keyedAnswered`.)
	//
	// The element's half of the same missing word: the keyed door returns the element it picks in ONE
	// word, so a float element is handed back as its payload. These refused at the door rather than
	// answering `[0.0, 0.0]` at exit 0 (or, for `key=abs`, reaching `llc` at exit 2 — ADR 0166).
	{
		src:   "def f(x):\n    return 1\nprint(sorted([2.5, 1.5], key=f))",
		names: "key=f",
		owns:  "Gap R.248",
	},
	{
		src:   "print(sorted([2.5, 1.5], key=abs))",
		names: "key=abs",
		owns:  "Gap R.248",
	},
	{
		src:   "print(sorted([2.5, 1.5], key=str))",
		names: "key=str",
		owns:  "Gap R.248",
	},
	{
		src:   "print(sorted([3.0, 1.0, 2.0], key=round))",
		names: "key=round",
		owns:  "Gap R.248",
	},
}

// keyedUnchanged is the negative control: the unkeyed answers, the documented repeat-flag reading of a
// positional second argument, and the roads that already lowered these calls. None of them may move,
// and a row in this table that starts refusing is as much a failure as a row above that stops answering.
var keyedUnchanged = []struct {
	src  string
	want string
}{
	{"print(sorted([3, 1, 2]))", "[1, 2, 3]"},
	{"print(sorted([3, 1, 2], reverse=True))", "[3, 2, 1]"},
	{"print(sorted([3, 1, 2], True))", "[3, 2, 1]"}, // this language's documented reading of a constant flag
	{"print(sorted([\"b\", \"a\"]))", "['a', 'b']"},
	{"print(sorted([3, 1, 2][0:2]))", "[1, 3]"},
	{"xs = [3, 1, 2]\nprint(sorted(xs))", "[1, 2, 3]"},
	{"print(sorted([3, 1, 2])[0])", "1"},
	{"print(min(3, 1, 2))", "1"},
	{"print(max(3, 1, 2))", "3"},
	{"print(min([3, 1, 2]))", "1"},
	{"print(max([3, 1, 2]))", "3"},
	{"print(min(\"b\", \"a\"))", "a"},
	{"print(max([1.5, 2.5]))", "2.5"},
	{"print(len(sorted([3, 1, 2])))", "3"},
	{"print(sum(sorted([3, 1, 2])))", "6"},
	{"def sorted(xs):\n    return [1]\nprint(sorted([3, 2]))", "[1]"}, // the program's own name is not the builtin
	{"xs = [3, 1, 2]\nfor v in sorted(xs): print(v)", "1\n2\n3"},
	{"xs = [3, 1, 2]\nfor v in reversed(xs): print(v)", "2\n1\n3"},
}

// TestTheKeyTheCompilerCanCallIsCalled runs the answered table on both legs the compiled answer is
// judged on: the module the compiler writes, and the JIT that executes it.
func TestTheKeyTheCompilerCanCallIsCalled(t *testing.T) {
	t.Parallel()
	for _, tt := range keyedAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); the reference prints %q", tt.src, got, tt.want)
			}
			if strings.TrimSpace(got) != tt.want {
				t.Fatalf("%q printed %q, want %q", tt.src, strings.TrimSpace(got), tt.want)
			}
			out, trap := runForParity(t, tt.src)
			if trap != "" {
				t.Fatalf("%q raised %s; the reference prints %q", tt.src, trap, tt.want)
			}
			if out != tt.want {
				t.Fatalf("%q printed %q through the JIT, want %q", tt.src, out, tt.want)
			}
		})
	}
}

// TestTheKeyThatIsNotCallableRaisesWhatTheReferenceRaises pins the class AND the message: the words a
// reader acts on are the reference's, and they have to survive as an exception the program can catch.
func TestTheKeyThatIsNotCallableRaisesWhatTheReferenceRaises(t *testing.T) {
	t.Parallel()
	for _, tt := range keyedRaises {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			class, message := jitTrap(t, tt.src)
			if class != tt.class {
				t.Fatalf("%q raised %s; the reference raises %s", tt.src, class, tt.class)
			}
			if message != tt.message {
				t.Fatalf("%q raised %s: %q, want %q", tt.src, class, message, tt.message)
			}
		})
	}
}

// TestTheKeyTheDoorCannotCallIsRefusedByName holds the refusals to ADR 0249: the sentence has to say
// which key was refused and which half is missing, with the row that owns it.
func TestTheKeyTheDoorCannotCallIsRefusedByName(t *testing.T) {
	t.Parallel()
	for _, tt := range keyedRefused {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, tt.src)
			if !refused {
				t.Fatalf("%q answered %q at exit 0 where the row expects a refusal", tt.src, out)
			}
			if !strings.Contains(out, tt.names) {
				t.Fatalf("%q refused with %q, which does not name %q", tt.src, out, tt.names)
			}
			if !strings.Contains(out, tt.owns) || !strings.Contains(out, "L11.") {
				t.Fatalf("%q refused without citing the row that owns the missing half (%q):\n%s", tt.src, tt.owns, out)
			}
		})
	}
}

// TestTheUnkeyedOrderStillAnswers runs the negative control on both legs.
func TestTheUnkeyedOrderStillAnswers(t *testing.T) {
	t.Parallel()
	for _, tt := range keyedUnchanged {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); this row did not touch it and the reference prints %q", tt.src, got, tt.want)
			}
			if strings.TrimSpace(got) != tt.want {
				t.Fatalf("%q printed %q, want %q", tt.src, strings.TrimSpace(got), tt.want)
			}
			out, trap := runForParity(t, tt.src)
			if trap != "" {
				t.Fatalf("%q raised %s; the reference prints %q", tt.src, trap, tt.want)
			}
			if out != tt.want {
				t.Fatalf("%q printed %q through the JIT, want %q", tt.src, out, tt.want)
			}
		})
	}
}

// TestTheKeyIsCalledByTheRoadThatAnswersAnyCall is the structural witness for the row's design: the
// key is not called through a door this file built. A user key appears in the module as a call to the
// program's own function, and the order is written by the comparator the unkeyed sorter already used.
func TestTheKeyIsCalledByTheRoadThatAnswersAnyCall(t *testing.T) {
	t.Parallel()
	src := "def f(x):\n    return 0-x\nprint(sorted([3, 1, 2], key=f))"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	for _, need := range []string{"@rt_elem_gt(", "@rt_put_elem(", "@rt_append_tagged(", "@rt_alloc("} {
		if !strings.Contains(res.IR, need) {
			t.Fatalf("the keyed order did not go through %s:\n%s", need, res.IR)
		}
	}
	// the unkeyed sorter's helper is NOT how a keyed order is written — one ordering, one road each.
	// The runtime text may define @rt_sort for other roads; the keyed module must not CALL it.
	if regexp.MustCompile(`call\s+[^@]*@rt_sort\(`).FindString(res.IR) != "" {
		t.Fatalf("a keyed order called the unkeyed sorter's @rt_sort; the two roads must not trade places")
	}
	for _, src := range []string{
		"print(sorted([3, 1, -2], key=abs, reverse=True))",
		"print(min([3, 1, -2], key=abs))",
	} {
		res, err := Compile(src)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if regexp.MustCompile(`call\s+[^@]*@rt_sort\(`).FindString(res.IR) != "" {
			t.Fatalf("%q called the unkeyed sorter's @rt_sort; keyed orders rank their own elements", src)
		}
	}
}

// TestTheOptionIsReadByNameNotByPosition is the measurement that opened the row: an option in the
// second slot used to be the reverse flag, which is how a program the reference stops printed a list
// at exit 0. These are the three shapes, pinned as behaviours and not as sentences.
func TestTheOptionIsReadByNameNotByPosition(t *testing.T) {
	t.Parallel()
	rows := []struct {
		src  string
		want string
	}{
		{"print(sorted([3, 1, -2], key=abs, reverse=True))", "[3, -2, 1]"},
		{"print(sorted([3, 1, 2], key=abs))", "[1, 2, 3]"},
	}
	for _, tt := range rows {
		t.Run(tt.src, func(t *testing.T) {
			t.Parallel()
			out, trap := runForParity(t, tt.src)
			if trap != "" {
				t.Fatalf("%q raised %s", tt.src, trap)
			}
			if out != tt.want {
				t.Fatalf("%q printed %q, want %q — reverse=True and a key are two options, read by name (ADR 0340)", tt.src, out, tt.want)
			}
		})
	}
	if class, _ := jitTrap(t, "print(sorted([3, 1, 2], key=3))"); class != "TypeError" {
		t.Fatalf("a non-callable key raised %s; the reference stops the program with TypeError", class)
	}
}

// TestWhatTheKeyRowLeavesOwed holds the refusals that a future cycle must MOVE: if one of these starts
// answering without this file changing, the row and this test move together (ADR 0233's rule that an
// answer is never allowed to arrive unlogged).
func TestWhatTheKeyRowLeavesOwed(t *testing.T) {
	t.Parallel()
	for _, tt := range keyedRefused {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, tt.src)
			if !refused {
				t.Fatalf("%q answered %q at exit 0; roadmap %s filed this as a refusal, so the row moved and this pin must move with it", tt.src, out, tt.owns)
			}
		})
	}
}

// TestTheKeyedOrdersAreOnTheRecord asks the record leg for the programs this row answers and the traps
// it raises, so an answer that only exists on one leg cannot pass (AGENTS.md's two witness legs). A
// missing record FAILS the case, which is what keeps this coverage from being deleted by deletion.
func TestTheKeyedOrdersAreOnTheRecord(t *testing.T) {
	t.Parallel()
	// The subtests run in sequence on purpose: the two guards below count how many rows
	// reached the record, and a t.Parallel() subtest body runs after this function
	// returns, which would have the guard read the counter before anyone filled it.
	asked := 0
	for _, tt := range keyedAnswered {
		t.Run("answer "+strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			// A row the retired engine never saw is not on the record and cannot be put there: the
			// engine is retired (ADR 0302), so its table is closed. Those rows are judged by the
			// reference leg — the `want` column, the JIT, and the CLI in integration/ — and the guard
			// at the end of this test keeps it from becoming a no-op while that is so.
			if !HasGoldenAnswer(tt.src) {
				t.Skipf("the retired engine left no record of this program")
			}
			asked++
			g := goldenLookup(t, tt.src)
			if !g.HasStd {
				t.Fatalf("the record holds no stdout for %q", tt.src)
			}
			if g.Stdout != tt.want+"\n" {
				t.Fatalf("the record says %q for %q, the reference says %q", g.Stdout, tt.src, tt.want)
			}
		})
	}
	if asked == 0 {
		t.Fatal("no keyed-answer row is on the record: the record leg is not being walked")
	}
	askedRaises := 0
	for _, tt := range keyedRaises {
		t.Run("raise "+strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			if !HasGoldenAnswer(tt.src) {
				t.Skipf("the retired engine left no record of this program")
			}
			askedRaises++
			g := goldenLookup(t, tt.src)
			if g.ExnType != tt.class {
				t.Fatalf("the record traps %q for %q, the reference raises %s", g.ExnType, tt.src, tt.class)
			}
			if g.ExnMsg != tt.message {
				t.Fatalf("the record's message is %q, the reference's is %q", g.ExnMsg, tt.message)
			}
		})
	}
	if askedRaises == 0 {
		t.Fatal("no keyed-raise row is on the record: the record leg is not being walked")
	}
}

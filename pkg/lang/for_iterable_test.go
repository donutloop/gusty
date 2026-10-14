package lang

import (
	"regexp"
	"strings"
	"testing"
)

// for_iterable_test.go — a `for` loop asks its ITERABLE what it is before it decides how to walk it
// (roadmap Gap R.246, ADR 0339).
//
// The loop road had three ways to step over an iterable and no question to choose between them: the
// unrolled literal road (`for v in [1, 2, 3]`, `for c in "abc"`), the runtime container road (a handle the
// runtime measures), and the counter road for `range(...)` and this language's `for x in <integer>` repeat
// count. A container the compiler could write down but could not NAME — `sorted([3, 1, 2])`,
// `reversed([1, 2, 3])`, `sorted([3, 1, 2], reverse=True)`, `sorted([])`, `reversed("abc")`, `sorted(xs)` of
// a named list, `sorted(b())` of a call's answer, a slice copy — reached the counter road, which reads the
// iterable's word as a repeat count. Four behaviours, all unforgivable, each cited against the reference:
//
//	for v in sorted([3,1,2]):  print(v)      # %t1 = icmp slt i32 %_ctr1.ld1, @.lst1
//	                                           # llc: "global variable reference must have pointer type"
//	                                           # → EXIT 2, the code ADR 0166 reserves for a compiler bug
//	for v in sorted(["b","a"]): print(v)     # prints NOTHING, at the exit code of success
//	for v in sorted([3,1,2][0:2]): print(v)  # prints `0`; the reference prints `1`, `3`
//	for c in reversed("abc"):   print(c)     # prints NOTHING; the reference prints c, b, a
//
// Two doors close it: the FOLD, which shares `sortedLiteralElems` with the `sorted` answer itself so the
// order a program sees when it prints a sorted list and the order it sees when it steps over one are
// written once (ADR 0258's one-table rule, applied to a loop), and the container PAIR — `isContainerExpr`,
// `staticContainerKind`, `containerHandleOf` — which learns the two builtins that hand back a container and
// lowers them to the handle @rt_sort / @rt_list_copy + @rt_reverse built. What neither door reaches is
// REFUSED by name, because a `for` whose body prints the loop variable answers at exit 0 whether it walked
// anything or not, and a refusal that names the missing door (ADR 0249) is the only one of the three
// answers a reader can act on.

// forIterablesAnswered are the loops this row newly answers. `want` is what CPython prints for the same
// program — never what this file remembers (Gap N's standing rule) — and each row is asked of the compiler,
// of the JIT, and in the integration file of the CLI against the pinned reference.
var forIterablesAnswered = []struct {
	src  string
	want string
}{
	// the four shapes that used to make `llc` reject the module, and the two that answered nothing
	{"for v in sorted([3, 1, 2]):\n    print(v)", "1\n2\n3"},
	{"for v in reversed([1, 2, 3]):\n    print(v)", "3\n2\n1"},
	{"for v in sorted([3, 1, 2], reverse=True):\n    print(v)", "3\n2\n1"},
	{"for v in sorted([]):\n    print(v)", ""},
	{"for v in reversed(\"abc\"):\n    print(v)", "c\nb\na"},
	{"for v in [1, 2, 3]:\n    print(v)", "1\n2\n3"}, // the road the fold hands over to
	// a sort and a reverse NEST: the operations apply in the order the reference applies them, so
	// unwrapping to the innermost literal (what `listCallElems` does for len/sum, where order does
	// not matter) would answer 2, 3, 1 for the second row below where the reference answers 3, 2, 1.
	{"for v in sorted(reversed([3, 1, 2])):\n    print(v)", "1\n2\n3"},
	{"for v in reversed(sorted([1, 3, 2])):\n    print(v)", "3\n2\n1"},
	{"for v in sorted([2, 2, 1]):\n    print(v)", "1\n2\n2"}, // equal elements keep their written order
	// a text is reversed by CODE POINT, not by byte (L11.5's rule, where the loop already unrolls a text)
	{"for c in reversed(\"café\"):\n    print(c)", "é\nf\na\nc"},
	// the unrolled road keeps the loop's own semantics: break skips the else, continue advances, else runs
	{"for v in sorted([3, 1, 2]):\n    if v == 2:\n        break\n    print(v)", "1"},
	{"for v in sorted([3, 1, 2]):\n    if v == 2:\n        continue\n    print(v)", "1\n3"},
	{"for v in sorted([3, 1]):\n    print(v)\nelse:\n    print(\"done\")", "1\n3\ndone"},
	// the loop variable is bound per element, so a body that reads it sees the element and not the counter
	{"for v in reversed([7, 8]):\n    print(v * 10)", "80\n70"},
	// the container PAIR's new members: a `sorted`/`reversed` over a heap list is walked by the runtime
	// road, through the same @rt_sort / @rt_reverse `print(sorted(xs))` uses
	{"xs = [3, 1, 2]\nfor v in sorted(xs):\n    print(v)", "1\n2\n3"},
	{"xs = [1, 2, 3]\nfor v in reversed(xs):\n    print(v)", "3\n2\n1"},
	{"def b():\n    return [3, 1, 2]\nfor v in sorted(b()):\n    print(v)", "1\n2\n3"},
	{"def b():\n    return [1, 2, 3]\nfor v in reversed(b()):\n    print(v)", "3\n2\n1"},
	{"for v in sorted([\"b\", \"a\"]):\n    print(v)", "a\nb"},
	{"xs = [\"b\", \"a\"]\nfor v in reversed(xs):\n    print(v)", "a\nb"},
	{"for v in sorted([3, 1, 2][0:2]):\n    print(v)", "1\n3"},
	{"for v in sorted([3, 1, 2], reverse=False):\n    print(v)", "1\n2\n3"},
	{"for v in sorted([1, 1, 1]):\n    print(v)", "1\n1\n1"},
}

// forIterablesRefused are the loops neither door reaches and the backend therefore REFUSES — each one used
// to answer at exit 0 (a `0`, or silence) or to hand `llc` a global in an i32 slot and die with exit 2. The
// `oracle` column is what the reference answers, cited from the pinned interpreter: where the reference
// itself stops the program, so does the refusal, and the column says which error it stops with.
var forIterablesRefused = []struct {
	src    string
	names  string // what the refusal has to say (ADR 0249: a refusal names the missing half)
	oracle string // what the reference answers, for the record
}{
	{"for v in reversed({1: 2}):\n    print(v)", "`for` over reversed({1: 2})", "1"},
	{"for v in sorted(\"bca\"):\n    print(v)", "`for` over sorted(\"bca\")", "a\nb\nc"},
	{"d = {'b': 1, 'a': 2}\nfor k in sorted(d):\n    print(k)", "`for` over sorted(d)", "a\nb"},
	{"for v in sorted([3, 1, 2], True):\n    print(v)", "`for` over sorted", "TypeError: 'bool' object is not callable"},
	{"for v in sorted(range(3)):\n    print(v)", "`for` over sorted(range(3))", "0\n1\n2"},
}

// forIterablesUnchanged are the loops the row must leave answering exactly what they answered: the roads the
// fold and the guard are allowed NOT to touch. A guard that fires too wide fails here, not silently.
var forIterablesUnchanged = []struct {
	src  string
	want string
}{
	{"for c in \"abc\":\n    print(c)", "a\nb\nc"},
	{"for i in range(3):\n    print(i)", "0\n1\n2"},
	{"for i in range(1, 7, 2):\n    print(i)", "1\n3\n5"},
	// this language's own deviation, documented in docs/language.md: an integer iterable is a repeat count
	{"for i in 3:\n    print(i)", "0\n1\n2"},
	{"xs = [1, 2, 3]\nfor v in xs:\n    print(v)", "1\n2\n3"},
	{"for v in [1, 2, 3][0:2]:\n    print(v)", "1\n2"},
	{"for k in {1: 2}:\n    print(k)", "1"},
	{"for v in {1, 2}:\n    print(v)", "1\n2"},
	{"def b():\n    return [1, 2, 3]\nfor v in b():\n    print(v)", "1\n2\n3"},
	// the `*Call` arm of the dispatch is NARROWED, not removed: a generator call's answer is still walked
	// by the runtime road, and `sorted`/`reversed` only got their turn because that arm stopped swallowing
	// every call before the container pair was asked.
	{"def gen():\n    yield 1\n    yield 2\nfor v in gen():\n    print(v)", "1\n2"},
	{"def gen():\n    yield 1\n    yield 2\nxs = gen()\nfor v in xs:\n    print(v)", "1\n2"},
	{"for v in [1, \"a\", 2.5]:\n    print(v)", "1\na\n2.5"},
	// a loop variable's kind claim is made from ITS OWN iterable and taken back otherwise — measured on
	// the commit before this row as `b`, `a`, `a`, `(null)` (container road) and `b`, `a`, `b`, `a`
	// (counter road), each at the exit code of success: the second loop printed the first loop's answer
	// because the name still claimed interned text (roadmap Gap R.252, paid by ADR 0339)
	{"words = [\"b\", \"a\"]\nxs = [1, 2]\nfor v in words:\n    print(v)\nfor v in xs:\n    print(v)", "b\na\n1\n2"},
	{"words = [\"b\", \"a\"]\nfor v in words:\n    print(v)\nfor v in range(2):\n    print(v)", "b\na\n0\n1"},
	{"words = [\"b\", \"a\"]\nfor v in sorted(words):\n    print(v)\nfor v in range(2):\n    print(v)", "a\nb\n0\n1"},
	{"words = [\"b\", \"a\"]\nxs = [1, 2]\nfor v in sorted(words):\n    print(v)\nfor v in sorted(xs):\n    print(v)", "a\nb\n1\n2"},
	{"for c in \"abc\":\n    print(c)\nfor i in range(2):\n    print(i)", "a\nb\nc\n0\n1"},
	{"for k in {\"a\": 1}:\n    print(k)\nfor v in [7, 8]:\n    print(v)", "a\n7\n8"},
	{"xs = []\nxs.append(4)\nxs.append(5)\nfor v in xs:\n    print(v)", "4\n5"},
	{"for v in []:\n    print(v)", ""},
	// a program that defines `sorted` itself is not the builtin's shape, and the fold must not steal it
	{"def sorted(xs):\n    return [9]\nfor v in sorted([1]):\n    print(v)", "9"},
}

// counterAgainstContainerIR is the exit-2 shape itself: the loop's counter compared with a container GLOBAL.
// `llc` rejects it ("global variable reference must have pointer type") and the contract files the exit code
// as a compiler bug, so the row's closure is not complete until the instruction is structurally gone.
var counterAgainstContainerIR = regexp.MustCompile(`icmp [a-z]+ i32 %[^,]+, @\.(lst|set|dict)\w*`)

// TestTheLoopNeverComparesItsCounterWithAContainer is that claim, asked of the emitted module rather than of
// the runtime: no module this suite compiles may put a container global where an i32 count belongs — the
// folds answer it, the pair answers it, the guard refuses it, and either way the instruction is not emitted.
func TestTheLoopNeverComparesItsCounterWithAContainer(t *testing.T) {
	for _, src := range []string{
		"for v in sorted([3, 1, 2]):\n    print(v)",
		"for v in reversed([1, 2, 3]):\n    print(v)",
		"for v in sorted([3, 1, 2], reverse=True):\n    print(v)",
		"for v in sorted([]):\n    print(v)",
		"for v in reversed(\"abc\"):\n    print(v)",
		"xs = [3, 1, 2]\nfor v in sorted(xs):\n    print(v)",
		"xs = [1, 2, 3]\nfor v in reversed(xs):\n    print(v)",
		"def b():\n    return [1, 2, 3]\nfor v in sorted(b()):\n    print(v)",
		"for v in sorted([3, 1, 2][0:2]):\n    print(v)",
		"for v in sorted([\"b\", \"a\"]):\n    print(v)",
		"for v in [1, 2, 3]:\n    print(v)",
		"for i in range(3):\n    print(i)",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			res, err := Compile(src)
			if err != nil {
				return // a refusal is the other honest answer; the IR cannot contain what was never emitted
			}
			if m := counterAgainstContainerIR.FindString(res.IR); m != "" {
				t.Fatalf("the module compares a loop counter with a container global (%s) — the shape llc rejects and the contract files as a compiler bug (ADR 0166):\n%s", m, res.IR)
			}
		})
	}
}

// TestTheLoopWalksTheIterableItCanWriteDown is the row's answer, on the compiler and the JIT. A refusal is a
// failure here: these programs are the reference's own, and the fold and the pair exist precisely so they
// are not refused (ADR 0249).
func TestTheLoopWalksTheIterableItCanWriteDown(t *testing.T) {
	for _, tt := range forIterablesAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); the reference prints %q", tt.src, got, tt.want)
			}
			if want := tt.want; want == "" {
				if strings.TrimSpace(got) != "" {
					t.Fatalf("%q printed %q, want nothing", tt.src, got)
				}
			} else if strings.TrimSpace(got) != want {
				t.Fatalf("%q printed %q, want %q", tt.src, strings.TrimSpace(got), want)
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

// TestTheLoopRefusesWhatItCannotWalk is the guard: every shape that used to be an exit-2 module or a silent
// exit-0 answer is now a refusal that names the iterable it refused, and none of them may reach exit 2 — the
// code the contract reserves for a compiler bug (ADR 0166).
func TestTheLoopRefusesWhatItCannotWalk(t *testing.T) {
	for _, tt := range forIterablesRefused {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			out, refused := compiledOutOrRefusal(t, tt.src)
			if !refused {
				t.Fatalf("%q answered %q at exit 0; the row expects a refusal — the loop read a container's word as a count, which is the answer this contract ranks worst (the reference prints %q)", tt.src, out, tt.oracle)
			}
			if !strings.Contains(out, tt.names) {
				t.Fatalf("%q refused with %q, which does not name the iterable it refused (%q); ADR 0249", tt.src, out, tt.names)
			}
			for _, need := range []string{"roadmap L11.1", "ADR 0339", "repeat count"} {
				if !strings.Contains(out, need) {
					t.Fatalf("%q refused without saying %q: a refusal must name the half that is missing\n%s", tt.src, need, out)
				}
			}
		})
	}
}

// TestTheLoopStillWalksWhatItAlwaysWalked is the negative control at the answer altitude: the fold may fire
// only on a call it can write down, the pair may claim only the two builtins over a list, and the guard may
// fire only where no container road took it. Every row here answered before the row and answers still.
func TestTheLoopStillWalksWhatItAlwaysWalked(t *testing.T) {
	for _, tt := range forIterablesUnchanged {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); this loop answered %q before the row and must go on answering it", tt.src, got, tt.want)
			}
			if strings.TrimSpace(got) != tt.want {
				t.Fatalf("%q printed %q, want %q", tt.src, strings.TrimSpace(got), tt.want)
			}
		})
	}
}

// TestTheFoldAndTheSortedAnswerShareOneOrdering is why `sortedLiteralElems` is one function: the program
// printed and the program looped over must not be able to disagree about the order, which is ADR 0258's
// complaint about a fold and a renderer moving apart, applied to a loop.
//
// `reversed` is NOT in this table, and cannot be: the loop walks the flipped copy, while `reversed` standing
// where an expression goes answers its argument UNREVERSED (see TestWhatTheLoopRowLeavesOwed). Sharing an
// ordering with a road that does not order is not the same discipline.
func TestTheFoldAndTheSortedAnswerShareOneOrdering(t *testing.T) {
	for _, tc := range []struct {
		printed string
		looped  string
	}{
		{"print(sorted([3, 1, 2]))", "for v in sorted([3, 1, 2]):\n    print(v)"},
		{"print(sorted([3, 1, 2], reverse=True))", "for v in sorted([3, 1, 2], reverse=True):\n    print(v)"},
		{"print(sorted([2, 2, 1]))", "for v in sorted([2, 2, 1]):\n    print(v)"},
		{"print(sorted([]))", "for v in sorted([]):\n    print(v)"},
		{"xs = [\"b\", \"a\"]\nprint(sorted(xs))", "xs = [\"b\", \"a\"]\nfor v in sorted(xs):\n    print(v)"},
		{"xs = [3, 1, 2]\nprint(sorted(xs, reverse=True))", "xs = [3, 1, 2]\nfor v in sorted(xs, reverse=True):\n    print(v)"},
	} {
		t.Run(tc.printed, func(t *testing.T) {
			p, refused := compiledOutOrRefusal(t, tc.printed)
			if refused {
				t.Fatalf("%q refused (%s); the answer this ordering is shared with must keep answering", tc.printed, p)
			}
			l, refusedL := compiledOutOrRefusal(t, tc.looped)
			if refusedL {
				t.Fatalf("%q refused (%s); the loop and the answer read one fold", tc.looped, l)
			}
			// "[1, 2, 3]" and "1\n2\n3" are the same sequence: compare the elements, not the brackets.
			if got, want := splitSeq(l), splitSeq(p); strings.Join(got, "|") != strings.Join(want, "|") {
				t.Fatalf("the loop sees %v and the printed list is %v — one ordering, written once (ADR 0258)", got, want)
			}
		})
	}
}

// splitSeq turns "[1, 2, 3]\n" or "1\n2\n3\n" into ["1","2","3"].
func splitSeq(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.Trim(strings.TrimSpace(p), "'"))
	}
	return out
}

// TestWhatTheLoopRowLeavesOwed pins the doors beside this row that still refuse, or still answer wrong, so
// that fixing them has to move a pinned expectation and not slip through. The `roadmap` column is the row
// that owns each one; the `oracle` column is what the pinned reference answers.
func TestWhatTheLoopRowLeavesOwed(t *testing.T) {
	for _, tc := range []struct {
		src     string
		names   string // what the refusal says today
		oracle  string // what the reference answers
		roadmap string // the row that owns it
	}{
		// Gap R.247 — `sorted` over an iterable the container pair cannot name: a TEXT. The loop's own half of
		// this family is in forIterablesRefused; the `print` half is the builtin's refusal beside it. A DICT is
		// the same door and is answered by `print(sorted(d))` (`['b']`, what the reference prints) while the
		// LOOP over it is refused — one word, two doors, the row that owns them owns both.
		{"print(sorted(\"bca\"))", "sorted", "['a', 'b', 'c']", "Gap R.247"},
		{"for v in sorted(range(3)):\n    print(v)", "not a container this backend can name", "0\n1\n2", "Gap R.247"},
		{"d = {'b': 1, 'a': 2}\nfor k in sorted(d):\n    print(k)", "its argument is a DICT", "a\nb", "Gap R.247"},
		{"xs = [\"b\", \"a\"]\nfor v in sorted(xs[0:2]):\n    print(v)", "comparator", "a\nb", "Gap R.247"},
		// Gap R.248 — a FLOAT list is refused with a sentence that blames a mix of kinds it does not have.
		{"xs = [1.5, 0.5]\nfor v in sorted(xs):\n    print(v)", "ELEMENT kind", "0.5\n1.5", "Gap R.248"},
		// Gap R.249 — a positional key is still the reference's TypeError the language does not answer;
		// the `key=` half of that row was paid by ADR 0340 and its loop rows now live in
		// call_key_test.go's answered table.
		// Gap R.250 — a `sorted` of a call that is not itself a builtin the pair can prove.
		{"xs = [3, 1, 2]\nfor v in sorted(sorted(xs)):\n    print(v)", "not a container this backend can name", "1\n2\n3", "Gap R.250"},
		// Gap R.251 — `reversed` standing where an EXPRESSION goes answers its argument UNREVERSED; the loop
		// beside it walks the flipped copy, so one word is answered two ways. The reference prints
		// <list_reverseiterator object at 0x…>, and `list(reversed(xs))` is the spelling that shows the order.
		{"xs = [1, 2, 3]\nprint(reversed(xs))", "", "[1, 2, 3]", "Gap R.251"},
	} {
		t.Run(strings.ReplaceAll(tc.src, "\n", "; "), func(t *testing.T) {
			out, refused := compiledOutOrRefusal(t, tc.src)
			if tc.names == "" {
				if refused {
					t.Fatalf("%q refused (%s); this owed row is about a WRONG answer, not a refusal — the compiled answer is what has to change", tc.src, out)
				}
				return
			}
			if !refused {
				t.Fatalf("%q answered %q at exit 0; roadmap %s filed this as a refusal, so the row moved (the reference answers %q)", tc.src, out, tc.roadmap, tc.oracle)
			}
			if !strings.Contains(out, tc.names) {
				t.Fatalf("%q refused with %q, which does not say %q (ADR 0249); filed as roadmap %s, the reference answers %q", tc.src, out, tc.names, tc.roadmap, tc.oracle)
			}
		})
	}
}

// TestTheLoopRowSharesTheSortWithTheBuiltin is the structural half of ADR 0258's claim: the loop's answer
// for a heap list is the SAME helper call the `sorted` answer emits, so a fix to one cannot leave the other
// behind. Asked of the module, not of the runtime.
func TestTheLoopRowSharesTheSortWithTheBuiltin(t *testing.T) {
	for _, tc := range []struct {
		src  string
		need string
	}{
		{"xs = [3, 1, 2]\nfor v in sorted(xs):\n    print(v)", "call void @rt_sort"},
		{"for v in sorted([\"b\", \"a\"]):\n    print(v)", "call void @rt_sort"},
		{"for v in sorted([3, 1, 2][0:2]):\n    print(v)", "call void @rt_sort"},
		{"xs = [1, 2, 3]\nfor v in reversed(xs):\n    print(v)", "call void @rt_reverse"},
		{"def b():\n    return [1, 2, 3]\nfor v in reversed(b()):\n    print(v)", "call void @rt_reverse"},
	} {
		t.Run(strings.ReplaceAll(tc.src, "\n", "; "), func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%q refused (%v); this loop is answered by the runtime road and must not be refused", tc.src, err)
			}
			if !strings.Contains(res.IR, tc.need) {
				t.Fatalf("%q did not emit %q — the loop is not walking the runtime's own container helper (ADR 0258)\n%s", tc.src, tc.need, res.IR)
			}
		})
	}
}

// TestTheLoopRowsAnswerIsNotTheCounter is the exit-0 shape asked directly: a loop variable bound to the
// counter prints `0` once, which is a value the program never wrote. No answered row may print `0` unless the
// program itself contains a 0.
func TestTheLoopRowsAnswerIsNotTheCounter(t *testing.T) {
	for _, tt := range forIterablesAnswered {
		if strings.Contains(tt.src, "0") || strings.Contains(tt.want, "0") || tt.want == "" {
			continue
		}
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			got, refused := compiledOutOrRefusal(t, tt.src)
			if !refused && strings.Contains(got, "0") {
				t.Fatalf("%q printed %q, which contains a 0 the program never wrote — the signature of a loop variable bound to the counter", tt.src, got)
			}
		})
	}
}

// TestWhatTheLoopRowLeavesFiledNotFixed covers the shape whose current answer is not a refusal at all: a
// membership test that hands `llc` a container where a runtime word belongs. It is not the loop's iterable
// door, so it is not fixed here — but it was measured on the way to this row, and the exit-2 it produces is
// the shape ADR 0166 says the compiler owns.
func TestWhatTheLoopRowLeavesFiledNotFixed(t *testing.T) {
	t.Run("membership over a sorted literal hands llc a global (Gap R.253)", func(t *testing.T) {
		// `1 in sorted([3, 1, 2])` builds `rt_contains(i32 @.lst1, i32 1)`: the container's GLOBAL in an i32
		// slot, which `llc` refuses — "global variable reference must have pointer type" — and the program
		// dies with exit 2, the code the contract reserves for exactly that (ADR 0166). The row is filed;
		// what may never happen is for the shape to answer at exit 0 with a value nobody wrote.
		const src = "print(1 in sorted([3, 1, 2]))\n"
		res, err := Compile(src)
		if err != nil {
			return // a refusal is the other honest answer; when this road learns to refuse, the row is paid
		}
		if !strings.Contains(res.IR, "@.lst") {
			t.Fatalf("%q no longer puts a container global where a runtime word belongs — roadmap Gap R.253 is paid, and the row's evidence has to move:\n%s", src, res.IR)
		}
	})
}

// TestTheLoopRowsAnswersAreOnTheRecord is the record leg: every loop this row answers has a record beside
// it, and the record says what the reference says. A missing record fails here — coverage cannot be bought
// back by deleting a row (ADR 0302), and the answer a loop gives is only evidence while the record holds it.
func TestTheLoopRowsAnswersAreOnTheRecord(t *testing.T) {
	for _, tt := range forIterablesAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			g := goldenLookup(t, tt.src)
			if !g.HasStd {
				t.Fatalf("%q is on the record with no stdout, but the row prints %q", tt.src, tt.want)
			}
			wantStd := tt.want
			if wantStd != "" {
				wantStd += "\n" // the record keeps the program's last newline; the table does not
			}
			if g.Stdout != wantStd {
				t.Fatalf("%q: the record holds %q and the row expects %q — one of them has to move, and the reference is the referee", tt.src, g.Stdout, tt.want)
			}
		})
	}
}

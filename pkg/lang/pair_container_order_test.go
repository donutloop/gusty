// pkg/lang/pair_container_order_test.go — the fold that orders containers walks their elements instead of
// reading a number out of the handle (roadmap Gap R.197, filed measuring ADR 0316; paid by ADR 0318).
//
// `xs = []` / `xs.append([1, 2])` / `xs.append([3])` / `a = xs[0]` / `b = xs[1]` / `print(min(a, b))` is the
// reference's `[1, 2]`. Before this cycle the compiled backend spent exit 3 on it — `TypeError: '<' not
// supported between instances of 'list' and 'list'` — because the fold door ordered only numbers and texts
// and had nothing to say about a container: an operand whose kind it could name went to the arithmetic door,
// which for a container answers a length as a value (Gap R.197's other arm, `min(la, o)` answering `2`, the
// ELEMENT COUNT, at exit 0 where the reference raises). A raise where the reference answered a value, and a
// value where it raised: the same wrong-answer class from both ends.
//
// The door the fold now walks is `@rt_pair_order`: numbers in the one word that holds int, float and bool;
// texts by CONTENT (ADR 0248's rule, one helper later); a LIST lexicographically, element by element, the
// shorter list the lesser when every shared element is equal; a SET by the subset operator, with the pair
// that is neither one's subset answering False to BOTH '<' and '>' rather than raising; and CPython's own
// sentence, naming the two kinds that actually failed, for everything else — including a dict against a
// copy of itself. The element pair inside a list is asked EQUALITY before ORDERING, which is the order the
// reference's own list comparison asks.
//
// Every row is judged on both witness legs: the record leg (`RecordedStdoutIs`/`RecordedRunError` — a source
// with no record FAILS, so coverage cannot be deleted by deleting a record) and, through
// `integration/pair_container_order_test.go`, the reference leg (the same programs through the CLI against
// CPython). Traps are RAISED with the reference's sentence and catchable by the program's own `except`; the
// shapes the door still declines stay refusals in words at exit 1 and never the contract's exit 2 (ADR 0166).
// The relational operators over two built containers are NOT claimed here — that half is Gap R.97's, and its
// measured state is filed as Gap R.201 beside this row.
package lang

import (
	"strings"
	"testing"
)

// The containers a fold is asked to order, spelled the way the positions meet them: built by the program and
// read back through a name, so nothing in the fold's own line says which of the three container kinds
// arrives, let alone what is inside one.
const (
	foldPairLists = "xs = []\nxs.append([1, 2])\nxs.append([3])\na = xs[0]\nb = xs[1]\n"
	foldPairSets  = "xs = []\nxs.append({1, 2})\nxs.append({1, 2, 3})\na = xs[0]\nb = xs[1]\n"
	foldPairEmpty = "xs = []\nxs.append([])\nxs.append([1, 2])\na = xs[0]\nb = xs[1]\n"
)

// foldPairAnswerRows is the answer table. The rows that decide the ANSWER are the lexicographic ones (a
// shorter list of equal elements is the lesser, as the reference has it) and the subset ones (a set that is
// not the other's subset orders neither way, so the incumbent survives — the reference's answer, not a
// raise). The rows that decide the KIND are the bound ones: the winner travels out with its own tag, so the
// print door, the binder and the f-string field all see a list and not a number wearing a list's bits.
var foldPairAnswerRows = []struct{ name, src, want string }{
	{"two built lists, min", foldPairLists + "print(min(a, b))\n", "[1, 2]\n"},
	{"two built lists, max", foldPairLists + "print(max(a, b))\n", "[3]\n"},
	{"one list asked against itself keeps the incumbent", foldPairLists + "print(min(a, a))\n", "[1, 2]\n"},
	{"the winner bound, printed, and asked whether it is the list it is",
		foldPairLists + "m = min(a, b)\nprint(m)\nprint(m == [1, 2])\n", "[1, 2]\nTrue\n"},
	{"the winner fills an f-string field", foldPairLists + "m = min(a, b)\nprint(f\"{m}\")\n", "[1, 2]\n"},
	{"the winner folded again by a second fold", foldPairLists + "m = min(a, b)\nn = max(m, b)\nprint(n)\n", "[3]\n"},
	{"two built sets, min", foldPairSets + "print(min(a, b))\n", "{1, 2}\n"},
	{"two built sets, max", foldPairSets + "print(max(a, b))\n", "{1, 2, 3}\n"},
	{"the empty list is the lesser list, min", foldPairEmpty + "print(min(a, b))\n", "[]\n"},
	{"the empty list read by max", foldPairEmpty + "print(max(a, b))\n", "[1, 2]\n"},
	{"two empty lists spelled as calls", "print(min(list(), list()))\n", "[]\n"},
	{"a list asked whether it is the other list", foldPairLists + "print(a == b)\n", "False\n"},
	{"the numbers the door already answered still answer", "print(min(3, 1))\nprint(min(\"b\", \"a\"))\n", "1\na\n"},
}

// foldPairTrapRows is the trap half, and it is the half that pays Gap R.197's second arm: the shapes that
// used to ANSWER at exit 0 — a container against a number, where the fold read the container's element count
// as its value — now raise the reference's sentence, in the order the failing comparison had the two kinds
// (the candidate first, the incumbent second: the rule ADR 0316 set for numbers and texts, extended to the
// containers).
//
// The last three rows are the ones that make the walk an element-wise one rather than a kind test on the two
// OUTSIDE tags: the failure is INSIDE two lists, and the sentence names the ELEMENTS' kinds, not the two
// containers the program wrote.
var foldPairTrapRows = []struct{ name, src, class, message string }{
	{"a list folded against a number (min)", foldPairLists + "print(min(a, 3))\n", "TypeError", "'<' not supported between instances of 'int' and 'list'"},
	{"a list folded against a number (max)", foldPairLists + "print(max(a, 3))\n", "TypeError", "'>' not supported between instances of 'int' and 'list'"},
	{"a set folded against a number", foldPairSets + "print(min(a, 3))\n", "TypeError", "'<' not supported between instances of 'int' and 'set'"},
	{"a list folded against a text", foldPairLists + "print(min(a, \"a\"))\n", "TypeError", "'<' not supported between instances of 'str' and 'list'"},
	{"a list folded against a set names both containers",
		"xs = []\nxs.append([1])\nxs.append({1})\na = xs[0]\nb = xs[1]\nprint(min(a, b))\n", "TypeError", "'<' not supported between instances of 'set' and 'list'"},
	{"two dicts raise", "d = {}\ne = {}\nprint(min(d, e))\n", "TypeError", "'<' not supported between instances of 'dict' and 'dict'"},
	{"a dict against itself has no ordering either", "d = {}\nprint(min(d, d))\n", "TypeError", "'<' not supported between instances of 'dict' and 'dict'"},
	{"the failing element inside two lists names the ELEMENTS' kinds",
		// The two kinds the sentence names are the ELEMENTS' — 'int' and 'str', not 'list' and 'list' —
		// and their order is the order the failing comparison had them: the reference's `min` asks the
		// LATER argument against the earlier one, so the element of the later list is named first.
		"xs = []\nxs.append([\"a\"])\nxs.append([1])\na = xs[0]\nb = xs[1]\nprint(min(a, b))\n", "TypeError", "'<' not supported between instances of 'int' and 'str'"},
	{"the same failure seen by max names '>'",
		"xs = []\nxs.append([\"a\"])\nxs.append([1])\na = xs[0]\nb = xs[1]\nprint(max(a, b))\n", "TypeError", "'>' not supported between instances of 'int' and 'str'"},
	{"an empty set spelled as a call", "print(min(set(), 3))\n", "TypeError", "'<' not supported between instances of 'int' and 'set'"},
	{"an empty list spelled as a call", "print(min(list(), 3))\n", "TypeError", "'<' not supported between instances of 'int' and 'list'"},
	{"a number against a set spelled as a call", "print(min(3, {4}))\n", "TypeError", "'<' not supported between instances of 'set' and 'int'"},
	{"a number first, a set second, a number last", "print(min(3, {4}, 5))\n", "TypeError", "'<' not supported between instances of 'set' and 'int'"},
	{"an empty list against a text", "print(min(list(), \"a\"))\n", "TypeError", "'<' not supported between instances of 'str' and 'list'"},
	{"an empty set against an empty list", "print(min(set(), list()))\n", "TypeError", "'<' not supported between instances of 'list' and 'set'"},
	{"a number among the lists a min folds", "print(min(3, [1], [2]))\n", "TypeError", "'<' not supported between instances of 'list' and 'int'"},
	{"the lists a min folds, with a number last", "print(min([1], [2], 3))\n", "TypeError", "'<' not supported between instances of 'int' and 'list'"},
}

// foldPairCatchRows is the half that makes a raise a raise: the program's own `except TypeError:` reaches the
// ordering door's failure in each of the places it can fail, and the program goes on answering afterwards.
var foldPairCatchRows = []struct{ name, src, want string }{
	{"the container-against-a-number raise caught, and the program carries on",
		foldPairLists + "try:\n    print(min(a, 3))\nexcept TypeError:\n    print(\"caught\")\nprint(\"carries-on\")\n", "caught\ncarries-on\n"},
	{"the fold still answers after a caught raise",
		foldPairLists + "try:\n    print(min(a, 3))\nexcept TypeError:\n    print(\"caught\")\nprint(min(a, b))\n", "caught\n[1, 2]\n"},
	{"the element-that-does-not-order raise caught, and the list reads back",
		"xs = []\nxs.append([\"a\"])\nxs.append([1])\na = xs[0]\nb = xs[1]\ntry:\n    print(min(a, b))\nexcept TypeError:\n    print(\"caught\")\nprint(a)\n", "caught\n['a']\n"},
	{"the dict raise caught", "d = {}\ntry:\n    print(min(d, d))\nexcept TypeError:\n    print(\"caught\")\n", "caught\n"},
}

// foldPairRefusalRows is the honest half. Each of these was measured on the compiled leg and on the
// reference before it was written down; none of them is claimed as an answer, and none of them is allowed to
// spend the exit-code contract's "the compiler is broken" code (ADR 0166). The first four are Gap R.198's
// untaggable shapes — a container WRITTEN among the folded values, where the door will not label the element
// — and the last four are the fold's answer used in a position that keeps one word for a whole value
// (Gap R.146) or by a builtin that asks the compiler to see inside the value.
var foldPairRefusalRows = []struct{ name, src, want string }{
	{"two literal containers", "print(min([1], [2]))\n", "container among values"},
	{"an answer folded against a literal container", foldPairLists + "print(min([min(a, b)], b))\n", "container among values"},
	{"an empty list spelled as a call beside a literal one", "print(min(list(), [1]))\n", "container among values"},
	{"an empty set spelled as a call beside a literal one", "print(min(set(), {1}))\n", "container among values"},
	{"the winner as a list element", foldPairLists + "print([min(a, b)])\n", "one word"},
	{"the winner asked against a literal list", foldPairLists + "print(min(a, b) == [1, 2])\n", "one word"},
	{"the winner measured by len", foldPairLists + "print(len(min(a, b)))\n", "len requires an inline list/dict/set literal"},
	{"the winner subscripted", foldPairLists + "m = min(a, b)\nprint(m[0])\n", "index of a non-literal variable"},
}

func TestAFoldOfContainersOrdersThemElementWise(t *testing.T) {
	for _, tc := range foldPairAnswerRows {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			RecordedStdoutIs(t, tc.src, tc.want)
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the reference prints: %v", err)
			}
			if strings.TrimSuffix(res.Output, "\n") != strings.TrimSuffix(tc.want, "\n") {
				t.Fatalf("compiled printed %q, want %q", res.Output, tc.want)
			}
			if res.Code == 2 {
				t.Fatalf("exit 2 — LLVM rejected the module the ordering was emitted into (ADR 0166)")
			}
		})
	}
}

func TestAFoldOfContainersRaisesWhatTheReferenceRaises(t *testing.T) {
	for _, tc := range foldPairTrapRows {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := RecordedRunError(t, tc.src)
			if err == nil {
				t.Fatalf("the program answered where the reference raises")
			}
			if ee, ok := err.(*TrapError); ok {
				if ee.ExnType != tc.class {
					t.Errorf("the record's class = %q, want %q", ee.ExnType, tc.class)
				}
				if ee.ExnMsg != tc.message {
					t.Errorf("the record's message = %q, want %s: %s", ee.ExnMsg, tc.class, tc.message)
				}
			} else if !strings.Contains(err.Error(), tc.class+": "+tc.message) {
				t.Errorf("the record's trap is %q, want %s: %s", err, tc.class, tc.message)
			}
			res, jerr := JIT(tc.src, 0)
			if jerr != nil {
				t.Fatalf("compiled to nothing where the reference raises: %v", jerr)
			}
			// An answer at the exit code of success is the defect this row exists to pay: the
			// element count of a container, printed where the reference raises.
			if res.Code == 0 {
				t.Fatalf("the fold ANSWERED %q at exit 0 where the reference raises — Gap R.197's reading of a length as a value", res.Output)
			}
			if res.Code == 2 {
				t.Fatalf("exit 2 — LLVM rejected the module this raise was emitted into (ADR 0166)")
			}
			if !strings.Contains(res.Stderr, tc.class+": "+tc.message) {
				t.Fatalf("compiled raised %q, want %s: %s", firstLine(res.Stderr), tc.class, tc.message)
			}
		})
	}
}

func TestAFoldOfContainersTrapIsCatchable(t *testing.T) {
	for _, tc := range foldPairCatchRows {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			RecordedStdoutIs(t, tc.src, tc.want)
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the reference answers: %v", err)
			}
			if res.Code != 0 {
				t.Fatalf("a caught ordering raise ended the program at exit %d: %s", res.Code, res.Stderr)
			}
			if strings.TrimSuffix(res.Output, "\n") != strings.TrimSuffix(tc.want, "\n") {
				t.Fatalf("compiled printed %q, want %q", res.Output, tc.want)
			}
		})
	}
}

func TestWhatTheFoldOfContainersStillRefusesIsRefusedInWords(t *testing.T) {
	for _, tc := range foldPairRefusalRows {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			res, err := JIT(tc.src, 0)
			if err == nil {
				t.Fatalf("answered %q where the door declines this shape", res.Output)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal does not name the missing half (%q):\n%v", tc.want, err)
			}
			if strings.Contains(err.Error(), "LLVM ERROR") || strings.Contains(err.Error(), "verifier") {
				t.Fatalf("failed as an IR problem instead of a front-end refusal: %v", err)
			}
			ir, cerr := Compile(tc.src)
			if cerr == nil {
				for _, line := range strings.Split(ir.IR, "\n") {
					if strings.Contains(line, "icmp ") && strings.Contains(line, "@.") {
						t.Fatalf("a global reached a comparison instruction (exit 2 waiting to happen): %s", strings.TrimSpace(line))
					}
				}
			}
		})
	}
}

// TestTheOrderingDoorIsOneHelperTheFoldAsks keeps the ordering at one address and pins that the FOLD asks it.
// A fold that ordered its operands by a rule of its own would pass every answer row above and still disagree
// with the rest of the language about the same pair — which is how ADR 0309's "fixed" features silently
// re-acquire the one-word bug. The negative rows are ADR 0309's gate: a module carries the ordering only for
// the folds it actually walks.
func TestTheOrderingDoorIsOneHelperTheFoldAsks(t *testing.T) {
	for _, tc := range []struct {
		name    string
		src     string
		must    []string
		mustNot []string
	}{
		{
			name: "a fold of built containers asks the ordering",
			src:  foldPairLists + "print(min(a, b))\n",
			must: []string{
				"define internal i32 @rt_pair_order(",
				"call i32 @rt_pair_fold(",
				// The fold does not compare the operands themselves: it asks the ordering and
				// branches on the WORD the ordering wrote.
				"call i32 @rt_pair_order(i32 0, i32 %ap, i32 %at, i32 %bp, i32 %bt,",
			},
		},
		{
			name: "a fold of empty containers asks it too",
			src:  "print(min(set(), 3))\n",
			must: []string{"define internal i32 @rt_pair_order(", "call i32 @rt_pair_fold("},
		},
		{
			name:    "a fold the compiler settles carries no door at all",
			src:     "print(min(2.5, 3))\nprint(sum([1, 2]))\n",
			mustNot: []string{"@rt_pair_fold", "@rt_pair_order", "@rt_fold_bad"},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the reference answers: %v", err)
			}
			for _, want := range tc.must {
				if !strings.Contains(res.IR, want) {
					t.Errorf("the module stopped asking the ordering door — %q is gone", want)
				}
			}
			for _, absent := range tc.mustNot {
				if strings.Contains(res.IR, absent) {
					t.Errorf("the module carries %q, which is a door no fold in this program walks (ADR 0309's gate)", absent)
				}
			}
		})
	}
}

// TestTheFoldGateOpensForAContainerItCanName is the gate asked directly, so a future narrowing fails a
// decision row instead of quietly sending a container back to the arithmetic door — the road that answered
// `min(la, o)` as `2`. An argument the container analysis describes opens the door; an all-literal fold keeps
// the constant road it has always had.
func TestTheFoldGateOpensForAContainerItCanName(t *testing.T) {
	g := &irGen{taggedVars: map[string]bool{}, pairRetDone: map[string]bool{}}
	for _, tc := range []struct {
		name      string
		call      string
		pair      bool
		container bool
	}{
		{"an empty set spelled as a call", "min(set(), 3)", true, true},
		{"an empty list spelled as a call", "min(list(), 3)", true, true},
		{"an empty dict spelled as a call", "min(dict(), 3)", true, true},
		{"a number against a brace set keeps the ordinary road", "min(3, {4})", false, false},
		{"nothing but numbers", "min(3, 1)", false, false},
		{"nothing but texts", "min(\"a\", \"b\")", false, false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			prog, err := parseProgram(tc.call + "\n")
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			c, ok := prog.Stmts[0].(*ExprStmt).Expr.(*Call)
			if !ok {
				t.Fatalf("%q is not a call", tc.call)
			}
			_, args, shapeOK := g.foldCallShape(c)
			if !shapeOK {
				t.Fatalf("the shape question declined %q, so the gate is never even asked", tc.call)
			}
			if got := g.foldMayNeedPair(args); got != tc.pair {
				t.Errorf("foldMayNeedPair = %v, want %v", got, tc.pair)
			}
			carries := false
			for _, a := range args {
				if g.foldArgCarriesAContainer(a) {
					carries = true
				}
			}
			if carries != tc.container {
				t.Errorf("an argument carries a container = %v, want %v", carries, tc.container)
			}
		})
	}
}

// TestAFoldOfCyclicContainersCompilesAndTheReferenceAnswers is the unit half of Gap R.203, and the shape of the
// row is itself a finding: a program whose fold walks a container that contains itself CRASHES its own process at
// exit 2, so the assertion cannot live in the test binary that runs the JIT — the crash would take the suite down
// with it (it does; this row was written after that happened). What the unit process can and does check is the
// half that is its own: the program is BUILT — the fold is compiled, the module carries the ordering — so the
// crash is a run-time one in the printer rather than a build refusal wearing the printer's clothes, and the
// reference's answer is asked here too, so the row cannot drift. The exit codes are asserted where a crash can be
// survived: at the CLI, in `integration/pair_container_order_test.go`, in a process per program.
func TestAFoldOfCyclicContainersCompilesAndTheReferenceAnswers(t *testing.T) {
	for _, tc := range []struct {
		name      string
		src       string
		refAnswer string // what CPython prints, or "" when the reference traps
		refTrap   string // the reference's trap sentence, when it traps
	}{
		{
			name:      "a cyclic list printed is the printer's hole",
			src:       "xs = []\nxs.append(xs)\nprint(xs)\nprint(\"after\")\n",
			refAnswer: "[[...]]\nafter\n",
		},
		{
			name:      "the same list folded against itself reaches that printer",
			src:       "xs = []\nxs.append(xs)\nprint(min(xs, xs))\nprint(\"after\")\n",
			refAnswer: "[[...]]\nafter\n",
		},
		{
			name:    "two independently cyclic containers hit the ordering's depth guard",
			src:     "xs = []\nxs.append(xs)\nys = []\nys.append(ys)\nprint(min(xs, ys))\n",
			refTrap: "RecursionError",
		},
		{
			// The arm the walk answers correctly: the cycle is on one side only, so the failing comparison
			// is a number against a container, and the sentence names the two kinds the reference names.
			// Before ADR 0318 it said 'list' and 'list'.
			name:    "a cyclic list against a plain one names the kinds that failed",
			src:     "xs = []\nxs.append(1)\nys = []\nys.append(xs)\nys.append(ys)\nprint(min(ys[0], ys[1]))\n",
			refTrap: "TypeError: '<' not supported between instances of 'list' and 'int'",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			refOut, refErr, refErr2 := PythonRun(tc.src)
			if tc.refAnswer != "" {
				if refErr2 != nil {
					t.Fatalf("the reference stopped answering this filed row (%v): %s", refErr2, refErr)
				}
				if refOut != tc.refAnswer {
					t.Fatalf("the reference's answer for this filed row drifted: %q, want %q", refOut, tc.refAnswer)
				}
			} else if refErr2 == nil {
				t.Fatalf("the reference answered %q where this row records its trap %s", refOut, tc.refTrap)
			} else if !strings.Contains(refErr, tc.refTrap) {
				t.Fatalf("the reference's trap is no longer %s: %s", tc.refTrap, firstLine(refErr))
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("the fold was refused at build time (%v); this row files a RUN-time crash, and a refusal "+
					"here would move the defect rather than fix it", err)
			}
			if strings.Contains(tc.src, "min(") &&
				!strings.Contains(res.IR, "define internal i32 @rt_pair_order(") {
				t.Errorf("the module carries no ordering door, so this fold is not walking a container at all")
			}
			for _, line := range strings.Split(res.IR, "\n") {
				if strings.Contains(line, "icmp ") && strings.Contains(line, "@.") {
					t.Fatalf("a global reached a comparison instruction, which is exit 2 at build time: %s",
						strings.TrimSpace(line))
				}
			}
		})
	}
}

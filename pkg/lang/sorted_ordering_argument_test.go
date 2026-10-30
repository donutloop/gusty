package lang

import (
	"regexp"
	"strings"
	"testing"
)

// sorted_ordering_argument_test.go — the doors ASK AN ORDERING WHAT IT WAS HANDED (roadmap **Gap R.247**,
// **ADR 0473**).
//
// **ADR 0472** gave the four container doors (membership, equality, the element slot, the fold) a question
// to ask a `sorted`/`reversed`, and the loop had already been given two (**ADR 0339**). What none of them
// asked was the question this file is about: what is the ARGUMENT? The container tables answered it with a
// spelling — `staticContainerKind`, a record of what a NAME holds, true of a list literal, a list variable
// and a numeric slice — and every iterable the reference builds a list from but this backend had no name
// for fell out of the doors:
//
//	for v in sorted("bca")            refused  (the reference prints a, b, c)
//	for k in sorted(d)  d a dict      refused  (the reference prints its KEYS)
//	for v in sorted(range(3))         refused  (the reference prints 0, 1, 2)
//	print(sorted("bca"))              printed 0  — the heap handle, at the exit code of success
//	print(sorted(range(3)))           printed the address of @.lst1 with %d
//	xs = ["b", "a"]                   printed ['b', 'a'] — the reference prints ['a', 'b'], because
//	print(sorted(xs[0:2]))            @rt_sort chose its comparator from the ARGUMENT'S SPELLING
//	xs = sorted("bca")                printed 2, the interned index — the binding recorded no element kind
//	print(xs[0])
//
// One substitution fixes all of them, asked as ONE question: `orderingSequenceArg` spells the list the
// reference builds — the CHARACTERS of a text (code points, **ADR 0225**/L11.5, never bytes), the KEYS of a
// dict (the same `@rt_dict_keys` stride **ADR 0417** paid for), and the integers of a `range(...)` whose
// bounds the program wrote. The roads that decide, name, build, print and bind all ask it and the kind door
// beside them, so one program cannot be walked by three sequences. The comparator comes with it: @rt_sort is
// told what the OBJECT's elements are, which is the end of the exit-0 wrong answer above — a list of text
// sliced into `sorted` was ordered by payload, that is, by arrival.
//
// What the row does NOT do is invent a kind. A float list, a mixed list and a call the scan cannot see every
// arm of still refuse, by name, because a loop variable holds one word and the tagged pair is the value word
// this backend does not have yet (**Gap R.248**, **Gap R.250**). A pinned refusal that moved to an ANSWER
// moved because the compiled leg and the reference agree; the two rows where they do not are filed below with
// both values, not fixed by deleting the row.

// orderingArgumentAnswered are the programs this row newly answers. `want` is what CPython prints for the
// same source — measured against the pinned reference, never remembered — and every row is asked of the
// compiler, of the JIT, of the record, and in the integration file of the CLI against python3.
var orderingArgumentAnswered = []struct {
	src  string
	want string
}{
	// ---- a TEXT is its characters, in code-point order ----
	{`print(sorted("bca"))`, "['a', 'b', 'c']"},
	{`print(sorted("café"))`, "['a', 'c', 'f', 'é']"}, // four members, not six bytes (L11.5)
	{`print(sorted(""))`, "[]"},
	{`print(sorted("bca", reverse=True))`, "['c', 'b', 'a']"},
	{`t = "bca"` + "\n" + `print(sorted(t))`, "['a', 'b', 'c']"},
	{`print(sorted("banana"))`, "['a', 'a', 'a', 'b', 'n', 'n']"},
	{`print(sorted(sorted("bca")))`, "['a', 'b', 'c']"},
	{`for v in sorted("bca"):` + "\n    " + `print(v)`, "a\nb\nc"},
	// ---- a DICT is its KEYS, in the order the program wrote the entries ----
	{`print(sorted({"b": 1, "a": 2}))`, "['a', 'b']"},
	{`print(sorted({3: 1, 1: 2}))`, "[1, 3]"},
	{`print(sorted({"b": 1}))`, "['b']"},
	{`d = {"b": 1, "a": 2}` + "\n" + `print(sorted(d))`, "['a', 'b']"},
	{`for k in sorted({"b": 1, "a": 2}):` + "\n    " + `print(k)`, "a\nb"},
	{`for v in reversed({1: 2}):` + "\n    " + `print(v)`, "1"},
	// ---- a `range(...)` the program wrote out is its integers ----
	{`print(sorted(range(3)))`, "[0, 1, 2]"},
	{`print(sorted(range(2, 5)))`, "[2, 3, 4]"},
	{`print(sorted(range(5, 2, -1)))`, "[3, 4, 5]"},
	{`print(sorted(range(0)))`, "[]"},
	{`print(sorted(range(3), reverse=True))`, "[2, 1, 0]"},
	{`print(len(sorted(range(2, 10, 3))))`, "3"},
	{`for v in sorted(range(3)):` + "\n    " + `print(v)`, "0\n1\n2"},
	// ---- the doors beside the loop read the same sequence ----
	{`print("a" in sorted("bca"))`, "True"},
	{`print(1 in sorted(range(3)))`, "True"},
	{`print(sorted({"b": 1, "a": 2}) == ["a", "b"])`, "True"},
	{`print([sorted("bca")])`, "[['a', 'b', 'c']]"},
	{`print([sorted({"b": 1, "a": 2})])`, "[['a', 'b']]"},
	{`print([sorted(range(2))])`, "[[0, 1]]"},
	{`print(sorted(sorted([3, 1, 2])))`, "[1, 2, 3]"},
	// ---- the object, not the spelling, decides the comparator (the exit-0 wrong answers) ----
	{`xs = ["b", "a"]` + "\n" + `print(sorted(xs[0:2]))`, "['a', 'b']"},
	{`xs = ["b", "a"]` + "\n" + `print(sorted(xs[0:2], reverse=True))`, "['b', 'a']"},
	{`xs = [1, 2]` + "\n" + `print(sorted(xs[0:2]))`, "[1, 2]"},
	// ---- the BINDING: the name receives the list, and has to say what its slots hold ----
	{`xs = sorted("bca")` + "\n" + `print(xs)`, "['a', 'b', 'c']"},
	{`xs = sorted("bca")` + "\n" + `print(xs[0])`, "a"},
	{`xs = sorted("banana")` + "\n" + `print(xs[3])`, "b"},
	{`xs = sorted("café")` + "\n" + `print(xs[3])`, "é"},
	{`xs = sorted("bca")` + "\n" + `for v in xs:` + "\n    " + `print(v)`, "a\nb\nc"},
	{`xs = sorted("bca")` + "\n" + `print(len(xs))`, "3"},
	{`xs = sorted("bca")` + "\n" + `print(xs[0:2])`, "['a', 'b']"},
	{`xs = sorted("bca")` + "\n" + `print(xs[0] + "x")`, "ax"},
	{`xs = sorted("bca")` + "\n" + `print("b" in xs)`, "True"},
	{`xs = sorted("bca")` + "\n" + `print(xs == ["a", "b", "c"])`, "True"},
	{`xs = sorted("bca")` + "\n" + `xs.append("z")` + "\n" + `print(xs)`, "['a', 'b', 'c', 'z']"},
	{`xs = sorted(range(3))` + "\n" + `print(xs[1])`, "1"},
	{`xs = sorted(range(3))` + "\n" + `for v in xs:` + "\n    " + `print(v)`, "0\n1\n2"},
	{`xs = sorted({"b": 1, "a": 2})` + "\n" + `print(xs[1])`, "b"},
	{`xs = sorted({"b": 1, "a": 2})` + "\n" + `for v in xs:` + "\n    " + `print(v)`, "a\nb"},
}

// orderingArgumentNeighbours are the roads this row had no business touching: the fold in the value
// position, the keyed door, the text reversal `reversed` performs on a string, and the literal haystack the
// loop answers with no object at all. Each answered before; a widening that broke one would be a second
// answer for one construct.
var orderingArgumentNeighbours = []struct {
	src  string
	want string
}{
	{`print(sorted([3, 1, 2]))`, "[1, 2, 3]"},
	{`for v in sorted([3, 1, 2]):` + "\n    " + `print(v)`, "1\n2\n3"},
	{`print(sum(sorted([3, 1, 2])))`, "6"},
	{`print(len(sorted([1, 2, 3])))`, "3"},
	{`print(sorted([3, 1, 2], key=lambda x: 0 - x))`, "[3, 2, 1]"},
	{`xs = [3, 1, 2]` + "\n" + `print(sorted(xs))`, "[1, 2, 3]"},
	{`xs = [3, 1, 2]` + "\n" + `for v in reversed(xs):` + "\n    " + `print(v)`, "2\n1\n3"},
	{`print(sorted(["b", "a"]))`, "['a', 'b']"},
	{`print(sorted({1, 2}))`, "[1, 2]"}, // a SET reaches the print door through the container tables; unchanged by this row
}

// orderingArgumentRefused are the shapes the doors still refuse — each with the sentence that names the half
// the reference has and this backend does not (ADR 0249), and the roadmap row that owns it. A refusal here
// that stopped describing the shape is the second-worst answer ADR 0249 ranks.
var orderingArgumentRefused = []struct {
	src      string
	sentence string
	roadmap  string
	cites    bool // does the sentence itself name the row that owns the half (ADR 0166)?
}{
	// A float list: one loop variable holds one word, and the pair is the value word this backend lacks.
	{`for v in sorted([1.5, 0.5]):` + "\n    " + `print(v)`, "its elements are not all integers or all text", "Gap R.248", true},
	{`xs = [1, "a"]` + "\n" + `for v in sorted(xs):` + "\n    " + `print(v)`, "ELEMENT kind", "Gap R.248", true},
	// A SET keeps its members in its own table, which @rt_sort does not walk — in the loop and in the
	// print door alike, where the alternative answer was the handle printed with `%d`.
	{`for v in sorted({1, 2}):` + "\n    " + `print(v)`, "its argument is a SET", "Gap R.247", true},
	// The reference's key, standing where this backend's repeat flag goes — the reference stops this program.
	{`for v in sorted([3, 1, 2], True):` + "\n    " + `print(v)`, "second POSITIONAL argument", "Gap R.249", true},
	{`print(sorted([1, "a"]))`, "of more than one kind", "Gap R.248", true}, // the sort door, not the print door, speaks
	// A `range` whose bound the program did not write: no constant, no list, and the value road has no
	// table for a builtin call's answer (**Gap R.250**'s half, not this row's).
	{`n = 3` + "\n" + `for v in sorted(range(n)):` + "\n    " + `print(v)`, "whose bound the program did not write", "Gap R.250", true},
	// A subscript of the ordering's answer: the elements the fold cannot see, so the index door asks for a
	// literal it does not have. Filed, not fixed — the answer is a refusal, never the reference's answer
	// printed wrongly (**Gap R.576**, and the same door for a list literal of text, **Gap R.577**).
	{`print(sorted("bca")[0])`, "index requires an inline list/dict/set literal", "Gap R.576", false},
	{`print(sorted(["b", "a"])[0])`, "index requires an inline list/dict/set literal", "Gap R.577", false},
	// The alias of a bound ordering: the binding door records the object, the alias door copies the claims of
	// a name it can see a literal behind (**Gap R.578**).
	{`xs = sorted("bca")` + "\n" + `yy = xs` + "\n" + `print(yy[0])`, "index of a non-literal variable", "Gap R.578", false},
}

// orderingArgumentFiledNotFixed pins the divergences this row measured and filed rather than fixed, with the
// reference's answer beside the compiled one. `1 in sorted("bca")` is the payload-collision family —
// **Gap R.575**'s number needle against interned-text payloads — newly reachable now that a text reaches the
// membership door; the honest answer is the reference's, and the row that owns it is R.575.
var orderingArgumentFiledNotFixed = []struct {
	src      string
	compiled string
	oracle   string
	row      string
}{
	{`print(1 in sorted("bca"))`, "True", "False", "Gap R.575"},
	{`print(1 in sorted({"b": 1, "a": 2}))`, "True", "False", "Gap R.575"},
	{`print(sorted([1.5, 0.5]))`, "[1.5, 0.5]", "[0.5, 1.5]", "Gap R.248"},
	// Printing the ITERATOR `reversed` returns is **Gap R.251**'s shape; over a TEXT it prints `0`, the heap
	// handle, on both entry points and on the parent build — pre-existing, filed here so the row that owns it
	// inherits the measurement rather than re-deriving it.
	{`print(reversed("abc"))`, "0", "<reversed object at 0x…>", "Gap R.251"},
}

// orderingArgumentDoors are the four questions the row leaves behind it, asked of the parser's own node. The
// columns are the doors: what the FOLD writes down, whether the loop can WALK the sequence, what the KIND
// door says its elements are, and whether the PRINT door sends the answer to a container printer. A row with
// kind=true and walkable=false is a shape the loop refuses while a door builds it; print=true with
// walkable=false is the `%d` of a handle. Neither is allowed anywhere in this table.
var orderingArgumentDoors = []struct {
	expr     string
	fold     bool
	walkable bool
	kind     bool
	printArg bool
}{
	// The three iterables this row added, and the nest: all four doors say yes to all four.
	{`sorted("bca")`, false, true, true, true},
	{`sorted("café")`, false, true, true, true},
	{`sorted({"b": 1, "a": 2})`, false, true, true, true},
	{`sorted({3: 1, 1: 2})`, false, true, true, true},
	{`sorted(range(3))`, false, true, true, true},
	{`sorted(range(5, 2, -1))`, false, true, true, true},
	{`sorted(sorted("bca"))`, false, true, true, true},
	{`sorted(sorted([3, 1, 2]))`, true, true, true, true},
	// The shapes the row left alone: the literal the fold writes down, and the name the runtime road walks.
	{`sorted([3, 1, 2])`, true, true, true, true},
	{`sorted(["b", "a"])`, false, true, true, true},
	{`sorted([3, 1, 2][0:2])`, false, true, true, true},
	// No kind, no walk, no print door — and no fabricated answer either (**Gap R.248**, **Gap R.250**).
	{`sorted([1.5, 0.5])`, false, false, false, true},
	{`sorted([1, "a"])`, false, false, false, true},
	{`sorted({1, 2})`, false, false, false, true},
	{`sorted(range(n))`, false, false, false, false},
	// `reversed` is the ITERATOR (**Gap R.251**, **ADR 0465**): the loop walks it, and the container doors
	// name no kind behind it. Its print door stays the narrow one — a text handed to `reversed` is the TEXT
	// road's reversal, printed as a string.
	{`reversed("abc")`, true, true, true, false},
	{`reversed([1, 2, 3])`, true, true, true, true},
}

// TestTheOrderingBuildsWhatTheReferenceBuilds is the compiled leg of every shape in the table: the same
// stdout the pinned reference prints, at the exit code of success, through the compiler and through the JIT.
func TestTheOrderingBuildsWhatTheReferenceBuilds(t *testing.T) {
	t.Parallel()
	for _, tt := range orderingArgumentAnswered {
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

// TestTheOrderingArgumentDoorsAnswerAsOnePair is the row's structural claim: the four doors are one question
// asked four times. Any row where the print door claims a container the walk cannot reach is a `%d` of a
// handle; any row where a kind is named for a sequence the loop will not walk is a tag over nothing
// (**ADR 0333**'s pair, extended from the value word to the ITERABLE by **ADR 0473**).
func TestTheOrderingArgumentDoorsAnswerAsOnePair(t *testing.T) {
	t.Parallel()
	for _, tc := range orderingArgumentDoors {
		t.Run(tc.expr, func(t *testing.T) {
			t.Parallel()
			g := newSortedContainerProbeGen()
			call, ok := parseExprForTest(t, "x = "+tc.expr).(*Call)
			if !ok {
				t.Fatalf("%q does not parse as a call expression", tc.expr)
			}
			if _, _, isCall := g.containerSortBuiltin(call); !isCall {
				t.Fatalf("%q is not an ordering builtin to the door that names them", tc.expr)
			}
			_, folded := g.foldedIterableElems(call)
			if folded != tc.fold {
				t.Fatalf("the fold door said %v for %q, want %v", folded, tc.expr, tc.fold)
			}
			_, walkable := g.sortedReversedOverWalkableList(call)
			if walkable != tc.walkable {
				t.Fatalf("the walk door said %v for %q, want %v", walkable, tc.expr, tc.walkable)
			}
			_, kindKnown := g.sortedReversedContainerElemKind(call)
			if kindKnown != tc.kind {
				t.Fatalf("the kind door said %v for %q, want %v", kindKnown, tc.expr, tc.kind)
			}
			printArg := g.printRoadSeesAnOrderingArgument(call.Fn.(*Name).Value, call.Args[0])
			if printArg != tc.printArg {
				t.Fatalf("the print door said %v for %q, want %v", printArg, tc.expr, tc.printArg)
			}
			// The pair rule. `printArg` is deliberately WIDER than `walkable`: the print road claims the
			// argument whenever its shape is a container or a substituted sequence, and then PROPAGATES the
			// build door's refusal — which is what keeps `print(sorted([1.5, 0.5]))` a sentence rather than a
			// `%d` of a handle (the rows in orderingArgumentRefused are the witness; a print road that let the
			// numeric road answer is caught there, not here). What must never happen is a KIND named for a
			// sequence no loop walks: that is a tag written over an object nobody built.
			if kindKnown && !walkable {
				t.Fatalf("%q: a kind is named for a sequence no loop walks", tc.expr)
			}
			if walkable && !g.sortedReversedContainerIsNameable(call) {
				t.Fatalf("%q: walkable but not nameable — the two halves of the build door disagree", tc.expr)
			}
		})
	}
}

// printfOfAContainerGlobalIR is the shape the print door exists to prevent: a container CONSTANT — the fold's
// `@.lstN` — handed to printf as the i32 argument of a `%d`. `llc` refuses it (exit 2); before this row the
// module that survived only refused to SAY it was a list, and printed `0` at exit 0.
var printfOfAContainerGlobalIR = regexp.MustCompile(`printf\([^)]*i32 @\.`)

// textSortGoesToTheRuntimeDoor is the other half of the claim: a TEXT is ordered by @rt_sort's text
// comparator, not by a compile-time copy of an ordering this backend would have to keep in step with strcmp.
var textSortGoesToTheRuntimeDoor = regexp.MustCompile(`call void @rt_sort\(i32 %[^,]+, i32 1\)`)

// TestThePrintDoorNeverPrintsAContainerWordAsANumber is the IR claim, over every program in every table.
func TestThePrintDoorNeverPrintsAContainerWordAsANumber(t *testing.T) {
	t.Parallel()
	probes := []string{}
	for _, tt := range orderingArgumentAnswered {
		probes = append(probes, tt.src)
	}
	for _, tt := range orderingArgumentNeighbours {
		probes = append(probes, tt.src)
	}
	for _, src := range probes {
		ir := mustCompileIR(t, src)
		if printfOfAContainerGlobalIR.MatchString(ir) {
			t.Fatalf("%q prints a container global with %%d — the ordering's answer never reached the container printer:\n%s", src, ir)
		}
	}
	// The text ordering is the runtime door's, with the text comparator (mode 1) — not a fold this backend
	// would have to keep in agreement with strcmp.
	if ir := mustCompileIR(t, `print(sorted("bca"))`); !textSortGoesToTheRuntimeDoor.MatchString(ir) {
		t.Fatalf("the text sort did not ask @rt_sort for its text comparator:\n%s", ir)
	}
	// …and the counter-row: the VALUE position keeps the compile-time constant **ADR 0472** left alone, so
	// the guard above cannot be satisfied by deleting the fold everywhere.
	if ir := mustCompileIR(t, "xs = sorted([3, 1, 2])\nprint(xs[0])"); !strings.Contains(ir, "@.lst") {
		t.Fatalf("the value road no longer emits the fold's constant — the repair deleted a road it was not asked about:\n%s", ir)
	}
}

// newOrderingBindingProbeGen is `newSortedContainerProbeGen` plus the notebooks the BINDING door writes:
// `settleContainerKind` clears every kind's claims before settling one, and a probe that hands it nil maps
// panics on the first program rather than telling anyone the door is under-exercised.
func newOrderingBindingProbeGen() *irGen {
	return &irGen{
		listVars:      map[string]bool{},
		runtimeDicts:  map[string]bool{},
		runtimeSets:   map[string]bool{},
		mixedLists:    map[string]bool{},
		mixedDicts:    map[string]bool{},
		mixedSets:     map[string]bool{},
		listElemStr:   map[string]bool{},
		listElemInt:   map[string]bool{},
		setElemStr:    map[string]bool{},
		setElemInt:    map[string]bool{},
		dictKeyStr:    map[string]bool{},
		dictKeyInt:    map[string]bool{},
		dictValStr:    map[string]bool{},
		dictValInt:    map[string]bool{},
		containerLits: map[string]Expr{},
		noneVars:      map[string]bool{},
		boolVars:      map[string]bool{},
		strVals:       map[string]string{},
		dictVals:      map[string]*DictLit{},
		internedVars:  map[string]bool{},
		containerRet:  map[string]containerAnswer{},
	}
}

// TestABoundOrderingAnswersItsOwnElements is the binding door's content: the three claims a container binding
// writes, asked of the door itself, because a program that prints right can still read a slot wrong — the
// shape this row found with `print(xs)` right and `print(xs[0])` printing the interned index.
func TestABoundOrderingAnswersItsOwnElements(t *testing.T) {
	t.Parallel()
	claims := []struct {
		src     string
		kind    string
		elemStr bool
		elemInt bool
		mixed   bool
	}{
		{`xs = sorted("bca")`, "list", true, false, false},
		{`xs = sorted("café")`, "list", true, false, false},
		{`xs = sorted({"b": 1, "a": 2})`, "list", true, false, false},
		{`xs = sorted(range(3))`, "list", false, true, false},
		{`xs = sorted([3, 1, 2])`, "list", false, true, false},
		{`xs = sorted(["b", "a"])`, "list", true, false, false},
		{`xs = reversed([3, 1, 2])`, "list", false, true, false},
	}
	for _, tc := range claims {
		t.Run(tc.src, func(t *testing.T) {
			t.Parallel()
			name := strings.TrimPrefix(tc.src, "xs = ")
			ir := mustCompileIR(t, tc.src+"\nprint("+name+")")
			if printfOfAContainerGlobalIR.MatchString(ir) {
				t.Fatalf("%q prints a container global with %%d:\n%s", tc.src, ir)
			}
			// The claims themselves: the element kind the binding wrote is the one the subscript, the
			// loop and the printer all read.
			g := newOrderingBindingProbeGen()
			call, ok := parseExprForTest(t, "x = "+strings.TrimPrefix(tc.src, "xs = ")).(*Call)
			if !ok {
				t.Fatalf("%q does not parse as a call", tc.src)
			}
			if !g.recordOrderingBinding(name, call) {
				t.Fatalf("%q: the binding door declined to record the answer at all", tc.src)
			}
			if g.containerKindHeld(name) != tc.kind {
				t.Fatalf("%q bound %q as %q, want %q", tc.src, name, g.containerKindHeld(name), tc.kind)
			}
			if g.listElemStr[name] != tc.elemStr || g.listElemInt[name] != tc.elemInt || g.mixedLists[name] != tc.mixed {
				t.Fatalf("%q claimed (str=%v int=%v mixed=%v), want (str=%v int=%v mixed=%v)",
					tc.src, g.listElemStr[name], g.listElemInt[name], g.mixedLists[name],
					tc.elemStr, tc.elemInt, tc.mixed)
			}
		})
	}
}

// TestTheOrderingArgumentRefusalsNameWhatIsMissing keeps ADR 0249's promise: the shapes the doors still
// decline are refused in words that describe THAT shape, and none of them reaches the toolchain.
func TestTheOrderingArgumentRefusalsNameWhatIsMissing(t *testing.T) {
	t.Parallel()
	for _, tt := range orderingArgumentRefused {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, tt.src)
			if !refused {
				t.Fatalf("%q compiled and printed %q; the row expects the refusal that names %s", tt.src, out, tt.roadmap)
			}
			msg := out
			if strings.Contains(msg, "llc") || strings.Contains(msg, "Verification") {
				t.Fatalf("%q reached the toolchain (exit 2 territory): %s", tt.src, msg)
			}
			if !strings.Contains(msg, tt.sentence) {
				t.Fatalf("%q refused with %q, which does not describe the shape (ADR 0249); %s owns the missing half", tt.src, msg, tt.roadmap)
			}
			if tt.cites && !strings.Contains(msg, "roadmap") {
				t.Fatalf("%q refused without citing the roadmap row that owns the half it lacks: %s", tt.src, msg)
			}
			if !tt.cites && !strings.Contains(msg, "index") {
				t.Fatalf("%q refused with a sentence from no door at all: %s", tt.src, msg)
			}
		})
	}
}

// TestTheNeighboursKeepTheirAnswers is the width check: the roads the row was not asked about keep the answers
// they had, byte for byte.
func TestTheNeighboursKeepTheirAnswers(t *testing.T) {
	t.Parallel()
	for _, tt := range orderingArgumentNeighbours {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); it answered before this row", tt.src, got)
			}
			if strings.TrimSpace(got) != tt.want {
				t.Fatalf("%q printed %q, want %q", tt.src, strings.TrimSpace(got), tt.want)
			}
		})
	}
}

// TestWhatThisRowLeavesFiledNotFixed pins the divergences with both legs' answers, so the next cycle inherits
// the measurement and cannot fix one by deleting the row.
func TestWhatThisRowLeavesFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tt := range orderingArgumentFiledNotFixed {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); the filed row %s expects the compiled answer %q", tt.src, got, tt.row, tt.compiled)
			}
			if strings.TrimSpace(got) != tt.compiled {
				t.Fatalf("%q: the compiled leg moved from %q to %q — %s was fixed, or this row drifted; update both",
					tt.src, tt.compiled, strings.TrimSpace(got), tt.row)
			}
		})
	}
}

// TestTheOrderingRowsAnswersAreOnTheRecord is the record leg: every program this row newly answers has an entry in
// testdata/interpreter-golden.json holding the same stdout, and a MISSING entry fails the case — coverage of
// this row cannot be deleted by deleting a record (**ADR 0302**).
func TestTheOrderingRowsAnswersAreOnTheRecord(t *testing.T) {
	t.Parallel()
	for _, tt := range orderingArgumentAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			g := goldenLookup(t, tt.src)
			if !g.HasStd {
				t.Fatalf("%q is on the record with no stdout, but the row prints %q", tt.src, tt.want)
			}
			if g.Stdout != tt.want+"\n" {
				t.Fatalf("%q: the record holds %q and the row expects %q — one of them has to move, and the reference is the referee", tt.src, g.Stdout, tt.want+"\n")
			}
		})
	}
}

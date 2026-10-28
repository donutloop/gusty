package lang

import (
	"strings"
	"testing"
)

// The count door PERFORMS the elements whose count it takes (roadmap **Gap R.494**, **ADR 0441**).
//
// `len([1, 2, 3])` is a number the spelling states, so the length door answers it from the element
// list and never builds the container. That is an optimisation, and — exactly like **ADR 0192**'s
// comprehension fold — it is not the meaning: the reference evaluates every element expression,
// builds the container, and only then asks the object how many slots it holds. An element is not
// only a value in this backend; it is an operation. It can call a function that prints, read a slot
// whose accident the subscript door raises (**Gap R.227**'s road), divide by zero, or run a
// comprehension that loops. The count door skipped all of that, and skipped it QUIETLY — every row
// below was measured off `94a1458` answering at the exit code of SUCCESS, which is the class of
// defect this house files loudest (**ADR 0166** reserves exit 2 for the compiler's own failure and
// exit 0 for an answer the program is entitled to).
//
// print(len([f()]))                     answered `1`        reference: `side` then `1`
// print(len([[1, 2][0][0], 1]))         answered `2`        reference: TypeError: 'int' object is not subscriptable
// print(len([1 // 0]))                  answered `1`        reference: ZeroDivisionError: …
// print(len([[1, 2][5]]))               answered `1`        reference: IndexError: list index out of range
//
// The rule is **Gap R.149**'s (**ADR 0275**): *the operand the test did not choose is still
// evaluated* — here "did not choose" is "threw the object away". `(*irGen).lowerLiteralElementsForEffect`
// asks each element one pure question — is this an operation, or a spelling? — lowers the operations
// in the order the reference evaluates them, and discards their words. Two rules keep it a door and
// not a broom: it builds into a scratch buffer and commits only when every element lowered (the
// discipline `chain_operand.go` and `answer_kind.go` exist for — an instruction after a terminator
// is exit 2, paid by the compiler), and an element with no lowering leaves the count the door answers
// TODAY rather than refusing a program that has always answered (`len` of a dict literal whose value
// is a call has always stopped the build in the dict road's own words, and it still does, unchanged
// by this commit).
//
// Nothing here claims a second engine ran: every expectation is what `python3` prints for the same
// source, cross-checked against `testdata/interpreter-golden.json` (the record leg, `runGoldenStdout`
// and `recordTrapOf`), and the sources the recorder refuses are filed rows, not deleted cases.

// theSideEffectFn is the element this family writes its operations with: a call the compiler cannot
// constant-fold, whose whole purpose is the line it prints.
const theSideEffectFn = "def f():\n    print(\"side\")\n    return 1\n"

// lengthElementEffects are the elements that ACT: each row is a container literal whose LENGTH the
// door reads off the spelling, and an element inside it that the reference has to run first.
var lengthElementEffects = []struct {
	name string
	src  string
	want string // what CPython prints, stdout only
}{
	{"the row as filed, one element that calls", theSideEffectFn + "print(len([f()]))", "side\n1"},
	{"the call is not the first element", theSideEffectFn + "print(len([1, f(), 3]))", "side\n3"},
	{"the call is inside a nested literal", theSideEffectFn + "print(len([1, [f(), 2], 3]))", "side\n3"},
	{"the call is in the first element of a nested literal", theSideEffectFn + "print(len([[f(), 2], 3]))", "side\n2"},
	{"a dict literal's value calls", theSideEffectFn + "print(len({\"a\": f()}))", "side\n1"},
	{"a set literal's element calls", theSideEffectFn + "print(len({f(), \"b\"}))", "side\n2"},
	{"a sorted literal's element calls", theSideEffectFn + "print(len(sorted([f(), 2])))", "side\n2"},
	{"two calls, in the order the program wrote them", theSideEffectFn + "print(len([f(), f()]))", "side\nside\n2"},
	{"a tuple literal's element calls", theSideEffectFn + "print(len((f(), 2)))", "side\n2"},
	{"a nested call next to a nested len", theSideEffectFn + "print(len([f(), len([f()])]))", "side\nside\n2"},
	{"a literal holding a comprehension the run time builds", "print(len([[x for x in [1, 2]], 1]))", "2"},
}

// TestTheCountDoorPerformsTheElementsItCounts is the answering half: the lines the elements print
// appear, in the reference's order, before the count the door already knew how to state. Both legs
// are asked — the compiled module, and the record's answer for the same source.
func TestTheCountDoorPerformsTheElementsItCounts(t *testing.T) {
	t.Parallel()
	for _, tt := range lengthElementEffects {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimRight(compiledOut(t, tt.src), "\n"); got != tt.want {
				t.Fatalf("printed %q, the reference prints %q", got, tt.want)
			}
			if rec, err := runGoldenStdout(t, tt.src); err == nil {
				if strings.TrimRight(rec, "\n") != tt.want {
					t.Fatalf("the record says %q and the compiled leg says %q for the same source", rec, tt.want)
				}
			}
		})
	}
}

// lengthElementTraps are the elements that RAISE. A count door that skips a raising element does not
// merely lose a line of output — it answers a program that, in the reference, never reaches an answer
// at all, at the exit code of success.
var lengthElementTraps = []struct {
	name     string
	prologue string // a binding the program writes BEFORE the block, never inside one
	src      string
	class    string
	message  string
	stdout   string // what the elements printed before the raise (the reference's, in order)
}{
	{
		"the filed row: an element whose read raises",
		"",
		"print(len([[1, 2][0][0], 1]))",
		"TypeError", "'int' object is not subscriptable", "",
	},
	{
		"an element that reads past the end",
		"",
		"print(len([[1, 2][5]]))",
		"IndexError", "list index out of range", "",
	},
	{
		"an element that divides by zero",
		"",
		"print(len([1 // 0]))",
		"ZeroDivisionError", "integer division or modulo by zero", "",
	},
	{
		// The order is the reference's and this is the row that proves the door walks the elements
		// in it: the first element's accident is the one the program reaches, so the second element's
		// (an out-of-range read) must stay unheard.
		"the first element's accident wins the order",
		"",
		"print(len([1 // 0, [2][5]]))",
		"ZeroDivisionError", "integer division or modulo by zero", "",
	},
	{
		// The same read the fold answers, one road over: `[1, 2][0]` is the integer `2` and the outer
		// read is `2[0]`, which the subscript door raises (**Gap R.227**, **ADR 0440**). The count door
		// performs the element, so it inherits that raise — and the element being the ONLY thing in the
		// literal is the row that proves the count did not come from the spelling alone.
		"a literal holding nothing but a read that raises",
		"",
		"print(len([[1, 2][0][0]]))",
		"TypeError", "'int' object is not subscriptable", "",
	},
	{
		// The other half of the same claim: an element that PRINTS and then an element that raises —
		// the line must be on the tool channel before the raise, exactly as the reference prints it.
		// The helper is bound before the `try:` rather than inside it on purpose: a `def` written inside
		// a block is its own toolchain failure (roadmap Gap R.497, pinned below), and a row that needs
		// it would be measuring two doors at once.
		"a call prints before a later element raises",
		theSideEffectFn,
		"print(len([f(), 1 // 0]))",
		"ZeroDivisionError", "integer division or modulo by zero", "side\n",
	},
}

// TestTheCountDoorsElementsRaiseWhatTheReferenceRaises walks the trap family on the compiled leg
// (`jitTrap` fails the run on exit 2 — ADR 0166's code — so a module the assembler rejects cannot
// pass as a raise) and then asks the record the same question.
func TestTheCountDoorsElementsRaiseWhatTheReferenceRaises(t *testing.T) {
	t.Parallel()
	for _, tt := range lengthElementTraps {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			class, message := jitTrap(t, tt.prologue+tt.src)
			if class != tt.class || message != tt.message {
				t.Fatalf("raised %s: %s, want %s: %s", class, message, tt.class, tt.message)
			}
			rec, trapped := recordTrapOf(t, tt.prologue+tt.src)
			if !trapped {
				t.Fatalf("the record ANSWERS this source; the compiled leg raises %s: %s, so one of the two is wrong", tt.class, tt.message)
			}
			if rec.ExnType != tt.class || rec.ExnMsg != tt.message {
				t.Fatalf("the record holds %s: %s and the compiled leg raised %s: %s — the reference is the definition, the record is the evidence",
					rec.ExnType, rec.ExnMsg, class, message)
			}
		})
	}
}

// What the elements PRINTED before they raised is half of the order claim, and `jitTrap` reads only
// the trap report, so the rows that print and then raise are measured a second way here, through the
// JIT's own split streams: the program's exit class, its stdout being EXACTLY the lines the elements
// printed, and the raise on the other channel.
func TestTheCountDoorsElementsPrintBeforeTheyRaise(t *testing.T) {
	t.Parallel()
	for _, tt := range lengthElementTraps {
		if tt.stdout == "" {
			continue
		}
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := JIT(tt.prologue+tt.src, 0)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference answers with a raise: %v", err)
			}
			if res.Code == 2 {
				t.Fatalf("the toolchain failed on its own program (exit 2):\n%s", res.Stderr)
			}
			if res.Code != 3 {
				t.Fatalf("exit %d, want the trap class 3; stdout %q", res.Code, res.Output)
			}
			if res.Output != tt.stdout {
				t.Fatalf("the elements printed %q before the raise, want the reference's %q", res.Output, tt.stdout)
			}
			if !strings.Contains(res.Stderr, tt.class+": "+tt.message) {
				t.Fatalf("the raise on the tool channel is not the reference's %s: %s:\n%s", tt.class, tt.message, res.Stderr)
			}
			if strings.Contains(res.Output, "Traceback") {
				t.Fatalf("the trap report leaked into the program's stdout: %q", res.Output)
			}
		})
	}
}

// The raises are the program's, so the program can catch them: the same sources wrapped in the
// handler they write, at the exit code that means the program finished. This is the half that a
// host-side refusal (exit 1, `codegen:`) could never pass.
func TestTheCountDoorsElementRaisesReachTheProgramsOwnHandler(t *testing.T) {
	t.Parallel()
	for _, tt := range lengthElementTraps {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			indented := strings.ReplaceAll(tt.src, "\n", "\n    ")
			prog := tt.prologue + "try:\n    " + indented + "\nexcept " + tt.class + " as e:\n    print(\"caught\", e)"
			// Whatever the elements printed on the way is the program's too — the handler runs after them.
			want := tt.stdout + "caught " + tt.message
			if out := strings.TrimRight(compiledOut(t, prog), "\n"); out != strings.TrimRight(want, "\n") {
				t.Fatalf("the raise is not catchable with its sentence: got %q, want %q", out, strings.TrimRight(want, "\n"))
			}
		})
	}
}

// The controls are what makes this a gate rather than a list: a container of SPELLINGS still answers
// its count with no builder, no allocation and no call in the module — the row's own negative control
// (`len([1, 2, 3])` emits no instruction for its elements), and the count door's constant folding has
// to survive the commit that made it perform the elements that act.
func TestTheCountDoorStillAnswersAContainerOfSpellingsWithoutBuildingIt(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		src  string
		want string
	}{
		{"print(len([1, 2, 3]))", "3"},
		{"print(len([1, [2, 3], 4]))", "3"},
		{"print(len([[1, 2], [3, 4]][0]))", "2"},
		{"print(len({\"a\": 1, \"b\": 2}))", "2"},
		{"print(len([1, 2]) + len([3, 4]))", "4"},
		{"print(len([1, 2, 3]) * 2)", "6"},
		{"xs = [1, 2, 3]\nprint(len(xs))", "3"},
		{theSideEffectFn + "print(len([f()]))", "side\n1"},
	} {
		tt := tt
		t.Run(tt.src, func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimRight(compiledOut(t, tt.src), "\n"); got != tt.want {
				t.Fatalf("answered %q, want the reference's %q", got, tt.want)
			}
			if rec, err := runGoldenStdout(t, tt.src); err == nil {
				if strings.TrimRight(rec, "\n") != tt.want {
					t.Fatalf("the record says %q and the compiled leg says %q for the same source", rec, tt.want)
				}
			}
		})
	}
	for _, tt := range []struct {
		src      string
		mustHave string // the effect the module has to carry, "" for the pure rows
	}{
		{"print(len([1, 2, 3]))", ""},
		{"print(len([1, [2, 3], 4]))", ""},
		{"print(len({\"a\": 1, \"b\": 2}))", ""},
		{theSideEffectFn + "print(len([f()]))", "call i32 @gy_f"},
		{theSideEffectFn + "print(len([1, [f(), 2], 3]))", "call i32 @gy_f"},
	} {
		t.Run("module/"+tt.src, func(t *testing.T) {
			t.Parallel()
			ir := mustCompileIR(t, tt.src)
			at := strings.Index(ir, "define i32 @main()")
			if at < 0 {
				t.Fatalf("the module for %q has no `main`:\n%s", tt.src, ir)
			}
			body := ir[at:]
			if end := strings.Index(body, "\n}"); end >= 0 {
				body = body[:end]
			}
			if tt.mustHave == "" {
				// No builder, no allocation: the count is the constant the spelling states.
				for _, forbidden := range []string{"call i32 @rt_list_new", "call i32 @rt_dict_new", "call i32 @rt_set_new"} {
					if strings.Contains(body, forbidden) {
						t.Fatalf("the count door built a container just to throw it away in %q (%s):\n%s", tt.src, forbidden, body)
					}
				}
				return
			}
			if !strings.Contains(body, tt.mustHave) {
				t.Fatalf("the element's operation is not in the module for %q (want %q):\n%s", tt.src, tt.mustHave, body)
			}
		})
	}
}

// The predicate is asked directly, because a gate widened by accident must fail a decision row and not
// only a behaviour row (the suite's standing rule): the elements it must name, and the spellings it
// must NOT name — a false here is an instruction the program never wrote, a true there is an effect lost.
func TestTheElementAsksToBeRunPredicateIsAskedDirectly(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		e    Expr
		want bool
	}{
		{"nothing at all", nil, false},
		{"an integer", &IntLit{Value: 3}, false},
		{"a double", &FloatLit{Value: 1.5}, false},
		{"a text", &StrLit{Value: "abc"}, false},
		{"a verdict", &BoolLit{Value: true}, false},
		{"the void", &NoneLit{}, false},
		{"a name, whose slot the checker proved was written", &Name{Value: "x"}, false},
		{"a list of integers", &ListLit{Elems: []Expr{&IntLit{Value: 1}, &IntLit{Value: 2}}}, false},
		{"a tuple of integers", &Tuple{Elems: []Expr{&IntLit{Value: 1}}}, false},
		{"a set of integers", &SetLit{Elems: []Expr{&IntLit{Value: 1}}}, false},
		{"a dict of integers", &DictLit{Keys: []Expr{&IntLit{Value: 1}}, Vals: []Expr{&IntLit{Value: 2}}}, false},
		{"a literal nested three deep, all spellings", &ListLit{Elems: []Expr{&ListLit{Elems: []Expr{&ListLit{Elems: []Expr{&IntLit{Value: 1}}}}}}}, false},
		{"a literal holding a call", &ListLit{Elems: []Expr{&Call{Fn: &Name{Value: "f"}}}}, true},
		{"a literal nested around a call", &ListLit{Elems: []Expr{&ListLit{Elems: []Expr{&ListLit{Elems: []Expr{&Call{Fn: &Name{Value: "f"}}}}}}}}, true},
		{"a dict whose VALUE calls", &DictLit{Keys: []Expr{&StrLit{Value: "a"}}, Vals: []Expr{&Call{Fn: &Name{Value: "f"}}}}, true},
		{"a dict whose KEY calls", &DictLit{Keys: []Expr{&Call{Fn: &Name{Value: "f"}}}, Vals: []Expr{&IntLit{Value: 1}}}, true},
		{"a tuple holding a read", &Tuple{Elems: []Expr{&Index{Obj: &ListLit{Elems: []Expr{&IntLit{Value: 1}}}, Idx: &IntLit{Value: 5}}}}, true},
		{"a call", &Call{Fn: &Name{Value: "f"}}, true},
		{"a read", &Index{Obj: &ListLit{Elems: []Expr{&IntLit{Value: 1}, &IntLit{Value: 2}}}, Idx: &IntLit{Value: 0}}, true},
		{"a division", &BinOp{Op: "//", L: &IntLit{Value: 1}, R: &IntLit{Value: 0}}, true},
		{"a comprehension", &Comp{Kind: CompList, Elems: []Expr{&Name{Value: "x"}}, ForVar: &Name{Value: "x"}, Iter: &ListLit{Elems: []Expr{&IntLit{Value: 1}}}}, true},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := literalElementAsksToBeRun(tt.e); got != tt.want {
				t.Fatalf("the predicate answered %v for %s, want %v", got, tt.name, tt.want)
			}
		})
	}
	// The whole-container question, asked the same way the door asks it before it touches a builder.
	if literalElementsAskToBeRun([]Expr{&IntLit{Value: 1}, &ListLit{Elems: []Expr{&StrLit{Value: "a"}}}}) {
		t.Fatal("a container of spellings asked to be run")
	}
	if !literalElementsAskToBeRun([]Expr{&IntLit{Value: 1}, &Call{Fn: &Name{Value: "print"}, Args: []Expr{&StrLit{Value: "x"}}}}) {
		t.Fatal("a container holding a call did not ask to be run")
	}
}

// What the count door still owes, pinned with today's answer beside the reference's rather than papered
// over — the residue of the same sweep, filed so the day the door grows, the row fails rather than
// disappears (**ADR 0440**'s row-hygiene rule, **Gap R.204**'s rule about claims that cannot be re-measured).
var countDoorFiledRows = []struct {
	name      string
	src       string
	kind      string // "assemble" — the module never becomes a program — or "refusal"
	names     string // the substring today's failure has to carry, so a new bug cannot inherit the row
	reference string // what CPython does with the same source
	row       string
}{
	{
		// The door loads the count field out of the FOLD's static global, and the fold did not answer
		// this one: the container is a heap object `%h1`, so the load names a function-local register as
		// a global and the assembler rejects the module. `pkg/lang/codegen_comp*.go` builds these and
		// `builtinLen`'s `*Comp` road still reads `g.compLen`/`g.compEls` — the fold's tables — for a
		// comprehension the run time built.
		"a comprehension the run time builds has its count read out of a global that is not there",
		"def g(v):\n    return v * 2\nprint(len([g(x) for x in [1, 2, 3]]))",
		"assemble", "invalid use of function-local name",
		"prints 3",
		"roadmap Gap R.496",
	},
	{
		// Found by the same sweep, and NOT this door's debt: this program has no `len` in it. A `def`
		// written inside a `try:` block has its whole `define` emitted into the enclosing block, so the
		// module is malformed wherever the block happened to end — which is why the helper every row
		// above binds sits before its `try:`. The road that owns it is the definition road, and the row
		// is pinned here only because this is where it was measured.
		"a def written inside a `try:` block emits its body into the enclosing block (the def road's row)",
		"try:\n    def f():\n        return 1\n    print(\"ok\")\nexcept ZeroDivisionError as e:\n    print(\"caught\", e)",
		"assemble", "expected instruction opcode",
		"prints ok",
		"roadmap Gap R.497",
	},
	{
		// The same shape without the `try:` — a `def` in an `if`, `for` or `while` — reaches the ordinary
		// call road, which does not know the name is a function the block defined. Again the def road's,
		// and again identical on the build before this commit and on it.
		"a def written inside an `if` block is called by a refusal the reference answers (the def road's row)",
		"if 1:\n    def f():\n        return 1\nprint(f())",
		"refusal", "is called but is not a function",
		"prints 1",
		"roadmap Gap R.498",
	},
	{
		// The residue of THIS door's own family that the count door cannot reach, and the reason it is a
		// row rather than part of the fix: the element is `[[KIND][0][0]]`, the fold answers the inner read,
		// and the OUTER read is claimed by the numeric-element road (`numericElemUse`), which hands back a
		// number for a slot holding a verdict, a double or the void — so no raise is emitted to be performed.
		// It is the SUBSCRIPT door's, not the count door's, which the shape written with no `len` in it proves:
		// `x = [[True][0][0]]` / `print("ok")` prints `ok` at exit 0 on the build before this commit and on it.
		"a literal whose element reads a verdict the numeric slot road answers with a number (the subscript door's row)",
		"print(len([[True][0][0]]))",
		"answer", "1",
		"raises TypeError: 'bool' object is not subscriptable",
		"roadmap Gap R.499",
	},
	{
		"a literal whose element reads the void, the same road (the subscript door's row)",
		"print(len([[None][0][0]]))",
		"answer", "1",
		"raises TypeError: 'NoneType' object is not subscriptable",
		"roadmap Gap R.499",
	},
	{
		"a literal whose element reads a double, the same road (the subscript door's row)",
		"print(len([[1.5][0][0]]))",
		"answer", "1",
		"raises TypeError: 'float' object is not subscriptable",
		"roadmap Gap R.499",
	},
	{
		"a literal holding that element beside a spelling, the same road (the subscript door's row)",
		"print(len([[[None][0][0]], 1]))",
		"answer", "2",
		"raises TypeError: 'NoneType' object is not subscriptable",
		"roadmap Gap R.499",
	},
}

func TestWhatTheCountDoorStillOwesIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tt := range countDoorFiledRows {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.kind == "answer" {
				// The toolchain ANSWERS this program at the exit code of success where the reference raises.
				// The record cannot hold it (the recorder certifies agreement, not greenness), so the row is
				// pinned here with today's bytes beside the reference's.
				out, refused := compiledOutOrRefusal(t, tt.src)
				if refused {
					t.Fatalf("%q was refused, where this row pins the answer %q (%s): %s", tt.src, tt.names, tt.row, out)
				}
				if got := strings.TrimRight(out, "\n"); got != tt.names {
					t.Fatalf("%q printed %q, not the %q this row pins; the reference %s (%s)", tt.src, got, tt.names, tt.reference, tt.row)
				}
				if HasGoldenAnswer(tt.src) {
					t.Fatalf("%q is on the record, although the compiled leg prints %q where the reference %s (%s)", tt.src, tt.names, tt.reference, tt.row)
				}
				return
			}
			res, err := JIT(tt.src, 0)
			if err == nil && res.Code == 0 {
				t.Fatalf("%q answered %q at exit 0, where this row is filed because the toolchain fails first (%s)", tt.src, res.Output, tt.row)
			}
			fail := ""
			if err != nil {
				fail = err.Error()
			} else {
				fail = res.Stderr
			}
			if tt.kind == "assemble" {
				if err == nil && res.Code != 2 {
					t.Fatalf("%q exited %d, where this row is filed because the module is rejected (exit 2 — ADR 0166's code) (%s):\n%s", tt.src, res.Code, tt.row, fail)
				}
				if !strings.Contains(fail, tt.names) && !strings.Contains(fail, "llc") {
					t.Fatalf("%q failed the assembly without naming %q (%s):\n%s", tt.src, tt.names, tt.row, fail)
				}
			} else if !strings.Contains(fail, tt.names) {
				t.Fatalf("%q refused without the sentence this row pins (%q) (%s):\n%s", tt.src, tt.names, tt.row, fail)
			}
			// The record cannot hold a program the compiled leg never runs, and an entry is the answer the
			// reference gives ONLY where both legs agree — so these rows live here and nowhere else.
			if HasGoldenAnswer(tt.src) {
				t.Fatalf("%q is on the record with an answer, although the compiled leg never becomes a program (%s)", tt.src, tt.row)
			}
		})
	}
}

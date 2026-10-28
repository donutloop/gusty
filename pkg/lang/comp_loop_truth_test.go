package lang

import (
	"strings"
	"testing"
)

// A comprehension standing where the LOOP, the CONDITION or `bool()` asks for a container is walked,
// tested and measured as the OBJECT it is (roadmap **Gap R.490** + **Gap R.500**, **ADR 0443**).
//
// There were two comprehensions and one door. `containerHandleOf` already knew how to make the object —
// the heap register a runtime builder returned, or the fold's global materialised into the heap by
// `containerOperand` (**ADR 0188**) — but it answered `""` for the KIND, because for a slot read only the
// tag knows. A comprehension is not a slot read: the program wrote `[`, `{a}` or `{a: b}`. Asked nothing,
// the loop arm took the handle, left `iterKind` empty, and the walk below was skipped for want of a kind —
// so the loop fell through to the numeric counter road, which for a FOLDED comprehension compared the
// counter against a container global
//
//	%t3 = icmp slt i32 %_ctr1.ld1, @.lst2      llc-20: global variable reference must have pointer type → exit 2
//
// (**ADR 0166**'s own code, **Gap R.490**) and for a BUILT one succeeded, bound the loop variable to the
// counter and printed `0` for CPython's `2`, `4`, `6` at the exit code of success (**Gap R.500**). The
// condition and the truth doors had no arm at all: `if [x for x in [1]]:` died the same way at exit **2**,
// and `print(bool([g(x) for x in [1, 2, 3]]))` probed the handle and printed `False` for `True`.
//
// The fix is one question asked of the brace, in one predicate (`compContainerKind`,
// `pkg/lang/for_iterable.go`) shared by the three doors — **ADR 0439**'s rule for an element's tag and
// **ADR 0442**'s rule for the count, arriving at the doors that DECIDE rather than measure. A loop's
// element tag, however, is a run-time fact and still is not carried for a comprehension the run time
// builds over a mixed iterable: that stays **Gap R.502**, pinned in the filed table below, and it is why
// every answered loop row here walks an iterable of ONE kind.

// compLoopCompFn is the element the built-comprehension rows are written with: a call the folder cannot
// see, so the comprehension takes the runtime builder road and NOT the fold.
const compLoopCompFn = "def g(v):\n    return v * 2\n"

// compIterableLoops are the loops the reference walks, on both roads of the comprehension — the FOLD's
// and the BUILDER's — and for all three braces.
var compIterableLoops = []struct {
	name string
	src  string
	want string
}{
	{"the row as filed (folded): the loop that died at exit 2", "for v in [x for x in [1]]:\n    print(v)", "1"},
	{"the row as filed (built): the loop that printed the counter", compLoopCompFn + "for v in [g(x) for x in [1, 2, 3]]:\n    print(v)", "2\n4\n6"},
	{"a built comprehension over a container variable", "xs = [1, 2, 3]\n" + compLoopCompFn + "for v in [g(x) for x in xs]:\n    print(v)", "2\n4\n6"},
	{"a built comprehension with a filter the folder cannot see", compLoopCompFn + "for v in [g(x) for x in [1, 2, 3] if g(x) > 2]:\n    print(v)", "4\n6"},
	{"a built comprehension over range()", compLoopCompFn + "for v in [g(x) for x in range(3)]:\n    print(v)", "0\n2\n4"},
	{"a SET comprehension walks its members", "for v in {x for x in [3, 3, 4]}:\n    print(v)", "3\n4"},
	{"a built SET comprehension walks its members (in the order the reference yields them)", compLoopCompFn + "for v in {g(x) for x in [2, 2, 4]}:\n    print(v)", "8\n4"},
	{"a DICT comprehension walks its KEYS", "for k in {x: 1 for x in [\"a\", \"b\"]}:\n    print(k)", "a\nb"},
	{"a nested comprehension", compLoopCompFn + "for v in [[g(y) for y in [1, 2]] for x in [1, 2]]:\n    print(v)", "[2, 4]\n[2, 4]"},
	{"break leaves the loop", "for v in [x for x in [1, 2, 3]]:\n    if v == 2:\n        break\n    print(v)", "1"},
	{"continue advances", "for v in [x for x in [1, 2, 3]]:\n    if v == 2:\n        continue\n    print(v)", "1\n3"},
	{"the else clause runs when the loop ends", "for v in [x for x in [1, 2]]:\n    print(v)\nelse:\n    print(\"else ran\")", "1\n2\nelse ran"},
	{"an empty comprehension walks nothing and the else clause still runs", "for v in [x for x in []]:\n    print(v)\nelse:\n    print(\"else ran\")", "else ran"},
	{"a comprehension loop followed by a plain one", "for v in [x for x in [9]]:\n    print(v)\nfor v in [4, 5]:\n    print(v)", "9\n4\n5"},
	{"a comprehension loop whose body breaks after the first item", "for v in [x for x in [1, 2]]:\n    print(\"once\")\n    break", "once"},
}

// TestTheLoopWalksAComprehensionStandingWhereItsIterableIs is the answering half of the loop road, on the
// compiled leg (`compiledOut` runs `llc`/`lli`, so the rejected module cannot pass) and the record leg
// (asked with NO tolerance: these sources are on the record).
func TestTheLoopWalksAComprehensionStandingWhereItsIterableIs(t *testing.T) {
	t.Parallel()
	for _, tt := range compIterableLoops {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimRight(compiledOut(t, tt.src), "\n"); got != tt.want {
				t.Fatalf("printed %q, the reference prints %q", got, tt.want)
			}
			rec, err := runGoldenStdout(t, tt.src)
			if err != nil {
				t.Fatalf("the record does not answer a program both legs agree on: %v", err)
			}
			if strings.TrimRight(rec, "\n") != tt.want {
				t.Fatalf("the record says %q and the compiled leg says %q for the same source", rec, tt.want)
			}
		})
	}
}

// compIterableTruths are the condition and `bool()` doors: the same object, asked whether it holds
// anything — the reference's answer, at the exit code of a program that finished.
var compIterableTruths = []struct {
	name string
	src  string
	want string
}{
	{"the row as filed (folded): the condition that died at exit 2", "if [x for x in [1]]:\n    print(\"yes\")\nelse:\n    print(\"no\")", "yes"},
	{"the row as filed (built): the condition that took the else arm", compLoopCompFn + "if [g(x) for x in [1, 2, 3]]:\n    print(\"yes\")\nelse:\n    print(\"no\")", "yes"},
	{"an empty comprehension is false", "if [x for x in []]:\n    print(\"yes\")\nelse:\n    print(\"no\")", "no"},
	{"an empty built comprehension is false", "def g(v):\n    return v\nif [g(x) for x in []]:\n    print(\"yes\")\nelse:\n    print(\"no\")", "no"},
	{"the row as filed (folded): bool of a comprehension died at exit 2", "print(bool([x for x in [1]]))", "True"},
	{"the row as filed (built): bool of a comprehension was False", compLoopCompFn + "print(bool([g(x) for x in [1, 2, 3]]))", "True"},
	{"bool of an empty comprehension", "print(bool([x for x in []]))", "False"},
	{"bool of a set comprehension that COLLAPSES to one member", "def g(v):\n    return v\nprint(bool({g(x) for x in [1, 1]}))", "True"},
	{"bool of an empty dict comprehension", "print(bool({x: 1 for x in []}))", "False"},
	{"bool of a dict comprehension with an entry", "def g(v):\n    return v\nprint(bool({g(x): 1 for x in [7]}))", "True"},
	{"not of a comprehension", "print(not [x for x in [1]])", "False"},
	{"and over two comprehensions", "if [x for x in [1]] and [x for x in []]:\n    print(\"both\")\nelse:\n    print(\"not both\")", "not both"},
	{"or over two comprehensions", "if [x for x in []] or [x for x in [2]]:\n    print(\"one\")\nelse:\n    print(\"none\")", "one"},
}

// TestTheConditionAndBoolAskAComprehensionWhetherItIsEmpty is the same object at the two doors beside the
// loop, both of which used to test the WORD the comprehension arrived in — a global in an `icmp` slot for
// the fold (exit 2) and an allocation index for the builder (always true).
func TestTheConditionAndBoolAskAComprehensionWhetherItIsEmpty(t *testing.T) {
	t.Parallel()
	for _, tt := range compIterableTruths {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimRight(compiledOut(t, tt.src), "\n"); got != tt.want {
				t.Fatalf("printed %q, the reference prints %q", got, tt.want)
			}
			rec, err := runGoldenStdout(t, tt.src)
			if err != nil {
				t.Fatalf("the record does not answer a program both legs agree on: %v", err)
			}
			if strings.TrimRight(rec, "\n") != tt.want {
				t.Fatalf("the record says %q and the compiled leg says %q for the same source", rec, tt.want)
			}
		})
	}
}

// The instruction shape is the claim the two exit-2 rows were made of. A loop over a comprehension walks
// a HANDLE through the runtime length helper, and no module the suite compiles compares a loop counter
// with a container global again.
func TestNoModuleComparesALoopCounterWithAComprehensionGlobal(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		src      string
		mustHave []string
	}{
		{"the fold's comprehension walks a materialised handle", "for v in [x for x in [1]]:\n    print(v)",
			[]string{"call i32 @rt_list_len(", "call i32 @rt_get_elem("}},
		{"the builder's comprehension walks the handle it made", compLoopCompFn + "for v in [g(x) for x in [1, 2, 3]]:\n    print(v)",
			[]string{"call i32 @rt_list_len(", "call i32 @rt_get_elem("}},
		{"a set comprehension asks the set its own question", "for v in {x for x in [3, 3, 4]}:\n    print(v)",
			[]string{"call i32 @rt_set_len("}},
		{"a dict comprehension asks the dict, and walks the keys at the stride", "for k in {x: 1 for x in [\"a\"]}:\n    print(k)",
			[]string{"call i32 @rt_dict_len("}},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ir := mustCompileIR(t, tt.src)
			for _, want := range tt.mustHave {
				if !strings.Contains(ir, want) {
					t.Fatalf("the module does not walk the object (%q is missing):\n%s", want, ir)
				}
			}
			// The instruction `llc` rejected: the counter compared against a container's GLOBAL.
			if strings.Contains(ir, "icmp slt i32 %_ctr") {
				for _, line := range strings.Split(ir, "\n") {
					if strings.Contains(line, "icmp slt i32 %_ctr") && strings.Contains(line, "@.") {
						t.Fatalf("a loop compares its counter with a container global (ADR 0188, ADR 0166's exit 2):\n%s", line)
					}
				}
			}
		})
	}
	// The truth door asks the object how many slots it holds — the same helper **ADR 0442**'s count door
	// asks — instead of probing the word the comprehension arrived in.
	t.Run("the condition asks rt_heap_len of the comprehension", func(t *testing.T) {
		t.Parallel()
		ir := mustCompileIR(t, "if [x for x in [1]]:\n    print(\"yes\")")
		if !strings.Contains(ir, "call i32 @rt_heap_len(") {
			t.Fatalf("the condition never asked the object how many slots it holds:\n%s", ir)
		}
	})
}

// What the three doors still cannot reach, pinned with today's bytes beside the reference's.
//
// The three rows this table carried when **Gap R.502** was open — the loop item of a built comprehension
// over a mixed iterable, the same comprehension printed whole, and a built dict comprehension's KEY —
// ANSWER now, and moved to `forwarded_answer_test.go`'s answering tables with **ADR 0445**: a slot written
// by a builder that guessed the kind is exactly what that door stopped guessing.
var compIterableFiledRows = []struct {
	name      string
	src       string
	kind      string
	names     string
	reference string
	row       string
}{
	{
		// The loop ITEM now travels as a (payload, tag) pair, and handing it to a call is a position that
		// keeps one word — so the door refuses naming L11.1 instead of answering. On the build before this
		// commit the same program was **ADR 0166**'s exit **2** (`%_ctr1.ld1, @.lst2`), which is why the
		// refusal is an improvement and not a regression, and why the row is still owed.
		"a comprehension loop item handed to a call is refused for want of the one-word position's tag",
		"def show(v):\n    print(v)\nfor v in [x * 2 for x in [1, 2]]:\n    show(v)",
		"refusal", "the element a loop stepped over",
		"prints 2, 4",
		"roadmap L11.1 · Gap R.146 (a tag with nowhere to go)",
	},
}

func TestWhatTheComprehensionLoopStillOwesIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tt := range compIterableFiledRows {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, tt.src)
			if tt.kind == "refusal" {
				if !refused {
					t.Fatalf("%q answered %q, where this row is filed because the door refuses naming %q (%s)", tt.src, out, tt.names, tt.row)
				}
				if !strings.Contains(out, tt.names) {
					t.Fatalf("%q refused without the sentence this row pins (%q) (%s):\n%s", tt.src, tt.names, tt.row, out)
				}
				return
			}
			if refused {
				t.Fatalf("%q was refused, where this row pins the answer %q (%s): %s", tt.src, tt.names, tt.row, out)
			}
			if got := strings.TrimRight(out, "\n"); got != tt.names {
				t.Fatalf("%q printed %q, not the %q this row pins; the reference %s (%s)", tt.src, got, tt.names, tt.reference, tt.row)
			}
			// The legs disagree, so the recorder will not certify it: the row lives here and nowhere else.
			if HasGoldenAnswer(tt.src) {
				t.Fatalf("%q is on the record, although the compiled leg prints %q where the reference %s (%s)", tt.src, tt.names, tt.reference, tt.row)
			}
		})
	}
}

// compBuildSurvivalPrograms are the programs whose elements CALL, and therefore collect, WHILE the
// comprehension that holds them is still being filled.
var compBuildSurvivalPrograms = []struct {
	name string
	src  string
	want string
}{
	{
		"the filed loop: three collections inside the build, and the loop still walks 2, 4, 6",
		"def g(v):\n    y = len([v * a for a in [2, 3]])\n    return v * 2\n\nfor v in [g(x) for x in [1, 2, 3]]:\n    print(v)",
		"2\n4\n6",
	},
	{
		"a loop whose element collects nine times before it answers",
		"def g(v):\n    total = 0\n    for a in [v, v + 1, v + 2]:\n        total = total + len([a * b for b in [2, 3]])\n    return v * 2\n\nfor v in [g(x) for x in [1, 2, 3]]:\n    print(v)",
		"2\n4\n6",
	},
	{
		"the same build asked three ways — `bool()`, `len`, the loop — answers three times correctly",
		"def g(v):\n    total = 0\n    for a in [v, v + 1, v + 2]:\n        total = total + len([a * b for b in [2, 3]])\n    return v * 2\n\nprint(bool([g(x) for x in [1, 2, 3]]))\nprint(len([g(x) for x in [1, 2, 3]]))\nfor v in [g(x) for x in [1, 2, 3]]:\n    print(v)",
		"True\n3\n2\n4\n6",
	},
	{
		"a set comprehension whose members collect collapses and walks in the reference's order",
		"def g(v):\n    y = len([v * a for a in [2, 3]])\n    return v\n\nfor v in {g(x) for x in [1, 1, 2]}:\n    print(v)",
		"1\n2",
	},
	{
		"a dict comprehension whose keys and values collect walks its keys",
		"def g(v):\n    y = len([v * a for a in [2, 3]])\n    return v * 2\n\nfor k in {x: g(x) for x in [1, 2]}:\n    print(k)",
		"1\n2",
	},
}

// TestABuiltComprehensionSurvivesTheCollectionItsOwnElementsTrigger is the second pillar of the commit.
// A comprehension the run time builds is ALLOCATED, parked in a bare register, and then filled by a road
// that is free to CALL — and a call's body collects (**ADR 0347**, **ADR 0317**). The half-filled object is
// live and reachable from no root, which is the one shape **ADR 0181** forbids; **ADR 0347** rooted the
// container a LOOP walks, and this is the same rule one construct earlier, at the build itself.
//
// The attribution is by control build, because a GC window cannot be read off a module: with the root taken
// back out of the four builders and nothing else changed, the loop of the first row prints `4`, `6`, `4`,
// `6` and the loop of the second prints `10`, `15`, `6` — both at **exit 0**, both programs the reference
// answers `2`, `4`, `6`. An unrooted object does not fail; it answers a DIFFERENT program, which is the
// class **ADR 0166** files loudest. With the root, every row below is the reference's bytes on both legs.
func TestABuiltComprehensionSurvivesTheCollectionItsOwnElementsTrigger(t *testing.T) {
	t.Parallel()
	for _, tt := range compBuildSurvivalPrograms {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimRight(compiledOut(t, tt.src), "\n"); got != tt.want {
				t.Fatalf("printed %q, the reference prints %q — a comprehension whose elements call must be "+
					"rooted for its own build (ADR 0181, ADR 0347, ADR 0443)", got, tt.want)
			}
			rec, err := runGoldenStdout(t, tt.src)
			if err != nil {
				t.Fatalf("the record does not answer a program both legs agree on: %v", err)
			}
			if strings.TrimRight(rec, "\n") != tt.want {
				t.Fatalf("the record says %q and the compiled leg says %q for the same source", rec, tt.want)
			}
		})
	}
	// The instruction shape: the object the builder allocated is stored into a frame slot and that slot is
	// pushed as a root BEFORE the element road runs — `rt_root_put` on a `%_cb` slot, the builders' own name.
	t.Run("the module parks the half-built object in a root slot", func(t *testing.T) {
		t.Parallel()
		ir := mustCompileIR(t, compBuildSurvivalPrograms[1].src)
		if !strings.Contains(ir, "call void @rt_root_put(i32* %_cb") {
			t.Fatalf("the built comprehension was never rooted for its build:\n%s", ir)
		}
		alloc := strings.Index(ir, "= call i32 @rt_alloc(")
		root := strings.Index(ir, "call void @rt_root_put(i32* %_cb")
		elem := strings.Index(ir, "call i32 @gy_g(")
		if alloc < 0 || root < 0 || elem < 0 {
			t.Fatalf("the module does not carry the alloc/root/call shape this row is about:\n%s", ir)
		}
		if !(alloc < root && root < elem) {
			t.Fatalf("the object is rooted after (or beside) the call that can collect it rather than before it")
		}
	})
	// …and the window is CLOSED when the build ends, so a comprehension nothing consumed is still
	// reclaimable: the last slot of the build is cleared (**ADR 0181**'s clearing rule, applied to a build).
	t.Run("the module clears the root when the build ends", func(t *testing.T) {
		t.Parallel()
		ir := mustCompileIR(t, compBuildSurvivalPrograms[1].src)
		if !strings.Contains(ir, "call void @rt_root_clear(i32* %_cb") {
			t.Fatalf("the build's root was never closed — a comprehension would stay live for the whole frame:\n%s", ir)
		}
	})
}

// The predicate is asked directly, because a deciding door that grows by accident must fail a decision
// row and not only a behaviour row (the suite's standing rule).
func TestTheComprehensionBraceAnswersTheKindQuestionDirectly(t *testing.T) {
	t.Parallel()
	list := &Comp{Kind: CompList, Elems: []Expr{&Name{Value: "x"}}, ForVar: &Name{Value: "x"}, Iter: &ListLit{Elems: []Expr{&IntLit{Value: 1}}}}
	set := &Comp{Kind: CompSet, Elems: []Expr{&Name{Value: "x"}}, ForVar: &Name{Value: "x"}, Iter: &ListLit{Elems: []Expr{&IntLit{Value: 1}}}}
	dict := &Comp{Kind: CompDict, Keys: []Expr{&Name{Value: "x"}}, Vals: []Expr{&IntLit{Value: 1}}, ForVar: &Name{Value: "x"}, Iter: &ListLit{Elems: []Expr{&IntLit{Value: 1}}}}
	for _, tt := range []struct {
		name string
		e    Expr
		want string
		ok   bool
	}{
		{"a list comprehension", list, "list", true},
		{"a set comprehension", set, "set", true},
		{"a dict comprehension", dict, "dict", true},
		{"a list literal is not a comprehension", &ListLit{Elems: []Expr{&IntLit{Value: 1}}}, "", false},
		{"a name is not a comprehension", &Name{Value: "xs"}, "", false},
		{"nothing at all is not a comprehension", nil, "", false},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := compContainerKind(tt.e)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("the brace answered (%q, %v), want (%q, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

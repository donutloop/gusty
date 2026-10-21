package lang

import (
	"strings"
	"testing"
)

// A list literal is read by the door a list variable's element is read by (roadmap Gap R.218, ADR 0331).
//
//     print(["a", "b"][0])   # a   — the program printed 0   (an index into @str_tab)
//     print([1.5, 2][0])     # 1.5 — the program printed 1   (a @float_box handle)
//     print([None, 2][0])    # None — the program printed 0
//     print([True, 2][0])    # True — the program printed 1
//
// All four answered a payload at exit 0 while the same element read off a *variable* answered correctly,
// because `runtimeSlotPairDeep` had a `*DictLit` arm from ADR 0327 and no `*ListLit` arm beside it. The
// rows below are the four answers, the fold that keeps what it genuinely reads, the residue the one-word
// positions still refuse, and the parity with the variable road that made this a one-line asymmetry rather
// than a missing feature.

func TestAListLiteralAnswersTheElementThePositionNames(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		src  string
		want string
	}{
		{`print(["a", "b"][0])`, "a"},
		{`print(["a", "b"][1])`, "b"},
		{`print([1.5, 2][0])`, "1.5"},
		{`print([1.5, 2][1])`, "2"},
		{`print([None, 2][0])`, "None"},
		{`print([True, 2][0])`, "True"},
		{`print(["a", 1.5, None, True][2])`, "None"},
		{`print(["a", 1.5, None, True][3])`, "True"},
		{`print(["a", "b"][-1])`, "b"},
		{`print([1, 2, 3][0])`, "1"},
		{`print([1, 2, 3][-1])`, "3"},
		{`print([[1, 2], [3, 4]][0])`, "[1, 2]"},
		{`print([[1, 2], [3, 4]][0][1])`, "2"},
		{`print([{"a": 1}][0])`, "{'a': 1}"},
		{`print([{"a": 1}][0]["a"])`, "1"},
		{`print(str(["a", 1.5][0]))`, "a"},
		{`print(repr(["a", 1.5][0]))`, "'a'"},
		{`print(str([None, "z"][0]))`, "None"},
		{`print(repr([None, "z"][1]))`, "'z'"},
		{`print(len(["a", "b", "c"]))`, "3"},
		{`print("a" in ["a", "b"])`, "True"},
		{`print(1 in [1, 2])`, "True"},
		{`print([1, 2, 3][1] + 10)`, "12"},
		{`first = ["a", "b"][0]` + "\n" + `print(first)`, "a"},
		{`mixed = [1.5, None, "z"]` + "\n" + `print(mixed[1])`, "None"},
		{"for v in [None, 1.5, \"z\"]:\n    print(v)", "None\n1.5\nz"},
		{`print([True, False][False])`, "True"},
	} {
		t.Run(tt.src, func(t *testing.T) {
			t.Parallel()
			if out := strings.TrimRight(compiledOut(t, tt.src), "\n"); out != tt.want {
				t.Fatalf("compiled %q, want %q", out, tt.want)
			}
		})
	}
}

// The fold keeps the shape it genuinely reads, and the door declines it rather than allocating an object
// the compiler already knew the answer to. `print([1, 2][0])` has always emitted a constant; the assertion
// is that the widened door did not take that away.
func TestTheListFoldStillAnswersWhatItCanRead(t *testing.T) {
	t.Parallel()
	ir := emittedModuleIR(t, `print([1, 2][0])`)
	if strings.Contains(ir, "rt_print_mixed_value") {
		t.Fatal("the all-integer literal went to the pair door; the fold owns this shape and emits no allocation")
	}
	for _, src := range []string{`print([1, 2][0])`, `print([1, 2, 3][2])`, `print([1, 2, 3][-1])`} {
		if out := strings.TrimRight(compiledOut(t, src), "\n"); out == "" {
			t.Fatalf("%q printed nothing", src)
		}
	}
}

// The residue, kept honest. A position that keeps one word for a whole value cannot carry the tag the
// element's kind lives in, and the dict road refused the same shape when ADR 0327 landed — so the list road
// refuses it too, naming the missing half, instead of printing a handle at exit 0.
//
// The argument position left this list with roadmap Gap R.220: `id(["a","b"][0])` is asserted as an answer
// by `TestACallArgumentCarriesAListLiteralElementItsOwnKind` below.
func TestTheOneWordPositionsRefuseTheListLiteralRead(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"def f():\n    return [\"a\",\"b\"][0]\nprint(f())",
		`print([1.5, 2][0] + 1)`,
		`print(["a"] [0] + "b")`,
		// (`print(f"{[None, 1.5][1]}")` sat here, on the belief that an f-string field keeps one word for a
		// whole value. It does not: the field is rendered by the module's one tag-reading printer, and ADR
		// 0356 gave the field the pair door print already asked — the field now prints `1.5`, which is what
		// CPython prints, and it is asserted as an answer in `pkg/lang/fstring_pair_read_test.go`.)
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, src)
			if !refused {
				t.Fatalf("%q answered %q; a one-word position cannot carry the element's kind, and printing the payload is the wrong answer this row had", src, out)
			}
			if !strings.Contains(out, "Gap R.218") || !strings.Contains(out, "kind") {
				t.Fatalf("%q refused without naming the missing half and its row: %s", src, out)
			}
		})
	}
}

// TestACallArgumentCarriesAListLiteralElementItsOwnKind is the paid half of the row above: an element read
// out of a list the program spelled out, handed to a call, arrives with its kind beside it. The answer is
// the same sentence the record and the reference print for the shape, and the int element keeps taking the
// one-word road it always took — the argument's pair is asked of the entry, never of the key.
func TestACallArgumentCarriesAListLiteralElementItsOwnKind(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{name: "text", src: "def id(v):\n    return v\nprint(id([\"a\",\"b\"][0]))\n", want: "a\n"},
		{name: "float", src: "def id(v):\n    return v\nprint(id([1,2.5][1]))\n", want: "2.5\n"},
		{name: "int_keeps_one_word", src: "def twice(v):\n    return v + v\nprint(twice([3,4][0]))\n", want: "6\n"},
		{name: "none", src: "def id(v):\n    return v\nprint(id([None,1][0]))\n", want: "None\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, tc.src)
			if refused {
				t.Fatalf("the paid row refused: %s", out)
			}
			if out != tc.want {
				t.Fatalf("answered %q, want %q", out, tc.want)
			}
		})
	}
}

// The condition is the position that asks only "is it true", and truth is the element's kind's fact, not its
// payload's. Before this row the fold answered `if [True][0]:` by testing the payload — right for a bool and
// wrong for the shapes whose payload means nothing on its own, which is why the retired engine's own record
// of `if [True][0]: print("yes")` had to stay runnable rather than become a new divergence.
func TestAListLiteralElementDecidesAConditionTheWayTheReferenceDecides(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		src  string
		want string
	}{
		{`if [True][0]:` + "\n" + `    print("yes")`, "yes"},
		{`if [False][0]:` + "\n" + `    print("yes")`, ""},
		{`if [None][0]:` + "\n" + `    print("yes")`, ""},
		{`if [""][0]:` + "\n" + `    print("yes")`, ""},
		{`if ["a"][0]:` + "\n" + `    print("yes")`, "yes"},
		// The three the payload cannot answer at all: a float's box handle, an empty container's handle and
		// a non-empty container's handle are all non-zero words, and only the tag says which is false.
		{`if [0.0, 1][0]:` + "\n" + `    print("yes")`, ""},
		{`if [[]][0]:` + "\n" + `    print("yes")`, ""},
		{`if [[1]][0]:` + "\n" + `    print("yes")`, "yes"},
		{`if [{"a": {}}][0]:` + "\n" + `    print("yes")`, "yes"},
		{`if [{"a": {}}][0] and 1:` + "\n" + `    print("yes")`, "yes"},
		{`if [None][0] or "x":` + "\n" + `    print("yes")`, "yes"},
		{`print(1 if [None, "a"][0] else 0)`, "0"},
		{`print(1 if ["z", None][0] else 0)`, "1"},
		{`while [0][0]:` + "\n" + `    print("loop")`, ""},
		// A position the fold reads outright keeps the fold's answer.
		{`if [1, 2][0]:` + "\n" + `    print("yes")`, "yes"},
		{`if [0, 1][0]:` + "\n" + `    print("yes")`, ""},
		{`if [1, 2][1]:` + "\n" + `    print("yes")`, "yes"},
		// Parity with the same element read off a variable.
		{`xs = [0.0, 1]` + "\n" + `if xs[0]:` + "\n" + `    print("yes")`, ""},
		{`xs = ["", "a"]` + "\n" + `if xs[0]:` + "\n" + `    print("yes")`, ""},
	} {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			if out := strings.TrimRight(compiledOut(t, tt.src), "\n"); out != tt.want {
				t.Fatalf("compiled %q, printed %q, want %q", tt.src, out, tt.want)
			}
		})
	}
}

// The gate ADR 0329 planted at the subscript has to stand at BOTH halves of the read. The pair door builds
// the list and hands the position to `normalizeIndex`; a text index is an index into `@str_tab`, so a door
// that asked only "is it in range" printed whichever slot the text counted to, and a list index reached the
// bounds check as a `@.lstN` global — the `llc` rejection Gap R.222 measured. The dict half of the same door
// has always asked its own version of the question (`checkKeyReadTagged`); now the list half does too.
func TestAnIndexThatIsNotAPositionIsRefusedAtTheLiteralDoor(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		src     string
		class   string
		message string
	}{
		{`print([1, 2]["a"])`, "TypeError", "list indices must be integers or slices, not str"},
		{`print(["a", "b"]["a"])`, "TypeError", "list indices must be integers or slices, not str"},
		{`print(["a", "b"][None])`, "TypeError", "list indices must be integers or slices, not NoneType"},
		{`print(["a", "b"][1.5])`, "TypeError", "list indices must be integers or slices, not float"},
		{`print(["a", "b"][[0]])`, "TypeError", "list indices must be integers or slices, not list"},
		{`print(["a", "b"][(0, 1)])`, "TypeError", "list indices must be integers or slices, not tuple"},
		{`print({1: ["a", "b"]}[1]["a"])`, "TypeError", "list indices must be integers or slices, not str"},
		{`print([1, 2][0])`, "", ""},
		{`print(["a", "b"][True])`, "", ""},
	} {
		t.Run(tt.src, func(t *testing.T) {
			t.Parallel()
			if tt.class == "" {
				// A bool is an int, and a legal position must not be accused along with the illegal ones.
				if out, refused := compiledOutOrRefusal(t, tt.src); refused {
					t.Fatalf("a legal position was refused: %s", out)
				}
				return
			}
			class, message := jitTrap(t, tt.src)
			if class != tt.class || message != tt.message {
				t.Fatalf("raised %s: %s, want %s: %s", class, message, tt.class, tt.message)
			}
		})
	}
	// The module the refused read leaves behind still verifies: the raise ends the block and the door
	// never lowers an index that has no word to lower to.
	for _, src := range []string{`print(["a", "b"][[0]])`, `print(["a", "b"]["a"])`} {
		ir := emittedModuleIR(t, src)
		if !strings.Contains(ir, "TypeError") {
			t.Fatalf("%q emitted no TypeError raise:\n%s", src, ir)
		}
	}
}

// The asymmetry that made this a defect rather than a missing feature: every row the variable road answers,
// the literal road must answer too. A future change that breaks the literal road while leaving the variable
// road green fails here.
func TestTheLiteralListRoadIsNeverWorseThanTheVariableRoad(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ literal, variable string }{
		{`print(["a", "b"][0])`, "xs = [\"a\", \"b\"]\nprint(xs[0])"},
		{`print([1.5, 2][0])`, "xs = [1.5, 2]\nprint(xs[0])"},
		{`print([None, 2][0])`, "xs = [None, 2]\nprint(xs[0])"},
		{`print([True, 2][0])`, "xs = [True, 2]\nprint(xs[0])"},
		{`print([[1, 2]][0])`, "xs = [[1, 2]]\nprint(xs[0])"},
		{`print(str(["a"][0]))`, "xs = [\"a\"]\nprint(str(xs[0]))"},
		{`print(len(["a", "b"]))`, "xs = [\"a\", \"b\"]\nprint(len(xs))"},
		{`print(1 in [1, 2])`, "xs = [1, 2]\nprint(1 in xs)"},
	} {
		t.Run(tt.literal, func(t *testing.T) {
			t.Parallel()
			litOut, litRefused := compiledOutOrRefusal(t, tt.literal)
			varOut, varRefused := compiledOutOrRefusal(t, tt.variable)
			if varRefused {
				t.Skipf("the variable road itself refuses: %s", varOut)
			}
			if litRefused {
				t.Fatalf("the variable road answered %q and the literal road refused: %s", varOut, litOut)
			}
			if litOut != varOut {
				t.Fatalf("the literal road answered %q where the variable road answered %q", litOut, varOut)
			}
		})
	}
}

// A position past the end is the reference's IndexError, catchable — the door the widened read reaches
// through, and the sentence ADR 0330 pinned.
func TestAPositionPastTheEndOfAListLiteralRaisesWhatTheReferenceRaises(t *testing.T) {
	t.Parallel()
	class, message := jitTrap(t, `print([1, 2][5])`)
	if class != "IndexError" || message != "list index out of range" {
		t.Fatalf("raised %s: %s, want IndexError: list index out of range", class, message)
	}
	if out := strings.TrimRight(compiledOut(t, "try:\n    print([1, 2][5])\nexcept IndexError:\n    print(\"caught\")"), "\n"); out != "caught" {
		t.Fatalf("the trap is not catchable: %q", out)
	}
}

// The classifier and the door's own decisions, asked away from codegen.
func TestTheListFoldGateAnswersTheShapeItCanRead(t *testing.T) {
	t.Parallel()
	g := &irGen{}
	ints := &ListLit{Elems: []Expr{&IntLit{Value: 1}, &IntLit{Value: 2}}}
	strs := &ListLit{Elems: []Expr{&StrLit{Value: "a"}, &StrLit{Value: "b"}}}
	for _, tt := range []struct {
		ll   *ListLit
		idx  Expr
		fold bool
		int  bool
	}{
		{ints, &IntLit{Value: 0}, true, true},
		{ints, &IntLit{Value: 7}, false, false}, // a position the literal does not have is not a fold answer either
		{ints, &BoolLit{Value: true}, false, false},
		{strs, &IntLit{Value: 0}, false, false},
		{&ListLit{}, &IntLit{Value: 0}, false, false},
		{&ListLit{Elems: []Expr{&IntLit{Value: 1}, &StrLit{Value: "a"}}}, &IntLit{Value: 0}, false, true},
	} {
		if got := g.listLiteralFoldAnswers(tt.ll, tt.idx); got != tt.fold {
			t.Fatalf("listLiteralFoldAnswers(%v, %v) = %v, want %v", tt.ll.Elems, tt.idx, got, tt.fold)
		}
		if got := g.listLiteralEntryIsPlainInt(tt.ll, tt.idx); got != tt.int {
			t.Fatalf("listLiteralEntryIsPlainInt = %v, want %v", got, tt.int)
		}
	}
	if _, err := listLiteralElems(strs); err == nil {
		t.Fatal("the fold's view of a list literal must refuse a text element, not return an index and call it a value")
	}
}

// The record leg: the retired engine's answer for every answered row, so a later cycle cannot delete the
// gate without deleting the evidence.
func TestTheListLiteralRowsAreRecorded(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`print(["a", "b"][0])`, `print([1.5, 2][0])`, `print([None, 2][0])`, `print([True, 2][0])`,
		`print(["a", "b"][-1])`, `print([1, 2, 3][0])`, `print([1, 2][5])`,
	} {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			rec := goldenLookup(t, src)
			if rec.Repr == "" && rec.Stdout == "" && rec.Err == "" {
				t.Fatalf("%q has no recorded answer: re-record it before shipping the door that reads it", src)
			}
		})
	}
}

package lang

import (
	"strings"
	"testing"
)

// A comprehension, a call or a literal read answered at compile time leaves the SUBSCRIPT standing on a
// number, and the reference answers `1[0]` with a TypeError the program can catch. This is roadmap
// **Gap R.227**: `print([[1, 2][0][0], 1])` and `print([[1][0][0], 1])` stopped the build with
// `codegen: index requires an inline list/dict/set literal` at exit 1 — a sentence that blames a literal
// the program did write, sent at the wrong half — while the reference raises
// `TypeError: 'int' object is not subscriptable`.
//
// The fold is not the bug and is not touched: `[1, 2][0]` IS the integer 2, and `(*irGen).constantSubscriptKind`
// walks one fold level so the OUTER subscript sees what the program actually wrote (`foldedLiteralElemOf` asks
// the same `listLiteralFoldAnswers` question valueIndex asks, so the fold and this walk cannot disagree —
// ADR 0326's one-question rule). What changed is that the subscript door now raises where it used to refuse
// (**ADR 0440**, beside **ADR 0326**'s KeyError and **ADR 0330**'s IndexError, all three emitted through
// `constantTrap` so the landing pad the enclosing `try:` pushed is reached and the verifier is never handed a
// block whose next instruction follows a terminator).
//
// Every row is judged on the two legs: the compiled answer, and `testdata/interpreter-golden.json`'s answer for
// the same source (the class and sentence the record holds, via recordTrapOf). Nothing here claims a second
// engine ran: the record is a record.

// foldedSubscriptTraps is the family: the same accident, written at every position the language has for a
// value, over each constant kind the fold can leave behind. `class`/`message` are CPython's, spelled by
// `notSubscriptableSentence`, which the run-time subscript door quotes too — one program, one message,
// whichever road read the slot.
var foldedSubscriptTraps = []struct {
	name     string
	src      string
	class    string
	message  string
	innerGap string // set when the trap is the INNER read's accident, which the reference reaches first
}{
	{"a read of a read of a literal, printed", "print([[1, 2][0][0], 1])", "TypeError", "'int' object is not subscriptable", ""},
	{"the row as filed, one element", "print([[1][0][0], 1])", "TypeError", "'int' object is not subscriptable", ""},
	{"the same read as the whole argument", "print([[1, 2][0][0]])", "TypeError", "'int' object is not subscriptable", ""},
	{"a literal subscripted twice without a nesting", "print([1, 2, 3][0][0])", "TypeError", "'int' object is not subscriptable", ""},
	{"a tuple's slot read twice", "print({(1, 2)[0][0]})", "TypeError", "'int' object is not subscriptable", ""},
	{"the read in arithmetic", "print([[1, 2][0][0] + 1])", "TypeError", "'int' object is not subscriptable", ""},
	{"the read compared", "print([[1, 2][0][0] == 1])", "TypeError", "'int' object is not subscriptable", ""},
	{"the read as a membership needle", "print([[1, 2][0][0] in [1]])", "TypeError", "'int' object is not subscriptable", ""},
	{"the read rendered", "print(str([[1, 2][0][0], 1]))", "TypeError", "'int' object is not subscriptable", ""},
	{"the read bound and then printed", "xs = [[1, 2][0][0], 1]\nprint(xs)", "TypeError", "'int' object is not subscriptable", ""},
	{"the read in a condition", "print([1, 2][0][0] if 1 else 0)", "TypeError", "'int' object is not subscriptable", ""},
	{"two of them in one literal", "print([[1, 2][0][0], [3][0][0]])", "TypeError", "'int' object is not subscriptable", ""},
	{"the read handed to a builtin", "print(sorted([[1, 2][0][0], 1]))", "TypeError", "'int' object is not subscriptable", ""},
	{"a comprehension element", "print([[1, 2][0][0] for x in [1]])", "TypeError", "'int' object is not subscriptable", ""},
	{"a comprehension iterable", "print([x for x in [[1, 2][0][0]]])", "TypeError", "'int' object is not subscriptable", ""},
	{"a set literal holding the read", "print({[1, 2][0][0]})", "TypeError", "'int' object is not subscriptable", ""},
	{"a verdict's slot, not a number's", "print([[True][0][0]])", "TypeError", "'bool' object is not subscriptable", ""},
	{"a double's slot", "print([[1.5][0][0]])", "TypeError", "'float' object is not subscriptable", ""},
	{"the void's slot", "print([[None][0][0]])", "TypeError", "'NoneType' object is not subscriptable", ""},
	{"three deep, the void winning", "print([[[None]][0][0][0]])", "TypeError", "'NoneType' object is not subscriptable", ""},
	{
		// The order is the reference's: the inner read is what the program wrote first, and its own accident
		// (a position that is not there) is what must surface — the outer TypeError would describe a read the
		// program never got to.
		"the inner read running off the end first", "print([[[1, 2][5]][0]])", "IndexError", "list index out of range", "Gap R.227's own order",
	},
}

// TestASubscriptOfAFoldedReadRaisesWhatTheReferenceRaises walks the family on the compiled leg and then asks
// the record the same question, so the class and sentence are witnesses rather than opinions. Exit 2 —
// ADR 0166's code for a compiler bug — is forbidden by `jitTrap`, which fails the run on it.
func TestASubscriptOfAFoldedReadRaisesWhatTheReferenceRaises(t *testing.T) {
	t.Parallel()
	for _, tt := range foldedSubscriptTraps {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			class, message := jitTrap(t, tt.src)
			if class != tt.class || message != tt.message {
				t.Fatalf("raised %s: %s, want %s: %s", class, message, tt.class, tt.message)
			}
			rec, trapped := recordTrapOf(t, tt.src)
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

// The same raises catchably, with the handler the program itself writes — including the `as` form, which is
// the one that reads the sentence and so the one a wrong message would break.
func TestTheFoldedSubscriptRaiseReachesTheProgramsOwnHandler(t *testing.T) {
	t.Parallel()
	for _, tt := range foldedSubscriptTraps {
		if tt.innerGap != "" {
			continue // the handler question is asked once per class, and this row's class is IndexError
		}
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			indented := strings.ReplaceAll(tt.src, "\n", "\n    ")
			prog := "try:\n    " + indented + "\nexcept " + tt.class + " as e:\n    print(\"caught\", e)"
			want := "caught " + tt.message
			if out := strings.TrimRight(compiledOut(t, prog), "\n"); out != want {
				t.Fatalf("the raise is not catchable with its sentence: got %q, want %q", out, want)
			}
		})
	}
}

// The controls are the half that keeps the fix honest: a chain whose reads the fold and the pair door already
// answer must print what it printed before, and a TEXT keeps being subscriptable twice because CPython's
// `"ab"[0][0]` is `'a'[0]` and answers `'a'`. Every expectation here is what CPython prints; a refusal fails
// the case, which is what makes this table a gate rather than a list.
func TestTheFoldedSubscriptControlsStillAnswer(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ src, want string }{
		{"print([[1, 2][0], 1])", "[1, 1]"},
		{"print([[[1, 2][0]][0], 1])", "[1, 1]"},
		{"print([[[1], [2]][1][0], 5])", "[2, 5]"},
		{"print([[[1, 2], [3, 4]][1][0], 9])", "[3, 9]"},
		{"print([[1, 2, 3][1] + 1])", "[3]"},
		{"print([[1, 2][0] == 1])", "[True]"},
		{"print([1, 2][1])", "2"},
		{"print([\"ab\"[0][0]])", "['a']"},
		{"print([\"ab\"[0], \"c\"[0]])", "['a', 'c']"},
		{"d = {\"a\": {\"b\": [1]}}\nprint(d[\"a\"][\"b\"][0])", "1"},
		{"print({\"a\": [1, 2][0]}[\"a\"])", "1"},
		{"print([[[1, 2]], [3]][0][0][0])", "1"},
		{"print([[[1, 2][0], [3][0]]])", "[[1, 3]]"},
		{"print([[[1, 2][0]], [3]][0][0])", "1"},
		{"xs = [[1, 2][0], 1]\nprint(xs[0])", "1"},
	} {
		t.Run(tt.src, func(t *testing.T) {
			t.Parallel()
			if out := strings.TrimRight(compiledOut(t, tt.src), "\n"); out != tt.want {
				t.Fatalf("answered %q, want the reference's %q", out, tt.want)
			}
			if rec, err := runGoldenStdout(t, tt.src); err == nil {
				if strings.TrimRight(rec, "\n") != tt.want {
					t.Fatalf("the record says %q and the compiled leg says %q for the same source", rec, tt.want)
				}
			}
		})
	}
}

// The trap has to be IN the module — a raise the compiler emitted rather than one it reported from the host —
// and the module must not carry the refusal the same program used to stop the build on. The second assertion
// is the one that fails on a regression to the old behaviour; the third is ADR 0188's rule, which this family
// reached by a different road (a container global in an `i32` operand is how the sibling family exited 2).
func TestTheFoldedSubscriptTrapIsEmittedAndTheRefusalIsGone(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"print([[1, 2][0][0], 1])",
		"print([[1][0][0], 1])",
		"print([[True][0][0]])",
		"print([[[None]][0][0][0]])",
		"print([[1, 2][0][0] for x in [1]])",
	} {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			ir := mustCompileIR(t, src)
			if !strings.Contains(ir, "TypeError") {
				t.Fatalf("the module carries no TypeError for %q:\n%s", src, ir)
			}
			if strings.Contains(ir, "index requires an inline") {
				t.Fatalf("the module still carries the fold's refusal for %q:\n%s", src, ir)
			}
			if strings.Contains(ir, "i32 @.") {
				t.Fatalf("a compile-time container global reached an operand position in %q (ADR 0188):\n%s", src, ir)
			}
		})
	}
	for _, tt := range []struct{ src, sentence string }{
		{"print([[1, 2][0][0], 1])", "'int' object is not subscriptable"},
		{"print([[True][0][0]])", "'bool' object is not subscriptable"},
		{"print([[1.5][0][0]])", "'float' object is not subscriptable"},
		{"print([[None][0][0]])", "'NoneType' object is not subscriptable"},
	} {
		t.Run("sentence/"+tt.sentence, func(t *testing.T) {
			t.Parallel()
			ir := mustCompileIR(t, tt.src)
			if !strings.Contains(ir, tt.sentence) {
				t.Fatalf("the module does not quote CPython's %q for %q:\n%s", tt.sentence, tt.src, ir)
			}
		})
	}
}

// The predicate is asked directly, because a door widened by accident fails a decision row rather than only a
// behaviour row (the suite's standing rule for a gate): the constants it must name, the containers and texts it
// must NOT name, and the fold level it must stop at.
func TestTheFoldedSubscriptPredicateIsAskedDirectly(t *testing.T) {
	t.Parallel()
	g := &irGen{}
	for _, tt := range []struct {
		name string
		e    Expr
		want string // "" means "no answer"
	}{
		{"an integer", &IntLit{Value: 7}, "int"},
		{"a double", &FloatLit{Value: 1.5}, "float"},
		{"a verdict", &BoolLit{Value: true}, "bool"},
		{"the void", &NoneLit{}, "NoneType"},
		{"a text, which is subscriptable once", &StrLit{Value: "a"}, ""},
		{"a read of a list of integers", &Index{Obj: &ListLit{Elems: []Expr{&IntLit{Value: 1}, &IntLit{Value: 2}}}, Idx: &IntLit{Value: 0}}, "int"},
		{"a read of a tuple of integers", &Index{Obj: &ListLit{Elems: []Expr{&IntLit{Value: 1}, &IntLit{Value: 2}}, Tuple: true}, Idx: &IntLit{Value: 1}}, "int"},
		{"a read of a list of texts, which the pair door owns", &Index{Obj: &ListLit{Elems: []Expr{&StrLit{Value: "a"}}}, Idx: &IntLit{Value: 0}}, ""},
		{"a read out of range, which the bounds check owns", &Index{Obj: &ListLit{Elems: []Expr{&IntLit{Value: 1}}}, Idx: &IntLit{Value: 5}}, ""},
		{"a read by a position the compiler cannot read", &Index{Obj: &ListLit{Elems: []Expr{&IntLit{Value: 1}, &IntLit{Value: 2}}}, Idx: &Name{Value: "i"}}, ""},
		{"a name, which the notebooks own", &Name{Value: "xs"}, ""},
		{"a call, which Gap R.214's road owns", &Call{Fn: &Name{Value: "f"}}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := g.constantSubscriptKind(tt.e)
			if ok != (tt.want != "") || got != tt.want {
				t.Fatalf("named %q (ok=%v), want %q", got, ok, tt.want)
			}
		})
	}
}

// Two shapes the sweep surfaced, pinned with today's answer beside the reference's rather than papered over.
// Neither is this commit's damage: both measure identically on `b276ddf` and on this build, which is what keeps
// them rows rather than part of the fix.
func TestWhatTheFoldedSubscriptSweepStillOwesIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		src     string
		today   string // what this backend does today, byte for byte
		oracle  string // what CPython answers
		gap     string
		noExit2 bool
		refusal bool
	}{
		{
			// `len` of a literal takes the literal's COUNT and never lowers the elements, so an element
			// whose read raises is skipped: the call inside the literal never runs either.
			"len of a literal whose element raises",
			"print(len([[1, 2][0][0], 1]))",
			"2", "TypeError: 'int' object is not subscriptable",
			"roadmap Gap R.494 (the count door never evaluates the spelled-out elements)", true, false,
		},
		{
			"len of a literal whose element calls",
			"def f():\n    print(\"side\")\n    return 1\nprint(len([f()]))",
			"1", "side\n1",
			"roadmap Gap R.494 (the same door, the effect the reference performs)", true, false,
		},
		// The fourth shape the sweep found — a subscript of a read out of a COMPREHENSION, Gap R.495 —
		// is filed beside its own door, where the pair-element residue lives:
		// pkg/lang/pair_element_read_test.go::TestWhatThePairElementDoorStillCannotReachIsFiledNotFixed.
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, tt.src)
			if tt.refusal {
				if !refused {
					t.Fatalf("%q answered %q where this row records a refusal and the reference answers %q — the door grew, so move the row", tt.src, out, tt.oracle)
				}
				if !strings.Contains(out, "list/dict/set literal") {
					t.Fatalf("%q refused without the sentence this row pins: %s", tt.src, out)
				}
				return
			}
			if refused {
				if strings.Contains(out, "codegen: internal invariant") || strings.Contains(out, "global variable reference") {
					t.Fatalf("%q failed as a compiler bug rather than a refusal: %s", tt.src, out)
				}
			}
			got := strings.TrimRight(out, "\n")
			if refused {
				got = "refusal: " + strings.TrimPrefix(strings.TrimRight(out, "\n"), "gustyc: jit: codegen: ")
			}
			if strings.Contains(tt.today, "object is not subscriptable") && !refused {
				// A trap row: the answer today is the raise, which compiledOut reports as a failure, so the
				// row is asserted through jitTrap instead.
				class, message := jitTrap(t, tt.src)
				if got2 := class + ": " + message; got2 != tt.oracle {
					t.Fatalf("raised %q where this row pins %q", got2, tt.today)
				}
				return
			}
			if got != tt.today {
				t.Fatalf("today's answer is %q, not the %q this row pins; the reference answers %q", got, tt.today, tt.oracle)
			}
		})
	}
}

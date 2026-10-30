package lang

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

// A slice whose base has no slots to slice at all — a number, a bool, the singleton.
//
// ADR 0466 installed the order (the object first, the bounds second) and asked it of the two containers that
// have no positions: a set and a dict. The same order was never asked of the bases that have no slots AT ALL,
// and those are the simplest cases the reference has. Every one of these programs raises, naming the kind:
//
//	print(1[0:1])        # TypeError: 'int' object is not subscriptable
//	print(1.5[0:1])      # TypeError: 'float' object is not subscriptable
//	print(True[0:1])     # TypeError: 'bool' object is not subscriptable — a bool is an int WHERE A POSITION
//	                     #  IS ASKED, and it is a bool where the object is asked
//	o = None
//	print(o[0:1])        # TypeError: 'NoneType' object is not subscriptable
//
// This backend refused them at exit 1 with the copy road's complaint — `cannot slice …: the copy is a list
// whose slots are the base's payloads with their tags, and the base is not a list the records describe` — a
// sentence about a list the program never wrote, standing where the program's own exception should stand
// (roadmap **Gap R.559**). The subscript door beside it had long since been given the answer: `print(1[0])`
// raises `'int' object is not subscriptable` through `constantSubscriptKind` (**ADR 0420**, Gap R.227). This
// row's whole content is asking THAT door again, from the slice road, instead of writing a second one.
//
// The claim this file guards, in order:
//
//   - every raise's class and sentence are the reference's bytes, on both witness legs (the compiled leg
//     through `jitTrap`, the record through `goldenRunError`), and no leg is allowed to answer with a value;
//   - the raise is the program's own to catch — `except TypeError:` runs the handler, and the message is what
//     `str(err)` binds — so the row is about an exception and not a diagnostic;
//   - the base is asked before the bounds, and the four bases answer with four DIFFERENT sentences (the list
//     with ADR 0336's bounds complaint, the set with the object's, the dict with hashing or the miss, and a
//     scalar with its own kind), which is the only way to tell this gate from one that raises one message;
//   - the kind is read from records — the subscript road's classifier for what the SOURCE wrote, and
//     `boolVars`/`noneVars`/`floatVars` for a name — and a name no record describes keeps its old road
//     (ADR 0232: a kind claim is imported from a writer, never invented);
//   - the module says so: a refusing base carries the raise and never calls `rt_slice`, while a base the road
//     serves still calls it (the counter-row that keeps the absence meaningful);
//   - what the gate declines stays filed, in the last table, with the reference's answer beside it.

// sliceScalarRaiseRows are the shapes this row answers with the object's own complaint. The class and message
// are read off CPython for each source — the standing rule that a raise row quotes the reference rather than
// paraphrasing it (Gap N) — and every one was confirmed against both witness legs before it was written here.
var sliceScalarRaiseRows = []struct {
	name    string
	src     string
	class   string
	message string
}{
	// ---- the numbers, spelled as literals ----
	{
		"a slice of an int literal",
		"print(1[0:1])",
		"TypeError", "'int' object is not subscriptable",
	},
	{
		// The parenthesised spelling of the same object: a gate that read the AST shape rather than the
		// object would answer this one with the value it refuses over.
		"a slice of an int the program parenthesised",
		"print((1)[0:1])",
		"TypeError", "'int' object is not subscriptable",
	},
	{
		"a slice of a float literal",
		"print(1.5[0:1])",
		"TypeError", "'float' object is not subscriptable",
	},
	{
		// The empty slice: no bound at all, so the only question the program asked is the one about the
		// object. A gate that asked the bounds first had nothing to say about this row.
		"a float sliced by nothing",
		"print(1.5[:])",
		"TypeError", "'float' object is not subscriptable",
	},
	// ---- the verdicts: an int where a POSITION is asked, a bool where the OBJECT is asked ----
	{
		"a slice of a verdict written True",
		"print(True[0:1])",
		"TypeError", "'bool' object is not subscriptable",
	},
	{
		"a slice of a verdict written False, by a step alone",
		"print(False[::2])",
		"TypeError", "'bool' object is not subscriptable",
	},
	// ---- the void ----
	{
		"a slice of None",
		"print(None[0:1])",
		"TypeError", "'NoneType' object is not subscriptable",
	},
	{
		"a slice of None by a step alone",
		"print(None[::])",
		"TypeError", "'NoneType' object is not subscriptable",
	},
	// ---- the same objects held in a name: the kind comes from the binding, not the spelling ----
	{
		"a slice of a name the program bound to a verdict",
		"b = True\nprint(b[0:1])",
		"TypeError", "'bool' object is not subscriptable",
	},
	{
		"a slice of a name the program bound to a float",
		"f = 1.5\nprint(f[:])",
		"TypeError", "'float' object is not subscriptable",
	},
	{
		"a slice of a name the program bound to the void",
		"o = None\nprint(o[0:1])",
		"TypeError", "'NoneType' object is not subscriptable",
	},
	// ---- the read the fold already answers: the element of a literal, sliced ----
	//
	// This is the row that makes the ADR 0420 reuse visible. `xs[0]` is a position the subscript road has
	// already decided the kind of, and the slice road asking the same door gets the same answer for free —
	// which is also why `print([1, 2][0][0])` and this program are one complaint (Gap R.227's fold).
	{
		"a slice of the element of a literal list",
		"print([1, 2][0][0:1])",
		"TypeError", "'int' object is not subscriptable",
	},
	// ---- the order rows: a bound that WOULD complain, written over a base that refuses first ----
	//
	// A text bound over a list earns ADR 0336's slice-indices sentence. Over an int it never gets to speak:
	// the reference asks the object whether it can be subscripted at all, and an int cannot. A gate placed
	// after the bounds door answers `slice indices must be integers or None or have an __index__ method` here,
	// which is a sentence about a container the program did not write.
	{
		"text bounds over an int say the object, not the bounds",
		"print(1[\"a\":\"b\"])",
		"TypeError", "'int' object is not subscriptable",
	},
	{
		"a float bound over the void says the object, not the bounds",
		"print(None[1.5:2])",
		"TypeError", "'NoneType' object is not subscriptable",
	},
	{
		// A bool bound is a position (bool is an int where a position is asked) and the base still decides:
		// this is the row that separates the two doors' ideas of a verdict. The list spelling of the same
		// slice — `[1, 2][True:2]` — answers [2], and is in the untouched table below.
		"a verdict bound over an int says the object",
		"print(1[True:1])",
		"TypeError", "'int' object is not subscriptable",
	},
}

// sliceScalarCatchRows ask the same raises as the program's own: a handler runs, the exit code is the
// program's 0, and the bytes on stdout are the reference's. A raise that could not be caught would be a
// diagnostic wearing an exception's clothes (ADR 0166's distinction).
var sliceScalarCatchRows = []struct {
	name string
	src  string
	want string
}{
	{
		"a program that catches the int's refusal",
		"try:\n    print(1[0:1])\nexcept TypeError:\n    print(\"the int refused the slice\")",
		"the int refused the slice",
	},
	{
		// The message is the raise's ARGUMENT, not just its caption: `print(e)` binds what the reference
		// binds (Gap R.113's rule about what an `except … as` name holds).
		"a handler that prints the message it caught",
		"try:\n    print(1[0:1])\nexcept TypeError as e:\n    print(e)",
		"'int' object is not subscriptable",
	},
	{
		"two refusals in one program, each caught by its own arm",
		"try:\n    print(1[0:1])\nexcept TypeError:\n    print(\"no slots\")\n\ntry:\n    print(None[0:1])\nexcept TypeError:\n    print(\"still no slots\")\n\nprint(\"done\")",
		"no slots\nstill no slots\ndone",
	},
	{
		"a verdict and a float caught together",
		"b = True\nf = 2.5\ntry:\n    print(b[0:1])\nexcept TypeError:\n    print(\"verdict\")\n\ntry:\n    print(f[0:1])\nexcept TypeError:\n    print(\"number\")\n\nprint(\"after\")",
		"verdict\nnumber\nafter",
	},
}

// sliceScalarUntouchedRows are the doors this gate must not lean on. Each answered before the row and answers
// the same bytes now; the reference is asked for the same string, so a row that drifted is caught by both legs.
var sliceScalarUntouchedRows = []struct {
	name string
	src  string
	want string
}{
	{
		"a list still slices",
		"print([1, 2][0:1])",
		"[1]",
	},
	{
		"a text still slices by counting characters",
		"print(\"abcd\"[1:3])",
		"bc",
	},
	{
		"a tuple still slices, and keeps its parentheses",
		"print((1, 2)[0:1])",
		"(1,)",
	},
	{
		"a name bound to a list still slices",
		"xs = [1, 2]\nprint(xs[0:2])",
		"[1, 2]",
	},
	{
		// The safety order made visible: the scalar records are not cleared when a container takes the name
		// over, so a gate that asked THEM first would refuse this program. It keeps its answer.
		"a name that held a float and now holds a list still slices",
		"xs = 1.5\nxs = [1, 2]\nprint(xs[0:1])",
		"[1]",
	},
	{
		// A bool bound IS a position: the row beside it refuses a bool BASE, and this one answers.
		"a verdict bound still indexes a list",
		"print([1, 2][True:2])",
		"[2]",
	},
	{
		"a text name still slices",
		"s = \"abc\"\nprint(s[0:1])",
		"a",
	},
	{
		"a name bound to a float still does arithmetic",
		"f = 1.5\nprint(f + 1)",
		"2.5",
	},
	{
		"a name bound to a verdict still compares",
		"b = True\nprint(b == True)",
		"True",
	},
	{
		"the void still prints",
		"o = None\nprint(o)",
		"None",
	},
	{
		// The ADR 0466 half, asked again so the two arms of the base gate cannot trade places.
		"a set still refuses by the object",
		"try:\n    print({1, 2}[0:1])\nexcept TypeError:\n    print(\"set\")",
		"set",
	},
	{
		"a dict still reports the miss by naming the key",
		"try:\n    print({1: 2}[0:1])\nexcept KeyError:\n    print(\"miss\")",
		"miss",
	},
}

// TestASliceOfAValueWithNoSlotsRaisesWhatItIs is the row's claim, asked of both witness legs: the class and the
// sentence are the reference's, and neither leg may answer with a value or stay silent.
func TestASliceOfAValueWithNoSlotsRaisesWhatItIs(t *testing.T) {
	t.Parallel()
	for _, tt := range sliceScalarRaiseRows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			class, message := jitTrap(t, tt.src)
			if class != tt.class || message != tt.message {
				t.Fatalf("%q raised %s: %s; the reference raises %s: %s", tt.src, class, message, tt.class, tt.message)
			}
			// The record leg, so the compiled leg cannot witness the row alone. An entry the record does not
			// hold is a failure, not a skip (ADR 0302).
			err := goldenRunError(t, tt.src)
			if err == nil {
				t.Fatalf("%q has no recorded trap, although the compiled run raises %s: %s", tt.src, class, message)
			}
			var te *TrapError
			if errors.As(err, &te) {
				if te.ExnType != tt.class || te.ExnMsg != tt.message {
					t.Fatalf("the record traps %s: %s; the reference raises %s: %s", te.ExnType, te.ExnMsg, tt.class, tt.message)
				}
			}
		})
	}
}

// TestAScalarBaseTrapIsCatchable is the same raises asked of the whole program: the handler runs, the exit is
// the program's own, and the record leg agrees byte for byte.
func TestAScalarBaseTrapIsCatchable(t *testing.T) {
	t.Parallel()
	for _, tt := range sliceScalarCatchRows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); the reference runs the handler and prints %q", tt.src, got, tt.want)
			}
			if got != tt.want+"\n" {
				t.Fatalf("%q printed %q, want %q", tt.src, got, tt.want+"\n")
			}
			rec, err := runGoldenStdout(t, tt.src)
			if err != nil {
				t.Fatalf("the record does not answer a program both legs agree on: %v", err)
			}
			if strings.TrimRight(rec, "\n") != tt.want {
				t.Fatalf("the record says %q and the compiled leg says %q for the same source", rec, got)
			}
		})
	}
}

// TestTheScalarBaseIsAskedBeforeTheBounds is the order asked as one table rather than as twelve rows: the same
// bound, written over the bases that answer it differently. Four bases, four sentences — a gate that had
// stopped reading the base would collapse them onto one string, and that collapse is what this test is for.
func TestTheScalarBaseIsAskedBeforeTheBounds(t *testing.T) {
	t.Parallel()
	type base struct {
		src     string
		class   string
		message string
	}
	for _, tt := range []struct {
		name  string
		bases []base
	}{
		{
			// A text bound: the list complains about the bound, the set and the int about themselves, and the
			// dict hashes the slice and misses.
			"text bounds over a list, a set, a dict and an int",
			[]base{
				{"xs = [1, 2]\nprint(xs[\"a\":\"b\"])", "TypeError", "slice indices must be integers or None or have an __index__ method"},
				{"print({1, 2}[\"a\":\"b\"])", "TypeError", "'set' object is not subscriptable"},
				{"print({1: 2}[\"a\":\"b\"])", "KeyError", "slice('a', 'b', None)"},
				{"print(1[\"a\":\"b\"])", "TypeError", "'int' object is not subscriptable"},
			},
		},
		{
			// A float bound, which the counting doors refuse and the others never look at.
			"a float bound over a list, a set, a dict and the void",
			[]base{
				{"xs = [1, 2]\nprint(xs[1.5:2])", "TypeError", "slice indices must be integers or None or have an __index__ method"},
				{"print({1, 2}[1.5:2])", "TypeError", "'set' object is not subscriptable"},
				{"print({1: 2}[1.5:2])", "KeyError", "slice(1.5, 2, None)"},
				{"print(None[1.5:2])", "TypeError", "'NoneType' object is not subscriptable"},
			},
		},
		{
			// A step of zero: the door that strides divides, the door that hashes never asks, and the door
			// with no slots never gets that far.
			"a step of zero over a list, a dict and an int",
			[]base{
				{"xs = [1, 2]\nprint(xs[0:1:0])", "ValueError", "slice step cannot be zero"},
				{"print({1: 2}[0:1:0])", "KeyError", "slice(0, 1, 0)"},
				{"print(1[0:1:0])", "TypeError", "'int' object is not subscriptable"},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			seen := map[string]bool{}
			for _, b := range tt.bases {
				class, message := jitTrap(t, b.src)
				if class != b.class || message != b.message {
					t.Fatalf("%q raised %s: %s; the reference raises %s: %s", b.src, class, message, b.class, b.message)
				}
				// The record leg asked of the same raise, so the order cannot be witnessed by the JIT alone: a
				// row whose entry has gone is a failure rather than a skip (ADR 0302).
				if err := goldenRunError(t, b.src); err == nil {
					t.Fatalf("%q has no recorded trap, although the compiled run raises %s: %s", b.src, class, message)
				} else {
					var te *TrapError
					if errors.As(err, &te) && (te.ExnType != b.class || te.ExnMsg != b.message) {
						t.Fatalf("the record traps %s: %s for %q; the reference raises %s: %s", te.ExnType, te.ExnMsg, b.src, b.class, b.message)
					}
				}
				seen[class+": "+message] = true
			}
			if len(seen) != len(tt.bases) {
				t.Fatalf("the %d bases answered with %d distinct sentences: %v", len(tt.bases), len(seen), seen)
			}
		})
	}
}

// TestTheScalarBaseGateNamesAKindOnlyWhereARecordNamesIt is the soundness half: the door answers only where a
// record or the source itself names the kind, and says nothing where nobody did. A door that guessed would
// take an answer away from a program that compiles today (ADR 0273), so the silent rows are the load-bearing
// ones — including the emitter-less door asked of a nil generator, which must not panic.
func TestTheScalarBaseGateNamesAKindOnlyWhereARecordNamesIt(t *testing.T) {
	t.Parallel()
	// A door with no emitter and no notebooks is a question nobody answered. It is also the call a future
	// fold would make, and a panic here would be the compiler's crash reported as the program's (ADR 0166).
	nilDoor := (*irGen)(nil)
	for _, e := range []Expr{
		&IntLit{Value: 1},
		&BoolLit{Value: true},
		&NoneLit{},
		&Name{Value: "x"},
		&StrLit{Value: "abc"},
	} {
		if kind, claimed := nilDoor.sliceBaseWithNoSlots(e); claimed {
			t.Fatalf("a door with no notebooks named %q for %v; a kind claim needs a writer (ADR 0232)", kind, e)
		}
	}

	g := &irGen{}
	for _, tt := range []struct {
		name string
		e    Expr
		kind string
	}{
		// The kinds the SOURCE wrote, from the subscript road's classifier.
		{"an int literal is an int", &IntLit{Value: 7}, "int"},
		{"a float literal is a float", &FloatLit{Value: 1.5}, "float"},
		{"a verdict is a verdict, not an int", &BoolLit{Value: true}, "bool"},
		{"the void is NoneType", &NoneLit{}, "NoneType"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			kind, claimed := g.sliceBaseWithNoSlots(tt.e)
			if !claimed || kind != tt.kind {
				t.Fatalf("the door answered (%q, %v); the reference names it %q", kind, claimed, tt.kind)
			}
		})
	}

	// The kinds only a binding record can name — asked of a generator whose notebooks were written by the
	// binding road, which is the same way the print door and the bounds door ask (ADR 0172, ADR 0336).
	recorded := &irGen{
		boolVars:  map[string]bool{"b": true},
		noneVars:  map[string]bool{"o": true},
		floatVars: map[string]bool{"f": true},
		listVars:  map[string]bool{"xs": true},
	}
	for _, tt := range []struct {
		name string
		e    Expr
		kind string
		want bool
	}{
		{"a name bound to a verdict", &Name{Value: "b"}, "bool", true},
		{"a name bound to the void", &Name{Value: "o"}, "NoneType", true},
		{"a name bound to a float", &Name{Value: "f"}, "float", true},
		// The container notebook answers FIRST, which is the safety order: a name the container roads serve
		// keeps its answer even if a scalar record still names it (see the untouched table's rebinding row).
		{"a name bound to a list is not this question", &Name{Value: "xs"}, "", false},
		{"a name no record describes is nobody's kind", &Name{Value: "mystery"}, "", false},
		{"a text is sliced by counting, not refused", &StrLit{Value: "abc"}, "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			kind, claimed := recorded.sliceBaseWithNoSlots(tt.e)
			if claimed != tt.want {
				t.Fatalf("the door claimed (%q, %v); wanted (%q, %v)", kind, claimed, tt.kind, tt.want)
			}
			if tt.want && kind != tt.kind {
				t.Fatalf("the door named %q; the reference names %q", kind, tt.kind)
			}
		})
	}
}

// TestTheScalarBaseGateRepairsNothingElse is the ADR 0273 half asked as a table: the bases the slice road serves
// — a list, a text, a tuple, and a name last bound to one of them — still answer, with the same bytes the
// reference prints. The rebinding row is the load-bearing one: the scalar records are not cleared when a
// container binds over them, so a gate that read THEM first would refuse a program that answers today.
func TestTheScalarBaseGateRepairsNothingElse(t *testing.T) {
	t.Parallel()
	for _, tt := range sliceScalarUntouchedRows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); this door answers %q and the row exists to keep it answering", tt.src, got, tt.want)
			}
			if got != tt.want+"\n" {
				t.Fatalf("%q printed %q, want %q", tt.src, got, tt.want+"\n")
			}
			// The record leg: the same source answers the same bytes there, and carries no trap.
			rec, err := runGoldenStdout(t, tt.src)
			if err != nil {
				t.Fatalf("the record does not answer a program both legs agree on: %v", err)
			}
			if strings.TrimRight(rec, "\n") != tt.want {
				t.Fatalf("the record says %q and the compiled leg says %q for the same source", rec, got)
			}
		})
	}
}

// TestNoScalarBaseIsHandedToTheCopyHelper is the IR half of the claim: the raise is emitted where the read was,
// with the reference's report text and the exception code, and the copy helper is never called for a base the
// road refuses. The counter-row keeps that absence meaningful — a list still calls the helper — because an
// emitter that dropped rt_slice everywhere would pass a test that only looked for its absence.
func TestNoScalarBaseIsHandedToTheCopyHelper(t *testing.T) {
	t.Parallel()
	for _, tt := range sliceScalarRaiseRows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ir := mustCompileIR(t, tt.src)
			report := tt.class + ": " + tt.message
			if !strings.Contains(ir, report) {
				t.Fatalf("the module never carries the raise %q the program is expected to report:\n%s", report, mainBodyOf(ir))
			}
			wantCode := "store i32 " + strconv.Itoa(exnCode(tt.class)) + ", i32* @exn_code"
			if !strings.Contains(ir, wantCode) {
				t.Fatalf("the raise did not set the exception code for %s:\n%s", tt.class, mainBodyOf(ir))
			}
			for _, line := range strings.Split(mainBodyOf(ir), "\n") {
				if strings.Contains(line, "call i32 @rt_slice(") {
					t.Fatalf("the road built a copy of a base that has no slots to copy:\n%s", line)
				}
			}
		})
	}
	// The counter-row.
	ir := mustCompileIR(t, "print([1, 2][0:1])")
	if !strings.Contains(mainBodyOf(ir), "call i32 @rt_slice(") {
		t.Fatalf("a list's slice no longer reaches the copy helper, which makes the rows above vacuous:\n%s", mainBodyOf(ir))
	}
}

// TestTheScalarBaseRaiseEndsTheBlockItBranchedFrom pins the non-local road: two refusals in one statement are
// two sites, each with its own continuation, because a shared reopen is how a raise starts printing the other
// half of the statement.
func TestTheScalarBaseRaiseEndsTheBlockItBranchedFrom(t *testing.T) {
	t.Parallel()
	ir := mustCompileIR(t, "b = True\nn = None\nprint(b[0:1], n[0:1])")
	sites := map[string]bool{}
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "sbb.") && strings.HasSuffix(trimmed, ":") {
			sites[trimmed] = true
		}
	}
	if len(sites) < 2 {
		t.Fatalf("the two refusing slices share a continuation (sites %v):\n%s", sites, mainBodyOf(ir))
	}
	if strings.Contains(ir, "call i32 @rt_slice(") {
		t.Fatalf("neither slice reached the copy helper, and the module says otherwise:\n%s", mainBodyOf(ir))
	}
}

// TestWhatTheScalarBaseDoorStillCannotReachIsFiledNotFixed keeps the shapes the gate declines as refusals
// rather than letting them become answers. Each is a program the reference traps by naming a kind; the
// toolchain declines at exit 1 because no record names that kind, which is the honest residue (ADR 0273's rule
// that a gate may not invent one). Exit 2 — the code the contract reserves for a compiler bug — fails the row
// wherever it appears (ADR 0166).
func TestWhatTheScalarBaseDoorStillCannotReachIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		src     string
		mustSay string
		oracle  string
		gap     string
	}{
		{
			// A name bound to an int is the ONE kind with no record: the scalar notebooks hold a float, a
			// verdict and the void, because those need a claim to be formatted at all. An int is the default
			// word, and nothing writes a claim about it, so the door has nothing to read.
			"a slice of a name the program bound to an int",
			"n = 1\nprint(n[0:1])",
			"cannot slice",
			"TypeError: 'int' object is not subscriptable",
			"roadmap Gap R.560 (the int binding record: `floatVars`/`boolVars`/`noneVars` exist because those kinds need a word to print, and an int has no such claim written)",
		},
		{
			// The last-binding rule is incomplete across the container/scalar boundary: a container binding
			// does not clear the scalar records and vice versa, so the door cannot tell which the name holds.
			"a slice of a name a container bound and a scalar rebound",
			"xs = [1, 2]\nxs = 1.5\nprint(xs[0:1])",
			"cannot slice",
			"TypeError: 'float' object is not subscriptable",
			"roadmap Gap R.561 (the rebinding residue: ADR 0172's last-binding rule is written by the scalar road over the singleton pair, and not by a container binding over the scalar records)",
		},
		{
			// The answer of a call has a kind, and the answer record is the road that carries it — for a
			// CONTAINER. A scalar the call built has no record this door may read.
			"a slice of the answer of a call",
			"def m(v):\n    return v\n\nprint(m(1)[0:1])",
			"cannot slice",
			"TypeError: 'int' object is not subscriptable",
			"roadmap Gap R.562 (the answer record describes a container's kind and nothing else, so a call that hands back a number is a base with no record)",
		},
		{
			// The kind is a run-time fact until L11.1's tagged word carries it: `constantSubscriptKind` answers
			// the element of a LITERAL (that row is answered above), and a list held in a NAME leaves no element
			// record for the door to read.
			"a slice of a slot of a list the program named",
			"xs = [1, 2]\nprint(xs[0][0:1])",
			"cannot slice",
			"TypeError: 'int' object is not subscriptable",
			"roadmap Gap R.562 (the element kind of a slot is a run-time fact until L11.1's tagged value word carries it, beside Gap R.540's slot read)",
		},
		{
			// The raise's word has no kind, so the outer read asks the slot road for a record and gets a
			// refusal. The reference never gets there: the inner slice raises first (**Gap R.556**'s shape,
			// now with a scalar base beside the set spelling ADR 0466 files).
			"a position read of a slice that raises",
			"print(None[0:1][0])",
			"index requires an inline list/dict/set literal",
			"TypeError: 'NoneType' object is not subscriptable",
			"roadmap Gap R.556 (the word a raise hands back carries no kind; the reference raises before the outer read is ever asked)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, trap, refused := sliceFiledRow(t, tc.src)
			if !refused {
				t.Fatalf("%q answered %q (trap %q); this shape is filed as owed, not fixed — if it now raises or prints, move it to the raise table (reference: %s)",
					tc.src, out, trap, tc.oracle)
			}
			if !strings.Contains(out, tc.mustSay) {
				t.Fatalf("the refusal does not name %q (ADR 0249): %s", tc.mustSay, out)
			}
		})
	}
}

// TestTheScalarBaseRowsAreRecorded is the record leg's own guard: every raise row has an entry in the record,
// so the door cannot be witnessed by the compiled leg alone, and cannot be retired by deleting its evidence
// (ADR 0302's rule that a missing record fails the case).
func TestTheScalarBaseRowsAreRecorded(t *testing.T) {
	t.Parallel()
	for _, tt := range sliceScalarRaiseRows {
		t.Run(tt.src, func(t *testing.T) {
			t.Parallel()
			rec := goldenLookup(t, tt.src)
			if rec.ExnType == "" && rec.Repr == "" && rec.Stdout == "" && rec.Err == "" {
				t.Fatalf("%q is not in pkg/lang/testdata/interpreter-golden.json; record it with `go run -tags=llvm20 ./tools/recmerge -sources …` (ADR 0302)", tt.src)
			}
		})
	}
	for _, tt := range sliceScalarCatchRows {
		t.Run(tt.src, func(t *testing.T) {
			t.Parallel()
			rec := goldenLookup(t, tt.src)
			if rec.Stdout == "" {
				t.Fatalf("%q has no recorded answer: the handler's bytes are the record's to hold", tt.src)
			}
		})
	}
}

package lang

// A membership question asked of a value whose KIND the program promises raises, on the compiled leg and
// on the record leg.
//
// roadmap Gap R.522 · ADR 0453 (the residue ADR 0452 filed)
//
// ADR 0452 closed the half where the SOURCE spells the haystack — `print(1 in 5)` — and pinned the other
// half with today's bytes:
//
//	x = 5                        def f():
//	print(1 in x)             →      return 5
//	print(1 in x)                    print(1 in f())
//	                                 False, at exit 0
//
// This file is that half paid, plus the family beside it: a name whose ONE binder is a spelled literal, a
// name bound from a call whose EVERY road answers such a literal, a module name read from inside a body,
// and a builtin's answer. Each row is asserted twice — the compiled leg's exit code, class and sentence,
// and the record leg's answer for the same source, which is CPython's (ADR 0302: a record a leg cannot
// self-certify).
//
// The load-bearing tables are the two that are NOT about raising. `TestTheMembershipPromiseRetiresWhenTheProgram
// WritesTheNameAgain` holds the programs where a raise would be the WRONG answer — an arm that may or may not
// run, a second binding, a loop target, a `global` written from another frame — and every one of them must
// keep printing what the reference prints. A door that raises too eagerly turns a working operator into a
// trap (ADR 0166's ladder), so those rows are worth more than the trap rows above them.
//
// `TestWhatTheMembershipDoorStillDeclines` is the residue, pinned with today's bytes and the row that owns
// each: a parameter's kind, a name bound by an expression rather than spelled, a body with two arms of two
// kinds, and a value that arrives as a (payload, tag) pair — which is L11.1's tagged word at a one-word
// position (Gap R.146), not this door's.

import (
	"os"
	"strings"
	"testing"
)

// membershipPromiseRows is the family the door reaches: a NAME whose one binder is a spelled literal, a
// CALL whose every road answers one, a name bound from such a call, a module name read inside a body, and a
// builtin's answer. `kind` is the word the reference puts inside its sentence.
func membershipPromiseRows() []struct{ name, src, kind string } {
	return []struct{ name, src, kind string }{
		{"a name bound to a whole number", "x = 5\nprint(1 in x)\n", "int"},
		{"a name bound to the void", "x = None\nprint(1 in x)\n", "NoneType"},
		{"a name bound to a double", "x = 1.5\nprint(\"a\" in x)\n", "float"},
		{"a name bound to a verdict", "x = True\nprint(1 in x)\n", "bool"},
		{"a name bound to the false verdict", "x = False\nprint(1 in x)\n", "bool"},
		{"the negated question, asked of a name", "x = 5\nprint(1 not in x)\n", "int"},
		{"a chain link asks it of a name", "x = 5\nprint(1 == 1 in x)\n", "int"},
		{"the same question inside a loop", "x = 5\nfor i in range(2):\n    print(i in x)\n", "int"},
		{"a call every road of answers a number", "def f():\n    return 5\n\nprint(1 in f())\n", "int"},
		{"both arms of a call answer the same kind", "def f(c):\n    if c:\n        return 1\n\n    return 2\n\nprint(1 in f(1))\n", "int"},
		{"a call every road of answers the void", "def f():\n    print(\"side\")\n\nprint(1 in f())\n", "NoneType"},
		{"a call that writes the void out loud", "def f():\n    return None\n\nprint(1 in f())\n", "NoneType"},
		{"a call that answers a double — the shape that died in llc", "def f():\n    return 1.5\n\nprint(\"a\" in f())\n", "float"},
		{"a name bound from a call's answer", "def f():\n    return 5\n\nx = f()\nprint(1 in x)\n", "int"},
		{"a builtin's answer", "xs = [1, 2]\nprint(1 in len(xs))\n", "int"},
		{"a conversion's answer", "print(1 in int(\"5\"))\n", "int"},
		{"a module name read from inside a body", "x = 5\ndef g():\n    print(1 in x)\n\ng()\n", "int"},
		{"the same name two frames down", "x = 5\ndef outer():\n    def inner():\n        print(1 in x)\n\n    inner()\n\nouter()\n", "int"},
		{"a body's own name", "def g():\n    x = 5\n    print(1 in x)\n\ng()\n", "int"},
		// The run-time door already answered this one; it is here so the two doors are measured side by
		// side and can never name the same value with two sentences (ADR 0232, ADR 0271).
		{"the runtime slot door, which answered all along", "xs = [1]\nprint(1 in xs[0])\n", "int"},
	}
}

func TestMembershipOfAPromisedKindRaises(t *testing.T) {
	t.Parallel()
	for _, tc := range membershipPromiseRows() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sentence := "argument of type '" + tc.kind + "' is not iterable"
			res, err := RunSnippet(tc.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference raises in: %v\n%s", err, tc.src)
			}
			if res.Code == 0 {
				t.Fatalf("answered %q at exit 0 where the reference raises %q\n%s", res.Output, sentence, tc.src)
			}
			if res.Code == 2 {
				t.Fatalf("exit 2 is the compiler's own mistake (ADR 0166):\n%s\n%s", res.Output+res.Stderr, tc.src)
			}
			if res.Code != 3 {
				t.Fatalf("exited %d, want 3 (the trap's exit class): %q\n%s", res.Code, res.Stderr, tc.src)
			}
			got := res.Stderr + res.Output
			if !strings.Contains(got, "TypeError") || !strings.Contains(got, sentence) {
				t.Fatalf("raised %q, want the reference's class and sentence %q\n%s", got, sentence, tc.src)
			}
			if strings.Contains(got, "object is not iterable") {
				t.Fatalf("the membership door spoke the LOOP's sentence (%s) — ADR 0271's one-trap-one-wording\n%s", got, tc.src)
			}
			if strings.Contains(res.Output, "False") || strings.Contains(res.Output, "True") {
				t.Fatalf("the program printed a verdict AND raised: %q\n%s", res.Output, tc.src)
			}
			// The record leg: the trap on record is the reference's, so the answer this row ships is not
			// self-certified (ADR 0302).
			if !HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge\n%s", tc.src, tc.src)
			}
			if rerr := RecordedRunError(t, tc.src); rerr == nil {
				t.Fatalf("the record says this program answers, and the compiled leg raises\n%s", tc.src)
			} else if !strings.Contains(rerr.Error(), sentence) {
				t.Fatalf("the record leg reports %q, not the reference's sentence %q\n%s", rerr, sentence, tc.src)
			}
		})
	}
}

// TestTheCallUnderTheMembershipQuestionStillRuns is the order the reference uses, asked of the half this row
// opened: the HAYSTACK is a call now, and a call has effects. A door that raised before lowering the
// haystack would drop a print, an append or a counter the program performed — the mistake ADR 0451 refused
// to make about a chain link, at the other end of an operator.
func TestTheCallUnderTheMembershipQuestionStillRuns(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name string
		src  string
		want string // the stdout the program must have produced before it raised
		n    int    // how many times that stdout must appear
	}{
		{
			"a haystack that prints",
			"def f():\n    print(\"haystack\")\n\n    return 5\n\nprint(1 in f())\n",
			"haystack", 1,
		},
		{
			"a haystack that counts",
			"calls = 0\n\ndef f():\n    global calls\n    calls = calls + 1\n\n    return 5\n\nprint(1 in f())\n",
			"", 0,
		},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			res, err := RunSnippet(r.src)
			if err != nil {
				t.Fatalf("compile/JIT: %v\n%s", err, r.src)
			}
			if res.Code == 0 || res.Code == 2 {
				t.Fatalf("exited %d where the reference raises the membership sentence\n%s", res.Code, r.src)
			}
			if n := strings.Count(res.Output, r.want); r.n > 0 && n != r.n {
				t.Errorf("the haystack's stdout %q appeared %d times, want %d: %q", r.want, n, r.n, res.Output)
			}
			if r.n > 0 && !strings.Contains(res.Stderr+res.Output, "argument of type 'int' is not iterable") {
				t.Errorf("the raise says %q, want the reference's sentence", res.Stderr+res.Output)
			}
		})
	}
}

// TestTheMembershipPromiseRaiseIsCatchable is what separates a raise from a refusal: the program's own
// `except TypeError:` reaches it, at the program's own exit 0 (ADR 0166's ladder — a refusal would stop a
// program the reference runs with a handler on it).
func TestTheMembershipPromiseRaiseIsCatchable(t *testing.T) {
	t.Parallel()
	rows := membershipCatchableRows()
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, r.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference runs: %v\n%s", err, r.src)
			}
			if got != r.want {
				t.Errorf("printed %q, want %q\n%s", got, r.want, r.src)
			}
			onRecordIs(t, r.src, r.want)
		})
	}
}

// TestTheMembershipPromiseRetiresWhenTheProgramWritesTheNameAgain is the load-bearing table. Every row here
// is a program the reference RUNS, printing a verdict, and the door must stay out of it: a raise where the
// reference answers is a worse defect than the `False` this row was filed for (the gate's own header states
// the counterexample this table reads). Each row is asserted against the reference's bytes on the compiled
// leg AND on the record leg.
func TestTheMembershipPromiseRetiresWhenTheProgramWritesTheNameAgain(t *testing.T) {
	t.Parallel()
	rows := membershipControlRows()
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, r.src)
			if err != nil {
				t.Fatalf("the compiled leg refused or trapped a program the reference prints %q: %v\n%s", r.want, err, r.src)
			}
			if got != r.want {
				t.Fatalf("printed %q, want %q — a membership answer changed, or the door raised where the reference answers\n%s", got, r.want, r.src)
			}
			onRecordIs(t, r.src, r.want)
		})
	}
}

// TestWhatTheMembershipDoorStillDeclines is the residue, pinned with today's bytes so it is a measurement
// and not a rumour. Each row names the row that owns it: these are the shapes whose kind is a RUN-TIME fact
// (a parameter, an arm, a copy, a pair) and the door has no promise to read — the honest answer today is the
// one the fold gave before this row, and the honest answer tomorrow is L11.1's tagged word arriving at the
// position (Gap R.146), never a guess from a name-keyed summary.
func TestWhatTheMembershipDoorStillDeclines(t *testing.T) {
	t.Parallel()
	t.Run("the shapes that answer False today, owned by L11.1 and Gap R.146", func(t *testing.T) {
		t.Parallel()
		rows := []struct{ name, src string }{
			{"a parameter's kind belongs to the call sites", "def g(v):\n    print(\"a\" in v)\n\ng(1.5)\n"},
			{"a name bound inside an arm", "x = [1]\nif 1 < 2:\n    x = 5\n\nprint(1 in x)\n"},
			{"a copy binding is an expression, not a spelled literal", "x = 5\ny = x\nprint(1 in y)\n"},
			{"a call that forwards its own parameter", "def f(v):\n    return v\n\nprint(1 in f(5))\n"},
			// The module `def` promises a number, and the program takes the promise back by writing the name
			// again. The door notices and says nothing — it does not raise and then explain the raise — so the
			// untagged fold answers `False` where the reference raises. Declining is the right shape for the
			// door; the ANSWER is L11.1's residue, beside the copy binding above it.
			{"a call the program re-points at another value", "def f():\n    return 5\n\nf = lambda: 5\n\nprint(1 in f())\n"},
			{"a body with two arms of two kinds", "def f():\n    if 1 < 2:\n        return 5\n\n    return [1]\n\nprint(1 in f())\n"},
			{"a builtin no door names an answer for", "print(1 in bool(1))\n"},
			{"a read written above its only binding", "def g():\n    print(1 in x)\n\nx = 5\n\ng()\n"},
			// The loop target is a binder too, so the promise retires — and the answer the fold then gives is
			// `False` where the reference raises, because the value the loop left behind is still an int. Same
			// class as the rows above it (the fold has no kind to read), same owner (L11.1's tagged word).
			{"a loop target leaves an int behind", "x = 5\nfor x in [1, 2]:\n    pass\n\nprint(1 in x)\n"},
		}
		for _, r := range rows {
			r := r
			t.Run(r.name, func(t *testing.T) {
				t.Parallel()
				got, err := compileAndRun(t, r.src)
				if err != nil {
					t.Skipf("this shape refuses today (%v), which is a different owed half\n%s", err, r.src)
				}
				if got != "False\n" {
					t.Fatalf("row is stale: the shape printed %q — the residue this row filed has moved, so move the row\n%s", got, r.src)
				}
			})
		}
	})
	t.Run("three shapes that refuse today, each on a row that is not this one's", func(t *testing.T) {
		t.Parallel()
		// These programs print a verdict in the reference and are refused HERE, for reasons that have nothing
		// to do with membership — a module slot that needs the tagged value word (L11.1, Gap R.48 / R.22), and
		// a list literal asked for an element kind a NAME does not hand it, in two element spellings
		// (Gap R.523). They are pinned here, with the refusal's own words, because a control table that claims
		// them would claim an answer neither leg gives; each row leaves this file when the row that owns it
		// pays — the tuple-unpacking row that used to sit here made exactly that move when **ADR 0454** paid
		// **Gap R.524**, and it is in `membershipControlRows` now.
		rows := []struct{ name, src, contains, owner string }{
			{
				"a module name another frame writes through global",
				"x = 5\ndef h():\n    global x\n\n    x = \"abc\"\n\nh()\nprint(\"b\" in x)\n",
				"`global x` names a module binding written as a text",
				"L11.1, Gap R.48 and Gap R.22",
			},
			{
				"a haystack the source builds from a name",
				"x = 5\nx = 5\nx += 1\nprint(1 in [x])\n",
				"list literal elements must be integers",
				"Gap R.523",
			},
			{
				// The same door, the other element kind: the reference prints `True` here, and the road answers
				// `list literal elements must be integers, not str` — the message names an element kind the
				// literal is not asking about, which is ADR 0166's second clause inside Gap R.523's first.
				"a haystack the source builds from a text",
				"y = \"a\"\nprint(\"a\" in [y])\n",
				"list literal elements must be integers",
				"Gap R.523",
			},
		}
		for _, r := range rows {
			r := r
			t.Run(r.name, func(t *testing.T) {
				t.Parallel()
				_, err := RunSnippet(r.src)
				if err == nil {
					t.Fatalf("the shape compiles today — the row that owns it (%s) has paid, so move this row to the control table\n%s", r.owner, r.src)
				}
				if !strings.Contains(err.Error(), r.contains) {
					t.Fatalf("it failed for another reason (%v); the row pins the refusal %s owns\n%s", err, r.owner, r.src)
				}
			})
		}
	})
	t.Run("a value that arrives as a pair is refused in words, not answered", func(t *testing.T) {
		t.Parallel()
		src := "xs = [1]\nx = xs[0]\nprint(1 in x)\n"
		if _, err := RunSnippet(src); err == nil {
			t.Fatalf("the pair position answered a membership question this backend has no word for\n%s", src)
		}
	})
}

// TestTheMembershipDoorEmitsARaiseAndAsksNoContainer is the IR claim: for a promised kind the door replaces
// the runtime container question rather than answering beside it, and the raise it writes is the non-local
// kind an `except` reaches. The negative half is the soundness claim in the module's own bytes: a haystack
// the door declines must not carry the sentence at all.
func TestTheMembershipDoorEmitsARaiseAndAsksNoContainer(t *testing.T) {
	t.Parallel()
	t.Run("a name the program settles asks no container door", func(t *testing.T) {
		t.Parallel()
		res, err := Compile("x = 5\nprint(1 in x)\n")
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		main := inMainBody(t, res.IR)
		if strings.Contains(main, "@rt_contains") {
			t.Errorf("@main still asks a runtime container door about a value the program settled:\n%s", main)
		}
		if !strings.Contains(main, "@exn_flag") {
			t.Errorf("@main carries no non-local raise, so the trap is not catchable:\n%s", main)
		}
		if !strings.Contains(res.IR, "argument of type 'int' is not iterable") {
			t.Errorf("the module does not carry the reference's sentence")
		}
	})
	t.Run("a call every road of settles asks no container door", func(t *testing.T) {
		t.Parallel()
		res, err := Compile("def f():\n    return 1.5\n\nprint(\"a\" in f())\n")
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if main := inMainBody(t, res.IR); strings.Contains(main, "@rt_contains") {
			t.Errorf("the float answer still reaches the untagged container scan, which is the shape llc rejected at exit 2:\n%s", main)
		}
	})
	t.Run("a haystack the door declines carries no sentence", func(t *testing.T) {
		t.Parallel()
		for _, src := range []string{
			"x = \"abc\"\nprint(\"b\" in x)\n",
			"x = [1, 2]\nprint(1 in x)\n",
			"x = 5\ny = x\nprint(1 in y)\n",
		} {
			res, err := Compile(src)
			if err != nil {
				t.Fatalf("compile: %v\n%s", err, src)
			}
			// @main is the program; the runtime block below it carries these sentences for the doors that
			// ask a TAG, and that is not the claim being made here.
			if strings.Contains(inMainBody(t, res.IR), "argument of type") {
				t.Errorf("the membership raise is in a program the reference answers:\n%s", src)
			}
		}
	})
}

// TestTheMembershipPromiseDoorIsAskedAsAFunction asks the gate directly, as the decisions it makes rather
// than as programs: a widening (a second binder counted as one) or a narrowing (an arm binding retired by a
// scan that forgot it) fails a row here before it can fail a program in the field. This is the pattern
// `numeric_slot_arith_test.go` and `pair_call_test.go` established for the other doors.
func TestTheMembershipPromiseDoorIsAskedAsAFunction(t *testing.T) {
	t.Parallel()

	kindOf := func(t *testing.T, src, name string, readLine int) (string, bool) {
		t.Helper()
		prog := mustParseMembership(t, src)
		g := &irGen{
			spelledScalars: scanSpelledScalars(nil, prog.Stmts, nil, programGlobalWrites(prog.Stmts)),
			callKinds:      callLiteralAnswerKinds(prog.Stmts),
			assignedNames:  assignedNamesEverywhere(prog.Stmts),
		}
		return g.spelledScalarKindName(name, readLine)
	}

	t.Run("a name's promise, per program", func(t *testing.T) {
		t.Parallel()
		rows := []struct {
			name string
			src  string
			bind string
			line int
			want string // "" means: the door must decline
		}{
			{"one spelled binding", "x = 5\nprint(1 in x)\n", "x", 2, "int"},
			{"the void", "x = None\nprint(1 in x)\n", "x", 2, "NoneType"},
			{"a double", "x = 1.5\nprint(1 in x)\n", "x", 2, "float"},
			{"a verdict", "x = True\nprint(1 in x)\n", "x", 2, "bool"},
			{"a text is not a no-member kind", "x = \"abc\"\nprint(\"b\" in x)\n", "x", 2, ""},
			{"a container is not a no-member kind", "x = [1]\nprint(1 in x)\n", "x", 2, ""},
			{"a second binding retires it", "x = 5\nx = 6\nprint(1 in x)\n", "x", 3, ""},
			{"a binding in an arm retires it", "x = 5\nif c:\n    x = 6\n\nprint(1 in x)\n", "x", 5, ""},
			{"a loop target retires it", "for x in [1, 2]:\n    pass\n\nprint(1 in x)\n", "x", 4, ""},
			{"a comprehension target retires it", "ys = [x for x in [1]]\nprint(1 in x)\n", "x", 2, ""},
			{"an unpack retires it", "x, y = 1, 2\nprint(1 in x)\n", "x", 2, ""},
			{"an annotated binding is not a plain one", "x: int = 5\nprint(1 in x)\n", "x", 2, ""},
			{"a read above the binding is not covered", "print(1 in x)\nx = 5\n", "x", 1, ""},
			{"a read on the binding's own line is not covered", "x = 5\nprint(1 in x)\n", "x", 1, ""},
			{"a name another frame writes is not this frame's", "x = 5\ndef h():\n    global x\n\n    x = \"abc\"\n\nh()\nprint(\"b\" in x)\n", "x", 7, ""},
			{"another frame's local of the same spelling does not retire it", "x = 5\ndef h():\n    x = \"abc\"\n\n    return x\n\nprint(1 in x)\n", "x", 6, "int"},
			{"a parameter does not retire the module's name", "def f(x):\n    return x\n\nx = 5\nprint(1 in x)\n", "x", 5, "int"},
		}
		for _, r := range rows {
			r := r
			t.Run(r.name, func(t *testing.T) {
				t.Parallel()
				got, ok := kindOf(t, r.src, r.bind, r.line)
				if r.want == "" {
					if ok {
						t.Fatalf("the door claims %q for %q, and a promise here is a raise over a program the reference runs\n%s", got, r.bind, r.src)
					}
					return
				}
				if !ok || got != r.want {
					t.Fatalf("the door says (%q, %v) for %q, want (%q, true)\n%s", got, ok, r.bind, r.want, r.src)
				}
			})
		}
	})

	t.Run("a call's answer kind, per body", func(t *testing.T) {
		t.Parallel()
		rows := []struct {
			name string
			src  string
			fn   string
			want string
		}{
			{"every road answers a number", "def f():\n    return 5\n", "f", "int"},
			{"two roads, one kind", "def f(c):\n    if c:\n        return 1\n\n    return 2\n", "f", "int"},
			{"no road at all answers the void", "def f():\n    print(\"x\")\n", "f", "NoneType"},
			{"a bare return answers the void", "def f():\n    return\n", "f", "NoneType"},
			{"a road that answers a double", "def f():\n    return 1.5\n", "f", "float"},
			{"two kinds is no kind", "def f(c):\n    if c:\n        return 1\n\n    return None\n", "f", ""},
			{"a road the source does not spell", "def f(v):\n    return v\n", "f", ""},
			{"a text answers membership, so it is not this door's", "def f():\n    return \"abc\"\n", "f", ""},
			{"a container answers membership, so it is not this door's", "def f():\n    return [1]\n", "f", ""},
			{"a body that can fall off its end has two answers", "def f(c):\n    if c:\n        return 1\n", "f", ""},
			{"a generator answers an accumulator, not a number", "def f():\n    yield 1\n", "f", ""},
		}
		for _, r := range rows {
			r := r
			t.Run(r.name, func(t *testing.T) {
				t.Parallel()
				prog := mustParseMembership(t, r.src)
				kinds := callLiteralAnswerKinds(prog.Stmts)
				got, ok := kinds[r.fn]
				if r.want == "" {
					if ok {
						t.Fatalf("the table names %q for %q, and a kind here is a raise over a program the reference prints\n%s", got, r.fn, r.src)
					}
					return
				}
				if !ok || got != r.want {
					t.Fatalf("the table says (%q, %v) for %q, want (%q, true)\n%s", got, ok, r.fn, r.want, r.src)
				}
			})
		}
	})

	t.Run("the set of kinds is the runtime table's set", func(t *testing.T) {
		t.Parallel()
		got := []string{}
		for _, want := range []string{"int", "float", "bool", "NoneType", "str", "list", "dict", "set", "object"} {
			if notIterableKind(want) {
				got = append(got, want)
			}
		}
		if strings.Join(got, " ") != "int float bool NoneType" {
			t.Fatalf("the door's set reads %v — it must be exactly the table the runtime road walks (ADR 0232)", got)
		}
	})
}

// mustParseMembership is the parse these gate rows need: the door reads the AST, so the rows are written as
// source and the scan is asked the question the emitter will ask it.
func mustParseMembership(t *testing.T, src string) *Program {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, src)
	}
	return prog
}

// TestTheMembershipPromiseRowsAreTheRowsTheRowClaims pins the shape of the tables themselves: the four
// kinds, the two doors, and the fact that the compiled leg and the record leg are asked about the SAME
// sources. A row silently dropped from `membershipPromiseRows` is a claim the commit message made and the
// suite did not.
func TestTheMembershipPromiseRowsAreTheRowsTheRowClaims(t *testing.T) {
	t.Parallel()
	kinds := map[string]int{}
	for _, r := range membershipPromiseRows() {
		kinds[r.kind]++
	}
	for _, want := range []string{"int", "float", "bool", "NoneType"} {
		if kinds[want] == 0 {
			t.Fatalf("no row covers the %q haystack — the door's four kinds are CPython's, not this file's", want)
		}
	}
	if n := len(membershipPromiseRows()); n < 18 {
		t.Fatalf("the promise table shrank to %d rows; the residue it covers grows, it does not shrink", n)
	}
}

// membershipCatchableRows is the recorder's list as well as the case's table: every source it holds is asserted
// against the record leg, so it is a source the record must hold (ADR 0302).
func membershipCatchableRows() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{
			"the handler runs instead of the print",
			"x = 5\ntry:\n    print(1 in x)\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"the exception names its type, as the reference's does",
			"x = 1.5\ntry:\n    print(\"a\" in x)\nexcept TypeError as e:\n    print(str(e))\n",
			"argument of type 'float' is not iterable\n",
		},
		{
			"a call's raise is catchable too",
			"def f():\n    return 5\n\ntry:\n    print(1 in f())\nexcept TypeError as e:\n    print(str(e))\n",
			"argument of type 'int' is not iterable\n",
		},
		{
			"a caught raise leaves the program running",
			"hits = 0\nx = 5\nfor k in [1, 2]:\n    try:\n        if 1 in x:\n            hits = hits + 1\n\n    except TypeError:\n        hits = hits + 1\n\nprint(hits)\n",
			"2\n",
		},
	}
}

// membershipControlRows is the recorder's list as well as the case's table: every source it holds is asserted
// against the record leg, so it is a source the record must hold (ADR 0302).
func membershipControlRows() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{
			"the arm that did not run would have changed the kind",
			"x = [1]\nif 1 > 2:\n    x = 5\n\nprint(1 in x)\n",
			"True\n",
		},
		{
			"two bindings, one name: no promise",
			"x = 5\nx = [1]\nprint(1 in x)\n",
			"True\n",
		},
		{
			"the body binds its own name of the same spelling",
			"x = 5\ndef g():\n    x = [1, 2]\n\n    return 1 in x\n\nprint(g())\n",
			"True\n",
		},
		// The tuple-unpacking target that used to sit in the refusal table below belongs here again: its module
		// was one `llc` rejected (**Gap R.524**), and **ADR 0454** paid it by handing the element to the plain
		// binding road. The unpacking binds the object, the name keeps the container's claims, and membership
		// asks the object the way it always has — so the source is an ordinary control row, recorded and all.
		{
			"an unpacking that binds the container the source spells",
			"x = 5\nx, y = [1], 2\nprint(1 in x)\n",
			"True\n",
		},
		// A module name another frame writes through `global` prints a verdict in the reference and is refused
		// here for reasons that are not membership's. It stays pinned, with its refusal words and the row that
		// owns it, in `TestWhatTheMembershipDoorStillDeclines`; a control table may only hold a source whose
		// answer both legs give, because the recorder reads this list and the record has no entry for a program
		// that never ran (ADR 0302).
		//
		// The ordinary answers, untouched by the gate.
		{"a list", "x = [1, 2]\nprint(1 in x)\n", "True\n"},
		{"a text", "x = \"hello\"\nprint(\"ell\" in x)\n", "True\n"},
		{"a text, absent", "x = \"hello\"\nprint(\"zz\" in x)\n", "False\n"},
		{"a dict's keys", "d = {\"a\": 1}\nprint(\"a\" in d)\n", "True\n"},
		{"a set", "s = {1, 2}\nprint(1 in s)\n", "True\n"},
		{"a tuple", "t = (1, 2)\nprint(1 in t)\n", "True\n"},
		{"a container the run time built", "xs = []\nxs.append(3)\nprint(3 in xs)\n", "True\n"},
		{"a name bound to a container a body handed back", "def g():\n    x = [1]\n\n    return x\n\nprint(1 in g())\n", "True\n"},
		{"a number asked of a container, not of a name", "x = 5\nprint(x in [5])\n", "True\n"},
	}
}

// TestTheRecordSourcesThisRowAsksFor is the recorder, not a case: with GUSTY_RECORD_MEMBERSHIP set it writes
// every source the tables in this file ask about to the file that variable names, in the JSON shape
// `tools/recmerge` reads, and skips. Without it, it skips — a case that wrote a file every run would let two
// parallel runs overwrite each other's ledger, which is why the artifact-writing modes stay single-process
// (`docs/operations.md`, and `golden.go`'s own `GUSTY_GOLDEN_MISSING` is the same shape).
//
// The list lives here rather than being retyped in a scratch program because these are the sources the cases
// actually ask: a recorder that drifts from its own table is the thing that lets a row ship with no record
// under it (ADR 0302).
func TestTheRecordSourcesThisRowAsksFor(t *testing.T) {
	out := os.Getenv("GUSTY_RECORD_MEMBERSHIP")
	if out == "" {
		t.Skip("the recorder: set GUSTY_RECORD_MEMBERSHIP=<file> to dump these sources for tools/recmerge")
	}
	var srcs []string
	add := func(s string) {
		for _, have := range srcs {
			if have == s {
				return
			}
		}
		srcs = append(srcs, s)
	}
	for _, r := range membershipPromiseRows() {
		add(r.src)
	}
	for _, r := range membershipCatchableRows() {
		add(r.src)
	}
	for _, r := range membershipControlRows() {
		add(r.src)
	}
	body, merr := marshalGoldenMissing(srcs)
	if merr != nil {
		t.Fatalf("rendering the source list: %v", merr)
	}
	if err := os.WriteFile(out, append(body, '\n'), 0o644); err != nil {
		t.Fatalf("writing %s: %v", out, err)
	}
	t.Logf("wrote %d sources to %s", len(srcs), out)
}

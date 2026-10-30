//go:build llvm20

package lang

import (
	"strings"
	"testing"
)

// ternary_pair_arg_test.go — an ANSWER whose kind the compiler knows only from a TABLE crosses a call
// boundary, and a `return` whose arm is a container literal arrives in a word the `phi` can name. Roadmap
// **Gap R.581** (the call-argument position) and **Gap R.584** (the folded global in the merge), **ADR 0475**.
//
// Both halves started the same way: a program `python3` prints, compiled, and answered by `llc` with
// **exit 2** — the code the contract reserves for a compiler bug (**ADR 0166**).
//
// The float half. `def f(x):` / `    return 1.5` is emitted `define double` (ADR 0274's promotion, and ADR
// 0357's for `return 1.5 if x else 2.5`), and handing that answer to `def g(v):` / `    return v` wrote
//
//	%t13 = call double @gy_f(double %t12)
//	%t16 = call i32 @gy_g(i32 %t13)      llc: '%t13' defined with type 'double' but expected 'i32'
//
// The pair road already had the answer — a boxed double is a payload plus the tag that says so — and the call
// argument was the one position that had not been asked to carry it. The box is `@rt_float_new`, the same one
// a float that changes its variable's mind takes (`floatbind.go`), asked of the SAME emitter so a slot, an
// argument and a container element can never disagree about what a boxed double looks like (**ADR 0232**).
//
// The container half. `def f(x):` / `    return [1, 2] if x else 0` folds its arm into the compile-time global
// `@.lstN`, and the ternary's merge is a typed `phi`, so the module carried
//
//	%t2 = phi i32 [ @.lst1, %tern.thenfwd3 ], [ 0, %tern.elsefwd4 ]   llc: global variable reference must have
//	                                                                   pointer type
//
// The arm now takes the heap copy an ARGUMENT of that shape has taken since Gap J.5, emitted in the arm's own
// block before the forwarding branch so the handle dominates the merge that names it.
//
// What this row does NOT claim is written down below with both legs' answers: the kinds that need a TAG at the
// call boundary — a text, a verdict, a `None`, a tuple — still arrive as a payload and print the number the
// payload happens to be. That is **Gap R.581**'s open remainder, and a table of wrong answers kept in the
// light is the reason the next cycle cannot mistake them for passing.

// ternaryPairArgRows is the family the two doors answer. Each row is asked of the compiled leg, of the
// reference, and — in the record-leg case — of the record.
var ternaryPairArgRows = []struct{ src, want string }{
	// The float road's answer, handed to a parameter: the row's own program and its print-in-the-callee,
	// two-defs and no-parameter twins.
	{"def f(x):\n    return 1.5\n\ndef g(v):\n    return v\n\nprint(g(f(1)))", "1.5"},
	{"def f(x):\n    return 1.5\n\ndef g(v):\n    print(v)\n\ng(f(1))", "1.5"},
	{"def f(x):\n    return 1.5\n\ndef g(v):\n    return v\n\ndef h(w):\n    return g(w)\n\nprint(h(f(1)))", "1.5"},
	{"def f():\n    return 2.5\n\ndef g(v):\n    return v\n\nprint(g(f()))", "2.5"},
	{"def f(x):\n    return 1.5\n\ndef g(v):\n    return v\n\nprint(g(f(1)), g(f(0)))", "1.5 1.5"},
	{"def f(x):\n    return 1.5\n\ndef g(v, k):\n    return v\n\nprint(g(f(1), 3))", "1.5"},
	// ADR 0357's road: the two arms agree on being doubles, so the body is `define double` and the argument
	// needs the box on both roads over there.
	{"def f(x):\n    return 1.5 if x else 2.5\n\ndef g(v):\n    return v\n\nprint(g(f(1)), g(f(0)))", "1.5 2.5"},
	// The container arm of a `return`, in every kind the fold writes a global for: list, comprehension,
	// dict, set, and the nested list whose elements are themselves handles.
	{"def f(x):\n    return [1, 2] if x else 0\n\nw = f(1)\nprint(w)", "[1, 2]"},
	{"def f(x):\n    return [1, 2] if x else 0\n\nprint(f(0))", "0"},
	{"def f(x):\n    return [i * 2 for i in [1, 2]] if x else 0\n\nw = f(1)\nprint(w)", "[2, 4]"},
	{"def f(x):\n    return [[1, 2], [3]] if x else 0\n\nw = f(1)\nprint(w)", "[[1, 2], [3]]"},
	{"def f(x):\n    return {\"a\": 1} if x else 0\n\nw = f(1)\nprint(w)", "{'a': 1}"},
	{"def f(x):\n    return {\"k\": 1.5} if x else 0\n\nw = f(1)\nprint(w)", "{'k': 1.5}"},
	{"def f(x):\n    return {1, 2} if x else 0\n\nw = f(1)\nprint(w)", "{1, 2}"},
	{"def f(x):\n    return {1, 2} if x else 0\n\nprint(f(0))", "0"},
	// The arm the test does NOT take keeps the answer it always had, and the int side of a container arm is
	// still an int — the copy is asked of the arm that needs it, not of both.
	{"def f(x):\n    return [1, 2] if x else 0\n\ndef g(v):\n    return v\n\nprint(g(f(0)))", "0"},
	{"def f(x):\n    return {1, 2} if x else 7\n\nprint(f(0) + 1)", "8"},
}

// TestTheTernaryArmAndTheFloatAnswerCrossTheCallBoundary asks every answered row of the compiled leg and of
// the reference in the same case, so a row cannot quietly stop being CPython's answer.
func TestTheTernaryArmAndTheFloatAnswerCrossTheCallBoundary(t *testing.T) {
	t.Parallel()
	for _, tt := range ternaryPairArgRows {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			got := strings.TrimRight(compiledOut(t, tt.src), "\n")
			if got != tt.want {
				t.Fatalf("compiled printed %q, want %q", got, tt.want)
			}
			want, _, err := PythonRun(tt.src + "\n")
			if err != nil {
				t.Fatalf("the reference could not run the program: %v", err)
			}
			if strings.TrimRight(want, "\n") != tt.want {
				t.Fatalf("this row's expectation has drifted from the reference: CPython prints %q, the row says %q",
					strings.TrimRight(want, "\n"), tt.want)
			}
		})
	}
}

// TestTheTernaryPairArgFamilyIsJudgedOnTheRecordLeg: every answered source has an answer ON RECORD, so a later
// cycle cannot take the door away without taking the evidence away with it (**ADR 0302**'s rule that a missing
// record fails the case rather than passing quietly).
func TestTheTernaryPairArgFamilyIsJudgedOnTheRecordLeg(t *testing.T) {
	t.Parallel()
	for _, tt := range ternaryPairArgRows {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			rec := goldenLookup(t, tt.src)
			if rec.Repr == "" && rec.Stdout == "" && rec.Err == "" {
				t.Fatalf("%q has no recorded answer: re-record it (tools/recmerge) before shipping the door that reads it", tt.src)
			}
			if out := goldenStdout(t, tt.src); strings.TrimRight(out, "\n") != tt.want {
				t.Fatalf("the record answers %q, the compiled leg answers %q", strings.TrimRight(out, "\n"), tt.want)
			}
		})
	}
}

// TestTheTernaryMergeCarriesNoFoldedGlobal is the IR shape the exit-2 half turns on. The merge is a typed
// `phi`; a compile-time global is an ARRAY, and the module that put one in an `i32` operand is the module
// `llc` refused. The positive half asserts the arm arrives through the allocator instead; the negative half
// keeps the fold, which is a real optimization this door must not delete (**ADR 0192**'s rule that the fix
// reach only the position that asked).
func TestTheTernaryMergeCarriesNoFoldedGlobal(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"def f(x):\n    return [1, 2] if x else 0\n\nw = f(1)\nprint(w)",
		"def f(x):\n    return {1, 2} if x else 0\n\nw = f(1)\nprint(w)",
		"def f(x):\n    return {\"a\": 1} if x else 0\n\nw = f(1)\nprint(w)",
		"def f(x):\n    return [i * 2 for i in [1, 2]] if x else 0\n\nw = f(1)\nprint(w)",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			mod := emittedModuleIR(t, src)
			for _, ln := range strings.Split(mod, "\n") {
				if strings.Contains(ln, "= phi i32") && strings.Contains(ln, " @.") {
					t.Fatalf("a compile-time global sits in an i32 phi — the sentence `llc` answers with exit 2:\n%s", ln)
				}
			}
			if !strings.Contains(mod, "call i32 @rt_alloc(") {
				t.Fatalf("the container arm never reached the heap; the merge has nothing to name:\n%s", mainBodyOf(mod))
			}
		})
	}
	// The fold survives where it was always right: a comprehension that is not a ternary arm keeps its
	// compile-time global, and a literal the fold answers outright still answers without the heap.
	t.Run("the comprehension fold is untouched", func(t *testing.T) {
		t.Parallel()
		mod := emittedModuleIR(t, "w = [i * 2 for i in [1, 2]]\nprint(w[0])")
		if !strings.Contains(mod, "@.lst") {
			t.Fatalf("the fold was dropped where it is the right road:\n%s", mainBodyOf(mod))
		}
	})
	t.Run("the folded literal read is untouched", func(t *testing.T) {
		t.Parallel()
		if got := strings.TrimSpace(compiledOut(t, "print(len([1, 2, 3]))")); got != "3" {
			t.Fatalf("a literal the fold answers now answers %q", got)
		}
	})
}

// TestTheFloatAnswerArrivesBoxedBesideItsTag is the same witness for the float half: the callee's parameter
// is TWO words, the argument is a box the float door owns, and the tag is the constant that says which kind
// the payload is. Delete the box and the row is back to `'%t13' defined with type 'double' but expected 'i32'`.
func TestTheFloatAnswerArrivesBoxedBesideItsTag(t *testing.T) {
	t.Parallel()
	mod := emittedModuleIR(t, "def f(x):\n    return 1.5\n\ndef g(v):\n    return v\n\nprint(g(f(1)))")
	body := mainBodyOf(mod)
	if !strings.Contains(mod, "define i32 @gy_g(i32 %p0, i32 %q0)") {
		t.Fatalf("the callee's parameter did not arrive as a (payload, tag) pair:\n%s", body)
	}
	if !strings.Contains(body, "call i32 @rt_float_new(double") {
		t.Fatalf("the float answer was not boxed by the door a rebound float variable uses:\n%s", body)
	}
	call := body[strings.Index(body, "call i32 @gy_g("):]
	args := call[strings.Index(call, "(")+1:]
	args = args[:strings.Index(args, ")")]
	fields := strings.Split(args, ", ")
	if len(fields) != 2 || !strings.HasPrefix(fields[0], "i32 %") || fields[1] != "i32 1" {
		t.Fatalf("the call hands (%s) — the box and its tag are the two words this row owes", args)
	}
	// And the same for ADR 0357's road, where the DOUBLE arrives from a phi rather than from a constant.
	mod2 := emittedModuleIR(t, "def f(x):\n    return 1.5 if x else 2.5\n\ndef g(v):\n    return v\n\nprint(g(f(1)))")
	if !strings.Contains(mod2, "call i32 @rt_float_new(double") {
		t.Fatalf("the double-answer's arm was handed over unboxed:\n%s", mainBodyOf(mod2))
	}
}

// TestTheCallArgumentDoorLeavesTheProvenKindsAlone is the negative control the two doors must keep: a parameter
// every call site hands an integer keeps the ONE word it always crossed in with, and a body the float return
// road owns is not reopened by a door that cannot give it a tag. Without these rows the scan could widen every
// call in the program into a pair — more words, more registers, and no answer the language needed (**ADR
// 0389**'s split, **ADR 0273**'s convention).
func TestTheCallArgumentDoorLeavesTheProvenKindsAlone(t *testing.T) {
	t.Parallel()
	mod := emittedModuleIR(t, "def g(v):\n    return v\n\nprint(g(3), g(4))")
	if strings.Contains(mod, "define i32 @gy_g(i32 %p0, i32 %q0)") {
		t.Fatalf("an int-only parameter was widened into a pair:\n%s", mainBodyOf(mod))
	}
	// A float-returning body keeps ADR 0274's word: the pair road stands aside for a body whose `ret` is a
	// double, which is the exclusion the scan asks before opening anything (Gap R.344 owns that answer word).
	mod2 := emittedModuleIR(t, "def add(v):\n    return v + 0.5\n\nprint(add(1))")
	if strings.Contains(mainBodyOf(mod2), "@rt_float_new") && strings.Contains(mod2, "define i32 @gy_add(i32 %p0, i32 %q0)") {
		t.Fatalf("the pair road claimed a body the float return owns:\n%s", mainBodyOf(mod2))
	}
}

// TestWhatTheCallArgumentDoorStillCannotAnswerIsFiledNotFixed keeps the row's remainder in the light. Each row
// names the gap that owns it, and each is measured HERE against both legs rather than remembered: the compiled
// answer, the reference's answer, and the exit codes. A kind that needs a TAG at the boundary still arrives as
// a payload alone — printing the number the payload happens to be is the wrong answer this table exists to
// keep visible (roadmap **Gap R.581**, and **Gap R.585** for the tuple arm the fold does not cover).
func TestWhatTheCallArgumentDoorStillCannotAnswerIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name, src, compiled, reference, road string
	}{
		{
			name:      "a text the callee hands back prints the interned index the payload is",
			src:       "def f(x):\n    return \"x\" if x else 0\n\ndef g(v):\n    return v\n\nprint(g(f(1)))",
			compiled:  "0",
			reference: "x",
			road:      "the tag has to cross the parameter with the payload — roadmap Gap R.581, Gap R.324",
		},
		{
			name:      "a verdict the callee hands back prints 1",
			src:       "def f(x):\n    return True if x else 0\n\ndef g(v):\n    return v\n\nprint(g(f(1)))",
			compiled:  "1",
			reference: "True",
			road:      "the tag has to cross the parameter with the payload — roadmap Gap R.581",
		},
		{
			name:      "a None the callee hands back prints 0",
			src:       "def f(x):\n    return None if x else 0\n\ndef g(v):\n    return v\n\nprint(g(f(1)))",
			compiled:  "0",
			reference: "None",
			road:      "the tag has to cross the parameter with the payload — roadmap Gap R.581",
		},
		{
			name:      "a container the callee hands back prints its heap slot",
			src:       "def f(x):\n    return [1, 2] if x else 0\n\ndef g(v):\n    return v\n\nprint(g(f(1)))",
			compiled:  "0",
			reference: "[1, 2]",
			road:      "the tag has to cross the parameter with the payload — roadmap Gap R.581, Gap R.67",
		},
		{
			name:      "a tuple arm of a `return` answers the int road even where the list arm now answers",
			src:       "def f(x):\n    return (1, 2) if x else 0\n\nw = f(1)\nprint(w)",
			compiled:  "0",
			reference: "(1, 2)",
			road:      "the tuple's fold is not the list fold — roadmap Gap R.585",
		},
		{
			name:      "a forwarded text inside a list prints the payload",
			src:       "def f(x):\n    return \"x\" if x else 0\n\ndef g(v):\n    return v\n\nprint([g(f(1))])",
			compiled:  "[0]",
			reference: "['x']",
			road:      "the element door needs the tag the argument dropped — roadmap Gap R.581, Gap R.220",
		},
		{
			name:      "str() of a forwarded answer refuses through the render table",
			src:       "def f(x):\n    return \"x\" if x else 0\n\ndef g(v):\n    return v\n\nprint(str(g(f(1))))",
			compiled:  "str(g(f(1))) is refused",
			reference: "x",
			road:      "the one str/repr table has no word for this kind — roadmap Gap R.171",
		},
		{
			name:      "len() of a forwarded answer refuses through the literal door",
			src:       "def f(x):\n    return \"abc\" if x else 0\n\ndef g(v):\n    return v\n\nprint(len(g(f(1))))",
			compiled:  "len requires an inline list/dict/set literal",
			reference: "3",
			road:      "the measure door reads a literal, not an answer — roadmap Gap R.324",
		},
		{
			name:      "the arms of a `return` that do not agree on a word still refuse",
			src:       "def f(x):\n    return 2.0 / 1 if x else 0\n\ndef g(v):\n    return v\n\nprint(g(f(1)))",
			compiled:  "the two arms of `2.0 / 1 if x else 0` do not agree on a word",
			reference: "2.0",
			road:      "ADR 0166's refusal, asked of the arms before the call — roadmap Gap R.344, Gap R.102",
		},
		{
			name:      "a forwarded float answer read by a condition keeps one word and refuses",
			src:       "def f(x):\n    return 2.5\n\ndef g(v):\n    return v\n\nif g(f(1)) > 2.0:\n    print(\"big\")",
			compiled:  "hands back the (payload, tag) pair its answer arrived in",
			reference: "big",
			road:      "the condition needs the tag beside the payload — roadmap Gap R.146, Gap R.581 (this row was exit 2 before the argument was boxed: the refusal is ADR 0166's ladder, not an answer)",
		},
	}
	for _, tt := range rows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, _ := compiledOutOrRefusal(t, tt.src)
			got = strings.TrimSpace(got)
			if !strings.Contains(got, tt.compiled) {
				t.Fatalf("the compiled leg answers %q; this filed row records %q — the row moves to the answered table when the door opens, and to nothing else",
					got, tt.compiled)
			}
			want, _, err := PythonRun(tt.src + "\n")
			if err != nil {
				t.Fatalf("the reference could not run the program: %v", err)
			}
			if ref := strings.TrimSpace(want); !strings.Contains(ref, tt.reference) {
				t.Fatalf("the reference answers %q; this row records %q", ref, tt.reference)
			}
			if !strings.Contains(tt.road, "Gap") {
				t.Fatalf("filed row %q names no roadmap row", tt.name)
			}
		})
	}
}

// TestTheTernaryPairArgTableCoversTheRowItself keeps the tables' size honest — a case deleted from the answered
// table without its record entry going with it is the failure this file exists to prevent.
func TestTheTernaryPairArgTableCoversTheRowItself(t *testing.T) {
	t.Parallel()
	if len(ternaryPairArgRows) < 16 {
		t.Fatalf("the answered table has %d rows; this row's family is wider than that", len(ternaryPairArgRows))
	}
	kinds := map[string]bool{}
	for _, tt := range ternaryPairArgRows {
		switch {
		case strings.Contains(tt.src, "1.5"), strings.Contains(tt.src, "2.5"):
			kinds["float"] = true
		case strings.Contains(tt.src, "["):
			kinds["list"] = true
		case strings.Contains(tt.src, "{"):
			kinds["container"] = true
		}
	}
	for _, k := range []string{"float", "list", "container"} {
		if !kinds[k] {
			t.Fatalf("no %s row in the answered table", k)
		}
	}
}

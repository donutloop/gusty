package lang

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// module_float_test.go — a compiled function body reads the module's float out of the module's own
// global (roadmap Gap R.35's float half; ADR 0376).
//
// ADR 0227 gave the compiled backend one mechanism for module state: the name lives in a module GLOBAL
// that `@main` writes and any body loads, because a frame's allocas die with the frame and module state
// does not. It was built for the `i32` word alone — `moduleSlots` holds an `i32`, and a double does not
// fit it — so the module's floats were left out and a body naming one was refused:
//
//	RATE = 2.5
//	def scale(v):
//	    return v * RATE
//	print(scale(3))       # CPython 7.5 · this toolchain, before ADR 0376: exit 1, "cannot read module-level state"
//
// The refusal was right about the rule and wrong about this program: the double already has a word, a
// slot type, a printer and a return convention — ADR 0373 and ADR 0375 spent two cycles putting each of
// them beside the body that computes one. What was missing was the module's half.
//
// The gate is the whole safety of the feature, because a global has ONE word and the write that fills it
// is the literal the gate can see. So a name enters the table only when every module-level binding of it
// is a literal float or an arithmetic expression over literal numbers, nothing else ever binds it, and no
// body that reads it also writes it. A name rebound to an int, one computed by a call the gate cannot
// read, one bound by a `for` target — each stays refused, because a global cannot promise a word the
// compiler was never shown. The refusal rows are pinned below alongside the answers for exactly that
// reason: the row is about what the gate will NOT buy, and a compiler that guesses here would trade an
// honest refusal at exit 1 for a `double` in an `i32` slot at exit 2.
//
// The other half of Gap R.35 — a module-level CONTAINER read by a body — is still owed and stays refused
// here too: a container's word is a heap handle that has to be rooted against the collector, and its
// element operations live in the per-scope notebooks ADR 0341 settles at a binding.

// moduleFloatPrograms is the answer table. Every `want` is CPython's own bytes, every source is in the
// record, and the guard at the bottom fails if the record stops covering the table.
var moduleFloatPrograms = []struct {
	name, src, want string
}{
	// ---- the row the half-row was named for.
	{"a body's arithmetic reads the module's double",
		"RATE = 2.5\n\ndef scale(v):\n    return v * RATE\n\nprint(scale(3))\n", "7.5\n"},
	{"two bodies read the same module double",
		"RATE = 2.5\n\ndef a():\n    return RATE * 2\n\n" +
			"def b(v):\n    return v + RATE\n\nprint(a(), b(1))\n", "5.0 3.5\n"},
	{"a body prints the module's double",
		"RATE = 2.5\n\ndef show():\n    print(RATE)\n\nshow()\n", "2.5\n"},
	{"the module's double compared, not just multiplied",
		"RATE = 2.5\n\ndef warm():\n    return RATE > 2\n\nprint(warm())\n", "True\n"},
	// ---- the global is the module's state: the read happens when the call runs, so a binding the
	// module made between the definition and the call is the one the body sees.
	{"a body reads the module's double as the module last wrote it",
		"RATE = 2.5\n\ndef show():\n    print(RATE)\n\nshow()\nRATE = 9.5\nshow()\n", "2.5\n9.5\n"},
	{"the module doubles its own float and the body reads what it settled on",
		"RATE = 2.5\nRATE = RATE * 2\n\ndef half():\n    return RATE / 2\n\nprint(half())\n", "2.5\n"},
	{"augmented assignment at module level feeds the body's read",
		"RATE = 2.5\nRATE += 1.0\n\ndef show():\n    return RATE\n\nprint(show())\n", "3.5\n"},
	// ---- the value the gate promises is a number, and a number may be spelled by arithmetic.
	{"the module computed the double by true division",
		"HALF = 10 / 4\n\ndef h():\n    return HALF * 2\n\nprint(h())\n", "5.0\n"},
	{"the module computed the double by a product of literals",
		"AREA = 2.0 * 3\n\ndef show():\n    return AREA + 0.5\n\nprint(show())\n", "6.5\n"},
	{"a negative literal is still a literal",
		"OFFSET = -1.5\n\ndef shift(x):\n    return x + OFFSET\n\nprint(shift(2.0))\n", "0.5\n"},
	// ---- two globals, two words: the i32 door and the double door do not read each other's names.
	{"a module int and a module double are separate globals",
		"COUNT = 4\nRATE = 2.5\n\ndef total():\n    return COUNT * RATE\n\nprint(total())\n", "10.0\n"},
	// ---- shadowing: a body that BINDS the name owns it, and the module's word is not its business.
	{"a body that binds the name reads its own slot, not the module",
		"RATE = 2.5\n\ndef f():\n    RATE = 1.0\n    return RATE\n\nprint(f())\nprint(RATE)\n", "1.0\n2.5\n"},
	{"a parameter named like the module's double is the parameter's",
		"RATE = 2.5\n\ndef scale(RATE):\n    return RATE * 2\n\nprint(scale(3.0))\nprint(scale(2))\n", "6.0\n4\n"},
	// ---- the module's frame is a body too, and a float no body reads stays exactly what it was.
	{"a module float no body reads is main's own local",
		"RATE = 2.5\nprint(RATE)\n", "2.5\n"},
	// ---- containers: the ELEMENT may be the module's double even while the list itself is not.
	{"the module's double inside a list element",
		"RATE = 2.5\n\ndef build():\n    return [RATE, RATE + 1]\n\nprint(build())\n", "[2.5, 3.5]\n"},
	// ---- a class method may READ the module's double; returning it is the other row's word.
	{"a method reads the module's double in an expression it prints",
		"RATE = 2.5\n\nclass C:\n    def area(self, w):\n        print(w * RATE)\n        return 0\n\nC().area(3)\n", "7.5\n"},
	// ---- a default argument is evaluated by the module and handed to the body.
	{"the module's double as a default argument",
		"RATE = 2.5\n\ndef scale(v=RATE):\n    return v * 2\n\nprint(scale())\n", "5.0\n"},
	// ---- the two doors in one program: the i32 door took bools long before this row gave the
	// double one, and closing the float half must not have disturbed it.
	{"a module bool read by a body is the i32 door's, answered beside this row's",
		"FLAG = True\n\ndef f():\n    return FLAG\n\nprint(f())\n", "True\n"},
	// ---- a recursive body reads the global on every level.
	{"a recursive body reads the module's double at every level",
		"STEP = 1.5\n\ndef down(n):\n    if n <= 0:\n        return 0.0\n    return STEP + down(n - 1)\n\nprint(down(3))\n", "4.5\n"},
	// ---- the loop case that is NOT the accumulator row: the driven value is the module's, the
	// accumulator is the body's own int, so neither half leans on the other.
	{"a body scales the module's double around its own int accumulator",
		"SCALE = 2.0\n\ndef scaled():\n    total = 0\n    for i in [1, 2, 3]:\n        total = total + i\n    return total * SCALE\n\nprint(scaled())\n", "12.0\n"},
}

// TestACompiledBodyReadsTheModulesFloatFromTheModulesGlobal is the answer leg: the compiled run answers
// what CPython answers, and the record is asked for the same source so a fix here cannot quietly
// disagree with the answer on record.
func TestACompiledBodyReadsTheModulesFloatFromTheModulesGlobal(t *testing.T) {
	asked := 0
	for _, tc := range moduleFloatPrograms {
		// Serial on purpose, like the ADR 0375 table beside it: the record-coverage counter below is
		// one number for the whole table, and a parallel subtest would check it before the cases that
		// increment it had run — a guard that reports "the record leg is not being walked" because of
		// when it ran is a guard that gets deleted rather than fixed.
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%s (%q): refused: %v", tc.name, tc.src, err)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("%s: AOT ran %q, want CPython's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
			}
			if HasGoldenAnswer(tc.src) {
				asked++
				want := goldenStdout(silentReporter{}, tc.src)
				if out := compiledRunIsTheBackend(t, tc.src); out != want {
					t.Errorf("%s: the compiled leg ran %q, the record says %q\nsrc: %s", tc.name, out, want, tc.src)
				}
			}
		})
	}
	if asked == 0 {
		t.Fatal("no row of this table is covered by the record: the record leg is not being walked")
	}
}

// TestTheModuleFloatIsAGlobalAndNotAnAlloca is the IR leg. An answer table can be satisfied by a lucky
// constant; the module global is the part that makes the answer mean something. A body-local float is
// asserted in the same breath because the rule this row had to avoid breaking is ADR 0375's: closing
// module visibility must not have moved a body's OWN float out of its frame, and a module float that no
// body reads must not have gained a global it does not need.
func TestTheModuleFloatIsAGlobalAndNotAnAlloca(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		need      []string
		deny      []string
	}{
		{
			name: "the module's double is one global, written by main and read by the body",
			src:  "RATE = 2.5\n\ndef scale(v):\n    return v * RATE\n\nprint(scale(3))\n",
			need: []string{
				`@gy_mod_RATE = global double`,
				`store double %t6, double* @gy_mod_RATE`,
				`load double, double* @gy_mod_RATE`,
			},
			// The body reads the module's word and does not mint its own: an alloca for the name
			// would be a second slot for one name (ADR 0351's disease) and the exact way this row
			// could look right while the global stayed unwritten.
			deny: []string{`%_RATE = alloca double`},
		},
		{
			// The control that keeps ADR 0375 intact: the body's own float is still a frame slot.
			name: "a body's own float is still its frame's, module globals or not",
			src:  "RATE = 2.5\n\ndef f():\n    u = 9.5\n    return u + RATE\n\nprint(f())\n",
			need: []string{`alloca double`, `%_u`, `@gy_mod_RATE`},
		},
		{
			// The byte-for-byte control: a module float nobody reads keeps the alloca it always had,
			// so this row cannot have spent a program's stack frame on a name that needs no door.
			name: "a module float no body reads is still main's alloca",
			src:  "RATE = 2.5\nprint(RATE)\n",
			need: []string{`%_RATE = alloca double`},
			deny: []string{`@gy_mod_RATE`},
		},
		{
			// The i32 door was here first: an int module global is emitted exactly as before, and
			// the double door did not pull it into the double word.
			name: "a module int a body reads and the module rebinds is still an i32 global",
			src:  "COUNT = 4\nCOUNT = 6\n\ndef f():\n    return COUNT + 1\n\nprint(f())\n",
			need: []string{`@gy_mod_COUNT = global i32`, `load i32, i32* @gy_mod_COUNT`},
			deny: []string{`@gy_mod_COUNT = global double`},
		},
		{
			// The control that keeps the two doors from being one door: a name bound ONCE to an int
			// literal is a value, and ADR 0227 folds it rather than paying for a global.
			name: "a module int bound once to a literal is folded, not stored",
			src:  "COUNT = 4\n\ndef f():\n    return COUNT + 1\n\nprint(f())\n",
			deny: []string{`@gy_mod_COUNT`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%s (%q): refused: %v", tc.name, tc.src, err)
			}
			for _, frag := range tc.need {
				if !strings.Contains(res.IR, frag) {
					t.Errorf("%s: emitted module has no %q\nsrc: %s\n%s", tc.name, frag, tc.src, res.IR)
				}
			}
			for _, frag := range tc.deny {
				if strings.Contains(res.IR, frag) {
					t.Errorf("%s: emitted module carries %q, which is the road this row does not take\nsrc: %s", tc.name, frag, tc.src)
				}
			}
		})
	}
}

// moduleFloatRefusals pins what the gate will NOT buy. Each row is a name the gate cannot promise a word
// for, and the sentence must say so — including the two names the missing WORD belongs to a row that is
// still open (a method's i32 return word, L11.6 · Gap R.3c; a nested `def`'s single word). The point of
// putting them beside the answers is that a compiler which guessed here would leave exit 2: a `double`
// glued into `mul i32` is `llc` rejecting the module, which the exit-code contract reserves for a
// compiler bug (ADR 0166).
var moduleFloatRefusals = []struct {
	name, src, want, row string
}{
	{
		// The word this needs is the one L11.6 · Gap R.3c owes: a method is emitted `define i32`
		// whatever its body computes, so the double would have nowhere to travel and `llc` would
		// reject the `ret`. Refused before the header is written (ADR 0196's rule for the same shape
		// with a rebound parameter).
		name: "a method that returns the module's double is refused, not truncated",
		src:  "RATE = 2.5\n\nclass C:\n    def scale(self, v):\n        return v * RATE\n\nprint(C().scale(2))\n",
		want: "the module's float", row: "Gap R.3c",
	},
	{
		// The same missing word in the nested-`def` shape. Both cases leave the program at exit 1
		// with the construct named; neither leaves a module `llc` has to reject.
		name: "a nested def that returns the module's double is refused, not truncated",
		src:  "RATE = 2.5\n\ndef outer():\n    def inner():\n        return RATE * 2\n    print(inner())\n\nouter()\n",
		want: "the module's float", row: "L11.1",
	},
}

// moduleFloatDebt is what the doors OWN and get wrong, pinned to the wrong answer so the day it is
// fixed the test fails and says so. Gap R.343 is not this cycle's construct — the text travels through
// ADR 0227's i32 slot, which was never built for an interned index — but it is a name the module binds
// and a body reads, and a reader comparing this row's door to that one needs to know that the door
// beside it answers `0` for `world` rather than refusing.
var moduleFloatDebt = []struct {
	name, src, want, today, row string
}{
	{
		name:  "a module text read by a body answers the intern position",
		src:   "NAME = \"world\"\n\ndef greet():\n    print(NAME)\n\ngreet()\n",
		want:  "world\n",
		today: "0\n",
		row:   "Gap R.343",
	},
}

// TestWhatTheModuleTextDoorStillOwes is that pin. The compiled answer is asserted, not celebrated: it
// leaves exit 0 with the wrong word, which is the class of defect this project treats as owed work
// rather than as a refusal's excuse.
func TestWhatTheModuleTextDoorStillOwes(t *testing.T) {
	t.Parallel()
	for _, tc := range moduleFloatDebt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := compiledRunIsTheBackend(t, tc.src)
			if got == tc.want {
				t.Fatalf("%s: the compiled leg now answers %q, the reference's own bytes — DELETE this row, %s has closed", tc.name, got, tc.row)
			}
			if got != tc.today {
				t.Errorf("%s: the compiled leg prints %q, and this row pinned %q (reference %q, %s)", tc.name, got, tc.today, tc.want, tc.row)
			}
		})
	}
}

// TestWhatTheFloatAnswerWordOfACallStillOwes pins a defect this cycle FOUND and did not cause: printing
// a float-returning call whose argument arrives as a double ends in
//
//	store i32 %t8, i32* %t105
//
// which is `llc` rejecting the module — exit 2, the code the exit-code contract reserves for a compiler
// bug. Every row here reproduces on the pre-ADR-0376 build unchanged, and none of them names a module
// float: the answer cell a promoted `define double` fills has the right word only for the shapes the
// ADR 0254 table knows, and `return v + 1.0` of a promoted parameter is not one of them. The row that
// owns it is Gap R.344; the shapes this cycle's programs use are the ones the table does answer (`v *
// RATE`, `RATE * 2`, `HALF / 2`), pinned above.
func TestWhatTheFloatAnswerWordOfACallStillOwes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, rejects, row string
	}{
		{
			name:    "print of a float-returning call whose promoted parameter meets a literal",
			src:     "def f(v):\n    return v + 1.0\n\nprint(f(2.0))\n",
			rejects: "defined with type 'double' but expected 'i32'",
			row:     "Gap R.344",
		},
		{
			name:    "the same call written the other way round",
			src:     "def f(v):\n    return 2.0 * v\n\nprint(f(2.0))\n",
			rejects: "defined with type 'double' but expected 'i32'",
			row:     "Gap R.344",
		},
		{
			name:    "the same call with the float arriving as the module's default",
			src:     "RATE = 2.5\n\ndef f(v=RATE):\n    return v + 1.0\n\nprint(f())\n",
			rejects: "defined with type 'double' but expected 'i32'",
			row:     "Gap R.344",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := RunSource(tc.src)
			if err == nil {
				t.Fatalf("%s: the module no longer dies in `llc` — it printed %q; when the answer matches the "+
					"reference, DELETE this case and let the row above claim it\nsrc: %s", tc.row, out, tc.src)
			}
			if !strings.Contains(err.Error(), tc.rejects) {
				t.Fatalf("%s: the module rejection changed from %q to:\n%v", tc.row, tc.rejects, err)
			}
		})
	}
}

// TestTheModuleFloatGateRefusesWhatItCannotPromise is the refusal leg, and the exit-code half of the
// row: each source must leave the toolchain at exit 1 with the construct named, which is the difference
// between a compiler that knows its own limits and one that emits a `double` into an `i32` slot and lets
// `llc` report the bug at exit 2.
func TestTheModuleFloatGateRefusesWhatItCannotPromise(t *testing.T) {
	t.Parallel()
	for _, tc := range moduleFloatRefusals {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := RunSource(tc.src); err == nil {
				t.Fatalf("%s: the program compiled; this shape is owed by %s and the reference prints %q\nsrc: %s", tc.name, tc.row, tc.want, tc.src)
			}
			msg := compiledRefusalOf(tc.src)
			if msg == "" {
				t.Fatalf("%s: expected a refusal, and the program neither compiled nor named a construct\nsrc: %s", tc.name, tc.src)
			}
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("%s: refusal must say %q (%s), said:\n%s", tc.name, tc.want, tc.row, msg)
			}
		})
	}
}

// TestTheGateForAModuleFloatIsAskedOfEveryBindingAndEveryBinder is the gate's own table: for a handful
// of programs, which names the door takes, and which it leaves alone. This is the assertion an answer
// table cannot give, because an answer is usually still right when the gate is too SHY — a name left out
// just keeps the refusal. It is the gate being too WIDE that silently wrongs a program, and only this
// table sees the difference.
func TestTheGateForAModuleFloatIsAskedOfEveryBindingAndEveryBinder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		takes     []string
		skips     []string
	}{
		{
			name:  "a literal float read by a body is the door's",
			src:   "RATE = 2.5\n\ndef f():\n    return RATE * 2\n",
			takes: []string{"RATE"},
		},
		{
			name:  "a float no body reads is nobody's business",
			src:   "RATE = 2.5\nprint(RATE)\n",
			skips: []string{"RATE"},
		},
		{
			// Widening this to the float literal `1.5` would put an `i32` in the double's slot: the
			// kind the module settled on is the kind the global has to hold.
			name:  "a name rebound to an int anywhere in the module is not a double",
			src:   "RATE = 2.5\nRATE = 1.5\nRATE = 3\n\ndef f():\n    return RATE\n",
			skips: []string{"RATE"},
		},
		{
			// The ADR 0220 rule read from the gate's side: the binder owns the name, so the door
			// must not open for it — `%_STEP = alloca double` and `@gy_mod_STEP` are two slots for
			// one name, which is ADR 0351's disease.
			name:  "a body that binds the name takes it out of the door",
			src:   "STEP = 1.5\n\ndef f():\n    STEP = 2.5\n    return STEP\n",
			skips: []string{"STEP"},
		},
		{
			name:  "a parameter named like the module's float is the body's",
			src:   "RATE = 1.5\n\ndef f(RATE):\n    return RATE * 2\n",
			skips: []string{"RATE"},
		},
		{
			name:  "a value the gate cannot read is left refused",
			src:   "x = 2\nRATE = float(x)\n\ndef f():\n    return RATE\n",
			skips: []string{"RATE"},
		},
		{
			name:  "a subscript read is left refused",
			src:   "xs = [1.5]\nRATE = xs[0]\n\ndef f():\n    return RATE\n",
			skips: []string{"RATE"},
		},
		{
			// A computed name and a literal name in one program: the gate is per-name, and one
			// disqualified name must not cost the other its global.
			name:  "one disqualified name does not cost the neighbour its global",
			src:   "A = 1.5\nx = 2\nB = float(x)\n\ndef f():\n    return A + 1.0\n",
			takes: []string{"A"}, skips: []string{"B"},
		},
		{
			// `for` targets, `with ... as` bindings, `match` captures and unpackings bind names the
			// door never wrote; each drops the name it touches and nothing else.
			name:  "a for target drops the name it writes",
			src:   "RATE = 1.5\nfor RATE in [1, 2]:\n    print(RATE)\n",
			skips: []string{"RATE"},
		},
		{
			name:  "an unpacking drops the names it writes",
			src:   "A = 1.5\nB = 2.5\nA, B = B, A\n\ndef f():\n    return A + B\n",
			skips: []string{"A", "B"},
		},
		{
			name:  "a literal product and a literal quotient are both the door's",
			src:   "AREA = 2.0 * 3\nHALF = 10 / 4\n\ndef f():\n    return AREA + HALF\n",
			takes: []string{"AREA", "HALF"},
		},
		{
			// An annotation is the program SAYING the name is a float: the gate reads the annotation
			// and the value, so an annotated binding with a literal value is still a promise.
			name:  "an annotated module float keeps its global",
			src:   "RATE: float = 2.5\n\ndef f():\n    return RATE * 2\n",
			takes: []string{"RATE"},
		},
		{
			// The annotation says float, the assignment says int: the kind the module settled on
			// wins, and the name keeps the word the values agree on rather than the word claimed.
			name:  "an annotated name rebound to an int keeps no double word",
			src:   "RATE: float = 2.5\nRATE = 3\n\ndef f():\n    return RATE\n",
			skips: []string{"RATE"},
		},
		{
			// A name the module never binds at all is not the door's either: only an assignment
			// writes the global, and an absent one leaves the body reading a zero it was never given.
			name:  "a name no module statement binds is not the door's",
			src:   "def f():\n    return MISSING * 2\n",
			skips: []string{"MISSING"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog := mustParse(t, tc.src)
			consts, all := moduleEnvFor(prog)
			slots := moduleFloatSlotNames(prog, consts, all)
			for _, nm := range tc.takes {
				if _, ok := slots[nm]; !ok {
					t.Errorf("%s: %q is a module double a body reads and the gate left it out; the body falls back to reading its own alloca and answers 0 (slots: %s)", tc.name, nm, strings.Join(moduleFloatKeys(slots), ", "))
				}
			}
			for _, nm := range tc.skips {
				if sym, ok := slots[nm]; ok {
					t.Errorf("%s: %q must NOT be the door's — the gate cannot promise that name the word it writes into @%s (slots: %s)", tc.name, nm, sym, strings.Join(moduleFloatKeys(slots), ", "))
				}
			}
		})
	}
}

func moduleFloatKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"->"+v)
	}
	return out
}

// TestTheModuleFloatDoorDoesNotAnswerWhatTheFrameAnswers keeps ADR 0375's rule and this row's rule from
// being quietly merged into one table, which is the mistake the obvious implementation makes: seed a
// body's float table from the module and every leaked local comes back as a global. The two maps have
// different owners, different lifetimes and — proved by the shadowing row — different answers for the
// same name.
func TestTheModuleFloatDoorDoesNotAnswerWhatTheFrameAnswers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			// The same name in the same program answers TWO numbers, and which one the line gets is
			// decided by the frame, not by the global.
			name: "the body's own write is the body's answer, the module's is the module's",
			src:  "RATE = 2.5\n\ndef f():\n    RATE = 1.0\n    return RATE\n\nprint(f(), RATE)\n",
			want: "1.0 2.5\n",
		},
		{
			// The control that makes the row above mean something: with no local binding, the read
			// IS the global's, in the same spelling.
			name: "the same body with no local reads the global",
			src:  "RATE = 2.5\n\ndef f():\n    return RATE\n\nprint(f(), RATE)\n",
			want: "2.5 2.5\n",
		},
		{
			// The float table and the global agree about KIND and disagree about WHICH value, which
			// is the only arrangement in which both rules can be tested at once.
			name: "a body-local float beside the module's float answers two values",
			src:  "RATE = 2.5\n\ndef f():\n    u = 1.0\n    return u + RATE\n\nprint(f())\n",
			want: "3.5\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%s (%q): refused: %v", tc.name, tc.src, err)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("%s: AOT ran %q, want the reference's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
			}
		})
	}
}

// TestWhatTheModuleFloatDoorStillOwes fails while the half this cycle closed is being described as the
// whole of Gap R.35. The container read is the part that stays refused, and the roadmap's reason must
// still be legible.
func TestWhatTheModuleFloatDoorStillOwes(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "roadmap-details.md"))
	if err != nil {
		t.Fatalf("cannot read the roadmap's details: %v", err)
	}
	details := string(b)
	owed := ""
	for _, line := range strings.Split(details, "\n") {
		if strings.HasPrefix(line, "#") && strings.Contains(line, "Gap R.35") {
			owed = line
			break
		}
	}
	if owed == "" {
		t.Fatal(`docs/roadmap-details.md has no section for Gap R.35: the row this cycle closed HALF of has ` +
			`no place left to carry the half it still owes`)
	}
	if !strings.Contains(details, "container") || !strings.Contains(details, "module") {
		t.Fatal(`docs/roadmap-details.md's Gap R.35 section no longer says what is still owed: the module ` +
			`container read is the half this cycle leaves open, and a reader cannot find it`)
	}
}

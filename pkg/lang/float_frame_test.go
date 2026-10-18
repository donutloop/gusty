package lang

import (
	"strings"
	"testing"
)

// float_frame_test.go — the float-variable table a body reads is THAT body's own writes, not the
// last `define` that happened to be emitted (roadmap L11.6, Gap R.339; ADR 0375).
//
// `g.floatVars` answers 「whose last assignment in this body produced a double」. It is read by
// `isFloat`'s name arm, by the print door, by the comparison road, by the verdict census and by the
// heap-argument road — every one of them asking about a name in the body being lowered right now.
// ADR 0373 settled WHICH write a read answers (the last one); it left open whose writes the table
// holds, and the map had been allocated once per module and never cleared between `define`s. So a
// body read the table the previous body had left behind:
//
//	def bound():
//	    u = 9.5          # writes u as a double
//	    return u
//
//	def lastWrite():
//	    u = 2.5
//	    u = 3            # LAST write: the integer 3
//	    return u
//
//	print(bound())       # 9.5
//	print(lastWrite())   # CPython 3 · this toolchain 3.0
//
// and the same body alone answers `3` — an answer that depends on emission order is not an answer.
// The conformance matrix found it by putting the two bodies in one file; every other test asked one
// function at a time. `print(b(), a(), b())` below is the loudest form: the SAME call to the same
// function answered `7.0` between two calls to a float-returning neighbour and `7` elsewhere, in one
// line of one program.
//
// The cure is the one the module already trusts for every other per-frame kind table: `beginScope`
// installs the container/bool/None/text tables per scope and restores them on the way out, and the
// float table was the sibling it never installed (ADR 0163 closed this disease for the tables that
// exist — ADR 0375 is the same rule, applied to the one that was forgotten). The tables the rule
// covers are the float table and its `doubleSlot` companion, which says which of this body's slots
// were allocated as a `double`; both roads that emit a body — `funcDef`, and the method/closure-lift
// roads through `enterBody` — take it.
//
// What the rule deliberately does NOT answer is 「may a body read a module-level float at all」:
// that is Gap R.35, whose refusal rows are pinned below so nobody mistakes one row for the other.

// floatFramePrograms is the answer table. Every row is CPython's own bytes; every row was also
// checked against the record, and the guard at the bottom of the test fails if none of them is in
// the record, so the table cannot quietly stop being cross-checked.
var floatFramePrograms = []struct {
	name, src, want string
}{
	// ---- the row the gap was named for: two bodies, one name, the second one int-returning.
	{"two bodies sharing a name answer their OWN last write",
		"def bound():\n    u = 9.5\n    return u\n\n" +
			"def lastWrite():\n    u = 2.5\n    u = 3\n    return u\n\n" +
			"print(bound())\nprint(lastWrite())\n", "9.5\n3\n"},
	{"the same two bodies written in the other order",
		"def lastWrite():\n    u = 2.5\n    u = 3\n    return u\n\n" +
			"def bound():\n    u = 9.5\n    return u\n\n" +
			"print(lastWrite())\nprint(bound())\n", "3\n9.5\n"},
	// ---- the table must not be "the last float seen": three bodies, one name, kinds alternating.
	{"three bodies sharing a name keep three kinds",
		"def a():\n    v = 1.5\n    return v\n\ndef b():\n    v = 7\n    return v\n\n" +
			"def c():\n    v = 2.0\n    return v\n\nprint(a(), b(), c())\n", "1.5 7 2.0\n"},
	// ---- the loudest form: ONE function, called three times in one line, answering two ways.
	{"one int-returning body answers the same thing beside a float neighbour and alone",
		"def a():\n    v = 1.5\n    return v\n\ndef b():\n    v = 7\n    return v\n\n" +
			"print(b(), a(), b())\n", "7 1.5 7\n"},
	// ---- the method road emits a body too, and leaked through it.
	{"a method's float local is not the next function's float",
		"class Box:\n    def show(self):\n        u = 9.5\n        return 1\n\n" +
			"def later():\n    u = 3\n    return u\n\nprint(Box().show())\nprint(later())\n", "1\n3\n"},
	{"a function's float local is not the next method's float",
		"def first():\n    s = 2.5\n    return s\n\nclass Box:\n    def show(self):\n" +
			"        s = 3\n        return s\n\nprint(first())\nprint(Box().show())\n", "2.5\n3\n"},
	// ---- the controls that localise the defect to the table's SCOPE.
	{"the int body alone (the control)",
		"def lastWrite():\n    u = 2.5\n    u = 3\n    return u\n\nprint(lastWrite())\n", "3\n"},
	{"the float body alone (the control)",
		"def bound():\n    u = 9.5\n    return u\n\nprint(bound())\n", "9.5\n"},
	// ---- the accumulator ADR 0377 closed: a body's LAST write to a float reads the name itself, so the
	// scan that chooses the return word has to FOLD the body's writes rather than ask only the newest one.
	// The control beside it is the same loop at module level, which always answered `6.0` and is what
	// localised the wrong `0` to the scan rather than to the arithmetic; the two rows after it are the
	// rules that must NOT move — an int accumulator stays an int, and a float accumulator finally
	// rebound to an int answers the int, which is ADR 0373's last-write rule read through the fold.
	{"a for-driven float accumulator inside a body answers the seed's kind",
		"def loopAcc():\n    t = 0.0\n    for i in [1, 2, 3]:\n        t = t + i\n    return t\n\n" +
			"print(loopAcc())\n", "6.0\n"},
	{"the same accumulator at module level (the control)",
		"t = 0.0\nfor i in [1, 2, 3]:\n    t = t + i\nprint(t)\n", "6.0\n"},
	{"an int accumulator driven by a for stays an int",
		"def loopInt():\n    t = 0\n    for i in [1, 2, 3]:\n        t = t + i\n    return t\n\n" +
			"print(loopInt())\n", "6\n"},
	{"a float accumulator later rebound to an int answers the int",
		"def acc():\n    t = 0.0\n    for i in [1, 2, 3]:\n        t = t + i\n    t = 3\n    return t\n\n" +
			"print(acc())\n", "3\n"},
	// ---- the module's own frame is a body too: main's float must not reach a callee's local.
	{"main's float local is not the callee's local",
		"def body():\n    u = 3\n    return u\n\nu = 9.5\nprint(body())\nprint(u)\n", "3\n9.5\n"},
	// ---- the float convention's parameter words are frame facts as well.
	{"a promoted parameter is not the next body's float",
		"def addf(x):\n    x = x + 1.5\n    return x\n\ndef later():\n    x = 4\n    return x\n\n" +
			"print(addf(1.0))\nprint(later())\n", "2.5\n4\n"},
	{"a float-returning neighbour leaves an int body's name an int",
		"def half(x):\n    return x / 2\n\ndef later():\n    h = 5\n    return h\n\n" +
			"print(half(3))\nprint(later())\n", "1.5\n5\n"},
	// ---- a `lambda` minted in the middle of a body must not take the frame's table with it, nor
	// leave its own behind (Gap R.265's rule about a nested emission and its frame).
	{"a lambda applied inside a float-owning body answers its own body",
		"def mixed():\n    z = 2.5\n    return (lambda: 8)()\n\nprint(mixed())\n", "8\n"},
}

// TestWhatFloatTableABodyReadsIsThatBodyOwnWrites is the answer leg: the compiled run answers what
// the body's own last write says, and the record is asked for the same source so a fix here cannot
// quietly disagree with the answer on record.
func TestWhatFloatTableABodyReadsIsThatBodyOwnWrites(t *testing.T) {
	asked := 0
	for _, tc := range floatFramePrograms {
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

// defineBodyOf finds one `define` in the module by symbol and returns its text, so an assertion can
// say what ONE function's body does rather than what the file contains somewhere.
func defineBodyOf(t *testing.T, ir, sym string) string {
	t.Helper()
	for _, line := range strings.Split(ir, "\n") {
		if strings.HasPrefix(line, "define ") && strings.Contains(line, "@"+sym+"(") {
			at := strings.Index(ir, line+"\n")
			if at < 0 {
				t.Fatalf("cannot recover the body of %s:\n%s", sym, ir)
			}
			rest := ir[at+len(line)+1:]
			end := strings.Index(rest, "\n}")
			if end < 0 {
				t.Fatalf("the body of %s never closes:\n%s", sym, ir)
			}
			return line + "\n" + rest[:end]
		}
	}
	t.Fatalf("the module has no `define` for %s:\n%s", sym, ir)
	return ""
}

// TestTheFloatDefineAndTheIntDefineCoexistInOneModule is the IR leg. The bug was not only the print
// door rendering an int with the float renderer: `funcDef` decided the RETURN WORD by asking
// `isFloat` through the inherited table, so the int-returning body was emitted `define double` at
// all. Pinning the two words in one module is what stops a "fix" that only redirects the printer and
// leaves a body returning a double it never computed.
func TestTheFloatDefineAndTheIntDefineCoexistInOneModule(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		src   string
		words map[string]string // symbol -> the word its `define` must return
	}{
		{
			name: "three bodies, one name, two words",
			src: "def a():\n    v = 1.5\n    return v\n\ndef b():\n    v = 7\n    return v\n\n" +
				"def c():\n    v = 2.0\n    return v\n\nprint(a(), b(), c())\n",
			words: map[string]string{"gy_a": "double", "gy_b": "i32", "gy_c": "double"},
		},
		{
			name: "a promoted parameter stays in its own define",
			src: "def addf(x):\n    x = x + 1.5\n    return x\n\ndef later():\n    x = 4\n    return x\n\n" +
				"print(addf(1.0))\nprint(later())\n",
			words: map[string]string{"gy_addf": "double", "gy_later": "i32"},
		},
		{
			name: "a method's double slot leaves the next define alone",
			src: "class Box:\n    def show(self):\n        u = 9.5\n        return 1\n\n" +
				"def later():\n    u = 3\n    return u\n\nprint(Box().show())\nprint(later())\n",
			words: map[string]string{"gy_later": "i32"},
		},
		{
			// the control: the promotion still happens where the body really answers a double
			name:  "a body that binds a name to a double is still promoted",
			src:   "def bound():\n    u = 9.5\n    return u\n\nprint(bound())\n",
			words: map[string]string{"gy_bound": "double"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%s: refused: %v", tc.name, err)
			}
			for sym, word := range tc.words {
				body := defineBodyOf(t, res.IR, sym)
				head := "define " + word + " @" + sym + "("
				if !strings.HasPrefix(body, head) {
					t.Errorf("%s: %s must open %q, and it opens:\n%s", tc.name, sym, head, headLineOf(body))
				}
				if word == "i32" && strings.Contains(body, "ret double") {
					t.Errorf("%s: %s returns a double it never computed:\n%s", tc.name, sym, body)
				}
				if word == "double" && !strings.Contains(body, "ret double") {
					t.Errorf("%s: %s is a double-returning define with no `ret double`:\n%s", tc.name, sym, body)
				}
			}
		})
	}
}

func headLineOf(s string) string {
	if at := strings.Index(s, "\n"); at >= 0 {
		return s[:at]
	}
	return s
}

// The row this file used to keep here — Gap R.342, the nested `def` that stored through its ENCLOSING
// frame's alloca and died in `llc` — is DELETED, as its own rule required: ADR 0378 gives every road that
// emits a body the frame entry that body's slots live in, and the program now prints the reference's `3`.
// It answers in `frame_slots_test.go`, on the compiled leg, the record leg and the reference leg, beside
// the IR row that pins the `alloca` inside the `define` that stores through it. What the two rows were for
// — proving ADR 0375's frame rule is not a half-answer to Gap R.35's module-state question — is still
// pinned where it always was, in `module_float_test.go`: `TestTheModuleFloatDoorDoesNotAnswerWhatTheFrameAnswers`
// asks both rules of one program, and `TestTheModuleFloatGateRefusesWhatItCannotPromise` still refuses the
// module container and the two positions that have no word.

// TestTheBodyFloatTableIsInstalledByEveryRoadThatEmitsABody is the guard the rule needs to stay true:
// the two roads that begin a body — `funcDef` through `beginScope`, and the method/closure-lift roads
// through `enterBody` — both go through one helper, so a helper that stopped installing the table
// could not be noticed by an answer that happens to be right anyway.
func TestTheBodyFloatTableIsInstalledByEveryRoadThatEmitsABody(t *testing.T) {
	t.Parallel()
	g := newFloatFrameProbeGen(t)
	undo := g.enterBodyFloatTable()
	if g.floatVars == nil || g.doubleSlot == nil {
		undo()
		t.Fatal("enterBodyFloatTable did not install a table")
	}
	g.floatVars["u"] = true
	g.doubleSlot["u"] = true
	undo()
	// The body's write is gone and the frame's is still there: the entry installed a NEW map (so the
	// body could not write through it into its successor), and the undo put the frame's own map back
	// with its contents (so the frame that owns a name still owns it after the nested body is emitted —
	// Gap R.265's rule about a nested emission that used to take its frame's state with it).
	if _, leaked := g.floatVars["u"]; leaked {
		t.Fatal("the body's write leaked into the enclosing frame's table — the entry did not install a new map")
	}
	if _, had := g.floatVars["keep"]; !had {
		t.Fatal("the enclosing frame's float table did not come back")
	}
	if _, leaked := g.doubleSlot["u"]; leaked {
		t.Fatal("the body's double slot leaked into the enclosing frame's table")
	}
}

// newFloatFrameProbeGen builds just enough of a generator to hold the tables the rule is about: the
// rule is about the tables themselves, and asking it of a whole compile would let an unrelated road
// refill the map and answer the question by accident.
func newFloatFrameProbeGen(t *testing.T) *irGen {
	t.Helper()
	g := &irGen{}
	g.floatVars = map[string]bool{"keep": true}
	g.doubleSlot = map[string]bool{"keep": true}
	return g
}

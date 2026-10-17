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

// TestTheFrameRuleIsNotAnAnswerToTheModuleStateQuestion keeps the two rows apart. Gap R.339 is about
// WHOSE writes a body reads; Gap R.35 is about what a body may read at all, and seeding the body's
// table from the module would have been that row's work in this commit. The refusal rows below are
// what those programs get, and the message must name the row that owes the answer.
//
// The last two rows are NEW defects, measured while writing this rule and filed rather than fixed
// here, both pre-existing (identical on the pre-ADR-0374 build):
//
//   - Gap R.341 — a float accumulator driven by a `for` inside a body answers `0` at exit 0, while
//     the identical loop at module level answers the reference's `6.0`;
//   - Gap R.342 — a nested `def` that shares a name with a float local of its enclosing body emits a
//     store to the ENCLOSING frame's alloca (`store i32 %t7, i32* %_w`), `llc` rejects the module and
//     the program leaves exit 2, which ADR 0166 reserves for a compiler bug.
func TestTheFrameRuleIsNotAnAnswerToTheModuleStateQuestion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want, today, gap, rejects, names string
	}{
		{
			name:  "a module float read by a body is still the module-state refusal",
			src:   "RATE = 2.5\n\ndef scale(v):\n    return v * RATE\n\nprint(scale(3))\n",
			want:  "7.5\n",
			gap:   "Gap R.35",
			names: "Gap R.35",
		},
		{
			name:  "a module float the module itself computed is answered by neither row",
			src:   "HALF = 10 / 4\n\ndef h():\n    return HALF * 2\n\nprint(h())\n",
			want:  "5.0\n",
			today: "0\n",
			gap:   "Gap R.35",
		},
		{
			name:  "a float accumulator driven by a for inside a body answers the void word",
			src:   "def loopAcc():\n    t = 0.0\n    for i in [1, 2, 3]:\n        t = t + i\n    return t\n\nprint(loopAcc())\n",
			want:  "6.0\n",
			today: "0\n",
			gap:   "Gap R.341",
		},
		{
			// the control that localises it to the body: the identical loop at module level
			name:  "the same accumulator at module level answers the reference",
			src:   "t = 0.0\nfor i in [1, 2, 3]:\n    t = t + i\nprint(t)\n",
			want:  "6.0\n",
			today: "6.0\n",
			gap:   "Gap R.341",
		},
		{
			name:    "a nested def sharing a name with its enclosing body's float local dies in llc",
			src:     "def outer():\n    def inner():\n        w = 3\n        return w\n    w = 1.5\n    return inner()\n\nprint(outer())\n",
			want:    "3\n",
			gap:     "Gap R.342",
			rejects: "use of undefined value '%_w'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.rejects != "" {
				got, err := RunSource(tc.src)
				if err == nil {
					t.Fatalf("%s: the compiled leg no longer dies in `llc` — it printed %q (the reference prints %q); "+
						"when those two agree, DELETE this case\n\t%s", tc.gap, got, tc.want, tc.src)
				}
				if !strings.Contains(err.Error(), tc.rejects) {
					t.Fatalf("%s: the module rejection changed from %q to:\n%v", tc.gap, tc.rejects, err)
				}
				return
			}
			if tc.today == "" {
				msg := compiledRefusalOf(tc.src)
				if msg == "" {
					t.Fatalf("%s: expected a refusal, the program compiled (the reference prints %q)", tc.gap, tc.want)
				}
				if tc.names != "" && !strings.Contains(msg, tc.names) {
					t.Fatalf("%s: the refusal does not name the row that owes the answer (%s): %s", tc.gap, tc.names, msg)
				}
				return
			}
			got := compiledRunIsTheBackend(t, tc.src)
			if got == tc.want && tc.want != tc.today {
				t.Fatalf("%s: the compiled leg now answers %q, the reference's own bytes — DELETE this row, %s has closed",
					tc.name, got, tc.gap)
			}
			if got != tc.today {
				t.Errorf("%s: the compiled leg prints %q, and this row pinned %q (reference %q, %s)",
					tc.name, got, tc.today, tc.want, tc.gap)
			}
		})
	}
}

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

// pkg/lang/math_const_spelling_test.go — the standard library answers the reference's NAME, not only ours
// (roadmap L11.6's last clause; the type of the value is ADR 0272's, the name is ADR 0321's).
//
// `import math` / `print(math.PI)` has been right on every engine since ADR 0272, and it is still the row the
// conformance ledger records as `not_applicable`: the reference has no `PI`, it has `pi`, so no CPython program
// exists that asks the question our program asks. A surface the reference cannot spell is a surface the
// reference leg cannot adjudicate — the comparison lived in a hand-written twin
// (`integration/module_const_test.go`) rather than in a shared program, which is fine for a value and hopeless
// for a name.
//
// `stdlib/math.gy` now declares the reference's spellings as the literal and this language's upper-case names
// as ALIASES of it. Five claims, each a different way for this to be false:
//   - the reference-spelled constants answer what CPython answers for the SAME source text — no twin, because
//     the program is a CPython program now — through the positions L11.6 owned (the negation, the product, the
//     comparison behind a binding, the digit count, the floored division, the modulo, a container, an f-string);
//   - the two spellings cannot disagree, because the module writes each value once and the second name reads
//     the first (`PI = pi`), which is read off the module's own text rather than asserted in prose;
//   - the extension names (`PHI`/`SQRT2`/`LN2`/`LN10` and their lower-case spellings) answer their values on the
//     compiled leg, where the reference has no opinion to compare;
//   - the record leg still answers for every spelling — the upper-case names are what the corpus was recorded
//     with, and the reference-spelled sources went in through tools/recmerge, which asks CPython first;
//   - a name the module does not declare is refused in words, and `math.inf` / `math.nan` are named as the
//     missing surface they are (roadmap Gap R.207) rather than answered by a lucky default.
package lang

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// theMathConstants: one row per constant the reference's `math` module carries, with this language's own
// spelling beside it and the answer both must give.
var theMathConstants = []struct {
	ref, own, want string
}{
	{"pi", "PI", "3.141592653589793\n"},
	{"e", "E", "2.718281828459045\n"},
	{"tau", "TAU", "6.283185307179586\n"},
}

// theMathExtensions are the names the reference's module does not have: asserted against the values, because
// there is no reference leg to ask about them.
var theMathExtensions = []struct {
	lower, upper, want string
}{
	{"phi", "PHI", "1.618033988749895\n"},
	{"sqrt2", "SQRT2", "1.4142135623730951\n"},
	{"ln2", "LN2", "0.6931471805599453\n"},
	{"ln10", "LN10", "2.302585092994046\n"},
}

// theMathQuestions are the positions a module constant can sit in that L11.6 had wrong at exit 0 — written
// through the reference's spelling, with the answer CPython gives for the SAME source.
var theMathQuestions = []struct{ name, src, want string }{
	{"the negation", "import math\nprint(-math.pi)\n", "-3.141592653589793\n"},
	{"times an integer", "import math\nprint(math.pi * 2)\n", "6.283185307179586\n"},
	{"bound, then compared", "import math\nx = math.pi\nprint(x > 3.14)\n", "True\n"},
	{"the digit count", "import math\nprint(round(math.pi, 2))\n", "3.14\n"},
	{"floored division", "import math\nprint(math.pi // 2)\n", "1.0\n"},
	{"the modulo", "import math\nprint(math.pi % 2)\n", "1.1415926535897931\n"},
	{"two of them in a container", "import math\nprint([math.pi, math.e])\n", "[3.141592653589793, 2.718281828459045]\n"},
	{"an f-string field", "import math\nprint(f\"{math.e}\")\n", "2.718281828459045\n"},
	{"the second constant negated", "import math\nprint(-math.e)\n", "-2.718281828459045\n"},
}

// TestTheReferenceSpellsItsMathConstantsAndTheCompiledLegAnswers is the reference leg: one source text, asked
// of CPython and of the compiled backend. The upper-case spelling is answered by the compiled leg only — the
// reference has no `math.PI` to compare it with, which is the reason the lower-case names exist.
func TestTheReferenceSpellsItsMathConstantsAndTheCompiledLegAnswers(t *testing.T) {
	for _, c := range theMathConstants {
		for _, spelling := range []string{c.ref, c.own} {
			src := "import math\nprint(math." + spelling + ")\n"
			t.Run(c.want+"as math."+spelling, func(t *testing.T) {
				compiled := compiledMathAnswer(t, src)
				if compiled != c.want {
					t.Errorf("compiled printed %q, want %q", compiled, c.want)
				}
				if spelling != c.ref {
					return
				}
				pyOut, pyErr, perr := PythonRun(src)
				if perr != nil || pyOut != c.want {
					t.Fatalf("the reference said %q (%v: %s), want %q — this row is only worth having because "+
						"CPython answers the same program", pyOut, perr, pyErr, c.want)
				}
			})
		}
	}
}

// TestAModuleConstantAnswersTheQuestionsL116Owned is the same reference leg through the positions that used to
// pour the double into an `i32`.
func TestAModuleConstantAnswersTheQuestionsL116Owned(t *testing.T) {
	for _, q := range theMathQuestions {
		t.Run(q.name, func(t *testing.T) {
			compiled := compiledMathAnswer(t, q.src)
			if compiled != q.want {
				t.Errorf("compiled printed %q, want %q", compiled, q.want)
			}
			pyOut, pyErr, perr := PythonRun(q.src)
			if perr != nil || pyOut != q.want {
				t.Fatalf("the reference said %q (%v: %s), want %q", pyOut, perr, pyErr, q.want)
			}
		})
	}
}

// TestTheExtensionsAnswerForBothSpellingsWithNoReferenceToAsk covers the four names the reference's module
// does not carry, on the leg that can answer them.
func TestTheExtensionsAnswerForBothSpellingsWithNoReferenceToAsk(t *testing.T) {
	for _, c := range theMathExtensions {
		for _, spelling := range []string{c.lower, c.upper} {
			src := "import math\nprint(math." + spelling + ")\n"
			t.Run(c.upper+" as math."+spelling, func(t *testing.T) {
				if got := compiledMathAnswer(t, src); got != c.want {
					t.Errorf("printed %q, want %q", got, c.want)
				}
			})
		}
	}
}

// TestTheTwoSpellingsOfAModuleConstantAreOneDeclaration checks the MODULE rather than the answer: `PI = pi`
// and not a second literal, so the two names cannot drift and the value is written in one place. A stdlib edit
// that copies the number under both names passes every answer test and is exactly the shape that rots.
func TestTheTwoSpellingsOfAModuleConstantAreOneDeclaration(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "stdlib", "math.gy"))
	if err != nil {
		t.Fatalf("read stdlib/math.gy: %v", err)
	}
	decl := regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.+?)\s*$`)
	declared := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := decl.FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("stdlib/math.gy has a line that is not a `name = value` declaration: %q", line)
		}
		if _, twice := declared[m[1]]; twice {
			t.Errorf("stdlib/math.gy declares %s twice", m[1])
		}
		declared[m[1]] = m[2]
	}
	literal := regexp.MustCompile(`^-?[0-9]+\.[0-9]+$`)
	// Which spelling holds the literal is the claim, and it differs by family: the reference's own name for the
	// three constants CPython has, and this language's documented name for the four it does not.
	for _, pair := range []struct{ canonical, alias string }{
		{"pi", "PI"}, {"e", "E"}, {"tau", "TAU"},
		{"PHI", "phi"}, {"SQRT2", "sqrt2"}, {"LN2", "ln2"}, {"LN10", "ln10"},
	} {
		v, ok := declared[pair.canonical]
		if !ok {
			t.Errorf("stdlib/math.gy does not declare %q at all", pair.canonical)
			continue
		}
		if !literal.MatchString(v) {
			t.Errorf("stdlib/math.gy's %q is %q — the canonical spelling is the one that is supposed to hold "+
				"the literal", pair.canonical, v)
		}
		if declared[pair.alias] != pair.canonical {
			t.Errorf("stdlib/math.gy's %q is %q, want the alias form `%s = %s` — one literal, two names, so the "+
				"two spellings cannot disagree (roadmap L11.6, ADR 0321)", pair.alias, declared[pair.alias], pair.alias, pair.canonical)
		}
	}
}

// theRecordedMathSources are the sources the corpus holds an answer for, both spellings, with the answer the
// record gives. The upper-case rows were recorded before this cycle; the lower-case rows went in through
// tools/recmerge, which asks CPython first and refuses to write an entry the compiled leg will not agree to —
// which is also why the four extension names are NOT in this table: the reference traps on `math.phi`, so
// the recorder rightly will not write an answer for it, and they are asserted on the compiled leg above.
var theRecordedMathSources = []struct{ src, want string }{
	{"import math\nprint(math.PI)\n", "3.141592653589793\n"},
	{"import math\nprint(math.E)\n", "2.718281828459045\n"},
	{"import math\nprint(math.pi)\n", "3.141592653589793\n"},
	{"import math\nprint(math.e)\n", "2.718281828459045\n"},
	{"import math\nprint(math.tau)\n", "6.283185307179586\n"},
	{"import math\nprint(math.pi * 2)\n", "6.283185307179586\n"},
	{"import math\nprint(-math.pi)\n", "-3.141592653589793\n"},
	{"import math\nx = math.pi\nprint(x > 3.14)\n", "True\n"},
	{"import math\nprint(round(math.pi, 2))\n", "3.14\n"},
	{"import math\nprint(math.pi // 2)\n", "1.0\n"},
	{"import math\nprint(math.pi % 2)\n", "1.1415926535897931\n"},
	{"import math\nprint([math.pi, math.e])\n", "[3.141592653589793, 2.718281828459045]\n"},
	{"import math\nprint(f\"{math.e}\")\n", "2.718281828459045\n"},
}

// TestTheRecordStillAnswersForTheNamesTheCorpusHolds is the record leg: a missing entry FAILS the case, so the
// names cannot be dropped from the stdlib without the suite noticing which answer went away.
func TestTheRecordStillAnswersForTheNamesTheCorpusHolds(t *testing.T) {
	for _, tc := range theRecordedMathSources {
		t.Run(strings.TrimRight(strings.TrimPrefix(tc.src, "import math\n"), "\n"), func(t *testing.T) {
			RecordedStdoutIs(t, tc.src, tc.want)
		})
	}
}

// TestAMathNameTheModuleDoesNotDeclareIsRefusedInWords keeps the new names from turning the module into a dict
// that can be probed: the refusal names what is missing, and the contract's exit 2 (ADR 0166) is nowhere in
// the table. `math.inf` and `math.nan` are on this list because the reference's module DOES have them — that
// absence is roadmap Gap R.207's row, and it stays a refusal in words until someone pays it.
func TestAMathNameTheModuleDoesNotDeclareIsRefusedInWords(t *testing.T) {
	for _, src := range []string{
		"import math\nprint(math.PI2)\n",
		"import math\nprint(math.pi_)\n",
		"import math\nprint(math.inf)\n",
		"import math\nprint(math.nan)\n",
	} {
		t.Run(src, func(t *testing.T) {
			_, err := Compile(src)
			if err == nil {
				t.Fatalf("the compiler accepted %q, which asks for a name the module does not declare", src)
			}
			msg := err.Error()
			if !strings.Contains(msg, "module attribute") {
				t.Errorf("the refusal does not name what is missing: %s", msg)
			}
			if strings.Contains(msg, "LLVM ERROR") || strings.Contains(msg, "does not match") {
				t.Errorf("the refusal is a broken module rather than a sentence: %s", msg)
			}
		})
	}
}

// compiledMathAnswer asks the compiled backend what it prints, and refuses to let an answer be compared with
// the record when the run did not answer: a non-zero code here is a refusal or a trap on a program the
// standard library exists to serve.
func compiledMathAnswer(t *testing.T, src string) string {
	t.Helper()
	res, err := JIT(src, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != 0 {
		t.Fatalf("exit %d on a program the standard library answers: %s", res.Code, res.Stderr)
	}
	return res.Output
}

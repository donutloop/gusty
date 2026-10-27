package lang

// Gap R.315 — a method's text parameter is a parameter.
//
// The row was measured by ADR 0365 and filed the same day: `class C: def fmt(self, s): return "x%s" % s`
// printed `x0`, `def rep_of_param(self, s): return s * 2` printed `0`, and both at **exit 0** — the
// @str_tab POSITION under the text, where the function twin of the same body printed `xhi` and `hihi`. Two
// roads were involved, and the pairing is the point: `funcDef` registers a text parameter in the table the
// print, `len`, `in`, `==`, formatting, repeat and char-method roads ask (`internedVars`), and
// `emitClassMethod` never did; `strArgKinds`/`strReturningFuncs` read the call sites of a `def`, and read no
// method at all. So the body could not know what its slot held, and the caller could not know what the
// answer was — one missing census, two wrong lines (Gap R.315, ADR 0366).
//
// The ladder is the one ADR 0273 sets for this family and the one `sequence_ops_test.go` uses: the **record
// leg** for the source, the **compiled leg** for the answer, and the **emitted IR** for the shape — plus the
// claim that made this a feature rather than a patch, that a method and a function built from the SAME body
// answer the SAME way. That claim is what fails if the two censuses are ever split again, which is why every
// row below is run twice.
//
// What the row leaves owed is filed, not smoothed over, and is pinned with today's answer beside the
// reference's in `TestWhatTheMethodAnswerStillOwesIsFiledNotFixed`: a verdict a method hands back prints as
// the number underneath it (**Gap R.316**), and a method named like a text method swallows the text-method
// call and recurses until the process dies (**Gap R.317**).
//
// @reftest python3

import (
	"fmt"
	"strings"
	"testing"
)

// methodTextBodies are the doors a text parameter meets. `want` is what `python3` prints for both spellings
// of the program — the method and the function — which is what makes the pairing an assertion rather than an
// accident: neither answer is quoted from this backend.
var methodTextBodies = []struct {
	name string
	body string
	want string
}{
	{"the parameter itself", "s", "hi\n"},
	{"the parameter repeated", "s * 2", "hihi\n"},
	{"the parameter repeated from the right", "2 * s", "hihi\n"},
	{"the parameter formatted", `"x%s" % s`, "xhi\n"},
	{"the parameter concatenated after", `s + "!"`, "hi!\n"},
	{"the parameter concatenated before", `"<" + s`, "<hi\n"},
	{"the parameter interpolated", `f"q{s}"`, "qhi\n"},
	{"a char method on the parameter", "s.upper()", "HI\n"},
	{"the trim family on the parameter", "s.strip()", "hi\n"},
	{"the parameter's length", "len(s)", "2\n"},
	{"a character out of the parameter", "s[0]", "h\n"},
	{"a slice of the parameter", "s[1:2]", "i\n"},
	{"the parameter rendered", "str(s)", "hi\n"},
	{"the parameter represented", "repr(s)", "'hi'\n"},
	{"replace on the parameter", `s.replace("h", "H")`, "Hi\n"},
	{"sandwiched between two literals", `"!" + s + "!"`, "!hi!\n"},
	{"the parameter joined to itself", "s + s", "hihi\n"},
	{"a formatted literal concatenated with it", `"%d" % 1 + s`, "1hi\n"},
}

func methodTextProgram(body string) string {
	return fmt.Sprintf("class C:\n    def m(self, s):\n        return %s\n\n\nprint(C().m(\"hi\"))\n", body)
}

func funcTextProgram(body string) string {
	return fmt.Sprintf("def m(s):\n    return %s\n\n\nprint(m(\"hi\"))\n", body)
}

// TestAMethodsTextParameterAnswersEveryDoorItsFunctionTwinAnswers is the feature. Each row compiles as a
// method and as a plain function with the same body; both must print the reference's answer, and the IR of
// the method form must carry the string printer rather than the number printer for the answer.
func TestAMethodsTextParameterAnswersEveryDoorItsFunctionTwinAnswers(t *testing.T) {
	t.Parallel()
	for _, r := range methodTextBodies {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			msrc, fsrc := methodTextProgram(r.body), funcTextProgram(r.body)
			// The record leg, where a record was ever taken for the source: the record and the
			// reference have to agree before the compiled leg is judged at all.
			if _, known := loadGolden(t)[msrc]; known {
				if got := goldenStdout(t, msrc); got != r.want {
					t.Fatalf("the record reads %q for the method form, want %q", got, msrc)
				}
			}
			if _, known := loadGolden(t)[fsrc]; known {
				if got := goldenStdout(t, fsrc); got != r.want {
					t.Fatalf("the record reads %q for the function form, want %q", got, fsrc)
				}
			}
			// The compiled legs. The claim is that a method and a function built from one body meet the SAME
			// door: a row the compiled backend cannot answer at all (a text method whose receiver the
			// compiler cannot read — the Gap I.2 door) is refused by BOTH spellings, and a row it can answer
			// is answered by both. A refusal on one leg only is the failure this row is about.
			fOut, fRefused := compiledOutOrRefusal(t, fsrc)
			mOut, mRefused := compiledOutOrRefusal(t, msrc)
			if fRefused || mRefused {
				if fRefused != mRefused {
					t.Fatalf("the function leg and the METHOD leg disagree about a program built from one body: "+
						"function refused=%v (%q), method refused=%v (%q) — the census that decides what the\n"+
						"parameter holds is reading one callable and not the other (Gap R.315, ADR 0366):\n%s",
						fRefused, fOut, mRefused, mOut, msrc)
				}
				for _, out := range []string{fOut, mOut} {
					if strings.Contains(out, "llc") || strings.Contains(out, "verify") {
						t.Fatalf("a refusal that reached the VERIFIER is exit 2, our bug, not the program's: %s", out)
					}
				}
				return
			}
			if fOut != r.want {
				t.Fatalf("the function leg printed %q, want %q\n%s", fOut, r.want, fsrc)
			}
			if mOut != r.want {
				t.Fatalf("the METHOD leg printed %q where the function leg prints %q — the two bodies are\n"+
					"the same program, so the census that decides what the parameter holds is reading one\n"+
					"callable and not the other (Gap R.315, ADR 0366):\n%s", mOut, r.want, msrc)
			}
			// The IR leg: the answer reaches a printer that reads an @str_tab index, and the intern
			// POSITION never travels to a `%d`.
			ir := mustCompileIR(t, msrc)
			if strings.Contains(ir, "call i32 (i8* %") && strings.Contains(ir, "@printf(i8* bitcast (i8* @.str") {
				t.Fatalf("the module still routes a method's text answer to printf's %%%%d leg: %s", msrc)
			}
		})
	}
}

// TestTheMethodCensusMarksTheArgumentPositionAndSkipsSelf is the off-by-one guard. `def m(self, s)` called as
// `z.m("hi")` puts the text in the callable's slot 1; reading it as slot 0 would mark `self` a text and leave
// `s` a number — the same trap boolargs.go names for the verdict census (Gap R.111), and the reason the two
// tables are written the same way.
func TestTheMethodCensusMarksTheArgumentPositionAndSkipsSelf(t *testing.T) {
	t.Parallel()
	prog, err := Parse("class C:\n    def m(self, s):\n        return s\n\n\nprint(C().m(\"hi\"))\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	kinds := strArgKinds(prog)
	if !kinds["m"][1] {
		t.Fatalf("the census did not mark position 1 (the text argument) of method m: %v", kinds)
	}
	if kinds["m"][0] {
		t.Fatalf("the census marked position 0 of method m — that is `self`, and marking it a text is how the\n"+
			"body's own `self` reads start printing interned text (Gap R.315, ADR 0366): %v", kinds)
	}
}

// TestTheMethodCensusRefusesToGuessWhenTwoCallablesShareAName is the contradiction half of the honesty rule:
// a method is classified only when ONE callable owns its name, because marking one from the other's call site
// would dress a number in the string table.
func TestTheMethodCensusRefusesToGuessWhenTwoCallablesShareAName(t *testing.T) {
	t.Parallel()
	one, err := Parse("class C:\n    def m(self, s):\n        return s\n\n\nprint(C().m(\"hi\"))\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strArgKinds(one)["m"][1] {
		t.Fatal("with one class owning the name, the census has to mark the parameter — the row is about answering")
	}
	two, err := Parse("class C:\n    def m(self, s):\n        return s\n\n\nclass D:\n    def m(self, s):\n        return s\n\n\n" +
		"print(C().m(\"hi\"))\nprint(D().m(2))\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := strArgKinds(two)["m"]; len(got) != 0 {
		t.Fatalf("two callables, one name: the census marked %v anyway, which is the answer that prints a\n"+
			"text through the number road for whichever class the call did not come from (ADR 0366)", got)
	}
	def, err := Parse("def m(s):\n    return s\n\n\nclass C:\n    def m(self, s):\n        return s\n\n\nprint(m(1))\nprint(C().m(\"hi\"))\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// One name, two callables: the table is keyed by the NAME alone, so marking it from the `def`'s
	// call site would mark the method's parameter too — which is how a number gets dressed in the
	// string table for whichever callable the call did not come from. The conservative answer is
	// neither, and the doors beside it stay honest: the function road refuses an unmarked text
	// argument in words (Gap J.5) and the method road refuses its own (the guard below).
	if got := strArgKinds(def)["m"]; len(got) != 0 {
		t.Fatalf("a `def` and a method share the name and the census marked %v — one key, two callables,\n"+
			"so one call site would classify the other's parameter (ADR 0366)", got)
	}
}

// TestATextIntoAnUnmarkedMethodParameterRefusesRatherThanAnswerTheIndex is the guard the registration needed
// beside it. A parameter some call site fills with a number is not a text parameter; handing that slot a text
// used to print the intern position at exit 0, where the FUNCTION road has refused the same shape in words
// since Gap J.5. A refusal is the honest answer to a program whose kinds the compiler cannot hold — and exit
// 1, never exit 2, and never the number underneath.
func TestATextIntoAnUnmarkedMethodParameterRefusesRatherThanAnswerTheIndex(t *testing.T) {
	t.Parallel()
	src := "class C:\n    def m(self, s):\n        return s * 2\n\n\nprint(C().m(\"hi\"))\nprint(C().m(2))\n"
	_, err := Compile(src)
	if err == nil {
		t.Fatalf("the mixed-kind method parameter answered rather than refusing — a text into a slot the body\n" +
			"reads as a number is the exit-0 wrong answer this guard exists to keep dead (Gap R.315)")
	}
	msg := err.Error()
	for _, want := range []string{"a text argument to method", "no compiled word", "Gap R.315"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the refusal does not name %q (it says %q); a refusal must be true of the compiler that\n"+
				"prints it and point at the row that owns the missing half", want, msg)
		}
	}
	if strings.Contains(msg, "exit 2") || strings.Contains(msg, "*lang.") {
		t.Fatalf("the refusal leaks the compiler's own machinery: %s", msg)
	}
	// An annotated parameter settles the question without the call sites, and answers.
	annot := "class C:\n    def m(self, s: str):\n        return s * 2\n\n\nprint(C().m(\"hi\"))\n"
	if got := compiledOut(t, annot); got != "hihi\n" {
		t.Fatalf("an annotated text parameter printed %q, want \"hihi\"", got)
	}
}

// TestAKeywordTextArgumentAndADefaultReachTheSameDoors covers the two spellings whose position the census
// cannot count off the argument list — a keyword names its parameter, a default names nothing.
func TestAKeywordTextArgumentAndADefaultReachTheSameDoors(t *testing.T) {
	t.Parallel()
	for _, r := range []struct {
		name, src, want string
	}{
		{
			"keyword argument",
			"class C:\n    def greet(self, name):\n        return \"hi \" + name\n\n\nprint(C().greet(name=\"ada\"))\n",
			"hi ada\n",
		},
		{
			"two text parameters",
			"class C:\n    def join2(self, a, b):\n        return a + b\n\n\nprint(C().join2(\"hi \", \"there\"))\n",
			"hi there\n",
		},
		{
			"a method hands its parameter to a function",
			"def shout(v):\n    return v + \"!\"\n\n\nclass C:\n    def outer(self, s):\n        return shout(s)\n\n\nprint(C().outer(\"hi\"))\n",
			"hi!\n",
		},
		{
			"the parameter of a method on a bound instance",
			"class C:\n    def shout(self, s):\n        return s.upper()\n\n\nc = C()\nprint(c.shout(\"hi\"))\n",
			"HI\n",
		},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			if _, known := loadGolden(t)[r.src]; known {
				if got := goldenStdout(t, r.src); got != r.want {
					t.Fatalf("the record reads %q, want %q — record and reference must agree first", got, r.want)
				}
			}
			if got := compiledOut(t, r.src); got != r.want {
				t.Fatalf("compiled %q, want %q\n%s", got, r.want, r.src)
			}
		})
	}
}

// TestTheMethodTextCensusLeavesNoShapeAnsweringTheWrongThing is the ladder's floor: for every shape this row
// touched, the compiled leg either prints the reference's answer or refuses in words at compile time — it may
// not print a number where the reference prints a text. That is the assertion ADR 0365 tightened for the
// sequence door, applied to the method road, and it is what makes "the census reads methods" a claim about
// behaviour rather than about a table.
func TestTheMethodTextCensusLeavesNoShapeAnsweringTheWrongThing(t *testing.T) {
	t.Parallel()
	for _, r := range []struct {
		name, src, want string
	}{
		{"repeat", methodTextProgram("s * 2"), "hihi\n"},
		{"format", methodTextProgram(`"x%s" % s`), "xhi\n"},
		{"concatenate", methodTextProgram(`s + "!"`), "hi!\n"},
		{"interpolate", methodTextProgram(`f"q{s}"`), "qhi\n"},
		{"char method", methodTextProgram("s.upper()"), "HI\n"},
		{"print the parameter", "class C:\n    def m(self, s):\n        print(s)\n\n\nC().m(\"hi\")\n", "hi\n"},
		{"membership in the parameter", "class C:\n    def m(self, s):\n        return \"h\" in s\n\n\nprint(C().m(\"hi\"))\n", "True\n"},
		{"the parameter's truthiness", "class C:\n    def m(self, s):\n        return bool(s)\n\n\nprint(C().m(\"hi\"))\n", "True\n"},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, r.src)
			if refused {
				if strings.Contains(got, "llc") || strings.Contains(got, "verify") {
					t.Fatalf("the shape was refused with a VERIFIER complaint, which is exit 2 and our bug: %s", got)
				}
				return // a refusal in words is honest; the row it owes is named in the sentence
			}
			if got != r.want {
				t.Fatalf("the compiled leg printed %q where the reference prints %q — a wrong value at exit 0 is\n"+
					"the outcome the ladder ranks worst (Gap R.315, ADR 0366):\n%s", got, r.want, r.src)
			}
		})
	}
}

// TestWhatTheMethodAnswerOnceOwedNowAnswers holds the three shapes ADR 0366 paid. Each was pinned to
// today's wrong answer in the table below; each now prints the reference's, and the assertion is that
// they KEEP doing so — a row that regresses re-opens the gap, and a row that stops answering has to be
// moved back with a reason.
//
// The verdict rows are the same registration seen from the calling side: the print road asks a `*Name`
// callee whether its body returns only verdicts (`bodyReturnsOnlyBools`) and asked no such question of
// an attribute callee, so `print(C().yes())` wrote the 0/1 a verdict is stored as where the reference
// writes True (Gap R.316, L15.4's remaining half). The field row is the getter: `return self.flag` is
// a verdict when every write of `flag` hands one. The recursion row is the dispatch: a receiver the
// compiler can see is a TEXT belongs to the text road, whatever the enclosing class calls itself, and
// resolving `s.upper()` to the class's own `upper` made every call call itself until the process died
// by SIGSEGV (Gap R.317, ADR 0166's artifact rule).
func TestWhatTheMethodAnswerOnceOwedNowAnswers(t *testing.T) {
	t.Parallel()
	for _, r := range []struct {
		name, src, want string
	}{
		{
			"a verdict a method hands back",
			"class C:\n    def yes(self):\n        return 1 == 1\n\n\nprint(C().yes())\n",
			"True\n",
		},
		{
			"a containment test a method hands back",
			"class C:\n    def has(self, s):\n        return \"h\" in s\n\n\nprint(C().has(\"hi\"))\n",
			"True\n",
		},
		{
			"a verdict held in a field, read out by a getter",
			"class C:\n    def __init__(self, flag):\n        self.flag = flag\n\n    def held(self):\n        return self.flag\n\n\nprint(C(True).held())\nprint(C(False).held())\n",
			"True\nFalse\n",
		},
		{
			"a verdict reached through `self`",
			"class C:\n    def literal(self):\n        return True\n\n    def via_self(self):\n        return self.literal()\n\n\nprint(C().via_self())\n",
			"True\n",
		},
		{
			"a method named like a text method, called on a text",
			"class C:\n    def upper(self, s):\n        return s.upper()\n\n\nc = C()\nprint(c.upper(\"hi\"))\n",
			"HI\n",
		},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledOut(t, r.src); got != r.want {
				t.Fatalf("the compiled leg printed %q, want %q — a row this cycle paid has come back", got, r.want)
			}
		})
	}
}

// TestWhatTheMethodAnswerStillOwesIsFiledNotFixed pins the shapes this cycle measured and did NOT fix,
// each with today's compiled answer beside the reference's, so a future cycle that pays one finds the
// row waiting rather than finding nothing. A wrong value at exit 0 is the outcome the ladder ranks
// worst, so it is pinned rather than left untested.
//
// The family they share is a method's answer whose kind lives in a FIELD (`self.w`) or in the caller's
// `self` (`self.inner(s)`): the text census has a verdict-shaped twin for the field reading
// (`methodCensusT.fieldIsVerdict`) and none for text, and `g.receiverClass` does not yet answer `self`.
// Both are refusals today or a fabricated number, and both are named in the sentences below.
func TestWhatTheMethodAnswerStillOwesIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, r := range []struct {
		name, src, today, reference, row string
	}{
		{
			"a text a constructor hands a field, read out by a getter",
			"class D:\n    def __init__(self, v):\n        self.v = v\n\n    def get(self):\n        return self.v\n\n\nprint(D(\"hi\").get())\n",
			"0\n", "hi\n", "Gap R.319",
		},
		{
			"a text attribute read beside a text parameter",
			"class C:\n    def __init__(self):\n        self.w = \"w\"\n\n    def both(self, s):\n        return self.w + s\n\n\nprint(C().both(\"hi\"))\n",
			"2\n", "whi\n", "Gap R.319",
		},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			got := compiledOut(t, r.src)
			if got == r.reference {
				t.Fatalf("%s has been PAID — the compiled leg prints %q, the reference's answer. Move the row\n"+
					"from this table to the answered ones and update roadmap %s", r.name, got, r.row)
			}
			if got != r.today {
				t.Fatalf("%s: the compiled leg changed shape (printed %q, pinned %q) — re-measure it and say\n"+
					"which row owns it now (roadmap %s)", r.name, got, r.today, r.row)
			}
		})
	}
	// The shapes below are refused in words rather than answered, and the guard is that they stay refusals —
	// never the intern position. `self.inner(s)` asks the dispatch for the receiver the body cannot
	// name, and a defaulted parameter that no call names arrives as a word nothing wrote. The last is the
	// one the reference refuses too, at the same exit code.
	for _, r := range []struct{ name, src string }{
		{
			"a method forwards its parameter to another method",
			"class C:\n    def outer(self, s):\n        return self.inner(s)\n\n    def inner(self, t):\n        return t + \"!\"\n\n\nprint(C().outer(\"hi\"))\n",
		},
		{
			"a text default no call names",
			"class C:\n    def greet(self, name=\"world\"):\n        return \"hi \" + name\n\n\nprint(C().greet())\n",
		},
		{
			// A constructor whose required parameter the program never passes. The argument-vector walk
			// (**ADR 0434**) reads the signature and refuses rather than hand the body a word nobody wrote,
			// and the reference agrees about the exit code: it raises
			// `TypeError: C.__init__() missing 1 required positional argument: 'v'`. This shape used to sit in
			// the table above claiming the reference prints `hi` — it does not. That claim was the retired
			// engine's answer, which allowed the call with the parameter unbound; where the record and the
			// reference disagree, the reference is right and the record is the debt (**ADR 0302**), and a row
			// may not pin a reference it never asked.
			"a constructor whose required parameter the program never passes",
			"class C:\n    def __init__(self, v):\n        self.v = \"hi\"\n\n    def get(self):\n        return self.v\n\n\nprint(C().get())\n",
		},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, r.src)
			if !refused {
				t.Fatalf("the shape %s printed %q where it used to refuse in words — if it now answers, the\n"+
					"row is paid: move it to the answered table and update the roadmap", r.name, out)
			}
			if strings.Contains(out, "llc") || strings.Contains(out, "verify") {
				t.Fatalf("a refusal that reached the VERIFIER is exit 2, our bug, not the program's: %s", out)
			}
		})
	}
}

package lang

// pkg/lang/leading_dot_float_test.go — the float the reference spells without an integer part is a LITERAL,
// and the lexer's previous-token rule is what separates it from the member operator (roadmap **Gap R.245**,
// **ADR 0464**).
//
// One character, five refusals:
//
//	print(.5)        →  parse error          CPython: 0.5
//	print(1+.5)      →  parse error          CPython: 1.5
//	print(-.5)       →  parse error          CPython: -0.5
//	print(.5+.5)     →  parse error          CPython: 1.0
//	x = .5           →  parse error          CPython: 0.5
//
// `lexNumber` had cases for a digit and for the `0x`/`0b`/`0o` prefixes, and none for a leading `.`. The
// obvious fix — accept `.` + digits wherever it appears — is wrong in a way the row names: `.` is also the
// MEMBER operator, so a rule that looked only at the next character would turn `obj.5` (which the reference
// refuses) into `obj` followed by a number, and `1 .5` into a float the reference never accepts. The fix
// therefore borrows **ADR 0338**'s walk: a `.` begins a literal exactly where what came before cannot END an
// expression — the same walk that decides whether `-1` is a sign or a subtraction — so the two rules cannot
// drift apart, and the token that comes out is an ordinary `TokFloat` with its own text and span.
//
// Nothing here claims a second engine ran. Rows are judged on the compiled leg, on the record leg
// (`HasGoldenAnswer` / `goldenStdout` over `testdata/interpreter-golden.json`), and — in
// `integration/leading_dot_float_test.go` — against a live `python3` read inside the case. The `.5` spelling
// and the `0.5` spelling are additionally required to answer IDENTICALLY, which is the assertion that keeps
// this row from becoming a second numeric road: two spellings, one value, one road.

import (
	"strings"
	"testing"
)

// leadingDotRows are the programs this row owns. `want` is what CPython writes for the same bytes; the
// integration twin re-reads those bytes from a live `python3` rather than trusting this file.
func leadingDotRows() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		// The five shapes the row measured as refusals.
		{"a bare leading-dot float", "print(.5)\n", "0.5\n"},
		{"a leading-dot float as an operand", "print(1+.5)\n", "1.5\n"},
		{"a negated leading-dot float", "print(-.5)\n", "-0.5\n"},
		{"two leading-dot floats summed", "print(.5+.5)\n", "1.0\n"},
		{"a leading-dot float bound at module level", "x = .5\nprint(x)\n", "0.5\n"},
		// The spellings that already worked must keep working — the row is an addition to the number grammar,
		// not a replacement of it.
		{"a leading-zero float is unchanged", "print(0.5)\n", "0.5\n"},
		{"a float with an integer part is unchanged", "print(1.5)\n", "1.5\n"},
		{"a float with a trailing dot is unchanged", "print(1.)\n", "1.0\n"},
		{"an underscore in a float is unchanged", "print(1_0.5)\n", "10.5\n"},
		{"a hex literal is unchanged", "print(0x1f)\n", "31\n"},
		// The positions a literal has to reach, so the row is not just `print` of one literal.
		{"a leading-dot float as an element", "print([.5, 1.5])\n", "[0.5, 1.5]\n"},
		{"a leading-dot float as a dict value", "d = {1: .5}\nprint(d)\n", "{1: 0.5}\n"},
		{"a leading-dot float as a call argument", "print(round(.5))\n", "0\n"},
		{"a leading-dot float tested by a condition", "if .5:\n    print(\"truthy\")\n\nelse:\n    print(\"falsy\")\n", "truthy\n"},
		{"a leading-dot float compared", "print(.5 > .4)\n", "True\n"},
		{"a leading-dot float asked of str", "print(str(.5))\n", "0.5\n"},
		{"a leading-dot float in an interpolation's field", "print(f\"{.5:.2f}\")\n", "0.50\n"},
		{"a leading-dot float in a comprehension's filter", "print([x for x in [1, 2] if x > .5])\n", "[1, 2]\n"},
		{"a leading-dot float in a dict slot read back", "d = {.5: \"half\"}\nprint(d[.5])\n", "half\n"},
		{"a leading-dot float scaled by another", "print(.5*.2)\n", "0.1\n"},
		{"an underscore inside a leading-dot float", "print(.5_1)\n", "0.51\n"},
		// A member access is NOT a float, and these two are the reason the gate exists: the reference refuses
		// both, and a gate that looked only at the character after the dot would accept them.
		{"an attribute written with a number is refused, as the reference refuses it", "obj = 1\nprint(obj.5)\n", ""},
		{"a float after a space beside a number is refused, as the reference refuses it", "print(1 .5)\n", ""},
		{"an underscore where a digit should lead", "print(._1)\n", ""},
	}
}

// leadingDotSpellings pairs the two ways the reference writes the same float, for the parity rows below.
func leadingDotSpellings() []struct{ name, dotted, zeroled string } {
	return []struct{ name, dotted, zeroled string }{
		{"print", "print(.5)\n", "print(0.5)\n"},
		{"sum", "print(1+.5)\n", "print(1+0.5)\n"},
		{"negated", "print(-.5)\n", "print(-0.5)\n"},
		{"bound and printed", "x = .5\nprint(x)\n", "x = 0.5\nprint(x)\n"},
		{"a list element", "print([.5, 1])\n", "print([0.5, 1])\n"},
		{"a dict key", "print({.5: \"h\"})\n", "print({0.5: \"h\"})\n"},
		{"an interpolation with a spec", "print(f\"{.5:.2f}\")\n", "print(f\"{0.5:.2f}\")\n"},
		{"a call argument", "print(round(.5))\n", "print(round(0.5))\n"},
		{"a comparison", "print(.5 > .4)\n", "print(0.5 > 0.4)\n"},
	}
}

// TestALeadingDotIsLexedAsTheFloatItSpells is the gate asked as a function (**ADR 0273**'s rule), because a
// behavioural table alone can be passed by a rule that accepts too much just as easily as by one that accepts
// too little — and the too-much side is exactly `obj.5`. The streams are the ones `tokenStream` renders, the
// same rendering **ADR 0338**'s minus rows are judged on: one rendering for "what did the lexer decide?".
func TestALeadingDotIsLexedAsTheFloatItSpells(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		src  string
		want string
		why  string
	}{
		{"print(.5)\n", "IDENT:print OP:( FLOAT:.5 OP:)", "the dot begins the literal, and the token keeps the program's own spelling"},
		{"print(1+.5)\n", "IDENT:print OP:( INT:1 OP:+ FLOAT:.5 OP:)", "after an operator the dot begins the literal"},
		{"print(-.5)\n", "IDENT:print OP:( OP:- FLOAT:.5 OP:)", "the minus stays the operator it is — ADR 0338's rule takes only a DIGIT into the literal — so `-.5` is the unary-minus road applied to the float"},
		{"x = .5\n", "IDENT:x OP:= FLOAT:.5", "after `=` the dot begins the literal"},
		{"print([.5])\n", "IDENT:print OP:( OP:[ FLOAT:.5 OP:] OP:)", "after `[` the dot begins the literal"},
		{"print(f\"{.5:.2f}\")\n", "IDENT:print OP:( FSTR:f\"{.5:.2f}\" OP:)", "an interpolation's spec is cut before the field is lexed, so the dot inside `.2f` is never a token"},
		// The member operator is unchanged: `.` after something that ends an expression is still `.`.
		{"print(obj.5)\n", "IDENT:print OP:( IDENT:obj OP:. INT:5 OP:)", "after a name the dot is the member operator, so this stays the reference's syntax error"},
		{"print(1 .5)\n", "IDENT:print OP:( INT:1 OP:. INT:5 OP:)", "after a number the dot is the member operator, and the reference refuses this too"},
		{"print(y.real)\n", "IDENT:print OP:( IDENT:y OP:. IDENT:real OP:)", "an ordinary attribute read"},
		// The `_` rules are the fraction's: a separator never begins the digits.
		{"print(.5_1)\n", "IDENT:print OP:( FLOAT:.5_1 OP:)", "an underscore between digits of the fraction"},
		{"print(._1)\n", "IDENT:print OP:( OP:. IDENT:_1 OP:)", "a dot that cannot begin a number stays the member operator, and `_1` is an identifier: the parser refuses the pair, as the reference does"},
	} {
		tc := tc
		t.Run(tc.src, func(t *testing.T) {
			t.Parallel()
			if got := tokenStream(t, tc.src); got != tc.want {
				t.Fatalf("token stream =\n  %s\nwant\n  %s\n(%s)\n%s", got, tc.want, tc.why, tc.src)
			}
		})
	}
}

// TestALeadingDotFloatCarriesTheFloatsOwnValue checks the token's PAYLOAD, which the stream rendering above
// prints but does not compare: `.5` must arrive as the double 0.5, and `-.5` as -0.5, with the sign taken by
// the minus road that already exists rather than by a new one.
func TestALeadingDotFloatCarriesTheFloatsOwnValue(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		src  string
		text string
		want float64
	}{
		{"print(.5)\n", ".5", 0.5},
		{"x = .5\n", ".5", 0.5},
		{"print(.5_1)\n", ".5_1", 0.51},
		{"print(.25)\n", ".25", 0.25},
		{"print(1+.5)\n", ".5", 0.5},
	} {
		tc := tc
		t.Run(tc.src, func(t *testing.T) {
			t.Parallel()
			toks, err := Lex(tc.src)
			if err != nil {
				t.Fatalf("Lex(%q): %v", tc.src, err)
			}
			var found *Token
			for i := range toks {
				if toks[i].Kind == TokFloat && toks[i].Text == tc.text {
					found = &toks[i]
				}
			}
			if found == nil {
				t.Fatalf("Lex(%q): no float token carrying %q", tc.src, tc.text)
			}
			if found.Float != tc.want {
				t.Fatalf("Lex(%q).Float = %v, want %v", tc.src, found.Float, tc.want)
			}
			if found.Span.Line != 1 || found.Span.Col == 0 {
				t.Fatalf("Lex(%q): the literal carries no usable span (%+v)", tc.src, found.Span)
			}
		})
	}
}

// TestANegatedLeadingDotFloatIsTheUnaryMinusRoadAppliedToTheFloat is the same fact from the other side: the
// lexer merges a `-` into a literal only when a DIGIT follows (**ADR 0338**), so `-.5` reaches the parser as an
// operator and a float, and the answer is written by the negation road that owns `bad operand type for unary -`.
// A lexer that merged `-.5` into one token would answer `-0.5` today and lose **Gap R.137**'s raise tomorrow.
func TestANegatedLeadingDotFloatIsTheUnaryMinusRoadAppliedToTheFloat(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		src, want, raises string
	}{
		{"print(-.5)\n", "-0.5\n", ""},
		{"print(- -.5)\n", "0.5\n", ""},
		{"print(- -.5 == .5)\n", "True\n", ""},
		// The negation road still owns a text: the raise is the reference's sentence, not a front-end refusal,
		// and this row did not move it.
		{"print(- \"hi\")\n", "", "bad operand type for unary -: 'str'"},
	} {
		tc := tc
		t.Run(strings.ReplaceAll(tc.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			if tc.raises != "" {
				// The raise is asked of the run, not of the front end: `ADR 0211` gives the trap its status, and
				// the sentence the negation road writes is on the tool channel rather than in RunSource's summary.
				res, err := JIT(tc.src, 0)
				if err != nil {
					t.Fatalf("the program did not reach a run: %v\n%s", err, tc.src)
				}
				if res.Code == 0 {
					t.Fatalf("a text's negation answered at exit 0 — the raise the negation road owns went missing\n%s", tc.src)
				}
				if !strings.Contains(res.Stderr, tc.raises) {
					t.Fatalf("the raise must say %q, said:\n%s\n%s", tc.raises, res.Stderr, tc.src)
				}
				return
			}
			if got := compiledRunIsTheBackend(t, tc.src); got != tc.want {
				t.Fatalf("compiled=%q reference=%q\n%s", got, tc.want, tc.src)
			}
		})
	}
}

// TestTheLeadingDotGateRefusesWhatTheReferenceRefuses pins the too-much side directly: these programs are
// syntax errors in CPython and must be front-end errors here, at the class the contract gives a program that
// never parsed — never **ADR 0166**'s exit 2, and never an answer.
func TestTheLeadingDotGateRefusesWhatTheReferenceRefuses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		src  string
		says string
	}{
		{"obj = 1\nprint(obj.5)\n", "parse error"},
		{"print(1 .5)\n", "parse error"},
		{"print(._1)\n", ""},
	} {
		tc := tc
		t.Run(strings.ReplaceAll(tc.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			_, err := RunSource(tc.src)
			if err == nil {
				t.Fatalf("the toolchain ran a program the reference refuses as a syntax error:\n%s", tc.src)
			}
			if strings.Contains(err.Error(), "llc") {
				t.Fatalf("the module was rejected (ADR 0166's exit 2) by a program the reference merely refuses: %v\n%s", err, tc.src)
			}
			if tc.says != "" && !strings.Contains(err.Error(), tc.says) {
				t.Fatalf("refusal must say %q, said: %v\n%s", tc.says, err, tc.src)
			}
		})
	}
}

// TestWhatALeadingDotFloatAnswersWhatTheReferencePrints is the compiled leg.
func TestWhatALeadingDotFloatAnswersWhatTheReferencePrints(t *testing.T) {
	t.Parallel()
	for _, tc := range leadingDotRows() {
		tc := tc
		if tc.want == "" {
			continue // the refused half lives in TestTheLeadingDotGateRefusesWhatTheReferenceRefuses
		}
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledRunIsTheBackend(t, tc.src); got != tc.want {
				t.Fatalf("compiled=%q reference=%q\n%s", got, tc.want, tc.src)
			}
		})
	}
}

// TestWhatALeadingDotFloatAnswersWhatTheRecordRecorded is the record leg. A source the record does not cover
// is SKIPPED, never deleted — the record cannot be widened from here — and the count of sources it does cover
// is asserted, so the leg cannot quietly stop being walked.
func TestWhatALeadingDotFloatAnswersWhatTheRecordRecorded(t *testing.T) {
	t.Parallel()
	asked := 0
	for _, tc := range leadingDotRows() {
		tc := tc
		if tc.want == "" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			// NOT parallel: the parent counts the rows this body walks, and a parallel subtest can still be
			// running when the parent answers.
			if !HasGoldenAnswer(tc.src) {
				t.Skipf("no record covers this program")
			}
			asked++
			want := goldenStdout(silentReporter{}, tc.src)
			if got := compiledRunIsTheBackend(t, tc.src); got != want {
				t.Fatalf("compiled=%q record=%q\n%s", got, want, tc.src)
			}
		})
	}
	if asked == 0 {
		t.Fatal("no row of this family is covered by the record: the record leg is not being walked")
	}
}

// TestALeadingDotFloatRefusesNothingItClaimsToAnswer: every answered row compiles, runs, and never reaches
// **ADR 0166**'s exit 2.
func TestALeadingDotFloatRefusesNothingItClaimsToAnswer(t *testing.T) {
	t.Parallel()
	for _, tc := range leadingDotRows() {
		tc := tc
		if tc.want == "" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if msg := compiledRefusalOf(tc.src); msg != "" {
				t.Fatalf("the toolchain refused a program the reference answers %q: %s\n%s", tc.want, msg, tc.src)
			}
			if _, err := RunSource(tc.src); err != nil && strings.Contains(err.Error(), "llc") {
				t.Fatalf("the module was rejected (ADR 0166's exit 2) for a program the reference answers: %v\n%s", err, tc.src)
			}
		})
	}
}

// TestTheTwoSpellingsOfTheSameFloatAnswerIdentically is the row's own safety net: `.5` and `0.5` are two ways
// of writing one number, so a second float road that drifted from the first would fail here rather than ship.
// It is also the cheapest statement of what the fix was NOT allowed to become — a literal that lexes and then
// means something slightly different.
func TestTheTwoSpellingsOfTheSameFloatAnswerIdentically(t *testing.T) {
	t.Parallel()
	for _, tc := range leadingDotSpellings() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dotted := compiledRunIsTheBackend(t, tc.dotted)
			zeroled := compiledRunIsTheBackend(t, tc.zeroled)
			if dotted != zeroled {
				t.Fatalf("the two spellings of the same float answered differently:\n  `.5` → %q\n  `0.5` → %q", dotted, zeroled)
			}
		})
	}
}

// TestTheExponentMarkerIsStillTheNeighbouringRow keeps this row honest about its own edge: `.5e2` is refused,
// and so is `1e5`, so the refusal is the exponent row (**Gap R.135**) and not a leading-dot hole. A rule that
// accepted `.5e2` while refusing `1e5` would be a half-implemented number grammar wearing this row's name.
func TestTheExponentMarkerIsStillTheNeighbouringRow(t *testing.T) {
	t.Parallel()
	for _, src := range []string{"print(.5e2)\n", "print(1e5)\n", "print(1.5e2)\n"} {
		src := src
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			_, err := RunSource(src)
			if err == nil {
				t.Fatalf("%s ran, but the exponent marker belongs to Gap R.135 — it must be refused WITH its siblings:\n%s", src, src)
			}
			if strings.Contains(err.Error(), "llc") {
				t.Fatalf("ADR 0166's exit 2 spent on a program the front end should refuse: %v\n%s", err, src)
			}
		})
	}
}

// TestWhatTheRecordSaysAboutTheFloatWithNoIntegerPart is the artifact guard for this row: the record is what
// the retired engine answered, plus what the reference answered for sources first asked after **ADR 0302**
// retired it — and 38 of those sources are this family (the five refusals, the positions a literal reaches, the
// parity spellings and the conformance program itself). The number is quoted from the file and compared to a
// literal, exactly as `TestTheArtifactTableCountsAreTheFilesOwn` does for the docs: a record that grows or shrinks
// without anyone saying so in the row that grew it fails here (roadmap **Gap R.204**).
func TestWhatTheRecordSaysAboutTheFloatWithNoIntegerPart(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	if got, want := countArtifact(t, root, countedArtifacts[0]), 10714; got != want {
		t.Fatalf("the record holds %d sources and this row's guard quotes %d — one of the two is stale; the file"+
			" is the authority, so the literal, AGENTS.md, docs/operations.md and roadmap.md's snapshot all move together",
			got, want)
	}
}

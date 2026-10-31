package lang

import (
	"strings"
	"testing"
)

// membership_set_needle_test.go — a set asked of a set ANSWERS; it does not refuse to hash itself.
// roadmap **Gap R.586**.
//
// Measured before this file existed, all at exit 3 with one sentence:
//
//	print({1} in {1, 2})      # False   in the reference;  this toolchain: TypeError: unhashable type: 'set'
//	print({1} in {1, 2, {1}}) # …the same
//	s = {1, 2}
//	print({1} not in s)       # True    …and the same under `not in`, in a condition, in `and`, in a `while`
//
// The sentence was half right and wholly misplaced. CPython refuses to hash a set — but a SET does not ask
// a set needle for its own hash: `set_contains` (and `set_discard`, `set_remove`) converts the needle to
// the **frozenset it hashes as** and looks THAT up. This language has no `frozenset` to find — a set cannot
// hold one, and `set()` and `{…}` are the only spellings of the empty one — so the conversion finds no
// member, and the answer is `False`. The unhashable rule belongs to the door beside it, where the reference
// really does raise: a dict asks the KEY's own hash, so `{1} in {1: 2}` dies `TypeError: unhashable type:
// 'set'`, and a needle that merely CARRIES a set is hashed element by element, so `(1, {2}) in {1, 2}`
// names 'set' too. Those two answers were already right, which is what makes this a placement bug rather
// than a missing rule: the run-time guard (`guardHashableNeedle`) had been carrying the set arm off since
// **ADR 0425** measured the identical asymmetry for `discard`/`remove`, and only the compile-time gate in
// front of it refused to know about it.
//
// So the fix is a question and a table, not a new road:
//
//   - the gate that fires the sentence is asked of the PAIR — `unhashableKeyGate(needle, haystack)` — and
//     the haystack, not the needle, decides which kinds it refuses; and
//   - the kinds are one table (`unhashableNeedleKinds`), read by the gate, by the store-side guard
//     (`guardHashableTag`) and by the run-time guard, so the raise and the answer cannot drift apart
//     (**ADR 0232**'s one question).
//
// Where the exemption may NOT be taken is the other half of the row, and it is asserted below rather than
// assumed. The kind of a NAME lives in the fourteen name-keyed notebooks the compiler keeps, and a notebook
// is a SUMMARY of the assignments a pass has seen, not a PROMISE about the value a read receives:
//
//	s = {"a": 1}
//	if no():
//	    s = {1, 2}
//	print({1} in s)   # the run is standing in the other arm: the haystack IS a dict, and raises
//
// A door that read the last-emitted claim would answer `False` there, which is a wrong answer at the exit
// code of success — the trade **ADR 0452** and **ADR 0453** both refuse to make. The exemption therefore
// runs on `programPromisesASet`: the spelled literal at the question, a name whose EVERY binder this frame
// spelled as a set literal behind the read, or a name the source never binds at all (a compiler-minted
// slot, whose kind only the minting road wrote). Everything else keeps the raise it has always kept, and is
// pinned in the residue table with the reference's answer beside it.
//
// Every `want` below is CPython's, re-derived from a live `python3` inside each case; the CLI half is
// `integration/membership_set_needle_test.go` and the one-program witness is
// `integration/programs/membership_set_needle.gy`.

// setNeedleAnswerRows is the family: a bare set asked of a set, at every position a verdict reaches.
type setNeedleRow struct {
	name string
	src  string
	want string // CPython's stdout, and the compiled leg's
}

var setNeedleAnswerRows = []setNeedleRow{
	// The row's own shape, and its three spellings: both sides spelled, one side named, neither named.
	{"the row's own program", "print({1} in {1, 2})", "False"},
	{"a spelled needle, a named haystack", "s = {1, 2}\nprint({1} in s)", "False"},
	{"a named needle, a spelled haystack", "n = {1}\nprint(n in {1, 2})", "False"},
	{"both sides named", "n = {1}\ns = {1, 2}\nprint(n in s)", "False"},
	{"one member", "print({1} in {1})", "False"},
	{"no overlap at all", "print({1} in {2, 3})", "False"},
	{"an empty haystack", "print({1} in set())", "False"},
	{"an empty needle", "print(set() in {1, 2})", "False"},
	{"a needle of two members", "n = {1, 2}\ns = {1, 2}\nprint(n in s)", "False"},
	{"two names, same content", "a = {1, 2}\nb = {1, 2}\nprint(a in b)", "False"},

	// The payload collision the TAG is there to prevent (ADR 0232's soundness rule): a set needle's payload
	// is a heap handle, a handle is a small number, and a set of small numbers is exactly where a payload-only
	// scan would find a member that was never stored.
	{"a handle beside the number it could be mistaken for", "s = {0}\nprint({0} in s)", "False"},
	{"a handle beside zero", "s = {0}\nn = {1}\nprint(n in s)", "False"},
	{"a handle beside the handle's own range", "s = {0, 1, 2, 3, 4, 5}\nprint({1} in s)", "False"},
	{"a tuple member, which is not a set", "s = {(1, 2)}\nprint({1} in s)", "False"},

	// `not in` is the same door with the verdict flipped, and a verdict reaches further than a print.
	{"negated", "s = {1, 2}\nprint({1} not in s)", "True"},
	{"both verdicts", "s = {1, 2}\nprint({1} in s, {1} not in s)", "False True"},
	{"in a condition", "s = {1, 2}\nif {1} in s:\n    print(\"yes\")\nelse:\n    print(\"no\")", "no"},
	{"in a while, which never runs", "s = {1, 2}\nwhile {1} in s:\n    print(\"in\")\n    break\nprint(\"done\")", "done"},
	{"in and/or", "print({1} in {1, 2} and 1 or 2)", "2"},
	{"in a ternary", "s = {1, 2}\nprint((\"yes\" if {1} in s else \"no\"))", "no"},
	{"through a parameter", "s = {1, 2}\ndef f(x):\n    return x in s\nprint(f({1}))", "False"},

	// A haystack the program did not write at the question: the call's container answer is materialised under
	// the rooted slot a container variable occupies, and THAT name's kind was written by the minting road —
	// which is the one kind of name this door trusts without a binder to read.
	{"a haystack that is a call's set", "def g():\n    return {1, 2}\nprint({1} in g())", "False"},
	{"a needle that is a call's set", "def f():\n    return {1}\nprint(f() in {1, 2})", "False"},

	// The order the reference keeps: an operand's effect runs before the answer is decided, and the exemption
	// must not delete it. Both legs print the marker, then the verdict.
	{"the haystack's own call still runs", "def hs():\n    print(\"build\")\n    return {1, 2}\nprint({1} in hs())", "build\nFalse"},
	{"the needle's own call still runs", "def nz():\n    print(\"needle\")\n    return {1}\nprint(nz() in {1, 2})", "needle\nFalse"},

	// The exemption is the SET haystack's. A list walks and compares contents, so the same needle is a member
	// of a list that holds it — and an answer of False there would be the door reaching past its exemption.
	{"a set asked of a list that holds it", "print({1} in [{1}])", "True"},
	{"a set asked of a list that does not", "print({1} in [2])", "False"},
	{"a set asked of a built list", "xs = [{1}, {2}]\nprint({1} in xs)", "True"},
	{"a set asked of a list slot", "xs = [{1}, {2}]\nprint(xs[0] in xs)", "True"},
	{"a set asked of a mixed list", "s = {1, 2}\nn = {1}\nprint([n in s, {1} in s, n in [1], n in [n]])", "[False, False, False, True]"},
	// A name rebound from a set to a LIST: the gate is not even asked (a list walks, it does not hash), which
	// is the shape that keeps this row's widening from costing a program that answers today.
	{"a set asked of a name rebound from a set to a list", "s = {1, 2}\ns = [1]\nprint({1} in s)", "False"},

	// The kinds the reference DOES refuse to hash stayed refusals-to-hash, in the same family of programs —
	// asserted as answers here only where the reference answers.
	{"a plain member", "print(1 in {1, 2})", "True"},
	{"a text member", "print(\"a\" in {\"a\"})", "True"},
	{"a tuple needle", "print((1,) in {1, 2})", "False"},
}

// TestASetAskedOfASetAnswers runs the family against the reference in the case itself: the compiled leg and
// CPython must print the same bytes, for the same source.
func TestASetAskedOfASetAnswers(t *testing.T) {
	t.Parallel()
	for _, tt := range setNeedleAnswerRows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := strings.TrimRight(compiledOut(t, tt.src), "\n")
			if got != tt.want {
				t.Fatalf("%q: compiled printed %q, want %q", tt.src, got, tt.want)
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

// TestASetAskedOfASetIsJudgedOnTheRecordLeg too: every answered source has an answer ON RECORD, so a later
// cycle cannot take the door away without taking the evidence away with it (**ADR 0302**'s rule that a
// missing record fails the case rather than passing quietly).
func TestASetAskedOfASetIsJudgedOnTheRecordLeg(t *testing.T) {
	t.Parallel()
	for _, tt := range setNeedleAnswerRows {
		t.Run(tt.name, func(t *testing.T) {
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

// setNeedleRaiseRows is the half of the rule that must NOT be relaxed with the exemption: a needle the
// reference really does refuse to hash. Each row is the class and message the program raises with, read off
// the traceback the compiled program prints (and the reference's own, which the case re-derives).
var setNeedleRaiseRows = []struct {
	name    string
	src     string
	class   string
	message string
}{
	{"a list asked of a set", "print([1] in {1, 2})", "TypeError", "unhashable type: 'list'"},
	{"a named list asked of a set", "n = [1]\ns = {1, 2}\nprint(n in s)", "TypeError", "unhashable type: 'list'"},
	{"a dict asked of a set", "print({1: 2} in {1, 2})", "TypeError", "unhashable type: 'dict'"},
	{"a set asked of a dict", "print({1} in {1: 2})", "TypeError", "unhashable type: 'set'"},
	{"a set asked of a named dict", "d = {1: 2}\nprint({1} in d)", "TypeError", "unhashable type: 'set'"},
	{"a named set asked of a named dict", "n = {1}\nd = {1: 2}\nprint(n in d)", "TypeError", "unhashable type: 'set'"},
	// The kind-keyed discipline (ADR 0476's rule one door over): the exemption belongs to a needle whose OWN
	// kind is a set. A tuple carries one, and CPython hashes a tuple by hashing its elements — so the inner
	// set names itself. A blanket "is this a container" test would exempt this program and lose the error class.
	{"a tuple carrying a set, asked of a set", "print((1, {2}) in {1, 2})", "TypeError", "unhashable type: 'set'"},
	{"a tuple carrying a list, asked of a set", "print(([1],) in {1, 2})", "TypeError", "unhashable type: 'list'"},
	{"a set asked of a set through `not in`", "s = {1, 2}\nprint([1] not in s)", "TypeError", "unhashable type: 'list'"},
	{"a dict asked of a named set", "s = {1, 2}\nprint({1: \"a\"} in s)", "TypeError", "unhashable type: 'dict'"},
}

// TestANeedleTheReferenceRefusesToHashStillRaises pins the raises on both legs: this backend's traceback
// class and message, and the reference's, from the same source.
func TestANeedleTheReferenceRefusesToHashStillRaises(t *testing.T) {
	t.Parallel()
	for _, tc := range setNeedleRaiseRows {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			class, message := jitTrap(t, tc.src)
			if class != tc.class || message != tc.message {
				t.Fatalf("%q raises %s: %s, the row says %s: %s", tc.src, class, message, tc.class, tc.message)
			}
			_, stderr, err := PythonRun(tc.src + "\n")
			if err == nil {
				t.Fatalf("%q: the reference was expected to raise and it answered", tc.src)
			}
			if !strings.Contains(stderr, tc.class+": "+tc.message) {
				t.Fatalf("the reference does not raise %s: %s for %q:\n%s", tc.class, tc.message, tc.src, stderr)
			}
		})
	}
}

// The raises belong to the PROGRAM (ADR 0214): the exemption may not turn one into an answer, and a raise it
// leaves standing stays catchable by the program's own `except`.
func TestAHashSetNeedleRaiseIsTheProgramsAndCatchable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{"a list needle caught", "try:\n    print([1] in {1, 2})\nexcept TypeError:\n    print(\"caught\")", "caught"},
		{"a set key caught", "try:\n    print({1} in {1: 2})\nexcept TypeError:\n    print(\"caught\")", "caught"},
		{"a tuple-carried set caught", "try:\n    print((1, {2}) in {1, 2})\nexcept TypeError:\n    print(\"caught\")", "caught"},
		// …and the exemption is not a raise in disguise: the catchable shape prints its own verdict, and the
		// handler never runs.
		{"the exempt pair does not reach the handler", "try:\n    print({1} in {1, 2})\nexcept TypeError:\n    print(\"caught\")", "False"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := strings.TrimRight(compiledOut(t, tc.src), "\n")
			if got != tc.want {
				t.Fatalf("%q printed %q, want %q", tc.src, got, tc.want)
			}
			want, _, err := PythonRun(tc.src + "\n")
			if err != nil {
				t.Fatalf("the reference could not run the program: %v", err)
			}
			if strings.TrimRight(want, "\n") != tc.want {
				t.Fatalf("the reference prints %q for %q, the row says %q", strings.TrimRight(want, "\n"), tc.src, tc.want)
			}
		})
	}
}

// The IR shape the row turns on. The exemption is not a folded-in verdict bolted beside the door: the
// program still asks the members, with the kind beside every payload — and the sentence the row retires is
// gone from the module that needs it gone and STILL PRESENT in the one that does not.
func TestTheHashSetNeedleIsAskedOfTheMembersAndNotOfTheGate(t *testing.T) {
	t.Parallel()
	ir := emittedModuleIR(t, "print({1} in {1, 2})")
	for _, want := range []string{"call i32 @rt_contains_tagged(i32 %"} {
		if !strings.Contains(mainBodyOf(ir), want) {
			t.Fatalf("the answered shape never asks the object (%q missing):\n%s", want, mainBodyOf(ir))
		}
	}
	// The sentence the row retires is gone from the PROGRAM's own bodies. The runtime block carries the
	// unhashable sentence whether or not the program can ever raise it — every container store helper names it
	// — so a whole-module check would report the runtime's vocabulary as the program's trap. The census is the
	// one every other IR row in this package uses: the program's @main/@gy_* bodies, never the runtime.
	if got := programRaisesUnhashable(ir); got != "" {
		t.Fatalf("the answered shape still raises %s \u2014 the gate refuses what the door answers:\n%s", got, mainBodyOf(ir))
	}
	// The same sentence, in the module that must keep it. This is the half that makes the exemption a
	// placement rather than a deletion: widen the gate back and this row fails.
	kept := emittedModuleIR(t, "print({1} in {1: 2})")
	if got := programRaisesUnhashable(kept); got != "unhashable type: 'set'" {
		t.Fatalf("a set asked of a DICT raises %q, want the reference's sentence\n%s", got, mainBodyOf(kept))
	}
	if !strings.Contains(mainBodyOf(kept), "br label %main.raiseexit") {
		t.Fatalf("a set asked of a DICT no longer leaves through the program's own exception road:\n%s", mainBodyOf(kept))
	}
	// A list needle asked of a set keeps its own sentence too, in the same module family.
	list := emittedModuleIR(t, "print([1] in {1, 2})")
	if got := programRaisesUnhashable(list); got != "unhashable type: 'list'" {
		t.Fatalf("a list asked of a set raises %q, want the reference's sentence\n%s", got, mainBodyOf(list))
	}
	// …and the exemption did not turn the answered shape into a compile-time verdict: the members are still
	// what answers, with the kind beside every payload.
	if strings.Contains(mainBodyOf(ir), "rt_dict_has(") || strings.Contains(mainBodyOf(ir), "rt_str_contains(") {
		t.Fatalf("the answered shape asks a door that compares payloads without their tags:\n%s", mainBodyOf(ir))
	}
}

// The table is the artifact the three doors read (**ADR 0232**), so it is asserted as one: two readings, the
// kinds nameable apart, and the set arm present exactly once — in the reading that has it.
func TestTheUnhashableNeedleTableIsTheOneArtifactThreeDoorsRead(t *testing.T) {
	t.Parallel()
	setHaystack := unhashableNeedleKinds(true)
	dictKey := unhashableNeedleKinds(false)
	names := func(kinds []unhashableNeedleKind) []string {
		out := make([]string, 0, len(kinds))
		for _, k := range kinds {
			out = append(out, k.name)
		}
		return out
	}
	if got := strings.Join(names(dictKey), ","); got != "list,dict,set" {
		t.Fatalf("a dict key refuses [%s]; the reference refuses [list,dict,set]", got)
	}
	if got := strings.Join(names(setHaystack), ","); got != "list,dict" {
		t.Fatalf("a set haystack refuses [%s]; the reference refuses [list,dict] and CONVERTS a set", got)
	}
	// The tags the guards test have to be the tags the objects write, or a guard tests a kind that no value
	// ever carries and the raise silently disappears from a program the reference stops.
	want := map[string]int32{"list": int32(TagList), "dict": int32(TagDict), "set": int32(TagSet)}
	seen := map[string]bool{}
	for _, k := range dictKey {
		if want[k.name] != k.tag {
			t.Fatalf("%q is labelled with tag %d, the objects write %d", k.name, k.tag, want[k.name])
		}
		if seen[k.name] {
			t.Fatalf("%q is in the table twice", k.name)
		}
		seen[k.name] = true
	}
	for _, k := range setHaystack {
		if !seen[k.name] || want[k.name] != k.tag {
			t.Fatalf("the set reading carries %q/%d, which is not a subset of the dict reading", k.name, k.tag)
		}
	}
	if len(setHaystack) != 2 || len(dictKey) != 3 {
		t.Fatalf("the set reading must be the dict reading minus the converted kind (%d, %d)", len(setHaystack), len(dictKey))
	}
}

// The family a literal spells, asked directly: the exemption's first reading, and the reason a tuple-spelled
// list is never mistaken for a set (which is the difference between answering `{1} in {1, 2}` and answering
// `(1, {2}) in {1, 2}`, one of which the reference stops).
func TestTheFamilyALiteralSpellsIsAskedOfTheLiteral(t *testing.T) {
	t.Parallel()
	g := &irGen{}
	for _, tc := range []struct {
		e      Expr
		family string
		why    string
	}{
		{&SetLit{Elems: []Expr{&IntLit{Value: 1}}}, "set", "a set literal"},
		{&SetLit{}, "set", "the empty set literal"},
		{&ListLit{Elems: []Expr{&IntLit{Value: 1}}}, "list", "a list literal"},
		{&DictLit{}, "dict", "a dict literal"},
		{&Tuple{Elems: []Expr{&IntLit{Value: 1}}}, "tuple", "a tuple"},
		{&IntLit{Value: 1}, "", "a number, which spells no family"},
		{&Name{Value: "xs"}, "", "a name, which the PROGRAM settles rather than the spelling"},
	} {
		if got := containerLiteralFamily(g, tc.e); got != tc.family {
			t.Fatalf("%s: containerLiteralFamily = %q, want %q", tc.why, got, tc.family)
		}
	}
	if !g.programPromisesASet(&SetLit{}, 0) {
		t.Fatal("a spelled set literal does not carry the promise it spells")
	}
	if g.programPromisesASet(&Tuple{Elems: []Expr{&SetLit{}}}, 0) {
		t.Fatal("a tuple CARRYING a set carries the promise too — the reference hashes its elements and raises")
	}
	if g.programPromisesASet(&Name{Value: "s"}, 1) {
		t.Fatal("a name carries the promise with no frame behind it — that reading is a notebook, not a promise")
	}
}

// The promise is asked of the PROGRAM, which means it is asked of a whole frame: every binder the name has
// has to spell a set, or the exemption is off and today's raise stays. These programs are one line apart,
// and they are the whole of the soundness argument — the first three answer, the fourth and fifth raise
// exactly as the reference raises, and the last is the residue the door cannot settle (pinned in the residue
// table, and owned by roadmap Gap R.588).
func TestTheSetPromiseIsAskedOfEveryBinderTheNameHas(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		src        string
		want       string // the compiled answer — stdout, or the exception class when trap is set
		trap       bool   // the compiled answer is a raise
		oracleTrap bool   // the reference raises too, so the raise is not a divergence
	}{
		{"one binder, behind the read", "s = {1, 2}\nprint({1} in s)", "False", false, false},
		{"two binders, both sets", "s = {1, 2}\ns = {3}\nprint({1} in s)", "False", false, false},
		{"three binders, all sets", "s = {1}\ns = {2}\ns = {3}\nprint({1} in s)", "False", false, false},
		// A name whose second binder spells another family is no longer a promise about anything. Asked of a
		// DICT the gate fires and the raise is the reference's own; asked of a LIST the gate is never reached,
		// which is the answer row the table above carries for it.
		{"a second binder spells a dict", "s = {1, 2}\ns = {\"a\": 1}\nprint({1} in s)", "TypeError", true, true},
		// A binder inside an `if` arm is a run-time fact: "it ran" is not nothing to decide, and the door that
		// guesses here is the door Gap R.582 and Gap R.583 were filed against.
		{"a binder inside an arm", "def no():\n    return False\ns = {1, 2}\nif no():\n    s = {\"a\": 1}\nprint({1} in s)", "TypeError", true, false},
		// A loop target that reuses the name is a binder the pass counts, which retires the promise even though
		// every value the loop hands the name is a set — the reference answers, this backend declines, and the
		// residue table owns that difference (Gap R.588).
		{"a loop binds the same name", "s = {1}\nfor s in [{2}]:\n    print({1} in s)", "TypeError", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.trap {
				class, message := jitTrap(t, tc.src)
				if class != tc.want {
					t.Fatalf("%q raises %s: %s, want a %s", tc.src, class, message, tc.want)
				}
				_, stderr, err := PythonRun(tc.src + "\n")
				if tc.oracleTrap && err == nil {
					t.Fatalf("the reference was expected to raise for %q", tc.src)
				}
				if !tc.oracleTrap && err != nil {
					t.Fatalf("%q: the compiled leg raises %s: %s where the reference answers — that divergence is a "+
						"row, not a table cell (see the residue table)\n%s", tc.src, class, message, stderr)
				}
				return
			}
			got := strings.TrimRight(compiledOut(t, tc.src), "\n")
			if got != tc.want {
				t.Fatalf("%q printed %q, want %q", tc.src, got, tc.want)
			}
			want, _, err := PythonRun(tc.src + "\n")
			if err != nil {
				t.Fatalf("the reference could not run the program: %v", err)
			}
			if strings.TrimRight(want, "\n") != tc.want {
				t.Fatalf("the reference prints %q, the row says %q", strings.TrimRight(want, "\n"), tc.want)
			}
		})
	}
}

// What the door still cannot reach, pinned with today's answer beside the reference's, so the row fails the
// day the door grows — and the day one of these answers drifts without a row owning it.
func TestWhatTheHashSetNeedleDoorStillCannotReachIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		src      string
		compiled string // what this backend answers today ("" = it refuses)
		oracle   string // what CPython answers
		gap      string
		raises   bool // the compiled answer is a raise, read off the traceback
	}{
		{
			// A binder the pass cannot see is a run-time fact, and the promise reader declines — so the raise
			// stands where the reference answers. The tag the value carries is the missing word (L11.1), which
			// is why this is a row rather than a wider gate.
			"a haystack bound in an arm is a set at run time and the raise is left standing",
			"def no():\n    return False\ns = {1, 2}\nif no():\n    s = {\"a\": 1}\nprint({1} in s)",
			"TypeError: unhashable type: 'set'", "False",
			"roadmap Gap R.588 (the promise covers a name the source settles; a name bound in an arm needs the tag that travels with the value, roadmap L11.1)",
			true,
		},
		{
			// The other face of the same declined promise: a loop that binds the name hands it a set every
			// time, and the binder walk cannot see that, so the raise stands where the reference answers.
			"a loop binds the name the question is asked of",
			"s = {1}\nfor s in [{2}]:\n    print({1} in s)",
			"TypeError: unhashable type: 'set'", "False",
			"roadmap Gap R.588 (the object's own kind word is the missing read; roadmap L11.1's tagged value)",
			true,
		},
		{
			// A parameter used as the needle is kinded per BODY, so the second call with another kind reads the
			// first call's word. Two one-call functions answer correctly, which is what says the defect is the
			// parameter's kind and not the door. The single-print spelling is the pin only so the row compares
			// one line; the two-print program behaves the same way.
			"a needle that is a parameter is kinded by one call site",
			"s = {1, 2}\ndef holds(needle):\n    return needle in s\nprint([holds(1), holds({1})])",
			"[False, False]", "[True, False]",
			"roadmap Gap R.589 (the every-call-site-or-nothing census the str and float censuses enforce, asked of the membership needle; Gap R.164 / ADR 0280's family)",
			false,
		},
		{
			// `set(<container>)` is the copy door, not this one: the refusal is the registry's own sentence and
			// predates the needle question.
			"a set built by copying a container never reaches the needle",
			"print({1} in set([1, 2]))",
			"", "False",
			"the `set(<container>)` copy door (the capability registry's copy family, roadmap L11.8)",
			false,
		},
		{
			// The chain, not the membership: `{1} in s == False` is a comparison chain whose operand is a
			// container, and the refusal names the tagged word.
			"a set verdict chained against a container",
			"s = {1, 2}\nprint({1} in {1, 2} == False)",
			"", "False",
			"roadmap Gap R.53 / L11.1 (a container operand of a comparison chain)",
			false,
		},
		{
			// A haystack the compiler cannot read is a different door's: `d.keys()` over a name.
			"a keys() view of a named container",
			"d = {1: 2}\nprint({1} in d.keys())",
			"", "TypeError: unhashable type: 'set'",
			"roadmap Gap R.174 / L11.1 (container methods over a name the runtime owns)",
			false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.raises {
				class, message := jitTrap(t, tc.src)
				if got := class + ": " + message; got != tc.compiled {
					t.Fatalf("%q raises %q today and the row records %q — either the door grew (close %s) or the "+
						"raise drifted without a row owning it", tc.src, got, tc.compiled, tc.gap)
				}
				return
			}
			out, refused := compiledOutOrRefusal(t, tc.src)
			if tc.compiled == "" {
				if !refused {
					t.Fatalf("%q answered %q; this row is filed because the door has no arm for it\n%s", tc.src, out, tc.gap)
				}
				return
			}
			if refused || strings.TrimRight(out, "\n") != tc.compiled {
				t.Fatalf("%q answered %q, the row records %q (%s)", tc.src, out, tc.compiled, tc.gap)
			}
		})
	}
}

// The other direction of the same sweep, and a bigger one than this row: ask a NON-text whether a TEXT
// contains it and the compiled leg has no answer at all. It is not this door's (the haystack here hashes, a
// text searches), it predates this commit — every face below behaves identically at the parent — and it is
// filed here because the sweep that closed Gap R.586 measured it and left nothing else written down.
//
//	class              CPython                                   this toolchain, today
//	{1} in "abc"       TypeError: 'in <string>' requires … 'set'  llc rejects the module (EXIT 2)
//	[1] in "abc"       …not list                                  llc rejects the module (EXIT 2)
//	{1: 2} in "abc"    …not dict                                  llc rejects the module (EXIT 2)
//	(1,) in "abc"      …not tuple                                 True, exit 0
//	{n} in "abc"       …not set                                   True, exit 0
//	1 in "abc"         …not int                                    SIGSEGV in the compiled program
//	1.5 / True         …not float / not bool                      SIGSEGV
//	None in "abc"      …not NoneType                              True, exit 0
//
// The three exit-2 faces are one bad call: the container literal lowers to its GLOBAL and is handed to
// `rt_str_contains(i32, i32)` as an `i32 @.setN`, which is the module `llc` cannot parse — **ADR 0166**'s
// code, spent on a program the reference stops with a sentence. The wrong-True and SIGSEGV faces are the
// same road without a global to trip over. Pinned as an IR shape, because an exit 2 cannot be asserted as a
// value and a fix that emits the raise instead makes this row fail in the right direction.
func TestANonTextNeedleAskedOfATextHasNoAnswerYetIsFiled(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"a set asked of a text", "print({1} in \"abc\")"},
		{"a list asked of a text", "print([1] in \"abc\")"},
		{"a dict asked of a text", "print({1: 2} in \"abc\")"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ir := emittedModuleIR(t, tc.src)
			if !strings.Contains(ir, "call i32 @rt_str_contains(i32 %") || !strings.Contains(ir, "i32 @.") {
				t.Fatalf("%q no longer hands a container GLOBAL to `rt_str_contains` — the door grew (close roadmap "+
					"Gap R.587 and move the row) or the shape changed without a row owning it:\n%s", tc.src, ir)
			}
			_, stderr, err := PythonRun(tc.src + "\n")
			if err == nil || !strings.Contains(stderr, "TypeError: 'in <string>' requires string as left operand") {
				t.Fatalf("the reference no longer refuses %q (%v):\n%s", tc.src, err, stderr)
			}
		})
	}
}

// programRaisesUnhashable names the unhashable sentence the PROGRAM raises, or "" when it raises none. It
// reads the module the way the other IR rows do — the program's own @main/@gy_* bodies (`programBodies`,
// heap_guard_test.go), where a runtime helper's own vocabulary is not the program's behaviour — and matches
// the sentence through the global the raise stores into @exn_msg/@exn_arg, because the text is a @.strN
// constant and a body mentions it by name.
func programRaisesUnhashable(ir string) string {
	sentences := map[string]string{}
	for _, ln := range strings.Split(ir, "\n") {
		k := strings.Index(ln, "unhashable type:")
		if k < 0 {
			continue
		}
		c := strings.LastIndex(ln[:k], `c"`)
		if c < 0 {
			continue
		}
		name := strings.TrimSpace(strings.SplitN(strings.TrimSpace(ln), " ", 2)[0])
		rest := ln[c+2:]
		j := strings.Index(rest, `"`)
		if j < 0 {
			continue
		}
		msg := rest[:j]
		if p := strings.Index(msg, ": "); p > 0 { // "TypeError: unhashable type: 'set'" \u2192 the sentence
			msg = msg[p+2:]
		}
		msg = strings.TrimSuffix(msg, `\00`) // the emitter's NUL terminator, spelled
		sentences[name] = msg
	}
	if len(sentences) == 0 {
		return ""
	}
	for _, body := range programBodies(ir) {
		for name, msg := range sentences {
			if strings.Contains(body, "i8* "+name+", i8** @exn_") {
				return msg
			}
		}
	}
	return ""
}

package lang

// pkg/lang/lambda_module_read_test.go — an applied `lambda` whose body is a read of a module-level
// literal answers the VALUE the module bound (roadmap **Gap R.331**, **ADR 0461**).
//
// The program is two lines of the most ordinary kind:
//
//	t = "hey"
//	print((lambda: t)())      →  `0`      reference: `hey`
//
// and `0` is not a random digit — it is the index the intern table gave `hey`. The read itself is fine:
// the module road carries an interned text through an `i32` exactly as **ADR 0227** designed (**Gap
// R.343** is that road's own row, one construct away). What was missing is the KIND at the call boundary.
// **ADR 0368**'s answer direction names a kind only where the SOURCE spells one, and `return t` spells
// nothing; the answer fell to the one-word road and the print door downstream never learned the word it
// held was an index. Writing the same body as a `def` answered `hey` all along — `return "hey"` spells it
// — which is what localised the row to the spelling rather than to the road.
//
// The fix is therefore a FOLD and not a second answer road: an applied `lambda` whose whole body is a read
// of a name the module bound once, to a literal, and never rebound, has a body that IS that literal. Once
// the body spells its kind, every existing door reads it — the text, verdict and void bodies go through
// **ADR 0401**'s hoist to the declaration and its registered answer, and the fold also removes the
// emission that left **Gap R.333**'s program calling `@rt_str_intern2` with no definition in the module.
//
// Nothing here claims a second engine ran. The answered rows are judged on the compiled leg and against
// `testdata/interpreter-golden.json` (`goldenStdout`, `HasGoldenAnswer` — the record leg); the CLI twin in
// `integration/lambda_module_read_test.go` reads every expectation out of a live `python3`. The sources
// this row first asked are recorded from the reference through the recorder at the bottom of this file
// (**ADR 0302**'s rule that a new entry is never self-certifying), and `tools/recmerge` refuses any entry
// the compiled leg will not agree to.

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// lambdaModuleReadRows are the programs whose answer this row owns. `want` is what CPython writes for the
// same bytes — read off `python3`, never off the toolchain.
func lambdaModuleReadRows() []struct {
	name, src, want string
} {
	return []struct{ name, src, want string }{
		// The row as filed: the digit is the intern table's index, not a value.
		{"a module-level TEXT read by an applied lambda", "t = \"hey\"\nprint((lambda: t)())\n", "hey\n"},
		{"a module-level TEXT with an escape read by an applied lambda", "t = \"a\\tb\"\nprint((lambda: t)())\n", "a\tb\n"},
		{"an EMPTY module text read by an applied lambda", "t = \"\"\nprint((lambda: t)())\n", "\n"},
		{"a module-level VERDICT read by an applied lambda", "b = True\nprint((lambda: b)())\n", "True\n"},
		{"a module-level false VERDICT read by an applied lambda", "b = False\nprint((lambda: b)())\n", "False\n"},
		{"a module-level VOID read by an applied lambda", "z = None\nprint((lambda: z)())\n", "None\n"},
		{"a module-level NUMBER read by an applied lambda", "n = 7\nprint((lambda: n)())\n", "7\n"},
		{"a module-level negative NUMBER read by an applied lambda", "n = -3\nprint((lambda: n)())\n", "-3\n"},
		// The positions the answer reaches, once the kind travels.
		{"the folded read is filled by a parameter the body never uses", "t = \"hey\"\nprint((lambda x: t)(9))\n", "hey\n"},
		{"the folded read bound and printed", "t = \"hey\"\nx = (lambda: t)()\nprint(x)\n", "hey\n"},
		{"the folded read bound, compared, measured and sent a message", "t = \"hey\"\nx = (lambda: t)()\nprint(x == \"hey\", len(x), x.upper())\n", "True 3 HEY\n"},
		{"the folded read inside an interpolation", "t = \"hey\"\nprint(f\"[{(lambda: t)()}]\")\n", "[hey]\n"},
		{"the folded read taken twice by a loop", "t = \"hey\"\nfor i in range(2):\n    print((lambda: t)())\n", "hey\nhey\n"},
		{"the folded read as a container element", "t = \"hey\"\nprint([(lambda: t)(), 2])\n", "['hey', 2]\n"},
		{"the folded read as a dict value", "t = \"hey\"\nprint({\"k\": (lambda: t)()})\n", "{'k': 'hey'}\n"},
		{"the folded read tested by a condition", "t = \"hey\"\nif (lambda: t)() == \"hey\":\n    print(\"yes\")\n\nelse:\n    print(\"no\")\n", "yes\n"},
		{"the folded read asked by str", "t = \"hey\"\nprint(str((lambda: t)()))\n", "hey\n"},
		{"the folded read of a text that names a kind", "t = \"None\"\nprint((lambda: t)(), None)\n", "None None\n"},
		// Two module names read by two applications of one lambda shape: the fold is per body, not per name.
		{"two folded reads of two module names", "a = \"x\"\nb = True\nprint((lambda: a)(), (lambda: b)())\n", "x True\n"},
		// The parameter gate is asked per NAME, and this is the row that says so: the module binds `n` once, and
		// the program's own parameter owns the name inside the body, so the argument answers and the fold does not
		// fire (asked directly, with the fold COUNT, in TestTheFoldFiresOnlyWhereTheTableAndTheScopesAgree).
		{"a name a parameter owns answers the argument, not the module", "n = 7\nprint((lambda n: n)(3))\n", "3\n"},
	}
}

// TestWhatAFoldedModuleReadAnswersWhatTheReferencePrints is the reference leg of the row: the compiled
// backend, against CPython's bytes written into the table above.
func TestWhatAFoldedModuleReadAnswersWhatTheReferencePrints(t *testing.T) {
	t.Parallel()
	for _, tc := range lambdaModuleReadRows() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledRunIsTheBackend(t, tc.src); got != tc.want {
				t.Fatalf("compiled=%q reference=%q\n%s", got, tc.want, tc.src)
			}
		})
	}
}

// TestWhatAFoldedModuleReadAnswersWhatTheRecordRecorded is the record leg. A source with no record is
// SKIPPED, not deleted — the record cannot be widened from here — and the count of sources the record does
// cover is asserted, so the leg cannot quietly stop being walked.
func TestWhatAFoldedModuleReadAnswersWhatTheRecordRecorded(t *testing.T) {
	t.Parallel()
	asked := 0
	for _, tc := range lambdaModuleReadRows() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// NOT parallel: the parent's own claim — that the record leg walked at least one row — is counted
			// by these bodies, and a parallel subtest can still be running when the parent answers.
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

// TestWhatAFoldedModuleReadRefusesNothingItClaimsToAnswer: a row the reference leg answers may not come
// back as a refusal, and no row of this family may reach **ADR 0166**'s exit 2.
func TestWhatAFoldedModuleReadRefusesNothingItClaimsToAnswer(t *testing.T) {
	t.Parallel()
	for _, tc := range lambdaModuleReadRows() {
		tc := tc
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

// TestTheFoldedReadCarriesTheKindIntoTheModule is the IR half: the answer travels by the door the `def`
// roads already use, which means the module DEFINES the intern helper rather than calling one it never
// declared (**Gap R.333**'s first half — the program used to reach `llc-20` with
// `use of undefined value '@rt_str_intern2'` and the toolchain still exited 0).
func TestTheFoldedReadCarriesTheKindIntoTheModule(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		calls     []string // the runtime entry the module must call AND define
	}{
		{"a folded text read interns its literal and defines the helper", "t = \"hey\"\nprint((lambda: t)())\n", []string{"rt_str_intern2"}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ir := mustCompileIR(t, tc.src)
			for _, sym := range tc.calls {
				if !strings.Contains(ir, "call i32 @"+sym+"(") {
					t.Fatalf("the module never calls @%s — the read did not travel the road that renders it:\n%s", sym, ir)
				}
				if !strings.Contains(ir, "define internal i32 @"+sym+"(") && !strings.Contains(ir, "define i32 @"+sym+"(") {
					t.Fatalf("the module calls @%s without defining it — that is `llc`'s `use of undefined value`, and the"+
						" runtime block the read needs was never requested (Gap R.333):\n%s", sym, ir)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The fold asked DIRECTLY, so that a gate which silently widens fails a decision row and not a program.
// ---------------------------------------------------------------------------

// foldedLambdaBodyOf runs the fold on a parsed program and answers what the body of the FIRST applied
// `lambda` became. It is the gate asked as a function (**ADR 0273**'s rule), because every behavioural
// table below can be passed by a fold that fires too often just as easily as by one that fires too seldom.
func foldedLambdaBodyOf(t *testing.T, src string) (Expr, int) {
	t.Helper()
	prog := parseProgramForTest(t, src)
	n := foldAppliedLambdaModuleReads(prog, nil)
	var body Expr
	w := &appliedLambdaWalk{visit: func(node Node) {
		if body != nil {
			return
		}
		c, isCall := node.(*Call)
		if !isCall || c == nil {
			return
		}
		if lam, ok := c.Fn.(*Lambda); ok && lam != nil {
			body = lam.Body
		}
	}}
	w.stmts(prog.Stmts)
	if body == nil {
		t.Fatalf("no applied `lambda` survives in the program to inspect:\n%s", src)
	}
	return body, n
}

// TestTheFoldFiresOnlyWhereTheTableAndTheScopesAgree is the decision table. Each row names what the fold
// must NOT read: a name a binder owns, a name the module rebinds, a body that is not one read, and a
// binding the module table refuses to call a literal.
func TestTheFoldFiresOnlyWhereTheTableAndTheScopesAgree(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		src   string
		kinds []string // the AST types the body may be, by name; empty means "untouched"
		folds int      // how many bodies the pass rewrote
	}{
		{"a text the module bound once", "t = \"hey\"\nprint((lambda: t)())\n", []string{"StrLit"}, 1},
		{"a verdict the module bound once", "b = True\nprint((lambda: b)())\n", []string{"BoolLit"}, 1},
		{"a void the module bound once", "z = None\nprint((lambda: z)())\n", []string{"NoneLit"}, 1},
		{"a number the module bound once", "n = 7\nprint((lambda: n)())\n", []string{"IntLit"}, 1},
		{
			// the module writes the name a second time: it is state, not a value, and the fold has to
			// leave it for **Gap R.343** / **Gap R.546** rather than answer the FIRST binding.
			"a name the module rebinds stays a read",
			"t = \"hey\"\nt = t + \"!\"\nprint((lambda: t)())\n", []string{"Name"}, 0,
		},
		{
			// the parameter owns the name inside the body; folding would print the module's text.
			"a name the lambda's own parameter owns stays a read",
			"t = \"hey\"\nprint((lambda t: t)(0))\n", []string{"Name"}, 0,
		},
		{
			// the same, one level out: the inner body's `t` belongs to the enclosing `lambda`.
			"a name an enclosing lambda owns stays a read",
			"t = \"mod\"\nprint((lambda t: (lambda: t)())(\"own\"))\n", []string{"Name", "Call"}, 0,
		},
		{
			// a name a `def` binds in its own body is not the module's, even when the module binds it too.
			"a name the enclosing def owns stays a read",
			"t = \"mod\"\ndef g():\n    t = \"loc\"\n    return (lambda: t)()\n\nprint(g())\n", []string{"Name"}, 0,
		},
		{
			// the module table admits no container: reading one as a literal would mint a fresh object
			// where the reference shares one, which is a worse answer than the refusal this leaves in place.
			"a container the module bound is not a literal to fold",
			"xs = [1, 2]\nprint((lambda: xs)())\n", []string{"Name"}, 0,
		},
		{
			// a name the module's own LOOP binds is state, not a literal: the table disqualifies it and the
			// fold has to agree with the table rather than with what the last iteration happened to write.
			"a loop target the module bound is not a literal to fold",
			"for t in [1]:\n    pass\n\nprint((lambda: t)())\n", []string{"Name"}, 0,
		},
		{
			// the body is not ONE read: the literal, the operation and the call all answer with more
			// than the module's value, so none of them is the fold's program.
			"a body that embeds the read in a literal is untouched",
			"t = \"hey\"\nprint((lambda: [t])())\n", []string{"ListLit"}, 0,
		},
		{"a body that embeds the read in an operation is untouched", "t = \"hey\"\nprint((lambda: t + \"!\")())\n", []string{"BinOp"}, 0},
		{"a body that is already a literal is untouched", "print((lambda: \"hey\")())\n", []string{"StrLit"}, 0},
		{
			// two applications, two module names: the fold is asked of each body.
			"two applications of the same shape fold twice",
			"a = \"x\"\nb = True\nprint((lambda: a)(), (lambda: b)())\n", []string{"StrLit"}, 2,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body, folds := foldedLambdaBodyOf(t, tc.src)
			if folds != tc.folds {
				t.Fatalf("the pass folded %d body(s), want %d (body is %s)\n%s", folds, tc.folds, reflect.TypeOf(body).String(), tc.src)
			}
			got := strings.TrimPrefix(reflect.TypeOf(body).String(), "*lang.")
			if len(tc.kinds) == 0 {
				return
			}
			for _, want := range tc.kinds {
				if got == want {
					return
				}
			}
			t.Fatalf("the body is a %s, want one of %v\n%s", got, tc.kinds, tc.src)
		})
	}
}

// TestTheFoldSharesNoNodeWithTheModulesOwnStatement pins the copy: the module's `AssignStmt` still holds
// the literal the parser built, and the folded body holds a different node. Two emitters walking one node
// is how a per-node cache — an interned-text slot, an inferred type — starts answering two programs at
// once, and a fold that shares would still pass every answer table above until it did not.
func TestTheFoldSharesNoNodeWithTheModulesOwnStatement(t *testing.T) {
	t.Parallel()
	const src = "t = \"hey\"\nprint((lambda: t)())\n"
	untouched := parseProgramForTest(t, src)
	body, folds := foldedLambdaBodyOf(t, src)
	if folds != 1 {
		t.Fatalf("the fold did not fire: %d", folds)
	}
	folded, ok := body.(*StrLit)
	if !ok {
		t.Fatalf("the folded body is a %s, want a *StrLit", reflect.TypeOf(body).String())
	}
	var bound *StrLit
	for _, st := range untouched.Stmts {
		if a, isAssign := st.(*AssignStmt); isAssign {
			if lit, isStr := a.Value.(*StrLit); isStr {
				bound = lit
			}
		}
	}
	if bound == nil {
		t.Fatal("the program's own binding is not a *StrLit any more — the fold edited the module's statement")
	}
	if folded == bound {
		t.Fatal("the folded body IS the module's node: two emitters, one node")
	}
	if folded.Value != bound.Value {
		t.Fatalf("the fold answered %q where the module bound %q", folded.Value, bound.Value)
	}
}

// TestTheFoldedReadAccountSaysWhatItFolded is the machine path: an agent reading `--lambda-answers` has to
// be able to tell a body the pass hoisted from a body the pass rewrote and left on the emitter's road
// (**ADR 0401**'s rule about an account written by the path the rewrite takes).
func TestTheFoldedReadAccountSaysWhatItFolded(t *testing.T) {
	t.Parallel()
	doc := lambdaAnswersAccountOf(t, "t = \"hey\"\nprint((lambda: t)())\n")
	if got := doc.ReadsFolded; got != 1 {
		t.Fatalf("reads_folded=%d, want 1: %s", got, doc.raw)
	}
	if len(doc.FoldedReads) != 1 {
		t.Fatalf("folded_reads holds %d rows, want 1: %s", len(doc.FoldedReads), doc.raw)
	}
	row := doc.FoldedReads[0]
	if row.Name != "t" || row.Literal != "\"hey\"" || row.Line != 2 {
		t.Fatalf("the account says the pass folded %q into %q on line %d, want t / \"hey\" / 2: %s", row.Name, row.Literal, row.Line, doc.raw)
	}
	if !doc.Rewritten {
		t.Fatalf("a program whose body the pass rewrote reports rewritten=false: %s", doc.raw)
	}
	// …and a program with nothing to fold says zero rather than lying by omission.
	none := lambdaAnswersAccountOf(t, "print((lambda: \"hey\")())\n")
	if none.ReadsFolded != 0 || len(none.FoldedReads) != 0 {
		t.Fatalf("a program with no module read reports %d fold(s): %s", none.ReadsFolded, none.raw)
	}
}

type lambdaAnswersDoc struct {
	Rewritten   bool `json:"rewritten"`
	ReadsFolded int  `json:"reads_folded"`
	FoldedReads []struct {
		Name    string `json:"name"`
		Literal string `json:"literal"`
		Line    int    `json:"line"`
	} `json:"folded_reads"`
	raw string
}

func lambdaAnswersAccountOf(t *testing.T, src string) lambdaAnswersDoc {
	t.Helper()
	out, err := LambdaAnswersJSON(src)
	if err != nil {
		t.Fatalf("the account: %v", err)
	}
	var doc lambdaAnswersDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("the account is not JSON: %v\n%s", err, out)
	}
	doc.raw = out
	return doc
}

// ---------------------------------------------------------------------------
// What this row measured and did NOT fix. Each case is DELETED — not edited — when its fix lands.
// ---------------------------------------------------------------------------

// TestWhatTheFoldedModuleReadLeavesStandingIsFiledNotFixed pins the two faces of the same missing kind
// that this row's fold cannot reach, each with today's bytes beside the reference's.
//
//   - **Gap R.546**: the module writes the name a SECOND time. `moduleEnvFor` then calls it state and the
//     fold declines (rightly — the answer is the module's LAST binding), and the two legs disagree the way
//     **Gap R.333** documented for its own program: the CLI writes the intern table's index `0` at the exit
//     code of success where the reference writes `hey!`, and the leg every `pkg/lang` compiled case walks
//     hands `llc-20` a call to `@rt_str_intern2` the module never defines. Neither exit code is honest — a
//     wrong value at 0, and a module rejection that is **ADR 0166**'s exit 2 arriving as something else.
//     The fold must not grow a "use the last binding" rule to close this: it is sound because the name
//     never changes, and a fold over mutable state is the wrong answer **ADR 0448** already made the
//     compiler pay for.
//   - **Gap R.547**: the same missing kind on the OTHER side of the boundary — a `def` whose `return` is a
//     read of a module-level `None`. **ADR 0368** registers a body's answer kinds from what its arms
//     spell, and `return t` spells none, so `print(f())` writes the void's payload `0`. The literal form
//     (`def f(): return None`) has answered `None` since **Gap R.228**, which is the control that says the
//     defect is the spelling and not the road.
//   - **Gap R.548**: a dict literal whose VALUE is a call, printed in a statement that goes on to print a
//     tuple, writes its text key's intern INDEX: `print({"k": (lambda: 7)()}, (1,))` says `{1: 7}` where
//     the reference says `{'k': 7}`. ADR 0173's per-side element-kind claims (`dictKeyStr`, `dictValStr`)
//     are lost across the sibling argument, and the order decides — `print((1,), {"k": (lambda: 7)()})`
//     answers correctly. Pre-existing and identical on the parent commit's own binary with a literal-bodied
//     `lambda`, which is what keeps it out of this row's fix and in this table;
//     `integration/programs/applied_lambda_module_read.gy` keeps the shape out of the corpus for the same
//     reason **Gap R.544**'s quadruple-printing shape was kept out of **ADR 0460**'s program.
func TestWhatTheFoldedModuleReadLeavesStandingIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		src     string
		want    string // what the reference prints
		today   string // what this toolchain prints today, on the leg that runs
		rejects string // when the leg dies in `llc` instead, the message it dies with
		gap     string
	}{
		{
			name:    "a module text the program rebinds, read by an applied lambda",
			src:     "t = \"hey\"\nt = t + \"!\"\nprint((lambda: t)())\n",
			want:    "hey!\n",
			today:   "0\n", // the CLI's answer, at exit 0 — pinned in integration/lambda_module_read_test.go
			rejects: "use of undefined value '@rt_str_intern2'",
			gap:     "Gap R.546",
		},
		{
			name:  "a def whose return READS a module-level void",
			src:   "t = None\ndef f():\n    return t\n\nprint(f())\n",
			want:  "None\n",
			today: "0\n",
			gap:   "Gap R.547",
		},
		{
			name:  "a dict literal whose value is a call, printed before a tuple, prints its key's intern index",
			src:   "print({\"k\": (lambda: 7)()}, (1,))\n",
			want:  "{'k': 7} (1,)\n",
			today: "{1: 7} (1,)\n",
			gap:   "Gap R.548",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.rejects != "" {
				got, err := RunSource(tc.src)
				if err == nil {
					t.Fatalf("%s: the leg that walks this family no longer dies in `llc` — it printed %q (the reference prints"+
						" %q); when the two agree, DELETE this case\n\t%s", tc.gap, got, tc.want, tc.src)
				}
				if !strings.Contains(err.Error(), tc.rejects) {
					t.Fatalf("%s: the module rejection changed from %q to:\n%v", tc.gap, tc.rejects, err)
				}
				return
			}
			got := compiledRunIsTheBackend(t, tc.src)
			if got == tc.want {
				t.Fatalf("%s: the program now prints the reference's %q — DELETE this case\n\t%s", tc.gap, tc.want, tc.src)
			}
			if got != tc.today {
				t.Fatalf("%s: today's answer moved from %q to %q — the reference prints %q; when it prints the"+
					" reference's bytes, DELETE this case\n\t%s", tc.gap, tc.today, got, tc.want, tc.src)
			}
		})
	}
}

// TestWhatTheFoldedModuleReadStillRefusesInWords pins the shapes this row leaves refused — in words, with
// the row that owns them named, and never at ADR 0166's exit 2. A fold that widened into these would
// trade a refusal for an answer the compiler cannot defend.
func TestWhatTheFoldedModuleReadStillRefusesInWords(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, says, gap string
	}{
		{
			// the module table admits no container, so the read stays the module road's and the module
			// road refuses state a body reads — the honest face of Gap R.35's remaining half.
			name: "a module CONTAINER read by an applied lambda",
			src:  "xs = [1, 2]\nprint((lambda: xs)())\n",
			says: "is bound at module level",
			gap:  "Gap R.35",
		},
		{
			// the second binding makes the name state, and the module road — not this fold — is the door
			// that answers it. The reference reads `None`; the door refuses rather than print a word whose
			// kind nothing recorded. That is the same road **Gap R.546**'s text face answers WRONGLY, which
			// is why the two are pinned in different tables: one program refuses, its neighbour answers.
			name: "a module void the program bound twice, read by an applied lambda",
			src:  "z = None\nz = None\nprint((lambda: z)())\n",
			says: "is bound at module level",
			gap:  "Gap R.35",
		},
		{
			// a double does not cross this boundary as one word; the sentence names the tagged value word
			// L11.1 still owes. (It names a CALL while the program wrote a NAME — the sentence's own half
			// of Gap R.116, and pinned here so the day it is rewritten this row says so.)
			name: "a module FLOAT read by an applied lambda",
			src:  "t = 2.5\nprint((lambda: t)())\n",
			says: "roadmap L11.1",
			gap:  "L11.1",
		},
		{
			// a `lambda` in a DEFAULT parameter is a callable sitting in a value slot, and calling it asks the
			// value what it is — the tagged word L11.1 still owns. The fold reached the body; the call never
			// got a road, and the row says which half is missing rather than answering.
			name: "a folded read handed to a default parameter and called",
			src:  "t = \"hey\"\ndef go(f=(lambda: t)):\n    return f()\n\nprint(go())\n",
			says: "roadmap L11.1",
			gap:  "L11.1",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msg := compiledRefusalOf(tc.src)
			if msg == "" {
				t.Fatalf("%s: expected a refusal, the program compiled\n\t%s", tc.gap, tc.src)
			}
			if !strings.Contains(msg, tc.says) {
				t.Fatalf("%s: the refusal does not name %q:\n%s\n\t%s", tc.gap, tc.says, msg, tc.src)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The recorder: the sources this row first asked, for `tools/recmerge`. Single-process by contract —
// the artifact-writing modes are the makefile's `make test`, never the concurrent runner.
// ---------------------------------------------------------------------------

// TestTheRecordSourcesTheFoldedModuleReadRowAsksFor dumps the family's sources for `tools/recmerge`, which
// asks CPython and refuses to write an entry the compiled leg will not agree to.
func TestTheRecordSourcesTheFoldedModuleReadRowAsksFor(t *testing.T) {
	out := os.Getenv("GUSTY_RECORD_LAMBDA_MODULE_READ")
	if out == "" {
		t.Skip("the recorder: set GUSTY_RECORD_LAMBDA_MODULE_READ=<file> to dump these sources for tools/recmerge")
	}
	var srcs []string
	for _, r := range lambdaModuleReadRows() {
		srcs = append(srcs, r.src)
	}
	// …and the corpus row itself: a row of `integration/programs/` is judged on the record like every
	// other program (**ADR 0453**'s rule that a conformance row without a record silently stops being
	// judged).
	if body, err := os.ReadFile("../../integration/programs/applied_lambda_module_read.gy"); err == nil {
		srcs = append(srcs, string(body))
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

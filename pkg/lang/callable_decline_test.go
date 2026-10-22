package lang

// pkg/lang/callable_decline_test.go — the defunctionalisation pass's decline reaches the READER (roadmap
// L11.7 · Gap R.337, ADR 0379).
//
// `defunctionalizeCallables` is all-or-nothing (ADR 0345): one shape it cannot prove and the whole program
// comes back untouched, so the call the rewrite would have built lands on the ordinary road and is refused
// by `callBuiltins`. The pass's withdrawal is correct as a rewrite rule and lossy as a diagnostic — it knew
// which name it declined on and why, and the reader used to see only the emitter's generic sentence, which
// points at the memory-model row that owns nothing here. These cases pin the other half: the sentence the
// pass spoke is quoted beside the call it refused, and it is quoted in the SAME bytes `gustyc --callables`
// prints, so one run cannot tell two stories about one program.
//
// Two directions are guarded, and the second is the one that makes the first a test rather than a print:
//
//   - a program the pass declined gets the pass's words appended, and still classifies as
//     `lower.unsupported.indirect-call` at exit 1 — never exit 2, which ADR 0166 reserves for a compiler bug;
//   - a program the pass never spoke about — one it simply had no work for — keeps the emitter's sentence
//     BYTE FOR BYTE, which is what the row that filed this defect asked for and what stops the append from
//     becoming a footnote stamped on every refusal in the package.
//
// The word travels as a value beside the program it describes, so the last case compiles declined and
// no-work programs ALTERNATELY, and in both orders: a package global would let one compile answer another's
// refusal, which is the failure ADR 0343 removed from the rest of the toolchain.

import (
	"encoding/json"
	"strings"
	"testing"
)

// uncallableCallSentence is the emitter's own refusal, written out here rather than imported from the road
// that emits it: a change to that sentence must fail the byte-for-byte case below, which is the point of
// spelling it out. Each row of callableNoWorkPrograms carries one of these with its own callee name in it.
func uncallableCallSentence(callee string) string {
	return "codegen: " + quoteLikeProgram(callee) + ` is called but is not a function this backend can build a call to: the reference asks the value what it is at the moment of the call and raises TypeError (` + "`'int' object is not callable`" + `), and asking that question needs the value word that carries its own kind (roadmap L11.1, ADR 0166)`
}

// quoteLikeProgram spells a name the way the refusal spells it — "t", not t.
func quoteLikeProgram(name string) string { return "\"" + name + "\"" }

// callableDeclinePrograms are programs whose call the pass would have built and could not. Each cites the
// row the pass names in its own account — the citation is what the reader used to lose.
var callableDeclinePrograms = []struct{ name, src, cite, says string }{
	{
		// The two shapes the entry-side capture road (Gap R.336, paid by ADR 0395) still declines, each naming
		// what is missing. The three that used to sit here — an entry lambda reading a local, the same read
		// twice by one callee, and the entry reading the frame's loop target — answer CPython now and moved to
		// the answering table of `lambda_applied_test.go`, with the account counting the words the pass minted
		// for them (`TestWhatTheCallablesAccountSaysAnEntryReadsItsFrame`).
		//
		// The forwarding word is a PARAMETER of the clone; a body that already owns that name would have two
		// slots under one spelling, and the pass declines rather than let the two reads answer each other.
		name: "an entry whose capture word the higher-order body already owns",
		src:  "def apply1(fn, v):\n    base = 9\n    return fn(v)\n\ndef use(seed):\n    base = 1\n    return apply1(lambda x: x + base, seed)\n\nprint(use(3))\n",
		cite: "Gap R.336",
		says: `already owns that name`,
	},
	{
		// An entry the pass cannot substitute because the callable is a predeclared name, which the pass
		// does not clone. The reference prints `3`.
		name: "an entry that is a builtin the pass has no declaration for",
		src:  "print((lambda f: f(3))(abs))\n",
		cite: "L11.7",
		says: `"abs" is not a declared function`,
	},
	{
		// The OTHER way a program comes back untouched: the pass planned a rewrite and its own invariant
		// — no declared function name may survive in a value position — took it away. `--callables` has
		// always said so; this is the same sentence reaching the terminal.
		name: "a rewrite the pass withdrew under its own invariant",
		src:  "def apply(f, x):\n    return f(x)\n\ndef inc(v):\n    return v + 1\n\nprint(apply(inc, 1))\nprint(inc)\n",
		cite: "L11.7",
		says: "the rewrite was withdrawn",
	},
}

// callableNoWorkPrograms are programs whose refusal is the emitter's and whose pass had nothing to say:
// every one of them calls a name that was never a function, and none of them uses a function as a value,
// so the pass's only answer is the one it gives every first-order program in the corpus. Their refusals are
// the ones that must NOT gain a quotation.
var callableNoWorkPrograms = []struct{ name, src, callee string }{
	{"a number called as a function", "t = 1\nprint(t())\n", "t"},
	{"a text called as a function", "s = \"abc\"\nprint(s(1))\n", "s"},
	{"a dict called as a function", "d = {\"a\": 1}\nprint(d(1))\n", "d"},
	{"a list called as a function", "xs = [1, 2]\nprint(xs(0))\n", "xs"},
}

// TestWhatTheUncallableCallRefusalCarriesThePassesOwnSentence is the half of Gap R.337 that a reader sees:
// the emitter's own words stay, the pass's words arrive beside them under a label that says who is speaking,
// and the refusal stays a refusal — exit 1's door, the capability code it has always carried, and no route
// to the assembler.
func TestWhatTheUncallableCallRefusalCarriesThePassesOwnSentence(t *testing.T) {
	for _, tc := range callableDeclinePrograms {
		t.Run(tc.name, func(t *testing.T) {
			msg := compiledRefusalOf(tc.src)
			if msg == "" {
				t.Fatalf("the program compiled; the pass was expected to decline this shape")
			}
			for _, want := range []string{
				"is called but is not a function this backend can build a call to", // the emitter still speaks
				"the defunctionalisation pass",                                     // the quotation is labelled
				tc.says,                                                            // and carries the pass's own words
				tc.cite,                                                            // naming the row that owns the shape
				"gustyc --callables",                                               // and where else to read them
			} {
				if !strings.Contains(msg, want) {
					t.Fatalf("the refusal omits %q:\n%s", want, msg)
				}
			}
			// A refusal is exit 1's door. A module that reached `llc` would mean the pass's decline was
			// ignored and a half-rewrite was emitted (ADR 0166's exit-code contract).
			for _, bad := range []string{"invalid module", "llc", "redefinition of global", "verbatim"} {
				if strings.Contains(msg, bad) {
					t.Fatalf("the declined program reached the toolchain instead of the refusal door (%q):\n%s", bad, msg)
				}
			}
			// L11.8's machine path survives the longer sentence: the appended text is a QUOTATION, not a new
			// shape, and the classifier's most-specific-marker rule must still answer indirect-call — a
			// refusal whose code silently moved is an agent's branch that stops firing (ADR 0346).
			kind, ok := ClassifyCapabilityRefusal(msg)
			if !ok || kind.Code != "lower.unsupported.indirect-call" {
				if ok {
					t.Fatalf("the longer refusal classifies as %s, want lower.unsupported.indirect-call:\n%s", kind.Code, msg)
				}
				t.Fatalf("the longer refusal classifies as nothing at all:\n%s", msg)
			}
		})
	}
}

// TestWhatTheCallablesAccountAndTheRefusalSpeakOneSentence is the row's "one run's output" claim: the
// `refused` entry of `--callables` and the terminal's refusal are the SAME bytes, so a caller that reads the
// document and a human who reads the error cannot be given different explanations of one compile.
func TestWhatTheCallablesAccountAndTheRefusalSpeakOneSentence(t *testing.T) {
	for _, tc := range callableDeclinePrograms {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := CallablesJSON(tc.src)
			if err != nil {
				t.Fatalf("a decline is still a document: %v", err)
			}
			var rep struct {
				Rewritten bool     `json:"rewritten"`
				Refused   []string `json:"refused"`
			}
			if err := json.Unmarshal([]byte(doc), &rep); err != nil {
				t.Fatalf("the account is not JSON: %v\n%s", err, doc)
			}
			if rep.Rewritten || len(rep.Refused) != 1 {
				t.Fatalf("the account does not describe a declined program: %s", doc)
			}
			msg := compiledRefusalOf(tc.src)
			if !strings.Contains(msg, rep.Refused[0]) {
				t.Fatalf("the terminal does not carry the account byte for byte.\n account: %s\n terminal : %s", rep.Refused[0], msg)
			}
		})
	}
}

// TestWhatTheUncallableCallRefusalKeepsItsOwnWordsWhenThePassHadNoWork is the guard that makes the append a
// diagnosis rather than a sticker: for a program the pass had no work for, the emitter's sentence is the
// whole message, byte for byte, with no sentence about a pass that never spoke.
func TestWhatTheUncallableCallRefusalKeepsItsOwnWordsWhenThePassHadNoWork(t *testing.T) {
	for _, tc := range callableNoWorkPrograms {
		t.Run(tc.name, func(t *testing.T) {
			msg := compiledRefusalOf(tc.src)
			if msg == "" {
				t.Fatalf("the program compiled; this row is about the refusal")
			}
			if msg != uncallableCallSentence(tc.callee) {
				t.Fatalf("the refusal is not the emitter's own sentence byte for byte:\n got: %s\nwant: %s", msg, uncallableCallSentence(tc.callee))
			}
			// The pass's silence is the claim, so the quotation's labels must be absent, not merely empty.
			for _, bad := range []string{"defunctionalisation", "defunctionalization declines", "--callables"} {
				if strings.Contains(msg, bad) {
					t.Fatalf("a program the pass never declined carries a quotation about %q:\n%s", bad, msg)
				}
			}
			if _, said := defunctionalizeCallablesTelling(parseProgramForTest(t, tc.src)); said != "" {
				t.Fatalf("the pass had a sentence for a program it had no work for: %s", said)
			}
		})
	}
}

// TestWhatThePassSaysTravelsWithTheProgramAndNotWithTheProcess is ADR 0343's rule aimed at the new field:
// the decline is returned beside the program it describes and copied onto that module's own irGen, so two
// programs compiled by one process — in either order, and repeatedly — keep their own refusals. Had this
// been a package variable, the no-work program compiled after a declined one would have borrowed its
// sentence, and the byte-for-byte case above would pass or fail on the schedule.
func TestWhatThePassSaysTravelsWithTheProgramAndNotWithTheProcess(t *testing.T) {
	declined := callableDeclinePrograms[0].src
	noWork := "t = 1\nprint(t())\n"
	rewritten := "def apply1(fn, v):\n    return fn(v)\n\nprint(apply1(lambda x: x + 1, 41))\n"

	// Each of the three shapes, in both interleavings, asserting the same two facts every time: the declined
	// program's message names the pass, the other two never do. The run is deliberately SEQUENTIAL — a
	// package global misbehaves under any interleaving, and a deterministic one is the one a reader can
	// reproduce (the suite's own concurrency, and `make testrace`, are the other half of this claim).
	for _, order := range [][]string{{declined, noWork, declined, noWork}, {noWork, rewritten, declined, rewritten}} {
		for i, src := range order {
			msg := compiledRefusalOf(src)
			wantQuotation := src == declined
			has := strings.Contains(msg, "the defunctionalisation pass")
			if has != wantQuotation {
				t.Fatalf("position %d of the interleaved run carries a quotation=%v, want %v:\n%s", i, has, wantQuotation, msg)
			}
			if !wantQuotation && src == noWork && msg != uncallableCallSentence("t") {
				t.Fatalf("position %d of the interleaved run lost the plain sentence:\n%s", i, msg)
			}
		}
	}
	// The third shape proves the quotation is not simply "this program uses a lambda": the pass REWROTE it,
	// there is nothing to say, and it answers.
	if msg := compiledRefusalOf(rewritten); msg != "" {
		t.Fatalf("the rewritten program was refused: %s", msg)
	}
	if _, said := defunctionalizeCallablesTelling(parseProgramForTest(t, rewritten)); said != "" {
		t.Fatalf("a program the pass rewrote has a decline to tell: %s", said)
	}
}

// declineCarryingAnswers are the programs the pass DOES rewrite — the regression half of this file, asked of
// both witness legs below. Taking the decline out of the pass alongside its program must not have cost the
// pass the rewrite; if the new entry point quietly started handing back the untouched tree, these are the
// rows that notice.
var declineCarryingAnswers = []struct{ name, src, want string }{
	{"an applied lambda that calls its own parameter", "print((lambda f: f(3))(lambda x: x * 2))\n", "6\n"},
	{"a higher-order def called with a lambda", "def apply1(fn, v):\n    return fn(v)\n\nprint(apply1(lambda x: x + 1, 41))\n", "42\n"},
	{"a clone answering its own recursion", "def fact(n, f):\n    return 1 if n == 0 else f(n) + fact(n - 1, f)\n\nprint(fact(4, lambda v: v))\n", "11\n"},
	{"a named lambda called through the name it was bound to", "neg = lambda v: 0 - v\nprint(sorted([3, 1, 2], key=neg))\n", "[3, 2, 1]\n"},
}

// TestWhatTheDeclineCarryingPassStillRewritesWhatItAlwaysRewrote is the compiled leg: what the rewrite
// produces has not moved, and none of these answers arrives as a refusal (ADR 0273's rule over a table that
// claims an answer).
func TestWhatTheDeclineCarryingPassStillRewritesWhatItAlwaysRewrote(t *testing.T) {
	for _, tc := range declineCarryingAnswers {
		t.Run(tc.name, func(t *testing.T) {
			if got := compiledRunIsTheBackend(t, tc.src); got != tc.want {
				t.Fatalf("compiled=%q want=%q", got, tc.want)
			}
			if msg := compiledRefusalOf(tc.src); msg != "" {
				t.Fatalf("refused a program the table claims answers: %s", msg)
			}
		})
	}
}

// TestWhatTheDeclineCarryingPassAnswersWhatTheRecordRecorded is the same table on the record leg: the answer
// on record is the answer the compiled leg gives. A source the retired engine never saw is skipped, not
// deleted — the reference leg at `integration/callable_decline_test.go` judges those — but a table that
// matches no record at all means the leg is not being walked, and that is a failure.
func TestWhatTheDeclineCarryingPassAnswersWhatTheRecordRecorded(t *testing.T) {
	asked := 0
	for _, tc := range declineCarryingAnswers {
		t.Run(tc.name, func(t *testing.T) {
			if !HasGoldenAnswer(tc.src) {
				t.Skipf("the retired engine left no record of this program")
			}
			asked++
			want := goldenStdout(silentReporter{}, tc.src)
			if got := compiledRunIsTheBackend(t, tc.src); got != want {
				t.Fatalf("compiled=%q record=%q", got, want)
			}
			if want != tc.want {
				t.Fatalf("this file's table and the record disagree: record=%q table=%q", want, tc.want)
			}
		})
	}
	if asked == 0 {
		t.Fatal("no row of this table matches a record entry: the record leg is not being walked")
	}
}

// TestWhatThePassHandsBackWhenItDeclines asks the pass itself: a decline returns the caller's OWN tree,
// which is ADR 0345's all-or-nothing rule, and the sentence that comes back with it is the one the emitter
// quotes — including the withdrawal, which is the second way a program comes back untouched and the one
// that used to be unwritable at all.
func TestWhatThePassHandsBackWhenItDeclines(t *testing.T) {
	for _, tc := range callableDeclinePrograms {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := parseProgram(tc.src)
			if err != nil {
				t.Fatalf("the row's program does not parse: %v", err)
			}
			out, said := defunctionalizeCallablesTelling(prog)
			if out != prog {
				t.Fatalf("a declined program came back as a different tree; the pass is all-or-nothing")
			}
			if !strings.HasPrefix(said, "defunctionalization declines: ") {
				t.Fatalf("the decline is not in the account's shape: %q", said)
			}
			if !strings.Contains(said, "roadmap L11.7") {
				t.Fatalf("the decline names no row: %q", said)
			}
		})
	}
}

package integration

// integration/callable_decline_test.go — the defunctionalisation pass's decline reaches the terminal, at the
// CLI, against the reference (roadmap L11.7 · Gap R.337, ADR 0379).
//
// `pkg/lang/callable_decline_test.go` decides the shape — which programs the pass declines, what the refusal
// says, and that the decline travels with the program rather than with the process. This file asks the two
// questions that only the shipped tool can answer:
//
//   - does a user who runs one of these programs see the pass's own words, on both compiled roads, at exit 1
//     — the refusal's door — and never at exit 2, which ADR 0166 reserves for a compiler bug; and
//   - do the two machine faces of one compile, the terminal's refusal and `gustyc --callables`'s `refused`,
//     agree IN ONE RUN, so an agent reading either gets the same explanation of the same program.
//
// Each refused program is a source the REFERENCE answers, and its bytes are written beside the row: the
// point of the family is that a door is shut, not that the program is broken. The last case is the guard on
// the whole idea — a program the pass never spoke about gets the emitter's sentence and nothing else, so the
// quotation stays a diagnosis and never becomes boilerplate on every refusal in the backend.

import (
	"encoding/json"
	"strings"
	"testing"
)

// callableDeclineCLIPrograms are the declined shapes, with the citation the pass puts in its own account and
// the reference's answer beside them. `ref` is what the door owes: these are not broken programs.
var callableDeclineCLIPrograms = []struct{ name, src, cite, ref string }{
	{
		// The two shapes the entry-side capture road (Gap R.336, paid by ADR 0395) still declines. The three
		// that opened this table — an entry reading a local of the frame its application is written in, the
		// same read twice by one callee, and the entry reading the frame's loop target — print CPython's bytes
		// now and live in `lambdaAppliedCLIPrograms`, the reference leg at the terminal.
		//
		// A forwarding word is a PARAMETER of the clone, and a body that already owns that name would have two
		// slots under one spelling: the pass says which name is in the way instead of writing it.
		"an entry whose capture word the higher-order body already owns",
		"def apply1(fn, v):\n    base = 9\n    return fn(v)\n\ndef use(seed):\n    base = 1\n    return apply1(lambda x: x + base, seed)\n\nprint(use(3))\n",
		"Gap R.336", "4\n",
	},
	{
		"an entry that is a builtin the pass has no declaration for",
		"print((lambda f: f(3))(abs))\n",
		"L11.7", "3\n",
	},
	{
		// The reference prints a function object for the last line — an address, not bytes to compare — so
		// this row asks only that the reference RUNS it, which is still the fact the row needs: the program
		// is not broken, the door is shut.
		"a rewrite the pass withdrew under its own invariant",
		"def apply(f, x):\n    return f(x)\n\ndef inc(v):\n    return v + 1\n\nprint(apply(inc, 1))\nprint(inc)\n",
		"the rewrite was withdrawn", "",
	},
}

// TestADeclinedCallableRefusalCarriesThePassesOwnSentence walks each declined shape through the shipped CLI
// on both compiled roads and asks what the reader is told.
func TestADeclinedCallableRefusalCarriesThePassesOwnSentence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, tc := range callableDeclineCLIPrograms {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// The reference has to ANSWER this source, or the row describes the wrong program: a shut door
			// and a broken program are different findings and only the reference tells them apart.
			got, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Skip("no python3 available to cross-check")
			}
			if tc.ref != "" && got != tc.ref {
				t.Fatalf("the reference's answer moved from what this row records: got %q, row says %q", got, tc.ref)
			}

			p := writeSrc(t, dir, "callable_decline.gy", tc.src)
			for _, road := range []struct {
				label string
				args  []string
			}{
				{"linked binary", []string{"--aot", "--file", p}},
				{"in-process JIT", []string{"--eval", tc.src}},
			} {
				out, code := cliRunMerged(t, road.args...)
				if code == 2 {
					t.Fatalf("%s leg: exit 2 on a program the reference answers — ADR 0166 reserves it for a compiler bug:\n%s", road.label, out)
				}
				if code != 1 {
					t.Fatalf("%s leg: exited %d, want 1 (a front-end refusal): %s", road.label, code, out)
				}
				for _, want := range []string{
					"is called but is not a function this backend can build a call to", // the emitter's own words
					"the defunctionalisation pass",                                     // the quotation is labelled
					tc.cite,                                                            // and says who owns the shape
					"gustyc --callables",                                               // and where else to read it
					"roadmap",                                                          // every refusal cites a row
				} {
					if !strings.Contains(out, want) {
						t.Fatalf("%s leg: the refusal omits %q:\n%s", road.label, want, out)
					}
				}
				// The pass's sentence is a QUOTATION of the account, not a paraphrase of it — a reader who
				// compares the two must find the same bytes (the row's "one run's output" claim, walked here
				// for the road that prints to a terminal rather than a tool channel).
				if account := callablesAccountOf(t, tc.src); !strings.Contains(out, account) {
					t.Fatalf("%s leg: the terminal does not carry the account byte for byte.\n account: %s\n terminal : %s", road.label, account, out)
				}
			}
		})
	}
}

// TestADeclinedCallableRefusalAndTheCallablesAccountAgreeInOneRun is the machine path: the `--json` payload's
// `error` and `--callables`'s `refused` are read out of ONE run of the tool for one program, and the second
// has to sit inside the first. An agent that branches on `capability.code` and an agent that reads the
// account are then reading one story, which is what ADR 0346's stable codes are for.
func TestADeclinedCallableRefusalAndTheCallablesAccountAgreeInOneRun(t *testing.T) {
	t.Parallel()
	for _, tc := range callableDeclineCLIPrograms {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			payload := cliRun(t, "--json", "--eval", tc.src)
			var doc struct {
				OK         bool   `json:"ok"`
				Exit       int    `json:"exit"`
				Phase      string `json:"phase"`
				Error      string `json:"error"`
				Capability *struct {
					Code string `json:"code"`
				} `json:"capability"`
			}
			if err := json.Unmarshal([]byte(payload), &doc); err != nil {
				t.Fatalf("the --json payload is not JSON: %v\n%s", err, payload)
			}
			if doc.OK || doc.Exit != 1 || doc.Phase != "codegen" {
				t.Fatalf("the refusal did not arrive as a codegen-phase exit 1: %s", payload)
			}
			if doc.Capability == nil || doc.Capability.Code != "lower.unsupported.indirect-call" {
				t.Fatalf("the longer refusal lost its stable capability code (ADR 0346): %s", payload)
			}
			if account := callablesAccountOf(t, tc.src); !strings.Contains(doc.Error, account) {
				t.Fatalf("--json and --callables disagree in one run.\n account: %s\n json   : %s", account, doc.Error)
			}
		})
	}
}

// TestTheUncallableCallRefusalKeepsItsOwnWordsAtTheCLI is the byte-for-byte control at the shipped
// interface: a program whose only problem is a name that was never a function is a program the pass had no
// work for, and its refusal is still the sentence this backend has shipped since ADR 0166 — same words,
// same exit code, no footnote about a pass that never spoke.
func TestTheUncallableCallRefusalKeepsItsOwnWordsAtTheCLI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, tc := range []struct{ name, src string }{
		{"a number called as a function", "t = 1\nprint(t())\n"},
		{"a text called as a function", "s = \"abc\"\nprint(s(1))\n"},
		{"a list called as a function", "xs = [1, 2]\nprint(xs(0))\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := writeSrc(t, dir, "callable_plain.gy", tc.src)
			out, code := cliRunMerged(t, "--aot", "--file", p)
			if code != 1 {
				t.Fatalf("exited %d, want 1: %s", code, out)
			}
			for _, bad := range []string{"defunctionalisation", "defunctionalization declines", "--callables"} {
				if strings.Contains(out, bad) {
					t.Fatalf("a program the pass never declined carries a quotation about %q:\n%s", bad, out)
				}
			}
			if !strings.Contains(out, `is called but is not a function this backend can build a call to`) ||
				!strings.Contains(out, `(roadmap L11.1, ADR 0166)`) {
				t.Fatalf("the refusal is no longer the emitter's own sentence:\n%s", out)
			}
			// The whole message: prefix and all. A trailing sentence the pass never said would show up here.
			if line := strings.TrimRight(out, "\n"); strings.Count(line, "codegen:") != 1 {
				t.Fatalf("the refusal carries more than one sentence:\n%s", out)
			}
		})
	}
}

// TestTheDeclineCarryingBackendStillAnswersWhatTheReferencePrints is the regression half at the CLI: the
// programs the pass DOES rewrite must keep answering, on both compiled roads, byte for byte the reference's
// bytes. Handing the decline out of the pass alongside the program must not have cost the pass its rewrite.
func TestTheDeclineCarryingBackendStillAnswersWhatTheReferencePrints(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, tc := range []struct{ name, src string }{
		{"an applied lambda that calls its own parameter", "print((lambda f: f(3))(lambda x: x * 2))\n"},
		{"a higher-order def called with a lambda", "def apply1(fn, v):\n    return fn(v)\n\nprint(apply1(lambda x: x + 1, 41))\n"},
		{"a clone answering its own recursion", "def fact(n, f):\n    return 1 if n == 0 else f(n) + fact(n - 1, f)\n\nprint(fact(4, lambda v: v))\n"},
		{"a named lambda reached through a hook", "neg = lambda v: 0 - v\nprint(sorted([3, 1, 2], key=neg))\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			want, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Skip("no python3 available to cross-check")
			}
			p := writeSrc(t, dir, "callable_answers.gy", tc.src)
			if out, code := cliRunMerged(t, "--aot", "--file", p); code != 0 || out != want {
				t.Fatalf("linked binary: code=%d out=%q, want %q", code, out, want)
			}
			if out, code := cliRunMerged(t, "--eval", tc.src); code != 0 || out != want {
				t.Fatalf("--eval: code=%d out=%q, want %q", code, out, want)
			}
		})
	}
}

// callablesAccountOf reads the pass's own document for a source — out of the SHIPPED tool, not out of the
// library, because the claim being checked is that the two faces of one `gustyc` run agree — and returns the
// single sentence it refused on. A document with no refusal, or with more than one, is a failure of the row
// that asked for it: the comparisons below are only meaningful against one sentence.
func callablesAccountOf(t *testing.T, src string) string {
	t.Helper()
	doc := cliRun(t, "--callables", src)
	var rep struct {
		Rewritten bool     `json:"rewritten"`
		Refused   []string `json:"refused"`
	}
	if err := json.Unmarshal([]byte(doc), &rep); err != nil {
		t.Fatalf("--callables is not JSON: %v\n%s", err, doc)
	}
	if rep.Rewritten || len(rep.Refused) != 1 || rep.Refused[0] == "" {
		t.Fatalf("--callables does not describe one declined program: %s", doc)
	}
	return rep.Refused[0]
}

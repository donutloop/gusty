package integration

// integration/named_hook_test.go — the reference leg for the hook that reads its key out of the NAME a
// lambda was bound to (roadmap L11.7 · Gap R.281, ADR 0369).
//
// `pkg/lang/named_hook_test.go` decides the SHAPE: which `key=` arguments the defunctionalisation pass
// retargets, what the emitted module calls, and which programs the pass withdraws from. This file asks the
// question no table can answer by itself: does the program that reads `key=neg` print what `python3`
// prints? Every expectation here is read off the reference at run time by `cpythonOut`; a compiled answer
// that differs fails, because an expectation copied out of a previous build is not evidence about a
// language (AGENTS.md's two legs — the record and the reference, never two engines).
//
// Three outcomes are distinguished, because the row spans all three:
//
//   - the reference ANSWERS → the CLI prints the reference's bytes at exit 0;
//   - the reference RAISES (`key=` holds a number, a container, a key of the wrong arity) → the compiled
//     program traps at exit 3 carrying the reference's own sentence, and the sentence names `<lambda>`,
//     not the symbol the pass minted (ADR 0166, ADR 0345 §3);
//   - the door cannot answer (a container built at run time) → an exit-1 refusal naming the program's
//     spelling, never exit 2, which ADR 0166 reserves for a compiler bug.
//
// The corpus program is `integration/programs/named_hook.gy`, the 209th row of the conformance matrix.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// namedHookCLIPrograms is the answered family, asked of the reference inside each case.
var namedHookCLIPrograms = []struct{ name, src string }{
	{
		"the row: sorted reads its key from a name holding a lambda",
		"neg = lambda v: 0 - v\nprint(sorted([3, 1, 2], key=neg))\n",
	},
	{
		"min reads its key from the same name",
		"neg = lambda v: 0 - v\nprint(min([3, 1, 2], key=neg))\n",
	},
	{
		"max reads its key from the same name",
		"neg = lambda v: 0 - v\nprint(max([3, 1, 2], key=neg))\n",
	},
	{
		"reverse=True counts the other side of the same key",
		"neg = lambda v: 0 - v\nprint(sorted([3, 1, 2], key=neg, reverse=True))\n",
	},
	{
		"the callable reaches the hook under a second name",
		"neg = lambda v: 0 - v\ntwo = neg\nprint(sorted([3, 1, 2], key=two))\n",
	},
	{
		"one callable, both names, two hooks",
		"neg = lambda v: 0 - v\ntwo = neg\nprint(min([3, 1, 2], key=two), max([3, 1, 2], key=neg))\n",
	},
	{
		"three hooks share one name",
		"neg = lambda v: 0 - v\nprint(sorted([1, 2, 3], key=neg), max([1, 2, 3], key=neg), min([1, 2, 3], key=neg))\n",
	},
	{
		"a hook inside a loop reads the name each time",
		"neg = lambda v: 0 - v\nfor k in range(2):\n    print(sorted([3, 1, 2], key=neg))\n",
	},
	{
		"a key that answers a text orders by the interned word",
		"label = lambda v: \"a\" if v > 1 else \"b\"\nprint(sorted([3, 1, 2], key=label))\n",
	},
	{
		"a key that answers a text of the elements themselves",
		"shout = lambda s: s.upper()\nprint(sorted([\"b\", \"A\", \"a\"], key=shout))\n",
	},
	{
		"a tie keeps the order the program wrote, through a name",
		"par = lambda v: v % 2\nprint(sorted([3, 2, 1], key=par))\n",
	},
	{
		"a key lambda with a default parameter",
		"keyed = lambda v, w=1: 0 - v - w\nprint(sorted([3, 1, 2], key=keyed))\n",
	},
	{
		"the control: a `def`'d name is the key",
		"def neg(v):\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=neg))\n",
	},
	{
		"the control: the hook lambda written inline",
		"print(sorted([3, 1, 2], key=lambda v: 0 - v))\n",
	},
	{
		"the control: a builtin is the key",
		"print(sorted([-3, 1, -2], key=abs))\n",
	},
	{
		"the control: key=None is no key at all",
		"neg = lambda v: 0 - v\nprint(sorted([3, 1, 2], key=None))\n",
	},
	{
		"the control: the name is called directly as well as through the hook",
		"neg = lambda v: 0 - v\nprint(neg(1), sorted([3, 1, 2], key=neg))\n",
	},
	{
		"the control: an empty container never calls the key",
		"neg = lambda v: 0 - v\nprint(sorted([], key=neg))\n",
	},
}

// TestAHookReadsItsNamedCallableAtTheCLI is the reference leg: the linked program and the JIT are asked
// beside `python3`, and the compiled answer has to match the reference rather than its own last build.
// Exit 2 — LLVM rejecting the compiler's own module — is a compiler bug on every row here.
func TestAHookReadsItsNamedCallableAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range namedHookCLIPrograms {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "named_hook.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("%s rejected the compiler's own module (ADR 0166 / exit-code contract):\n%s", engine, cliRun(t, engine, path))
				}
				if code != 0 {
					t.Fatalf("%s exited %d on a program the reference prints %q: %s", engine, code, want, cliRun(t, engine, path))
				}
				if out != want {
					t.Fatalf("%s printed %q, CPython printed %q — for %s", engine, out, want, tc.src)
				}
			}
		})
	}
}

// TestTheNamedHookCorpusProgramAnswersWhatCPythonAnswers walks the corpus row itself through the CLI. The
// matrix covers it as a pin; this asks it live, against a reference running now.
func TestTheNamedHookCorpusProgramAnswersWhatCPythonAnswers(t *testing.T) {
	t.Parallel()
	src := readProgramSrc("named_hook")
	path := writeSrc(t, t.TempDir(), "named_hook.gy", src)
	want, ok := cpythonOut(t, path)
	if !ok {
		t.Skip("no live python3 to ask")
	}
	if strings.TrimSpace(want) == "" {
		t.Fatal("the reference printed nothing for the corpus program")
	}
	for _, engine := range cliEngines {
		out, code := cliRunCode(t, engine, path)
		if code == 2 {
			t.Fatalf("%s rejected the compiler's own module (ADR 0166):\n%s", engine, cliRun(t, engine, path))
		}
		if code != 0 {
			t.Fatalf("%s exited %d on the corpus program the reference prints:\n%s", engine, code, cliRun(t, engine, path))
		}
		if out != want {
			t.Fatalf("%s diverged from the reference on the corpus program:\n--- compiled ---\n%s\n--- reference ---\n%s", engine, out, want)
		}
	}
}

// TestTheNamedHookProgramsAreOnTheRecord is ADR 0302's rule at this altitude: every program this row
// answers has an answer on record, and a record that disappeared fails here rather than passing by having
// nothing to compare against. The records were written from the reference by tools/recmerge, never from
// this backend.
func TestTheNamedHookProgramsAreOnTheRecord(t *testing.T) {
	t.Parallel()
	onRecord := 0
	sources := []string{readProgramSrc("named_hook")}
	for _, tc := range namedHookCLIPrograms {
		sources = append(sources, tc.src)
	}
	for i, src := range sources {
		// No t.Parallel: the parent's "did any row hit the record" check runs when this function
		// returns, and a parallel subtest has not run by then.
		name := "corpus program"
		if i > 0 {
			name = namedHookCLIPrograms[i-1].name
		}
		t.Run(name, func(t *testing.T) {
			if !lang.HasGoldenAnswer(src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge, never from this backend", src)
			}
			onRecord++
		})
	}
	if onRecord == 0 {
		t.Fatal("not one of this row's programs is on the record: the record leg is not being walked")
	}
}

// TestTheNamedHookTrapAtTheCLI pins the half of the row that must NOT become an answer. A key that is not
// callable, or that cannot be called with one element, is the reference's TypeError, mid-run, at exit 3 —
// and the sentence is the reference's, which names the anonymous function `<lambda>` where the pass minted
// a symbol (ADR 0166, ADR 0345 §3; the leak Gap R.325 measured on the inline spelling).
func TestTheNamedHookTrapAtTheCLI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, tc := range []struct {
		name string
		src  string
		msg  string
	}{
		{
			"a name holding a number is not callable",
			"n = 5\nprint(sorted([3, 1, 2], key=n))\n",
			"TypeError: 'int' object is not callable",
		},
		{
			"a name rebound to a number is not callable either",
			"neg = lambda v: 0 - v\nneg = 3\nprint(sorted([3, 1, 2], key=neg))\n",
			"TypeError: 'int' object is not callable",
		},
		{
			"a container is not callable",
			"print(sorted([3, 1, 2], key={\"a\": 1}))\n",
			"TypeError: 'dict' object is not callable",
		},
		{
			"a key lambda that takes nothing says so the reference's way",
			"k = lambda: 0\nprint(sorted([3, 1, 2], key=k))\n",
			"TypeError: <lambda>() takes 0 positional arguments but 1 was given",
		},
		{
			"a key lambda missing a required parameter says so too",
			"k = lambda a, b: 0\nprint(sorted([3, 1, 2], key=k))\n",
			"TypeError: <lambda>() missing 1 required positional argument: 'b'",
		},
		{
			"and the inline spelling says the same thing",
			"print(sorted([3, 1, 2], key=lambda: 0))\n",
			"TypeError: <lambda>() takes 0 positional arguments but 1 was given",
		},
		{
			"a `def`'d key with no parameter keeps its own name in the sentence",
			"def k():\n    return 0\n\nprint(sorted([3, 1, 2], key=k))\n",
			"TypeError: k() takes 0 positional arguments but 1 was given",
		},
		{
			"min over nothing has no winner",
			"neg = lambda v: 0 - v\nprint(max([], key=neg))\n",
			"ValueError: max() iterable argument is empty",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// The reference has to raise for this same source, or the row describes the wrong program.
			pyOut, pyOK := cpythonPlainOut(t, dir, tc.src)
			if !pyOK {
				t.Skip("no python3 available to cross-check")
			}
			if !strings.Contains(pyOut, tc.msg) {
				t.Fatalf("row is stale: the reference does not raise %q for %q (%s)", tc.msg, tc.src, pyOut)
			}
			for _, engine := range cliEngines {
				p := writeSrc(t, dir, "named_hook_trap", tc.src)
				out, code := cliRunMerged(t, engine, "--file", p)
				if code == 2 {
					t.Fatalf("%s exited 2 — ADR 0166's compiler-bug code — where the reference raises: %s", engine, out)
				}
				if code != 3 {
					t.Fatalf("%s exited %d, want 3 (the trap the reference traps on): %s", engine, code, out)
				}
				if !strings.Contains(out, tc.msg) {
					t.Fatalf("%s did not carry the reference's sentence %q: %s", engine, tc.msg, out)
				}
				if strings.Contains(out, "$lambda$") || strings.Contains(out, "$callable$") {
					t.Fatalf("%s named the symbol the pass minted rather than the program's word: %s", engine, out)
				}
				if strings.Contains(out, "goroutine") || strings.Contains(out, "index out of range") {
					t.Fatalf("%s leaked a Go panic instead of the compiler's own diagnostic: %s", engine, out)
				}
			}
		})
	}
}

// TestTheNamedHookRefusalsStayRefusalsAtTheCLI is the third outcome: a door that cannot answer says so at
// exit 1, in words that name the program's own spelling, and points at the row that owns the missing half.
// Each row here is refused on ALL three spellings of the key — `def`, inline lambda and name — which is
// what makes it a door limitation rather than a defect in the retarget this row added.
func TestTheNamedHookRefusalsStayRefusalsAtTheCLI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, tc := range []struct {
		name   string
		src    string
		phrase string
	}{
		{
			"a container built at run time, key read from a name",
			"xs = [3, 1, 2]\nneg = lambda v: 0 - v\nprint(sorted(xs, key=neg))\n",
			"sorted(xs, key=<lambda>) is refused",
		},
		{
			"the same container, the same key spelled as a def",
			"xs = [3, 1, 2]\ndef neg(v):\n    return 0 - v\n\nprint(sorted(xs, key=neg))\n",
			"sorted(xs, key=neg) is refused",
		},
		{
			"the same container again, the key written inline",
			"xs = [3, 1, 2]\nprint(sorted(xs, key=lambda v: 0 - v))\n",
			"is refused",
		},
		{
			"min over a run-time container",
			"xs = [3, 1, 2]\nneg = lambda v: 0 - v\nprint(max(xs, key=neg))\n",
			"max(xs, key=<lambda>) is refused",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, engine := range cliEngines {
				p := writeSrc(t, dir, "named_hook_refusal", tc.src)
				out, code := cliRunMerged(t, engine, "--file", p)
				if code == 0 {
					t.Fatalf("%s answered %q at exit 0; the door still has no value word to carry an element to the key", engine, out)
				}
				if code == 2 {
					t.Fatalf("%s exited 2 on a refusal (ADR 0166): %s", engine, out)
				}
				if code == 3 {
					t.Fatalf("%s trapped where the reference prints: %s", engine, out)
				}
				if !strings.Contains(out, tc.phrase) {
					t.Fatalf("%s refused without naming %q:\n%s", engine, tc.phrase, out)
				}
				if strings.Contains(out, "$lambda$") || strings.Contains(out, "$callable$") {
					t.Fatalf("%s named the symbol the pass minted rather than the program's word:\n%s", engine, out)
				}
				if !strings.Contains(out, "roadmap L11") {
					t.Fatalf("%s refused without pointing at its row:\n%s", engine, out)
				}
			}
		})
	}
}

// TestTheNamedHookAccountCarriesTheKeyCount is the machine path: an agent reading the pass's own JSON can
// tell a hook whose key the pass pointed at its callable from one it never looked at, without scraping IR.
func TestTheNamedHookAccountCarriesTheKeyCount(t *testing.T) {
	t.Parallel()
	doc, err := lang.CallablesJSON("neg = lambda v: 0 - v\nprint(sorted([3, 1, 2], key=neg))\n")
	if err != nil {
		t.Fatalf("the account failed to print: %v", err)
	}
	for _, want := range []string{`"key_arguments": 1`, `"hoisted_lambdas": 1`, `"rewritten": true`, "\"neg$lambda$"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("the account is missing %s:\n%s", want, doc)
		}
	}
	var account struct {
		Rewritten    bool     `json:"rewritten"`
		KeyArguments int      `json:"key_arguments"`
		Refused      []string `json:"refused"`
	}
	if uerr := json.Unmarshal([]byte(doc), &account); uerr != nil {
		t.Fatalf("the account is not the document the schema promises: %v\n%s", uerr, doc)
	}
	if !account.Rewritten || account.KeyArguments != 1 || len(account.Refused) != 0 {
		t.Fatalf("the account does not report the retarget:\n%s", doc)
	}
	// The schema describes the field, or an agent consuming it is reading an undocumented key.
	if !strings.Contains(lang.ASTIRSchema, "key_arguments") {
		t.Fatal("the pass's account carries key_arguments, and the schema (--schema) does not describe it")
	}
}

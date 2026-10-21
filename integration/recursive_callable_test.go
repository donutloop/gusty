package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// recursive_callable_test.go — the reference leg for the higher-order body that calls itself
// (roadmap L11.7 · Gap R.267, and the entry-tuple ordinal it exposed, Gap R.313; ADR 0364).
//
// `pkg/lang/recursive_callable_test.go` decides the SHAPE: which calls of a body are the recursion, which
// clone a recursion belongs to, what the clone's IR looks like, and which recursions the pass declines in
// words. This file asks the question no table can answer by itself: does the clone that calls ITSELF print
// what CPython prints? Every expectation below is read off `python3` at run time by `cpythonOut`; a
// compiled answer that differs fails, because a literal copied out of a previous build is not evidence
// about a language (AGENTS.md's two legs).
//
// The programs are the same sources as the compiled-leg table, in the same order, so the two files read as
// one experiment: same programs, two witnesses.

var recursiveHigherOrderPrograms = []struct{ name, src string }{
	{
		"the row's own program",
		"def fact(n, f):\n    if n <= 1:\n        return 1\n\n    return f(n) * fact(n - 1, f)\n\nprint(fact(4, lambda x: x))\n",
	},
	{
		"a declared function enters the recursion",
		"def fact(n, f):\n    if n <= 1:\n        return 1\n\n    return f(n) * fact(n - 1, f)\n\ndef dbl(x):\n    return x * 2\n\nprint(fact(3, dbl))\n",
	},
	{
		"a recursion that sums",
		"def down(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + down(n - 1, f)\n\nprint(down(3, lambda v: v))\n",
	},
	{
		"the same recursion, two different callables",
		"def down(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + down(n - 1, f)\n\ndef sq(x):\n    return x * x\n\nprint(down(3, sq), down(2, lambda v: 0 - v))\n",
	},
	{
		"a callable that is not the first parameter, recursively",
		"def total(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + total(n - 1, f)\n\nprint(total(4, lambda v: 0 - v))\n",
	},
	{
		"two called parameters, neither first",
		"def pick(a, f, g, b):\n    if a <= 0:\n        return b\n\n    return f(a) + g(a) + pick(a - 1, f, g, b)\n\nprint(pick(2, lambda v: v, lambda v: v * 2, 100))\n",
	},
	{
		"a parameter the body calls twice, no recursion",
		"def apply2(v, f):\n    return f(f(v))\n\nprint(apply2(3, lambda x: x * 3))\n",
	},
	{
		"the recursion prints on the way down",
		"def walk(n, f):\n    if n <= 0:\n        return 0\n\n    print(f(n))\n    return walk(n - 1, f)\n\nprint(walk(3, lambda v: v * 10))\n",
	},
	{
		"the recursion is entered from another body",
		"def total(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + total(n - 1, f)\n\ndef outer(k):\n    return total(k, lambda v: v * 2)\n\nprint(outer(3))\n",
	},
	{
		"five deep",
		"def fact(n, f):\n    if n <= 1:\n        return 1\n\n    return f(n) * fact(n - 1, f)\n\nprint(fact(5, lambda v: v))\n",
	},
	{
		"the base case asks the callable too",
		"def walk(n, f):\n    if n <= 0:\n        return f(0)\n\n    return f(n) + walk(n - 1, f)\n\nprint(walk(3, lambda v: v))\n",
	},
	{
		"the answer is used as a number",
		"def down(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + down(n - 1, f)\n\nprint(1 + down(3, lambda v: v), down(3, lambda v: v) * 2)\n",
	},
	{
		"a loop calls the parameter and the recursion closes the body",
		"def each(n, f):\n    total = 0\n    for k in range(1, n + 1):\n        total = total + f(k)\n\n    if n <= 0:\n        return total\n\n    return total + each(n - 1, f)\n\nprint(each(3, lambda v: v * 2))\n",
	},
	{
		"a callable that answers a verdict",
		"def count(n, f):\n    if n <= 0:\n        return 0\n\n    if f(n):\n        return 1 + count(n - 1, f)\n\n    return count(n - 1, f)\n\nprint(count(5, lambda v: v > 2))\n",
	},
	{
		"two recursive higher-order bodies in one program",
		"def up(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + up(n - 1, f)\n\ndef dn(n, g):\n    if n <= 0:\n        return 1\n\n    return g(n) * dn(n - 1, g)\n\nprint(up(3, lambda v: v), dn(3, lambda v: v))\n",
	},
	{
		"the recursion is a condition's operand",
		"def down(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + down(n - 1, f)\n\nprint(1 if down(3, lambda v: v) > 5 else 0)\n",
	},
	{
		"a recursive higher-order body beside a plain one",
		"def twice(fn, v):\n    return fn(v) + fn(v)\n\ndef down(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + down(n - 1, f)\n\ndef d2(x):\n    return x * 2\n\nprint(twice(d2, 3), down(3, d2))\n",
	},
	{
		"the recursion feeds a builtin's argument",
		"def step(n, f):\n    if n <= 0:\n        return 0 - 6\n\n    return abs(f(step(n - 1, f)))\n\nprint(step(3, lambda v: v))\n",
	},
	{
		"the negative control: a plain recursion, no callable parameter",
		"def fact(n):\n    if n <= 1:\n        return 1\n\n    return n * fact(n - 1)\n\nprint(fact(5))\n",
	},
	{
		"the negative control: a higher-order body with no recursion",
		"def apply1(fn, v):\n    return fn(v)\n\ndef inc(x):\n    return x + 1\n\nprint(apply1(inc, 41))\n",
	},
	{
		"a recursive callable parameter omitted from the self-call",
		"def run(n, f=lambda x: x):\n    if n <= 0:\n        return 0\n\n    return f(n) + run(n - 1)\n\nprint(run(3))\n",
	},
	{
		"a recursive callable parameter repeated as a separate lambda",
		"def run(n, f=lambda x: x):\n    if n <= 0:\n        return 0\n\n    return f(n) + run(n - 1, lambda x: x)\n\nprint(run(3))\n",
	},
	{
		"a recursive callable parameter defaulted to a declared function",
		"def inc(x):\n    return x + 1\n\ndef run(n, f=inc):\n    if n <= 0:\n        return 0\n\n    return f(n) + run(n - 1)\n\nprint(run(3))\n",
	},
	{
		"a recursion between two callable parameters and an ordinary default",
		"def inc(x):\n    return x + 1\n\ndef dbl(x):\n    return x * 2\n\ndef run(n, a=inc, mode=1, b=dbl):\n    if n <= 0:\n        return 0\n\n    return a(n) + b(n) + mode + run(n - 1)\n\nprint(run(2))\n",
	},
}

// TestARecursiveHigherOrderBodyAnswersWhatCPythonAnswersAtTheCLI is the reference leg: the module this
// backend writes and the JIT that runs it are asked beside `python3`, and the compiled answer has to match
// the reference's rather than its own last build. Exit 2 — LLVM rejecting the compiler's own module — is a
// compiler bug on every row here, including the ones the pass declines.
func TestARecursiveHigherOrderBodyAnswersWhatCPythonAnswersAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range recursiveHigherOrderPrograms {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "recursive_callable.gy", tc.src)
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

// TestTheRecursiveCallableProgramsAreOnTheRecord is ADR 0302's rule at this altitude: every program this
// row answers has an answer on record, and a record that disappeared fails here rather than passing by
// having nothing to compare against. The record was written from the reference by tools/recmerge, never
// from this backend.
func TestTheRecursiveCallableProgramsAreOnTheRecord(t *testing.T) {
	t.Parallel()
	onRecord := 0
	for _, tc := range recursiveHigherOrderPrograms {
		// No t.Parallel: the parent's "did any row hit the record" check runs when this function
		// returns, and a parallel subtest has not run by then.
		t.Run(tc.name, func(t *testing.T) {
			if !lang.HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge, never from this backend", tc.src)
			}
			onRecord++
		})
	}
	if onRecord == 0 {
		t.Fatal("not one of this row's programs is on the record: the record leg is not being walked")
	}
}

// TestTheRecursiveCallableRefusalsStayRefusalsAtTheCLI is the other half of the promise: the recursions
// this pass cannot follow keep an exit-1 refusal that names the shape, never exit 2 (a refusal is not a
// broken module), and the pass's own account says which recursion it declined (ADR 0166, ADR 0273).
func TestTheRecursiveCallableRefusalsStayRefusalsAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		src     string
		phrase  string
		decline string
	}{
		{
			"the recursion swaps the callable mid-flight",
			"def run(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + run(n - 1, other)\n\ndef other(x):\n    return x\n\nprint(run(2, lambda v: v))\n",
			"is not a function this backend can build a call to",
			"hands \"f\" something other than itself",
		},
		{
			"two higher-order bodies that recurse into each other",
			"def a(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + b(n - 1, f)\n\ndef b(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + a(n - 1, f)\n\ndef inc(x):\n    return x\n\nprint(a(3, inc))\n",
			"is not a function this backend can build a call to",
			"a recursion between two higher-order functions",
		},
		{
			"the recursion hands the callable a text",
			"def run(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + run(n - 1, \"x\")\n\nprint(run(2, lambda v: v))\n",
			"is not a function this backend can build a call to",
			"hands \"f\" something other than itself",
		},
		{
			"the recursion omits a required callable parameter",
			"def run(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + run(n - 1)\n\nprint(run(2, lambda v: v))\n",
			"expects 2 arguments",
			"",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "recursive_callable_refusal.gy", tc.src)
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 0 {
					t.Fatalf("%s answered %q at exit 0; this recursion is still owed by roadmap L11.7", engine, out)
				}
				if code == 2 {
					t.Fatalf("%s exited 2 on a refusal (ADR 0166): %s", engine, out)
				}
				combined := cliRun(t, engine, path)
				if !strings.Contains(combined, tc.phrase) {
					t.Fatalf("%s refused without naming %q:\n%s", engine, tc.phrase, combined)
				}
			}
			doc, err := lang.CallablesJSON(tc.src)
			if err != nil {
				t.Fatalf("the account of a declined program is an answer, not a tool failure: %v", err)
			}
			// The account is JSON, so a sentence arrives with its inner quotes escaped: compare the
			// decoded field rather than the bytes of the document, or a refusal the pass names exactly
			// reads as a pass that said nothing.
			var account struct {
				Rewritten bool     `json:"rewritten"`
				Refused   []string `json:"refused"`
			}
			if uerr := json.Unmarshal([]byte(doc), &account); uerr != nil {
				t.Fatalf("the account is not the document the schema promises: %v\n%s", uerr, doc)
			}
			if account.Rewritten {
				t.Fatalf("the pass rewrote a recursion it says it declined:\n%s", doc)
			}
			joined := strings.Join(account.Refused, "\n")
			if tc.decline != "" {
				if !strings.Contains(joined, tc.decline) {
					t.Fatalf("the pass did not say which recursion it declined (%s):\n%s", tc.decline, joined)
				}
				if !strings.Contains(joined, "roadmap L11.7") {
					t.Fatalf("the decline does not point at its row:\n%s", joined)
				}
			}
		})
	}
}

// TestTheRecursiveCallableAccountCarriesTheRecursionCount is the machine path at the CLI: the account an
// agent reads says how many self-calls the rewrite answered, and the count is not a field that only the
// Go side can see.
func TestTheRecursiveCallableAccountCarriesTheRecursionCount(t *testing.T) {
	t.Parallel()
	doc, err := lang.CallablesJSON("def fact(n, f):\n    if n <= 1:\n        return 1\n\n    return f(n) * fact(n - 1, f)\n\ndef dbl(x):\n    return x * 2\n\nprint(fact(3, dbl), fact(3, lambda v: v))\n")
	if err != nil {
		t.Fatalf("the account failed to print: %v", err)
	}
	for _, want := range []string{`"recursive_sites": 2`, `"clones": 2`, `"rewritten": true`, "\"fact\""} {
		if !strings.Contains(doc, want) {
			t.Fatalf("the account is missing %s:\n%s", want, doc)
		}
	}
}

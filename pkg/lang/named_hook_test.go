package lang

// pkg/lang/named_hook_test.go — the hook that reads its key out of the NAME a lambda was bound to
// (roadmap L11.7 · Gap R.281, ADR 0345's alias road read at the argument position, ADR 0340's key door).
//
// `sorted`, `min` and `max` call their `key=` argument once per element. That call is not in the source
// and not in the AST: the door writes it. So the one position where a name holds a callable and is never
// spelled as a callee is also the one position where the program can be answered by a word instead of a
// function:
//
//	neg = lambda v: 0 - v
//	print(sorted([3, 1, 2], key=neg))   #  TypeError: 'int' object is not callable  ← this toolchain
//	                                    #  [3, 2, 1]                                ← the reference
//
// Two controls localised it exactly (they are in the tables below, and they stayed correct while the row
// was open): the same hook written INLINE answers, and the same hook written as a `def` answers. The hoist
// worked and the call worked — what was missing is that binding the hoisted lambda to a name left the name
// holding the payload word `0`, the same number Gap R.277's defaults read.
//
// The fix is the alias road performed one position further out. A name the pass gave a declaration of its
// own is retargeted in the `key=` slot to that declaration, exactly as a call's callee is, and the door
// then meets a name with a `define` behind it. `key_arguments` in the pass's account (`gustyc
// --callables`) is the witness that the retarget happened, and the diagnostics keep speaking the program's
// word and the reference's `<lambda>` rather than the symbol the pass minted (ADR 0166, ADR 0345 §3 — the
// half of this row that Gap R.325 measured on the inline road).
//
// The legs are the two this tree has: the answer on record (`testdata/interpreter-golden.json`, written
// from the reference by tools/recmerge) and the reference itself (integration/named_hook_test.go asks
// `python3` beside the CLI). Nothing here compares two engines; there is one.

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// namedHookAnswered is the family the reference answers and this backend must answer identically. Every
// `want` is what `python3` prints; integration/named_hook_test.go re-derives the same strings from the
// reference at run time, so a `want` that was ever tuned to keep this file green is caught there.
var namedHookAnswered = []struct {
	name string
	src  string
	want string
}{
	// ---- the row ----
	{
		"the row: sorted, key read from a name holding a lambda",
		"neg = lambda v: 0 - v\nprint(sorted([3, 1, 2], key=neg))",
		"[3, 2, 1]",
	},
	{
		"min reads its key from the same name",
		"neg = lambda v: 0 - v\nprint(min([3, 1, 2], key=neg))",
		"3",
	},
	{
		"max reads its key from the same name",
		"neg = lambda v: 0 - v\nprint(max([3, 1, 2], key=neg))",
		"1",
	},
	{
		"reverse=True counts the other side of the same key",
		"neg = lambda v: 0 - v\nprint(sorted([3, 1, 2], key=neg, reverse=True))",
		"[1, 2, 3]",
	},

	// ---- the same callable under two names (ADR 0345's alias road, one position out) ----
	{
		"the callable is handed to the hook under a second name",
		"neg = lambda v: 0 - v\ntwo = neg\nprint(sorted([3, 1, 2], key=two))",
		"[3, 2, 1]",
	},
	{
		"one callable, both names, two hooks in one program",
		"neg = lambda v: 0 - v\ntwo = neg\nprint(min([3, 1, 2], key=two), max([3, 1, 2], key=neg))",
		"3 1",
	},
	{
		"three hooks share one name",
		"neg = lambda v: 0 - v\nprint(sorted([3, 1, 2], key=neg), max([1, 2, 3], key=neg), min([1, 2, 3], key=neg))",
		"[3, 2, 1] 1 3",
	},
	{
		"a hook inside a loop reads the name each time",
		"neg = lambda v: 0 - v\nfor k in range(2):\n    print(sorted([3, 1, 2], key=neg))",
		"[3, 2, 1]\n[3, 2, 1]",
	},

	// ---- the key's ANSWER kind: the door compares numbers one way and texts another ----
	{
		"a key that answers a text orders by the interned word",
		"label = lambda v: \"a\" if v > 1 else \"b\"\nprint(sorted([3, 1, 2], key=label))",
		"[3, 2, 1]",
	},
	{
		"a key that answers a text of the elements themselves",
		"shout = lambda s: s.upper()\nprint(sorted([\"b\", \"A\", \"a\"], key=shout))",
		"['A', 'a', 'b']",
	},
	{
		"a tie keeps the order the program wrote (the sorter's stability, through a name)",
		"par = lambda v: v % 2\nprint(sorted([3, 2, 1], key=par))",
		"[2, 3, 1]",
	},

	// ---- the lambda's own shapes, beside the hook (ADR 0352's parameters, ADR 0345's anonymous word) ----
	{
		"a key lambda with a default parameter",
		"k = lambda v, w=1: 0 - v - w\nprint(sorted([3, 1, 2], key=k))",
		"[3, 2, 1]",
	},
	{
		"a key lambda with an optional second parameter is still callable with one",
		"k = lambda v, w=1: 0 - v\nprint(max([3, 1, 2], key=k))",
		"1",
	},

	// ---- the controls: every spelling of the same hook that already answered ----
	{
		"the control: a `def`'d name is the key",
		"def neg(v):\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=neg))",
		"[3, 2, 1]",
	},
	{
		"the control: the hook lambda written inline",
		"print(sorted([3, 1, 2], key=lambda v: 0 - v))",
		"[3, 2, 1]",
	},
	{
		"the control: a builtin is the key",
		"print(sorted([-3, 1, -2], key=abs))",
		"[1, -2, -3]",
	},
	{
		"the control: key=None is no key at all",
		"neg = lambda v: 0 - v\nprint(sorted([3, 1, 2], key=None))",
		"[1, 2, 3]",
	},
	{
		"the control: a builtin key and a named lambda key in one program",
		"neg = lambda v: 0 - v\nprint(sorted([-3, 1, -2], key=abs), sorted([3, 1, 2], key=neg))",
		"[1, -2, -3] [3, 2, 1]",
	},
	{
		"the control: the same name called directly as well as through the hook",
		"neg = lambda v: 0 - v\nprint(neg(1), sorted([3, 1, 2], key=neg))",
		"-1 [3, 2, 1]",
	},
	{
		"the control: an empty container never calls the key",
		"neg = lambda v: 0 - v\nprint(sorted([], key=neg))",
		"[]",
	},
}

// TestAHookReadsItsKeyFromTheNameALambdaWasBoundTo is the row on both of the compiled legs: the linked
// module and the JIT. A refusal fails here — the reference runs every program in this table — and so does
// a raise, because the reference's only complaint about these programs is the order it prints them in.
func TestAHookReadsItsKeyFromTheNameALambdaWasBoundTo(t *testing.T) {
	t.Parallel()
	for _, tc := range namedHookAnswered {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, tc.src)
			if refused {
				t.Fatalf("%q refused (%s); the reference prints %q", tc.src, got, tc.want+"\n")
			}
			if got != tc.want+"\n" {
				t.Fatalf("%q printed %q, want %q", tc.src, got, tc.want+"\n")
			}
			out, trap := runForParity(t, tc.src)
			if trap != "" {
				t.Fatalf("%q raised %s; the reference prints %q", tc.src, trap, tc.want)
			}
			if out != tc.want {
				t.Fatalf("%q printed %q through the JIT, want %q", tc.src, out, tc.want)
			}
		})
	}
}

// TestTheNamedHookIsOnTheRecord walks the record leg by name. These sources were refused when this file was
// written, so an absent entry FAILS the case rather than skipping it: a row cannot be closed by losing the
// answer it owed, and a record that disappeared is the same event seen from the other side.
func TestTheNamedHookIsOnTheRecord(t *testing.T) {
	t.Parallel()
	onRecord := 0
	for _, tc := range namedHookAnswered {
		t.Run(tc.name, func(t *testing.T) {
			// No t.Parallel: the parent's "did any row hit the record" check runs when this function
			// returns, and a parallel subtest has not run by then.
			if !HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record: a newly answered program is recorded from the reference by tools/recmerge, never from this backend", tc.src)
			}
			rec := goldenLookup(t, tc.src)
			if !rec.HasStd {
				t.Fatalf("%q: the record holds no stdout for a program that prints", tc.src)
			}
			if rec.Stdout != tc.want+"\n" {
				t.Errorf("%q: the record printed %q, the reference prints %q", tc.src, rec.Stdout, tc.want+"\n")
			}
			onRecord++
		})
	}
	if onRecord == 0 {
		t.Fatal("not one of this row's programs is on the record: the record leg is not being walked")
	}
}

// TestThePassRetargetsTheHooksKey is the row at the altitude it is decided at. The answer comes from the
// AST the emitter sees, so the table asserts on THAT tree: the `key=` argument names the declaration the
// pass minted, and the binding it was written under is gone from the program as a value.
func TestThePassRetargetsTheHooksKey(t *testing.T) {
	t.Parallel()
	src := "neg = lambda v: 0 - v\nprint(sorted([3, 1, 2], key=neg))\n"
	prog, err := parseProgram(src)
	if err != nil {
		t.Fatalf("the program does not parse: %v", err)
	}
	out, rep := applyDefunctionalization(prog)
	if rep == nil {
		t.Fatalf("the pass withdrew a rewrite the row answers: %q", src)
	}
	if rep.KeyArguments != 1 {
		t.Fatalf("key_arguments = %d, want 1 (the one `key=` the pass pointed at its callable)", rep.KeyArguments)
	}
	if rep.HoistedLambdas != 1 {
		t.Fatalf("hoisted_lambdas = %d, want 1 (the lambda bound to %q)", rep.HoistedLambdas, "neg")
	}
	key := hookKeyArgument(t, out)
	if key == "neg" {
		t.Fatalf("the `key=` still names the binding the pass deleted: the door would read a word where the callable lives")
	}
	if !strings.Contains(key, callableLambdaInfix) {
		t.Fatalf("the `key=` names %q, which is not the declaration this pass minted", key)
	}
	decl, declared := hookDeclarations(out)[key]
	if !declared {
		t.Fatalf("the `key=` names %q and no define carries that name: the door would call a symbol nothing defines", key)
	}
	if len(decl.Params) != 1 {
		t.Fatalf("the declaration the hook calls takes %d parameters; the door calls a key with one element", len(decl.Params))
	}
	// The account an agent reads (`gustyc --callables`) names the same symbol from the same plan.
	doc, err := CallablesJSON(src)
	if err != nil {
		t.Fatalf("the account failed to print: %v", err)
	}
	if !strings.Contains(doc, `"key_arguments": 1`) || !strings.Contains(doc, `"`+key+`"`) {
		t.Fatalf("the account does not report the key it retargeted:\n%s", doc)
	}
}

// TestTheNamedHookModuleCallsTheDeclaration is the IR leg: the hook's call is a direct `call` to the
// `define` the pass wrote, and no value ever becomes a number on the way there. `ptrtoint`/`inttoptr` is
// the shape ADR 0345 refuses to build, and this row is exactly the shape that would tempt someone to.
func TestTheNamedHookModuleCallsTheDeclaration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		from string // the prefix the minted declaration carries
	}{
		{
			"sorted",
			"neg = lambda v: 0 - v\nprint(sorted([3, 1, 2], key=neg))\n",
			"neg" + callableLambdaInfix,
		},
		{
			"max",
			"neg = lambda v: 0 - v\nprint(max([3, 1, 2], key=neg))\n",
			"neg" + callableLambdaInfix,
		},
		{
			"the alias, spelled by the second name",
			"neg = lambda v: 0 - v\ntwo = neg\nprint(sorted([3, 1, 2], key=two))\n",
			"neg" + callableLambdaInfix,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mod := compileSrc(t, tc.src)
			if !strings.Contains(mod, "define i32 @gy_"+tc.from) {
				t.Fatalf("the lambda bound to a name was not hoisted into a define:\n%s", firstLines(mod, 20))
			}
			call := regexp.MustCompile(`call i32 @gy_` + regexp.QuoteMeta(tc.from) + `[0-9a-f]+_\d+\(i32 `)
			if !call.MatchString(mod) {
				t.Fatalf("the hook never called the declaration the pass minted for the name:\n%s", firstLines(mod, 40))
			}
			for _, junk := range []string{"ptrtoint", "inttoptr"} {
				if strings.Contains(mod, junk) {
					t.Fatalf("module contains the junk shape %q:\n%s", junk, mod)
				}
			}
		})
	}
}

// TestTheNamedHookTrapKeepsTheReferencesWords is the half of the row that must NOT turn into an answer: a
// key the reference cannot call stops the program mid-run with a TypeError, and a key whose arity is wrong
// says so in the reference's sentence — which names the anonymous function `<lambda>` and not the symbol
// the pass minted (ADR 0166, ADR 0345 §3; the same leak Gap R.325 measured on the inline road).
func TestTheNamedHookTrapKeepsTheReferencesWords(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			"a name holding a number is not callable",
			"n = 5\nprint(sorted([3, 1, 2], key=n))",
			"TypeError: 'int' object is not callable",
		},
		{
			"a name rebound to a number is not callable either",
			"neg = lambda v: 0 - v\nneg = 3\nprint(sorted([3, 1, 2], key=neg))",
			"TypeError: 'int' object is not callable",
		},
		{
			"a container is not callable",
			"print(sorted([3, 1, 2], key={\"a\": 1}))",
			"TypeError: 'dict' object is not callable",
		},
		{
			"a key lambda that takes nothing says so the reference's way",
			"k = lambda: 0\nprint(sorted([3, 1, 2], key=k))",
			"TypeError: <lambda>() takes 0 positional arguments but 1 was given",
		},
		{
			"a key lambda missing a required parameter says so too",
			"k = lambda a, b: 0\nprint(sorted([3, 1, 2], key=k))",
			"TypeError: <lambda>() missing 1 required positional argument: 'b'",
		},
		{
			"and the inline spelling says the same thing",
			"print(sorted([3, 1, 2], key=lambda: 0))",
			"TypeError: <lambda>() takes 0 positional arguments but 1 was given",
		},
		{
			"a `def`'d key with no parameter keeps its own name in the sentence",
			"def k():\n    return 0\n\nprint(sorted([3, 1, 2], key=k))",
			"TypeError: k() takes 0 positional arguments but 1 was given",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, trap := runForParity(t, tc.src)
			if !strings.Contains(trap, tc.want) {
				t.Fatalf("%q trapped %q, want %q", tc.src, trap, tc.want)
			}
		})
	}
}

// TestTheNamedHookRefusalNamesWhatIsMissing is the other half of the diagnostics promise: where the door
// cannot answer, the sentence names the program's own spelling rather than a symbol no programmer wrote.
func TestTheNamedHookRefusalNamesWhatIsMissing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		src     string
		phrase  string
		decline string
	}{
		{
			"a container built at run time keeps its name in the refusal",
			"xs = [3, 1, 2]\nneg = lambda v: 0 - v\nprint(sorted(xs, key=neg))",
			"sorted(xs, key=<lambda>) is refused",
			"",
		},
		{
			"and min says the same thing about the same container",
			"xs = [3, 1, 2]\nneg = lambda v: 0 - v\nprint(max(xs, key=neg))",
			"max(xs, key=<lambda>) is refused",
			"",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("%q was answered; the door still has no value word to carry an element to the key", tc.src)
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.phrase) {
				t.Fatalf("%q refused without naming %q:\n%s", tc.src, tc.phrase, msg)
			}
			if strings.Contains(msg, callableLambdaInfix) {
				t.Fatalf("%q refused with the symbol the pass minted rather than the program's word:\n%s", tc.src, msg)
			}
			if !strings.Contains(msg, "roadmap L11") {
				t.Fatalf("the refusal does not point at its row:\n%s", msg)
			}
		})
	}
}

// TestTheNamedHookAccountCountsItsKeys is the machine path: an agent reading `--callables` can tell a hook
// whose key the pass pointed at its callable from one it never looked at, without scraping IR text.
func TestTheNamedHookAccountCountsItsKeys(t *testing.T) {
	t.Parallel()
	doc, err := CallablesJSON("neg = lambda v: 0 - v\nprint(sorted([3, 1, 2], key=neg))\n")
	if err != nil {
		t.Fatalf("the account failed to print: %v", err)
	}
	var rep struct {
		Rewritten      bool     `json:"rewritten"`
		KeyArguments   int      `json:"key_arguments"`
		HoistedLambdas int      `json:"hoisted_lambdas"`
		Symbols        []string `json:"symbols"`
		Hash           string   `json:"hash"`
		Refused        []string `json:"refused"`
	}
	if err := json.Unmarshal([]byte(doc), &rep); err != nil {
		t.Fatalf("the account is not the document the schema promises: %v\n%s", err, doc)
	}
	if !rep.Rewritten || rep.KeyArguments != 1 || rep.HoistedLambdas != 1 {
		t.Fatalf("the account does not report the key it retargeted: %s", doc)
	}
	if len(rep.Refused) != 0 {
		t.Fatalf("a rewritten program reports refusals: %v", rep.Refused)
	}
	if rep.Hash == "" || rep.Hash == "none" {
		t.Fatalf("a rewritten program has no hash: %q", rep.Hash)
	}
	// The hash covers the key retarget: two programs whose only difference is a hook the pass answered
	// must not certify each other, or the lit-style IR rows that read the hash are reading one answer
	// for two rewrites.
	other, err := CallablesJSON("neg = lambda v: 0 - v\nprint(sorted([3, 1, 2], key=neg))\nprint(sorted([1, 2], key=neg))\n")
	if err != nil {
		t.Fatalf("the account failed to print: %v", err)
	}
	var second struct {
		KeyArguments int    `json:"key_arguments"`
		Hash         string `json:"hash"`
	}
	if err := json.Unmarshal([]byte(other), &second); err != nil {
		t.Fatalf("the account is not JSON: %v\n%s", err, other)
	}
	if second.KeyArguments != 2 || second.Hash == rep.Hash {
		t.Fatalf("one hash covers two different rewrites (%s vs %s): the key retarget is not in the digest", rep.Hash, second.Hash)
	}
	// A program with no hook at all spends one comparison on the question and reports nothing.
	baseline, err := CallablesJSON("neg = lambda v: 0 - v\nprint(neg(1))\n")
	if err != nil {
		t.Fatalf("the account failed to print: %v", err)
	}
	var plain struct {
		KeyArguments int  `json:"key_arguments"`
		Rewritten    bool `json:"rewritten"`
	}
	if err := json.Unmarshal([]byte(baseline), &plain); err != nil {
		t.Fatalf("the account is not JSON: %v\n%s", err, baseline)
	}
	if plain.KeyArguments != 0 {
		t.Fatalf("a program with no `key=` reports %d key arguments:\n%s", plain.KeyArguments, baseline)
	}
}

// TestTheNamedHookLeavesProgramsThePassCannotReadAlone is the pass's all-or-nothing rule asked at this
// row: a name the pass cannot prove is called is a name it must not touch, and the program keeps the answer
// (or the refusal) the backend always gave for it. Each of these withdraws the rewrite, so the door is
// answering the untouched tree — the shapes that came back DIFFERENT would be a pass rewriting half a
// program.
func TestTheNamedHookLeavesProgramsThePassCannotReadAlone(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		why  string
	}{
		{
			"the name is also read as a value",
			"neg = lambda v: 0 - v\nprint(neg)\nprint(sorted([3, 1, 2], key=neg))",
			"the program it produced still used a function as a value",
		},
		{
			"the name is bound twice",
			"neg = lambda v: 0 - v\nneg = lambda v: v\nprint(sorted([3, 1, 2], key=neg))",
			"",
		},
		{
			"the name is the program's own `sorted`",
			"def sorted(xs):\n    return xs\n\nneg = lambda v: 0 - v\nprint(sorted([3, 1, 2], key=neg))",
			"",
		},
		{
			"the key names something the program never declared",
			"print(sorted([3, 1, 2], key=nobody))",
			"",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog, err := parseProgram(tc.src)
			if err != nil {
				t.Fatalf("the program does not parse: %v", err)
			}
			out, rep := applyDefunctionalization(prog)
			if rep == nil {
				return // declined before the rewrite, which is the pass's safest answer
			}
			if rep.Rewritten {
				t.Fatalf("the pass rewrote a program it should leave alone:\n%+v\n%s", rep, tc.src)
			}
			if out == nil {
				t.Fatalf("a declined pass returned no program at all")
			}
			if tc.why != "" && !strings.Contains(strings.Join(rep.Refused, "\n"), tc.why) {
				t.Fatalf("the pass did not say why it withdrew:\n%v", rep.Refused)
			}
		})
	}
}

// hookKeyArgument reads the `key=` argument of the program's one hook call, as the emitter reads it.
func hookKeyArgument(t *testing.T, prog *Program) string {
	t.Helper()
	found := ""
	n := 0
	var w callableWalk
	w.visit = func(node Node, enc *FuncDef) {
		c, isCall := node.(*Call)
		if !isCall {
			return
		}
		fn, isName := c.Fn.(*Name)
		if !isName {
			return
		}
		_, key, ok := keyDoorOptions(fn.Value, c.Args)
		if !ok || key == nil {
			return
		}
		n++
		found = exprSurface(key)
	}
	w.stmts(prog.Stmts, nil)
	if n != 1 {
		t.Fatalf("the table expected one hook call, found %d", n)
	}
	return found
}

// hookDeclarations is the program's own table of definitions, by name.
func hookDeclarations(prog *Program) map[string]*FuncDef {
	out := map[string]*FuncDef{}
	for _, s := range prog.Stmts {
		if fd, isDef := s.(*FuncDef); isDef {
			out[fd.Name] = fd
		}
	}
	return out
}

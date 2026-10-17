package lang

// pkg/lang/lambda_default_param_test.go — a `lambda`'s parameter list is spelled the same way a `def`'s
// is, so `lambda x, y=1: x + y` is a program and not a parse error (roadmap Gap R.263, ADR 0352).
//
// The row sat in L11.7's next-action list as "the front end's `lambda` default-parameter parse error", and
// that was the whole defect. The parser's lambda loop read plain names only, with a comment explaining why
// it could not call `parseParam()`: that routine would greedily treat the `:` body separator as a type
// annotation. So `def` got `=default` and annotations from one routine while `lambda` got a hand-written
// loop that stopped at `=`:
//
//	f = lambda x, y=1: x + y
//	print(f(1))         # parse error at 1:16: expected ":"   ← this toolchain
//	                    # 2                                     ← the reference
//
// The `:` worry is real, and it is why the default is parsed in the lambda's own loop instead of by sharing
// `parseParam()`: a lambda's body separator and an annotation's introducer are the same token, and the
// disambiguation is "is the token after `:` a type keyword". An `=` is not ambiguous at all, and no `:` is
// reachable from a default's expression — `:` is no operator, so `parseExpr()` stops at it by itself, and a
// default that contains one (`{"a": 1}`, `"a:b"`) consumes it inside its own brackets.
//
// Everything past the grammar already existed: `defunctionalizeCallables` hoists the lambda, ADR 0322's rule
// that a callable's options live at the call reads the defaults there, and the arity check already counted
// `to N`. That is why this row is one parser block and not a codegen feature — and why the tables below
// certify the parse, the answers, and the shapes that must still be refused.
//
// What the row does NOT claim is CPython-parity for a default that is not a number or a text. The probe
// session found `def d(y=None): return y` printing `0` at exit 0 where the reference prints `None`, and the
// filed table pins that with today's answer beside the reference's (roadmap Gap R.277).

import (
	"encoding/json"
	"strings"
	"testing"
)

// lambdaDefaultAnswered is the family the reference answers and this toolchain must answer identically.
// Every `want` is what `python3` prints; the integration file re-derives the same strings from the reference
// inside the test body, so a `want` that was ever tuned to make this file pass is caught there.
var lambdaDefaultAnswered = []struct {
	src  string
	want string
}{
	// ---- the row ----
	{"f = lambda x, y=1: x + y\nprint(f(1))", "2"},
	{"f = lambda x, y=1: x + y\nprint(f(1, 5))", "6"},
	{"f = lambda x=2, y=3: x * y\nprint(f(), f(2), f(2, 5))", "6 6 10"},
	{"f = lambda x=7: x\nprint(f())", "7"},

	// ---- the call sites ADR 0322 reads a callable's options at ----
	{"f = lambda x, y=1: x + y\nprint(f(1, y=9))", "10"},
	{"def two():\n    return 2\n\nf = lambda x, y=two(): x * y\nprint(f(3))", "6"},
	{"STEP = 2\nf = lambda x, k=STEP: x * k\nprint(f(3))", "6"},
	{"flag = True\nf = lambda x, y=1 if flag else 2: x + y\nprint(f(1))", "2"},
	{"f = lambda x, tag=\"?\": x + tag\nprint(f(\"a\"))", "a?"},
	// a default containing the very token the row turns on: `:` inside its own brackets
	{"f = lambda x, d={\"a\": 1}: x + d[\"a\"]\nprint(f(1))", "2"},
	{"f = lambda x, s=\"a:b\": x + s\nprint(f(\"z\"))", "za:b"},
	{"f = lambda x, y=(2): x * y\nprint(f(3))", "6"},
	// a default's name resolves where a `def`'s would — module scope, not the caller's
	{"k = 100\nf = lambda x, k=3: x * k\nprint(f(2), k)", "6 100"},

	// ---- the bodies and shapes that must not move ----
	{"f = lambda x: 1 if x else 2\nprint(f(True), f(False))", "1 2"},
	{"f = lambda x: {\"a\": x}\nprint(f(1))", "{'a': 1}"},
	{"f = lambda: 5\nprint(f())", "5"},
	{"f = lambda x: x + 1\nprint(f(1))", "2"},
	{"print(sorted([3, 1, 2], key=lambda v: 0 - v))", "[3, 2, 1]"},
	{"def apply(g, v):\n    return g(v, 2)\n\nprint(apply(lambda x, k: x * k, 4))", "8"},

	// ---- the `def` road beside it: the row is parity with a `def`, so a `def` must not move ----
	{"def d(a=2, b=3):\n    return a * b\n\nprint(d(), d(5), d(5, 5))", "6 15 25"},
	{"def d(x, y=1):\n    return x + y\n\nprint(d(1, y=9))", "10"},
}

// TestALambdaTakesDefaultParameters is the row on the compiled leg and on the JIT: each shape prints what the
// reference prints, and a refusal is a failure, because the reference runs every one of these.
func TestALambdaTakesDefaultParameters(t *testing.T) {
	t.Parallel()
	for _, tt := range lambdaDefaultAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); the reference prints %q", tt.src, got, tt.want+"\n")
			}
			if got != tt.want+"\n" {
				t.Fatalf("%q printed %q, want %q", tt.src, got, tt.want+"\n")
			}
			out, trap := runForParity(t, tt.src)
			if trap != "" {
				t.Fatalf("%q raised %s; the reference prints %q", tt.src, trap, tt.want)
			}
			if out != tt.want {
				t.Fatalf("%q printed %q through the JIT, want %q", tt.src, out, tt.want)
			}
		})
	}
}

// TestALambdaDefaultIsOnTheRecord asks the record leg by name. These sources are newly answered, so an absent
// entry fails the case instead of skipping it: a row cannot be closed by losing its expectation.
func TestALambdaDefaultIsOnTheRecord(t *testing.T) {
	t.Parallel()
	for _, tt := range lambdaDefaultAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			if !HasGoldenAnswer(tt.src) {
				t.Fatalf("%q is not on the record: a newly answered program is recorded from the reference, not self-certified", tt.src)
			}
			rec := goldenLookup(t, tt.src)
			if !rec.HasStd {
				t.Fatalf("%q: the record holds no stdout for a program that prints", tt.src)
			}
			if rec.Stdout != tt.want+"\n" {
				t.Errorf("%q: the record printed %q, the reference prints %q", tt.src, rec.Stdout, tt.want+"\n")
			}
		})
	}
}

// lambdaDefaultASTCases ask the grammar directly. Asserted on the AST rather than on printed answers because
// the row IS a grammar row: a printed-answer table can be satisfied by a parser that accepted something
// else and dropped the value on the floor, which is exactly what "the default is never bound" would look
// like from the outside.
var lambdaDefaultASTCases = []struct {
	name     string
	src      string
	params   int // how many parameters the lambda came with
	defaults int // how many of them carry a default
}{
	{name: "the row", src: "f = lambda x, y=1: x + y\nprint(f(1))", params: 2, defaults: 1},
	{name: "every parameter defaulted", src: "f = lambda x=2, y=3: x * y\nprint(f())", params: 2, defaults: 2},
	{name: "one parameter, defaulted", src: "f = lambda x=7: x\nprint(f())", params: 1, defaults: 1},
	{name: "a call as the default", src: "f = lambda x, y=two(): x * y\nprint(f(3))", params: 2, defaults: 1},
	{name: "a dict literal as the default", src: "f = lambda x, d={\"a\": 1}: x\nprint(f(1))", params: 2, defaults: 1},
	{name: "a text with a colon in it", src: "f = lambda x, s=\"a:b\": x\nprint(f(1))", params: 2, defaults: 1},
	{name: "a conditional as the default", src: "f = lambda x, y=1 if flag else 2: x + y\nprint(f(1))", params: 2, defaults: 1},
	{name: "a parenthesised default", src: "f = lambda x, y=(2): x\nprint(f(3))", params: 2, defaults: 1},
	{name: "no parameters at all", src: "f = lambda: 5\nprint(f())", params: 0, defaults: 0},
	{name: "none defaulted, the shape that already worked", src: "f = lambda x, y: x + y\nprint(f(1, 2))", params: 2, defaults: 0},
}

func TestTheLambdaGrammarCarriesTheDefaultIntoTheAST(t *testing.T) {
	t.Parallel()
	for _, tc := range lambdaDefaultASTCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			params := firstLambdaParams(t, tc.src)
			if len(params) != tc.params {
				t.Fatalf("%q parsed to %d parameters, want %d", tc.src, len(params), tc.params)
			}
			withDefault := 0
			for _, p := range params {
				if _, ok := p["default"]; ok {
					withDefault++
				}
				if name, ok := p["name"].(string); !ok || name == "" {
					t.Fatalf("%q produced a parameter with no name: %v", tc.src, p)
				}
			}
			if withDefault != tc.defaults {
				t.Fatalf("%q carries %d default(s), want %d — the parse IS the row: a default dropped at parse time is a call site with a missing argument\n%v",
					tc.src, withDefault, tc.defaults, params)
			}
		})
	}
}

// firstLambdaParams asks the AST through the same JSON the `--emit-ast` machine path prints, and returns the
// parameter list of the first node that has one. Read through the document rather than through a hand-written
// AST walk on purpose: the walk would have to know every node type to reach a lambda, and the row is about
// what the document an agent consumes says.
func firstLambdaParams(t *testing.T, src string) []map[string]any {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	doc, err := json.Marshal(prog)
	if err != nil {
		t.Fatalf("marshal %q: %v", src, err)
	}
	var root any
	if err := json.Unmarshal(doc, &root); err != nil {
		t.Fatalf("unmarshal %q: %v", src, err)
	}
	var found []map[string]any
	var walk func(node any)
	walk = func(node any) {
		if found != nil {
			return
		}
		switch n := node.(type) {
		case map[string]any:
			if raw, ok := n["params"]; ok {
				list, _ := raw.([]any) // a zero-parameter lambda marshals "params": null
				out := make([]map[string]any, 0, len(list))
				for _, p := range list {
					if pm, ok := p.(map[string]any); ok {
						out = append(out, pm)
					}
				}
				found = out
				return
			}
			for _, v := range n {
				walk(v)
			}
		case []any:
			for _, v := range n {
				walk(v)
			}
		}
	}
	walk(root)
	if found == nil {
		t.Fatalf("%q's AST carries no parameter list to ask about:\n%s", src, doc)
	}
	return found
}

// TestTheLambdaGrammarStillDeclinesWhatNoRowHasPaid is the negative half of widening a grammar: the parser
// must not quietly start accepting a spelling that has no answer behind it. `*args`/`**kwargs` and a
// trailing comma are parse errors today and are filed as Gap R.278 and Gap R.279 — this case fails if the
// grammar accepts them before the row is paid, which is the guard that keeps a grammar change and its
// semantics from being split apart by accident.
func TestTheLambdaGrammarStillDeclinesWhatNoRowHasPaid(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc     string
		src      string
		contains string
		gap      string
	}{
		{desc: "star args", src: "f = lambda *a: len(a)\nprint(f(1, 2, 3))", contains: `expected ":"`, gap: "roadmap Gap R.278"},
		{desc: "double star", src: "f = lambda **kw: len(kw)\nprint(f(a=1))", contains: `expected ":"`, gap: "roadmap Gap R.278"},
		{desc: "trailing comma", src: "f = lambda x, y=1, : x + y\nprint(f(1))", contains: "lambda param must be a name", gap: "roadmap Gap R.279"},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			_, err := Parse(tc.src)
			if err == nil {
				t.Fatalf("%q parses now, and nothing answers it: pay the row before the grammar accepts the spelling (%s)", tc.src, tc.gap)
			}
			if !strings.Contains(err.Error(), tc.contains) {
				t.Fatalf("%q refused with %q, not the pinned %q (%s)", tc.src, err.Error(), tc.contains, tc.gap)
			}
		})
	}
}

// TestNoLambdaDefaultProgramExitsTwo is the exit-code half. A parse error at exit 1 was the right code for a
// source the language could not express; the moment the grammar accepts a shape, rejecting it further down
// is a compiler bug (ADR 0166).
func TestNoLambdaDefaultProgramExitsTwo(t *testing.T) {
	t.Parallel()
	for _, tt := range lambdaDefaultAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			res, err := JIT(tt.src, 0)
			if err != nil {
				t.Fatalf("jit: %v", err)
			}
			if res.Code == 2 {
				t.Fatalf("%q left the toolchain as exit 2 (a compiler bug under ADR 0166):\n%s", tt.src, res.Stderr)
			}
		})
	}
}

// TestWhatTheLambdaDefaultRowLeavesStandingIsFiledNotFixed pins what this cycle measured and did NOT fix,
// today's answer beside the reference's, so none of it can quietly become normal. Each case is DELETED — not
// edited — when its fix lands.
//
//   - a `lambda` binds no capture list, so a name it reads from the function it is written in is
//     `undefined name` — while the identical shape with a nested `def` answers, in the same file, on the same
//     build (roadmap Gap R.276, which is what makes this a lambda row and not a closure row);
//   - **a default that is not a scalar is never bound**: `def d(y=None): return y` prints `0` at exit 0 where
//     the reference prints `None`; `xs=[]` prints `0` for `[]` and `w={}` prints `0` for `{}`; `y is None` is
//     `False`. Every scalar default answers, and a container default *supplied by the caller* answers — so the
//     road that is missing is the one that binds an omitted argument (roadmap Gap R.277, the priority-1 class;
//     it is why this row's Definition of done is `def`-parity and not CPython-parity for those shapes);
//   - no parameter list has `*args`/`**kwargs` (Gap R.278) or a trailing comma (Gap R.279);
//   - a `lambda`'s arity is refused by codegen where a `def`'s is diagnosed by the front end — same exit
//     code, different door and a different sentence from the reference's `TypeError` (Gap R.280).
func TestWhatTheLambdaDefaultRowLeavesStandingIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc    string
		src     string
		want    string // what the reference prints
		today   string // what this toolchain prints, when it prints
		refusal string // when today's answer is a refusal, the substring it names
		trap    string // when today's answer is a raise, the message it names
		gap     string
	}{
		{
			desc: "a lambda reads the enclosing function's local", gap: "Gap R.276",
			src:  "def outer():\n    b = 42\n    f = lambda: b\n    return f()\n\nprint(outer())",
			want: "42", refusal: `undefined name "b"`,
		},
		{
			desc: "a lambda reads the enclosing function's parameter", gap: "Gap R.276",
			src:  "def outer(v):\n    f = lambda: v * 2\n    return f()\n\nprint(outer(3))",
			want: "6", refusal: `undefined name "v"`,
		},
		{
			// the control: the nested `def` that does the same thing answers
			desc: "the nested def beside it answers", gap: "Gap R.276's control",
			src:  "def outer():\n    b = 42\n    def inner():\n        return b\n    return inner()\n\nprint(outer())",
			want: "42", today: "42",
		},
		{
			desc: "a None default is never bound", gap: "Gap R.277",
			src:  "def d(y=None):\n    return y\n\nprint(d())",
			want: "None", today: "0",
		},
		{
			desc: "a None default asked as a question", gap: "Gap R.277",
			src:  "def d(y=None):\n    return y is None\n\nprint(d())",
			want: "True", today: "False",
		},
		{
			desc: "a list default is never bound", gap: "Gap R.277",
			src:  "def d(xs=[]):\n    return xs\n\nprint(d())",
			want: "[]", today: "0",
		},
		{
			desc: "a dict default is never bound", gap: "Gap R.277",
			src:  "def d(w={}):\n    return w\n\nprint(d())",
			want: "{}", today: "0",
		},
		{
			desc: "the same default, supplied by the caller, is bound", gap: "Gap R.277's control",
			src:  "def d(xs=[]):\n    return len(xs)\n\nprint(d([1, 2, 3]))",
			want: "3", today: "3",
		},
		{
			desc: "a scalar default is bound", gap: "Gap R.277's control",
			src:  "def d(x=5):\n    return x\n\nprint(d())",
			want: "5", today: "5",
		},
		{
			// the container default is answered on its own (the answered table above) and defeats the
			// function-value pass for an UNRELATED higher-order call when the two share a program.
			desc: "a container default beside a higher-order call", gap: "Gap R.282",
			src:  "def two():\n    return 2\n\nlookup = lambda n, d={\"a\": 1}: n + d[\"a\"]\n\ndef apply(fn, v):\n    return fn(v, 2)\n\nprint(apply(lambda a, b: a * b, 4))\n\ndef d(a=2, b=3):\n    return a * b\n",
			want: "8", refusal: `"fn" is called but is not a function`,
		},
		{
			// the two controls: the higher-order call alone answers `8`, and the container-default lambda
			// alone answers `2` (in the answered table). It is the pair, in one program, that the pass
			// cannot take — and the repro is as small as the probe could shrink it, five statements, each
			// of them individually removable.
			desc: "the higher-order call alone answers", gap: "Gap R.282's control",
			src:  "def apply(fn, v):\n    return fn(v, 2)\n\nprint(apply(lambda a, b: a * b, 4))",
			want: "8", today: "8",
		},
		{
			desc: "star args on a def", gap: "Gap R.278",
			src:  "def d(*a):\n    return len(a)\n\nprint(d(1, 2, 3))",
			want: "3", refusal: "parse error",
		},
		{
			desc: "a trailing comma in a def's list", gap: "Gap R.279",
			src:  "def d(x, y=1,):\n    return x + y\n\nprint(d(1))",
			want: "2", refusal: "parse error",
		},
		{
			desc: "a def's missing argument reaches the same codegen door as the lambda's", gap: "Gap R.280 (the CLI door contrast is the integration file's)",
			src:  "def d(x, y=1):\n    return x + y\n\nprint(d())",
			want: "", refusal: `missing argument "x"`,
		},
		{
			desc: "a lambda's missing argument is a codegen refusal", gap: "Gap R.280",
			src:  "f = lambda x, y=1: x + y\nprint(f())",
			want: "", refusal: `missing argument "x"`,
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			if tc.refusal != "" {
				_, err := Compile(tc.src)
				if err == nil {
					out, trap := runForParity(t, tc.src)
					t.Fatalf("%q no longer refuses with %q (printed %q, trap %q): the filed row moved — close it and delete this case, do not edit it (the reference prints %q) [%s]",
						tc.src, tc.refusal, out, trap, tc.want, tc.gap)
				}
				if !strings.Contains(err.Error(), tc.refusal) {
					t.Fatalf("%q refused with %q, not the pinned %q — re-measure the filing [%s]", tc.src, err.Error(), tc.refusal, tc.gap)
				}
				return
			}
			out, trap := runForParity(t, tc.src)
			if tc.trap != "" {
				if !strings.Contains(trap, tc.trap) {
					t.Fatalf("%q raised %q, not the pinned %q — re-measure the filing [%s]", tc.src, trap, tc.trap, tc.gap)
				}
				return
			}
			if trap != "" {
				t.Fatalf("%q raised %s", tc.src, trap)
			}
			if got := strings.TrimRight(out, "\n"); got != tc.today {
				t.Fatalf("the pinned answer moved: %q printed %q, this file recorded %q (the reference prints %q — if the row was closed, delete this case) [%s]",
					tc.src, got, tc.today, tc.want, tc.gap)
			}
		})
	}
}

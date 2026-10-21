package lang

import (
	"regexp"
	"strings"
	"testing"
)

// call_key_param_test.go — the `key=` door writes a call the AST never wrote, so the scan that
// decides a parameter's kind has to be told about it. roadmap L11.7 · Gap R.264; ADR 0363.
//
// ADR 0340 gave the ordering builtins a key door: `sorted(xs, key=f)` calls `f` once per element,
// through the ordinary call road, so arity, closures and every raise inside the key are the ordinary
// call's business. ADR 0345 gave a `key=lambda …` hook a declaration of its own so the door had a
// name to call. Both were true, and the program still refused:
//
//	print(sorted(['bbb', 'a', 'cc'], key=lambda s: len(s)))   # gustyc: len of a non-string variable
//	def k(s): return len(s)                                    #   the same program with a named key
//	print(sorted(['bbb','a','cc'], key=k))                     # gustyc: len of a non-string variable
//	print(sorted(['Z','a'], key=lambda s: s.lower()))          # gustyc: string-method lower on a
//	                                                           #   receiver that is not a text
//	print(sorted(['ab','ba'], key=lambda s: s[0]))             # gustyc: index of a non-literal variable
//
// while the reference prints `['a', 'cc', 'bbb']`, `['a', 'cc', 'bbb']`, `['a', 'Z']`, `['ab', 'ba']`.
// The same body answered when the program called it itself — `def k(s): return len(s)` / `k("abc")`
// prints `3` — because that call is in the tree, and `strArgKinds` reads the tree to decide which
// parameters receive texts. The door's call is not in the tree: it is built at codegen time out of
// the element standing in front of it. So the parameter arrived unclaimed, and every door the body
// asked — `len`, a string method, a subscript, a concatenation — asked a question the claim answers.
//
// The fix is one reading, in the one place that already reads call sites: `keyDoorCallSites` reads a
// `sorted`/`min`/`max` call the way the door itself reads it (options by name, the iterable alone,
// the candidates written out) and hands the scan the expressions the key will be called with. The
// claims are then the SAME claims the ordinary call road makes — `strArgClaimOf` is the single
// predicate under both — so a parameter a key hands a text and a call site hands a number cancels
// out and refuses, exactly as it does anywhere else in the language (ADR 0348's rule: a position is
// claimed only when every site that fills it agrees).
//
// The refusals that stay are the ones that were honest before and are honest now: a container the
// program built at run time (Gap R.254), candidates of two kinds (L11.1's tagged value word), and a
// key that is not a name the program declared (Gap R.255).
//
// Every `want` below is CPython's own text, read off the reference; `integration/call_key_param_test.go`
// runs the same programs through `python3` and derives its expectation from the interpreter.

// keyedParamAnswered is the answer table: a key whose body asks the ELEMENT what it is.
var keyedParamAnswered = []struct {
	src  string
	want string
}{
	// the four programs the row measured, in the order the row states them
	{"print(sorted([\"bbb\", \"a\", \"cc\"], key=lambda s: len(s)))", "['a', 'cc', 'bbb']"},
	{"def k(s):\n    return len(s)\nprint(sorted([\"bbb\", \"a\", \"cc\"], key=k))", "['a', 'cc', 'bbb']"},
	{"print(sorted([\"Z\", \"a\", \"Mb\"], key=lambda s: s.lower()))", "['a', 'Mb', 'Z']"},
	{"print(sorted([\"ab\", \"ba\"], key=lambda s: s[0]))", "['ab', 'ba']"},
	// the body asks the same doors a plain call asks, and the parameter carries the element's kind
	{"print(sorted([\"b\", \"a\"], key=lambda s: s.upper()))", "['a', 'b']"},
	{"print(sorted([\"b\", \"a\"], key=lambda s: s + \"!\"))", "['a', 'b']"},
	{"def k(s):\n    return s.strip()\nprint(sorted([\" b\", \"a\"], key=k))", "['a', ' b']"},
	{"def k(s):\n    return s[0]\nprint(sorted([\"ba\", \"ab\"], key=k))", "['ab', 'ba']"},
	// descending, and the tie half: the order the program wrote survives equal keys
	{"print(sorted([\"bbb\", \"a\", \"cc\"], key=lambda s: 0-len(s)))", "['bbb', 'cc', 'a']"},
	{"print(sorted([\"bbb\", \"a\", \"cc\"], key=lambda s: len(s), reverse=True))", "['bbb', 'cc', 'a']"},
	{"print(sorted([\"aa\", \"bb\", \"c\", \"d\"], key=lambda s: len(s)))", "['c', 'd', 'aa', 'bb']"},
	// the ordering is asked of the TEXT, not of the intern table's arrival order (ADR 0248's rule at
	// this door): these three texts arrive z, a, Mb and leave a, Mb, z
	{"print(sorted([\"z\", \"a\", \"Mb\"], key=lambda s: s.lower()))", "['a', 'Mb', 'z']"},
	// a code point, not a byte: the same unit every other position question is asked in (ADR 0225)
	{"print(sorted([\"café\", \"a\", \"ab\"], key=lambda s: len(s)))", "['a', 'ab', 'café']"},
	// min and max take the key's parameter the same way
	{"print(min([\"bbb\", \"a\", \"cc\"], key=lambda s: len(s)))", "a"},
	{"print(max([\"bbb\", \"a\", \"cc\"], key=lambda s: len(s)))", "bbb"},
	{"print(min(\"bbb\", \"a\", key=lambda s: len(s)))", "a"},
	{"print(max(\"bbb\", \"a\", \"cc\", key=lambda s: len(s)))", "bbb"},
	{"def k(s):\n    return 0-len(s)\nprint(max([\"bbb\", \"a\", \"cc\"], key=k))", "a"},
	{"def k(s):\n    return 0-len(s)\nprint(min([\"bbb\", \"a\", \"cc\"], key=k))", "bbb"},
	{"best = max([\"bbb\", \"a\", \"cc\"], key=lambda s: len(s))\nprint(best, best.upper(), len(best))", "bbb BBB 3"},
	// the answer is a list like any other: bound, measured, indexed, looped over
	{"xs = sorted([\"bbb\", \"a\"], key=lambda s: len(s))\nprint(xs, len(xs), xs[0])", "['a', 'bbb'] 2 a"},
	{"for v in sorted([\"bbb\", \"a\", \"cc\"], key=lambda s: len(s)):\n    print(v)", "a\ncc\nbbb"},
	{"print(len(sorted([\"bbb\", \"a\", \"cc\"], key=lambda s: len(s))))", "3"},
	// a key that reads its parameter AND answers a number, beside a key that answers a text
	{"def L(s):\n    return len(s)\nprint(sorted([\"bbbb\", \"a\", \"cc\"], key=L))", "['a', 'cc', 'bbbb']"},
	{"def first(s):\n    return s[0]\nprint(sorted([\"zz\", \"am\"], key=first))", "['am', 'zz']"},
	// the negative controls: what answered before this row keeps answering, unchanged
	{"print(sorted([3, 1, 2], key=lambda x: 0-x))", "[3, 2, 1]"},
	{"print(sorted([\"bbb\", \"a\", \"cc\"], key=len))", "['a', 'cc', 'bbb']"},
	{"print(sorted([\"b\", \"a\"]))", "['a', 'b']"},
	{"print(sorted([3, 1, 2]))", "[1, 2, 3]"},
	{"def k(s):\n    return len(s)\nprint(k(\"abc\"), k(\"a\"))", "3 1"},
	{"def sorted(xs):\n    return [1]\nprint(sorted([3, 2]))", "[1]"}, // the program's own name is not the builtin
}

// keyedParamRefused names what the door still cannot do, and the row that owns the missing half.
var keyedParamRefused = []struct {
	src   string
	names string
	// owns is the row the refusal must point at. It is empty for a refusal the whole language already
	// says, which this row quotes rather than edits.
	owns string
}{
	{
		// a container the program built: the elements exist only where the runtime puts them
		src:   "ys = [\"bbb\", \"a\"]\nprint(sorted(ys, key=lambda s: len(s)))",
		names: "exist only at run time",
		owns:  "Gap R.254",
	},
	{
		// two candidates of two kinds: one parameter, two representations — the tag is the missing word
		src:   "print(max(\"bbb\", 1, key=lambda s: 1))",
		names: "strings are not supported as function arguments",
		owns:  "",
	},
	{
		// the same parameter, one door handing it a text and one call site handing it a number
		src:   "def k(s):\n    return len(s)\nprint(k(3))\nprint(sorted([\"bbb\", \"a\"], key=k))",
		names: "len of a non-string variable",
		owns:  "",
	},
	{
		// `round` is a key the door can now ask (roadmap Gap R.255): the one-argument call answers an
		// integer, so the verdict is the ordinary call road's — and the ordinary road's own sentence for a
		// TEXT operand is what the program hears. The reference RAISES `TypeError: type str doesn't
		// define __round__ method` there, which is the raise the plain `round("a")` already owes
		// (Gap R.363), not a sentence this door invents.
		src:   "print(sorted([\"bbb\", \"a\"], key=round))",
		names: "cannot round a string",
		owns:  "Gap R.363",
	},
	{
		// The same sentence from the ordinary road, without the key door in front of it: the reference
		// RAISES `TypeError: type str doesn't define __round__ method`, and this backend refuses.
		src:   "print(round(\"a\"))",
		names: "cannot round a string",
		owns:  "Gap R.363",
	},
	{
		// the door answers a heap handle, and an INDEX asks it of a NAME the census can read; the bound
		// form of the same program — `xs = sorted(...)` then `xs[0]` — answers `a`
		src:   "print(sorted([\"bbb\", \"a\"], key=lambda s: len(s))[0])",
		names: "index requires an inline list",
		owns:  "",
	},
}

// TestAKeyThatAsksItsElementAnswersWhatTheReferenceAnswers is the compiled leg: the module the
// compiler writes, and the JIT that runs it.
func TestAKeyThatAsksItsElementAnswersWhatTheReferenceAnswers(t *testing.T) {
	t.Parallel()
	for _, tt := range keyedParamAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); the reference prints %q", tt.src, got, tt.want)
			}
			if strings.TrimSpace(got) != tt.want {
				t.Fatalf("%q printed %q, want %q", tt.src, strings.TrimSpace(got), tt.want)
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

// TestAKeyThatAsksItsElementIsWhatTheRecordSaid is the record leg. The sources this row first asked
// were recorded from the reference (the record's own meta.added_after_0302 says so); a source the
// record does not hold skips this leg rather than passing it silently.
func TestAKeyThatAsksItsElementIsWhatTheRecordSaid(t *testing.T) {
	t.Parallel()
	asked := 0
	for _, tt := range keyedParamAnswered {
		// No t.Parallel here: the parent's "did any row hit the record" check runs when this function
		// returns, and a parallel subtest has not run by then — the guard would fail on a record that
		// was fully walked.
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			src := tt.src + "\n"
			if !HasGoldenAnswer(src) {
				t.Skipf("the record holds no answer for this source")
			}
			asked++
			want := strings.TrimSpace(goldenStdout(silentReporter{}, src))
			if want != tt.want {
				t.Fatalf("the record says %q, this table says %q — one of them is wrong", want, tt.want)
			}
			got, refused := compiledOutOrRefusal(t, src)
			if refused {
				t.Fatalf("%q refused (%s); the record prints %q", src, got, want)
			}
			if strings.TrimSpace(got) != want {
				t.Fatalf("%q printed %q, the record printed %q", src, strings.TrimSpace(got), want)
			}
		})
	}
	if asked == 0 {
		t.Fatal("the answer table matches no record row at all: the record leg is not being walked")
	}
}

// TestWhatTheKeyDoorCannotCallStillRefusesByName holds a refusal to ADR 0249: it names the shape and
// the row that owns it, and it never leaves the answer to a run-time question to the exit code.
func TestWhatTheKeyDoorCannotCallStillRefusesByName(t *testing.T) {
	t.Parallel()
	for _, tt := range keyedParamRefused {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			msg := compiledRefusalOf(tt.src)
			if msg == "" {
				t.Fatalf("compiled clean; want a refusal mentioning %q", tt.names)
			}
			if !strings.Contains(msg, tt.names) {
				t.Fatalf("refusal = %q, want it to mention %q", msg, tt.names)
			}
			// The door's own refusals point at the row that owns the missing half. A refusal that is a
			// sentence the whole language already says — "len of a non-string variable", "index requires
			// an inline list" — is the language's, not this door's, and this row does not edit it; those
			// rows name the owner in the table and ask only for the sentence.
			if tt.owns != "" && !strings.Contains(msg, tt.owns) && !strings.Contains(msg, "roadmap") {
				t.Fatalf("refusal %q does not point at a row (%s)", msg, tt.owns)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The scan's own account: which parameter the door claims, and what cancels it.
// ---------------------------------------------------------------------------

// TestTheKeyDoorsCallSitesAreReadByTheParameterScan is the unit test for the claim itself — the
// table the doors read, asked of the tree before any IR exists.
func TestTheKeyDoorsCallSitesAreReadByTheParameterScan(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want map[string][]int // function name -> the parameter indices claimed as texts
	}{
		{
			name: "sorted hands its literal elements to the key",
			src:  "def k(s):\n    return len(s)\nprint(sorted([\"a\", \"b\"], key=k))\n",
			want: map[string][]int{"k": {0}},
		},
		{
			name: "min and max hand their written-out candidates to the key",
			src:  "def k(s):\n    return len(s)\nprint(min(\"a\", \"b\", key=k), max(\"a\", \"b\", \"c\", key=k))\n",
			want: map[string][]int{"k": {0}},
		},
		{
			name: "a key that is a builtin is nobody's parameter",
			src:  "print(sorted([\"a\", \"b\"], key=len))\n",
			want: map[string][]int{},
		},
		{
			name: "a key over numbers claims nothing",
			src:  "def k(s):\n    return 0-s\nprint(sorted([3, 1, 2], key=k))\n",
			want: map[string][]int{},
		},
		{
			// one parameter, two claims: the call site says number, the door says text, and
			// the honest answer is that the compiler does not know
			name: "contradictory evidence cancels the claim",
			src:  "def k(s):\n    return len(s)\nprint(k(3))\nprint(sorted([\"a\", \"b\"], key=k))\n",
			want: map[string][]int{},
		},
		{
			name: "the program's own sorted is not the builtin",
			src:  "def k(s):\n    return len(s)\ndef sorted(xs, key=None):\n    return [1]\nprint(sorted([\"a\"], key=k))\n",
			want: map[string][]int{},
		},
		{
			// the identity key: the reference never calls it, and neither does this compiler
			name: "key=None is no call at all",
			src:  "def k(s):\n    return len(s)\nprint(sorted([\"a\", \"b\"], key=None))\nprint(k(1) + 1)\n",
			want: map[string][]int{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got := strArgKinds(prog)
			for fn, idxs := range tc.want {
				if len(idxs) == 0 {
					if len(got[fn]) != 0 {
						t.Fatalf("%q claimed %v, want no text parameter", fn, got[fn])
					}
					continue
				}
				for _, i := range idxs {
					if !got[fn][i] {
						t.Fatalf("%q did not claim parameter %d as a text: %v", fn, i, got[fn])
					}
				}
			}
		})
	}
}

// TestTheKeyDoorClaimsAHoistedHookLambdasParameter is the same claim one level in: the lambda the
// pass names is a declaration like any other, and it is that declaration whose parameter the door
// fills — which is why the hook's body can ask `len` at all (ADR 0345's hoist, ADR 0363's claim).
func TestTheKeyDoorClaimsAHoistedHookLambdasParameter(t *testing.T) {
	t.Parallel()
	src := "print(sorted([\"bbb\", \"a\", \"cc\"], key=lambda s: len(s)))\n"
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	prog = defunctionalizeCallables(prog)
	kinds := strArgKinds(prog)
	claimed := 0
	for fn, m := range kinds {
		if !strings.Contains(fn, callableLambdaInfix) {
			continue
		}
		if !m[0] {
			t.Fatalf("the hoisted hook %q has no claimed parameter: %v", fn, m)
		}
		claimed++
	}
	if claimed == 0 {
		t.Fatal("no hoisted hook lambda carried a claimed parameter — the pass or the scan regressed")
	}
}

// TestTheKeyBodyAsksTheDoorThatReadsTheText is the IR shape: the hook's body measures the text
// through the runtime door instead of treating the parameter as a number, and no keyed module hands
// a global to a word or a double to an i32 parameter (ADR 0166's two forbidden shapes).
func TestTheKeyBodyAsksTheDoorThatReadsTheText(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		must      []string
	}{
		{
			name: "len of the key's parameter counts code points",
			src:  "print(sorted([\"bbb\", \"a\", \"cc\"], key=lambda s: len(s)))\n",
			must: []string{"define i32 @gy_sorted" + callableLambdaInfix, "call i32 @rt_str_nchars(i32 %p0)", "call i32 @rt_elem_gt(i32 %"},
		},
		{
			name: "a named key measures its parameter the same way",
			src:  "def k(s):\n    return len(s)\nprint(sorted([\"bbb\", \"a\"], key=k))\n",
			must: []string{"define i32 @gy_k(", "call i32 @rt_str_nchars(i32 %p0)"},
		},
		{
			name: "a text answer orders by the text, not by the intern table's arrival order",
			src:  "print(sorted([\"z\", \"a\", \"Mb\"], key=lambda s: s.lower()))\n",
			must: []string{"call i32 @rt_elem_gt(i32 %", ", i32 1)", "define internal i32 @rt_elem_gt(", "@strcmp"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mod := compileSrc(t, tc.src)
			for _, want := range tc.must {
				if !strings.Contains(mod, want) {
					t.Fatalf("the module does not ask %q:\n%s", want, mod)
				}
			}
			for _, forbidden := range []string{"i32 @.", "ptrtoint", "inttoptr"} {
				if forbidden == "i32 @." {
					if regexp.MustCompile(`call i32 @\w*\(i32 @`).MatchString(mod) ||
						regexp.MustCompile(`ret i32 @`).MatchString(mod) {
						t.Fatalf("a global reached a value position:\n%s", mod)
					}
					continue
				}
				if strings.Contains(mod, forbidden) {
					t.Fatalf("the module contains %q:\n%s", forbidden, mod)
				}
			}
			if m := regexp.MustCompile(`fdiv double ,|fadd double ,`).FindString(mod); m != "" {
				t.Fatalf("the module divides by an operand it invented (%s):\n%s", m, mod)
			}
		})
	}
}

// TestTheKeyDoorAnswersTheSameQuestionTwice is the one-answer rule this row exists for: the loop that
// walks the keyed list and the print of the same keyed list must order the same way, because both go
// through the same door and the same claimed parameter (the shape Gap R.251 named for `reversed`).
func TestTheKeyDoorAnswersTheSameQuestionTwice(t *testing.T) {
	t.Parallel()
	const src = "print(sorted([\"bbb\", \"a\", \"cc\"], key=lambda s: len(s)))\n"
	const loop = "for v in sorted([\"bbb\", \"a\", \"cc\"], key=lambda s: len(s)):\n    print(v)\n"
	printed, refused := compiledOutOrRefusal(t, src)
	if refused {
		t.Fatalf("the print door refused: %s", printed)
	}
	walked, refused := compiledOutOrRefusal(t, loop)
	if refused {
		t.Fatalf("the loop door refused: %s", walked)
	}
	want := strings.Join([]string{"a", "cc", "bbb"}, "\n")
	if strings.TrimSpace(walked) != want {
		t.Fatalf("the loop walked %q, want the order the print printed (%q)", strings.TrimSpace(walked), strings.TrimSpace(printed))
	}
	if strings.TrimSpace(printed) != "['a', 'cc', 'bbb']" {
		t.Fatalf("the print door printed %q", printed)
	}
}

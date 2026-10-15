package lang

import (
	"strings"
	"testing"
)

// fstring_value_test.go — an f-string is a TEXT, not only a printing instruction.
// roadmap L11.1, Gap R.146's value half; ADR 0359.
//
// The print road has honoured an interpolation for as long as this backend has had one: `print(f"x{n}")`
// builds a printf format string and writes `x3`. The same source standing where a VALUE goes was refused
// outright — `value()` answered "f-string requires a constant expression (AOT backend)" — and with it every
// position that lowers an expression to a word:
//
//	s = f"x{n}"            # refused; print(s) with the name answered
//	f"a{n}" + "!"          # refused
//	f"v={n}".upper()       # refused
//	len(f"a{n}b")          # refused
//	def f(): return f"x{3}"  # refused
//	[f"a{n}"], {f"k{n}": 1}, d[f"v{n}"] = 1, for c in f"ab{n}"   # refused
//
// while CPython answers all of them. Two more shapes were not refused but wrong at the code that means the
// program ran: `print(f"v={None}")` wrote `v=0` and `xs = [1, 2]` / `print(f"{xs}")` wrote `0`, because the
// one format string the print road builds can only be handed an i32 or an i8* — the void has no digits, and
// a container's text is its slots, not its heap handle (roadmap ADR 0166's forbidden class).
//
// One door answers the field now: `renderPair`, the same question `str()` and `repr()` ask, so a field, the
// builtin and the print of the same value cannot disagree about `7` versus `7.0` versus a handle (ADR 0258's
// one renderer, ADR 0303's one printer). The parts are joined by `rt_str_cat`, the builder `s + t` already
// used, so the answer is an ordinary @str_tab index and every text road downstream — `len`, `==`, `in`,
// a subscript, a slice, a `for`, a container slot, a string parameter — takes it without knowing what built
// it. A field the AST already states is folded into the text around it, and that fold is ONE function
// (`fstringConstTextOf`) asked by three roads — `value()`, `stringVal` and the container literal's kind
// census through `stringConst` — because when those three had different answers, `[f"a1", 1]` built an object
// that claimed every element is interned text, printed the integer 1 through the string table and died with
// a segfault (exit 2, ADR 0166 again).
//
// Every `want` below is CPython's, read off the reference; the same programs run through `python3 -` by
// `integration/fstring_value_test.go`, which derives its expectations from the interpreter rather than from
// this file.

func fstringValueRows() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		// ---- the row's own shapes: a text where a value goes -------------------------------
		{"a binding", "n = 3\ns = f\"x{n}\"\nprint(s)\n", "x3\n"},
		{"concatenated with a text", "n = 3\nprint(f\"a{n}\" + \"!\")\n", "a3!\n"},
		{"concatenated with another interpolation", "n = 3\nprint(f\"a{n}\" + f\"b{n}\")\n", "a3b3\n"},
		{"a method on the text", "n = 3\nprint(f\"v={n}\".upper())\n", "V=3\n"},
		{"the length of the text", "n = 3\nprint(len(f\"a{n}b\"))\n", "3\n"},
		{"compared with a text", "n = 3\nprint(f\"a{n}\" == \"a3\")\n", "True\n"},
		{"a substring test over it", "n = 3\nprint(\"3\" in f\"a{n}\")\n", "True\n"},
		{"indexed", "n = 3\nprint(f\"a{n}b\"[1])\n", "3\n"},
		{"sliced", "n = 3\nprint(f\"a{n}b\"[1:])\n", "3b\n"},
		{"returned by a function", "def f():\n    return f\"x{3}\"\nprint(f())\n", "x3\n"},
		{"returned by a method", "class C:\n    def m(self):\n        return f\"m{2}\"\nprint(C().m())\n", "m2\n"},
		{"handed to a text parameter", "n = 3\ndef g(t):\n    print(t)\ng(f\"x{n}\")\n", "x3\n"},
		{"a list element", "n = 3\nprint([f\"a{n}\", 1])\n", "['a3', 1]\n"},
		{"a dict entry", "n = 3\nprint({\"k\": f\"a{n}\"})\n", "{'k': 'a3'}\n"},
		{"a dict key", "n = 3\nd = {f\"k{n}\": 1}\nprint(d)\n", "{'k3': 1}\n"},
		{"a dict key written by assignment", "n = 3\nd = {}\nd[f\"v{n}\"] = 1\nprint(d)\n", "{'v3': 1}\n"},
		{"a set member", "n = 3\nprint({f\"a{n}\"})\n", "{'a3'}\n"},
		{"iterated as a text", "n = 3\nfor c in f\"ab{n}\":\n    print(c)\n", "a\nb\n3\n"},
		{"rendered by str", "n = 3\nprint(str(f\"a{n}\"))\n", "a3\n"},
		{"rendered by repr", "n = 3\nprint(repr(f\"a{n}\"))\n", "'a3'\n"},
		{"bound then concatenated with itself", "n = 3\ns = f\"{n}\"\nprint(s + s)\n", "33\n"},

		// ---- the fields the print road never had to ask an object about ---------------------
		{"a void field in a value", "print(f\"v={None}\")\n", "v=None\n"},
		{"a container field in a value", "xs = [1, 2]\nprint(f\"{xs}\")\n", "[1, 2]\n"},
		{"a container literal field", "n = 3\nprint(f\"{[n]}\")\n", "[3]\n"},
		{"a container-answer field", "def build():\n    return [1, 2]\nprint(f\"{build()}\")\n", "[1, 2]\n"},
		{"a container field, bound", "xs = [1, 2]\nt = f\"<{xs}>\"\nprint(t)\n", "<[1, 2]>\n"},
		{"a verdict field in a value", "b = True\nprint(f\"{b}\")\n", "True\n"},
		{"a float field in a value", "n = 2.5\nprint(f\"v={n}\")\n", "v=2.5\n"},
		{"a builtin over a field", "xs = [1, 2]\nprint(f\"{len(xs)}\")\n", "2\n"},
		{"arithmetic in the field", "n = 3\nprint(f\"{n + 1}\")\n", "4\n"},
		{"a comparison in the field", "n = 3\nprint(f\"{n == 3}\")\n", "True\n"},
		{"a ternary field", "n = 3\nprint(f\"{'x' if n else 'y'}{n}\")\n", "x3\n"},
		{"a call in the field", "def f():\n    print(\"side\")\n    return 7\nprint(f\"v={f()}\")\n", "side\nv=7\n"},
		{"a nested interpolation", "n = 3\nprint(f\"{f'q{n}'}\")\n", "q3\n"},
		{"a repr conversion in a value", "s = \"ab\"\nprint(f\"{s!r}\")\n", "'ab'\n"},
		{"a text literal with repr stays the printer's", "print(f\"{'a'!r}\")\n", "'a'\n"},

		// ---- the loop variable: the loop's own claim, taken back when the loop ends ---------
		{"a counter field", "for i in range(2):\n    t = f\"x{i}\"\n    print(t)\n", "x0\nx1\n"},
		{"a container element field", "xs = [1, 2]\nfor v in xs:\n    t = f\"v{v}\"\n    print(t)\n", "v1\nv2\n"},
		{"a literal-loop element field", "for v in [1, 2]:\n    t = f\"[{v}]\"\n    print(t)\n", "[1]\n[2]\n"},

		// ---- the text built by the value road behaves as a text everywhere ------------------
		{"its length", "n = 3\nprint(len(f\"x{n}{n}\"))\n", "3\n"},
		{"ordered against a text", "n = 3\nprint(f\"a{n}\" < \"b\")\n", "True\n"},
		{"folded by min", "n = 3\nprint(min(f\"a{n}\", \"b\"))\n", "a3\n"},
		{"in a membership test over a literal list", "n = 3\nprint(f\"x{n}\" in [f\"x{n}\"])\n", "True\n"},
		{"in a dict lookup", "n = 3\nprint(f\"{n}\" in {\"3\": 1})\n", "True\n"},
		{"compared to another interpolation", "n = 3\nprint(f\"a{n}\" == f\"a{n}\")\n", "True\n"},
		{"indexed then measured", "n = 3\nt = f\"ab{n}\"\nprint(t[2], len(t))\n", "3 3\n"},
		{"built then appended", "n = 3\nxs = [1]\nxs.append(f\"a{n}\")\nprint(xs)\n", "[1, 'a3']\n"},
		{"appended to a runtime list", "n = 3\nxs = []\nxs.append(f\"a{n}\")\nprint(xs)\n", "['a3']\n"},
		{"a slot read back out of a list", "n = 3\nxs = [f\"a{n}\"]\nprint(xs[0], xs[0].upper())\n", "a3 A3\n"},
		{"a dict entry read back by key", "n = 3\nd = {f\"k{n}\": f\"v{n}\"}\nprint(d[\"k3\"])\n", "v3\n"},
		{"a strip method over a field", "s = \"  a  \"\nprint(f\"[{s.strip()}]\")\n", "[a]\n"},

		// ---- constants: the fold, and the roads the fold lets in ----------------------------
		{"a whole-constant interpolation", "print(f\"hello\")\n", "hello\n"},
		{"a constant field inside text", "print(f\"a{1}b\")\n", "a1b\n"},
		{"a constant interpolation bound to a name", "s = f\"a{1}b\"\nprint(s, len(s))\n", "a1b 3\n"},
		{"a constant interpolation as a list element", "print([f\"a1\", 1])\n", "['a1', 1]\n"},
		{"a percent sign in the literal", "n = 3\nprint(f\"100%{n}\")\n", "100%3\n"},
		{"braces written twice", "print(f\"a{{b}}c\")\n", "a{b}c\n"},
		{"a non-ASCII literal", "n = 3\nprint(f\"\\u2713{n}\")\n", "✓3\n"},
		// `end="!"` replaces the line ending, so the reference's bytes are `a3b!` with NO trailing newline;
		// `sep="-"` cannot show between one argument's parts, which is the thing this row pins.
		{"the print road's separator still stands", "n = 3\nprint(f\"a{n}b\", sep=\"-\", end=\"!\")\n", "a3b!"},
	}
}

// TestAnFStringUsedAsAValueAnswersItsText is the answer table: the record leg (the retired engine's answer
// for the same source, which cannot be deleted by deleting a record) and the compiled leg, on every row.
func TestAnFStringUsedAsAValueAnswersItsText(t *testing.T) {
	t.Parallel()
	for _, tc := range fstringValueRows() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			RecordedStdoutIs(t, tc.src, tc.want)
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the reference prints: %v\n%s", err, tc.src)
			}
			if res.Output != tc.want {
				t.Fatalf("compiled %q, want the reference's %q\nsource: %s", res.Output, tc.want, tc.src)
			}
		})
	}
}

// TestTheConstantInterpolationFoldsToItsText asks the fold directly, so a widening or a narrowing of it
// fails a decision row rather than silently changing what every interpolation in the language does. The
// fold is ONE function asked by three roads; a fourth opinion is the defect the row started from.
func TestTheConstantInterpolationFoldsToItsText(t *testing.T) {
	t.Parallel()
	lit := func(s string) Expr { return &StrLit{Value: s} }
	for _, tc := range []struct {
		name string
		fs   *FString
		want string
		ok   bool
		why  string
	}{
		{"a plain literal part", &FString{Parts: []FStringPart{{Lit: "ab"}}}, "ab", true, "nothing to interpolate"},
		{"an integer field", &FString{Parts: []FStringPart{{Lit: "n="}, {Expr: &IntLit{Value: 7}}}}, "n=7", true,
			"the AST states the digits"},
		{"a float field", &FString{Parts: []FStringPart{{Expr: &FloatLit{Value: 2.5}}}}, "2.5", true,
			"Python's float repr, the printer's own"},
		{"a text field", &FString{Parts: []FStringPart{{Expr: lit("ab")}}}, "ab", true, "the characters it holds"},
		{"a void field", &FString{Parts: []FStringPart{{Expr: &NoneLit{}}}}, "None", true, "the void's one name"},
		{"a verdict field", &FString{Parts: []FStringPart{{Expr: &Name{Value: "True"}}}}, "True", true, "the word the AST gives"},
		{"a text field asked for repr", &FString{Parts: []FStringPart{{Expr: lit("ab"), Conv: ConvRepr}}}, "", false,
			"quoting is the printer's act — the fold splicing a quoted literal is Gap R.192"},
		{"an integer field asked for repr", &FString{Parts: []FStringPart{{Expr: &IntLit{Value: 7}, Conv: ConvRepr}}}, "7", true,
			"the digits are the same under both halves"},
		{"a field with a format spec", &FString{Parts: []FStringPart{{Expr: &IntLit{Value: 7}, Spec: ">3"}}}, "", false,
			"the spec engine owns this field; folding it is how `f\"{3.5:.2f}\"` answered 3.5 (Gap R.186)"},
		{"a field that is a name", &FString{Parts: []FStringPart{{Expr: &Name{Value: "n"}}}}, "", false,
			"the value is the program's fact, not the AST's"},
		{"a field that is a call", &FString{Parts: []FStringPart{{Expr: &Call{Fn: &Name{Value: "f"}}}}}, "", false, "same"},
		{"a literal part after a foldable field", &FString{Parts: []FStringPart{{Expr: &IntLit{Value: 1}}, {Lit: "b"}, {Expr: &IntLit{Value: 2}}}}, "1b2", true,
			"every part states itself"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := fstringConstTextOf(tc.fs)
			if ok != tc.ok {
				t.Fatalf("fold answered ok=%v, want %v (%s): %q", ok, tc.ok, tc.why, got)
			}
			if ok && got != tc.want {
				t.Fatalf("folded to %q, want %q (%s)", got, tc.want, tc.why)
			}
		})
	}
}

// TestTheFoldedInterpolationBuildsNoTextAtRunTime is the negative control a fold that emitted the runtime
// road anyway would fail: `f"a{1}b"` is the text "a1b" and the module says so with one intern, no concatenation
// and no renderer call. Beside it, the shape that made this row a segfault: the container literal's kind
// census and the builder must ask the SAME fold, or `[f"a1", 1]` marks an object "every element is text"
// while one slot holds the integer 1 — exit 2, ADR 0166's forbidden code.
func TestTheFoldedInterpolationBuildsNoTextAtRunTime(t *testing.T) {
	t.Parallel()
	ir := emittedModuleIR(t, "print(f\"a{1}b\")")
	body := mainBodyOf(ir)
	for _, forbidden := range []string{"@rt_str_cat", "@rt_str_of_value", "@rt_str_of_int", "@rt_str_of_container"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("a constant interpolation reached the run-time renderer (%s):\n%s", forbidden, body)
		}
	}
	if strings.Contains(body, "@str_tab") {
		t.Fatalf("a constant interpolation was interned at run time rather than folded:\n%s", body)
	}
	// The fold's witness: the bytes the program wrote are the module's OWN constant, handed to the one
	// printf the print road builds. `f"a{1}b"` is `"a1b"` — no buffer, no table entry, no renderer.
	if !strings.Contains(ir, `c"a1b\00"`) || !strings.Contains(body, "@printf") {
		t.Fatalf("the constant interpolation did not fold to its own text:\n%s", body)
	}
	// The census and the builder agree: a mixed literal takes the tagged road and says its slots describe
	// themselves (bit 8), which is what stops the printer reading the integer through the string table.
	mixed := mainBodyOf(emittedModuleIR(t, "print([f\"a1\", 1])"))
	if !strings.Contains(mixed, "@rt_mark_estr(i32 %t1, i32 8)") {
		t.Fatalf("the literal with a folded text and an integer did not promote itself to per-slot tags:\n%s", mixed)
	}
}

// TestTheValueRoadAsksTheOneRenderer is the IR half of ADR 0258/0303's rule, applied to a text built at run
// time: the field's bytes come from the module's single tag-reading printer, and the parts are joined by the
// builder `s + t` uses. A second renderer written for f-strings is the failure this row guards.
func TestTheValueRoadAsksTheOneRenderer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		src    string
		want   []string // calls the program's own code must make
		deny   []string // calls it must never make (a second door is the failure this row guards)
		global []string // module constants the fold produced
	}{
		{
			"a number field", "n = 3\nt = f\"x{n}\"\nprint(t)\n",
			[]string{"call i32 @rt_str_of_int(", "call i32 @rt_str_cat("}, nil, nil,
		},
		{
			"a void field", "t = f\"v={None}\"\nprint(t)\n",
			// The whole interpolation folds — the void's NAME is a fact of the AST, not a value the run
			// time has to render. So the module carries the program's own bytes and calls no renderer for
			// the field: the fold is what stopped `v=0` answering a program that never ran an instruction.
			nil, []string{"@rt_str_of_value(", "@rt_str_cat(", "@rt_str_of_container(", "@rt_str_of_int("},
			[]string{`c"v=None\00"`},
		},
		{
			"a container field", "xs = [1, 2]\nt = f\"{xs}\"\nprint(t)\n",
			[]string{"call i32 @rt_str_of_container("}, nil, nil,
		},
		{
			"a slot read in the field", "xs = []\nxs.append(7)\nn = xs[0]\nt = f\"v{n}\"\nprint(t)\n",
			// One printer, two sinks: a field in a VALUE position asks the str half of the door
			// (`rt_str_of_value`), and must not route through the print sink — which is the second door
			// this row exists to keep shut (ADR 0258, ADR 0303).
			[]string{"call i32 @rt_str_of_value("}, []string{"@rt_print_mixed_value("}, nil,
		},
		{
			"a counter field", "for i in range(2):\n    t = f\"x{i}\"\n    print(t)\n",
			[]string{"call i32 @rt_str_of_int("}, nil, nil,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ir := emittedModuleIR(t, tc.src)
			body := mainBodyOf(ir)
			for _, want := range tc.want {
				if !strings.Contains(body, want) {
					t.Fatalf("the interpolation never asked %s:\n%s", want, body)
				}
			}
			for _, deny := range tc.deny {
				if strings.Contains(body, deny) {
					t.Fatalf("the interpolation asked %s, which is a second door where the module has one:\n%s", deny, body)
				}
			}
			for _, want := range tc.global {
				if !strings.Contains(ir, want) {
					t.Fatalf("the fold never produced %s:\n%s", want, ir)
				}
			}
			// The bytes reach printf only through the printer's pointer. A `%d` beside an i32 operand here
			// is the handle of a box or of an interned text posing as the value (Gap R.38's class).
			for _, line := range strings.Split(body, "\n") {
				if !strings.Contains(line, "@printf(") {
					continue
				}
				if strings.Contains(line, ", i32 %") {
					t.Fatalf("a built text reached printf as a number:\n%s", strings.TrimSpace(line))
				}
			}
		})
	}
}

// TestTheLoopDigitsClaimEndsWithTheLoop is Gap R.252's rule applied to the new record: the loop variable of a
// numeric loop is a number BECAUSE the loop bound it, and a name the program also assigns to is not the
// loop's to vouch for. The row fails the day the claim outlives the loop or survives an assignment.
func TestTheLoopDigitsClaimEndsWithTheLoop(t *testing.T) {
	t.Parallel()
	// Inside the loop the field answers; the same name after the loop has no claim left, and the value road
	// refuses rather than printing the digits of whatever the slot holds next.
	inside := "for i in range(2):\n    t = f\"x{i}\"\n    print(t)\n"
	if out := strings.TrimRight(compiledOut(t, inside), "\n"); out != "x0\nx1" {
		t.Fatalf("the loop variable's field printed %q, want %q", out, "x0\nx1")
	}
	after := "for i in range(2):\n    print(i)\nt = f\"x{i}\"\nprint(t)\n"
	if !CompiledRefusal(t, after, "roadmap L11.1") {
		if _, err := Compile(after); err == nil {
			t.Fatalf("the loop's numeric claim survived the loop: the value road answered %q", after)
		}
	}
	// A body that assigns to the loop variable takes the claim away — the value is no longer the loop's alone.
	rebound := "for i in range(2):\n    i = i * 100\n    t = f\"x{i}\"\n    print(t)\n"
	if _, err := Compile(rebound); err == nil {
		t.Fatalf("the field answered digits for a name the body rebinds: %q", rebound)
	} else if !strings.Contains(err.Error(), "roadmap L11.1") {
		t.Fatalf("the refusal does not name the row that owns it:\n%v", err)
	}
}

// TestAnFStringAsAValueRefusesWhatHasNoRenderer keeps the refusals the feature does NOT make: each names the
// field or the method it cannot lower, never blames a construct the program does not contain (Gap R.38), and
// never spends the code a compiler bug owns (ADR 0166).
func TestAnFStringAsAValueRefusesWhatHasNoRenderer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"a spec over a name", "n = 3\nt = f\"x{n:>3}\"\nprint(t)\n", "format spec"},
		{"a spec over a float", "n = 3.5\nt = f\"{n:.2f}\"\nprint(t)\n", "format spec"},
		{"a method with no run-time road", "n = 3\nprint(f\"a{n}b\".replace(\"b\", \"c\"))\n", "string-method replace"},
		{"a split with no run-time road", "n = 3\nprint(f\"a{n}b\".split(\"b\"))\n", "string-method split"},
		{"a repeat of a text", "n = 3\nprint(f\"x{n}\" * 2)\n", "not supported"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !CompiledRefusal(t, tc.src, "roadmap") {
				res, err := Compile(tc.src)
				if err != nil {
					if !strings.Contains(err.Error(), tc.want) {
						t.Fatalf("the refusal does not name %q:\n%v", tc.want, err)
					}
					if strings.Contains(err.Error(), " *%!") || strings.Contains(err.Error(), "%!s(") {
						t.Fatalf("the refusal prints Go rather than the program:\n%v", err)
					}
					return
				}
				t.Fatalf("a program this backend declines to build compiled:\n%s", res.IR)
			}
		})
	}
}

// TestAnInterpolationFieldNamesItselfWhenItRefuses is ADR 0249's rule for a refusal sentence, applied to the
// new door: the field is quoted as the program wrote it, and the module's own temp names and Go type names
// never appear where the source text belongs (Gap R.232 measured the same disease at the membership door).
func TestAnInterpolationFieldNamesItselfWhenItRefuses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"a name field", "for i in range(1):\n    print(i)\nt = f\"x{i}\"\nprint(t)\n", "i"},
		{"a call field of an unknown kind", "def pick(k):\n    if k:\n        return \"a\"\n    return 0\nt = f\"v{pick(1)}\"\nprint(t)\n", "v"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Compile(tc.src)
			if err == nil {
				t.Skipf("the backend answers this program now — move the row into the answer table")
			}
			msg := err.Error()
			for _, forbidden := range []string{"%!s(", "*lang.", "__cans", "%!d("} {
				if strings.Contains(msg, forbidden) {
					t.Fatalf("the refusal leaks the compiler's own words (%q):\n%v", forbidden, err)
				}
			}
		})
	}
}

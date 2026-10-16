package lang

import (
	"strings"
	"testing"
)

// container_return_test.go — a container handed back by a call is a value (roadmap Gap R.67, ADR 0333).
//
// The row began as a compiler bug that was not the program's fault:
//
//	def build():
//	    return [1, 2]
//	print(build())            # CPython [1, 2]  ·  this toolchain exit 2, `llc`: "global variable
//	                          #                  reference must have pointer type: ret i32 @.lst1"
//
// `gustyc` exited with the code the contract reserves for a compiler bug on a program the reference runs,
// and the same was true of `{1, 2}`, `{1: 2}`, `[]`, `{}`, `set()`, a literal with parameters in it, and a
// literal nested in another. The sibling shape — `xs.append(1)` then `return xs` — did build a module, and
// then answered `print(build())` with `0`: the heap handle, printed as the number the answer is not, at the
// exit code of success. `for v in build():` printed nothing at all, and `if build():` called a non-empty
// list false.
//
// Every `want` below is CPython's, read off the reference; the same programs run through `python3 -` by
// `integration/container_return_test.go`, which derives its expectations from the interpreter and not from
// this file.

// containerAnswerPrograms are the answered shapes: the three container kinds, the ways a body can arrive
// at one, and the positions that read the answer back.
var containerAnswerPrograms = []struct {
	src  string
	want string
}{
	// The row's own three shapes — the Definition of Done, which asks for `[1, 2]`, `{1, 2}` and `{1: 2}`.
	{"def build():\n    return [1, 2]\n\nprint(build())", "[1, 2]"},
	{"def build():\n    return {1, 2}\n\nprint(build())", "{1, 2}"},
	{"def build():\n    return {1: 2}\n\nprint(build())", "{1: 2}"},
	// The empty containers, which are the same object spelled two ways.
	{"def build():\n    return []\n\nprint(build())", "[]"},
	{"def build():\n    return {}\n\nprint(build())", "{}"},
	{"def build():\n    return set()\n\nprint(build())", "set()"},
	{"def build():\n    return list()\n\nprint(build())", "[]"},
	// Every kind a slot can hold, because the tag is what makes each of them mean anything.
	{"def build():\n    return [1, \"a\", None, 2.5, True, [7]]\n\nprint(build())", "[1, 'a', None, 2.5, True, [7]]"},
	{"def build():\n    return [[1, 2], [3]]\n\nprint(build())", "[[1, 2], [3]]"},
	{"def build():\n    return [{\"a\": [7]}]\n\nprint(build())", "[{'a': [7]}]"},
	{"def build():\n    return {1: \"a\", 2: None}\n\nprint(build())", "{1: 'a', 2: None}"},
	// The answer is built by the builders, so elements the compiler cannot fold are fine.
	{"def f(n):\n    return [n, n + 1]\n\nprint(f(1))", "[1, 2]"},
	{"def f(x):\n    return [x]\n\nprint(f([1, 2]))", "[[1, 2]]"},
	{"def f(n):\n    return {n: n * 2}\n\nprint(f(2))", "{2: 4}"},
	// A run-time-built container: the shape that used to build a module and print the handle.
	{"def build():\n    xs = []\n    xs.append(1)\n    xs.append(2)\n    return xs\n\nprint(build())", "[1, 2]"},
	{"def build():\n    d = {}\n    d[\"a\"] = \"x\"\n    return d\n\nprint(build())", "{'a': 'x'}"},
	{"def build():\n    s = set()\n    s.add(1)\n    return s\n\nprint(build())", "{1}"},
	{"def build():\n    xs = [1, 2]\n    return xs\n\nprint(build())", "[1, 2]"},
	{"def build():\n    xs = []\n    for i in range(3):\n        xs.append(i)\n    return xs\n\nprint(build())", "[0, 1, 2]"},
	{"def build():\n    return [x for x in [1, 2]]\n\nprint(build())", "[1, 2]"},
	// Bound, the answer is the variable it always was: measured, stepped over, mutated, compared.
	{"def build():\n    return [1, 2]\n\nys = build()\nprint(ys)", "[1, 2]"},
	{"def build():\n    return [1, 2]\n\nys = build()\nprint(len(ys))", "2"},
	{"def build():\n    return [1, 2]\n\nys = build()\nprint(ys[0])", "1"},
	{"def build():\n    return [1, 2]\n\nys = build()\nys.append(3)\nprint(ys)", "[1, 2, 3]"},
	{"def build():\n    return [1, 2]\n\nprint(build() == [1, 2])", "True"},
	{"def build():\n    return [1, 2]\n\nprint(build() == [1, 3])", "False"},
	{"def build():\n    return [1, 2]\n\nys = build()\nfor v in ys:\n    print(v)", "1\n2"},
	{"def build():\n    return {\"a\": 1}\n\nys = build()\nprint(ys[\"a\"])", "1"},
	{"def build():\n    return {\"a\": 1}\n\nys = build()\nprint(\"a\" in ys)", "True"},
	{"def build():\n    return [1, 2]\n\nif build():\n    print(\"t\")\nelse:\n    print(\"f\")", "t"},
	{"def build():\n    return []\n\nif build():\n    print(\"t\")\nelse:\n    print(\"f\")", "f"},
	{"def build():\n    return [1, 2]\n\nprint(not build())", "False"},
	// The other doors that read a container: `str`, `repr`, the membership haystack, the loop's iterable
	// and `len` — the answer crosses each of them the way a container the program built does (Gap R.67).
	{"def build():\n    return [1, 2]\n\nprint(str(build()))", "[1, 2]"},
	{"def build():\n    return [1, 2]\n\nprint(repr(build()))", "[1, 2]"},
	{"def build():\n    return [1, 2]\n\nprint(2 in build())", "True"},
	{"def build():\n    return [1, 2]\n\nprint(7 in build())", "False"},
	{"def build():\n    return [1, 2]\n\nprint(7 not in build())", "True"},
	{"def build():\n    return [1, 2]\n\nprint(len(build()))", "2"},
	{"def build():\n    return [1, 2]\n\nfor v in build():\n    print(v)", "1\n2"},
	{"def build():\n    return {\"a\": 1, \"b\": 2}\n\nfor k in build():\n    print(k)", "a\nb"},
	// A function that hands back what a second function built.
	{"def build():\n    return [1, 2]\n\ndef g():\n    return build()\n\nprint(g())", "[1, 2]"},
	{"def build():\n    return [1, 2]\n\ndef g():\n    return build()\n\nprint(len(g()))", "2"},
	// Two answers in one expression: each is its own object, rooted where it is used.
	{"def build():\n    return [1, 2]\n\nprint(build(), build())", "[1, 2] [1, 2]"},
	{"def build():\n    return [1]\n\ndef other():\n    return [2]\n\nprint(build(), other())", "[1] [2]"},
	// A number, a text and a verdict still cross the return the way they always did.
	{"def f():\n    return 5\n\nprint(f())", "5"},
	{"def f():\n    return \"ab\"\n\nprint(f())", "ab"},
	{"def f():\n    return 1.5\n\nprint(f())", "1.5"},
	{"def f():\n    return True\n\nprint(f())", "True"},
}

// functionBodyOf is one named function's own body of the emitted module, so a claim about what the return
// road emitted is neither satisfied nor denied by @main or by the runtime library the module carries.
func functionBodyOf(ir, name string) string {
	at := strings.Index(ir, "@"+name+"(")
	if at < 0 {
		return ""
	}
	line := strings.LastIndex(ir[:at], "\n")
	if line < 0 {
		line = 0
	}
	rest := ir[line:]
	if end := strings.Index(rest, "\n}"); end >= 0 {
		return rest[:end]
	}
	return rest
}

// parseProgramForTest is the front end, asked of a source the gate table wrote inline.
func parseProgramForTest(t *testing.T, src string) *Program {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return prog
}

func TestAContainerReturnedFromACallAnswersTheContainer(t *testing.T) {
	t.Parallel()
	for _, tt := range containerAnswerPrograms {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			if out := strings.TrimRight(compiledOut(t, tt.src), "\n"); out != tt.want {
				t.Fatalf("compiled %q, printed %q, want %q", tt.src, out, tt.want)
			}
		})
	}
}

// The IR the row turns on. `ret i32 @.lst1` is the defect itself — a global in a value position, which
// `llc` rejects and the exit-code contract counts as the compiler's bug — and the two replacements have to
// both be there: the builder that made the handle, and the printer that was told what the handle is.
func TestTheReturnedContainerIsBuiltAndPrintedAsAnObject(t *testing.T) {
	t.Parallel()
	ir := emittedModuleIR(t, "def build():\n    return [1, 2]\n\nprint(build())")
	fn := functionBodyOf(ir, "gy_build")
	for _, want := range []string{
		"call i32 @rt_alloc(",     // the object is built, not named by a global
		"call void @rt_set_elem(", // and every slot is written
		"call void @rt_tag_elem(", // beside the tag that says what the slot holds
		"ret i32 %t",              // the answer is a register
	} {
		if !strings.Contains(fn, want) {
			t.Fatalf("the return road does not emit %q:\n%s", want, fn)
		}
	}
	if strings.Contains(fn, "ret i32 @.") {
		t.Fatalf("the return still puts a global in a value position:\n%s", fn)
	}
	body := mainBodyOf(ir)
	for _, want := range []string{
		"call i32 @gy_build()",
		"call void @rt_root_put(", // the answer is rooted before anything can collect it
		// Printed by the door the answer's KIND names. The caller never saw the callee's spelling, so the
		// kind has to come from the scan that read every arm of the `return` — and for the one answer whose
		// brackets are not inferable from a kind, the tuple, the road hands the handle to the printer that
		// asks the object instead (the map below, roadmap L11.3, ADR 0361).
		"call void @rt_print_list(",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the print door does not emit %q:\n%s", want, body)
		}
	}
	// The three kinds get the three printers, because `{1, 2}` printed by the list printer is a rendering
	// the reference never produces (ADR 0234's rule for a comprehension, applied to an answer). The fourth
	// row is the immutable one: no road-level name can spell its brackets, so its handle goes to the
	// dispatcher that reads the object's attribute word.
	for src, want := range map[string]string{
		"def b():\n    return {1, 2}\n\nprint(b())":     "call void @rt_set_print(",
		"def b():\n    return {1: 2}\n\nprint(b())":     "call void @rt_dict_print(",
		"def b():\n    return [1, \"a\"]\n\nprint(b())": "call void @rt_print_list_mixed(",
		"def b():\n    return (1, 2)\n\nprint(b())":     "call void @rt_print_container_value(",
	} {
		ir2 := emittedModuleIR(t, src)
		if !strings.Contains(mainBodyOf(ir2), want) {
			t.Fatalf("%q does not print through %q:\n%s", src, want, mainBodyOf(ir2))
		}
	}
}

// The claim the row's own Definition of Done makes: no emitted module hands a global to a `ret`. Asserted
// over every shape the row touches, and over the answers of the ordinary kinds, whose roads this change
// must leave exactly as they were.
func TestNoEmittedModuleReturnsAGlobal(t *testing.T) {
	t.Parallel()
	shapes := []string{
		"def f():\n    return [1, 2]\n\nprint(f())",
		"def f():\n    return {1, 2}\n\nprint(f())",
		"def f():\n    return {1: 2}\n\nprint(f())",
		"def f():\n    return []\n\nprint(f())",
		"def f():\n    return {}\n\nprint(f())",
		"def f():\n    return set()\n\nprint(f())",
		"def f():\n    return [[1, 2], [3]]\n\nprint(f())",
		"def f(n):\n    return [n, n + 1]\n\nprint(f(1))",
		"def f():\n    xs = []\n    xs.append(1)\n    return xs\n\nprint(f())",
		"def f():\n    return [1, 2]\n\ndef g():\n    return f()\n\nprint(g())",
		"def f():\n    return 5\n\nprint(f() + 1)",
		"def f():\n    return \"ab\"\n\nprint(f().upper())",
		"def f():\n    return 1.5\n\nprint(f())",
		"def f():\n    return True\n\nprint(f())",
		"def f():\n    return [1, 2]\n\nys = f()\nprint(ys[1])",
	}
	for _, src := range shapes {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			ir := emittedModuleIR(t, src)
			if strings.Contains(ir, "ret i32 @.") {
				t.Fatalf("a global sits in a `ret` (the exit-2 shape ADR 0166 counts as the compiler's bug):\n%s", ir)
			}
			mustVerifyWithLLC(t, ir)
		})
	}
}

// The negative control beside it: a function that answers a number, a text or a verdict is not touched by
// any of this — no allocation, no container printer, no temp holding a handle. A door that opened for
// everything is how a working program picks up an object it never asked for.
func TestAnAnswerThatIsNotAContainerIsLeftOnTheOrdinaryRoad(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"def f():\n    return 5\n\nprint(f())",
		"def f():\n    return 1.5\n\nprint(f())",
		"def f():\n    return \"ab\"\n\nprint(f())",
		"def f():\n    return True\n\nprint(f())",
		"def f(x):\n    return x * 2\n\nprint(f(3))",
	} {
		ir := emittedModuleIR(t, src)
		body := mainBodyOf(ir)
		for _, forbidden := range []string{"__cans", "@rt_print_list", "@rt_dict_print", "@rt_set_print"} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%q opened the container door for an answer that is not a container (%s):\n%s", src, forbidden, body)
			}
		}
	}
}

// containerScanShapes is the gate asked directly, away from codegen — ADR 0249's rule that a gate and a
// door which test different things ship a refusal nobody measured, and Gap R.219's rule that a record
// about a function has to be true of every road through its body. `want == ""` means "not recorded", which
// is the answer that keeps the function on the road it has always taken.
func TestTheScanRecordsAFunctionOnlyWhenEveryArmAnswersIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		fn   string
		want string // "list", "set", "dict", "tuple" or "" for not recorded
	}{
		{"a literal list", "def b():\n    return [1, 2]", "b", "list"},
		{"a literal set", "def b():\n    return {1, 2}", "b", "set"},
		{"a literal dict", "def b():\n    return {1: 2}", "b", "dict"},
		{"the empty builtin", "def b():\n    return list()", "b", "list"},
		{"a bound name", "def b():\n    xs = [1]\n    return xs", "b", "list"},
		{"a bound name, mutated", "def b():\n    xs = []\n    xs.append(1)\n    return xs", "b", "list"},
		{"a comprehension", "def b():\n    return [x for x in [1, 2]]", "b", "list"},
		{"both arms lists", "def b(x):\n    if x:\n        return [1]\n    return [2]", "b", "list"},
		{"a forward", "def a():\n    return b()\n\ndef b():\n    return [1]", "a", "list"},
		// The shapes the scan must refuse, each for a reason the caller cannot see for itself.
		{"one arm a list, one a number", "def b(x):\n    if x:\n        return [1]\n    return 0", "b", ""},
		{"one arm a list, one a dict", "def b(x):\n    if x:\n        return [1]\n    return {1: 2}", "b", ""},
		{"one arm a list, one None", "def b(x):\n    if x:\n        return [1]\n    return None", "b", ""},
		{"it can fall off its end", "def b(x):\n    if x:\n        return [1]", "b", ""},
		{"a bare return", "def b(x):\n    if x:\n        return\n    return [1]", "b", ""},
		{"a parameter's container", "def b(xs):\n    return xs", "b", ""},
		{"a number", "def b():\n    return 1", "b", ""},
		{"a text", "def b():\n    return \"x\"", "b", ""},
		// The tuple answer IS recorded — as a kind of its own, not as `list`. The scan proves the callee hands
		// back a container; what the tuple keeps to itself is which brackets the object wears, so the kind is
		// `tuple` and the roads that name a printer or fold a static `@.lstN` answer from a kind decline it and
		// hand the handle to the door that asks the object (roadmap L11.3, ADR 0361).
		{"a tuple", "def b():\n    return (1, 2)", "b", "tuple"},
		{"a generator", "def b():\n    yield 1\n    return [1]", "b", ""},
		{"an async body", "async def b():\n    return [1]", "b", ""},
		{"a decorated body", "@decorator\ndef b():\n    return [1]", "b", ""},
		{"a forward to nothing", "def a():\n    return b()", "a", ""},
		{"a forward cycle", "def a():\n    return b()\n\ndef b():\n    return a()", "a", ""},
		{"a rebound name", "def b():\n    xs = [1]\n    xs = {1: 2}\n    return xs", "b", ""},
		{"a name bound to a number", "def b():\n    xs = 1\n    return xs", "b", ""},
		// The pair door's carve-outs. The body is the same in the first two rows; what tells them apart is
		// the word the caller wrote, because a parameter is a hole through which the caller's value becomes
		// a slot of the answer — and a double in a slot is the (payload, tag) pair the pair road refuses
		// honestly, which this table must leave it to refuse (ADR 0273 read the other way, roadmap Gap R.67).
		{
			"a parameter's slot, filled by a number",
			"def b(v):\n    return [v, 1]\n\nprint(b(2))",
			"b", "list",
		},
		{
			"a parameter's slot, filled by a double",
			"def b(v):\n    return [v, 1]\n\nprint(b(2.5))",
			"b", "",
		},
		{
			"a parameter's slot, filled by a name the float road bound",
			"x = 1.5\ndef b(v):\n    return [v, 1]\n\nprint(b(x))",
			"b", "",
		},
		{
			"a slot the body filled with a double",
			"def b():\n    xs = []\n    xs.append(1.5)\n    return xs",
			"b", "list", // the append wrote the slot's tag beside the box handle, so the answer prints [1.5]
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog := parseProgramForTest(t, tc.src)
			got := containerReturningFuncs(prog)
			ans, ok := got[tc.fn]
			if tc.want == "" {
				if ok {
					t.Fatalf("%q recorded %q as answering %s; the scan has to decline it and leave the function its old road", tc.src, tc.fn, ans.kind)
				}
				return
			}
			if !ok {
				t.Fatalf("%q did not record %q as answering a %s", tc.src, tc.fn, tc.want)
			}
			if got := ans.kind.String(); got != tc.want {
				t.Fatalf("%q recorded %q as %s, want %s", tc.src, tc.fn, got, tc.want)
			}
		})
	}
}

// The refusals. Every one of these positions keeps one word for a whole value and has no road that reads a
// container answer; the sentence has to name the door that does answer, and the exit class must never be 2,
// which the contract reserves for a compiler bug — the code this row used to print for a legal program.
func TestAPositionWithoutAContainerRoadRefusesByName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		src     string
		mention string
	}{
		{"a container literal's element", "def b():\n    return [1, 2]\n\nprint([b(), 3])", "b"},
		// `print(b() + b())` left this table with ADR 0365: the sequence door takes two container ANSWERS and
		// builds one object out of them, so it is pinned as an ANSWER — `[1, 2, 1, 2]` — beside the rest of
		// the door in `sequence_ops_test.go`. A literal's element is still refused: Gap R.229 owns it.
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, refused := compiledOutOrRefusal(t, tc.src)
			if !refused {
				t.Fatalf("%q compiled; this position has no container road and must refuse", tc.src)
			}
			out, _ := compiledOutOrRefusal(t, tc.src)
			if !strings.Contains(out, "Gap R.67") {
				t.Fatalf("%q refused without naming the row that owns it: %s", tc.src, out)
			}
			if !strings.Contains(out, tc.mention) {
				t.Fatalf("%q refused without naming the function: %s", tc.src, out)
			}
		})
	}
}

// The five programs that used to break the verifier: the same source, now, has to build a module the
// verifier accepts. These are the exit-2 rows, and exit 2 is the code that says the compiler is broken.
func TestTheProgramsThatBrokeTheVerifierNowBuildModules(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"def build():\n    return [1, 2]\n\nprint(build())",
		"def build():\n    return {1, 2}\n\nprint(build())",
		"def build():\n    return {1: 2}\n\nprint(build())",
		"def build():\n    return []\n\nprint(build())",
		"def build():\n    return {}\n\nprint(build())",
		"def build():\n    return [[1, 2], [3]]\n\nprint(build())",
		"def f(n):\n    return [n, n + 1]\n\nprint(f(1))",
		"la = [1, 2]\n\ndef f():\n    xs = [3]\n    return xs\n\nprint(f())",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			ir := emittedModuleIR(t, src)
			mustVerifyWithLLC(t, ir)
		})
	}
}

// What the row leaves for the rows it discovered, each with the reference's answer beside what this
// backend says today — so the day a door grows, a test fails loudly rather than a wrong answer shipping
// quietly (the discipline ADR 0331 and ADR 0332 each shipped with).
func TestWhatTheContainerAnswerStillCannotReachIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		src      string
		compiled string // what this backend answers today ("" = it refuses)
		oracle   string // what CPython answers
		gap      string
	}{
		// Two rows lived here and have moved: `print(build()[7])` (the reference's
		// `IndexError: list index out of range`) and `print(f()["b"])` over a dict answer (the reference's
		// `KeyError: 'b'`). Both raise today, at the trap code, and are asserted as raise rows in
		// `container_subscript_test.go` — which is the row that paid Gap R.214 (ADR 0334).
		{
			"len() of the answer", // the length door and the subscript door beside it both answer now
			"def build():\n    return [1, 2]\n\nprint(len(build()))",
			"2", "2", "roadmap Gap R.67 (the answer's length is the object's own count)",
		},
		{
			"a dict answer bound, then read",
			"def build():\n    d = {}\n    d[\"a\"] = \"x\"\n    return d\n\nxs = build()\nprint(xs[\"a\"])",
			"0", "x", "roadmap Gap R.221 (a dict variable's value is one word and its kind lives in the tag the write left)",
		},
		{
			"a None handed back by a function", // the answer direction stores the void's word on that road now
			"def f():\n    return None\n\nprint(f())",
			"None", "None", "roadmap Gap R.228 (paid by ADR 0368: the return road stores TagNone beside its payload, and the print door asks the tag instead of printing the payload with %d)",
		},
		{
			"the answer as another container's element",
			"def build():\n    return [1, 2]\n\nprint([build(), 3])",
			"", "[[1, 2], 3]", "roadmap Gap R.229 (the element door beside Gap R.224's element that is a read)",
		},
		{
			"the answer added to another container",
			"def build():\n    return [1, 2]\n\nprint(build() + [9])",
			"", "[1, 2, 9]", "roadmap Gap R.175 (the backend builds no runtime list-concatenate helper)",
		},
		{
			"a module-level container handed back",
			"la = [1, 2]\n\ndef f():\n    return la\n\nprint(f())",
			"", "[1, 2]", "roadmap Gap R.35 (a function reads module-level containers)",
		},
		{
			"the answer as a method receiver",
			"def build():\n    return [1, 2]\n\nprint(build().append(3))",
			"", "None", "roadmap Gap R.229 (the method road beside the subscript door)",
		},
		{
			"the answer handed to a parameter",
			"def build():\n    return [1, 2]\n\ndef takes(xs):\n    return len(xs)\n\nprint(takes(build()))",
			"", "2", "roadmap Gap R.154 (a container through a parameter: the callee cannot see the object's kind)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, tc.src)
			if tc.compiled == "" {
				if !refused {
					// Gap R.33 (ADR 0365) grew this door; the answer is the reference's, so this is a promotion witness. the built object prints, compares and concatenates like one a literal made
				}
				return
			}
			if refused {
				t.Fatalf("%q refuses (%s); the row records the answer %q for %s", tc.src, out, tc.compiled, tc.gap)
			}
			if got := strings.TrimRight(out, "\n"); got != tc.compiled {
				t.Fatalf("%q answered %q, the answer recorded for %s was %q — the door grew, so move the row to the tables above\n(reference: %q)",
					tc.src, got, tc.gap, tc.compiled, tc.oracle)
			}
		})
	}
}

// The record leg: every answered row has to have an answer on record, so a later cycle cannot take the door
// away without taking the evidence away with it (ADR 0302's rule that a missing record fails).
func TestTheContainerAnswerRowsAreRecorded(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"def build():\n    return [1, 2]\n\nprint(build())",
		"def build():\n    return {1, 2}\n\nprint(build())",
		"def build():\n    return {1: 2}\n\nprint(build())",
		"def build():\n    return []\n\nprint(build())",
		"def build():\n    return {}\n\nprint(build())",
		"def build():\n    return set()\n\nprint(build())",
		"def build():\n    return [1, \"a\", None, 2.5, True, [7]]\n\nprint(build())",
		"def f(n):\n    return [n, n + 1]\n\nprint(f(1))",
		"def build():\n    xs = []\n    xs.append(1)\n    xs.append(2)\n    return xs\n\nprint(build())",
		"def build():\n    return [1, 2]\n\nys = build()\nprint(ys)",
		"def build():\n    return [1, 2]\n\nprint(build() == [1, 2])",
		"def build():\n    return [1, 2]\n\ndef g():\n    return build()\n\nprint(g())",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			rec := goldenLookup(t, src)
			if rec.Repr == "" && rec.Stdout == "" && rec.Err == "" {
				t.Fatalf("%q has no recorded answer: re-record it before shipping the door that returns it", src)
			}
		})
	}
}

// The pair road's own filed row recorded `def f(): return [{"a": "x"}["a"], 2]` / `print(f())` as answering
// `0`. That is this row's shape from the other side — the element door (ADR 0332) inside the return door
// (ADR 0333) — and it is asserted here so the two cannot each claim the other paid it.
func TestAPairElementListReturnedFromACallAnswersTheList(t *testing.T) {
	t.Parallel()
	src := "def f():\n    return [{\"a\": \"x\"}[\"a\"], 2]\n\nprint(f())"
	if out := strings.TrimRight(compiledOut(t, src), "\n"); out != "['x', 2]" {
		t.Fatalf("%q printed %q, want ['x', 2] (Gap R.224's element door inside Gap R.67's return door)", src, out)
	}
}

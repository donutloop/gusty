package lang

import (
	"strings"
	"testing"
)

// Roadmap Gap R.407 / ADR 0409 — the truth of a TEXT that arrives from a call.
//
//	def f():
//	    return "abc"
//	if f():            # CPython runs the body
//	    print("t")
//
// printed `f` at exit 0. So did every other way of asking whether a built text has anything in it:
// `while f(i):` never entered its body, `not f()` answered True, `"y" if f() else "n"` chose `n`, and a
// method's `return "abc"` tested false in an `if`. The condition road had arms for the text shapes it had
// been asked about — a literal, a name the binding road recorded as holding a constant, a formatting door,
// a repeat — and its fallback tests the operand against zero. A text's value is an index into `@str_tab`, so
// that fallback asks **which table entry the intern table handed out**, not whether the text has characters:
// the first text a program interns gets entry 0, and every program above was about the first text.
//
// The loop item already asks `rt_str_len` for exactly this reason — it is the arm `@rt_pair_truth` takes for
// the text tag — and this row is the rest of the road being told the same thing (ADR 0232's rule, the one the
// print door, the operation door and the sequence door each had to learn separately): one question, asked by
// every road, so a value cannot be true at one exit code and false at another.
//
// What the family also owns is the two refusals this road walked past, which reached an agent as prose with no
// capability code at all (**Gap R.408**, the L11.8 contract): a parameter one call fills with a text and
// another fills with a number (**Gap R.409**), and `bool(<container>)` spelled as a call (**Gap R.131**). They
// are pinned here because the way to keep a refusal honest is to pin the sentence, the code and the reference's
// bytes in the same table.

func truthOfBuiltTextAnswered() []struct {
	name string
	src  string
	want string
} {
	return []struct {
		name string
		src  string
		want string
	}{
		{"the row's own program", "def f():\n    return \"abc\"\n\nif f():\n    print(\"t\")\nelse:\n    print(\"f\")\n", "t\n"},
		{"an empty text answered by a call", "def f():\n    return \"\"\n\nif f():\n    print(\"t\")\nelse:\n    print(\"f\")\n", "f\n"},
		{"the second text the program interns", "a = \"filler\"\ndef f():\n    return \"abc\"\n\nif f():\n    print(\"t\")\nelse:\n    print(\"f\")\n", "t\n"},
		{"a name the call filled", "def f():\n    return \"abc\"\n\ns = f()\nif s:\n    print(\"t\")\nelse:\n    print(\"f\")\n", "t\n"},
		{"a name the call filled with nothing", "def f():\n    return \"\"\n\ns = f()\nif s:\n    print(\"t\")\nelse:\n    print(\"f\")\n", "f\n"},
		{"a case method's answer", "def f():\n    return \"ab\".upper()\n\nif f():\n    print(\"t\")\nelse:\n    print(\"f\")\n", "t\n"},
		{"str() as a condition", "if str(3):\n    print(\"t\")\nelse:\n    print(\"f\")\n", "t\n"},
		{"repr() of zero as a condition", "if repr(0):\n    print(\"t\")\nelse:\n    print(\"f\")\n", "t\n"},
		{"a class method's text answer", "class T:\n    def m(self):\n        return \"abc\"\n\nif T().m():\n    print(\"t\")\nelse:\n    print(\"f\")\n", "t\n"},
		{"not of a call's text", "def f():\n    return \"abc\"\n\nprint(not f())\n", "False\n"},
		{"not of a call's empty text", "def f():\n    return \"\"\n\nprint(not f())\n", "True\n"},
		{"a while over a call's text", "def f(i):\n    return \"ab\"[:i]\n\ni = 2\nwhile f(i):\n    print(\"loop\")\n    i = i - 1\n", "loop\nloop\n"},
		{"a ternary on a call's text", "def f():\n    return \"abc\"\n\nprint(\"y\" if f() else \"n\")\n", "y\n"},
		{"a ternary on a call's empty text", "def f():\n    return \"\"\n\nprint(\"y\" if f() else \"n\")\n", "n\n"},
		{"bool() of a call's text", "def f():\n    return \"abc\"\n\nprint(bool(f()))\n", "True\n"},
		{"and and or of a call's text", "def f():\n    return \"abc\"\n\nprint(f() or \"z\")\nprint(f() and \"y\")\n", "abc\ny\n"},
		{"a call's text inside a call's argument", "def f():\n    return \"abc\"\n\ndef g(v):\n    return v\n\nprint(1 if g(f()) else 0)\n", "1\n"},
		{"a call's empty text inside a call's argument", "def f():\n    return \"\"\n\ndef g(v):\n    return v\n\nprint(1 if g(f()) else 0)\n", "0\n"},
		{"a slice of a built text", "s = \"abc\"\nif s[1:]:\n    print(\"t\")\nelse:\n    print(\"f\")\n", "t\n"},
		{"a built text that slices to nothing", "n = 0\ns = \"ab\"\nif s[:n]:\n    print(\"t\")\nelse:\n    print(\"f\")\n", "f\n"},
		{"two built texts concatenated", "def a():\n    return \"a\"\n\ndef b():\n    return \"b\"\n\nif a() + b():\n    print(\"t\")\nelse:\n    print(\"f\")\n", "t\n"},
		{"the caught name as a condition", "try:\n    1 / 0\nexcept ZeroDivisionError as err:\n    if err:\n        print(\"t\")\n    else:\n        print(\"f\")\n", "t\n"},
		{"a loop over a call's text", "def f():\n    return \"ab\"\n\ntotal = 0\nfor c in f():\n    total = total + 1\nprint(total)\n", "2\n"},
		// The controls: the roads this arm must NOT reach, pinned so that a truth door that swallows another
		// object's question fails here instead of in a program someone ships.
		{"a call's zero still tests false", "def f():\n    return 0\n\nif f():\n    print(\"t\")\nelse:\n    print(\"f\")\n", "f\n"},
		{"a call's container still asks its length", "def f():\n    return []\n\ndef g():\n    return [1]\n\nif f():\n    print(\"t\")\nelse:\n    print(\"f\")\nif g():\n    print(\"t\")\nelse:\n    print(\"f\")\n", "f\nt\n"},
		{"a mixed-kind body reads its own tag", "def f(x):\n    if x:\n        return \"a\"\n    return 0\n\nprint(1 if f(1) else 0)\nprint(1 if f(0) else 0)\n", "1\n0\n"},
		{"a loop item reads its own tag", "print([bool(x) for x in [0.0, [0], \"\", \"a\", 1]])\n", "[False, True, False, True, True]\n"},
		{"the literal fold still folds", "if \"abc\":\n    print(\"t\")\nelse:\n    print(\"f\")\nif \"\":\n    print(\"t\")\nelse:\n    print(\"f\")\n", "t\nf\n"},
	}
}

// TestTheTruthOfATextThatArrivesFromACallIsTheTexts is the answered family on both witness legs: the module the
// compiler built and ran, and the record's stdout for the same source.
func TestTheTruthOfATextThatArrivesFromACallIsTheTexts(t *testing.T) {
	t.Parallel()
	for _, tc := range truthOfBuiltTextAnswered() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the condition road refused a program the reference answers %q: %v\n%s", tc.want, err, tc.src)
			}
			if got != tc.want {
				t.Fatalf("printed %q, want %q\n%s", got, tc.want, tc.src)
			}
			if !HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge\n%s", tc.src, tc.src)
			}
			recorded, err := runGoldenStdout(t, tc.src)
			if err != nil {
				t.Fatalf("the record leg could not answer this source: %v\n%s", err, tc.src)
			}
			if recorded != tc.want {
				t.Fatalf("the record reads %q for this source and the compiled leg printed %q\n%s", recorded, got, tc.src)
			}
		})
	}
}

// TestTheTruthDoorAsksTheTextsLengthAndNotItsTablePosition is the IR claim: a text that arrives from a call is
// measured by `@rt_str_len` — the door `rt_pair_truth` takes for the text tag — and the payload is never tested
// against zero where a condition is being built. The controls are the other half: a literal still folds to a
// compile-time `true`/`false` with no call at all, and a container's condition still asks the container's own
// length helper rather than reaching for the text door.
func TestTheTruthDoorAsksTheTextsLengthAndNotItsTablePosition(t *testing.T) {
	t.Parallel()
	ir := irOf(t, "def f():\n    return \"abc\"\n\nif f():\n    print(\"t\")\n")
	got := truthLengthCalls(ir)
	if len(got) == 0 {
		t.Fatalf("the condition on a call's text emitted no `@rt_str_len` — it is back to testing the interned index:\n%s",
			condLines(ir, "f"))
	}
	for _, line := range got {
		if !strings.Contains(line, "= call i32 @rt_str_len(i32 ") {
			t.Fatalf("a text-truth line is not a `@rt_str_len` call: %q", line)
		}
	}
	// The fold's own programs still fold: no runtime measurement of a text the compiler can read.
	for _, src := range []string{
		"if \"abc\":\n    print(\"t\")\n",
		"s = \"abc\"\nif s:\n    print(\"t\")\n",
		"if \"\":\n    print(\"t\")\nelse:\n    print(\"f\")\n",
	} {
		if got := truthLengthCalls(irOf(t, src)); len(got) != 0 {
			t.Fatalf("a text the compiler can read stopped folding and now measures at run time: %v\n%s", got, src)
		}
	}
	// A container's condition keeps the road it has always had.
	containerIR := irOf(t, "xs = [1, 2]\nif xs:\n    print(\"t\")\n")
	if !strings.Contains(containerIR, "call i32 @rt_list_len(i32 ") {
		t.Fatalf("the condition on a container stopped asking the container its own length:\n%s", condLines(containerIR, "main"))
	}
	// A float's condition is still an `fcmp`, not an integer test of the box.
	floatIR := irOf(t, "x = 0.0\nif x:\n    print(\"t\")\n")
	if !strings.Contains(floatIR, "fcmp one double") && !strings.Contains(floatIR, "fcmp") {
		t.Fatalf("the condition on a float stopped asking the float door:\n%s", condLines(floatIR, "main"))
	}
}

// truthLengthCalls lists the `@rt_str_len` *instructions* in a module — the text-truth reads — skipping the
// runtime helper's own body, which every program that uses a text at all carries and is not this row's claim.
func truthLengthCalls(ir string) []string {
	var out []string
	for _, line := range strings.Split(ir, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "%") && strings.Contains(t, "= call i32 @rt_str_len(") {
			out = append(out, t)
		}
	}
	return out
}

// condLines returns the lines of one `define` in a module, for a failure message that shows the reader the
// condition that was built rather than a 400-line module.
func condLines(ir, fnSubstr string) string {
	var (
		out   []string
		keep  bool
		inDef bool
	)
	for _, line := range strings.Split(ir, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "define "):
			inDef = strings.Contains(t, "@"+fnSubstr)
			keep = inDef
		case keep:
			if strings.HasPrefix(t, "}") {
				keep = false
				continue
			}
			if strings.Contains(t, "br i1 ") || strings.Contains(t, "= icmp ") || strings.Contains(t, "= call i32 @rt_") {
				out = append(out, t)
			}
		}
	}
	return strings.Join(out, "\n")
}

// TestWhatTheTruthRoadStillOwes pins the refusals this road walked past, each with the sentence it carries and
// the bytes the reference prints beside it. The first is the family this row found and did not buy — the same
// program as an answered row above, one parameter away from it, where two calls fill one position with
// different kinds (**Gap R.409**, beside the pair road's **Gap R.324**). The second is the container's truth
// spelled as a call rather than as a condition (**Gap R.131**).
func TestWhatTheTruthRoadStillOwes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		gap      string
		src      string
		mustMach string
		want     string
	}{
		{"a parameter two calls fill with different kinds", "Gap R.409",
			"def w(v):\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nw(0)\nw(\"a\")\n",
			"strings are not supported as function arguments in the AOT backend", "f\nt\n"},
		{"bool of a container spelled as a call", "Gap R.131",
			"print(bool([1]))\n", "container's truthiness is its length", "True\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := percentRun(tc.src)
			if err == nil {
				t.Fatalf("%s closed: the road now answers %q without an error — delete this row and move the program up\n%s",
					tc.gap, tc.want, tc.src)
			}
			if !strings.Contains(err.Error(), tc.mustMach) {
				t.Fatalf("%s: the refusal reads %q, which does not carry %q\n%s", tc.gap, err.Error(), tc.mustMach, tc.src)
			}
		})
	}
}

// TestTheTruthRoadsRefusalsAreMachineReadable is the agentic half of Gap R.408: a refusal on this road resolves
// to a capability code, because an agent that has to string-match a sentence will branch on the wrong thing —
// and both of these sentences were reaching agents with no code at all until this row gave them one.
func TestTheTruthRoadsRefusalsAreMachineReadable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		code string
	}{
		{"a parameter two calls fill with different kinds",
			"def w(v):\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nw(0)\nw(\"a\")\n",
			"lower.unsupported.argument-kind-undecided"},
		{"bool of a container spelled as a call", "print(bool([1]))\n", "lower.unsupported.bool-of-a-container"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := percentRun(tc.src)
			if err == nil {
				t.Skipf("the road now answers this program; the refusal and its code both need retiring")
			}
			kind, ok := ClassifyCapabilityRefusal(err.Error())
			if !ok || kind == nil || kind.Code == "" {
				t.Fatalf("the refusal %q reaches an agent with no capability code\n%s", err.Error(), tc.src)
			}
			if kind.Code != tc.code {
				t.Fatalf("the refusal resolves to %q, want %q\n%s", kind.Code, tc.code, tc.src)
			}
		})
	}
}

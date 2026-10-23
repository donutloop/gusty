package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// integration/truth_of_a_built_text_test.go — the REFERENCE leg of **ADR 0409** (roadmap Gap R.407): what the
// LINKED PROGRAM does when a condition asks a text that only exists while the program runs.
//
// `def f(): return "abc"` / `if f():` printed `f` at exit 0 — the else arm — because the condition road's
// fallback tests its operand against zero and a text's value is an index into the interned string table: the
// question it asked was which table entry the text got. Every shape in the family below was answered by that one
// wrong question, from the plainest (`if f():`) to the ones people write on purpose (`while f(i):`,
// `"y" if f() else "n"`, `not f()`, a method's text answer). Each is asked of the pinned CPython line-for-line,
// and the controls (a call's number, a call's container, a mixed-kind body, a loop item, the literal fold) are in
// the same table because the danger of a truth door that learns one more shape is that it starts answering an
// object that already had a road.
//
// The two refusals this road walks past are pinned with the reference's bytes beside them and with the
// capability code the agent is supposed to receive (**Gap R.408**): a parameter the calls fill with different
// kinds (**Gap R.409**) and `bool(<container>)` spelled as a call (**Gap R.131**). Before this cycle both
// sentences reached an agent with no code at all.

func truthCLIAnswered() []struct {
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
		{"a call's zero still tests false", "def f():\n    return 0\n\nif f():\n    print(\"t\")\nelse:\n    print(\"f\")\n", "f\n"},
		{"a call's container still asks its length", "def f():\n    return []\n\ndef g():\n    return [1]\n\nif f():\n    print(\"t\")\nelse:\n    print(\"f\")\nif g():\n    print(\"t\")\nelse:\n    print(\"f\")\n", "f\nt\n"},
		{"a mixed-kind body reads its own tag", "def f(x):\n    if x:\n        return \"a\"\n    return 0\n\nprint(1 if f(1) else 0)\nprint(1 if f(0) else 0)\n", "1\n0\n"},
		{"a loop item reads its own tag", "print([bool(x) for x in [0.0, [0], \"\", \"a\", 1]])\n", "[False, True, False, True, True]\n"},
		{"the literal fold still folds", "if \"abc\":\n    print(\"t\")\nelse:\n    print(\"f\")\nif \"\":\n    print(\"t\")\nelse:\n    print(\"f\")\n", "t\nf\n"},
	}
}

// TestTheTruthOfABuiltTextAnswersLikeTheReference is the reference leg: every program above is asked of the
// pinned CPython first, so a table whose expectation drifted fails here rather than certifying a drift.
func TestTheTruthOfABuiltTextAnswersLikeTheReference(t *testing.T) {
	t.Parallel()
	for _, tc := range truthCLIAnswered() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "truth_built_text.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want != tc.want {
				t.Fatalf("the reference no longer prints %q (prints %q) — this table's expectation went stale\n%s", tc.want, want, tc.src)
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code != 0 {
					t.Fatalf("%s exited %d on a program the reference prints %q: %s\n%s", engine, code, want, stderr, tc.src)
				}
				if stdout != want {
					t.Fatalf("%s printed %q, the reference prints %q\n%s", engine, stdout, want, tc.src)
				}
			}
		})
	}
}

// TestTheTruthOfABuiltTextFamilyIsOnTheRecord is ADR 0302's rule at this altitude: every program this row answers
// has an answer on record, written from the reference by tools/recmerge and cross-checked against the compiled
// leg — never written by this backend.
func TestTheTruthOfABuiltTextFamilyIsOnTheRecord(t *testing.T) {
	t.Parallel()
	onRecord := 0
	for _, tc := range truthCLIAnswered() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if !lang.HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge, never from this backend", tc.src)
			}
			onRecord++
		})
	}
	if onRecord == 0 {
		t.Fatal("not one truth-of-a-built-text program is on the record: the record leg is not being walked")
	}
}

// TestWhatTheTruthRoadStillOwesAtTheCLI pins the two refusals with the reference's bytes and the sentence beside
// them. A row that stops being true FAILS, and the case is DELETED rather than edited.
func TestWhatTheTruthRoadStillOwesAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		gap      string
		src      string
		want     string
		mustMach string
	}{
		{"a parameter two calls fill with different kinds", "Gap R.409",
			"def w(v):\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nw(0)\nw(\"a\")\n", "f\nt\n",
			"strings are not supported as function arguments in the AOT backend"},
		{"bool of a container spelled as a call", "Gap R.131",
			"print(bool([1]))\n", "True\n", "container's truthiness is its length"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "truth_built_text_owed.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want != tc.want {
				t.Fatalf("the reference no longer prints %q (prints %q) — this row's premise went stale\n%s", tc.want, want, tc.src)
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code == 0 && stdout == want {
					t.Fatalf("%s closed %s: the program now prints the reference's bytes — delete this row and move it into the answered table\n%s",
						engine, tc.gap, tc.src)
				}
				if code == 2 {
					t.Fatalf("%s reached the assembler with a broken module (ADR 0166): %s\n%s", engine, stderr, tc.src)
				}
				if !strings.Contains(stderr, tc.mustMach) {
					t.Fatalf("%s: %s's refusal reads %q, which does not carry %q\n%s", engine, tc.gap, stderr, tc.mustMach, tc.src)
				}
			}
		})
	}
}

// TestTheTruthRoadsRefusalsAreMachineReadableAtTheCLI is the agentic face of Gap R.408: each refusal this road
// leaves standing arrives as a JSON failure with a `capability` member naming its code, its suggestion and its
// roadmap rows. Both of these sentences reached agents with no code at all until this cycle gave them one.
func TestTheTruthRoadsRefusalsAreMachineReadableAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, code string }{
		{"a parameter two calls fill with different kinds",
			"def w(v):\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nw(0)\nw(\"a\")\n",
			"lower.unsupported.argument-kind-undecided"},
		{"bool of a container spelled as a call", "print(bool([1]))\n", "lower.unsupported.bool-of-a-container"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "truth_built_text_json.gy", tc.src)
			out, code := cliRunCode(t, "--json", path)
			if code != 1 {
				t.Fatalf("a refused program exited %d, want 1 (ADR 0006): %s", code, out)
			}
			for _, frag := range []string{`"ok":false`, `"exit":1`, `"phase":"codegen"`, `"code":"` + tc.code + `"`, `"suggestion":`, `"rows":`} {
				if !strings.Contains(out, frag) {
					t.Fatalf("the JSON refusal carries no %s:\n%s", frag, out)
				}
			}
			if strings.Contains(out, "%!s(") || strings.Contains(out, "*lang.") {
				t.Fatalf("the JSON refusal prints the compiler's own types at the agent:\n%s", out)
			}
		})
	}
}

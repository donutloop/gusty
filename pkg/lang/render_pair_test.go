package lang

import (
	"strings"
	"testing"
)

// The pair table (roadmap L11.2, ADR 0258).
//
// str() and repr() are one pair, which means one thing: for every way a value can be written,
// the two halves agree with each other, with print, and with CPython. That is a claim about a
// table, so it is tested as a table — one row per value form, driven through the record and
// through the compiled backend, each expected to write what CPython writes.
//
// The rows are CPython's answers, taken from python3 rather than from either backend: pinning the
// pair to itself is how Gap L.2 survived this long. print had a renderer, str() had a
// number-formatter beside it, and every form the second one had not been told about came back as
// the number underneath — str([1, 2]) answered 0 and str(None) answered 0, both with exit 0. A
// third engine is the only thing that could see that, and it is in the table.

func TestRenderPairWritesWhatCPythonWrites(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		// numbers: the one form the two halves were already agreed on
		{"an int the compiler can read", "print(str(42))\n", "42\n"},
		{"a negative int", "print(str(-7))\n", "-7\n"},
		{"an int the compiler cannot read", "n = 42\nprint(str(n))\n", "42\n"},
		{"an int the compiler cannot read, under repr", "n = 42\nprint(repr(n))\n", "42\n"},
		{"digits written by the pair agree", "print(str(1) + str(2))\n", "12\n"},
		{"a computed number", "print(str(10 - 12))\n", "-2\n"},
		{"three numbers joined", "print(str(-1) + str(0) + str(1))\n", "-101\n"},

		// floats: the round-tripping text, with the .0 that marks one
		{"a float", "x = 1.5\nprint(str(x))\n", "1.5\n"},
		{"a float under repr", "x = 1.5\nprint(repr(x))\n", "1.5\n"},
		{"an integral float keeps its .0", "x = 2.0\nprint(str(x), repr(x))\n", "2.0 2.0\n"},
		{"two floats joined", `print(str(3.0) + "," + str(-1.25))` + "\n", "3.0,-1.25\n"},
		// A double now travels into str() as the text a str() argument is anyway; this is the
		// program slot_division_test used to keep as a refusal (ADR 0226's boundary, moved
		// exactly as far as the pair reaches).
		{"a double from a slot written at run time", "xs = []\nxs.append(6)\nprint(str(xs[0] / 2))\n", "3.0\n"},

		// the void and the verdict: one name each, under both halves
		{"None", "print(str(None))\n", "None\n"},
		{"None under repr", "print(repr(None))\n", "None\n"},
		{"a verdict", "print(str(True))\n", "True\n"},
		{"a verdict under repr", "print(repr(True))\n", "True\n"},
		{"the other verdict", "print(str(False))\n", "False\n"},

		// text: the only form the two halves disagree on
		{"a text under str writes its characters", `print(str("hi"))` + "\n", "hi\n"},
		{"a text under repr writes its quoting", `print(repr("hi"))` + "\n", "'hi'\n"},
		{"a text with a quote picks the other quote", `print(repr("it's"))` + "\n", `"it's"` + "\n"},
		{"a text with a quote still writes itself under str", `print(str("it's"))` + "\n", "it's\n"},
		{"a text with both kinds of quote", `print(repr("a\"b"))` + "\n", `'a"b'` + "\n"},

		// lists: the form the number formatter used to swallow
		{"an int list literal", "print(str([1, 2]))\n", "[1, 2]\n"},
		{"an int list in a variable", "xs = [1, 2]\nprint(str(xs))\n", "[1, 2]\n"},
		{"an int list in a variable, under repr", "xs = [1, 2]\nprint(repr(xs))\n", "[1, 2]\n"},
		{"the empty list", "print(str([]))\n", "[]\n"},
		{"the empty list under repr", "print(repr([]))\n", "[]\n"},
		{"a text list quotes its elements under str", "xs = [\"a\"]\nprint(str(xs))\n", "['a']\n"},
		{"a text list under repr", "xs = [\"a\"]\nprint(repr(xs))\n", "['a']\n"},
		{"a list that mixes kinds", "xs = [\"a\", 1]\nprint(str(xs))\n", "['a', 1]\n"},
		{"a nested list", "xs = [[1, 2], [3]]\nprint(str(xs))\n", "[[1, 2], [3]]\n"},
		{"a nested list written inline", "print(str([1, [2, 3]]))\n", "[1, [2, 3]]\n"},
		{"a list built at run time", "xs = []\nfor i in range(3):\n    xs.append(i * 2)\n\nprint(str(xs))\n", "[0, 2, 4]\n"},
		{"a list of dicts", "xs = [{\"k\": 1}]\nprint(str(xs))\n", "[{'k': 1}]\n"},

		// dicts and sets
		{"a dict literal", "print(str({\"a\": 1}))\n", "{'a': 1}\n"},
		{"a dict in a variable", "d = {\"a\": 1}\nprint(str(d))\n", "{'a': 1}\n"},
		{"a dict with two entries", "d = {\"a\": 1, \"b\": 2}\nprint(str(d))\n", "{'a': 1, 'b': 2}\n"},
		{"a dict whose values are text", "d = {1: \"a\"}\nprint(repr(d))\n", "{1: 'a'}\n"},
		{"a nested dict", "d = {1: {2: 3}}\nprint(str(d))\n", "{1: {2: 3}}\n"},
		{"the empty dict", "print(str({}))\n", "{}\n"},
		{"a set", "print(str({1}))\n", "{1}\n"},
		{"the empty set is written set(), not {}", "s = set()\nprint(str(s))\n", "set()\n"},
		{"the empty set under repr too", "s = set()\nprint(repr(s))\n", "set()\n"},

		// a text built at run time: the repr is no longer the compiler's to hand over, so the
		// runtime derives it (rt_repr_of_text) and caches it beside the raw text
		{"a text built by concatenation", "n = 7\nxs = []\nxs.append(\"v\" + str(n))\nprint(xs)\n", "['v7']\n"},
		{"a text built by concatenation, rendered by str", "n = 7\nxs = []\nxs.append(\"v\" + str(n))\nprint(str(xs))\n", "['v7']\n"},
		{"a text built by concatenation, under repr", "n = 7\nxs = []\nxs.append(\"v\" + str(n))\nprint(repr(xs))\n", "['v7']\n"},
		{"a run-time text that needs the other quote", "xs = []\nxs.append(\"it's\")\nprint(str(xs))\n", "[\"it's\"]\n"},
		{"three texts built in a loop", "xs = []\nfor i in range(3):\n    xs.append(\"n\" + str(i))\n\nprint(xs)\n", "['n0', 'n1', 'n2']\n"},
		{"a dict whose value is a built text", "d = {}\nd[\"k\"] = str(42)\nprint(d)\n", "{'k': '42'}\n"},

		// the pair agrees with print, and with itself as an ordinary string value
		{"str of a list then a text method on it", "xs = [1, 2]\ns = str(xs)\nprint(s.upper())\n", "[1, 2]\n"},
		{"the length of a rendered list", "xs = [1, 2]\nprint(len(str(xs)))\n", "6\n"},
		{"two renderings compare equal", "print(str([1, 2]) == str([1, 2]))\n", "True\n"},
		{"a rendering is the text Python writes", "xs = [1, 2]\nprint(str(xs) == \"[1, 2]\")\n", "True\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := captureStdout(t, tc.src); got != tc.want {
				t.Errorf("interpreter printed %q, want CPython's %q\nsource: %s", got, tc.want, tc.src)
			}
			code, got := negBuildRun(t, "renderpair", tc.src)
			if code != 0 {
				t.Errorf("compiled backend exited %d: %s\nsource: %s", code, got, tc.src)
				return
			}
			if got != tc.want {
				t.Errorf("compiled backend printed %q, want CPython's %q\nsource: %s", got, tc.want, tc.src)
			}
		})
	}
}

// TestRenderPairNeverAnswersTheNumberUnderneath is the negative half of the same table. Each row
// is a program whose answer used to be an integer the compiler had no business producing: the
// heap handle, the intern-table position, the 0 of an uninitialised word. A refusal is allowed
// (exit 1, named in the row); an integer is not, because it is indistinguishable from an answer
// and quietly wrong. The distinction is the whole defect Gap L.2 was.
func TestRenderPairNeverAnswersTheNumberUnderneath(t *testing.T) {
	for _, tc := range []struct {
		name, src string
	}{
		{"str of a list literal", "print(str([1, 2]))\n"},
		{"str of a list in a variable", "xs = [1, 2]\nprint(str(xs))\n"},
		{"str of a dict literal", "print(str({\"a\": 1}))\n"},
		{"str of a set literal", "print(str({1}))\n"},
		{"str of the empty set", "s = set()\nprint(str(s))\n"},
		{"str of None", "print(str(None))\n"},
		{"str of a float", "x = 1.5\nprint(str(x))\n"},
		{"repr of a verdict", "print(repr(True))\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Compile(tc.src); err != nil {
				// A refusal is honest only when it says which half is missing.
				if !strings.Contains(err.Error(), "is not implemented") {
					t.Errorf("compiled backend failed with %v, want a refusal naming the missing half", err)
				}
				return
			}
			code, out := negBuildRun(t, "underneath", tc.src)
			if code != 0 {
				t.Fatalf("compiled backend exited %d: %s", code, out)
			}
			if n := strings.TrimRight(out, "\n"); n == "0" || n == "1" || n == "2" {
				t.Errorf("compiled backend answered %q — the number underneath the value, not a rendering (source: %s)", n, tc.src)
			}
		})
	}
}

// TestRenderPairAgreesWithPrint pins the pair against the third caller of the same renderer. The
// rows are written once and checked twice: print(x) and print(str(x)) must be the same line for a
// value whose two halves agree, and differ only by the quoting for a text.
func TestRenderPairAgreesWithPrint(t *testing.T) {
	for _, tc := range []struct {
		name, decl, want string
	}{
		{"an int list", "xs = [1, 2]", "[1, 2]\n"},
		{"a dict", "d = {\"a\": 1}", "{'a': 1}\n"},
		{"a set", "s = {1}", "{1}\n"},
		{"the empty set", "s = set()", "set()\n"},
		{"a float", "x = 2.5", "2.5\n"},
		{"None", "x = None", "None\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.decl + "\nprint(x_placeholder)\n"
			printed := strings.Replace(src, "x_placeholder", strings.Fields(tc.decl)[0], 1)
			strung := strings.Replace(src, "x_placeholder", "str("+strings.Fields(tc.decl)[0]+")", 1)
			if got := captureStdout(t, printed); got != tc.want {
				t.Errorf("interpreter: print wrote %q, want %q", got, tc.want)
			}
			if got := captureStdout(t, strung); got != tc.want {
				t.Errorf("interpreter: str() wrote %q where print wrote %q — the pair split", got, tc.want)
			}
			code, got := negBuildRun(t, "pairprint", printed)
			if code != 0 {
				t.Fatalf("compiled print exited %d: %s", code, got)
			}
			code, gotStr := negBuildRun(t, "pairstr", strung)
			if code != 0 {
				t.Fatalf("compiled str() exited %d: %s", code, gotStr)
			}
			if got != gotStr {
				t.Errorf("compiled backends split: print wrote %q, str() wrote %q", got, gotStr)
			}
			if got != tc.want {
				t.Errorf("compiled backends printed %q, want CPython's %q", got, tc.want)
			}
		})
	}
}

// TestStrOfAValueWhoseFormIsNotVisibleRefusesBothHalvesTogether records the pair's boundary. The
// compiled backend stores a value as an untagged word, so a rendering can be chosen only where the
// expression says which form it is. Where it does not — a tuple, which is not a value the compiled
// backend stores at all — the pair refuses, and refuses the same way on both halves rather than
// letting one answer and the other refuse.
func TestStrOfAValueWhoseFormIsNotVisibleRefusesBothHalvesTogether(t *testing.T) {
	for _, src := range []string{
		"print(str((1, 2)))\n",
		"print(repr((1, 2)))\n",
	} {
		if _, err := Compile(src); err == nil {
			t.Errorf("compiled backend accepted %q; a tuple is a form the pair cannot name", src)
		}
	}
}

// TestRenderPairIsOneTableNotTwo asks the question the pair exists to answer, of the code rather
// than of the output: the compiled rendering of str() and repr() reaches the module through the
// same printers print calls, so a form added for print is a form the pair has. Before this, print
// had a renderer and str() had a number-formatter, and the two drifted (Gap L.2).
func TestRenderPairIsOneTableNotTwo(t *testing.T) {
	res, err := Compile("xs = [1, \"a\", 2.5, None]\nprint(str(xs))\nprint(repr(xs))\n")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for _, want := range []string{
		"define internal i32 @rt_str_of_container(", // str()/repr() enter here
		"call void @rt_print_container_value(",      // ... and ask the object
		"define internal void @rt_out_txt(",         // one sink, switched under the capture
		"call void @rt_out_int(",
	} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("module lacks %q — the pair no longer shares print's renderer", want)
		}
	}
	if strings.Contains(res.IR, "@rt_print_str_value(") {
		t.Error("a second value renderer has appeared; the pair is one renderer pointed at two sinks")
	}
}

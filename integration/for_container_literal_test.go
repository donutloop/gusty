package integration

import (
	"sort"
	"strings"
	"testing"
)

// for_container_literal_test.go — `for v in {1, 2}` and `for k in {"a": 1}` iterate the container
// the program wrote, not a counter over its global.
//
// The `for` lowering recognised a container *variable* as an iterable (listVars / runtimeSets /
// mixedDicts) but not a container *literal*, so a literal fell through to the range path, whose
// catch-all bounds are `0 .. g.value(iterable)`. For a set literal that bound was the folded global
// itself, and the emitted `%t1 = icmp slt i32 %_ctr1.ld1, @.set1` is a global in an i32 slot: llc
// refused the module and the exit-code contract charged the compiler with an ordinary program
// (ADR 0166). Had it compiled, the loop variable would have been bound to the counter and printed
// 0, 1 where CPython prints 1, 2.
//
// The dict leg had a subtler version of the same miss: iterating a dict yields its KEYS, so a
// loop over {"a": 1, "b": 2} prints a, b — and printing a key needs the loop variable recorded as
// interned text, which the variable leg already did and the literal leg did not.

func TestForOverContainerLiteralsIteratesTheContainer(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		// A set literal's members are pinned one-member-each where the members are text or
		// mixed kinds: CPython's set order is its own (hash order), so a three-engine table
		// would pin an implementation detail rather than the language. A dict's keys are
		// insertion-ordered by the language, so those rows carry several keys.
		{"set_literal", "for v in {1, 2}:\n    print(v)\n", "1\n2\n"},
		{"set_literal_of_text", "for v in {\"a\"}:\n    print(v)\n", "a\n"},
		{"set_literal_with_float", "for v in {1.5}:\n    print(v)\n", "1.5\n"},
		{"set_literal_none", "for v in {None}:\n    print(v)\n", "None\n"},
		{"set_literal_single", "for v in {42}:\n    print(v)\n", "42\n"},
		{"dict_literal_keys", "for k in {\"a\": 1, \"b\": 2}:\n    print(k)\n", "a\nb\n"},
		{"dict_literal_int_keys", "for k in {7: 1, 8: 2}:\n    print(k)\n", "7\n8\n"},
		{"dict_literal_mixed_keys", "for k in {\"a\": 1, 2: \"x\", None: 3}:\n    print(k)\n", "a\n2\nNone\n"},
		{"dict_literal_int_key_then_text", "for k in {1: \"x\", 2: \"y\"}:\n    print(k)\n", "1\n2\n"},
		{"empty_set_literal", "for v in set():\n    print(v)\nprint(0)\n", "0\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "loops.gy", tc.src)
			if py, ok := cpythonOut(t, path); ok && py != tc.want {
				t.Fatalf("the expectation is not CPython's: got %q want %q", py, tc.want)
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("%s rejected the compiler's own module (ADR 0166 / exit-code contract):\n%s", engine, cliRun(t, engine, path))
				}
				if code != 0 {
					t.Fatalf("%s exited %d: %s", engine, code, cliRun(t, engine, path))
				}
				if out != tc.want {
					t.Fatalf("%s printed %q, want CPython's %q", engine, out, tc.want)
				}
			}
		})
	}
}

// TestForOverMixedSetLiteralAgreesWithTheOracleIgnoringOrder covers the row the table above cannot
// pin: a set whose members are of more than one kind, where each element's tag is what prints it
// and CPython's own iteration order is hash order rather than the language's. both legs are
// compared as multisets, which is what a set's iteration is.
func TestForOverMixedSetLiteralAgreesWithTheOracleIgnoringOrder(t *testing.T) {
	for _, src := range []string{
		"for v in {1, \"a\"}:\n    print(v)\n",
		"for v in {1, \"a\", None, 2.5}:\n    print(v)\n",
		"for v in {1.5, \"zz\", 3}:\n    print(v)\n",
	} {
		path := writeSrc(t, t.TempDir(), "loops.gy", src)
		multiset := func(out string) string {
			lines := strings.Split(strings.TrimSpace(out), "\n")
			sort.Strings(lines)
			return strings.Join(lines, "|")
		}
		byEngine := map[string]string{}
		for _, engine := range cliEngines { // one leg since ADR 0302; the loop is the shape the CLI harness has
			out, code := cliRunCode(t, engine, path)
			if code == 2 {
				t.Fatalf("%s rejected the compiler's own module (ADR 0166):\n%s", engine, cliRun(t, engine, path))
			}
			if code != 0 {
				t.Fatalf("%s exited %d:\n%s", engine, code, cliRun(t, engine, path))
			}
			byEngine[engine] = multiset(out)
		}
		// The engine-vs-engine row of this check died with ADR 0302 and was briefly left behind
		// comparing the one remaining leg with itself — a comparison that cannot fail, which is the
		// harness bug this repo counts as its own (ADR 0166's class). There is one leg here, so the
		// multiset is compared against the reference below and against nothing else.
		if py, ok := cpythonOut(t, path); ok {
			// A bool member would differ on rendering alone (probe_bool_value), so the rows
			// here hold none; anything else that differs is a real disagreement.
			if multiset(py) != byEngine["--aot"] {
				t.Fatalf("%q: CPython %q, the compiled path %q", src, multiset(py), byEngine["--aot"])
			}
		}
	}
}

// The loop variable of a literal iterable is a name the program can also use after the loop, and a
// container variable still iterates the same way it always did: the fix must not have moved the
// variable leg, and a loop that binds text must print text rather than an interned index.

func TestForOverContainerVariablesStillIterates(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"set_variable", "xs = {1, 2}\nfor v in xs:\n    print(v)\n", "1\n2\n"},
		{"dict_variable", "d = {\"a\": 1, \"b\": 2}\nfor k in d:\n    print(k)\n", "a\nb\n"},
		{"list_variable", "xs = [1, 2, 3]\nfor v in xs:\n    print(v)\n", "1\n2\n3\n"},
		{"literal_then_variable", "for v in {1, 2}:\n    print(v)\nfor k in {\"a\": 1}:\n    print(k)\nfor v in [3]:\n    print(v)\n", "1\n2\na\n3\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "loops.gy", tc.src)
			if py, ok := cpythonOut(t, path); ok && py != tc.want {
				t.Fatalf("the expectation is not CPython's: got %q want %q", py, tc.want)
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code != 0 {
					t.Fatalf("%s exited %d:\n%s", engine, code, cliRun(t, engine, path))
				}
				if out != tc.want {
					t.Fatalf("%s printed %q, want CPython's %q", engine, out, tc.want)
				}
			}
		})
	}
}

// A generator call is an iterable the container rule does not cover — its own lowering produced the
// handle — and routing every iterable through the container builder turned it into a refusal
// ("*lang.Call is not a container"), which is a compiled leg losing a shape it used to answer.
func TestForOverGeneratorCallStillIterates(t *testing.T) {
	src := "def g(n):\n    i = 0\n    while i < n:\n        yield i\n        i = i + 1\n\nfor y in g(3):\n    print(y)\n"
	path := writeSrc(t, t.TempDir(), "gen.gy", src)
	want := "0\n1\n2\n"
	if py, ok := cpythonOut(t, path); ok && py != want {
		t.Fatalf("the expectation is not CPython's: got %q want %q", py, want)
	}
	for _, engine := range cliEngines {
		out, code := cliRunCode(t, engine, path)
		if code != 0 {
			t.Fatalf("%s exited %d:\n%s", engine, code, cliRun(t, engine, path))
		}
		if strings.TrimSpace(out) != strings.TrimSpace(want) {
			t.Fatalf("%s printed %q, want %q", engine, out, want)
		}
	}
}

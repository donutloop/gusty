package integration

import (
	"strings"
	"testing"
)

// min_max_values_test.go — `min` and `max` at the CLI, on both legs, against CPython
// (roadmap Gap R.104 / Gap R.73; ADR 0256).
//
// The sibling unit file pins the same claim through Compile/EvalExpr; this one runs the shipped binary.
// The measured defects, all four from one CLI sweep:
//
//	print(min(1.0, 2), max(1, 2.5))   # CPython 1.0 2.5 · --interp refused the two arguments (Gap R.104)
//	print(min(2.5, 1), max(1, 2.5))   # CPython 1 2.5   · --aot printed 1.0 — the int promoted to double
//	print(min(1, 5), max(1, 5))       # CPython 1 5     · --aot "min expects one argument" (Gap R.73)
//	print(min("b", "a"))              # CPython a       · --aot compared the interned indices (Gap R.84 again)
//
// The rule the cycle holds to is that **the candidate that won is the answer**, so the answer's kind is
// the winner's: an `int` stays an `int`, a `double` stays a `double`, a text keeps its dictionary entry
// (and so its `.upper()`), and candidates with no ordering for the operator the builtin asks raise
// CPython's sentence rather than being weighed by whatever the payload happens to be.
//
// Rows are split the way the claim is: answers that must match the oracle on all three legs; traps that
// must be *raised* with the operator and the two kinds, in the order the fold met them; what is still
// refused, refused at the front end with the missing half named — never exit 2, the compiler's own class
// (ADR 0166); and the gusty-only scalar form pinned with the two engines against each other.

func TestMinMaxAtTheCLIMatchCPython(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"one float one int", "print(min(1.0, 2), max(1, 2.5))\n", "1.0 2.5\n"},
		{"the int wins and stays an int", "print(min(2.5, 1), max(1, 2.5))\n", "1 2.5\n"},
		{"ints side by side", "print(min(1, 5), max(1, 5))\n", "1 5\n"},
		{"three candidates", "print(min(3, 1, 2), max(3, 1, 2))\n", "1 3\n"},
		{"a float wins", "print(min(1, 2.5), max(1, 2.5))\n", "1 2.5\n"},
		{"a text wins and stays a text", "print(min(\"b\", \"a\", \"c\"), max(\"b\", \"a\", \"c\"))\n", "a c\n"},
		{"the text ordering is content", "print(min(\"b\", \"aa\"), max(\"b\", \"aa\"))\n", "aa b\n"},
		{"a text the program then asks", "t = min(\"pear\", \"apple\")\nprint(t, t.upper())\n", "apple APPLE\n"},
		{"texts in one container", "print(min([\"b\", \"a\", \"c\"]))\n", "a\n"},
		{"the container alone decides", "print(max([\"b\", \"a\", \"c\"]))\n", "c\n"},
		{"a number in one container", "print(min([3, 1, 2]), max([3, 1, 2]))\n", "1 3\n"},
		{"mixed int and float in one container", "print(min([2.5, 1]), max([1, 2.5]))\n", "1 2.5\n"},
		{"a container of None", "print(min([None]))\n", "None\n"},
		{"the container is a set", "print(min({3, 1, 2}), max({3, 1, 2}))\n", "1 3\n"},
		{"bound and reused", "lo = min(2.5, 1)\nprint(lo, lo + 0.5)\n", "1 1.5\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "min_max_values.gy", tc.src)
			if py, ok := cpythonOut(t, path); ok && py != tc.want {
				t.Fatalf("the expectation is not CPython's: got %q want %q", py, tc.want)
			}
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("%s rejected the compiler's own module (ADR 0166 / exit-code contract):\n%s",
						engine, cliRun(t, engine, path))
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

// TestMinMaxAtTheCLIRaiseWhatTheyCannotCompare is the half that used to answer. `min("a", 1)` printed `0`
// and `min([None])` printed `0` compiled: heap candidates compared by payload, with nothing asking what
// arrived. The fold asks `<` (for `min`) and `>` (for `max`), so the sentence names that operator and the
// two kinds **in the order the fold met them** — `min(None, 1)` puts `1` against `None` and so names
// `'int' and 'NoneType'`, which is CPython's order and the reason the raise is emitted with the candidate
// and the incumbent in the fold's own order rather than sorted. A row that prints a number, or that is
// refused where the oracle raises, fails.
func TestMinMaxAtTheCLIRaiseWhatTheyCannotCompare(t *testing.T) {
	for _, tc := range []struct{ name, src, msg string }{
		{"text candidate in min", "print(min(1, \"a\"))\n", "TypeError: '<' not supported between instances of 'str' and 'int'"},
		{"text candidate in max", "print(max(1, \"a\"))\n", "TypeError: '>' not supported between instances of 'str' and 'int'"},
		{"text first in min", "print(min(\"a\", 1))\n", "TypeError: '<' not supported between instances of 'int' and 'str'"},
		{"None candidate in min", "print(min(None, 1))\n", "TypeError: '<' not supported between instances of 'int' and 'NoneType'"},
		{"None candidate in max", "print(max(1, None))\n", "TypeError: '>' not supported between instances of 'NoneType' and 'int'"},
		{"None incumbent in max", "print(max(None, 1))\n", "TypeError: '>' not supported between instances of 'int' and 'NoneType'"},
		{"container candidate in min", "print(min(1, [2]))\n", "TypeError: '<' not supported between instances of 'list' and 'int'"},
		{"container candidate in max", "print(max(1, [2]))\n", "TypeError: '>' not supported between instances of 'list' and 'int'"},
		{"mixed container", "print(min([1, \"a\"]))\n", "TypeError: '<' not supported between instances of 'str' and 'int'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "min_max_trap.gy", tc.src)
			py, pyCode := oracleTrap(t, path)
			if pyCode == 0 || !strings.Contains(py, tc.msg) {
				t.Fatalf("the oracle does not raise what the table claims: exit %d, %q", pyCode, py)
			}
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliRunCode(t, engine, path)
				if code == 0 {
					t.Errorf("%s exited 0 on a program the oracle dies on: stdout=%q", engine, out)
				}
				if code == 2 {
					t.Errorf("%s exited 2 (compiler bug, ADR 0166) on a program the oracle dies on:\n%s", engine, out)
				}
				combined := cliRun(t, engine, path)
				if !strings.Contains(combined, "Traceback (most recent call last):") || !strings.Contains(combined, tc.msg) {
					t.Errorf("%s did not raise %q:\n%s", engine, tc.msg, combined)
				}
			}
		})
	}
}

// TestMinMaxTrapsAreCatchableAtTheCLI pins the difference between exit 3 and exit 1: a handler reaches
// these raises on both legs, including the ones the compiler settles before the module is written.
func TestMinMaxTrapsAreCatchableAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"typeerror_is_caught",
			"try:\n    print(min(1, \"a\"))\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"maxs_typeerror_is_caught",
			"try:\n    print(max(1, None))\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"the_next_candidate_still_compares",
			"try:\n    print(min(1, \"a\"))\nexcept TypeError:\n    print(min(1, 5))\n",
			"1\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "min_max_catch.gy", tc.src)
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliRunCode(t, engine, path)
				if code != 0 || out != tc.want {
					t.Fatalf("%s printed %q exit %d, want %q (%s)", engine, out, code, tc.want, cliRun(t, engine, path))
				}
			}
		})
	}
}

// TestMinMaxScalarCandidatesAreAGustyExtension pins the one place this builtin is deliberately wider than
// CPython: **one candidate that is not a container** is a one-element collection, so `min(7)` is `7`. The
// oracle rejects the shape, so the row is the two engines against each other — the convention ADR 0246
// set for the set-subscript extension — and both must agree.
func TestMinMaxScalarCandidatesAreAGustyExtension(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"one int", "print(min(7), max(7))\n", "7 7\n"},
		{"one float", "print(min(1.5), max(1.5))\n", "1.5 1.5\n"},
		{"one negative int", "print(min(-3), max(-3))\n", "-3 -3\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "min_max_scalar.gy", tc.src)
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliRunCode(t, engine, path)
				if code != 0 || out != tc.want {
					t.Fatalf("%s printed %q exit %d, want %q (%s)", engine, out, code, tc.want, cliRun(t, engine, path))
				}
			}
		})
	}
}

// TestMinMaxDivergencesPinnedWithEachEngine records the shapes this feature does *not* make agree, with
// each engine's real answer in the row rather than a sentence about it. A text that reaches `print`
// through a user function is printed as its interned index (Gap R.38 — the callee's return type is the
// missing half, not `min`), and a bool candidate is an `int` candidate until L11.2 gives bool its own tag
// (Gap R.35). Both rows are ordinary programs: exit 2 still fails them, and each is the roadmap's row, not
// a new one.
func TestMinMaxDivergencesPinnedWithEachEngine(t *testing.T) {
	for _, tc := range []struct {
		name        string
		src         string
		interp, aot string
		gap         string
	}{
		{
			name:   "a text through a user call prints its interned index",
			src:    "def lo(a, b):\n    return min(a, b)\n\nprint(lo(\"b\", \"a\"))\n",
			interp: "a\n", aot: "1\n",
			gap: "Gap R.38",
		},
		// The row that used to sit here — “a bool candidate is an int candidate“, pinning `0 1` on both
		// legs for what ADR 0261 closed as Gap R.117 — is gone rather than re-pinned. A divergence table
		// that keeps a paid debt is a green suite asserting nothing: `print(min(True, 0), max(True, 1))`
		// answers CPython's `0 True` now, and `pkg/lang/min_max_values_test.go` pins that answer, with
		// `programs/probe_bool_chosen_by_an_operator.gy` carrying it through the oracle as parity surface.
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "min_max_divergence.gy", tc.src)
			want := map[string]string{"--interp": tc.interp, "--aot": tc.aot}
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("%s reached the compiler's own exit 2 on %s (ADR 0166):\n%s", engine, tc.gap, cliRun(t, engine, path))
				}
				if code != 0 {
					t.Fatalf("%s exited %d: %s", engine, code, cliRun(t, engine, path))
				}
				if out != want[engine] {
					t.Fatalf("%s printed %q, want the answer pinned for %s: %q", engine, out, tc.gap, want[engine])
				}
			}
		})
	}
}

// TestMinMaxShapesStillRefuseHonestlyAtTheCLI is this cycle's honesty table: the **compiled** half refuses
// where the interpreter answers, and says which half is missing. A container the program built is a heap
// address the compiler cannot read, so its elements stay unknown; candidates whose kinds straddle `int`
// and `double` only at run time need the *winner's* kind to travel, which is L11.1's tagged value word
// (Gap R.109); and a container **among** the candidates is a value the compiled fold has no ordering for.
// The zero-candidate and empty-container forms stay Gap R.37's. Every leg runs: exit 2 is the compiler's
// own class and fails the row (ADR 0166), and the interpreter's answer is pinned so the divergence cannot
// be silently "fixed" by moving the row into the parity table.
func TestMinMaxShapesStillRefuseHonestlyAtTheCLI(t *testing.T) {
	for _, tc := range []struct {
		name        string
		src         string
		why         string
		interpWhy   string // what the interpreter says instead, when it answers
		interRaises bool   // when the interpreter raises rather than answers
	}{
		{
			name:      "a container the program built",
			src:       "xs = [3, 1, 2]\nprint(min(xs))\n",
			why:       "min requires an inline list/set/dict literal",
			interpWhy: "1\n",
		},
		{
			name:      "text candidates in a variable",
			src:       "xs = [\"b\", \"a\"]\nprint(min(xs))\n",
			why:       "min requires an inline list/set/dict literal",
			interpWhy: "a\n",
		},
		{
			name:      "a text candidate on its own",
			src:       "print(min(\"a\"))\n",
			why:       "min requires an inline list/set/dict literal",
			interpWhy: "a\n",
		},
		{
			name:      "a None candidate on its own",
			src:       "print(min(None))\n",
			why:       "min requires an inline list/set/dict literal",
			interpWhy: "None\n",
		},
		{
			name:      "float and int candidates the compiler cannot see",
			src:       "a = 2.5\nb = 1\nprint(min(a, b))\n",
			why:       "min of these arguments mixes a double and an int whose winner is not known until run time",
			interpWhy: "1\n",
		},
		{
			name:        "no candidate at all",
			src:         "print(min())\n",
			why:         "min/max expect at least one argument",
			interpWhy:   "min expected at least 1 argument, got 0",
			interRaises: true,
		},
		{
			name:        "an empty container",
			src:         "print(min([]))\n",
			why:         "min requires an inline list/set/dict literal",
			interpWhy:   "min() iterable argument is empty",
			interRaises: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "min_max_refusal.gy", tc.src)

			out, code := cliRunCode(t, "--aot", path)
			if code == 2 {
				t.Fatalf("--aot reached the compiler's own exit 2 (ADR 0166):\n%s", cliRun(t, "--aot", path))
			}
			if code == 0 {
				t.Fatalf("--aot answered %q where the row says the shape is refused", out)
			}
			combined := cliRun(t, "--aot", path)
			if !strings.Contains(combined, tc.why) {
				t.Fatalf("--aot did not name the missing half %q:\n%s", tc.why, combined)
			}

			iOut, iCode := cliRunCode(t, "--interp", path)
			if iCode == 2 {
				t.Fatalf("--interp reached exit 2 (ADR 0166):\n%s", cliRun(t, "--interp", path))
			}
			iCombined := cliRun(t, "--interp", path)
			switch {
			case tc.interRaises:
				if iCode == 0 || !strings.Contains(iCombined, tc.interpWhy) || !strings.Contains(iCombined, "Traceback") {
					t.Fatalf("--interp should raise %q on %s, got exit %d %q", tc.interpWhy, tc.src, iCode, iCombined)
				}
			case iCode != 0 || !strings.Contains(iOut, tc.interpWhy):
				t.Fatalf("--interp's half of this row is %q, got exit %d %q", tc.interpWhy, iCode, iCombined)
			}
		})
	}
}

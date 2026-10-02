package lang

import (
	"strings"
	"testing"
)

// The ordering half of the tagged read (roadmap L11.1, Gap R.82).
//
// ADR 0247 gave equality the (payload, tag) pair and left orderings behind. Two things show up when
// the orderings of slot reads are measured against CPython rather than assumed:
//
//   - a container whose one kind the compiler can see orders correctly now that text orders by its
//     characters (ADR 0248, Gap R.84) — those rows are pinned as answers below, because the same
//     reads used to answer by interned index; and
//   - a container whose slots describe themselves has no answer yet: the read lowers to nothing at
//     all in a comparison context, so the program is refused. Those rows are pinned as refusals,
//     with the interpreter's answer recorded beside them, which is what makes the refusal a gap
//     (Gap R.82) rather than a limitation the program deserves.

func TestSlotOrderingAnswersInBothEngines(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			"two slots of a text-only list, ordered",
			"xs = [\"b\", \"a\"]\nprint(1 if xs[0] > xs[1] else 0)\nprint(1 if xs[0] < xs[1] else 0)\n", "1\n0\n",
		},
		{
			"a text slot ordered against a literal",
			"xs = [\"b\", \"a\"]\nprint(1 if xs[0] > \"a\" else 0)\nprint(1 if xs[1] > \"a\" else 0)\n", "1\n0\n",
		},
		{
			"a text slot ordered through an index the program computes",
			"xs = [\"a\", \"b\"]\ni = 0\nprint(1 if xs[i] < \"c\" else 0)\nprint(1 if xs[i] > \"c\" else 0)\n", "1\n0\n",
		},
		{
			"a container the program built itself orders its own text",
			"xs = []\nxs.append(\"b\")\nxs.append(\"a\")\nprint(1 if xs[0] > xs[1] else 0)\n", "1\n",
		},
		{
			"a dict entry ordered as text",
			"d = {}\nd[\"k\"] = \"b\"\nprint(1 if d[\"k\"] > \"a\" else 0)\n", "1\n",
		},
		{
			"an int slot ordered against a number",
			"xs = [1, 2, 3]\nprint(1 if xs[0] < xs[2] else 0)\nprint(1 if xs[2] <= 3 else 0)\n", "1\n1\n",
		},
		{
			"a float slot ordered against a number",
			"xs = [1.5, 2.5]\nprint(1 if xs[0] < xs[1] else 0)\nprint(1 if xs[0] >= 1.5 else 0)\n", "1\n1\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%q refused: %v", tc.src, err)
			}
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("AOT ran %q, want CPython's %q\nsrc: %s", out, tc.want, tc.src)
			}
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("interpreter printed %q, want CPython's %q\nsrc: %s", out, tc.want, tc.src)
			}
		})
	}
}

// The read that lowers to nothing in a comparison context. Each row names which half is missing; a
// refusal that says "needs a single static kind" about a program whose kinds the object knows is the
// same message for two different debts, so the two tables are kept apart on purpose.
func TestSlotOrderingRefusesWhatItCannotProve(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			"a slot of a container that mixes kinds, ordered against text",
			"xs = [1, \"a\"]\nprint(1 if xs[1] > \"a\" else 0)\n",
			"more than one kind",
		},
		{
			"two slots of a container that mixes kinds, ordered against each other",
			"xs = [\"b\", \"a\", 1]\nprint(1 if xs[0] > xs[1] else 0)\n",
			"more than one kind",
		},
		{
			"a mixed slot ordered in an if condition",
			"xs = [\"b\", 1]\nif xs[0] > \"a\":\n    print(\"big\")\nelse:\n    print(\"small\")\n",
			"more than one kind",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.src)
			if err == nil {
				out := captureStdout(t, tc.src)
				t.Fatalf("%q compiled; want a refusal (the interpreter prints %q, which is the answer\n"+
					"the compiled backend owes — roadmap L11.1, Gap R.82)\nsrc: %s", tc.src, out, tc.src)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("refused with %q, want it to mention %q", err.Error(), tc.want)
			}
			if strings.Contains(err.Error(), "LLVM ERROR") || strings.Contains(err.Error(), "verifier") {
				t.Errorf("failed as an IR problem instead of a front-end refusal: %v", err)
			}
		})
	}
}

// An ordering between kinds that do not order against each other is a TypeError in CPython. The
// interpreter raises it; the compiled leg answers these with a verdict today, which is a separate
// measured debt (roadmap Gap R.85 — the refusal the ordering door owes, beside Gap R.37's standing
// one). Pinned here for the interpreter so the two backends cannot drift apart on what traps.
func TestSlotOrderingTrapsWherePythonTraps(t *testing.T) {
	for _, tc := range []struct {
		name    string
		src     string
		class   string
		message string
	}{
		{
			"a number slot ordered against text",
			"xs = [1, \"a\"]\nprint(1 if xs[0] > \"a\" else 0)\n",
			"TypeError", "'>' not supported between instances of 'int' and 'str'",
		},
		{
			"a text slot ordered against a number",
			"xs = [\"a\", 1]\nprint(1 if xs[0] < 1 else 0)\n",
			"TypeError", "'<' not supported between instances of 'str' and 'int'",
		},
		{
			"a container slot ordered against a number",
			"xs = [[1, 2], \"a\"]\nprint(1 if xs[0] < 1 else 0)\n",
			"TypeError", "'<' not supported between instances of 'list' and 'int'",
		},
		{
			"None ordered against a number",
			"xs = [None, \"a\"]\nprint(1 if xs[0] >= 1 else 0)\n",
			"TypeError", "'>=' not supported between instances of 'NoneType' and 'int'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ee := trapRun(t, tc.src)
			if ee.ExnType != tc.class {
				t.Errorf("class = %q, want %q (msg %q)", ee.ExnType, tc.class, ee.ExnMsg)
			}
			if ee.ExnMsg != tc.message {
				t.Errorf("message =\n  %q\nwant\n  %q", ee.ExnMsg, tc.message)
			}
		})
	}
}

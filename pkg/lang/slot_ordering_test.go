package lang

import (
	"strings"
	"testing"
)

// The ordering half of the tagged read (roadmap L11.1, Gap R.82).
//
// ADR 0247 gave equality the (payload, tag) pair and left orderings behind, and ADR 0248 taught the
// static ordering to read the text instead of the interned index. What neither could answer was the
// ordering whose operands are slots whose kind only the object knows: `xs = [1, "a"]` holds a number
// and a text, so `xs[i] > "z"` is not one question but three — two numbers, two texts, or the pair
// CPython refuses — and the answer is a run-time one.
//
// The door below emits all three arms and lets the tags pick. Two rules keep it honest, and both are
// measured rather than assumed:
//
//   - an arm nobody can reach is not emitted, because a merge that names a predecessor no branch takes
//     is the module `llc` rejects — the exit class ADR 0166 reserves for the compiler's own bugs; and
//   - where the tags would have to be guessed (a container built by a loop, a slot that could hold a
//     container, numbers that could be ints or floats at once) the program is refused at the front end.
//     The refusal rows below record the answer CPython prints beside them, which is what makes each one
//     a gap rather than a limitation the program deserves.
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
		// The rows below are the ones this door exists for: the pair is a run-time question, and the
		// program prints a verdict or raises exactly what CPython raises.
		{
			"two text slots of a container that also holds numbers",
			"xs = [\"b\", \"a\", 1]\nprint(1 if xs[0] > xs[1] else 0)\nprint(1 if xs[1] > xs[0] else 0)\n", "1\n0\n",
		},
		{
			"two number slots of a container that also holds text",
			"xs = [1, \"a\", 2]\nprint(1 if xs[0] < xs[2] else 0)\nprint(1 if xs[2] <= xs[0] else 0)\n", "1\n0\n",
		},
		{
			"a slot that could be a number or a text, ordered against text",
			"xs = [1, \"a\"]\nprint(1 if xs[1] > \"a\" else 0)\nprint(1 if xs[1] < \"z\" else 0)\n", "0\n1\n",
		},
		{
			"a slot that could be a number or a text, ordered through an index the program computes",
			"xs = [1, \"a\"]\ni = 0\nprint(1 if xs[i] < 5 else 0)\n", "1\n",
		},
		{
			"the same slot, the other position in the source",
			"xs = [1, \"a\"]\nprint(1 if 5 > xs[0] else 0)\n", "1\n",
		},
		{
			"a float slot ordered against text asks the tag rather than answering by index",
			"xs = [1.5, \"a\"]\nprint(1 if xs[1] > \"a\" else 0)\n", "0\n",
		},
		{
			"an int slot ordered against a float",
			"xs = [1, \"a\"]\nprint(1 if xs[0] < 1.5 else 0)\nprint(1 if xs[0] > 1.5 else 0)\n", "1\n0\n",
		},
		{
			"a mixed slot ordered in an if condition",
			"xs = [\"b\", 1]\nif xs[0] > \"a\":\n    print(\"big\")\nelse:\n    print(\"small\")\n", "big\n",
		},
		{
			"a mixed slot ordered in a while condition",
			"xs = [1, \"a\"]\ni = 1\nwhile xs[i] < \"z\":\n    print(\"step\")\n    break\n", "step\n",
		},
		{
			"a mixed slot ordered in a boolean compound",
			"xs = [\"b\", 1]\nif xs[0] > \"a\" and xs[0] < \"c\":\n    print(\"between\")\n", "between\n",
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

// The pairs CPython refuses. The compiled program has to die with the same sentence — naming the left
// operand's type first, which is why an ordering whose sides could each be either family asks the tags
// before it writes either sentence.
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
			"the number is on the left in the source",
			"xs = [1, \"a\"]\nprint(1 if 2 > xs[1] else 0)\n",
			"TypeError", "'>' not supported between instances of 'int' and 'str'",
		},
		{
			"the text is on the left in the source",
			"xs = [\"a\", 1]\nprint(1 if \"z\" < xs[1] else 0)\n",
			"TypeError", "'<' not supported between instances of 'str' and 'int'",
		},
		{
			"a number and a text ordered through an index the program computes",
			"xs = [1, \"a\"]\ni = 0\nprint(1 if xs[i] > \"z\" else 0)\n",
			"TypeError", "'>' not supported between instances of 'int' and 'str'",
		},
		{
			"a float slot ordered against text names float, not int",
			"xs = [1.5, \"a\"]\nprint(1 if xs[0] > \"a\" else 0)\n",
			"TypeError", "'>' not supported between instances of 'float' and 'str'",
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
		{
			"a number slot ordered against text in a condition",
			"xs = [1, \"a\"]\nif xs[0] > \"a\":\n    print(\"yes\")\n",
			"TypeError", "'>' not supported between instances of 'int' and 'str'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ee := trapRun(t, tc.src)
			if ee.ExnType != tc.class {
				t.Errorf("interpreter class = %q, want %q (msg %q)", ee.ExnType, tc.class, ee.ExnMsg)
			}
			if ee.ExnMsg != tc.message {
				t.Errorf("interpreter message =\n  %q\nwant\n  %q", ee.ExnMsg, tc.message)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the oracle traps on: %v", err)
			}
			out := runIRMayTrap(t, res.IR)
			if !strings.Contains(out, "Traceback (most recent call last):") {
				t.Errorf("the compiled program printed no traceback:\n%s", out)
			}
			if !strings.Contains(out, tc.message) {
				t.Errorf("compiled message missing %q:\n%s", tc.message, out)
			}
			if strings.Contains(out, "codegen:") {
				t.Errorf("the compiled backend refused what the oracle traps on:\n%s", out)
			}
		})
	}
}

// Where the tags would have to be guessed the program is refused, in words, before any module is
// written. Each row records what CPython prints so the refusal stays a debt rather than a claim.
func TestSlotOrderingRefusesWhatItCannotProve(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			"two containers ordered against each other need the elementwise order Gap R.86 owes",
			"xs = [[1], [2]]\nprint(1 if xs[0] < xs[1] else 0)\n",
			"more than one kind",
		},
		{
			"two dicts ordered against each other",
			"xs = [{1: 2}, {1: 3}]\nprint(1 if xs[0] < xs[1] else 0)\n",
			"more than one kind",
		},
		{
			"a container of containers ordered against a literal container",
			"xs = [[1, 2]]\nprint(1 if xs[0] < [1, 3] else 0)\n",
			"more than one kind",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.src)
			if err == nil {
				out := captureStdout(t, tc.src)
				t.Fatalf("%q compiled and printed %q; want a refusal — CPython prints the answer above\n"+
					"and the compiled backend owes it too (roadmap L11.1, Gap R.82)\nsrc: %s", tc.src, out, tc.src)
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

// The bounds check is not negotiable: an ordering reads the slot, and a slot that is not there traps
// with IndexError before any comparison runs. The door reads tags and payloads in the block the
// comparison starts in, so the trap it inherits is the one the read always raised.
func TestSlotOrderingKeepsTheBoundsCheck(t *testing.T) {
	for _, tc := range []struct {
		name    string
		src     string
		message string
	}{
		{
			"an out-of-range slot ordered against text",
			"xs = [1, \"a\"]\nprint(1 if xs[7] > \"a\" else 0)\n",
			"IndexError",
		},
		{
			"an out-of-range slot ordered against a number",
			"xs = [1, \"a\"]\nprint(1 if xs[7] < 5 else 0)\n",
			"IndexError",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the oracle traps on: %v", err)
			}
			out := runIRMayTrap(t, res.IR)
			if !strings.Contains(out, tc.message) {
				t.Errorf("compiled run did not raise %s:\n%s", tc.message, out)
			}
		})
	}
}

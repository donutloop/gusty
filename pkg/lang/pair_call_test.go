package lang

// pkg/lang/pair_call_test.go — the (payload, tag) pair crosses a call, in both directions (roadmap
// Gap R.139, ADR 0273).
//
// `def twice(v): return v * 2` with `print(twice(xs[0][0]))` is CPython's `14` and the interpreter's
// `14`; the compiled leg refused it, because the parameter has one word and the argument has two — the
// callee cannot see the caller's slot. The door is two words, one each way: the argument arrives as
// payload + tag, and the answer's kind travels back in a word the callee stores beside its own return.
//
// Five tables, because the five claims fail differently:
//
//   - the answers, on both engines, for the shapes the scan accepts (an int slot, a float slot, a dict
//     slot by key, a bool slot, three levels, a keyword argument, a default, a pair-bound name);
//   - the module shape: the `define` carries the tag word, the `call` passes it, the answer's word is
//     stored on every return road — including the fall-off-the-end and unwinding ones, whose stale tag
//     would be read as the next answer's kind;
//   - the gate: a program whose call sites the scan cannot read keeps the road it always had, and the
//     pair-needing call site keeps the refusal it already had (no answer becomes a refusal, and no
//     refusal becomes a wrong number);
//   - the body half of the gate: a body that uses the parameter where the pair doors do not reach (`%`,
//     an index, a container element) is not given a tagged parameter at all — the shape the
//     `function_calls` benchmark caught the day this landed;
//   - the traps: the callee's arithmetic raises CPython's own sentence, catchable by `except TypeError`.
//
// The three-engine comparison against CPython, through the CLI, lives in
// integration/pair_call_test.go.

import (
	"strings"
	"testing"
)

const builtTwice = "def twice(v):\n    return v * 2\n\nxs = []\nxs.append([7, 8])\n"

// TestASlotReadHandedToAFunctionAnswersOnBothBackends is the row itself: the expression the print door
// answers (ADR 0265) is the same expression a call hands to a parameter.
func TestASlotReadHandedToAFunctionAnswersOnBothBackends(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"the row's own shape", builtTwice + "print(twice(xs[0][0]))\n", "14\n"},
		{
			// A parameter that receives a pair is bound as a tagged variable; a call site that hands
			// it a plain literal is a pair with a constant tag. One `define`, both call sites.
			"the same function called with a literal",
			builtTwice + "print(twice(xs[0][0]))\nprint(twice(3))\n", "14\n6\n",
		},
		{
			// The answer's kind is the answer's own: the slot says float, the callee's arithmetic
			// lifts it, and the tag that comes back is what the printer follows.
			"a float slot keeps its float",
			"def twice(v):\n    return v * 2\n\nxs = []\nxs.append([1.5, 2])\nprint(twice(xs[0][0]))\n", "3.0\n",
		},
		{
			"an int slot beside a float slot, same function",
			"def twice(v):\n    return v * 2\n\nxs = []\nxs.append([7, 8])\nxs.append([1.5, 2])\nprint(twice(xs[0][0]))\nprint(twice(xs[1][0]))\n", "14\n3.0\n",
		},
		{
			"both operands are slot reads",
			"def add(a, b):\n    return a + b\n\nxs = []\nxs.append([7, 8])\nprint(add(xs[0][0], xs[0][1]))\n", "15\n",
		},
		{
			"one operand is a slot read and the other a literal",
			"def add(a, b):\n    return a + b\n\nxs = []\nxs.append([7, 8])\nprint(add(xs[0][1], 1))\nprint(add(1, xs[0][1]))\n", "9\n9\n",
		},
		{
			"the keyword form lands on the same position",
			builtTwice + "print(twice(v=xs[0][1]))\n", "16\n",
		},
		{
			"a dict slot by key",
			"def twice(v):\n    return v * 2\n\nd = {}\nd[\"k\"] = 40\nprint(twice(d[\"k\"]))\n", "80\n",
		},
		{
			// Python's bool is a number, and the tag the caller passed is the bool's own: the pair
			// road (ADR 0233's rule at the arithmetic door) is what makes it answer 2 and not raise.
			"a bool slot is a number",
			"def twice(v):\n    return v * 2\n\nxs = []\nxs.append([True, 3])\nprint(twice(xs[0][0]))\n", "2\n",
		},
		{
			"three levels deep",
			"def twice(v):\n    return v * 2\n\nxs = []\nxs.append([[7, 8]])\nprint(twice(xs[0][0][1]))\n", "16\n",
		},
		{
			"arithmetic in the argument itself",
			builtTwice + "print(twice(xs[0][0] + 1))\n", "16\n",
		},
		{
			// A name the arithmetic door bound (ADR 0267) is the same missing word on the calling
			// side — Gap R.146's argument half, paid by the same door.
			"a pair-bound name as the argument",
			builtTwice + "n = xs[0][0] * 2\nprint(twice(n))\n", "28\n",
		},
		{
			"the answer bound to a name",
			builtTwice + "n = twice(xs[0][0])\nprint(n)\n", "14\n",
		},
		{
			// Two answers bound from one function: each carries its own kind, because the word the
			// callee wrote is read before the next call can write it again.
			"two answers bound from one function",
			"def twice(v):\n    return v * 2\n\nxs = []\nxs.append([7, 8])\nxs.append([1.5, 2])\na = twice(xs[0][0])\nb = twice(xs[1][0])\nprint(a, b)\n", "14 3.0\n",
		},
		{
			"a container the loop built",
			"def twice(v):\n    return v * 2\n\nxs = []\ni = 0\nwhile i < 2:\n    xs.append([i * 7, 8])\n    i = i + 1\nprint(twice(xs[1][0]))\n", "14\n",
		},
		{
			// The pair is passed, not bound: a body that only reads the parameter as a number needs
			// the tag and no return road at all.
			"a body that prints its parameter",
			"def show(v):\n    print(v)\n    return 0\n\nxs = []\nxs.append([7, 8])\nshow(xs[0][0])\n", "7\n",
		},
		{
			// The pair is passed and the body branches on it: both return roads store the answer's kind,
			// so the tag the caller reads is the one the arm that ran chose. CPython's own two answers
			// are pinned here (the taken arm doubles, the untaken arm hands the slot back).
			"a condition over the parameter, both arms",
			"def big(v):\n    if v > 10:\n        return v * 2\n    return v\n\nxs = []\nxs.append([7, 18])\nprint(big(xs[0][1]))\nprint(big(xs[0][0]))\n", "36\n7\n",
		},
		{
			"the answer is a pair the callee chose, printed twice",
			builtTwice + "print(twice(xs[0][0]))\nprint(twice(xs[0][0]))\n", "14\n14\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := captureStdout(t, tc.src)
			if out != tc.want {
				t.Errorf("interpreter: stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compiled leg refused a program the oracle prints (%v): %s", err, tc.src)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			if got := runIR(t, res.IR); got != tc.want {
				t.Errorf("compiled: stdout %q, want %q\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}

// TestAPairCrossingACallWritesBothWordsIs the module half: the arity, the answer's word, and the roads
// that must say None rather than leave the previous answer's kind standing in the word.
func TestAPairCrossingACallWritesBothWords(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		// want lists the fragments the module must carry; absent, the row asserts their absence.
		want []string
	}{
		{
			"the argument's two words and the answer's one",
			builtTwice + "print(twice(xs[0][0]))\n",
			[]string{
				"define i32 @gy_twice(i32 %p0, i32 %q0)",
				"@gy_twice.anst = internal global i32 0",
				"store i32 %q0, i32* %_v_tag",
			},
		},
		{
			// A body that can reach its end without a `return` answers None. A tag word left holding
			// the previous answer's kind would print a number-shaped wrong answer for that None.
			"the fall-off-the-end road says None",
			"def maybe(v):\n    if v > 0:\n        return v * 2\n    return v\n\nxs = []\nxs.append([7, 8])\nprint(maybe(xs[0][0]))\n",
			[]string{"store i32 3, i32* @gy_maybe.anst"},
		},
		{
			// A pair-aware caller reads the word after the call; a callee that raises never lets it
			// be read, so the unwinding road must name its own answer too.
			"the unwinding road says None",
			builtTwice + "print(twice(xs[0][0]))\n",
			[]string{"twice.raiseexit:"},
		},
		{
			// The gate's other face: a program the pair road has no business in does not carry the
			// door at all — no extra parameter, no tag word, no global.
			"a program with no slot read carries no door",
			"def twice(v):\n    return v * 2\n\nprint(twice(3))\n",
			nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compiled leg refused a program the oracle prints (%v): %s", err, tc.src)
			}
			for _, frag := range tc.want {
				if !strings.Contains(res.IR, frag) {
					t.Errorf("the module does not carry %q\nsrc: %s", frag, tc.src)
				}
			}
			if tc.want == nil {
				for _, frag := range []string{".anst = internal global", "%q0"} {
					if strings.Contains(res.IR, frag) {
						t.Errorf("the module carries %q, which it should not need for this program\nsrc: %s", frag, tc.src)
					}
				}
			}
		})
	}
}

// TestThePairCallKeepsTheRefusalItAlreadyHad is the no-regression half of the gate. A parameter is
// pair-carrying only where every call site can supply a pair from its own spelling; one argument the
// pass cannot read (a call, a container, a text) closes it, and the program keeps the refusal the
// ordinary road gave it. A refusal is fine; a digit that is not CPython's is not.
func TestThePairCallKeepsTheRefusalItAlreadyHad(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			// The three rows below are the gate's own words: one call site the scan cannot read as a
			// number closes the parameter, the whole function keeps the convention it had before this
			// door, and the call site that needed the pair keeps the refusal the ordinary road has always
			// given it — the same sentence, character for character, that the same program printed
			// before ADR 0273. A refusal whose wording moved would mean the road moved.
			"a text argument closes the parameter",
			"def twice(v):\n    return v * 2\n\nxs = []\nxs.append([7, 8])\nprint(twice(xs[0][0]))\nprint(twice(\"hi\"))\n",
			"is not supported in the AOT backend",
		},
		{
			"a call as the argument closes the parameter",
			"def twice(v):\n    return v * 2\n\ndef make():\n    return 7\n\nxs = []\nxs.append([7, 8])\nprint(twice(xs[0][0]))\nprint(twice(make()))\n",
			"cannot reach into",
		},
		{
			"a container argument closes the parameter",
			"def twice(v):\n    return v * 2\n\nxs = []\nxs.append([7, 8])\nprint(twice(xs[0][0]))\nprint(twice([1, 2]))\n",
			"cannot reach into",
		},
		{
			// A pair-carrying parameter beside an ordinary one: the body's `a + b` asks the shared
			// arithmetic door for a second operand, and that door has no kind to name for a parameter the
			// caller never tagged (`slotArithmeticIsProven` answers numbers it can see, not parameters).
			// The scan therefore does not open `a` at all, the argument goes down the ordinary numeric road
			// exactly as it did before this door, and the program prints the refusal it always printed.
			// Filed as roadmap Gap R.154 rather than half-answered.
			"a default parameter beside a pair-carrying one is refused, not half-paired",
			"def shift(a, b=100):\n    return a + b\n\nxs = []\nxs.append([7, 8])\nprint(shift(xs[0][1]))\nprint(shift(xs[0][1], 2))\n",
			"cannot reach into",
		},
		{
			// The pair road's own honesty: an answer is not a payload. Reading the payload alone is
			// how a float box's handle gets printed as an int, so the position that keeps one word
			// for the value refuses instead (roadmap Gap R.146's shape, one operator further out).
			"the answer used as one number is refused, not half-read",
			builtTwice + "print(twice(xs[0][0]) + 1)\n",
			"hands back the (payload, tag) pair",
		},
		{
			"an answer handed to another function is refused, not half-read",
			"def twice(v):\n    return v * 2\n\ndef show(w):\n    return w\n\nxs = []\nxs.append([7, 8])\nprint(show(twice(xs[0][0])))\n",
			"hands back the (payload, tag) pair",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("%q compiled; this shape is owed a refusal, not an answer\nsrc: %s", tc.name, tc.src)
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.want) {
				t.Errorf("refused without naming the shape: %q does not mention %q", msg, tc.want)
			}
			if strings.Contains(msg, "LLVM ERROR") || strings.Contains(msg, "verifier") || strings.Contains(msg, "panic") {
				t.Fatalf("%q failed as an IR problem instead of a front-end refusal: %v", tc.src, err)
			}
		})
	}
}

// TestAPairParameterIsOnlyGivenWhereTheBodyCanReadItBack is the body half of the gate, and the reason
// it exists: the `function_calls` benchmark body computes `(a * 31 + b * 17) % 100003`, where `%` asks
// the ordinary numeric road for one i32 and the tag is nowhere in sight. A body the doors cannot serve
// is not given a tagged parameter — it keeps the convention it has always had, so a program that
// compiled before still compiles (roadmap ADR 0273).
func TestAPairParameterIsOnlyGivenWhereTheBodyCanReadItBack(t *testing.T) {
	bodies := []struct{ name, body string }{
		{"floor division", "    return v % 3\n"},
		{"a power", "    return v ** 2\n"},
		{"a true division", "    return v / 2\n"},
		{"a subscript of the parameter", "    out = [1, 2]\n    return out[v]\n"},
		{"a container literal", "    return [v]\n"},
		{"a call over the parameter", "    return abs(v)\n"},
		{"an f-string field", "    return f\"{v}\"\n"},
		{"a text beside the parameter", "    return \"v\" + v\n"},
		{"the parameter handed to another function", "    return other(v)\n"},
	}
	for _, b := range bodies {
		t.Run(b.name, func(t *testing.T) {
			src := "def f(v):\n" + b.body + "\ndef other(w):\n    return w\n\nxs = []\nxs.append([7, 8])\nprint(f(xs[0][0]))\n"
			prog := mustParse(t, src)
			if spec := pairCallSpecs(prog)["f"]; spec != nil && len(spec.params) > 0 {
				t.Fatalf("the body cannot read a tagged parameter and was given one anyway\nsrc: %s", src)
			}
			res, err := Compile(src)
			if err == nil {
				if strings.Contains(res.IR, "%q0") || strings.Contains(res.IR, ".anst = internal global") {
					t.Fatalf("the module carries the pair door for a body that cannot read it\nsrc: %s", src)
				}
			} else if strings.Contains(err.Error(), "LLVM ERROR") || strings.Contains(err.Error(), "verifier") {
				t.Fatalf("%q failed as an IR problem instead of a refusal: %v", b.name, err)
			}
		})
	}
}

// TestACalleeRaiseLeavesThroughTheEmittedDoor is the trap run rather than the trap emitted: the
// arithmetic the callee performs over the caller's payload raises the reference's own sentence, and
// `except` at the call site reaches it (ADR 0228's rule, one frame further out).
func TestACalleeRaiseLeavesThroughTheEmittedDoor(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"a text slot under the callee's multiplication",
			"def twice(v):\n    return v * 2\n\nxs = []\nxs.append(\"hi\")\ntry:\n    print(twice(xs[0]))\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"a None slot under the callee's sum",
			"def addone(v):\n    return v + 1\n\nxs = []\nxs.append(None)\ntry:\n    print(addone(xs[0]))\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"an answer past the compiled int word",
			"def big(v):\n    return v * 1000000000\n\nxs = []\nxs.append([7, 8])\ntry:\n    print(big(xs[0][0]))\nexcept OverflowError:\n    print(\"caught\")\n",
			"caught\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compiled leg failed: %v", err)
			}
			if !strings.Contains(res.IR, "@rt_num_arith") {
				t.Errorf("the trap did not go through the door: %s", tc.src)
			}
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("compiled printed %q, want the handler's %q\nsrc: %s", out, tc.want, tc.src)
			}
		})
	}
}

// TestThePairScanIsAskedOfTheProgramNotTheEmittingOrder pins why the scan is pure: a `define` emitted
// after its first call must agree with that call about the arity, or the module verifier reports
// mismatched types at exit 2 — the contract's compiler-bug code, spent on a file layout.
func TestThePairScanIsAskedOfTheProgramNotTheEmittingOrder(t *testing.T) {
	// The call is written before the `def`. Whether a top-level program may do that at all is a
	// name-binding question this row does not open (the interpreter answers CPython's `NameError`, and
	// so does the reference); what the row owns is that the *scan*, which decides the arity, is asked of
	// the AST and not of the emitting order — so the `define` and the `call` cannot disagree, which is
	// the module-verifier failure ADR 0166 classes as a compiler bug.
	after := "print(twice(xs[0][0]))\n\ndef twice(v):\n    return v * 2\n\nxs = []\nxs.append([7, 8])\n"
	before := "def twice(v):\n    return v * 2\n\nxs = []\nxs.append([7, 8])\nprint(twice(xs[0][0]))\n"
	if out := captureStdout(t, before); out != "14\n" {
		t.Errorf("the same program with the `def` first: interpreter stdout %q, want %q", out, "14\n")
	}
	// The purity claim, asked of the scan itself: the two files differ only in where the `def` sits, and
	// a decision that read the emitting order would mark one of them and not the other.
	for _, src := range []string{before, after} {
		spec := pairCallSpecs(mustParse(t, src))["twice"]
		if spec == nil || !spec.params[0] || !spec.returnsPair {
			t.Errorf("the scan did not open the parameter for:\n%s\ngot %+v", src, spec)
		}
	}
	res, err := Compile(before)
	if err != nil {
		t.Fatalf("the def-first program was refused (%v): %s", err, before)
	}
	if !strings.Contains(res.IR, "define i32 @gy_twice(i32 %p0, i32 %q0)") {
		t.Errorf("the define did not take the pair the call passes:\n%s", res.IR)
	}
	assertNoForbiddenIR(t, before, res.IR)
	if got := runIR(t, res.IR); got != "14\n" {
		t.Errorf("compiled: stdout %q, want %q", got, "14\n")
	}
	// The def-last file is a different matter, and an honest one: a container's kind follows the
	// statement order, so the slot read above the `xs = []` has no literal to be read from and the
	// ordinary index road refuses, as it refused before this door existed. A front-end refusal is exit 1
	// (ADR 0166); an arity disagreement would have been exit 2, which is what this row is here to keep.
	if _, err := Compile(after); err == nil {
		t.Errorf("the def-last program compiled; the ordinary index road was expected to refuse it:\n%s", after)
	} else if !strings.Contains(err.Error(), "cannot reach into") && !strings.Contains(err.Error(), "inline list/dict/set literal") {
		t.Errorf("the def-last program failed for a reason that is not the container's missing kind: %v", err)
	}
}

// TestThePairSpecGateIsAskedDirectly asks the scan as a function, which is the only way to see a gate
// that lets one shape through and closes its neighbour: each row states the *decision*, so a widening
// or narrowing of the door fails a row rather than quietly changing what programs compile.
func TestThePairSpecGateIsAskedDirectly(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		pair      bool
	}{
		{
			"a slot read under print",
			builtTwice + "print(twice(xs[0][0]))\n", true,
		},
		{
			// A loop variable is answered by ADR 0185's loop-element tag, not by the pair door; the
			// day this file claimed it as a pair-bound name, the benchmark stopped compiling.
			"a loop variable over a built container is not a pair read",
			"def mix(a, b):\n    return (a * 31 + b * 17) % 100003\n\nxs = []\nxs.append([1, 2])\ns = 0\nfor x in xs:\n    s = mix(s, x)\nprint(s)\n", false,
		},
		{
			"a literal container the compiler still reads is not a pair read",
			"def twice(v):\n    return v * 2\n\nxs = [7, 8]\nprint(twice(xs[0]))\n", false,
		},
		{
			"a plain number argument needs no pair",
			builtTwice + "print(twice(3))\n", false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prog := mustParse(t, tc.src)
			specs := pairCallSpecs(prog)
			spec := specs["twice"]
			if spec == nil {
				spec = specs["mix"]
			}
			got := spec != nil && len(spec.params) > 0
			if got != tc.pair {
				t.Errorf("the scan answered pair=%v, want pair=%v\nsrc: %s", got, tc.pair, tc.src)
			}
		})
	}
}

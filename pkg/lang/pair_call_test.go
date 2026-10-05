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
			// The argument the ordinary road truncated: `twice(2.5)` printed 4 at exit 0, because the
			// parameter took one i32 for a value that has two words. The kind the argument arrived in is the
			// kind the answer comes back in — which is what the two lines below pin together (roadmap
			// L11.6, Gap P.1's argument half, ADR 0276).
			"a double is handed to a function, an int to the same one",
			"def twice(v):\n    return v * 2\n\nprint(twice(2.5))\nprint(twice(2))\nprint(twice(True))\n", "5.0\n4\n2\n",
		},
		{
			// The parameter the call sites hand nothing but integers needs no pair of its own: the
			// arithmetic door tags the literal it can see and reads `h` as the plain number it is.
			"a parameter beside a provably-integer one",
			"def area(w, h):\n    return w * h\n\nprint(area(2.5, 2))\nprint(area(3, 4))\n", "5.0\n12\n",
		},
		{
			// A default is an argument the caller did not have to write, and carries the same pair: no
			// call site mentions `times` at all, so an arity-only scan would never open it.
			"a default is an argument too",
			"def greet(name, times=1.5):\n    return times\n\nprint(greet(\"a\"))\nprint(greet(\"a\", 2))\n", "1.5\n2\n",
		},
		{
			"a slot of a literal list that holds a double",
			"def twice(v):\n    return v * 2\n\nys = [1, 2.5]\nprint(twice(ys[1]))\nprint(twice(ys[0]))\n", "5.0\n2\n",
		},
		{
			// Gap R.154's shape, answered: the pair-carrying parameter and the ordinary one share a body,
			// and `a + b` needs a kind for both — `b` is proven by the two call sites that write it.
			"a pair parameter beside an ordinary parameter",
			"def shift(a, b=100):\n    return a + b\n\nxs = []\nxs.append([7, 8])\nprint(shift(xs[0][1]))\nprint(shift(xs[0][1], 2))\n",
			"108\n10\n",
		},
		{
			// The answer direction: the callee's own answer is a pair, and the caller hands it on in the
			// word its convention already promised (roadmap L11.1, Gap R.139's caller half).
			"a pair answer handed through another function",
			"def g(y):\n    return y * 2\n\ndef f(x):\n    x = x + 1.5\n    return g(x)\n\nprint(f(1.0))\n", "5.0\n",
		},
		{
			// Gap R.161: the argument is handed on through a second function's parameter. The pair that
			// existed one frame earlier has to survive both boundaries, or the number is truncated twice
			// over — `print(outer(2.5))` printed 4 where CPython prints 5.0 (roadmap L11.6, ADR 0277).
			"a double forwarded through another function's parameter",
			"def twice(v):\n    return v * 2\n\ndef outer(x):\n    return twice(x)\n\nprint(outer(2.5))\nprint(outer(3))\n",
			"5.0\n6\n",
		},
		{
			// Two boundaries deep: the middle frame forwards too, and each of the three frames has to
			// agree on the arity without anyone asking the emission order.
			"the same forwarding, two frames deep",
			"def twice(v):\n    return v * 2\n\ndef middle(x):\n    return twice(x)\n\ndef outer(x):\n    return middle(x)\n\nprint(outer(2.5))\n",
			"5.0\n",
		},
		{
			// The forwarded parameter and the provably-integer one share a body: only the name the call
			// sites hand doubles to carries a tag.
			"a forwarded parameter beside an ordinary one",
			"def twice(v):\n    return v * 2\n\ndef outer(a, b):\n    return twice(b)\n\nprint(outer(1, 2.5))\nprint(outer(1, 2))\n",
			"5.0\n4\n",
		},
		{
			// Forwarding and arithmetic in the same callee: `a` arrives as a pair from the caller above,
			// `b` is the literal the body writes, and the door needs a kind for both operands.
			"a forwarded parameter in arithmetic with a literal",
			"def add(a, b):\n    return a + b\n\ndef outer(x):\n    return add(x, 1)\n\nprint(outer(2.5))\nprint(outer(2))\n",
			"3.5\n3\n",
		},
		{
			// A slot read handed to a function which hands it on again: the row ADR 0273 pinned as a
			// refusal, answered by the call graph rather than by the body's own text (Gap R.161).
			"a slot read handed on through a second function",
			"def f(v):\n    return other(v)\n\ndef other(w):\n    return w\n\nxs = []\nxs.append([7, 8])\nprint(f(xs[0][0]))\n",
			"7\n",
		},
		{
			"a float slot read handed on, and the same function called with an int",
			"def f(v):\n    return other(v)\n\ndef other(w):\n    return w * 2\n\nxs = []\nxs.append([1.5, 8])\nprint(f(xs[0][0]))\nprint(f(3))\n",
			"3.0\n6\n",
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
	bodies := []struct{ name, body, other string }{
		{"floor division", "    return v % 3\n", ""},
		{"a power", "    return v ** 2\n", ""},
		{"a true division", "    return v / 2\n", ""},
		{"a subscript of the parameter", "    out = [1, 2]\n    return out[v]\n", ""},
		{"a container literal", "    return [v]\n", ""},
		{"a call over the parameter", "    return abs(v)\n", ""},
		{"an f-string field", "    return f\"{v}\"\n", ""},
		{"a text beside the parameter", "    return \"v\" + v\n", ""},
		// "the parameter handed to another function" left this table in ADR 0277: with the call graph in
		// the scan, `return other(v)` is served — `other`'s own parameter is marked from the same evidence,
		// the callee answers a pair, and the caller hands it on. It answers on both engines in
		// TestASlotReadHandedToAFunctionAnswersOnBothBackends, and its arity in either declaration order is
		// pinned by TestTheForwardedPairIsSettledWhicheverOrderTheDefsAreWritten. What the gate still
		// declines is the two rows below: a callee that cannot carry the pair closes the caller with it,
		// because a tag word no callee reads is a pair that would be half-read one frame down.
		{"the parameter handed to a callee that floors it", "    return other(v)\n", "    return w % 3\n"},
		{"the parameter handed to a callee that returns a text", "    return other(v)\n", "    return str(w)\n"},
	}
	for _, b := range bodies {
		t.Run(b.name, func(t *testing.T) {
			otherBody := b.other
			if otherBody == "" {
				otherBody = "    return w\n"
			}
			src := "def f(v):\n" + b.body + "\ndef other(w):\n" + otherBody + "\nxs = []\nxs.append([7, 8])\nprint(f(xs[0][0]))\n"
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

// TestTheForwardedPairIsSettledWhicheverOrderTheDefsAreWritten is Gap R.161's module half, and the reason
// the answer direction has to be a scan answer at all. `def f(v): return other(v)` written *above*
// `def other(w): return w * 2` asks whether `other` hands back a pair while other's own body is still
// ungenerated; asked from the emission order, that question answered "no", and the caller was left storing
// whatever the tag word happened to hold — the program refused with ADR 0273's sentence for two lines whose
// text says nothing about order. Both orders are pinned below, with the tag word's definition beside them:
// the definition is written where the callee's `define` is emitted and read where the caller calls, so the
// two cannot share the one-shot guard that writes it (roadmap L11.1, ADR 0277).
func TestTheForwardedPairIsSettledWhicheverOrderTheDefsAreWritten(t *testing.T) {
	const fDef = "def f(v):\n    return other(v)\n\n"
	const otherDef = "def other(w):\n    return w * 2\n\n"
	const tail = "xs = []\nxs.append([1.5, 8])\nprint(f(xs[0][0]))\nprint(f(3))\n"
	for _, order := range []struct{ name, src string }{
		{"the caller is written first", fDef + otherDef + tail},
		{"the callee is written first", otherDef + fDef + tail},
	} {
		t.Run(order.name, func(t *testing.T) {
			specs := pairCallSpecs(mustParse(t, order.src))
			for _, fn := range []string{"f", "other"} {
				if specs[fn] == nil || !specs[fn].returnsPair {
					t.Fatalf("%s is not marked as a pair-carrying, pair-answering function\nsrc: %s", fn, order.src)
				}
			}
			const want = "3.0\n6\n"
			if out := captureStdout(t, order.src); out != want {
				t.Errorf("interpreter: stdout %q, want %q\nsrc: %s", out, want, order.src)
			}
			res, err := Compile(order.src)
			if err != nil {
				t.Fatalf("compiled leg refused a program the oracle prints (%v): %s", err, order.src)
			}
			assertNoForbiddenIR(t, order.src, res.IR)
			for _, fn := range []string{"gy_f", "gy_other"} {
				line := defineLine(res.IR, fn)
				if p, q := wordParams(line); p != 1 || q != 1 {
					t.Errorf("%s does not take one payload word and one tag word: %s", fn, line)
				}
			}
			// One tag word per pair-answering function, written by its own body and read by its caller.
			if n := strings.Count(res.IR, ".anst = internal global"); n != 2 {
				t.Errorf("module declares %d tag words, want 2\nsrc: %s", n, order.src)
			}
			for _, fn := range []string{"gy_f", "gy_other"} {
				slot := "@" + fn + ".anst"
				if !strings.Contains(res.IR, "store i32") || strings.Count(res.IR, slot) < 2 {
					t.Errorf("%s's tag word is not both stored and read: %s", fn, slot)
					continue
				}
				stored, loaded := false, false
				for _, ln := range strings.Split(res.IR, "\n") {
					if strings.Contains(ln, slot) && strings.HasPrefix(strings.TrimSpace(ln), "store") {
						stored = true
					}
					if strings.Contains(ln, slot) && strings.Contains(ln, "load i32") {
						loaded = true
					}
				}
				if !stored || !loaded {
					t.Errorf("%s's tag word is stored=%v read=%v; the answer's kind has to travel both ways", fn, stored, loaded)
				}
			}
			if got := runIR(t, res.IR); got != want {
				t.Errorf("compiled: stdout %q, want %q\nsrc: %s", got, want, order.src)
			}
		})
	}
}

// TestAForwardingCallerIsClosedByACalleeThatCannotCarryThePair is the prune, and the reason a forwarding edge
// is a promise rather than a permission: `def f(v): return other(v)` is only worth two words while `other`
// can carry the pair it is handed. Close the callee — its body floors the value, or renders it a text — and
// the caller's mark has to go with it, because a tag word the callee never reads leaves the pair to be
// half-read one frame down, which is the wrong number this file exists to remove.
func TestAForwardingCallerIsClosedByACalleeThatCannotCarryThePair(t *testing.T) {
	for _, tc := range []struct{ name, otherBody string }{
		{"the callee floors it", "    return w % 3\n"},
		{"the callee renders it a text", "    return str(w)\n"},
		{"the callee indexes with it", "    out = [1, 2]\n    return out[w]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "def f(v):\n    return other(v)\n\ndef other(w):\n" + tc.otherBody + "\nprint(f(2.5))\n"
			specs := pairCallSpecs(mustParse(t, src))
			for _, fn := range []string{"f", "other"} {
				if specs[fn] != nil && len(specs[fn].params) > 0 {
					t.Fatalf("%s was given a tagged parameter although the callee cannot carry the pair\nsrc: %s", fn, src)
				}
			}
			res, err := Compile(src)
			if err != nil {
				if strings.Contains(err.Error(), "LLVM ERROR") || strings.Contains(err.Error(), "verifier") {
					t.Fatalf("%s failed as an IR problem instead of keeping its old road: %v", tc.name, err)
				}
				return
			}
			if strings.Contains(res.IR, ".anst = internal global") {
				t.Fatalf("the module carries the pair door for a chain that cannot carry it\nsrc: %s", src)
			}
		})
	}
}

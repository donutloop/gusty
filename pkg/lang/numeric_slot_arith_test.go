package lang

// pkg/lang/numeric_slot_arith_test.go — the number half of roadmap L11.1's last clause (ADR 0265).
//
// Three things are pinned here, and they are three different failures:
//
//   - The *gate*: which programs the door opens for. The gate is a plain Go question about the AST, so
//     it is answered directly rather than inferred from emitted IR — a door that opened for a container
//     holding text would print a TypeError where CPython returns a joined string, and no amount of
//     "the module verified" would catch that.
//   - The *module*: that a proven program emits the runtime call and the unproven one does not, so a
//     program that never asks the question is not saddled with a snprintf-ing raise formatter (the
//     ADR 0173/0192 rule: a referenced internal function that was never emitted is the module llc
//     rejects).
//   - The *refusals*: what the door still declines, named rather than crashed.
//
// The printed answers — the actual comparison against CPython, both engines — live in
// integration/numeric_slot_arith_test.go, where the reference gets to vote.

import (
	"strings"
	"testing"
)

func slotIR(t *testing.T, src string) string {
	t.Helper()
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("compiled leg failed for %q: %v", src, err)
	}
	assertNoForbiddenIR(t, src, res.IR)
	return res.IR
}

func slotRefusal(t *testing.T, src, want string) {
	t.Helper()
	_, err := Compile(src)
	if err == nil {
		t.Fatalf("%q compiled; wanted a refusal mentioning %q", src, want)
	}
	msg := err.Error()
	if !strings.Contains(msg, want) {
		t.Fatalf("%q refused with %q, want it to mention %q", src, msg, want)
	}
	if strings.Contains(msg, "LLVM ERROR") || strings.Contains(msg, "verifier") {
		t.Fatalf("%q failed as an IR problem instead of a front-end refusal: %v", src, err)
	}
}

// TestNumericSlotChainGate asks the gate directly. Every row is a program whose answer the reference is
// known to give; `open` says the compiler is willing to codegen the arithmetic at run time.
func TestNumericSlotChainGate(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		open bool
	}{
		{"the row's own shape", "xs = []\nxs.append([7, 8])\nprint(xs[0][0] + 1)\n", true},
		{"the float slot", "xs = []\nxs.append([7.5, 8])\nprint(xs[0][0] * 2)\n", true},
		{"the negation", "xs = []\nxs.append([7, 8])\nprint(-xs[0][0])\n", true},
		{"a bool is a number", "xs = []\nxs.append([True, 2])\nprint(xs[0][0] + 1)\n", true},
		{"a dict value", "d = {}\nd[\"k\"] = 40\nprint(d[\"k\"] + 2)\n", true},
		{"both operands are slots", "xs = []\nxs.append([7, 8])\nprint(xs[0][0] + xs[0][1])\n", true},
		{
			// CPython answers this with a joined string, and this backend cannot build a string from a
			// slot (Gap R.82), so the door has to stay shut.
			"text stored in a container",
			"xs = []\nxs.append(\"hi\")\nprint(xs[0] + 1)\n", false,
		},
		{
			// A list beside a list is answered by the reference too — repetition — and the notebook has
			// nothing to say about what the appended call returned.
			"a value with no spelling",
			"def f():\n    return [7, 8]\n\nxs = []\nxs.append(f())\nprint(xs[0][0] + 1)\n", false,
		},
		{
			// The same list, one text somewhere: coarse on purpose, and coarse means refusing more.
			"text anywhere in any container",
			"xs = []\nxs.append([7, 8])\nys = []\nys.append(\"word\")\nprint(xs[0][0] + 1)\n", false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := parseProgram(tc.src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			var nm *Name
			var depth int
			for _, st := range prog.Stmts {
				if es, ok := st.(*ExprStmt); ok {
					if call, ok := es.Expr.(*Call); ok {
						for _, a := range call.Args {
							nm, depth = chainDepth(a)
						}
					}
				}
			}
			if nm == nil {
				t.Skip("row has no print(...) argument to walk")
			}
			c := computeNumericSlotChains(prog)
			if got := c.numeric(nm.Value, depth); got != tc.open {
				t.Errorf("gate on %s at depth %d = %v, want %v (levels %v, ok %v)",
					nm.Value, depth, got, tc.open, c.depth[nm.Value], c.ok[nm.Value])
			}
		})
	}
}

// TestAnArithmeticAnswerBoundToANameCarriesItsTag is the Gap R.138 half (ADR 0266): the arithmetic the
// print position answered, bound to a name one statement earlier. Before this commit the print position
// answered `xs[0][0] * 2` and the binding refused the very same expression, because the pair road was
// opened at the printer only. The binding is where the pair belongs: the name stores a payload *and* the
// tag its own expression produced, in the tagged-variable shape the print door, `arithOperandPair` and
// the comparisons already read — so `runIR` here is the whole claim, module verified and answer printed.
func TestAnArithmeticAnswerBoundToANameCarriesItsTag(t *testing.T) {
	for _, tc := range []struct{ name, src, slot, want string }{
		{
			"the row's own shape",
			"xs = []\nxs.append([7, 8])\nn = xs[0][0] * 2\nprint(n)\n",
			"i32* %_n_tag", "14\n",
		},
		{
			// The answer's kind is the answer's own: nothing was rewritten to a double to get `15.0`, the
			// tag the target wrote is what the printer reads.
			"the float answer keeps the float through the name",
			"xs = []\nxs.append([7.5, 8])\nm = xs[0][0] * 2\nprint(m)\n",
			"i32* %_m_tag", "15.0\n",
		},
		{
			"the negation bound",
			"xs = []\nxs.append([7, 8])\nk = -xs[0][0]\nprint(k)\n",
			"i32* %_k_tag", "-7\n",
		},
		{
			"two slots summed into a name",
			"xs = []\nxs.append([7, 8])\nt = xs[0][0] + xs[0][1]\nprint(t)\n",
			"i32* %_t_tag", "15\n",
		},
		{
			"a dict value bound",
			"d = {}\nd[\"k\"] = 40\nn = d[\"k\"] + 2\nprint(n)\n",
			"i32* %_n_tag", "42\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("the binding refused a program the reference answers: %v", err)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			for _, want := range []string{"@rt_num_arith", tc.slot, tc.slot + "\n", "@rt_print_mixed_value"} {
				if !strings.Contains(res.IR, want) {
					t.Errorf("the pair did not reach the name: missing %s", want)
				}
			}
			if !strings.Contains(res.IR, "store i32 %") || !strings.Contains(res.IR, ", "+tc.slot) {
				t.Errorf("the tag slot was never written: want a store into %s", tc.slot)
			}
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("printed %q, want the reference's %q", out, tc.want)
			}
		})
	}
}

// TestAnArithmeticBindingKeepsTheGateThePrintRoadUses is the cost half of the new binding: a program that
// binds an ordinary sum of literals does not pick up the pair road, and one whose slots are not proven
// numeric still refuses rather than answering — the gate is shared, so the binding cannot open a door the
// print position had to keep shut (ADR 0265's gate, ADR 0266's new caller).
func TestAnArithmeticBindingKeepsTheGateThePrintRoadUses(t *testing.T) {
	ir := slotIR(t, "n = 6 * 7\nprint(n)\n")
	for _, absent := range []string{"@rt_num_arith", "@rt_lift_num"} {
		if strings.Contains(ir, absent) {
			t.Errorf("a plain literal binding carries %s — the pair road took a program the ordinary road answers", absent)
		}
	}
	slotRefusal(t, "xs = []\nxs.append([7, 8])\nys = []\nys.append(\"word\")\nn = xs[0][0] + 1\nprint(n)\n", "cannot reach into")
	slotRefusal(t, "def f():\n    return [7, 8]\n\nxs = []\nxs.append(f())\nn = xs[0][0] + 1\nprint(n)\n", "cannot reach into")
}

// TestAnArithmeticBindingToADoubleSlotIsLeftAlone pins the one store the pair road must not take: a name
// already settled as a double has a `double* %_n` slot, and storing the pair's i32 payload into it is the
// module `llc` rejects — ADR 0166's own class. The road declines that binding and the program keeps the
// refusal it always had; the word a double travels in is Gap R.98's, not this binding's.
func TestAnArithmeticBindingToADoubleSlotIsLeftAlone(t *testing.T) {
	slotRefusal(t, "xs = []\nxs.append([7, 8])\nn = 1.5\nn = xs[0][0] * 2\nprint(n)\n", "cannot reach into")
	// A *different* name keeping a double slot is no reason to refuse: the double and the pair coexist in
	// one module, each in its own word.
	const beside = "xs = []\nxs.append([7, 8])\nrate = 1.5\nn = xs[0][0] * 2\nprint(rate)\nprint(n)\n"
	res, err := Compile(beside)
	if err != nil {
		t.Fatalf("a double beside a pair refused: %v", err)
	}
	assertNoForbiddenIR(t, beside, res.IR)
	if out := runIR(t, res.IR); out != "1.5\n14\n" {
		t.Errorf("printed %q, want %q", out, "1.5\n14\n")
	}
}

func TestNumericSlotArithEmitsTheRuntimeDoor(t *testing.T) {
	cases := []struct{ name, src string }{
		{"addition", "xs = []\nxs.append([7, 8])\nprint(xs[0][0] + 1)\n"},
		{"multiplication keeps the float", "xs = []\nxs.append([7.5, 8])\nprint(xs[0][0] * 2)\n"},
		{"negation", "xs = []\nxs.append([7, 8])\nprint(-xs[0][0])\n"},
		{"subtraction", "xs = []\nxs.append([7, 8])\nprint(xs[0][1] - 3)\n"},
		{"a dict value", "d = {}\nd[\"k\"] = 40\nprint(d[\"k\"] + 2)\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ir := slotIR(t, tc.src)
			for _, want := range []string{"@rt_num_arith", "@rt_lift_num", "@rt_print_mixed_value"} {
				if !strings.Contains(ir, want) {
					t.Errorf("%q missing %s", tc.name, want)
				}
			}

		})
	}
}

// TestNumericSlotArithDoesNotRideAlongInOtherModules is the cost half: a program that never asks the
// question must not carry the door, and a program that never touches a heap slot must not carry the heap
// either — the same rule the float and heap blocks are emitted under (ADR 0173, ADR 0192).
func TestNumericSlotArithDoesNotRideAlongInOtherModules(t *testing.T) {
	ir := slotIR(t, "print(2 + 3)\nprint(1.5 * 2.0)\n")
	for _, absent := range []string{"@rt_num_arith", "@rt_num_bad", "@rt_lift_num"} {
		if strings.Contains(ir, absent) {
			t.Errorf("a plain literal program carries %s", absent)
		}
	}
}

// TestNumericSlotArithRefusalsNameTheMissingHalf pins what the door still declines. Each is a program
// the reference answers, and the answer this backend gives is a compile-time refusal that says which
// half is missing — never an exit 2, and never a verdict (ADR 0166).
func TestNumericSlotArithRefusalsNameTheMissingHalf(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"text beside a slot under +",
			"xs = []\nxs.append(\"hi\")\nprint(xs[0] + 1)\n",
			"concatenating a string with a value that is not a string",
		},
		{
			// A loop variable takes its kind per iteration; that is the tagged value word's job, not this
			// read's (Gap R.125's loop half, still open).
			"a loop variable over a mixed list",
			"xs = [1, \"a\"]\nfor x in xs:\n    print(x + 1)\n",
			"needs a tagged value",
		},
		{
			// `/` has had its own door since ADR 0253 and answers a float whatever arrives; the pair road
			// must not steal it.
			"true division keeps its own door",
			"xs = []\nxs.append([7, 8])\nprint(xs[0][0] / 2)\n",
			"",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.want == "" {
				ir := slotIR(t, tc.src)
				if !strings.Contains(ir, "@rt_print_mixed_value") {
					t.Errorf("the division door disappeared: %s", tc.src)
				}
				if strings.Contains(ir, "@rt_num_arith") {
					t.Errorf("`/` was rerouted through the tagged arithmetic door")
				}
				return
			}
			slotRefusal(t, tc.src, tc.want)
		})
	}
}

// TestNumericSlotArithRaisesThroughTheEmittedDoor is the IR half of the raise contract: the helper only
// fills a buffer and returns a status, and the *emitted code* stores the message and branches, which is
// what makes `except TypeError:` reach it (ADR 0228). A runtime helper that raised itself would be
// unreachable to the program.
func TestNumericSlotArithRaisesThroughTheEmittedDoor(t *testing.T) {
	ir := slotIR(t, "xs = []\nxs.append(\"hi\")\nprint(-xs[0])\n")
	for _, want := range []string{"@rt_num_arith", "@rt_num_bad", "store i32 1, i32* @exn_flag", "i32* @exn_code", "@exn_msg"} {
		if !strings.Contains(ir, want) {
			t.Errorf("the raise does not leave through the emitted door: missing %s", want)
		}
	}
	if strings.Contains(ir, "call void @rt_raise(") && !strings.Contains(ir, "@rt_num_bad") {
		t.Error("the arithmetic helper raised for itself instead of returning a status")
	}
}

// TestNumericSlotArithCatchesItsOwnTrap is the raise run rather than merely emitted: the trap is a
// program-visible raise, so `except TypeError:` and `except OverflowError:` reach it in the module that
// is written, and the program carries on. runIR is the same executor the memory tests use, so this row
// goes all the way through `llc` and back rather than stopping at a string match.
func TestNumericSlotArithCatchesItsOwnTrap(t *testing.T) {
	for _, src := range []string{
		"xs = []\nxs.append([7, 8])\ntry:\n    print(xs[0][0] * 1000000000)\nexcept OverflowError:\n    print(\"caught\")\n",
		"xs = []\nxs.append(\"hi\")\ntry:\n    print(-xs[0])\nexcept TypeError:\n    print(\"caught\")\n",
	} {
		res, err := Compile(src)
		if err != nil {
			t.Fatalf("compiled leg failed: %v", err)
		}
		for _, want := range []string{"@rt_num_arith"} {
			if !strings.Contains(res.IR, want) {
				t.Errorf("%q: the trap did not go through the door: missing %s", src[:24], want)
			}
		}
		if out := runIR(t, res.IR); out != "caught\n" {
			t.Errorf("%q printed %q, want the handler's %q", src[:24], out, "caught\n")
		}
	}
}

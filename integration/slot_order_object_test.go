package integration

import (
	"strings"
	"testing"
)

// slot_order_object_test.go — the ordering comparison of a slot **no literal describes**, at the CLI
// and against CPython (roadmap L11.1, Gap R.93; ADR 0252).
//
// The sibling unit file pins the same claim through the record; this one runs the shipped binary
// on both legs and the oracle on the same source, so a door that only works inside the compiler's own
// test harness cannot pass. The shapes are the ones Gap R.93 measured as a *verdict for a TypeError*:
//
//	xs = []
//	for i in [1, 2]:
//	    xs.append(i)
//	print(1 if xs[0] > "a" else 0)   # CPython raises · --aot raises · --aot printed 1
//
// and the ones ADR 0251 left half-answered:
//
//	xs = []
//	xs.append([3, "a"])
//	print(1 if xs[0][0] > 1 else 0)  # 1 everywhere; the compiled leg used to exit 1
//
// An ordering is the comparison whose *verdict* is a bool whatever the operands turn out to be, which
// is why the object can be asked and the module can still be written: two numbers become doubles and
// compare, two texts go to strcmp, and every other pair raises CPython's sentence with the slot's real
// kind named — a chain over the tags the writers can leave beside a payload. A row that exits 2 (the
// contract's "the compiler is broken" class) fails this file, in every table.

func TestSlotOrderOfAnUnliteralisedSlotMatchesCPython(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"appended_int_above_number", "xs = []\nxs.append(3)\nprint(1 if xs[0] > 1 else 0)\n", "1\n"},
		{"appended_int_below_number", "xs = []\nxs.append(3)\nprint(1 if xs[0] < 5 else 0)\n", "1\n"},
		{"appended_int_against_float", "xs = []\nxs.append(3)\nprint(1 if xs[0] > 1.5 else 0)\n", "1\n"},
		{"appended_float_against_int", "xs = []\nxs.append(1.5)\nprint(1 if xs[0] > 1 else 0)\n", "1\n"},
		{"appended_float_against_itself", "xs = []\nxs.append(1.5)\nprint(1 if xs[0] >= 1.5 else 0)\nprint(1 if xs[0] > 1.5 else 0)\n", "1\n0\n"},
		{"appended_negative", "xs = []\nxs.append(-3)\nprint(1 if xs[0] < 0 else 0)\n", "1\n"},
		{"le_and_ge", "xs = []\nxs.append(2)\nprint(1 if xs[0] <= 2 else 0)\nprint(1 if xs[0] >= 3 else 0)\n", "1\n0\n"},
		{
			"int_slot_and_text_slot_in_one_container",
			"xs = []\nxs.append(3)\nxs.append(\"a\")\nprint(1 if xs[0] > 1 else 0)\nprint(1 if xs[1] > \"A\" else 0)\n",
			"1\n1\n",
		},
		{
			"three_kinds_in_one_container",
			"xs = []\nxs.append(3)\nxs.append(\"b\")\nxs.append(1.5)\nprint(1 if xs[0] > 2 else 0)\nprint(1 if xs[1] > \"a\" else 0)\nprint(1 if xs[2] < 2 else 0)\n",
			"1\n1\n1\n",
		},
		{
			// The text ordering is the one ADR 0248 paid for literals; here it is reached through the
			// tag, so the verdict cannot depend on which spelling the program mentioned first.
			"appended_texts_order_as_text",
			"xs = []\nxs.append(\"b\")\nxs.append(\"a\")\nprint(1 if xs[0] > \"a\" else 0)\nprint(1 if xs[1] > \"a\" else 0)\n",
			"1\n0\n",
		},
		{"loop_built_against_number", "xs = []\nfor i in [1, 2]:\n    xs.append(i)\nprint(1 if xs[0] > 1 else 0)\n", "0\n"},
		{"loop_built_texts", "xs = []\nfor s in [\"b\", \"a\"]:\n    xs.append(s)\nprint(1 if xs[0] > \"a\" else 0)\nprint(1 if xs[1] > \"a\" else 0)\n", "1\n0\n"},
		{"dict_slot_against_number", "d = {}\nd[\"a\"] = 5\nprint(1 if d[\"a\"] > 4 else 0)\n", "1\n"},
		{"dict_slot_against_float", "d = {}\nd[\"a\"] = 1.5\nprint(1 if d[\"a\"] < 2 else 0)\n", "1\n"},
		{"nested_slot_against_number", "xs = []\nxs.append([3, \"a\"])\nprint(1 if xs[0][0] > 1 else 0)\n", "1\n"},
		{"nested_text_slot", "xs = []\nxs.append([3, \"a\"])\nprint(1 if xs[0][1] > \"A\" else 0)\n", "1\n"},
		{"nested_float_slot", "xs = []\nxs.append([1.5, 2])\nprint(1 if xs[0][0] >= 1.5 else 0)\n", "1\n"},
		{"nested_dict_slot_by_key", "xs = []\nxs.append({\"k\": 3})\nprint(1 if xs[0][\"k\"] > 1 else 0)\n", "1\n"},
		{"dict_of_lists_read_twice", "d = {}\nd[\"a\"] = [1, 9]\nprint(1 if d[\"a\"][1] > 5 else 0)\n", "1\n"},
		{"in_a_condition", "xs = []\nxs.append(3)\nif xs[0] > 2:\n    print(\"big\")\nelse:\n    print(\"small\")\n", "big\n"},
		{"in_a_while_head", "xs = []\nxs.append(3)\ni = 0\nwhile xs[0] > i:\n    print(i)\n    i = i + 1\n", "0\n1\n2\n"},
		{"in_an_and_compound", "xs = []\nxs.append(3)\nprint(1 if (xs[0] > 1 and xs[0] < 5) else 0)\n", "1\n"},
		// The settled side may be a variable the compiler can name a kind for — the object is asked about
		// one side only, which is the shape the gate allows (ADR 0252).
		{"appended_int_against_int_variable", "xs = []\nxs.append(3)\ni = 2\nprint(1 if xs[0] > i else 0)\nprint(1 if xs[0] < i else 0)\n", "1\n0\n"},
		{"appended_float_against_float_variable", "xs = []\nxs.append(1.5)\nf = 2.0\nprint(1 if xs[0] < f else 0)\nprint(1 if f > xs[0] else 0)\n", "1\n1\n"},
		{"appended_text_against_text_variable", "xs = []\nxs.append(\"b\")\nt = \"a\"\nprint(1 if xs[0] > t else 0)\nprint(1 if t > xs[0] else 0)\n", "1\n0\n"},
		// What the older doors owned, pinned so this one cannot take it over.
		{"literal_container_still_orders", "xs = [3, \"a\"]\nprint(1 if xs[0] > 1 else 0)\nprint(1 if xs[1] > \"A\" else 0)\n", "1\n1\n"},
		{"literal_nested_read_still_orders", "m = [[1, 2]]\nprint(1 if m[0][1] > 1 else 0)\n", "1\n"},
		{"literal_dict_nested_read_still_orders", "d = {\"a\": [1, 2]}\nprint(1 if d[\"a\"][1] > 3 else 0)\n", "0\n"},
		{"comprehension_built_container_orders", "xs = [x for x in [3, 4]]\nprint(1 if xs[0] > 2 else 0)\n", "1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "slot_order.gy", tc.src)
			if py, ok := cpythonOut(t, path); ok && py != tc.want {
				t.Fatalf("the expectation is not CPython's: got %q want %q", py, tc.want)
			}
			for _, engine := range cliEngines {
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

// TestSlotOrderOfAnUnliteralisedSlotTrapsLikeCPython is the half that Gap R.93 measured as a printed
// verdict: the program crashes in the oracle, so both compiled legs have to crash too, with the same
// class and the same sentence — and the slot's own kind in that sentence, whichever kind it turned out
// to be. A refusal (exit 1, `codegen: …`) fails the row: it would be the compiler answering a question
// about the program's data that only the program's data can answer.
func TestSlotOrderOfAnUnliteralisedSlotTrapsLikeCPython(t *testing.T) {
	for _, tc := range []struct {
		name, src, msg string
		// oracle is CPython's sentence when it differs from the one the two engines agree on. gusty
		// says `index out of range` where CPython names the container (roadmap Gap R.90); the class and
		// the exit code are what the row holds the compiled path to.
		oracle string
	}{
		{
			name: "int_slot_ordered_against_text",
			src:  "xs = []\nxs.append(3)\nprint(1 if xs[0] > \"a\" else 0)\n",
			msg:  "TypeError: '>' not supported between instances of 'int' and 'str'",
		},
		{
			name: "text_on_the_left_of_an_int_slot",
			src:  "xs = []\nxs.append(3)\nprint(1 if \"a\" < xs[0] else 0)\n",
			msg:  "TypeError: '<' not supported between instances of 'str' and 'int'",
		},
		{
			name: "float_slot_ordered_against_text",
			src:  "xs = []\nxs.append(1.5)\nprint(1 if xs[0] >= \"a\" else 0)\n",
			msg:  "TypeError: '>=' not supported between instances of 'float' and 'str'",
		},
		{
			name: "none_slot_ordered_against_number",
			src:  "xs = []\nxs.append(None)\nprint(1 if xs[0] > 1 else 0)\n",
			msg:  "TypeError: '>' not supported between instances of 'NoneType' and 'int'",
		},
		{
			name: "list_slot_ordered_against_number",
			src:  "xs = []\nxs.append([1, 2])\nprint(1 if xs[0] > 1 else 0)\n",
			msg:  "TypeError: '>' not supported between instances of 'list' and 'int'",
		},
		{
			name: "dict_slot_ordered_against_number",
			src:  "xs = []\nxs.append({\"k\": 1})\nprint(1 if xs[0] < 1 else 0)\n",
			msg:  "TypeError: '<' not supported between instances of 'dict' and 'int'",
		},
		{
			name: "set_slot_ordered_against_number",
			src:  "xs = []\nxs.append({1, 2})\nprint(1 if xs[0] > 1 else 0)\n",
			msg:  "TypeError: '>' not supported between instances of 'set' and 'int'",
		},
		{
			name: "a_text_slot_ordered_against_an_int_variable",
			src:  "xs = []\nxs.append(\"a\")\ni = 1\nprint(1 if xs[0] > i else 0)\n",
			msg:  "TypeError: '>' not supported between instances of 'str' and 'int'",
		},
		{
			name: "an_int_slot_ordered_against_a_text_variable",
			src:  "xs = []\nxs.append(3)\nt = \"a\"\nprint(1 if xs[0] <= t else 0)\n",
			msg:  "TypeError: '<=' not supported between instances of 'int' and 'str'",
		},
		{
			name: "nested_slot_ordered_against_text",
			src:  "xs = []\nxs.append([3, \"a\"])\nprint(1 if xs[0][0] > \"a\" else 0)\n",
			msg:  "TypeError: '>' not supported between instances of 'int' and 'str'",
		},
		{
			name:   "the_bounds_check_fires_before_the_ordering",
			src:    "xs = []\nxs.append([1, 2])\nprint(1 if xs[0][9] > 1 else 0)\n",
			msg:    "IndexError: index out of range",
			oracle: "IndexError: list index out of range",
		},
		{
			// The CLASS was always the agreement; the wording is the reference's now too — a KeyError
			// carries the key's repr (roadmap Gap R.189, closed on the compiled leg in ADR 0302's cycle,
			// where the raise site renders a literal key into the message). This row is the ratchet: if
			// the compiled program ever goes back to the module's prose constant, the two columns part
			// and the case fails.
			name:   "a_missing_key_raises_its_own_key_error",
			src:    "d = {}\nd[\"a\"] = [1, 2]\nprint(1 if d[\"z\"][0] > 1 else 0)\n",
			msg:    "KeyError: 'z'",
			oracle: "KeyError: 'z'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "slot_order_trap.gy", tc.src)
			pyOut, pyCode := oracleTrap(t, path)
			if pyCode == 0 {
				t.Fatalf("the oracle did not trap; the row claims a trap:\n%s", pyOut)
			}
			want := tc.msg
			if tc.oracle != "" {
				want = tc.oracle
			}
			if !strings.Contains(pyOut, want) {
				t.Fatalf("the oracle's sentence is not what the row says (%q):\n%s", want, pyOut)
			}
			for _, engine := range cliEngines {
				out := cliRun(t, engine, path)
				if strings.Contains(out, "codegen:") {
					t.Fatalf("%s refused a program the oracle traps on: %s", engine, out)
				}
				// A KeyError carries the key's repr. The compiled raise site renders a key it can see as
				// a literal into the message, which is the reference's sentence exactly (roadmap
				// Gap R.189, closed on that door). A read that reaches the container through a computed
				// path — the heap-argument door, where the key arrives as a (payload, tag) pair — still
				// raises the module's generic sentence, because naming that key means rendering a value
				// whose kind is a run-time fact, which is L11.1's tagged word. The class and the exit
				// class below are checked either way, and the generic answer is counted as the filed
				// half of the gap rather than allowed to pass unseen.
				want := tc.msg
				if tc.oracle != "" && strings.HasPrefix(tc.oracle, "KeyError: '") {
					want = tc.oracle
					if !strings.Contains(out, want) {
						if !strings.Contains(out, "KeyError: key not found") {
							t.Errorf("%s raised neither the reference's %q nor the generic KeyError:\n%s", engine, want, out)
						}
						noteCompiledGap(t, tc.src, out)
					}
				} else if !strings.Contains(out, want) {
					t.Errorf("%s did not raise %q:\n%s", engine, want, out)
				}
				if _, code := cliRunCode(t, engine, path); code != 3 {
					t.Errorf("%s exited %d, want the trap class 3 (ADR 0166):\n%s", engine, code, out)
				}
			}
		})
	}
}

// TestSlotOrderOfAnUnliteralisedSlotTrapIsCatchable holds the raise to being an exception: the program
// can catch it, so the compiled leg leaves through the same exit class as the record and not
// through a crash (roadmap ADR 0166, Gap R.37's rule that a trap must be a trap).
func TestSlotOrderOfAnUnliteralisedSlotTrapIsCatchable(t *testing.T) {
	src := "xs = []\nxs.append(3)\ntry:\n    print(1 if xs[0] > \"a\" else 0)\nexcept TypeError:\n    print(\"caught\")\n"
	path := writeSrc(t, t.TempDir(), "slot_order_catch.gy", src)
	const want = "caught\n"
	if py, ok := cpythonOut(t, path); ok && py != want {
		t.Fatalf("the expectation is not CPython's: %q", py)
	}
	for _, engine := range cliEngines {
		out, code := cliRunCode(t, engine, path)
		if code != 0 || out != want {
			t.Errorf("%s printed %q with exit %d, want %q and exit 0", engine, out, code, want)
		}
	}
}

// TestSlotOrderOfABoolSlotNamesBool is the row ADR 0259 turned from a debt into a parity claim: a
// bool goes into a container slot carrying its own tag, so the sentence the ordering trap prints
// names 'bool' the way CPython's does — where ADR 0232's vocabulary tagged the slot as the number
// and the compiled path said 'int'. ADR 0257 had already made a bool print its verdict where the front
// end could see the expression that made it; a container slot is where it cannot, which is why this
// row needed the tag and not the predicate.
func TestSlotOrderOfABoolSlotNamesBool(t *testing.T) {
	src := "xs = []\nxs.append(True)\nprint(1 if xs[0] > \"a\" else 0)\n"
	path := writeSrc(t, t.TempDir(), "slot_order_bool.gy", src)
	if pyOut, pyCode := oracleTrap(t, path); pyCode == 0 || !strings.Contains(pyOut, "TypeError") {
		t.Fatalf("the oracle should reject this shape outright:\n%s", pyOut)
	}
	const want = "TypeError: '>' not supported between instances of 'bool' and 'str'"
	for _, engine := range cliEngines {
		out := cliRun(t, engine, path)
		if !strings.Contains(out, want) {
			t.Errorf("%s raised %q, want the bool's own sentence %q", engine, out, want)
		}
		if _, code := cliRunCode(t, engine, path); code != 3 {
			t.Errorf("%s exited %d, want 3", engine, code)
		}
	}
}

// TestSlotOrderOfTwoUnliteralisedSlotsStaysFiledNotFixed walks the two shapes the door still leaves to
// the lowering underneath, through the shipped binary, and holds the *current* answer so a divergence
// cannot silently widen or quietly "pass" a CPython comparison it does not pass: roadmap Gap R.97 (a
// slot against a slot of containers the program built) and Gap R.83 (the other operand's kind being
// unprovable). Today the record traps exactly as the oracle does and the compiled leg answers a
// verdict — that disagreement *is* the row, and the row is where it stays written until someone closes
// it (Gap R.37's rule: a known wrong answer needs a failing check somewhere, not a comment). Neither
// leg may exit 2.
func TestSlotOrderOfTwoUnliteralisedSlotsStaysFiledNotFixed(t *testing.T) {
	for _, tc := range []struct {
		name, src, aotOut, oracle, gap string
	}{
		{
			"two_built_slots_of_different_kinds",
			"xs = []\nxs.append(\"a\")\nys = []\nys.append(1)\nprint(1 if xs[0] > ys[0] else 0)\n",
			"0\n",
			"TypeError: '>' not supported between instances of 'str' and 'int'",
			"roadmap Gap R.97",
		},
		{
			"built_slot_against_a_call_of_two_kinds",
			"def pick(c):\n    if c:\n        return \"z\"\n    return 7\n\nxs = []\nxs.append(3)\nprint(1 if xs[0] > pick(1) else 0)\n",
			"1\n",
			"TypeError: '>' not supported between instances of 'int' and 'str'",
			"roadmap Gap R.83",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "slot_order_filed.gy", tc.src)
			if pyOut, pyCode := oracleTrap(t, path); pyCode == 0 || !strings.Contains(pyOut, tc.oracle) {
				t.Fatalf("the oracle's answer is not what the row records (%s):\n%s", tc.oracle, pyOut)
			}
			// The retired engine was on the oracle's side of this divergence — it trapped with the
			// reference's TypeError — which is what made it a divergence rather than a shared blind
			// spot. With that engine gone the row's claim is directly against the reference: the
			// compiled program traps with the same class and words, refuses the shape by name, or the
			// disagreement sits on the reference-debt ledger with the roadmap row that owes it. The
			// retired engine's side of that story is preserved in the golden record and its own ledger.
			compiledOut, compiledCode := cliRunMerged(t, "--aot", path)
			requireReferenceTrapOrHonestRefusal(t, tc.src, tc.oracle, compiledOut, compiledCode, tc.gap,
				"an ordering between two values whose kinds are run-time facts")
			out, code := cliRunCode(t, "--aot", path)
			if code == 2 {
				t.Fatalf("--aot rejected the compiler's own module (ADR 0166):\n%s", out)
			}
			if code == 3 && strings.Contains(out, tc.oracle) {
				t.Fatalf("--aot traps the way the oracle does now: the compiled leg caught up, so delete " +
					"this row and close " + tc.gap)
			}
			if code != 0 || out != tc.aotOut {
				t.Errorf("--aot printed %q with exit %d where the row recorded %q and exit 0 — the shape "+
					"moved, update it and %s with it", out, code, tc.aotOut, tc.gap)
			}
		})
	}
}

// TestSlotOrderOfAnUnliteralisedSlotKeepsTheRefusalExitClass is the last leg of the exit-code contract
// for this feature: the shape the door declines has to leave through 1 with a sentence that names the
// missing half, never through 2 — `xs[0] > [0]` (a container on the settled side) is Gap R.87's class,
// an ordering that reaches llc with a container global in a value position.
func TestSlotOrderOfAnUnliteralisedSlotKeepsTheRefusalExitClass(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"nested_slot_against_a_container_literal",
			"xs = []\nxs.append([3, \"a\"])\nprint(1 if xs[0][0] > [0] else 0)\n",
			"cannot reach into xs's slots",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "slot_order_refuse.gy", tc.src)
			for _, engine := range cliEngines {
				out := cliRun(t, engine, path)
				if _, code := cliRunCode(t, engine, path); code == 2 {
					t.Fatalf("%s turned a declined shape into the compiler's own bug (exit 2, ADR 0166):\n%s",
						engine, out)
				}
				if engine == "--aot" && !strings.Contains(out, tc.want) {
					t.Errorf("the compiled leg declined without naming the missing half (%q):\n%s", tc.want, out)
				}
			}
		})
	}
}

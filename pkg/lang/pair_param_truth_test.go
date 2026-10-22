package lang

import (
	"strings"
	"testing"
)

// pair_param_truth_test.go — the callee that TESTS its own parameter asks the tag the caller wrote
// (roadmap L11.1, Gap R.361, ADR 0393).
//
// ADR 0389 gave the call argument two words; this row gives the callee's condition the second one. The
// shapes below are the four positions that ask a value's truth — `if`/`elif`/`while`, a ternary's head,
// `not`, and the builtin `bool()` — over a parameter whose kind the call site decided. Every `want` is what
// `python3` printed for the same source, read off the reference before the row was written.
//
// The three empty-container-as-a-call kinds are NOT in the answer ladder: `list()`, `dict()` and `set()`
// stored as a dict entry and handed over cross with an integer tag (the print door renders them as `3`),
// which is ADR 0391's road one door further out and roadmap Gap R.369's to close. They are pinned below,
// beside the other shapes this row deliberately left where it found them.

// paramTruthRow is one answered shape: a body, the entry it is called with, and the bytes the reference
// prints for it.
type paramTruthRow struct {
	name, src, want string
}

// paramTruthLadder is the answer table. `truthy` is the reference's verdict on the entry — the fact the
// five positions have to agree on — and each position renders it in its own words.
func paramTruthLadder() []paramTruthRow {
	kinds := []struct {
		name   string
		entry  string
		truthy bool
	}{
		{"empty_list", "[]", false},
		{"empty_dict", "{}", false},
		{"empty_text", `""`, false},
		{"text", `"hi"`, true},
		{"list", "[1]", true},
		{"none", "None", false},
		{"zero", "0", false},
		{"one", "1", true},
		{"zero_float", "0.0", false},
		{"negzero_float", "-0.0", false},
		{"float", "2.5", true},
		{"true", "True", true},
		{"false", "False", false},
		{"dict", "{1: 2}", true},
		{"set", "{1}", true},
	}
	bodies := []struct {
		name  string
		body  string
		yes   func(bool) string
		param string
	}{
		{
			name:  "if_else",
			body:  "def f(v):\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\n",
			yes:   func(t bool) string { return map[bool]string{true: "t\n", false: "f\n"}[t] },
			param: "v",
		},
		{
			name:  "elif_chain",
			body:  "def f(v):\n    if v:\n        print(\"a\")\n    elif v:\n        print(\"b\")\n    else:\n        print(\"c\")\n\n",
			yes:   func(t bool) string { return map[bool]string{true: "a\n", false: "c\n"}[t] },
			param: "v",
		},
		{
			name:  "not",
			body:  "def f(v):\n    print(not v)\n\n",
			yes:   func(t bool) string { return map[bool]string{true: "False\n", false: "True\n"}[t] },
			param: "v",
		},
		{
			name:  "bool_builtin",
			body:  "def f(v):\n    print(bool(v))\n\n",
			yes:   func(t bool) string { return map[bool]string{true: "True\n", false: "False\n"}[t] },
			param: "v",
		},
		{
			name:  "ternary_head",
			body:  "def f(v):\n    print(\"y\" if v else \"n\")\n\n",
			yes:   func(t bool) string { return map[bool]string{true: "y\n", false: "n\n"}[t] },
			param: "v",
		},
		{
			// The loop head is the position where a wrong truth is not one wrong line but a wrong number
			// of iterations — the reason the gate below asks the body before it trusts the tag.
			name:  "while_head",
			body:  "def f(v):\n    while v:\n        print(\"once\")\n        break\n    print(\"done\")\n\n",
			yes:   func(t bool) string { return map[bool]string{true: "once\ndone\n", false: "done\n"}[t] },
			param: "v",
		},
	}
	var out []paramTruthRow
	for _, b := range bodies {
		for _, k := range kinds {
			out = append(out, paramTruthRow{
				name: b.name + "_" + k.name,
				src:  b.body + "d = {\"k\": " + k.entry + "}\nf(d[\"k\"])\n",
				want: b.yes(k.truthy),
			})
		}
	}
	return out
}

// TestTheTruthOfACallerSuppliedParameterAsksTheTag is the row itself: the callee's own condition reads the
// kind the caller supplied, on every road that asks a value whether it is true.
func TestTheTruthOfACallerSuppliedParameterAsksTheTag(t *testing.T) {
	t.Parallel()
	for _, tc := range paramTruthLadder() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Errorf("printed %q, want %q — the condition tested the parameter's payload, not its tag\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
	t.Run("the_row_program", func(t *testing.T) {
		t.Parallel()
		src := "def f(v):\n    if v:\n        return 1\n    return 0\n\nd = {\"e\": [], \"t\": \"\", \"l\": [1], \"n\": None}\nprint(f(d[\"e\"]), f(d[\"t\"]), f(d[\"l\"]), f(d[\"n\"]))\n"
		if got := compiledOut(t, src); got != "0 0 1 0\n" {
			t.Errorf("printed %q, want the reference's `0 0 1 0`\nsrc: %s", got, src)
		}
	})
}

// TestTheTruthOfTheParameterAgreesWithTheRecord is the record leg: every ladder program is on the record,
// and the record's answer is the compiled answer. A ladder row whose record went missing fails here rather
// than being witnessed by one leg only.
func TestTheTruthOfTheParameterAgreesWithTheRecord(t *testing.T) {
	t.Parallel()
	rows := paramTruthLadder()
	rows = append(rows, paramTruthRow{
		name: "the_row_program",
		src:  "def f(v):\n    if v:\n        return 1\n    return 0\n\nd = {\"e\": [], \"t\": \"\", \"l\": [1], \"n\": None}\nprint(f(d[\"e\"]), f(d[\"t\"]), f(d[\"l\"]), f(d[\"n\"]))\n",
		want: "0 0 1 0\n",
	})
	for _, tc := range rows {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !HasGoldenAnswer(tc.src) {
				t.Fatalf("the record does not cover %s — add it with tools/recmerge (it is answered on the reference leg)", tc.src)
			}
			rec, err := runGoldenStdout(t, tc.src)
			if err != nil {
				t.Fatalf("the record leg did not answer %s: %v", tc.src, err)
			}
			if rec != tc.want {
				t.Errorf("the record prints %q, the reference prints %q", rec, tc.want)
			}
			if got := compiledOut(t, tc.src); got != rec {
				t.Errorf("the compiled leg prints %q, the record prints %q for %s", got, rec, tc.src)
			}
		})
	}
}

// functionRegion is the part of the module the compiler wrote for one user function — the runtime helpers
// carry `@rt_pair_truth` as a definition of their own, so a claim about what a body ASKED has to be read
// off that body.
func functionRegion(t *testing.T, src, fn string) string {
	t.Helper()
	ir := emittedModuleIR(t, src)
	start := strings.Index(ir, "define i32 @gy_"+fn+"(")
	if start < 0 {
		t.Fatalf("the module has no @gy_%s:\n%s", fn, ir)
	}
	rest := ir[start:]
	if end := strings.Index(rest, "\n}"); end >= 0 {
		return rest[:end]
	}
	return rest
}

// TestTheTruthOfTheParameterReachesThePairTable asks the module rather than the answer: the five positions
// are answered by the runtime table the printer and `and`/`or` already consult, and none of them is
// answered by testing the payload.
func TestTheTruthOfTheParameterReachesThePairTable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{"if_arm", "def f(v):\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {\"k\": []}\nf(d[\"k\"])\n"},
		{"not", "def f(v):\n    print(not v)\n\nd = {\"k\": []}\nf(d[\"k\"])\n"},
		{"bool", "def f(v):\n    print(bool(v))\n\nd = {\"k\": []}\nf(d[\"k\"])\n"},
		{"ternary", "def f(v):\n    print(\"y\" if v else \"n\")\n\nd = {\"k\": []}\nf(d[\"k\"])\n"},
		{"while_head", "def f(v):\n    while v:\n        print(\"once\")\n        break\n\nd = {\"k\": []}\nf(d[\"k\"])\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := functionRegion(t, tc.src, "f")
			if !strings.Contains(body, "@rt_pair_truth") {
				t.Errorf("%s: the condition did not ask the pair table:\n%s", tc.name, body)
			}
			if !strings.Contains(body, "i32* %_v_tag") {
				t.Errorf("%s: the condition never loaded the tag the caller wrote:\n%s", tc.name, body)
			}
		})
	}
}

// The three negative controls that make the claim above a measurement rather than a widening.
//
//  1. an argument whose kind the compiler can prove — the pair never crosses, so the body keeps the plain
//     `icmp ne i32 %p0, 0` it has always had;
//  2. a name the ARITHMETIC door bound — its tag can only ever name int, float or bool, so ADR 0268's lift
//     answers it and this row's table stays out of the module (the file's gate is the ORIGIN);
//  3. a body that rebinds its own parameter — the tag word would be a stale one by the second iteration, so
//     the gate declines and the program keeps the road (and the answer, wrong as Gap R.366 files it) it had
//     before. `rt_pair_truth` absent here is the gate working, not the row not landing.
func TestTheTruthOfTheParameterDoorOpensOnlyWhereItShould(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, absent, present string
		inMain                     bool
	}{
		{
			name:   "a_provable_integer_argument_keeps_the_plain_compare",
			src:    "def f(v):\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {1: 2}\nf(d[1])\n",
			absent: "@rt_pair_truth",
		},
		{
			// A module-level name the ARITHMETIC door bound, so the ask lives in `main` and both
			// claims are read there: the lift stayed, the table never arrived.
			name:    "a_name_the_arithmetic_door_bound_keeps_the_lift",
			src:     "xs = []\nxs.append([7, 8])\nn = xs[0][0] * 2\nif n:\n    print(1)\n",
			absent:  "@rt_pair_truth",
			present: "@rt_lift_num",
			inMain:  true,
		},
		{
			name:   "a_body_that_rebinds_its_parameter_never_asks_the_stale_tag",
			src:    "def f(v):\n    while v:\n        print(\"once\")\n        v = 0\n    print(\"done\")\n\nd = {\"k\": 1}\nf(d[\"k\"])\n",
			absent: "@rt_pair_truth",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var body string
			if tc.inMain {
				body = mainRegion(t, tc.src)
			} else {
				body = functionRegion(t, tc.src, "f")
			}
			if strings.Contains(body, tc.absent) {
				t.Errorf("%s reached %s though the gate should decline it:\n%s", tc.name, tc.absent, body)
			}
			if tc.present != "" && !strings.Contains(body, tc.present) {
				t.Errorf("%s lost %s — the road this row replaced rather than widened:\n%s", tc.name, tc.present, body)
			}
		})
	}
}

// TestTheOperandOfNotRunsOnce is roadmap Gap R.365, paid by the same move: `not` asks the truth road, and
// used to lower the operand for its value first and throw the value away. A call there is an effect, and the
// effect ran twice — `print(not boom())` wrote `boom` twice at exit 0 where the reference writes it once.
func TestTheOperandOfNotRunsOnce(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{
			name: "a_call_under_not",
			src:  "def boom():\n    print(\"boom\")\n    return 1\n\ndef f(v):\n    print(not boom())\n\nf(1)\n",
			want: "boom\nFalse\n",
		},
		{
			name: "a_call_under_not_over_a_pair_carrying_parameter",
			src:  "def boom():\n    print(\"boom\")\n    return 1\n\ndef f(v):\n    print(not boom())\n\nd = {\"k\": []}\nf(d[\"k\"])\n",
			want: "boom\nFalse\n",
		},
		{
			// The operand's own effects still run when the test cannot choose them — the rule ADR 0275
			// wrote for `and`/`or`, asked of `not`.
			name: "the_operand_of_not_is_still_evaluated",
			src:  "def boom():\n    print(\"side\")\n    return 0\n\ndef f(v):\n    if not boom():\n        print(\"falsy\")\n\nf(1)\n",
			want: "side\nfalsy\n",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Errorf("printed %q, want %q\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}

// TestWhatTheTruthOfAPairSuppliedParameterStillOwes pins, with today's bytes, the shapes this row leaves
// standing. Each is the same door one position further, and each fails — here, where its own text says to
// delete it — the day that door opens.
func TestWhatTheTruthOfAPairSuppliedParameterStillOwes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, today, wantSub, row string
	}{
		{
			// Gap R.366: the tag word is written once, by the entry store; the plain rebinding road
			// stores the payload and retires the compile-time record, leaving the tag describing no
			// value the name holds.
			name:  "a_parameter_the_body_rebinds_is_asked_a_stale_tag",
			src:   "def f(v):\n    n = 0\n    while v:\n        n = n + 1\n        v = 0\n    return n\n\nd = {\"e\": [], \"t\": \"\", \"l\": [1], \"n\": None}\nprint(f(d[\"e\"]), f(d[\"t\"]), f(d[\"l\"]), f(d[\"n\"]))\n",
			today: "1 1 1 0\n",
			row:   "Gap R.366",
		},
		{
			// The same body with an empty container on its second line: the JIT path answers `2` where
			// the reference answers `1`, and the LINKED binary — the same module through `cc` — never
			// finishes at all. Both witnesses of that one program are pinned: this row, and
			// `TestARebindingParameterLoopStillRunsForever` in integration/pair_param_truth_test.go.
			name:  "a_rebinding_loop_counts_the_iterations_it_should_not",
			src:   "def f(v):\n    n = 0\n    while v:\n        n = n + 1\n        v = []\n    return n\n\nd = {\"l\": [1]}\nprint(f(d[\"l\"]))\n",
			today: "2\n",
			row:   "Gap R.366",
		},
		{
			// Gap R.367: `and`/`or` choose an operand by its truth, and for a parameter the pair road
			// binds the whole function stays on the ordinary road — the condition answers `True` for an
			// empty list, and the VALUE position refuses naming the tagged value word Gap R.146 owns.
			name:  "an_and_operand_that_is_a_caller_supplied_parameter",
			src:   "def f(v):\n    if v and 1:\n        return \"T\"\n    return \"F\"\n\nd = {\"e\": [], \"t\": \"\", \"l\": [1], \"n\": None, \"z\": 0, \"o\": 1, \"s\": \"hi\", \"f\": 0.0}\nprint(f(d[\"e\"]), f(d[\"t\"]), f(d[\"l\"]), f(d[\"n\"]), f(d[\"z\"]), f(d[\"o\"]), f(d[\"s\"]), f(d[\"f\"]))\n",
			today: "T T T F F T T T\n",
			row:   "Gap R.367",
		},
		{
			name:    "the_chosen_operand_of_an_and_over_a_parameter",
			src:     "def f(v):\n    return v and 7\n\nd = {\"e\": []}\nprint(f(d[\"e\"]))\n",
			wantSub: "chooses between two values whose kinds",
			row:     "Gap R.367",
		},
		{
			// Gap R.368: a body whose answer is a text (or a double) is emitted under a convention that
			// owns its parameter words, so the callee never gets the tag to ask.
			name:  "a_text_answering_body_keeps_its_parameters_one_word",
			src:   "def f(v):\n    while v:\n        print(\"once\")\n        break\n    return \"done\"\n\nd = {\"e\": [], \"t\": \"\", \"l\": [1], \"n\": None, \"z\": 0, \"o\": 1, \"s\": \"hi\", \"f\": 0.0}\nprint(f(d[\"e\"]), f(d[\"t\"]), f(d[\"l\"]), f(d[\"n\"]), f(d[\"z\"]), f(d[\"o\"]), f(d[\"s\"]), f(d[\"f\"]))\n",
			today: "once\nonce\nonce\nonce\nonce\nonce\ndone done done done done done done done\n",
			row:   "Gap R.368",
		},
		{
			// Gap R.366 again, seen from the print door: an empty container written as a CALL and stored
			// in a dict literal crosses with an integer tag, so the truth it is handed is a number's.
			name:  "an_empty_container_written_as_a_call_crosses_as_an_integer",
			src:   "def f(v):\n    print(v)\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {\"k\": list()}\nf(d[\"k\"])\n",
			today: "3\nt\n",
			row:   "Gap R.369",
		},
		{
			name:  "the_set_spelling_of_the_same_entry",
			src:   "def f(v):\n    print(v)\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {\"k\": set()}\nf(d[\"k\"])\n",
			today: "3\nt\n",
			row:   "Gap R.369",
		},
		{
			// Gap R.367, the other half of the chosen operand: the ternary. Its HEAD is served by this
			// row's door (`print("y" if v else "n")` answers right), its ARM is not — the arm IS the answer,
			// one `select` has one type for both of them, and the tag has nowhere to ride.
			name:  "a_ternary_arm_that_is_a_caller_supplied_parameter",
			src:   "def f(v):\n    return v if v else \"z\"\n\nd = {\"e\": []}\nprint(f(d[\"e\"]))\n",
			today: "3\n",
			row:   "Gap R.367",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.wantSub != "" {
				out, isRefusal := compiledOutOrRefusal(t, tc.src)
				if !isRefusal {
					t.Fatalf("%s answered %q, want the %s refusal", tc.src, out, tc.row)
				}
				if !strings.Contains(out, tc.wantSub) {
					t.Errorf("%s refused without naming %q:\n%s", tc.src, tc.wantSub, out)
				}
				return
			}
			if got := compiledOut(t, tc.src); got != tc.today {
				t.Errorf("%s printed %q; this row pinned %q — the shape changed, DELETE or rewrite the pin (%s)\nsrc: %s", tc.name, got, tc.today, tc.row, tc.src)
			}
		})
	}
}

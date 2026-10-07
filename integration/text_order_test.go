package integration

import (
	"strings"
	"testing"
)

// An ordering of two texts reads the text, not the interned index (roadmap Gap R.84, ADR 0248).
//
// The unit table in pkg/lang/text_order_test.go holds the hand-derived expectations; this file is the
// one with the oracle in it, so a row cannot be written that flatters the compiler. Every row is run
// through python3 first, and the compiled path must print what it printed — which matters most for the
// rows whose arrival order and text order disagree, since those are exactly the ones the old
// index comparison got wrong.

func TestTextOrderingMatchesCPython(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{
			"literal_pair_arriving_backwards",
			"print(1 if \"b\" > \"a\" else 0)\nprint(1 if \"b\" < \"a\" else 0)\n",
		},
		{
			"two_text_variables",
			"a = \"b\"\nb = \"a\"\nprint(1 if a > b else 0)\nprint(1 if a >= b else 0)\n",
		},
		{
			// The pair an index comparison answers correctly only by luck: both words are equal as
			// indices only if the program meant them so, and the ordering has to read the bytes.
			"equal_texts_order_equal",
			"a = \"a\"\nb = \"a\"\nprint(1 if a <= b else 0, 1 if a >= b else 0, 1 if a == b else 0)\n",
		},
		{
			"empty_text_is_below_everything",
			"a = \"\"\nb = \"z\"\nprint(1 if a < b else 0)\nprint(1 if b > a else 0)\nprint(1 if a < \"\" else 0)\n",
		},
		{
			// The intern table orders by arrival, so this row is stated in both arrival orders: the
			// first pins the answer, the second pins the answer the old code got wrong.
			"uppercase_below_lowercase_mentioned_first",
			"a = \"B\"\nb = \"a\"\nprint(1 if a < b else 0)\nprint(1 if b < a else 0)\n",
		},
		{
			"uppercase_below_lowercase_mentioned_second",
			"a = \"a\"\nb = \"B\"\nprint(1 if a < b else 0)\nprint(1 if b < a else 0)\n",
		},
		{
			"a_prefix_is_below_the_longer_text",
			"a = \"app\"\nb = \"apple\"\nprint(1 if a < b else 0)\nprint(1 if b <= a else 0)\n",
		},
		{
			"in_a_condition",
			"a = \"b\"\nif a > \"a\":\n    print(\"later\")\nelse:\n    print(\"earlier\")\n",
		},
		{
			"in_a_condition_the_other_way_round",
			"a = \"a\"\nif a > \"b\":\n    print(\"later\")\nelse:\n    print(\"earlier\")\n",
		},
		{
			// The row that used to be refused in a binding and answered wrongly in a print.
			"stored_in_a_variable",
			"a = \"b\"\nb = \"a\"\nlater = a > b\nprint(1 if later else 0)\n",
		},
		{
			"a_string_methods_result_orders_by_its_text",
			"a = \"B\"\nprint(1 if a.lower() > \"a\" else 0)\nprint(1 if a.lower() < \"a\" else 0)\n",
		},
		{
			"in_a_sort_where_the_elements_arrive_out_of_order",
			"xs = [\"b\", \"a\", \"c\"]\nprint(sorted(xs))\n",
		},
		{
			"in_a_loop_body_against_the_literal_it_arrives_after",
			"for w in [\"pear\", \"apple\"]:\n    print(1 if w > \"a\" else 0)\n",
		},
		{
			"through_a_function_parameter",
			"def after(w):\n    return w > \"a\"\n\nprint(1 if after(\"b\") else 0)\nprint(1 if after(\"Z\") else 0)\n",
		},
		{
			// The slot rows: the same ordering, reached through a container the literal describes and
			// through one the program built.
			"two_slots_of_a_text_only_list",
			"xs = [\"b\", \"a\"]\nprint(1 if xs[0] > xs[1] else 0)\nprint(1 if xs[0] < xs[1] else 0)\n",
		},
		{
			"a_text_slot_ordered_against_a_literal",
			"xs = [\"b\", \"a\"]\nprint(1 if xs[0] > \"a\" else 0)\nprint(1 if xs[1] > \"a\" else 0)\n",
		},
		{
			"a_text_slot_through_an_index_the_program_computes",
			"xs = [\"a\", \"b\"]\ni = 0\nprint(1 if xs[i] < \"c\" else 0)\nprint(1 if xs[i] > \"c\" else 0)\n",
		},
		{
			"a_container_the_program_built_itself_orders_its_own_text",
			"xs = []\nxs.append(\"b\")\nxs.append(\"a\")\nprint(1 if xs[0] > xs[1] else 0)\n",
		},
		{
			"a_dict_entry_ordered_as_text",
			"d = {}\nd[\"k\"] = \"b\"\nprint(1 if d[\"k\"] > \"a\" else 0)\n",
		},
		{
			// An ordering of a parameter against a text, asked in a context that does not ask the
			// function what kind it returns. Asked through print(f("hi")) the same body is refused —
			// measured and recorded as Gap R.88, not flattered here.
			"ordering_on_a_string_parameter_in_a_condition",
			"def f(s):\n    return s < \"z\"\n\nprint(1 if f(\"hi\") else 0)\nprint(1 if f(\"zzz\") else 0)\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "order.gy", tc.src)
			py, ok := cpythonOut(t, path)
			if !ok {
				t.Skipf("no oracle to ask")
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("%s rejected the compiler's own module (ADR 0166 / exit-code contract):\n%s",
						engine, cliRun(t, engine, path))
				}
				if code != 0 {
					t.Fatalf("%s exited %d on a program the oracle answers: %s", engine, code,
						cliRun(t, engine, path))
				}
				if out != py {
					t.Errorf("%s printed %q, want the oracle's %q\nsrc: %s", engine, out, py, tc.src)
				}
			}
		})
	}
}

// What the ordering door still will not answer, kept honest at the CLI: it must name the half that is
// missing, stay in the refusal exit class (never 2, which ADR 0166 reserves for the compiler's own
// mistake), and leave the record answering the program.
func TestTextOrderingStillRefusedHonestly(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			// Arithmetic on a text stays refused: ADR 0248 answered orderings, not this.
			"arithmetic_on_a_string_parameter",
			"def f(s):\n    return s * 2\n\nprint(f(\"hi\"))\n",
			"is not supported in the AOT backend",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "order_refuse.gy", tc.src)
			if py, ok := cpythonOut(t, path); !ok || py == "" {
				t.Fatalf("this row is about a program CPython answers")
			}
			// CPython answers, which is what makes this row's refusal a limit of this backend rather
			// than a limit of the language. The claim, then, is the two-way one: the compiled path
			// answers, or it refuses with the missing half named — and the row's own `want` column
			// checks the sentence, so an honest refusal is checked and a mute one fails.
			if _, code := cliRunCode(t, "--aot", path); code == exitIRVerify {
				t.Fatalf("--aot: exit 2 — LLVM rejected the module gusty emitted (ADR 0166):\n%s", cliRun(t, "--aot", path))
			} else if code != 0 {
				combined := cliRun(t, "--aot", path)
				if code != 1 || !refusesHonestly(combined) {
					t.Fatalf("--aot exited %d on a program CPython answers, without naming the missing half: %s", code, combined)
				}
				noteCompiledGap(t, tc.src, combined)
			}
			out, code := cliRunCode(t, "--aot", path)
			if code == 2 {
				t.Fatalf("--aot rejected the compiler's own module (exit 2): %s", cliRun(t, "--aot", path))
			}
			if code == 0 {
				t.Fatalf("--aot answered %q where this row is about a refusal", out)
			}
			if combined := cliRun(t, "--aot", path); !strings.Contains(combined, tc.want) {
				t.Fatalf("refusal %q does not name %q", combined, tc.want)
			}
			// Gap R.38's rule: a refusal may not claim something false about the other leg, so the
			// leg it names has to answer the program for real.
			if combined := cliRun(t, "--aot", path); strings.Contains(combined, "the interpreter supports all") {
				t.Fatalf("the refusal claims the interpreter supports a whole class: %s", combined)
			}
		})
	}
}

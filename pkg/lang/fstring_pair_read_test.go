package lang

// The f-string field is a print in disguise, so it asks the print road's question.
//
// `f"{d[1]}|{xs[0]}"` renders exactly the slots `print(d[1])` renders, and for as long as the field had
// its own narrower view of the world the two lines disagreed about one value:
//
//	d = {1: 3.5, 2: "x", 3: None, 4: True, 5: [2]}
//	print(d[1])            # 3.5   — the pair door, since ADR 0232
//	print(f"{d[1]}")       # 5     — the @float_box handle, at exit 0
//	print(f"{d[2]}")       # 0     — the text's index into @str_tab
//	print(f"{d[5]}")       # 5     — a heap handle wearing a list
//
// The field road asked NONE of the five subscript arms the print road asks (roadmap Gap R.283, ADR 0356).
// `pkg/lang/list_literal_read.go`'s own docstring had been promising for two cycles that "a position that
// reads a value by its kind — print, `str`/`repr`, an f-string field, a binding, a `len`, a membership
// test, a loop — asks the door for the pair"; the field was on the list and not in the code, which is the
// reason no test caught it: the claim was written where a reader would trust it.
//
// So the arms are one door now (`subscriptPairOf`, in that file's house style), asked by the print road and
// this one, and this file asserts the two cannot disagree: same read, both spellings, one answer — judged on
// the record leg (the retired engine's own stdout for the read) and the reference leg (CPython).
//
// The record leg deserves a word about what it can and cannot witness. The retired engine ran 7008 sources
// and exactly ONE of them interpolated a subscript into an f-string (`print(f"{max([True, 0])}")`, a call,
// not a slot) — which is precisely why the defect survived: no record ever put a slot read inside a field, so
// no record could notice what the field did with one. The leg below therefore asks the record about the READ
// (`print(d[False])` → `3.5`) and the compiled backend about the FIELD, and requires one text. Minting a
// record for the field spelling would be the compiled backend testifying about itself, which ADR 0302 forbids
// in the sentence above the drift file.
//
// The IR rows are the other half — an f-string field is rendered by the module's ONE tag-reading printer, and
// the compile-time fold keeps the shapes it genuinely reads, so widening the door did not make the cheap case
// pay for the general one.

import (
	"regexp"
	"strings"
	"testing"
)

// fstringFieldOf is the field spelling of a read: the same expression, interpolated, with the padding that
// used to make it a syntax error (Gap R.294) left in on purpose.
func fstringFieldOf(prefix, expr string) string {
	return prefix + "print(f\"{ " + expr + " }\")\n"
}

// TestAFstringFieldOfASelfDescribingSlotRendersWhatTheReferenceRenders is the answered half: every kind a
// slot can hold, read by a field, against the reference — through the compiler.
func TestAFstringFieldOfASelfDescribingSlotRendersWhatTheReferenceRenders(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"a_float_entry", "d = {1: 3.5}\nprint(f\"{d[1]}\")\n"},
		{"a_float_entry_spaced", "d = {1: 3.5}\nprint(f\"{ d[1] }\")\n"},
		{"a_text_entry", "d = {1: \"x\"}\nprint(f\"{d[1]}\")\n"},
		{"a_none_entry", "d = {1: None}\nprint(f\"{d[1]}\")\n"},
		{"a_bool_entry", "d = {1: True}\nprint(f\"{d[1]}\")\n"},
		{"a_list_entry", "d = {1: [2]}\nprint(f\"{d[1]}\")\n"},
		{"a_dict_entry", "d = {1: {\"k\": 1}}\nprint(f\"{d[1]}\")\n"},
		{"an_int_entry", "d = {1: 7}\nprint(f\"{d[1]}\")\n"},
		{"every_kind_in_one_dict", "d = {1: 3.5, 2: \"x\", 3: None, 4: True, 5: [2], 6: {\"k\": 1}, 7: 7}\nprint(f\"{d[1]}|{d[2]}|{d[3]}|{d[4]}|{d[5]}|{d[6]}|{d[7]}\")\n"},
		{"the_repr_conversion_of_each", "d = {1: 3.5, 2: \"x\", 3: None, 4: True, 5: [2]}\nprint(f\"{d[1]!r}|{d[2]!r}|{d[3]!r}|{d[4]!r}|{d[5]!r}\")\n"},
		{"the_str_conversion_of_each", "d = {1: 3.5, 2: \"x\", 3: None, 4: True, 5: [2]}\nprint(f\"{d[1]!s}|{d[2]!s}|{d[3]!s}|{d[4]!s}|{d[5]!s}\")\n"},
		{"a_mixed_list_slot", "xs = [1.5, \"a\", None, True, [7]]\nprint(f\"{xs[0]}|{xs[1]}|{xs[2]}|{xs[3]}|{xs[4]}\")\n"},
		{"a_mixed_list_slot_repr", "xs = [1.5, \"a\", None, True, [7]]\nprint(f\"{xs[0]!r}|{xs[1]!r}|{xs[2]!r}|{xs[3]!r}|{xs[4]!r}\")\n"},
		{"a_slot_one_level_deeper", "xs = []\nxs.append([7, 8])\nprint(f\"{xs[0][0]}|{xs[0][1]}\")\n"},
		{"a_slot_two_levels_deep", "d = {1: [1, [9, 10]]}\nprint(f\"{d[1][0]}|{d[1][1][1]}\")\n"},
		{"a_list_literal_element", "print(f\"{[None, 1.5][1]}|{['a', 2][0]}\")\n"},
		{"a_dict_literal_entry", "print(f\"{ {'a': 1.5}['a'] }\")\n"},
		{"a_dict_literal_entry_unspaced", "print(f\"{ {'a': 1.5}['a']}\")\n"},
		{"a_set_membership_from_a_dict_entry", "d = {1: [1, 2]}\nprint(f\"{d[1][1] == 2}\")\n"},
		{"text_around_the_field", "xs = [1.5, \"a\"]\nprint(f\"v={xs[1]} and {xs[0]}!\")\n"},
		{"a_field_that_is_arithmetic_over_a_slot", "xs = [1.5, \"a\"]\nprint(f\"{xs[0] + 1}\")\n"},
		{"a_field_read_out_of_a_list_bound_from_dict_entries", "d = {1: 3.5, 2: \"x\", 3: None}\nxs = [d[1], d[2], d[3]]\nprint(f\"{xs[0]}|{xs[1]}|{xs[2]}\")\n"},
		{"a_field_of_a_string_position", "s = \"abc\"\nprint(f\"{s[1]}{s[-1]}\")\n"},
		{"a_field_of_a_comprehension_element", "ys = [i * i for i in range(4)]\nprint(f\"{ys[2]}\")\n"},
		{"a_field_whose_key_is_an_expression", "xs = [10, 20, 30]\nprint(f\"{xs[0 or 1]}\")\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			want := referenceOut(t, tc.src)
			got, refused := compiledOutOrRefusal(t, tc.src)
			if refused {
				t.Fatalf("the f-string field refused a program the reference answers: %s\n%s", got, tc.src)
			}
			if got != want {
				t.Fatalf("the compiled leg printed %q, the reference prints %q:\n%s", got, want, tc.src)
			}
		})
	}
}

// The pairs of spelling this row exists for: one read, the `print(...)` road and the field road. A
// disagreement is a door with an arm the other road does not have — which is the shape of the bug, not its
// symptom, so the check is on the pair rather than on any one expected text.
var fstringTwinReads = []struct {
	name   string
	prefix string
	read   string
}{
	{"a_float_slot", "d = {False: 3.5}\n", "d[False]"},
	{"a_text_slot", "d = {None: \"hi\"}\n", "d[None]"},
	{"a_list_slot", "d = {False: [1, 2]}\n", "d[False]"},
	{"a_dict_slot", "d = {False: {\"k\": 1}}\n", "d[False]"},
	{"a_none_slot", "d = {False: None}\n", "d[False]"},
	{"a_bool_slot", "d = {False: True}\n", "d[False]"},
	{"a_mixed_list_slot", "xs = [1, \"a\", None]\n", "xs[1]"},
	{"a_float_in_a_list", "xs = [1.5]\n", "xs[0]"},
	{"a_deep_slot", "xs = []\nxs.append([7, 8])\n", "xs[0][0]"},
	{"a_comprehension_element", "ys = [i * i for i in range(4)]\n", "ys[2]"},
	{"a_dict_comprehension_entry", "da = {k: k * 2 for k in [1, 2]}\n", "da[2]"},
	{"a_string_position", "s = \"abc\"\n", "s[1]"},
	{"an_expression_key", "xs = [10, 20, 30]\n", "xs[0 or 1]"},
	{"a_list_literal_element", "", "[1.5, 2][0]"},
	// A dict literal read by a literal key is covered on the reference leg above: the engine never ran the
	// `print({'a': 1.5}['a'])` spelling, and this leg's rows all have to be sources the record holds.
}

func TestTheFieldAndThePrintCannotDisagreeAboutOneRead(t *testing.T) {
	t.Parallel()
	for _, tc := range fstringTwinReads {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			printed := strings.TrimRight(compiledOut(t, tc.prefix+"print("+tc.read+")\n"), "\n")
			// The padded spelling, so the pair also covers the field's own padding (Gap R.294).
			fielded := strings.TrimRight(compiledOut(t, fstringFieldOf(tc.prefix, tc.read)), "\n")
			if printed != fielded {
				t.Fatalf("%s%s\n  print(%s) printed %q and f\"{%s}\" printed %q — one value, two renderings",
					tc.prefix, tc.read, tc.read, printed, tc.read, fielded)
			}
		})
	}
}

// The record leg: for a read the retired engine printed, the field must render the engine's own text. The
// recorded source is the `print(E)` spelling — the one the engine ran — so the expectation is never the
// compiled backend's own answer, and a source the record does not hold fails the case in `goldenLookup`
// rather than passing quietly.
func TestTheRecordLegAnswersTheFstringFieldReads(t *testing.T) {
	t.Parallel()
	for _, tc := range fstringTwinReads {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			recorded := tc.prefix + "print(" + tc.read + ")\n"
			want, err := runGoldenStdout(t, recorded)
			if err != nil {
				// The record refused or trapped this read; the field owes the same silence, not a text.
				if strings.Contains(err.Error(), "recorded divergence") {
					t.Skipf("ADR 0302: %s is a recorded divergence", recorded)
				}
				t.Fatalf("the record has nothing to say about %s: %v", recorded, err)
			}
			got, refused := compiledOutOrRefusal(t, fstringFieldOf(tc.prefix, tc.read))
			if refused {
				t.Fatalf("the field refused a read the engine printed %q: %s\n%s", want, got, recorded)
			}
			if got != want {
				t.Fatalf("the field printed %q, the record printed %q for %s", got, want, recorded)
			}
		})
	}
}

// No row in the record ever interpolated a slot read, and that silence is the reason this defect lived.
// Pinning it stops a later reader (or a later test) from claiming the record leg witnessed the field, and
// stops someone from "fixing" the count by minting a record the engine never produced.
func TestTheRecordNeverInterpolatedASlotReadSoTheFieldIsWitnessedByItsPrintTwinAlone(t *testing.T) {
	t.Parallel()
	src := recordedFStringSlotReadSources(t)
	if len(src) != 1 {
		t.Fatalf("the record holds %d f-string sources with a subscript inside a field, expected the one the "+
			"probes left: %v\nMinting another record is the compiled backend testifying about itself (ADR 0302)",
			len(src), src)
	}
	if !strings.Contains(src[0], "max([True, 0])") {
		t.Fatalf("the one recorded f-string subscript field changed shape: %q", src[0])
	}
}

var fstringFieldWithSubscript = regexp.MustCompile(`f["']\{[^{}]*\[`)

// recordedFStringSlotReadSources lists the recorded sources that interpolate a subscript into an f-string
// field, straight out of the artifact.
func recordedFStringSlotReadSources(t *testing.T) []string {
	t.Helper()
	g := loadGolden(t)
	var out []string
	for src := range g {
		if fstringFieldWithSubscript.MatchString(src) {
			out = append(out, src)
		}
	}
	return out
}

// The module has ONE printer, and a field that renders a value goes to it. `rt_str_of_value` points the
// printer at a buffer and `rt_print_mixed_value` is the printer; a second renderer anywhere is a second set
// of kinds to get wrong (ADR 0303's door, ADR 0224's interned-text rule).
func TestAFstringFieldIsRenderedByTheModulesOnePrinter(t *testing.T) {
	t.Parallel()
	ir := emittedModuleIR(t, "d = {1: 3.5, 2: \"x\", 3: [2]}\nprint(f\"{d[1]}|{d[2]}|{d[3]}\")\n")
	for _, want := range []string{"@rt_str_of_value", "@rt_print_mixed_value", "@rt_dict_get_tagged", "@rt_dict_value_tag"} {
		if !strings.Contains(ir, want) {
			t.Fatalf("the field road did not ask %s:\n%s", want, ir)
		}
	}
	// The interned index must never reach printf as a number: `%d` of an `@str_tab` index printed `0` where
	// the line above printed `x` (Gap R.42), and a field that regressed to it would print a number rather
	// than fail to compile.
	if strings.Contains(ir, "@str_tab") && !strings.Contains(ir, "@rt_str_ptr") {
		t.Fatalf("the field handed printf a string-table index without going through rt_str_ptr:\n%s", ir)
	}
}

// The fold keeps what it reads. `print([1, 2][0])` has always emitted a constant, and `print(f"{[1, 2][0]}")`
// must not pay for an object the compiler already had: no allocation, no printer, no helper call.
func TestAFstringFieldOverAFoldableLiteralStillFolds(t *testing.T) {
	t.Parallel()
	ir := emittedModuleIR(t, "print(f\"{[1, 2, 3][1]}\")\n")
	for _, banned := range []string{"call i32 @rt_alloc", "@rt_get_elem", "@rt_tag_of", "@rt_print_mixed_value"} {
		if strings.Contains(ir, banned) {
			t.Fatalf("the foldable field went to the runtime door (%s); the fold owns this shape:\n%s", banned, ir)
		}
	}
	if out := compiledOut(t, "print(f\"{[1, 2, 3][1]}\")\n"); out != "2\n" {
		t.Fatalf("the folded field printed %q, the reference prints \"2\\n\"", out)
	}
}

// What the field still cannot do, pinned rather than wished away: a FORMAT SPEC over a slot the compiler
// cannot see the value of. ADR 0299 built the spec engine and chose to refuse where it cannot read the value
// at compile time; the row keeps that refusal honest — the missing half named, and never a blank or a
// payload printed at exit 0 as if it were formatted. The blank is the interesting failure: the spec engine
// read a field it did not recognise as an empty TEXT and aligned it, so `x = 5; print(f"{x:>6}")` wrote six
// spaces where CPython writes `     5` (roadmap Gap R.60 / L12.8, Gap R.292, ADR 0358).
func TestAFormatSpecOverASlotStillRefusesAndSaysSo(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"a_float_spec_over_a_dict_entry", "d = {1: 3.5}\nprint(f\"{d[1]:.2f}\")\n"},
		{"a_width_spec_over_a_list_slot", "xs = [1.5]\nprint(f\"{xs[0]:>6}\")\n"},
		{"a_width_spec_over_a_name", "x = 5\nprint(f\"{x:>6}\")\n"},
		{"a_width_spec_over_a_call", "print(f\"{max(1, 2):>6}\")\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, tc.src)
			if !refused {
				t.Fatalf("%s answered %q; the spec engine has no value to format, and printing blanks or the "+
					"payload is the answer ADR 0299 exists to prevent (Gap R.60 / L12.8, Gap R.292)", tc.name, out)
			}
			if !strings.Contains(out, "format spec") {
				t.Fatalf("%s refused without naming the format spec it cannot feed: %s", tc.name, out)
			}
		})
	}
}

// …and the specs the engine CAN read still answer, so the refusal above stays a refusal and does not become
// a regression dressed as safety.
func TestAFormatSpecOverAValueTheEngineReadsStillAnswers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		src  string
		want string
	}{
		{"print(f\"{5:>6}\")\n", "     5\n"},
		{"print(f\"{5.5:>8.2f}\")\n", "    5.50\n"},
		{"print(f\"{5.5:.2f}\")\n", "5.50\n"},
		{"print(f\"{'ab':>6}\")\n", "    ab\n"},
	} {
		tc := tc
		t.Run(strings.TrimSpace(tc.src), func(t *testing.T) {
			t.Parallel()
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Errorf("%s printed %q, want %q", tc.src, got, tc.want)
			}
			if want := referenceOut(t, tc.src); want != tc.want {
				t.Fatalf("the reference disagrees with this row: %q vs %q", want, tc.want)
			}
		})
	}
}

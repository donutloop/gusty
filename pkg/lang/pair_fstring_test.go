package lang

// pkg/lang/pair_fstring_test.go — an f-string FIELD asks the tag (roadmap L11.1, Gap R.146's rendering
// positions; ADR 0307, ADR 0303's one printer, ADR 0265's per-kind arithmetic door).
//
// `print(n)` has asked the tag since ADR 0303 and `print([n])` since ADR 0306, but `print(f"{n}")` kept
// refusing: the print road builds ONE printf format string, and every arm of its field chain asked the field
// for a single word — `%d` for a number, `%s` for an interned text's bytes — while a pair-bound name has two
// words and the payload means a different thing per kind (ADR 0252's closed tag set). Interpolating the
// payload alone is the Gap R.38 family with syntax around it: a text field would print its `@str_tab` index
// and a float field its box handle, at exit 0.
//
// The field now goes through the module's one tag-reading printer — the same `rt_str_of_value` that `str()`
// and `repr()` were routed through in ADR 0303 — and contributes `%s` over the bytes that printer captured.
// Two consequences worth naming:
//
//   - the field, `str(n)` and `print(n)` cannot disagree, because they are one renderer pointed at three sinks;
//   - a field that asked for `!r` asks the same door with the quote flag on, which is what `repr()` does. That
//     also ended two measured defects on this road: `f"{x!r}"` over a VARIABLE answered the empty string at
//     exit 0 (the compile-time spec engine returns "" for a field it cannot see, and an empty answer with the
//     exit code of success is the outcome this repo refuses to keep), and `print(f"{'a'!r}")` spent the
//     contract's forbidden exit 2 splicing quote characters into a format-string global (Gap R.192).
//
// The CLI comparison against CPython lives in integration/pair_fstring_test.go.

import (
	"strings"
	"testing"
)

// TestAPairBoundNameFillsAnFStringField is the row this cycle exists for. Every shape is an f-string whose
// field is a name the pair road bound, and every answer is the record's, which is CPython's.
func TestAPairBoundNameFillsAnFStringField(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"an int slot", "xs = []\nxs.append(7)\nn = xs[0]\nprint(f\"{n}\")\n", "7\n"},
		{"the field inside text", "xs = []\nxs.append(7)\nn = xs[0]\nprint(f\"[{n}]\")\n", "[7]\n"},
		{"the field twice", "xs = []\nxs.append(7)\nn = xs[0]\nprint(f\"{n}-{n}\")\n", "7-7\n"},
		{"a text slot", "sb = []\nsb.append(\"a\")\nn = sb[0]\nprint(f\"{n}\")\n", "a\n"},
		{
			// The failure mode this whole file is written against: without the tag the field prints
			// the interned index of "a" — a number, at exit 0, inside a sentence.
			"a text slot between literals",
			"sb = []\nsb.append(\"a\")\nn = sb[0]\nprint(f\"x{n}y\")\n", "xay\n",
		},
		{"a float slot", "xs = []\nxs.append(2.5)\nn = xs[0]\nprint(f\"{n}\")\n", "2.5\n"},
		{"a None slot", "xs = []\nxs.append(None)\nn = xs[0]\nprint(f\"{n}\")\n", "None\n"},
		{"a bool slot", "xs = []\nxs.append(True)\nn = xs[0]\nprint(f\"{n}\")\n", "True\n"},
		{
			"a container slot, nested",
			"xs = []\nxs.append([1, 2])\nn = xs[0]\nprint(f\"{n}\")\n", "[1, 2]\n",
		},
		{
			"a dict slot by key",
			"d = {}\nd[\"k\"] = 9\nn = d[\"k\"]\nprint(f\"{n}\")\n", "9\n",
		},
		{
			"the answer of arithmetic over a slot",
			"xs = []\nxs.append([7, 8])\nn = xs[0][0] * 2\nprint(f\"{n}\")\n", "14\n",
		},
		{
			"arithmetic inside the field",
			"xs = []\nxs.append(7)\nn = xs[0]\nprint(f\"{n - 1}\")\n", "6\n",
		},
		{
			"a negation inside the field",
			"xs = []\nxs.append(7)\nn = xs[0]\nprint(f\"{-n}\")\n", "-7\n",
		},
		{
			"a floored quotient inside the field",
			"xs = []\nxs.append(7)\nn = xs[0]\nprint(f\"{n // 2}\")\n", "3\n",
		},
		{
			"a true quotient inside the field",
			"xs = []\nxs.append(7)\nn = xs[0]\nprint(f\"{n / 2}\")\n", "3.5\n",
		},
		{
			"a comparison inside the field",
			"xs = []\nxs.append(7)\nn = xs[0]\nprint(f\"{n == 7}\")\n", "True\n",
		},
		{
			"a builtin over the field",
			"xs = []\nxs.append(7)\nn = xs[0]\nprint(f\"{round(n / 2)}\")\n", "4\n",
		},
		{
			"the loop variable",
			"xs = []\nxs.append(5)\nfor v in xs:\n    print(f\"v={v}\")\n", "v=5\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			RecordedStdoutIs(t, tc.src, tc.want)
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the reference answers: %v", err)
			}
			if got := strings.TrimSuffix(res.Output, "\n"); got != strings.TrimSuffix(tc.want, "\n") {
				t.Fatalf("compiled printed %q, want %q", got, strings.TrimSuffix(tc.want, "\n"))
			}
		})
	}
}

// TestAnFStringConversionAsksThePrinterIs the half that answers a conversion instead of throwing it away.
// The conversion used to reach the compile-time spec engine, which returns the empty text for any field it
// cannot see: every `!r` and `!s` over a VARIABLE printed nothing at exit 0, and a text LITERAL with `!r`
// emitted a format-string global `llc` rejected (Gap R.192). `!r` of a value is what `repr()` is, so the
// field asks the same door with the quote flag.
func TestAnFStringConversionAsksThePrinter(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"repr of a text variable", "s = \"a\"\nprint(f\"{s!r}\")\n", "'a'\n"},
		{"str of a text variable", "s = \"a\"\nprint(f\"{s!s}\")\n", "a\n"},
		{"repr of an int variable", "x = 7\nprint(f\"{x!r}\")\n", "7\n"},
		{"str of an int variable", "x = 7\nprint(f\"{x!s}\")\n", "7\n"},
		{"repr of a float variable", "x = 2.5\nprint(f\"{x!r}\")\n", "2.5\n"},
		{
			"repr of a text slot read",
			"sb = []\nsb.append(\"a\")\nn = sb[0]\nprint(f\"{n!r}\")\n", "'a'\n",
		},
		{
			"str of a text slot read",
			"sb = []\nsb.append(\"a\")\nn = sb[0]\nprint(f\"{n!s}\")\n", "a\n",
		},
		{
			"repr of an int slot read",
			"xs = []\nxs.append(7)\nn = xs[0]\nprint(f\"{n!r}\")\n", "7\n",
		},
		{
			// The exit-2 shape: quoting a text literal inside the format string emitted
			// `@.fmt1 = private constant [0 x i8]` against a `[4 x i8]` use.
			"repr of a text literal field", "print(f\"{'a'!r}\")\n", "'a'\n",
		},
		{"str of a text literal field", "print(f\"{'a'!s}\")\n", "a\n"},
		{
			"repr inside brackets",
			"s = \"hi\"\nprint(f\"[{s!r}]\")\n", "['hi']\n",
		},
		{
			"a format spec still goes to the spec engine",
			"print(f\"{3.5:.2f}\")\n", "3.50\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			RecordedStdoutIs(t, tc.src, tc.want)
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the reference answers: %v", err)
			}
			if got := strings.TrimSuffix(res.Output, "\n"); got != strings.TrimSuffix(tc.want, "\n") {
				t.Fatalf("compiled printed %q, want %q", got, strings.TrimSuffix(tc.want, "\n"))
			}
		})
	}
}

// TestAnFStringFieldStillRefusesWhatHasNoReading is this cycle's honest half. A field whose operator the
// arithmetic door will not vouch for (`n + 1`, whose slots could be text), and a field asked for a format
// SPEC over a value the module cannot see, both keep the sentence they have always printed — they name the
// missing half and who owes it, and the field never answers the payload alone.
func TestAnFStringFieldStillRefusesWhatHasNoReading(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"a product the door will not prove", "xs = []\nxs.append(7)\nn = xs[0]\nprint(f\"{n + 1}\")\n"},
		{"a modulo over a slot", "xs = []\nxs.append(7)\nn = xs[0]\nprint(f\"{n % 3}\")\n"},
		{"a format spec over a slot", "xs = []\nxs.append(7)\nn = xs[0]\nprint(f\"{n:>.2f}\")\n"},
		{"an f-string used as a value", "xs = []\nxs.append(7)\nn = xs[0]\ns = f\"x{n}\"\nprint(s)\n"},
		{"an f-string concatenated", "xs = []\nxs.append(7)\nn = xs[0]\nprint(f\"{n}\" + f\"{n}\")\n"},
		{"a method on an f-string", "xs = []\nxs.append(7)\nn = xs[0]\nprint(f\"v={n}\".upper())\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			CompiledRefusal(t, tc.src, "roadmap")
		})
	}
}

// TestTheFStringFieldRoutesThroughTheOnePrinter is the IR half of ADR 0303's rule as applied to a field: the
// rendering must come from the module's single tag-reading printer, not a second renderer built for f-strings.
func TestTheFStringFieldRoutesThroughTheOnePrinter(t *testing.T) {
	src := "xs = []\nxs.append(2.5)\nn = xs[0]\nprint(f\"[{n}]\")\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("refused a program the reference prints: %v", err)
	}
	for _, want := range []string{"call i32 @rt_str_of_value(", "call void @rt_print_mixed_value(", "call i8* @rt_str_ptr("} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("the field never asked %s — the f-strings have grown their own renderer", want)
		}
	}
	// A field the printer rendered is handed to printf as BYTES. The module's runtime helpers keep their own
	// snprintf formats, so the check is scoped to the program's own lines — everything after the field asked
	// the printer. A `%d`/`i32 %` operand there would mean the field's payload (the raw box handle of 2.5)
	// went to printf beside the text: the wrong answer this cycle exists to keep impossible (Gap R.38).
	body := res.IR[strings.Index(res.IR, "call i32 @rt_str_of_value("):]
	for _, line := range strings.Split(body, "\n") {
		if !strings.Contains(line, "@printf(") {
			continue
		}
		if strings.Contains(line, ", i32 %") || strings.Contains(line, "%d") {
			t.Errorf("a pair field reached printf as a number, not as the printer's text:\n%s", strings.TrimSpace(line))
		}
	}
}

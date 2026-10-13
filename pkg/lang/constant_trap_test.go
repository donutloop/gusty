package lang

import (
	"strings"
	"testing"
)

// Gap R.37, ADR 0326: the constant fold used to refuse, or answer, what the reference raises. A program
// whose only mistake was a wrong literal index — `[][0]`, `"abc"[7]`, `{1: 2}[3]` — was a compile error
// at the exit code reserved for "your program is wrong", and once the refusal was lifted the same
// programs were answered (`float("x")` → 0.0, `float(None)` → 0.0, `ord("ab")` → 97). These tests pin
// the door those rows open: a trap the program cannot avoid executing is emitted as a trap, and the
// runtime raises it — the class the reference raises, the sentence the record holds, catchable by an
// `except` and never a refusal.

// constantTrapRecordLeg asks the record what a program's answer is and asserts the compiled backend
// gives the same raise. The record leg is the one that decides the *sentence*: it is the retired
// engine's own wording (`index out of range`, not CPython's `list index out of range`) and a fold that
// invented a second sentence would split one trap into two wordings.
func constantTrapRecordLeg(t *testing.T, src, wantMsg string) {
	t.Helper()
	want := goldenLookup(t, src)
	if want.Err == "" {
		t.Fatalf("the record holds no raise for %q (it says %q) — the row is not a trap case", src, want.Err)
	}
	if want.Err != wantMsg {
		t.Fatalf("the record says %q for %q, the test expected %q", want.Err, src, wantMsg)
	}
	res, err := JIT(src, 0)
	if err != nil {
		t.Fatalf("compiling %q: %v", src, err)
	}
	if res.Code != 3 {
		t.Fatalf("%q exited %d where a trap is the answer (ADR 0166); stdout %q stderr %q", src, res.Code, res.Output, res.Stderr)
	}
	if !strings.Contains(res.Stderr, wantMsg) {
		t.Fatalf("%q did not raise %q; stderr %q", src, wantMsg, res.Stderr)
	}
	if strings.Contains(res.Stderr, "codegen:") {
		t.Fatalf("%q was refused rather than run: %s", src, res.Stderr)
	}
}

// constantTrapReferenceLeg asks CPython, and requires the compiled backend to agree class *and*
// sentence. Where the two languages differ in words (the list-index rows, Gap R.90) the row goes to
// constantTrapBothLegs with its own wording instead.
func constantTrapReferenceLeg(t *testing.T, src, wantMsg string) {
	t.Helper()
	_, refErr, refExecErr := PythonRun(src)
	if refExecErr == nil {
		t.Fatalf("CPython answered %q where a raise of %q was the point", src, wantMsg)
	}
	if !strings.Contains(refErr, wantMsg) {
		t.Fatalf("CPython does not raise %q for %q; stderr %q", wantMsg, src, refErr)
	}
	res, err := JIT(src, 0)
	if err != nil {
		t.Fatalf("compiling %q: %v", src, err)
	}
	if res.Code != 3 {
		t.Fatalf("%q exited %d where a trap is the answer (ADR 0166); stdout %q stderr %q", src, res.Code, res.Output, res.Stderr)
	}
	if !strings.Contains(res.Stderr, wantMsg) {
		t.Fatalf("%q did not raise %q; stderr %q", src, wantMsg, res.Stderr)
	}
	if strings.Contains(res.Stderr, "codegen:") {
		t.Fatalf("%q was refused rather than run: %s", src, res.Stderr)
	}
}

// constantTrapBothLegs is the wording-debt shape: the reference raises one sentence, this language
// raises another of the same class. The class and the exit code are pinned exactly; the sentences are
// written out per row, so paying Gap R.90 has to break this test rather than pass it silently.
func constantTrapBothLegs(t *testing.T, src, ourMsg, refMsg string) {
	t.Helper()
	_, refErr, refExecErr := PythonRun(src)
	if refExecErr == nil {
		t.Fatalf("CPython answered %q where a raise was the point", src)
	}
	if !strings.Contains(refErr, refMsg) {
		t.Fatalf("CPython does not raise %q for %q; stderr %q", refMsg, src, refErr)
	}
	res, err := JIT(src, 0)
	if err != nil {
		t.Fatalf("compiling %q: %v", src, err)
	}
	if res.Code != 3 {
		t.Fatalf("%q exited %d where a trap is the answer (ADR 0166); stdout %q stderr %q", src, res.Code, res.Output, res.Stderr)
	}
	if !strings.Contains(res.Stderr, ourMsg) {
		t.Fatalf("%q did not raise %q; stderr %q", src, ourMsg, res.Stderr)
	}
	if strings.Contains(res.Stderr, "codegen:") {
		t.Fatalf("%q was refused rather than run: %s", src, res.Stderr)
	}
}

// TestAConstantTrapRaisesWhatTheRecordRaises is the record leg: positions, keys and conversions in three
// classes, each one a program the compiled backend used to refuse at compile time. The rows are in the
// record, so a deletion is a test failure rather than a coverage loss.
func TestAConstantTrapRaisesWhatTheRecordRaises(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			name: "a_position_past_the_end_of_a_literal_string",
			src:  `print("abc"[7])`,
			want: "IndexError: string index out of range",
		},
		{
			name: "a_key_absent_from_a_literal_dict",
			src:  "print({1:2}[3])",
			want: "KeyError: 3",
		},
		{
			name: "a_character_position_out_of_range",
			src:  `print("abc"[-4])`,
			want: "IndexError: string index out of range",
		},
		{
			name: "an_integer_written_as_nothing",
			src:  `print(int("x"))`,
			want: "ValueError: invalid literal for int() with base 10: 'x'",
		},
		{
			name: "a_number_written_with_a_space_inside",
			src:  `print(int("1 0"))`,
			want: "ValueError: invalid literal for int() with base 10: '1 0'",
		},
		{
			name: "a_number_written_in_another_base",
			src:  `print(int("0x10"))`,
			want: "ValueError: invalid literal for int() with base 10: '0x10'",
		},
		{
			name: "a_float_written_as_letters",
			src:  `print(float("x"))`,
			want: "ValueError: could not convert string to float: 'x'",
		},
		{
			name: "a_float_written_as_a_space",
			src:  `print(float(" "))`,
			want: "ValueError: could not convert string to float: ' '",
		},
		{
			name: "a_number_written_with_two_underscores",
			src:  `print(int("1__0"))`,
			// `1__0` is not a Python integer literal: an underscore has to sit between two digits.
			want: "ValueError: invalid literal for int() with base 10: '1__0'",
		},
		{
			name: "nothing_to_turn_into_a_character",
			src:  `print(ord(""))`,
			want: `TypeError: ord() expected a character, but string of length 0 found`,
		},
		{
			name: "too_much_for_one_character",
			src:  `print(ord("ab"))`,
			want: `TypeError: ord() expected a character, but string of length 2 found`,
		},
		{
			name: "a_character_number_below_the_start",
			src:  `print(chr(-1))`,
			want: "ValueError: chr() arg not in range(0x110000)",
		},
		{
			name: "a_character_number_above_a_surrogate",
			src:  `print(chr(1114112))`,
			want: "ValueError: chr() arg not in range(0x110000)",
		},
		{
			name: "a_list_asked_to_be_a_number",
			src:  `print(int([1]))`,
			want: "TypeError: int() argument must be a string, a bytes-like object or a real number, not 'list'",
		},
		{
			name: "a_dict_asked_to_be_a_float",
			src:  `print(float({"a":1}))`,
			want: `TypeError: float() argument must be a string or a real number, not 'dict'`,
		},
		{
			name: "a_set_asked_to_be_a_float",
			src:  `print(float({1}))`,
			want: `TypeError: float() argument must be a string or a real number, not 'set'`,
		},
		{
			name: "nothing_asked_to_be_a_float",
			src:  `print(float(None))`,
			want: `TypeError: float() argument must be a string or a real number, not 'NoneType'`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) { constantTrapRecordLeg(t, tc.src, tc.want) })
	}
}

// TestAConstantTrapRaisesWhatTheReferenceRaises covers the rows the record cannot hold: the list-index
// ones, where this language's sentence differs from CPython's (Gap R.90) so the recorder refuses to
// write an entry the two legs do not agree on. The class, the raise and the exit code are still pinned
// against the reference — only the words are owed, and by one row that owns both roads.
func TestAConstantTrapRaisesWhatTheReferenceRaises(t *testing.T) {
	for _, tc := range []struct {
		name, src, ours, theirs string
	}{
		{
			name:   "a_position_past_the_end_of_a_literal_list",
			src:    "print([][0])",
			ours:   "IndexError: index out of range",
			theirs: "IndexError: list index out of range",
		},
		{
			name:   "a_position_past_the_end_of_a_filled_literal_list",
			src:    "print([1,2][5])",
			ours:   "IndexError: index out of range",
			theirs: "IndexError: list index out of range",
		},
		{
			name:   "a_negative_position_below_the_start",
			src:    "print([1,2][-3])",
			ours:   "IndexError: index out of range",
			theirs: "IndexError: list index out of range",
		},
		// The same trap under the other constant read that used to refuse: a comprehension's object.
		// The other one — a call result subscripted (`build()[7]`) — is still a refusal, and lives in
		// TestTheCallResultStillRefuses; it is the container-source gap, not the trap door.
		{
			name:   "a_position_below_a_comprehension",
			src:    "xs = [x for x in [1, 2]]\nprint(xs[9])",
			ours:   "IndexError: index out of range",
			theirs: "IndexError: list index out of range",
		},
		{
			name:   "a_position_below_a_method_body",
			src:    "class C:\n    def m(self):\n        return [][0]\nprint(C().m())",
			ours:   "IndexError: index out of range",
			theirs: "IndexError: list index out of range",
		},
	} {
		t.Run(tc.name, func(t *testing.T) { constantTrapBothLegs(t, tc.src, tc.ours, tc.theirs) })
	}
}

// TestANumberWiderThanTheWordIsStillAnHonestRefusal is the one row the door does not open. The reference
// has an unbounded int; this backend has a 64-bit slot, so folding
// `int("99999999999999999999999999")` would answer a plausible wrong number where the reference answers
// the true one. That is the bounded-versus-arbitrary decision roadmap L12.12 is waiting for, and until
// it is made the compiler names the limit instead of either trapping (which would be the wrong error —
// the number is fine, the slot is not) or answering it.
func TestANumberWiderThanTheWordIsStillAnHonestRefusal(t *testing.T) {
	const src = `print(int("99999999999999999999999999"))`
	_, err := JIT(src, 0)
	if err == nil {
		t.Fatalf("%q compiled, where the fold should name the word it cannot hold", src)
	}
	for _, want := range []string{"wider than this backend's word", "L12.12"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal %q does not name %q", err.Error(), want)
		}
	}
}

// TestTheCallResultStillRefuses keeps the neighbouring gap visible rather than quietly folded into this
// one: `build()[7]` cannot be read as a constant because the compiler does not know which list `build()`
// hands back — that is the container-source row, and this row only pins that it stays an honest refusal
// naming the shape it could not describe.
func TestTheCallResultStillRefuses(t *testing.T) {
	const src = "def build():\n    return [1, 2]\nprint(build()[7])"
	_, err := JIT(src, 0)
	if err == nil {
		t.Fatalf("%q compiled, where the door should refuse and name the shape", src)
	}
	if !strings.Contains(err.Error(), "index requires an inline list/dict/set literal") {
		t.Fatalf("the refusal %q does not name the shape it could not read", err.Error())
	}
}

// TestTheConversionFoldsAnswerRatherThanTrap pins the other side of the door. A literal that *does*
// spell a number answers — including the spellings CPython accepts and a naive parser drops — and the
// no-argument forms answer 0 and 0.0. A trap that fires on a good literal is as wrong as a silent 0 on
// a bad one.
func TestTheConversionFoldsAnswerRatherThanTrap(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{src: `print(int(" 12 "))`, want: "12"},
		{src: `print(int("+42"))`, want: "42"},
		{src: `print(int("-7"))`, want: "-7"},
		{src: `print(int("1_0"))`, want: "10"},
		{src: `print(int(True))`, want: "1"},
		{src: `print(int(False))`, want: "0"},
		{src: `print(int())`, want: "0"},
		{src: `print(float("1_0.5"))`, want: "10.5"},
		{src: `print(float(True))`, want: "1.0"},
		{src: `print(float())`, want: "0.0"},
	} {
		t.Run(tc.src, func(t *testing.T) {
			got := strings.TrimSpace(captureStdout(t, tc.src))
			if got != tc.want {
				t.Fatalf("%q printed %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}

// TestAConstantTrapIsCatchable is the half the refusal could never have: the trap is an ordinary raise,
// so an `except` in the same function catches it, prints what it caught, and the program carries on.
// A compile error has no such second chance — and a program that dies at compile time cannot be
// debugged by wrapping its reads, which is how people fix an index.
func TestAConstantTrapIsCatchable(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			name: "index_error_is_catchable",
			src:  "try:\n    print([][0])\nexcept IndexError:\n    print(\"caught\")\nprint(\"after\")",
			want: "caught\nafter",
		},
		{
			name: "string_index_error_is_catchable",
			src:  "try:\n    print(\"abc\"[7])\nexcept IndexError:\n    print(\"caught\")\nprint(\"after\")",
			want: "caught\nafter",
		},
		{
			name: "key_error_is_catchable",
			src:  "try:\n    print({1:2}[3])\nexcept KeyError:\n    print(\"caught\")\nprint(\"after\")",
			want: "caught\nafter",
		},
		{
			name: "value_error_is_catchable",
			src:  "try:\n    print(int(\"x\"))\nexcept ValueError:\n    print(\"caught\")\nprint(\"after\")",
			want: "caught\nafter",
		},
		{
			name: "type_error_is_catchable",
			src:  "try:\n    print(ord(\"ab\"))\nexcept TypeError:\n    print(\"caught\")\nprint(\"after\")",
			want: "caught\nafter",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.TrimRight(captureStdout(t, tc.src), "\n")
			if got != tc.want {
				t.Fatalf("%q printed %q, want %q", tc.src, got, tc.want)
			}
			// CPython agrees, to the line: a caught trap is an ordinary path through the program.
			refOut, refErr, refExecErr := PythonRun(tc.src)
			if refExecErr != nil {
				t.Fatalf("CPython failed on %q: %v (%s)", tc.src, refExecErr, refErr)
			}
			if strings.TrimRight(refOut, "\n") != tc.want {
				t.Fatalf("CPython printed %q, this language prints %q", strings.TrimRight(refOut, "\n"), tc.want)
			}
		})
	}
}

// TestAConstantTrapEndsItsBlock cleanly is the verifier half of the row. `raiseTo` ends the current
// basic block, so a fold that raises and returns has to open a fresh one — otherwise whatever the
// caller emits next lands after a terminator and the module never verifies (ADR 0166's exit 2). These
// programs keep using values after the trap, in the same block.
func TestAConstantTrapEndsItsBlockCleanly(t *testing.T) {
	for _, src := range []string{
		`x = 1
y = [1,2][5]
print(x + 1)`,
		`try:
    print(int("x"))
except ValueError:
    pass
print("done")`,
		`def f():
    return "ab"[9]
try:
    f()
except IndexError:
    print("caught")
print("done")`,
	} {
		t.Run(src, func(t *testing.T) {
			// JIT is the verifier's customer: it refuses a module the verifier rejects, and the
			// toolchain test suite additionally checks every emitted module passes `opt -passes=verify`.
			if _, err := JIT(src, 0); err != nil {
				t.Fatalf("compiling %q: %v", src, err)
			}
		})
	}
}

// TestTheTrapDoorSpeaksTheSharedSentenceTable is the regression behind the wording. The five constant
// read roads and their dynamic counterparts raise from one table, so a fold cannot have invented a
// second phrasing of its own — the failure mode the row had was a refusal, and a fold that raised with
// a sentence only it used would have traded a refusal for a divergence.
func TestTheTrapDoorSpeaksTheSharedSentenceTable(t *testing.T) {
	if got, want := listIndexOutOfRange(), "index out of range"; got != want {
		t.Fatalf("listIndexOutOfRange() = %q, want %q", got, want)
	}
	if got, want := stringIndexOutOfRange(), "string index out of range"; got != want {
		t.Fatalf("stringIndexOutOfRange() = %q, want %q", got, want)
	}
	// The conversion sentences are CPython's, and the reference leg above is what proves it — these
	// two lines only pin that the door reads them from one place.
	if got, want := intLiteralSentence("x"), "invalid literal for int() with base 10: 'x'"; got != want {
		t.Fatalf("intLiteralSentence(\"x\") = %q, want %q", got, want)
	}
	if got, want := chrSentence(), "chr() arg not in range(0x110000)"; got != want {
		t.Fatalf("chrSentence() = %q, want %q", got, want)
	}
}

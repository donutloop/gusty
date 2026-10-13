package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Gap R.37, ADR 0326 at the command line. The symptom these rows pin is an exit code: a program whose
// only mistake was a wrong literal index, a missing key or a text that does not spell a number used to
// leave `gustyc` at exit 1 with a `codegen:` sentence — the class the exit-code contract reserves for
// "your program is wrong", issued before the program had run a single instruction, and with it the
// program's own `except` taken away. Through the CLI the same programs now exit 3 with a Python-shaped
// traceback, exactly as the reference does, and the ones that catch their trap exit 0 with the answer.

// TestAConstantTrapExitsAsATrapThroughTheCLI is the exit-code half. Each row is a program the reference
// dies on; each has to die the same way here — not at the compile door, not at the verifier's exit 2,
// and not at 0 with a silent 0 where an error belongs.
func TestAConstantTrapExitsAsATrapThroughTheCLI(t *testing.T) {
	for _, tc := range []struct {
		name, src, msg string
		// theirs is what the reference raises. It is written separately from `msg` only where the two
		// languages differ in words, which is the list-index family: this language says
		// `index out of range` and the reference says `list index out of range` (roadmap Gap R.90) — a
		// wording debt that one row owns, for the constant road and the dynamic one together.
		theirs string
	}{
		{
			name: "a_position_past_the_end_of_a_literal_list",
			src:  "print([][0])\n",
			// ADR 0330 moved the sentence to the reference's, so the two columns agree — which is
			// the assertion now (roadmap Gap R.223).
			msg:    "IndexError: list index out of range",
			theirs: "IndexError: list index out of range",
		},
		{
			name: "a_position_past_the_end_of_a_literal_string",
			src:  "print(\"abc\"[7])\n",
			msg:  "IndexError: string index out of range",
		},
		{
			name: "a_key_absent_from_a_literal_dict",
			src:  "print({1: 2}[3])\n",
			msg:  "KeyError: 3",
		},
		{
			name: "a_text_that_does_not_spell_a_number",
			src:  "print(int(\"abc\"))\n",
			msg:  "ValueError: invalid literal for int() with base 10: 'abc'",
		},
		{
			name: "a_text_that_does_not_spell_a_float",
			src:  "print(float(\"zzz\"))\n",
			msg:  "ValueError: could not convert string to float: 'zzz'",
		},
		{
			name: "a_container_asked_to_be_a_number",
			src:  "print(float([1, 2]))\n",
			msg:  "TypeError: float() argument must be a string or a real number, not 'list'",
		},
		{
			name: "nothing_asked_to_be_a_float",
			src:  "print(float(None))\n",
			msg:  "TypeError: float() argument must be a string or a real number, not 'NoneType'",
		},
		{
			name: "a_string_of_two_characters_asked_for_its_code_point",
			src:  "print(ord(\"ab\"))\n",
			msg:  "TypeError: ord() expected a character, but string of length 2 found",
		},
		{
			name: "a_character_number_above_the_largest_code_point",
			src:  "print(chr(1114112))\n",
			msg:  "ValueError: chr() arg not in range(0x110000)",
		},
		{
			name: "a_position_below_a_comprehension",
			src:  "xs = [x for x in [1, 2]]\nprint(xs[9])\n",
			// ADR 0330 moved the sentence to the reference's, so the two columns agree — which is
			// the assertion now (roadmap Gap R.223).
			msg:    "IndexError: list index out of range",
			theirs: "IndexError: list index out of range",
		},
		{
			name: "a_position_out_of_range_inside_a_method",
			src:  "class C:\n    def m(self):\n        return [][0]\nc = C()\nprint(c.m())\n",
			msg:  "IndexError: list index out of range",
			// The reference raises the same class from the same place, and since ADR 0330 the
			// same words too (roadmap Gap R.223).
			theirs: "IndexError: list index out of range",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			theirs := tc.theirs
			if theirs == "" {
				theirs = tc.msg
			}
			path := writeSrc(t, t.TempDir(), "const_trap.gy", tc.src)
			want, pyCode := oracleTrap(t, path)
			if !strings.Contains(want, theirs) {
				t.Fatalf("the reference does not raise %q, it says %q", theirs, want)
			}
			if pyCode == 0 {
				t.Fatalf("the reference exited 0 on a program meant to trap:\n%s", want)
			}
			for _, engine := range cliEngines {
				combined := cliRun(t, engine, path)
				_, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Errorf("%s exited 2 (a compiler bug, ADR 0166) on a program that should trap:\n%s", engine, combined)
				}
				if code == 1 {
					t.Errorf("%s exited 1 (the program-is-wrong class, issued before the program ran) on a program the reference runs and traps: %s", engine, combined)
				}
				if code != 3 {
					t.Errorf("%s exited %d where a trap is the answer (ADR 0166): %s", engine, code, combined)
				}
				if !strings.Contains(combined, "Traceback (most recent call last):") {
					t.Errorf("%s printed no traceback:\n%s", engine, combined)
				}
				if !strings.Contains(combined, tc.msg) {
					t.Errorf("%s did not raise %q:\n%s", engine, tc.msg, combined)
				}
				if strings.Contains(combined, "codegen:") {
					t.Errorf("%s refused what the reference traps:\n%s", engine, combined)
				}
			}
		})
	}
}

// TestTheCatchableConstantTrapsCarryTheProgramOn is the reason the door matters to a person rather than
// only to an exit-code table: wrapping the read in an `except` is how someone finds out which index was
// wrong. A compile error cannot be wrapped, so the whole debugging move was unavailable — and these
// programs print, carry on, and exit 0, exactly as they do under the reference.
func TestTheCatchableConstantTrapsCarryTheProgramOn(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			name: "an_index_that_isnt",
			src:  "try:\n    print([1, 2][5])\nexcept IndexError:\n    print(\"no fifth element\")\nprint(\"still here\")\n",
			want: "no fifth element\nstill here\n",
		},
		{
			name: "a_key_that_isnt",
			src:  "try:\n    print({1: 1}[2])\nexcept KeyError:\n    print(\"no such key\")\nprint(\"still here\")\n",
			want: "no such key\nstill here\n",
		},
		{
			name: "a_number_that_never_was",
			src:  "try:\n    print(int(\"12 x\"))\nexcept ValueError:\n    print(\"not a number\")\nprint(42)\n",
			want: "not a number\n42\n",
		},
		{
			name: "a_character_that_was_two",
			src:  "try:\n    print(ord(\"ab\"))\nexcept TypeError:\n    print(\"not one character\")\nprint(7)\n",
			want: "not one character\n7\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "const_catch.gy", tc.src)
			pyOut, pyCode := oracleTrap(t, path)
			if pyCode != 0 {
				t.Fatalf("the reference exited %d on a program meant to be caught:\n%s", pyCode, pyOut)
			}
			if pyOut != tc.want {
				t.Fatalf("the reference prints %q, want %q", pyOut, tc.want)
			}
			compiled := compiledRun(t, tc.src)
			if compiled.code != 0 {
				t.Errorf("the compiled backend exited %d on a program the reference catches (stdout %q, stderr %q)", compiled.code, compiled.stdout, compiled.stderr)
			}
			if compiled.stdout != tc.want {
				t.Errorf("the compiled backend printed %q, the reference prints %q", compiled.stdout, tc.want)
			}
			if strings.Contains(compiled.stderr, "codegen:") {
				t.Errorf("the compiled backend refused a program the reference runs:\n%s", compiled.stderr)
			}
			aot := aotRun(t, tc.src)
			if aot.code != 0 || aot.stdout != tc.want {
				t.Errorf("--build exited %d and printed %q, want 0 and %q", aot.code, aot.stdout, tc.want)
			}
		})
	}
}

// TestTheConstantTrapProgramIsAnAssertedMatrixRow keeps the corpus honest: the program the cycle added
// has to be a row that asserts and matches, not one the matrix shrugs at. A family pinned only in unit
// tests is a family that can regress in the CLI, which is where the symptom was.
func TestTheConstantTrapProgramIsAnAssertedMatrixRow(t *testing.T) {
	matrix, err := os.ReadFile(filepath.Join(recordRoot, "integration", "conformance-matrix.json"))
	if err != nil {
		t.Fatalf("read the matrix (run `go test ./integration` once to write it): %v", err)
	}
	var m struct {
		Results []struct {
			Case struct {
				Name     string `json:"name"`
				Asserted bool   `json:"asserted"`
				Kind     string `json:"kind"`
			} `json:"case"`
			Conformant bool `json:"conformant"`
			AOTOK      bool `json:"aot_ok"`
			PythonOK   bool `json:"python_ok"`
		} `json:"results"`
	}
	if err := json.Unmarshal(matrix, &m); err != nil {
		t.Fatalf("the matrix does not parse: %v", err)
	}
	const want = "constant_trap.gy"
	for _, r := range m.Results {
		if r.Case.Name != want {
			continue
		}
		if !r.Case.Asserted {
			t.Errorf("%s is in the matrix but not asserted — a program the reference answers is parity surface, "+
				"not a row that records an agreement (roadmap Gap R.37, ADR 0326)", want)
		}
		if !r.Conformant || !r.AOTOK || !r.PythonOK {
			t.Errorf("%s is not conformant (conformant=%v aot_ok=%v python_ok=%v)", want, r.Conformant, r.AOTOK, r.PythonOK)
		}
		return
	}
	t.Fatalf("%s is not in the conformance matrix at all", want)
}

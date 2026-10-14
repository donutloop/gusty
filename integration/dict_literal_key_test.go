package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Gap R.215, ADR 0327 at the command line. The symptom was an exit code: `gustyc --file` on
// `print({"a": 1}["a"])` reported the program-is-wrong class (exit 1) for a program CPython answers with
// `1`, and `print({"a": 1}["b"])` reported the same class for a program CPython dies on with
// `KeyError: 'b'`. Both were a gate in the subscript road that let a *name* reach the object and condemned
// a *literal* to the fold; through the CLI the answers are now the reference's and the missing key is a
// trap the program's own `except KeyError:` catches.

// TestADictLiteralReadsItsEntryThroughTheCLI is the answering half. Each row is asserted three ways: what
// the reference prints, what the compiled leg prints through the CLI, and what the `--aot` pipeline prints
// after `llc`/`opt` have had their say about the module.
func TestADictLiteralReadsItsEntryThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{
			name: "a_text_key",
			src:  `print({"a": 1}["a"])` + "\n",
			want: "1\n",
		},
		{
			name: "an_entry_whose_value_is_text",
			src:  `print({"a": "x"}["a"])` + "\n",
			want: "x\n",
		},
		{
			name: "an_entry_whose_value_is_a_float",
			src:  "print({1: 1.5}[1])\n",
			want: "1.5\n",
		},
		{
			name: "an_entry_whose_value_is_nothing",
			src:  "print({1: None}[1])\n",
			want: "None\n",
		},
		{
			name: "a_bool_key_asks_the_number_entry",
			src:  `print({1: "a"}[True])` + "\n",
			want: "a\n",
		},
		{
			name: "a_none_key",
			src:  "print({None: 5}[None])\n",
			want: "5\n",
		},
		{
			name: "an_entry_whose_value_is_a_container",
			src:  `print({"a": [1, 2]}["a"])` + "\n",
			want: "[1, 2]\n",
		},
		{
			name: "an_entry_read_through_two",
			src:  `print({"a": {"b": 3}}["a"]["b"])` + "\n",
			want: "3\n",
		},
		{
			name: "an_entry_bound_then_printed",
			src:  "x = {\"a\": \"x\"}[\"a\"]\nprint(x)\n",
			want: "x\n",
		},
		{
			name: "an_entry_measured",
			src:  `print(len({"a": [1, 2]}["a"]))` + "\n",
			want: "2\n",
		},
		{
			name: "an_entry_looped_over",
			src:  "for v in {\"a\": [1, 2]}[\"a\"]:\n    print(v)\n",
			want: "1\n2\n",
		},
		{
			name: "an_entry_rendered_by_str_and_repr",
			src:  `print(str({"a": "x"}["a"]), repr({"a": "x"}["a"]))` + "\n",
			want: "x 'x'\n",
		},
		{
			name: "a_key_the_fold_cannot_read_over_a_dict_of_numbers",
			src:  `print({1: 2, "a": 3}["a"])` + "\n",
			want: "3\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "dict_literal_key.gy", tc.src)
			refOut, refCode := oracleTrap(t, path)
			if refCode != 0 {
				t.Fatalf("the reference exited %d on a program meant to answer:\n%s", refCode, refOut)
			}
			if refOut != tc.want {
				t.Fatalf("the reference prints %q, want %q", refOut, tc.want)
			}
			for _, engine := range cliEngines {
				combined := cliRun(t, engine, path)
				if _, code := cliRunCode(t, engine, path); code != 0 {
					t.Errorf("%s exited %d on a program the reference answers: %s", engine, code, combined)
				}
				if strings.Contains(combined, "codegen:") {
					t.Errorf("%s refused a program the reference answers:\n%s", engine, combined)
				}
				if strings.TrimSuffix(combined, "\n") != strings.TrimSuffix(tc.want, "\n") {
					t.Errorf("%s printed %q, the reference prints %q", engine, combined, tc.want)
				}
			}
			compiled := compiledRun(t, tc.src)
			if compiled.code != 0 || compiled.stdout != tc.want {
				t.Errorf("the record leg printed %q (exit %d), the reference prints %q — stderr %q",
					compiled.stdout, compiled.code, tc.want, compiled.stderr)
			}
		})
	}
}

// TestAMissingKeyOfADictLiteralExitsAsATrapThroughTheCLI is the exit-code half. An absent key is a fact
// about the program's execution, so it belongs in the runtime-error class with a traceback and a reachable
// `except` — not in the compile door, which is where it had been since the constant read was written.
func TestAMissingKeyOfADictLiteralExitsAsATrapThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, msg string }{
		{
			name: "a_text_key_the_literal_does_not_hold",
			src:  `print({"a": 1}["b"])` + "\n",
			msg:  "KeyError: 'b'",
		},
		{
			name: "a_number_key_a_dict_of_texts_does_not_hold",
			src:  `print({"a": 1}[1])` + "\n",
			msg:  "KeyError: 1",
		},
		{
			name: "a_text_key_a_dict_of_numbers_does_not_hold",
			src:  `print({1: 2}["a"])` + "\n",
			msg:  "KeyError: 'a'",
		},
		{
			// The fold could read this literal's values but not its key, so the fold must not be asked:
			// a lookup of a text key in an integer table answers `2` where the reference raises.
			name: "a_text_key_over_a_literal_whose_values_the_fold_could_read",
			src:  `print({0: 2}["a"])` + "\n",
			msg:  "KeyError: 'a'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "dict_literal_trap.gy", tc.src)
			refOut, refCode := oracleTrap(t, path)
			if refCode == 0 {
				t.Fatalf("the reference answered %q where %q is the point:\n%s", tc.src, tc.msg, refOut)
			}
			if !strings.Contains(refOut, tc.msg) {
				t.Fatalf("the reference does not raise %q: %s", tc.msg, refOut)
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

// TestTheLiteralDictsKeyErrorIsCaughtThroughTheCLI is the part an exit-code table cannot show: the same
// program, wrapped, prints its handler's line and the line after it, and leaves at 0. That is the debugging
// move a compile stop takes away, which is why these rows are the point of the change rather than a bonus.
func TestTheLiteralDictsKeyErrorIsCaughtThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{
			name: "a_key_that_isnt_there",
			src:  "try:\n    print({\"a\": 1}[\"b\"])\nexcept KeyError:\n    print(\"no such key\")\nprint(\"after\")\n",
			want: "no such key\nafter\n",
		},
		{
			name: "a_key_of_the_wrong_kind",
			src:  "try:\n    print({1: 2}[\"a\"])\nexcept KeyError:\n    print(\"not a number key\")\nprint(7)\n",
			want: "not a number key\n7\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "dict_literal_catch.gy", tc.src)
			refOut, refCode := oracleTrap(t, path)
			if refCode != 0 {
				t.Fatalf("the reference exited %d on a program meant to be caught:\n%s", refCode, refOut)
			}
			if refOut != tc.want {
				t.Fatalf("the reference prints %q, want %q", refOut, tc.want)
			}
			compiled := compiledRun(t, tc.src)
			if compiled.code != 0 || compiled.stdout != tc.want {
				t.Errorf("the record leg printed %q (exit %d), want %q — stderr %q", compiled.stdout, compiled.code, tc.want, compiled.stderr)
			}
			aot := aotRun(t, tc.src)
			if aot.code != 0 || aot.stdout != tc.want {
				t.Errorf("the compiled leg printed %q (exit %d), want %q — stderr %q", aot.stdout, aot.code, tc.want, aot.stderr)
			}
		})
	}
}

// TestTheOneWordPositionsOfALiteralDictReadRefuseAtTheCLI keeps the refusals honest at the exit-code
// contract: the position this row does not cover leaves at 1 with a sentence naming the missing half, never
// at 2 (a toolchain failure) and never at 0 with the payload printed as though it were the value.
func TestTheOneWordPositionsOfALiteralDictReadRefuseAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{
			name: "an_entry_handed_back_by_a_return",
			src:  "def f():\n    return {\"a\": \"x\"}[\"a\"]\nprint(f())\n",
		},
		{
			name: "an_entry_handed_to_a_call",
			src:  "def g(v):\n    print(v)\ng({\"a\": \"x\"}[\"a\"])\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "dict_literal_refuse.gy", tc.src)
			for _, engine := range cliEngines {
				combined := cliRun(t, engine, path)
				if _, code := cliRunCode(t, engine, path); code != 1 {
					t.Errorf("%s exited %d on a position whose half is missing (want the refusal class, 1):\n%s", engine, code, combined)
				}
				if !strings.Contains(combined, "codegen:") {
					t.Errorf("%s did not name what it cannot do:\n%s", engine, combined)
				}
				for _, want := range []string{"(payload, tag) pair", "one word", "Gap R.146"} {
					if !strings.Contains(combined, want) {
						t.Errorf("%s's refusal does not name %s:\n%s", engine, want, combined)
					}
				}
			}
		})
	}
}

// TestTheDictLiteralKeyProgramIsAnAssertedMatrixRow keeps the corpus honest: the program this cycle added
// has to be a row that asserts and matches on both legs, not one the matrix shrugs at.
func TestTheDictLiteralKeyProgramIsAnAssertedMatrixRow(t *testing.T) {
	t.Parallel()
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
	const want = "dict_literal_key.gy"
	for _, r := range m.Results {
		if r.Case.Name != want {
			continue
		}
		if !r.Case.Asserted {
			t.Errorf("%s is in the matrix but not asserted — a program the reference answers is parity surface, "+
				"not a row that records an agreement (roadmap Gap R.215, ADR 0327)", want)
		}
		if !r.Conformant || !r.AOTOK || !r.PythonOK {
			t.Errorf("%s is not conformant (conformant=%v aot_ok=%v python_ok=%v)", want, r.Conformant, r.AOTOK, r.PythonOK)
		}
		return
	}
	t.Fatalf("%s is not in the conformance matrix at all", want)
}

package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// Gap R.221, ADR 0353 at the command line. `gustyc --eval 'd = {None: 3.5}\nprint(d[None])'` printed
// `3` — the handle of the `@float_box` object — and the same program with an integer key printed
// `3.5`. Nothing raised and nothing refused: the read kept the entry's payload and left its kind in the
// slot, because the door that reads a self-describing dict by (payload, tag) kept a list of key
// spellings and `None`, `True` and `False` were not on it. Through the CLI the answers are now
// CPython's, the traps are traps (exit 3, not the compiler's exit 1), and the whole row is one program
// whose expectation is the text CPython printed for it.

// TestADictEntryReadByABooleanOrNoneKeyThroughTheCLI is the answering half. Each row is asserted three
// ways: what the reference prints, what the CLI prints, and what the `--aot` pipeline prints after
// `llc`/`opt` have had their say about the module.
func TestADictEntryReadByABooleanOrNoneKeyThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"a_float_entry", "d = {None: 3.5}\nprint(d[None])\n", "3.5\n"},
		{"a_zero_float_entry", "d = {None: 0.0}\nprint(d[None])\n", "0.0\n"},
		{"a_text_entry", "d = {None: \"hi\"}\nprint(d[None])\n", "hi\n"},
		{"a_none_entry", "d = {None: None}\nprint(d[None])\n", "None\n"},
		{"a_bool_entry", "d = {None: True}\nprint(d[None])\n", "True\n"},
		{"a_false_entry", "d = {False: False}\nprint(d[False])\n", "False\n"},
		{"a_list_entry", "d = {True: [1, 2]}\nprint(d[True])\n", "[1, 2]\n"},
		{"a_dict_entry", "d = {True: {\"k\": 1}}\nprint(d[True])\n", "{'k': 1}\n"},
		{"an_int_entry", "d = {False: 3}\nprint(d[False])\n", "3\n"},
		{"an_entry_bound", "d = {None: 3.5}\nx = d[None]\nprint(x)\n", "3.5\n"},
		{"an_entry_measured", "d = {None: [1, 2]}\nprint(len(d[None]))\n", "2\n"},
		{"an_entry_looped_over", "d = {False: [1, 2]}\nfor v in d[False]:\n    print(v)\n", "1\n2\n"},
		{"an_entry_rendered", "d = {None: \"hi\"}\nprint(str(d[None]), repr(d[None]))\n", "hi 'hi'\n"},
		{"an_entry_in_a_list", "d = {None: 3.5}\nprint([d[None], 1])\n", "[3.5, 1]\n"},
		{"an_entry_chosen_by_or", "d = {None: \"hi\"}\nprint(d[None] or \"z\")\n", "hi\n"},
		{"three_keys_one_dict", "d = {None: 1.5, True: \"a\", False: [2]}\nprint(d[None], d[True], d[False])\n", "1.5 a [2]\n"},
		{"a_run_built_entry", "d = {}\nd[None] = 3.5\nprint(d[None])\n", "3.5\n"},
		// A bool key is the number the reference says it is, and which entry that is is the object's answer.
		{"false_reads_the_zero_key", "d = {0: \"zero\"}\nprint(d[False])\n", "zero\n"},
		{"true_reads_the_one_key", "d = {1: \"one\"}\nprint(d[True])\n", "one\n"},
		{"the_later_write_of_one_key", "d = {True: 1.5, 1: \"one\"}\nprint(d[True])\n", "one\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "dict_none_bool_key.gy", tc.src)
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

// TestAMissingBooleanOrNoneKeyTrapsThroughTheCLI is the trap half, and the half the exit-code contract
// cares about: a missing key is the program being wrong (exit 3 with a traceback the program's own
// `except` can reach), never the compiler refusing at exit 1 and never the toolchain at exit 2.
func TestAMissingBooleanOrNoneKeyTrapsThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, wantErr string }{
		{"none_key_absent", "d = {True: 3.5}\nprint(d[None])\n", "KeyError: None"},
		{"true_key_absent", "d = {None: 3.5}\nprint(d[True])\n", "KeyError: True"},
		{"false_key_absent", "d = {None: 3.5}\nprint(d[False])\n", "KeyError: False"},
		{"text_key_absent", "d = {None: 3.5}\nprint(d[\"nope\"])\n", "KeyError: 'nope'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "missing_key.gy", tc.src)
			refOut, refCode := oracleTrap(t, path)
			if refCode == 0 {
				t.Fatalf("the reference answered a program meant to raise:\n%s", refOut)
			}
			if !strings.Contains(refOut, tc.wantErr) {
				t.Fatalf("the reference's sentence is %q, want %q", refOut, tc.wantErr)
			}
			stdout, stderr, code := cliStreams(t, "--file", path)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
			}
			if code == 1 {
				t.Fatalf("the compiler refused a program the reference runs (exit 1):\n%s\n%s", stdout, stderr)
			}
			if code != 3 {
				t.Fatalf("exit %d, want the trap class 3\nstdout %q\nstderr %q", code, stdout, stderr)
			}
			if !strings.Contains(stderr, tc.wantErr) {
				t.Errorf("the compiled trap is %q, the reference raises %q", stderr, tc.wantErr)
			}
			if strings.Contains(stderr, "codegen:") {
				t.Errorf("the trap was a refusal:\n%s", stderr)
			}
		})
	}
}

// TestTheNoneKeyProgramMatchesTheReference is the whole row as one program, line for line against the
// answer CPython gave it. `testdata/dict_none_bool_key.expected` was written by CPython and never by
// this backend.
func TestTheNoneKeyProgramMatchesTheReference(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/dict_none_bool_key.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if strings.TrimSpace(string(src)) == "" {
		t.Fatal("the program is empty")
	}
	want, err := os.ReadFile("testdata/dict_none_bool_key.expected")
	if err != nil {
		t.Fatalf("read the reference's answer: %v", err)
	}
	stdout, stderr, code := cliStreams(t, "--file", "programs/dict_none_bool_key.gy")
	if code == 2 {
		t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
	}
	if code != 0 {
		t.Fatalf("exit %d, want 0\nstderr %q", code, stderr)
	}
	got := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	wantLines := strings.Split(strings.TrimRight(string(want), "\n"), "\n")
	if len(got) != len(wantLines) {
		t.Fatalf("the program printed %d lines, the reference printed %d:\n%s", len(got), len(wantLines), strings.Join(got, "\n"))
	}
	for i := range wantLines {
		if got[i] != wantLines[i] {
			t.Fatalf("line %d: the program answered %q where CPython answers %q\n%s", i+1, got[i], wantLines[i], strings.Join(got, "\n"))
		}
	}
}

// TestTheNoneKeyProgramIsOnTheRecord is ADR 0302's rule at this altitude: the program's answer survives
// as a record, and a record that disappeared fails here rather than passing by having nothing to compare
// against.
func TestTheNoneKeyProgramIsOnTheRecord(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/dict_none_bool_key.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if !lang.HasGoldenAnswer(string(src)) {
		t.Fatal("integration/programs/dict_none_bool_key.gy is not on the record — record it from the reference with tools/recmerge, never from this backend")
	}
}

// The half the row does not own, at the CLI: a text entry that is *operated on* still leaves the pair
// door's reach, and the two faces of that gap are both pinned — one road refuses with a sentence that
// claims CPython raises where it answers, and the other prints the payload at exit 0. The rows the fix
// left standing are pinned here too — the truth test, the `None`-valued name key, and the two untaggable-key
// refusals — each with the roadmap row that owes it, so the assertion is that none of them has quietly
// become something else.
//
// The f-string field used to be in this table, printing `3` for `f"{d[None]}"` beside `print(d[None])`'s
// `3.5`. It is answered now, and lives in `TestTheFstringFieldOfANoneKeyEntryIsAnsweredAtTheCLI` below
// (roadmap Gap R.283, ADR 0356).
func TestTheNoneKeyRowLeavesTheOperatorRoadsOnTheTracker(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		src       string
		wantOut   string
		wantErr   string
		reference string
		row       string
	}{
		{
			name:      "a_text_entry_concatenated_refuses",
			src:       "d = {None: \"a\"}\nprint(d[None] + \"b\")\n",
			wantErr:   "concatenating a string with a value that is not a string",
			reference: `"ab"`,
			row:       "Gap R.285",
		},
		{
			name:      "a_text_entry_repeated_answers_the_payload",
			src:       "d = {None: \"a\"}\nprint(d[None] * 2)\n",
			wantOut:   "0\n",
			reference: `"aa"`,
			row:       "Gap R.285",
		},
		{
			name:      "the_truth_of_an_entry_answers_the_payload",
			src:       "d = {None: \"hi\"}\nif d[None]:\n    print(\"y\")\nelse:\n    print(\"n\")\n",
			wantOut:   "n\n",
			reference: "y",
			row:       "Gap R.284",
		},
		{
			name:      "a_name_holding_none_is_read_as_an_untagged_key",
			src:       "d = {None: 3.5}\nk = None\nprint(d[k])\n",
			wantErr:   "KeyError",
			reference: "3.5",
			row:       "Gap R.286",
		},
		{
			name:      "an_untaggable_key_offers_the_retired_way_to_run",
			src:       "d = {[1]: \"x\"}\nprint(d[[1]])\n",
			wantErr:   "or run it interpreted",
			reference: "TypeError: unhashable type: 'list' — the refusal is honest, the offer of another way to run it is not",
			row:       "Gap R.288",
		},
		{
			name:      "a_tuple_key_names_an_ast_node",
			src:       "d = {(1, 2): \"x\"}\nprint(d[(1, 2)])\n",
			wantErr:   "unsupported expression *lang.Tuple",
			reference: "x — a tuple is hashable; the missing tuple object is L11.3's, this row is the sentence",
			row:       "Gap R.288",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "filed_not_fixed.gy", tc.src)
			stdout, stderr, code := cliStreams(t, "--file", path)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
			}
			if tc.wantErr != "" {
				if code == 0 || !strings.Contains(stdout+stderr, tc.wantErr) {
					t.Errorf("%s: expected today's %q, got exit %d stdout %q stderr %q — the reference prints %s (%s)",
						tc.name, tc.wantErr, code, stdout, stderr, tc.reference, tc.row)
				}
				return
			}
			if code != 0 || stdout != tc.wantOut {
				t.Errorf("%s: the compiled leg printed %q (exit %d), this row pinned %q — the reference prints %s (%s)",
					tc.name, stdout, code, tc.wantOut, tc.reference, tc.row)
			}
		})
	}
}

// The field road's paid row, kept at the CLI so the answer is witnessed where a caller sees it: one value,
// the `print` line and the field line, both the reference's text. This is the pair ADR 0356 exists for — the
// field had its own narrower view of the subscript roads and rendered the payload alone (the `@float_box`
// handle of a float entry, the `@str_tab` index of a text entry, a heap slot for a list entry), all at exit 0
// with the line above printing the value (roadmap Gap R.283).
func TestTheFstringFieldOfANoneKeyEntryIsAnsweredAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{"a_float_entry", "d = {None: 3.5}\nprint(d[None])\nprint(f\"{d[None]}\")\n", "3.5\n3.5\n"},
		{"a_text_entry", "d = {None: \"hi\"}\nprint(d[None])\nprint(f\"{d[None]}\")\n", "hi\nhi\n"},
		{"a_list_entry", "d = {None: [1, 2]}\nprint(d[None])\nprint(f\"{d[None]}\")\n", "[1, 2]\n[1, 2]\n"},
		{"a_none_entry", "d = {None: None}\nprint(d[None])\nprint(f\"{d[None]}\")\n", "None\nNone\n"},
		{"the_int_key_twin", "d = {1: 3.5}\nprint(d[1])\nprint(f\"{d[1]}\")\n", "3.5\n3.5\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "field_answered.gy", tc.src)
			stdout, stderr, code := cliStreams(t, "--file", path)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
			}
			if code != 0 || stdout != tc.want {
				t.Fatalf("the compiled leg printed %q (exit %d), want %q — stderr %q", stdout, code, tc.want, stderr)
			}
			ref := pythonOutput(t, tc.src)
			if ref != tc.want {
				t.Fatalf("the reference prints %q, so this row's expectation is invented", ref)
			}
		})
	}
}

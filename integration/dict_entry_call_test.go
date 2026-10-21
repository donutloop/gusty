package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// dict_entry_call_test.go — the call argument across the whole pipeline (roadmap Gap R.220, ADR 0389).
//
// `gustyc --eval 'd = {"a": "x"}\ndef id(v):\n    return v\nprint(id(d["a"]))'` printed `1` — the text's
// `@str_tab` index — and exited 0. A float entry printed the `@float_box` handle, a `None` entry printed
// `0`, a container entry printed a heap handle. The read produces a (payload, tag) pair and the parameter
// kept one word, so the kind stayed behind in the dict: this is Gap R.146's one-word position measured
// from the dict side, and the same program with the literal written in place refused at exit 1, which is
// the asymmetry ADR 0327 left standing.
//
// The rows below are the same programs the compiler-side file pins, witnessed through the CLI: what the
// reference prints, what each execution path prints, and what the record holds. The refusals are pinned
// too, and the exit code each one leaves is part of the assertion — a program the reference answers may
// leave exit 1 with a sentence, never exit 2 (the toolchain's own failure) and never exit 0 with a
// payload dressed as a value.

// TestACallArgumentOfASelfDescribingEntryThroughTheCLI is the answering half.
func TestACallArgumentOfASelfDescribingEntryThroughTheCLI(t *testing.T) {
	t.Parallel()
	const idf = "def id(v):\n    return v\n\n"
	for _, tc := range []struct{ name, src, want string }{
		{"a_text_entry", idf + "d = {\"a\": \"x\"}\nprint(id(d[\"a\"]))\n", "x\n"},
		{"an_empty_text_entry", idf + "d = {\"a\": \"\"}\nprint(id(d[\"a\"]))\n", "\n"},
		{"a_float_entry", idf + "d = {\"a\": 3.5}\nprint(id(d[\"a\"]))\n", "3.5\n"},
		{"a_negative_zero_entry", idf + "d = {\"a\": -0.0}\nprint(id(d[\"a\"]))\n", "-0.0\n"},
		{"a_none_entry", idf + "d = {\"a\": None}\nprint(id(d[\"a\"]))\n", "None\n"},
		{"a_verdict_entry", idf + "d = {\"a\": True}\nprint(id(d[\"a\"]))\n", "True\n"},
		{"a_list_entry", idf + "d = {\"a\": [1, 2]}\nprint(id(d[\"a\"]))\n", "[1, 2]\n"},
		{"an_empty_list_entry", idf + "d = {\"a\": []}\nprint(id(d[\"a\"]))\n", "[]\n"},
		{"a_dict_entry", idf + "d = {\"a\": {\"k\": 1}}\nprint(id(d[\"a\"]))\n", "{'k': 1}\n"},
		{"an_int_entry", idf + "d = {\"a\": 7}\nprint(id(d[\"a\"]))\n", "7\n"},
		{
			// The literal written in place is the same read as the bound name (ADR 0327's door, now asked
			// of the argument position too).
			name: "the_literal_written_in_place",
			src:  idf + "print(id({\"a\": \"x\"}[\"a\"]))\n",
			want: "x\n",
		},
		{
			// One parameter, several call sites: the integer entry and the float entry answer differently
			// and both are right.
			name: "two_call_sites_one_parameter",
			src:  idf + "d = {0: 1, 1: 2.5}\nprint(id(d[0]), id(d[1]))\n",
			want: "1 2.5\n",
		},
		{
			name: "the_body_arithmetics_on_the_parameter",
			src:  "def f(v):\n    return v + 1\n\nd = {\"a\": 3.5}\nprint(f(d[\"a\"]))\n",
			want: "4.5\n",
		},
		{
			name: "two_entries_summed",
			src:  "def total(x, y):\n    return x + y\n\nd = {\"a\": 2.5, \"b\": 1.5}\nprint(total(d[\"a\"], d[\"b\"]))\n",
			want: "4.0\n",
		},
		{
			// Two parameters, two entries of two different kinds: each argument is asked on its own, so the
			// integer entry keeps its one word and the float entry brings its tag beside its payload.
			name: "two_parameters_two_entries_of_two_kinds",
			src:  "def takes(a, b):\n    return b\n\nd = {\"a\": 1, \"b\": 2.5}\nprint(takes(d[\"a\"], d[\"b\"]))\n",
			want: "2.5\n",
		},
		{
			// The element of a mixed list, and the container inside one: the same read, one position later.
			// (A callee that TESTS its own parameter is not on this table — `if v:` over a pair-carrying
			// parameter still asks the payload, which is the row roadmap Gap R.361 owns and
			// `TestWhatTheCallArgumentStillOwesTheOtherRowsOwn` files.)
			name: "a_mixed_list_element_and_a_container_inside_one",
			src:  "def id(v):\n    return v\n\nxs = [[1, 2], \"a\"]\nprint(id(xs[0]), id(xs[1]))\n",
			want: "[1, 2] a\n",
		},
		{
			// A dict the program wrote again is still a container the program built: the door reads the
			// kinds the writer left, so the entry handed to the call is the entry, not its slot.
			name: "an_entry_of_a_dict_the_program_wrote_again",
			src:  "def id(v):\n    return v\n\nd = {\"a\": \"x\"}\nd[\"b\"] = \"y\"\nprint(id(d[\"b\"]))\n",
			want: "y\n",
		},
		{
			name: "the_entry_forwarded_through_a_second_frame",
			src:  idf + "def f(v):\n    return id(v)\n\nd = {\"a\": 2.5}\nprint(f(d[\"a\"]))\n",
			want: "2.5\n",
		},
		{
			// The key is a spelling. ADR 0353's rule, restated where the argument crosses.
			name: "none_true_and_int_keys_read_their_own_entries",
			src:  idf + "d = {None: 3.5, True: \"t\", 0: [4]}\nprint(id(d[None]), id(d[True]), id(d[0]))\n",
			want: "3.5 t [4]\n",
		},
		// (`a_float_returning_body_meets_a_double` — `def f(v): return v + 1.0` called with `2.0` — sat
		// here. It has no container read in it at all: it is ADR 0254's missing return-kind row for a
		// promoted parameter meeting a float literal in the returned expression, it reaches `llc` at exit 2,
		// and roadmap Gap R.344 already pins it as an exit-2 row in `pkg/lang/module_float_test.go`. A
		// program that dies in `llc` cannot be a row of this table.)
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "dict_entry_call.gy", tc.src)
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

// TestTheCallArgumentRefusalsLeaveTheTrapCodesAlone is the exit-code half. Each source names a position
// the pair road cannot serve, so the compiler refuses (exit 1) with a sentence about the construct; what
// must not happen is exit 0 with the payload, exit 2 from a module `llc` rejected, or exit 3 dressed up as
// one of the program's own traps.
func TestTheCallArgumentRefusalsLeaveTheTrapCodesAlone(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{
			name: "a_bound_entry_keeps_its_own_row",
			src:  "def id(v):\n    return v\n\nd = {\"a\": []}\nx = d[\"a\"]\nprint(id(x))\n",
			want: "holds a slot the program built at run time",
		},
		{
			// The element of a list that mixes kinds, handed to the arithmetic a call sits in front of: the
			// `+ ""` is a position that keeps one word, and the pair road does not reach past it.
			name: "a_mixed_element_asked_to_concatenate_below_the_call",
			src:  "def id(v):\n    return v\n\nxs = [1, \"a\"]\nprint(id(xs[1] + \"\"))\n",
			want: "so one element is a (value, tag) pair",
		},
		// (Four rows left this table when the cycle that paid Gap R.220 landed. Three of them answer at
		// exit 0 with the payload where the reference answers with the value, and one reaches `llc` at exit
		// 2; none of them is the argument's row, and each names its own in
		// `TestWhatTheCallArgumentStillOwesTheOtherRowsOwn`. A row that leaves exit 2 cannot sit here, and a
		// row that answers wrongly cannot be asserted as an answer — filing it is the honest form.)
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "dict_entry_call_refusal.gy", tc.src)
			stdout, stderr, code := cliStreams(t, "--file", path)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
			}
			if code == 0 {
				t.Fatalf("answered %q at exit 0; the payload must not cross without its tag\nstdout %q\nstderr %q", stdout, stdout, stderr)
			}
			if code == 3 {
				t.Fatalf("left the program's own trap class on a construct the compiler cannot lower:\n%s\n%s", stdout, stderr)
			}
			if code != 1 {
				t.Fatalf("exit %d, want the compiler's refusal at 1\nstdout %q\nstderr %q", code, stdout, stderr)
			}
			if !strings.Contains(stderr, tc.want) {
				t.Errorf("the refusal does not say %q:\n%s", tc.want, stderr)
			}
			if strings.Contains(stderr, "or run it interpreted") {
				t.Errorf("the refusal offers the execution backend ADR 0302 retired:\n%s", stderr)
			}
		})
	}
}

// TestWhatTheCallArgumentStillOwesTheOtherRowsOwn is the ledger side of this file. Four programs sit at the
// same call boundary and are answered by other rows: the callee that TESTS its own parameter, the recursive
// callee that hands the parameter back, the ternary whose chosen arm is the parameter, and the read a
// self-call makes of a module-level dict. Each is measured against the reference and compared through the
// debt ledger, so this row cannot be read as claiming them and they cannot be dropped without a line in
// `testdata/cpython-debt.json` disappearing.
func TestWhatTheCallArgumentStillOwesTheOtherRowsOwn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, tc := range []struct{ name, src, row, why string }{
		{
			name: "a callee that tests its own parameter",
			src:  "def f(v):\n    if v:\n        return 1\n    return 0\n\nd = {\"e\": [], \"t\": \"\", \"l\": [1], \"n\": None}\nprint(f(d[\"e\"]), f(d[\"t\"]), f(d[\"l\"]), f(d[\"n\"]))\n",
			row:  "roadmap L11.1, Gap R.361 (a condition asks a parameter its own kind)",
			why:  "the pair crossed the argument, and the callee's `if v:` still tests the payload against zero",
		},
		{
			name: "a recursive callee that hands the parameter back",
			src:  "def f(v, n):\n    if n <= 0:\n        return v\n    return f(v, n - 1)\n\nd = {\"a\": 3.5}\nprint(f(d[\"a\"], 2))\n",
			row:  "roadmap L11.1, Gap R.146 (the tagged value word a recursive body's answer keeps)",
			why:  "the answer came back along the ordinary numeric road, so the float travelled as its payload alone",
		},
		{
			name: "a ternary whose arm is the parameter",
			src:  "def pick(v):\n    return v if v else 0\n\nd = {\"a\": 2.5}\nprint(pick(d[\"a\"]))\n",
			row:  "roadmap L11.1, Gap R.147 (the chosen operand of a ternary has no tagged value word)",
			why:  "the select keeps one word for whichever arm it chooses, and that is where the kind is lost",
		},
		{
			name: "a self-call that reads a module-level dict from inside the body",
			src:  "def f(v, n):\n    if n <= 0:\n        return 0\n    return f(d[\"a\"], n - 1)\n\nd = {\"a\": 3.5}\nprint(f(d[\"a\"], 2))\n",
			row:  "roadmap L11.1, Gap R.362 (a dict read inside a body reaches the unexplained constant-index refusal)",
			why:  "the body's own read of a module dict asks the fold's door and gets the old `index must be a constant`",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, dir, "dict_entry_call_owed.gy", tc.src)
			ref, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Skipf("the reference could not answer this program: %s", tc.src)
			}
			got, code := cliRunMerged(t, "--file", path)
			if code == 2 {
				t.Fatalf("exit 2 — LLVM rejected the module gusty emitted (ADR 0166):\n%s", got)
			}
			if code != 0 && code != 1 {
				t.Fatalf("the compiled run exited %d:\n reference: %q\n compiled: %q", code, ref, got)
			}
			requireReferenceAgreement(t, tc.src, ReferenceAgreement{
				Python: ref, Compiled: got, Code: code,
			}, tc.row, tc.why)
		})
	}
}

// TestTheCallArgumentProgramMatchesTheReference is the whole row as one program, line for line against the
// answer CPython gave it. `testdata/dict_entry_call_argument.expected` was written by CPython and never by
// this backend.
func TestTheCallArgumentProgramMatchesTheReference(t *testing.T) {
	t.Parallel()
	path := "programs/dict_entry_call_argument.gy"
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if strings.TrimSpace(string(src)) == "" {
		t.Fatal("the program is empty")
	}
	for _, keep := range []string{
		`print(id(d["a"]), id(d["i"]))`,
		`print(id(d["n"]), id(d["z"]), id(d["l"]), id(d["e"]))`,
		`print(id({"a": "x"}["a"]))`,
		`print(twice(d["i"]))`,
		`print(forwarded(d["a"]), forwarded(d["l"]))`,
		`print(total(n["a"], n["b"]))`,
		`print(id(w[None]), id(w[True]), id(w[0]))`,
		`print(id(xs[1]), id(xs[0]))`,
	} {
		if !strings.Contains(string(src), keep) {
			t.Fatalf("the conformance program stopped carrying Gap R.220's row %q", keep)
		}
	}
	want, err := os.ReadFile("testdata/dict_entry_call_argument.expected")
	if err != nil {
		t.Fatalf("read the reference's answer: %v", err)
	}
	stdout, stderr, code := cliStreams(t, "--file", path)
	if code == 2 {
		t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
	}
	if code != 0 {
		t.Fatalf("exit %d, want 0\nstderr %q", code, stderr)
	}
	gotLines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	wantLines := strings.Split(strings.TrimRight(string(want), "\n"), "\n")
	if len(gotLines) != len(wantLines) {
		t.Fatalf("the program printed %d lines, the reference printed %d:\n%s", len(gotLines), len(wantLines), strings.Join(gotLines, "\n"))
	}
	for i := range wantLines {
		if gotLines[i] != wantLines[i] {
			t.Fatalf("line %d: the program answered %q where CPython answers %q\n%s", i+1, gotLines[i], wantLines[i], strings.Join(gotLines, "\n"))
		}
	}
}

// TestTheCallArgumentProgramIsOnTheRecord is ADR 0302's rule at this altitude: the program's answer
// survives as a record, and a record that disappeared fails here rather than passing by having nothing to
// compare against.
func TestTheCallArgumentProgramIsOnTheRecord(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/dict_entry_call_argument.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if !lang.HasGoldenAnswer(string(src)) {
		t.Fatal("integration/programs/dict_entry_call_argument.gy is not on the record — record it from the reference with tools/recmerge, never from this backend")
	}
}

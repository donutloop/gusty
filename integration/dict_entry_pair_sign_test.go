package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// dict_entry_pair_sign_test.go — roadmap Gap R.287 (ADR 0387) at the command line.
//
// `gustyc --eval 'd = {None: 3.5}\nprint(abs(d[None]))'` wrote `3` at exit 0. The dict entry itself has
// printed `3.5` since ADR 0353; the signless call and the folds were the callers that still asked only for
// its payload. Through the CLI they now print what CPython prints, and non-numeric entries raise CPython's
// sentence rather than answering a handle at the success exit code.

func TestTheSelfDescribingDictEntrySignlessRoadAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"abs_none_key", "d = {None: -3.5}\nprint(abs(d[None]))\n", "3.5\n"},
		{"abs_int_key", "d = {1: -3.5}\nprint(abs(d[1]))\n", "3.5\n"},
		{"abs_text_key", "d = {\"k\": -2.75}\nprint(abs(d[\"k\"]))\n", "2.75\n"},
		{"abs_bool_key", "d = {True: -1.25}\nprint(abs(d[True]))\n", "1.25\n"},
		{"abs_run_built", "d = {}\nd[None] = -4.5\nprint(abs(d[None]))\n", "4.5\n"},
		{"abs_nested", "xs = [{None: -2.5}]\nprint(abs(xs[0][None]))\n", "2.5\n"},
		{"min_float", "d = {None: -3.5}\nprint(min(d[None], 2))\n", "-3.5\n"},
		{"max_float", "d = {None: 3.5}\nprint(max(d[None], 2))\n", "3.5\n"},
		{"max_chooses_int", "d = {None: -3.5}\nprint(max(d[None], 2))\n", "2\n"},
		{"max_two_entries", "d = {None: 1.5, True: 2.5}\nprint(max(d[None], d[True]))\n", "2.5\n"},
		{"sum_float", "d = {None: -3.5}\nprint(sum([d[None], 0]))\n", "-3.5\n"},
		{"bool_winner", "d = {None: True}\nprint(max(d[None], 1))\n", "True\n"},
		{"print_and_abs_agree", "d = {None: -3.5}\nprint(d[None])\nprint(abs(d[None]))\n", "-3.5\n3.5\n"},
		{"nested_print_and_abs_agree", "xs = [{None: -2.5}]\nprint(xs[0][None])\nprint(abs(xs[0][None]))\n", "-2.5\n2.5\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "dict_entry_pair_sign.gy", tc.src)
			ref, code := oracleTrap(t, path)
			if code != 0 {
				t.Fatalf("the reference exited %d on a program meant to answer:\n%s", code, ref)
			}
			if ref != tc.want {
				t.Fatalf("the reference prints %q, want %q", ref, tc.want)
			}
			for _, engine := range cliEngines {
				out := cliRun(t, engine, path)
				if _, exit := cliRunCode(t, engine, path); exit != 0 {
					t.Errorf("%s exited %d on a program the reference answers: %s", engine, exit, out)
				}
				if strings.Contains(out, "codegen:") {
					t.Errorf("%s refused a program the reference answers:\n%s", engine, out)
				}
				if out != tc.want {
					t.Errorf("%s printed %q, the reference prints %q", engine, out, tc.want)
				}
			}
		})
	}
}

func TestTheSelfDescribingDictEntrySignlessTrapsAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, wantErr string }{
		{"text_entry_abs", "d = {None: \"hi\"}\nprint(abs(d[None]))\n", "TypeError: bad operand type for abs(): 'str'"},
		{"none_entry_abs", "d = {None: None}\nprint(abs(d[None]))\n", "TypeError: bad operand type for abs(): 'NoneType'"},
		{"list_entry_abs", "d = {None: [1]}\nprint(abs(d[None]))\n", "TypeError: bad operand type for abs(): 'list'"},
		{"text_entry_fold", "d = {None: \"hi\"}\nprint(max(d[None], 1))\n", "TypeError: '>' not supported between instances of 'int' and 'str'"},
		{"list_entry_fold", "d = {None: [1]}\nprint(min(d[None], 1))\n", "TypeError: '<' not supported between instances of 'int' and 'list'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "dict_entry_pair_sign_trap.gy", tc.src)
			ref, refCode := oracleTrap(t, path)
			if refCode == 0 {
				t.Fatalf("the reference answered a program meant to raise:\n%s", ref)
			}
			if !strings.Contains(ref, tc.wantErr) {
				t.Fatalf("the reference's sentence is %q, want %q", ref, tc.wantErr)
			}
			stdout, stderr, code := cliStreams(t, "--file", path)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
			}
			if code == 1 {
				t.Fatalf("the compiler refused a program the reference stops on (exit 1):\n%s\n%s", stdout, stderr)
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

func TestTheSelfDescribingDictEntrySignlessCatchabilityAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{
			name: "abs_raises_to_except",
			src:  "d = {None: \"hi\"}\ntry:\n    print(abs(d[None]))\nexcept TypeError:\n    print(\"caught abs\")\n",
			want: "caught abs\n",
		},
		{
			name: "fold_raises_to_except",
			src:  "d = {None: [1]}\ntry:\n    print(min(d[None], 1))\nexcept TypeError:\n    print(\"caught fold\")\n",
			want: "caught fold\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "dict_entry_pair_sign_except.gy", tc.src)
			stdout, stderr, code := cliStreams(t, "--file", path)
			if code != 0 || stdout != tc.want {
				t.Fatalf("the compiled leg printed %q (exit %d), want %q — stderr %q", stdout, code, tc.want, stderr)
			}
			if ref := pythonOutput(t, tc.src); ref != tc.want {
				t.Fatalf("the expectation is not CPython's: reference %q", ref)
			}
		})
	}
}

func TestTheSelfDescribingDictEntrySignlessProgramMatchesTheReference(t *testing.T) {
	t.Parallel()
	path := "programs/dict_none_bool_key.gy"
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	want, err := os.ReadFile("testdata/dict_none_bool_key.expected")
	if err != nil {
		t.Fatalf("read the reference's answer: %v", err)
	}
	if !strings.Contains(string(src), "print(abs(s[None]))") || !strings.Contains(string(src), "print(min(q[1], 1), max(q[1], 1))") {
		t.Fatal("the conformance program stopped carrying Gap R.287's rows")
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
			t.Fatalf("line %d: compiled %q, reference %q\n%s", i+1, gotLines[i], wantLines[i], strings.Join(gotLines, "\n"))
		}
	}
}

func TestTheSelfDescribingDictEntrySignlessProgramIsOnTheRecord(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/dict_none_bool_key.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if !lang.HasGoldenAnswer(string(src)) {
		t.Fatal("integration/programs/dict_none_bool_key.gy is not on the record — record it from the reference with tools/recmerge, never from this backend")
	}
	for _, one := range []string{
		"d = {None: -3.5}\nprint(abs(d[None]))\n",
		"d = {1: -3.5}\nprint(abs(d[1]))\n",
		"d = {None: 3.5}\nprint(max(d[None], 2))\n",
		"d = {None: -3.5}\nprint(min(d[None], 2))\n",
	} {
		if !lang.HasGoldenAnswer(one) {
			t.Errorf("the signless dict-entry row %q is not on the record", one)
		}
	}
}

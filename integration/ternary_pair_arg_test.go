package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// ternary_pair_arg_test.go — the answer whose kind lives in a table the callee cannot read, handed to a
// parameter, and the `return` whose arm is a container literal, through the CLI (roadmap **Gap R.581**,
// **Gap R.584**, **ADR 0475**).
//
// At this altitude both halves were the same shape: a program `python3` prints, `gustyc` compiled, and `llc`
// answered with the contract's compiler-bug code.
//
//	$ gustyc --eval 'def f(x):
//	      return 1.5
//
//	  def g(v):
//	      return v
//
//	  print(g(f(1)))'
//	gustyc: jit: llc: exit status 1
//	llc-20: error: '%t9' defined with type 'double' but expected 'i32'
//	  %t12 = call i32 @gy_g(i32 %t9)
//	$ echo $?
//	2
//
// and with the container arm:
//
//	llc-20: error: global variable reference must have pointer type
//	  %t2 = phi i32 [ @.lst1, %tern.thenfwd3 ], [ 0, %tern.elsefwd4 ]
//	$ echo $?
//	2
//
// **ADR 0166** reserves exit 2 for a compiler bug, and neither program is one: the reference runs both. Every
// row below is judged against a live `python3`; the module table re-reads the emitted IR so the shape that
// reached `llc` cannot come back, and the refusal rows are checked for the vocabulary a refusal must never
// use (**ADR 0249**).

// ternaryPairArgCLISources is the answering family at the CLI — the same programs the unit table asks, read
// again against the reference rather than from this file.
func TestTheTernaryArmAndTheFloatAnswerCrossACallThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"def f(x):\n    return 1.5\n\ndef g(v):\n    return v\n\nprint(g(f(1)))",
		"def f(x):\n    return 1.5\n\ndef g(v):\n    print(v)\n\ng(f(1))",
		"def f(x):\n    return 1.5\n\ndef g(v):\n    return v\n\ndef h(w):\n    return g(w)\n\nprint(h(f(1)))",
		"def f():\n    return 2.5\n\ndef g(v):\n    return v\n\nprint(g(f()))",
		"def f(x):\n    return 1.5\n\ndef g(v):\n    return v\n\nprint(g(f(1)), g(f(0)))",
		"def f(x):\n    return 1.5\n\ndef g(v, k):\n    return v\n\nprint(g(f(1), 3))",
		"def f(x):\n    return 1.5 if x else 2.5\n\ndef g(v):\n    return v\n\nprint(g(f(1)), g(f(0)))",
		"def f(x):\n    return [1, 2] if x else 0\n\nw = f(1)\nprint(w)",
		"def f(x):\n    return [1, 2] if x else 0\n\nprint(f(0))",
		"def f(x):\n    return [i * 2 for i in [1, 2]] if x else 0\n\nw = f(1)\nprint(w)",
		"def f(x):\n    return [[1, 2], [3]] if x else 0\n\nw = f(1)\nprint(w)",
		"def f(x):\n    return {\"a\": 1} if x else 0\n\nw = f(1)\nprint(w)",
		"def f(x):\n    return {\"k\": 1.5} if x else 0\n\nw = f(1)\nprint(w)",
		"def f(x):\n    return {1, 2} if x else 0\n\nw = f(1)\nprint(w)",
		"def f(x):\n    return {1, 2} if x else 0\n\nprint(f(0))",
		"def f(x):\n    return [1, 2] if x else 0\n\ndef g(v):\n    return v\n\nprint(g(f(0)))",
		"def f(x):\n    return {1, 2} if x else 7\n\nprint(f(0) + 1)",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program (exit 2 is ADR 0166's compiler-bug code):\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: the reference answers this program\nstdout %q\nstderr %q", code, stdout, stderr)
			}
			if got := strings.TrimSpace(stdout); got != strings.TrimSpace(want) {
				t.Fatalf("printed %q, the reference prints %q", got, strings.TrimSpace(want))
			}
		})
	}
}

// TestTheTernaryPairArgModuleVerifies is the module table: the emitted IR is re-read for the two shapes that
// `llc` refused, and every program is verified by the toolchain's own `--verify-llvm` leg. A row that answers
// but whose module carries a global in an `i32` operand is the row this case exists to catch before `llc` does.
func TestTheTernaryPairArgModuleVerifies(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"def f(x):\n    return [1, 2] if x else 0\n\nw = f(1)\nprint(w)",
		"def f(x):\n    return {1, 2} if x else 0\n\nw = f(1)\nprint(w)",
		"def f(x):\n    return {\"a\": 1} if x else 0\n\nw = f(1)\nprint(w)",
		"def f(x):\n    return [i * 2 for i in [1, 2]] if x else 0\n\nw = f(1)\nprint(w)",
		"def f(x):\n    return 1.5\n\ndef g(v):\n    return v\n\nprint(g(f(1)))",
		"def f(x):\n    return 1.5 if x else 2.5\n\ndef g(v):\n    return v\n\nprint(g(f(1)))",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			ir, stderr, code := cliStreams(t, "--emit-llvm", src)
			if code != 0 {
				t.Fatalf("--emit-llvm exited %d:\n%s", code, stderr)
			}
			for _, ln := range strings.Split(ir, "\n") {
				if strings.Contains(ln, "= phi i32") && strings.Contains(ln, "@.") {
					t.Fatalf("a compile-time global sits in an i32 phi — the shape `llc` refuses at exit 2:\n%s", ln)
				}
				if strings.Contains(ln, "= call i32 @gy_") && strings.Contains(ln, "double %") {
					t.Fatalf("a double is being handed to an i32 parameter — the shape `llc` refuses at exit 2:\n%s", ln)
				}
			}
			if v, verr, vcode := cliStreams(t, "--verify-llvm", src); vcode != 0 || !strings.Contains(v, "module verified") {
				t.Fatalf("the module did not verify: exit %d\n%s\n%s", vcode, v, verr)
			}
		})
	}
}

// TestTheStillRefusedCallArgumentRefusesInProgramWords keeps the remainder honest: the positions this row does
// NOT claim still refuse, and they refuse naming the program — never a Go type, never a compiler temp.
func TestTheStillRefusedCallArgumentRefusesInProgramWords(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, src, want string
	}{
		{
			name: "str of a forwarded answer",
			src:  "def f(x):\n    return \"x\" if x else 0\n\ndef g(v):\n    return v\n\nprint(str(g(f(1))))",
			want: "str(g(f(1))) is refused",
		},
		{
			name: "len of a forwarded answer",
			src:  "def f(x):\n    return \"abc\" if x else 0\n\ndef g(v):\n    return v\n\nprint(len(g(f(1))))",
			want: "len requires an inline list/dict/set literal",
		},
		{
			name: "arms that do not agree on a word",
			src:  "def f(x):\n    return 2.0 / 1 if x else 0\n\ndef g(v):\n    return v\n\nprint(g(f(1)))",
			want: "do not agree on a word",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("exit 2 is the code a compiler bug owns; this program should refuse at 1:\n%s", stderr)
			}
			if code != 1 {
				t.Fatalf("exit %d, want the refusal's 1\n%s", code, stderr)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Fatalf("the refusal does not say what is missing (%q):\n%s", tt.want, stderr)
			}
			for _, forbidden := range []string{"%!s(", "*lang.", "__cans", "%p0"} {
				if strings.Contains(stderr, forbidden) {
					t.Fatalf("the refusal quotes the compiler's own vocabulary (%q) where the program's belongs — ADR 0249:\n%s", forbidden, stderr)
				}
			}
		})
	}
}

// The whole family as one program, line for line against CPython.
func TestTheTernaryPairArgProgramMatchesTheReferenceThroughTheCLI(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/ternary_pair_arg.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if strings.TrimSpace(string(src)) == "" {
		t.Fatal("the program is empty")
	}
	want, err := os.ReadFile("testdata/ternary_pair_arg.expected")
	if err != nil {
		t.Fatalf("read the reference's answer: %v", err)
	}
	stdout, stderr, code := cliStreams(t, "--file", "programs/ternary_pair_arg.gy")
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

func TestTheTernaryPairArgProgramIsAnAssertedMatrixRow(t *testing.T) {
	t.Parallel()
	cases := conformanceCases()
	var row *lang.ConformanceCase
	for i := range cases {
		if cases[i].Name == "ternary_pair_arg.gy" {
			row = &cases[i]
			break
		}
	}
	if row == nil {
		t.Fatal("no conformance matrix row runs ternary_pair_arg.gy")
	}
	if !row.Asserted {
		t.Fatalf("row %s is not asserted: its stdout would run unwatched", row.ID)
	}
	if row.Oracle != "match" {
		t.Fatalf("row %s declares oracle %q; the compiled leg answers this program, so it is a match", row.ID, row.Oracle)
	}
	for _, want := range []string{
		"print(show(half(1)))",
		"bound = picks(1)",
		"rows = built(1)",
		"row = entry(1)",
	} {
		if !strings.Contains(row.Source, want) {
			t.Fatalf("row %s runs a program without %q", row.ID, want)
		}
	}
}

package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// membership_needle_pair_test.go — the membership needle that IS a slot read, through the CLI (roadmap
// **Gap R.232**, **ADR 0474**).
//
// At this altitude the symptom is an exit code and a sentence. Before the door existed:
//
//	$ gustyc --eval 'def words():
//	      return ["hi", "ho"]
//
//	  print(words()[0] in words())'
//	gustyc: jit: codegen: testing &{%!s(*lang.Call=&{0x4000…  … list[str]}) …} against the container
//	      "__cans10" whose slots describe themselves needs a needle whose kind the compiler can prove …
//	$ echo $?
//	1
//
// while `python3` prints `True`, and while the same test over a static text list prints `True` too. The
// reader of that sentence was told neither what was asked (the needle's Go representation is not the
// program's text) nor which road was missing (the compiler's own temp name is not the container's name).
// Every row below is judged against a live `python3`: the answering rows compare stdout line for line, the
// raise rows read the `Class: message` out of the reference per row, and the refusal rows are checked for
// the shape a refusal must never have — the compiler's vocabulary where the program's belongs (**ADR 0249**).

// needlePairCLISources is the answering family at the CLI.
func TestAMembershipNeedleAnswersThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"def words():\n    return [\"hi\", \"ho\"]\n\nprint(words()[0] in words())",
		"def words():\n    return [\"hi\", \"ho\"]\n\nw = words()\nprint(w[0] in w)",
		"def words():\n    return [\"hi\", \"ho\"]\n\nprint(words()[0] in words(), words()[1] not in words())",
		"def mk():\n    return [1, \"x\"]\n\nprint(mk()[1] in mk())",
		"xs = [1, \"a\"]\nprint(xs[0] in xs)",
		"xs = [1, \"a\"]\nprint(xs[1] in xs)",
		"xs = [1, \"a\"]\nprint(xs[0] not in xs)",
		"xs = [1, \"a\"]\nprint(xs[0] in xs, xs[1] in xs, 3 in xs)",
		"xs = [1, \"a\"]\nprint(xs[-1] in xs)",
		"xs = [1, \"a\", None, 2.5]\nprint(xs[3] in xs, xs[1] in xs)",
		"xs = [1, \"a\"]\ni = 1\nprint(xs[i] in xs)",
		"xs = [[7, 8], \"a\"]\nprint(xs[0] in xs)",
		"xs = [[7, 8], \"a\"]\nprint(xs[0][0] in xs[0])",
		"xs = []\nxs.append(\"q\")\nxs.append(1)\nprint(xs[0] in xs)",
		"ints = [1, 2]\ntexts = [\"zz\", 1]\nprint(texts[0] in ints)",
		"ints = [1, 2]\ntexts = [\"zz\", 1]\nprint(texts[1] in ints)",
		"xs = [1, \"a\"]\nif xs[1] in xs:\n    print(\"yes\")",
		"xs = [1, \"a\"]\nprint(1 if xs[1] in xs else 0)",
		"xs = [1, \"a\"]\nfor v in xs:\n    print(v in xs)",
		"xs = [1, \"a\"]\nprint(xs[1] in xs, xs[1] == \"a\")",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
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

// The raises the needle carries are the program's exceptions: exit 3 (never 0, never 2), silence on stdout,
// the reference's class and sentence on stderr, and the handler reaching them — each expectation read out of
// `python3` inside the row rather than remembered here (the standing rule that an expectation is derived).
func TestATrapInsideAMembershipNeedleExitsAsATrapThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name           string
		pre, stmt      string
		class, message string
		catch          string
	}{
		{"a position past the end of the needle's base", "xs = [1, \"a\"]", "print(xs[9] in xs)", "IndexError", "list index out of range", "IndexError"},
		{"a negative position past the front", "xs = [1, \"a\"]", "print(xs[-3] in xs)", "IndexError", "list index out of range", "IndexError"},
		{"a position past the end of a call's answer", "def words():\n    return [\"hi\", \"ho\"]", "print(words()[5] in words())", "IndexError", "list index out of range", "IndexError"},
		{"a missing key in the needle's dict", "d = {\"a\": 1}", "print(d[\"b\"] in d)", "KeyError", "'b'", "KeyError"},
		{"a position into a number", "xs = [1, 2]", "print(xs[0][0] in xs)", "TypeError", "'int' object is not subscriptable", "TypeError"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := tt.pre + "\n" + tt.stmt
			refOut, refErr, rerr := lang.PythonRun(src + "\n")
			if rerr == nil {
				t.Fatalf("the reference ANSWERS %q (%q); this row is filed as a raise — re-measure it before trusting the table", src, refOut)
			}
			want := lastTracebackLine(refErr)
			if want == "" {
				t.Fatalf("cannot read the reference's trap for %q:\n%s", src, refErr)
			}
			// The table's own claim about the reference: the row says which exception, and the reference is
			// the only thing that gets to agree.
			if want != tt.class+": "+tt.message {
				t.Fatalf("this row's expectation of the reference has drifted: CPython raises %q, the row says %q", want, tt.class+": "+tt.message)
			}
			stdout, stderr, code := cliStreams(t, "--eval", src)
			if code == 2 {
				t.Fatalf("exit 2 — the code the contract reserves for a compiler bug (ADR 0166):\n%s", stderr)
			}
			if code == 0 {
				t.Fatalf("exit 0 with stdout %q where the reference raises %q: a value printed where the program raises is the defect this family exists to stop", stdout, want)
			}
			if code != 3 {
				t.Fatalf("exit %d, want 3 (the uncaught-exception exit):\nstdout %q\nstderr %q", code, stdout, stderr)
			}
			if out := strings.TrimSpace(stdout); out != "" {
				t.Fatalf("%q printed %q on the way to the raise; the reference prints nothing", src, out)
			}
			if strings.Contains(stderr, "codegen:") {
				t.Fatalf("the compiler refused the program rather than the runtime raising %q:\n%s", want, stderr)
			}
			if !strings.Contains(stderr, want) {
				t.Fatalf("%q did not raise %q:\n%s", src, want, stderr)
			}
			// The same raise, taken by the program's own handler: exit 0 and the handler's bytes. The
			// definitions stay where the program wrote them — re-indenting a `def`'s own body would ask a
			// different program than the one the reference ran.
			handler := tt.pre + "\ntry:\n    " + tt.stmt + "\nexcept " + tt.catch + ":\n    print(\"caught\")"
			wantCaught := pythonOutput(t, handler+"\n")
			caughtOut, caughtErr, caughtCode := cliStreams(t, "--eval", handler)
			if caughtCode != 0 {
				t.Fatalf("the handler did not run: exit %d\nstdout %q\nstderr %q", caughtCode, caughtOut, caughtErr)
			}
			if got := strings.TrimSpace(caughtOut); got != strings.TrimSpace(wantCaught) {
				t.Fatalf("the handler printed %q, the reference's handler prints %q", got, strings.TrimSpace(wantCaught))
			}
		})
	}
}

// What the door still declines, declined in the program's words, each naming the door that declines it. The
// forbidden substrings are the row's definition of done — the sentence the row was filed with handed the
// reader a Go type and the compiler's own temp name (**ADR 0249**; roadmap **Gap R.232**, and the same rule
// as re-measured for the copy road in **Gap R.563**).
func TestAMembershipNeedleWithoutAKindRefusesThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		src     string
		mustSay []string
	}{
		{
			"an answer whose two arms settle nothing",
			"def pick(v):\n    if v:\n        return v\n    return \"a\"\n\nxs = [1, \"a\"]\nprint(1 if pick(0) in xs else 0)",
			[]string{"needle", "pick(0)", "xs"},
		},
		{
			"a call's answer as the needle",
			"def f(x):\n    return x\n\nxs = [1, \"a\"]\nprint(f(xs[1]) in xs)",
			[]string{"needle", "f(xs[1])", "xs"},
		},
		{
			// The haystack the SOURCE spells is built by the literal road (**ADR 0456**), and the needle that
			// is a pair has no tag to travel in there: a different door refuses, and it is honest about which
			// one — the sentence names the pair and the one-word position, and its row is Gap R.146/R.82.
			"a pair asked of a container the source spells",
			"def words():\n    return [\"hi\", \"ho\"]\n\nprint(words()[1] in [\"hi\", \"ho\"])",
			[]string{"(value, tag) pair", "single static kind"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("exit 2 on a legal program — the code the contract reserves for a compiler bug (ADR 0166):\n%s", stderr)
			}
			if code != 1 {
				t.Fatalf("exit %d, want 1 (a position this backend declines to build):\nstdout %q\nstderr %q", code, stdout, stderr)
			}
			if strings.TrimSpace(stdout) != "" {
				t.Fatalf("the refusal printed %q on stdout: a refusal is not an answer", stdout)
			}
			for _, want := range tt.mustSay {
				if !strings.Contains(stderr, want) {
					t.Fatalf("the refusal does not name %q (ADR 0249):\n%s", want, stderr)
				}
			}
			for _, forbidden := range []string{"%!s(", "*lang.", "__cans", "%!d("} {
				if strings.Contains(stderr, forbidden) {
					t.Fatalf("the refusal leaks the compiler's own representation %q (roadmap Gap R.232):\n%s", forbidden, stderr)
				}
			}
		})
	}
}

// The module the door emits: verified by the pinned toolchain, and carrying the needle's kind as a REGISTER
// beside the payload rather than as an opinion the compiler formed at compile time. `--verify-llvm` is the
// guard against the ADR 0166 class — instructions after a terminator, a global in a value position — on the
// exact shapes whose needle traps (**ADR 0166**, **ADR 0211**).
func TestTheMembershipNeedleModuleVerifiesAndCarriesTheTagRegister(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"xs = [1, \"a\"]\nprint(xs[1] in xs)",
		"def words():\n    return [\"hi\", \"ho\"]\n\nprint(words()[0] in words())",
		"xs = []\nxs.append(\"q\")\nxs.append(1)\nprint(xs[0] in xs)",
		"xs = [1, \"a\"]\ni = 1\nprint(xs[i] in xs)",
		"xs = [1, \"a\"]\ni = 0\nwhile i < 3:\n    print(i, xs[i] in xs)\n    i += 1",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			out, stderr, code := cliStreams(t, "--verify-llvm", src)
			if code != 0 || !strings.Contains(out, "module verified") {
				t.Fatalf("the module does not verify: exit %d, %q %q", code, out, stderr)
			}
			ir, irErr, irCode := cliStreams(t, "--emit-llvm", src)
			if irCode != 0 {
				t.Fatalf("--emit-llvm exited %d:\n%s", irCode, irErr)
			}
			body := cliMainBody(ir)
			for _, want := range []string{"call i32 @rt_get_elem(", "call i32 @rt_tag_of(", "call i32 @rt_contains_tagged(i32 %"} {
				if !strings.Contains(body, want) {
					t.Fatalf("the program's own body lacks %q:\n%s", want, body)
				}
			}
			for _, ln := range strings.Split(body, "\n") {
				if !strings.Contains(ln, "call i32 @rt_contains_tagged(") {
					continue
				}
				args := ln[strings.Index(ln, "rt_contains_tagged(")+len("rt_contains_tagged("):]
				args = strings.TrimSuffix(args, ")")
				if fields := strings.Split(args, ", "); len(fields) != 3 || !strings.HasPrefix(fields[2], "i32 %") {
					t.Fatalf("the membership call's needle tag is a constant, not the register the objects wrote (%q):\n%s", ln, body)
				}
			}
			// A global in an i32 operand is the instruction `llc` rejects at exit 2 — asserted here rather
			// than discovered by the verifier, because the suite's exit-code contract is the contract.
			for _, ln := range strings.Split(body, "\n") {
				if strings.Contains(ln, "call") && strings.Contains(ln, "i32 @.") {
					t.Fatalf("a module-level global sits in an i32 operand:\n%s\n%s", ln, body)
				}
			}
		})
	}
}

// The whole family as one program, line for line against CPython.
func TestTheMembershipNeedleProgramMatchesTheReferenceThroughTheCLI(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/membership_needle_pair.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if strings.TrimSpace(string(src)) == "" {
		t.Fatal("the program is empty")
	}
	want, err := os.ReadFile("testdata/membership_needle_pair.expected")
	if err != nil {
		t.Fatalf("read the reference's answer: %v", err)
	}
	stdout, stderr, code := cliStreams(t, "--file", "programs/membership_needle_pair.gy")
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

func TestTheMembershipNeedleProgramIsAnAssertedMatrixRow(t *testing.T) {
	t.Parallel()
	cases := conformanceCases()
	var row *lang.ConformanceCase
	for i := range cases {
		if cases[i].Name == "membership_needle_pair.gy" {
			row = &cases[i]
			break
		}
	}
	if row == nil {
		t.Fatal("no conformance matrix row runs membership_needle_pair.gy")
	}
	if !row.Asserted {
		t.Fatalf("row %s is not asserted: its stdout would run unwatched", row.ID)
	}
	if row.Oracle != "match" {
		t.Fatalf("row %s declares oracle %q; the compiled leg answers this program, so it is a match", row.ID, row.Oracle)
	}
	for _, want := range []string{
		`print(words()[0] in words())`,
		"print(nested[0][0] in nested[0])",
		"print(texts[0] in ints)",
	} {
		if !strings.Contains(row.Source, want) {
			t.Fatalf("row %s runs a program without %q", row.ID, want)
		}
	}
}

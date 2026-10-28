package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// A subscript standing on a comprehension the RUN TIME BUILDS asks the object for its slot, its kind and
// its own accident, through the CLI (roadmap **Gap R.501**, **ADR 0442**'s two-comprehensions-one-door
// rule, **ADR 0443**'s brace for the kind).
//
// The family this row belongs to was measured one door at a time: **ADR 0442** moved `len` off the fold's
// global, **ADR 0443** gave the loop, the condition and `bool()` the object, and the subscript stayed.
// `print([g(x) for x in [1, 2, 3]][1])` died with the program's own `IndexError` at exit **3** although the
// object held the answer, and the same read at the set brace refused at exit **1** instead of raising the
// reference's `TypeError`. Every answer and every exception below is asked live of `python3`, and every
// program must earn **exit 0** (or the reference's exception class, with the program allowed to catch it).

const cliBuiltCompDouble = "def g(v):\n    return v * 2\n\n"
const cliBuiltCompIdentity = "def g(v):\n    return v\n\n"

func TestASubscriptOfABuiltComprehensionThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		cliBuiltCompDouble + "print([g(x) for x in [1, 2, 3]][1])",
		cliBuiltCompDouble + "print([g(x) for x in [1, 2, 3]][0])",
		cliBuiltCompDouble + "print([g(x) for x in [1, 2, 3]][-1])",
		cliBuiltCompDouble + "v = [g(x) for x in [1, 2, 3]][1]\nprint(v)",
		cliBuiltCompDouble + "print(len([g(x) for x in [1, 2, 3]]) + [g(x) for x in [1, 2, 3]][1])",
		cliBuiltCompDouble + "print([g(x) for x in [1, 2, 3]][1] == 4)",
		cliBuiltCompIdentity + "print([g(x) for x in [1, 2, 3]][1] == 4)",
		cliBuiltCompDouble + "print([[g(y) for y in [1, 2]] for x in [1, 2]][0][1])",
		cliBuiltCompDouble + "print([[g(y) for y in [1, 2]] for x in [1, 2]][-1][0])",
		cliBuiltCompIdentity + "print({g(x): 1 for x in [1, 2]}[2])",
		cliBuiltCompIdentity + "print({g(x): g(x) for x in [1, 2]}[2])",
		"xs = [1, 2, 3]\n" + cliBuiltCompDouble + "print([g(x) for x in xs][2])",
		cliBuiltCompDouble + "print([g(x) for x in range(3)][1])",
		// The folded control beside the built rows: the fold was not widened, and still answers the
		// same value.
		"print([x * 2 for x in [1, 2, 3]][1])",
		// The item of a built comprehension whose iterable is a container of containers is the item,
		// not the position the fold would have bound (**Gap R.491**, paid by the same door).
		"print([[x for x in [[1]]][0], 2])",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program (exit 2 — ADR 0166's code):\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: the reference prints %q\nstderr %q", code, strings.TrimSpace(want), stderr)
			}
			if got := strings.TrimSpace(stdout); got != strings.TrimSpace(want) {
				t.Fatalf("printed %q, the reference prints %q", got, strings.TrimSpace(want))
			}
			// The record leg is not optional: **Gap R.501** moved the answer out of the filed tables only
			// after `tools/recmerge` wrote the entry from a reference/compiled agreement.
			lang.RecordedStdoutIs(t, src, strings.TrimSpace(want)+"\n")
		})
	}
}

func TestASubscriptTrapStandingOnABuiltComprehensionThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		src     string
		class   string
		message string
	}{
		{
			"past the end",
			cliBuiltCompDouble + "print([g(x) for x in [1, 2, 3]][7])",
			"IndexError", "list index out of range",
		},
		{
			"too far negative",
			cliBuiltCompDouble + "print([g(x) for x in [1, 2, 3]][-9])",
			"IndexError", "list index out of range",
		},
		{
			"a missing built dict key",
			cliBuiltCompIdentity + "print({g(x): 1 for x in [1, 2]}[3])",
			"KeyError", "3",
		},
		{
			"a float where a position belongs",
			cliBuiltCompIdentity + "print([g(x) for x in [1, 2]][1.5])",
			"TypeError", "list indices must be integers or slices, not float",
		},
		{
			"a built set has no position",
			cliBuiltCompIdentity + "print({g(x) for x in [1, 2]}[0])",
			"TypeError", "'set' object is not subscriptable",
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program (exit 2 — ADR 0166's code):\n%s", stderr)
			}
			if code != 3 {
				t.Fatalf("exit %d, want the program's trap at exit 3; stdout %q stderr %q", code, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout %q, want the trap to print nothing", stdout)
			}
			want := tt.class + ": " + tt.message
			if !strings.Contains(stderr, want) {
				t.Fatalf("stderr does not carry %q:\n%s", want, stderr)
			}

			// The reference's own verdict, asked through the same door as every CLI row.
			pyOut, pyErr, runErr := lang.PythonRun(tt.src + "\n")
			if runErr == nil {
				t.Fatalf("the reference answered %q where this row expects %s: %s", pyOut, tt.class, tt.message)
			}
			ref := strings.TrimSpace(pyErr)
			if lines := strings.Split(ref, "\n"); len(lines) > 0 {
				ref = strings.TrimSpace(lines[len(lines)-1])
			}
			if ref != want {
				t.Fatalf("the reference reports %q, this row records %q", ref, want)
			}
			if recErr := lang.RecordedRunError(t, tt.src); recErr == nil {
				t.Fatalf("the record does not trap %q, although the reference and compiled leg both raise %q", tt.src, want)
			}
		})
	}
}

func TestATrapStandingOnABuiltComprehensionIsCatchableThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		cliBuiltCompDouble + "try:\n    print([g(x) for x in [1, 2, 3]][7])\nexcept IndexError as e:\n    print(\"caught\", e)",
		cliBuiltCompIdentity + "try:\n    print({g(x): 1 for x in [1, 2]}[3])\nexcept KeyError as e:\n    print(\"caught\", e)",
		cliBuiltCompIdentity + "try:\n    print({g(x) for x in [1, 2]}[0])\nexcept TypeError as e:\n    print(\"caught\", e)",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program (exit 2):\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: the reference catches this raise\nstderr %q", code, stderr)
			}
			if got := strings.TrimSpace(stdout); got != strings.TrimSpace(want) {
				t.Fatalf("printed %q, the reference prints %q", got, strings.TrimSpace(want))
			}
			lang.RecordedStdoutIs(t, src, strings.TrimSpace(want)+"\n")
		})
	}
}

func TestTheBuiltComprehensionSubscriptTakesTheObjectsRoadThroughTheCLI(t *testing.T) {
	t.Parallel()
	ir, stderr, code := cliStreams(t, "--emit-llvm", cliBuiltCompDouble+"print([g(x) for x in [1, 2, 3]][1])")
	if code != 0 {
		t.Fatalf("--emit-llvm failed on a program the reference answers: %s", stderr)
	}
	for _, want := range []string{
		"call i32 @rt_alloc(i32 1)",
		"call void @rt_append_tagged(i32 %h",
		"call i32 @rt_list_len(i32 %h",
		"call i32 @rt_get_elem(i32 %h",
		"call i32 @rt_tag_of(i32 %h",
	} {
		if !strings.Contains(ir, want) {
			t.Fatalf("the built comprehension subscript does not carry %q:\n%s", want, ir)
		}
	}
	if strings.Contains(ir, "load i32, i32* getelementptr({i32, [0 x i32]}") {
		t.Fatalf("the built comprehension subscript still reads a count out of a zero-length fold global:\n%s", ir)
	}

	folded, ferr, fcode := cliStreams(t, "--emit-llvm", "print([x * 2 for x in [1, 2, 3]][1])")
	if fcode != 0 {
		t.Fatalf("--emit-llvm failed on the fold's control: %s", ferr)
	}
	if !strings.Contains(folded, "load i32, i32* getelementptr({i32, [3 x i32]}, {i32, [3 x i32]}* @.lst") {
		t.Fatalf("the folded comprehension no longer reads the global the fold filled:\n%s", folded)
	}
	if strings.Contains(folded, "call i32 @rt_get_elem(i32") {
		t.Fatalf("the fold was widened onto the built object's read:\n%s", folded)
	}
}

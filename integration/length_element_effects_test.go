package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// The count door performs the elements whose count it takes, through the CLI (roadmap **Gap R.494**,
// **ADR 0441**).
//
// `gustyc --eval 'print(len([f()]))'` printed `1` — the count the spelling states — and never called
// `f`, so the line the program wrote inside the literal never reached the terminal; and
// `gustyc --eval 'print(len([[1, 2][0][0], 1]))'` printed `2` at **exit 0** where CPython raises
// `TypeError: 'int' object is not subscriptable`, where the subscript door beside it (**ADR 0440**)
// raises for the same element written anywhere else, and where an exit-0 wrong answer is the one
// defect this house cannot leave unfiled (**ADR 0166**). Length is asked of a spelled-out container
// from its spelling, and the spelling's ELEMENTS are operations: `(*irGen).lowerLiteralElementsForEffect`
// performs them, in source order, and throws their values away.
//
// Every expectation below is what `python3` prints for the same source — the reference is run live, not
// quoted — and exit **2** (ADR 0166's code for a toolchain failure) is forbidden on every answering row.
// The rows the toolchain still gets wrong are pinned with today's exit code beside the reference's in
// the last table, never deleted.

const cliSideEffectFn = "def f():\n    print(\"side\")\n    return 1\n"

// TestTheCountDoorPerformsItsElementsThroughTheCLI is the effect half: the lines an element prints
// appear on stdout, in the reference's order, before the count.
func TestTheCountDoorPerformsItsElementsThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		cliSideEffectFn + `print(len([f()]))`,
		cliSideEffectFn + `print(len([1, f(), 3]))`,
		cliSideEffectFn + `print(len([1, [f(), 2], 3]))`,
		cliSideEffectFn + `print(len([[f(), 2], 3]))`,
		cliSideEffectFn + `print(len({"a": f()}))`,
		cliSideEffectFn + `print(len({f(), "b"}))`,
		cliSideEffectFn + `print(len(sorted([f(), 2])))`,
		cliSideEffectFn + `print(len([f(), f()]))`,
		cliSideEffectFn + `print(len((f(), 2)))`,
		cliSideEffectFn + `print(len([f(), len([f()])]))`,
		cliSideEffectFn + `xs = [f(), 2]` + "\n" + `print(len(xs))`,
		`print(len([[x for x in [1, 2]], 1]))`,
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: the reference answers this program\nstderr %q", code, stderr)
			}
			if got := strings.TrimSpace(stdout); got != strings.TrimSpace(want) {
				t.Fatalf("printed %q, the reference prints %q", got, strings.TrimSpace(want))
			}
		})
	}
}

// TestTheCountDoorsElementsRaiseThroughTheCLI is the half that answers at the wrong exit code today:
// an element that raises takes the whole program down with it, at the trap class, with the count never
// printed. stdout is checked for SILENCE — an answer the program never earned is the defect this row
// files, so the count appearing there is the regression.
func TestTheCountDoorsElementsRaiseThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		src      string
		contains string
		stdout   string // what the elements got printed before the raise
	}{
		{`print(len([[1, 2][0][0], 1]))`, "TypeError: 'int' object is not subscriptable", ""},
		{`print(len([[1][0][0], 1]))`, "TypeError: 'int' object is not subscriptable", ""},
		{`print(len([[1, 2][0][0]]))`, "TypeError: 'int' object is not subscriptable", ""},
		{`print(len([[1, 2][5]]))`, "IndexError: list index out of range", ""},
		{`print(len([1 // 0]))`, "ZeroDivisionError: integer division or modulo by zero", ""},
		{`print(len([1 // 0, [2][5]]))`, "ZeroDivisionError: integer division or modulo by zero", ""},
		{cliSideEffectFn + `print(len([f(), 1 // 0]))`, "ZeroDivisionError: integer division or modulo by zero", "side"},
	} {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
			}
			if code == 0 {
				t.Fatalf("answered %q at exit 0 where the reference raises %s — a count the program never reached is this row's defect", stdout, tt.contains)
			}
			if code == 1 && strings.Contains(stderr, "codegen") {
				t.Fatalf("the program stopped the build (%q) where the reference raises %s", stderr, tt.contains)
			}
			if code != 3 {
				t.Fatalf("exit %d, want the trap class 3; stdout %q stderr %q", code, stdout, stderr)
			}
			if got := strings.TrimSpace(stdout); got != tt.stdout {
				t.Fatalf("stdout was %q, want the elements' %q and no count", got, tt.stdout)
			}
			if !strings.Contains(stderr, tt.contains) {
				t.Fatalf("raised the wrong sentence, want %q, got:\n%s", tt.contains, stderr)
			}
		})
	}
}

// The raises belong to the program, so the program can catch them — at the exit code that means it
// finished, beside what `python3` prints for the same handler.
func TestTheCountDoorsElementRaisesAreCatchableThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"try:\n    print(len([[1, 2][0][0], 1]))\nexcept TypeError as e:\n    print(\"caught\", e)",
		"try:\n    print(len([[1, 2][5]]))\nexcept IndexError as e:\n    print(\"caught\", e)",
		"try:\n    print(len([1 // 0]))\nexcept ZeroDivisionError as e:\n    print(\"caught\", e)",
		cliSideEffectFn + "try:\n    print(len([f(), 1 // 0]))\nexcept ZeroDivisionError as e:\n    print(\"caught\", e)",
		cliSideEffectFn + "try:\n    print(len([f(), f()]))\nexcept TypeError:\n    print(\"no raise\")",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: the handler took the raise in the reference\nstderr %q", code, stderr)
			}
			if got := strings.TrimSpace(stdout); got != strings.TrimSpace(want) {
				t.Fatalf("printed %q, the reference prints %q", got, strings.TrimSpace(want))
			}
		})
	}
}

// The controls keep the optimisation the fix had to preserve: a container of spellings still answers
// its count as the constant the spelling states — and the module for `print(len([1, 2, 3]))` carries no
// container builder at all (the row's own negative control, asserted at this altitude through
// --emit-llvm).
func TestTheCountDoorControlsStillAnswerThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`print(len([1, 2, 3]))`,
		`print(len([1, [2, 3], 4]))`,
		`print(len([[1, 2], [3, 4]][0]))`,
		`print(len({"a": 1, "b": 2}))`,
		`print(len([1, 2]) + len([3, 4]))`,
		`print(len([1, 2, 3]) * 2)`,
		"xs = [1, 2, 3]\nprint(len(xs))",
		`print(len("abcd"))`,
		`print(len([x for x in [1, 2, 3]]))`,
		`print(len([[1, 2], [3, 4]]))`,
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: the reference answers this program\nstderr %q", code, stderr)
			}
			if got := strings.TrimSpace(stdout); got != strings.TrimSpace(want) {
				t.Fatalf("printed %q, the reference prints %q", got, strings.TrimSpace(want))
			}
		})
	}
	t.Run("module/no builder for a container of spellings", func(t *testing.T) {
		t.Parallel()
		ir, stderr, code := cliStreams(t, "--emit-llvm", `print(len([1, 2, 3]))`)
		if code != 0 {
			t.Fatalf("--emit-llvm failed on a program this row controls: %s", stderr)
		}
		if !strings.Contains(ir, "define i32 @main()") {
			t.Fatalf("--emit-llvm printed no module:\n%s", ir)
		}
		for _, forbidden := range []string{"call i32 @rt_list_new", "call i32 @rt_dict_new", "call i32 @rt_set_new"} {
			if strings.Contains(ir, forbidden) {
				t.Fatalf("the count door built a container just to throw it away (%s):\n%s", forbidden, ir)
			}
		}
	})
}

// TestTheCountDoorAsksTheObjectForAComprehensionTheRunTimeBuildsThroughTheCLI is the door that used to
// spend **ADR 0166**'s exit **2** on a program the reference answers with a digit (**roadmap Gap R.496**,
// **ADR 0442**): `len` of a comprehension the run time BUILDS read the count out of the fold's static
// global, and the fold had never answered that comprehension, so `llc` rejected a load whose pointer was
// a function-local register. The count is a word inside the object now.
//
// Every expectation is what `python3` prints for the same source, asked live; the two rows that COLLAPSE
// duplicates are the ones that prove the number came from the object and not from the iterable.
func TestTheCountDoorAsksTheObjectForAComprehensionTheRunTimeBuildsThroughTheCLI(t *testing.T) {
	t.Parallel()
	const cliDoubleFn = "def g(v):\n    return v * 2\n"
	for _, src := range []string{
		cliDoubleFn + `print(len([g(x) for x in [1, 2, 3]]))`,
		"def f(v):\n    print(\"side\")\n    return v\n" + `print(len([f(x) for x in [1, 2, 3]]))`,
		cliDoubleFn + `print(len([g(x) for x in [1, 2, 3] if g(x) > 2]))`,
		cliDoubleFn + `print(len({g(x): 1 for x in [1, 2, 3]}))`,
		cliDoubleFn + `print(len({g(x) for x in [1, 2, 3]}))`,
		cliDoubleFn + `print(len({g(x) for x in [1, 1, 2]}))`,
		cliDoubleFn + `print(len({g(x) // 4: 1 for x in [1, 2, 3]}))`,
		cliDoubleFn + `print(len([[g(y) for y in [1, 2]] for x in [1, 2]]))`,
		"xs = [1, 2, 3]\n" + cliDoubleFn + `print(len([g(x) for x in xs]))`,
		cliDoubleFn + `print(len([g(x) for x in range(4)]))`,
		cliDoubleFn + `print(len([g(x) for x in [1, 2, 3]]) + 1)`,
		cliDoubleFn + "xs = [g(x) for x in [1, 2, 3]]\nprint(len(xs))",
		cliDoubleFn + "def h():\n    return len([g(x) for x in [1, 2, 3]])\nprint(h())",
		"class C:\n    def m(self, v):\n        return v * 2\nprint(len([C().m(x) for x in [1, 2, 3]]))",
		// The fold's road, kept: a comprehension the compiler CAN write down still answers its count
		// from the global the fold filled — asserted by the module row below.
		`print(len([x * 2 for x in [1, 2, 3]]))`,
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program (exit 2 — ADR 0166's own code):\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: the reference answers this program\nstderr %q", code, stderr)
			}
			if got := strings.TrimSpace(stdout); got != strings.TrimSpace(want) {
				t.Fatalf("printed %q, the reference prints %q", got, strings.TrimSpace(want))
			}
		})
	}
	// The instruction shape, through the machine door an agent reads: the built comprehension asks the
	// object, the folded one keeps its global and asks nothing.
	t.Run("module/the object answers a built comprehension, the fold keeps its global", func(t *testing.T) {
		t.Parallel()
		ir, stderr, code := cliStreams(t, "--emit-llvm", cliDoubleFn+`print(len([g(x) for x in [1, 2, 3]]))`)
		if code != 0 {
			t.Fatalf("--emit-llvm failed on a program the reference answers: %s", stderr)
		}
		if !strings.Contains(ir, "call i32 @rt_heap_len(") {
			t.Fatalf("the module never asked the object how many slots it holds:\n%s", ir)
		}
		if strings.Contains(ir, "getelementptr({i32, [0 x i32]}, {i32, [0 x i32]}* %") {
			t.Fatalf("the module still loads a count out of a register named as a global (the instruction llc rejected):\n%s", ir)
		}
		folded, stderr, code := cliStreams(t, "--emit-llvm", `print(len([x * 2 for x in [1, 2, 3]]))`)
		if code != 0 {
			t.Fatalf("--emit-llvm failed on the fold's control: %s", stderr)
		}
		if strings.Contains(folded, "call i32 @rt_heap_len(") {
			t.Fatalf("the fold's comprehension asked the runtime a question the compile time already answered:\n%s", folded)
		}
		if !strings.Contains(folded, "load i32, i32* getelementptr({i32, [3 x i32]}") {
			t.Fatalf("the fold's comprehension no longer reads the global the fold filled:\n%s", folded)
		}
	})
}

// TestWhatTheCountDoorStillOwesThroughTheCLI pins the residue of the same sweep with today's exit code
// beside the reference's: a divergence is a row only while both legs still disagree, and a row that
// stops being true fails rather than quietly disappearing (**Gap R.204**'s rule, **ADR 0440**'s
// row-hygiene rule).
func TestWhatTheCountDoorStillOwesThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		src       string
		kind      string
		contains  string
		reference string
		row       string
	}{
		// **Gap R.496**'s own row MOVED with the fix — it is answered above, in
		// TestTheCountDoorAsksTheObjectForAComprehensionTheRunTimeBuildsThroughTheCLI, and pinned in the
		// module by its instruction shape. **Gap R.500**'s three rows MOVED with **ADR 0443**'s fix — they
		// are answered in `integration/comp_loop_truth_test.go`, on both sides of the pipe, beside a live
		// `python3`. What is left of that family is the object's ELEMENT (**Gap R.502**) and the subscript
		// door's row below.
		{
			"a subscript of a comprehension the run time builds reads a slot the builder never filled (the subscript door's row)",
			"def g(v):\n    return v * 2\nprint([g(x) for x in [1, 2, 3]][1])",
			"trap3", "IndexError: list index out of range", "4",
			"roadmap Gap R.501",
		},
		{
			"a def inside a `try:` block lands its body inside the enclosing block (the def road's row)",
			"try:\n    def f():\n        return 1\n    print(\"ok\")\nexcept ZeroDivisionError as e:\n    print(\"caught\", e)",
			"exit2", "expected instruction opcode", "ok",
			"roadmap Gap R.497",
		},
		{
			"a def inside an `if` block is called by a refusal (the def road's row)",
			"if 1:\n    def f():\n        return 1\nprint(f())",
			"refusal", "is called but is not a function", "1",
			"roadmap Gap R.498",
		},
		{
			// The one face of the count door's own family it cannot reach, and the shape with no `len` in it
			// that puts it on the SUBSCRIPT door rather than this one: `x = [[True][0][0]]` / `print("ok")`
			// prints `ok` at exit 0 too, identically on the build before **ADR 0441** and on it.
			"an element that reads a verdict answers with a number (the subscript door's row)",
			"print(len([[True][0][0]]))",
			"answer:1", "", "TypeError: 'bool' object is not subscriptable",
			"roadmap Gap R.499",
		},
		{
			"an element that reads the void answers with a number (the subscript door's row)",
			"print(len([[None][0][0]]))",
			"answer:1", "", "TypeError: 'NoneType' object is not subscriptable",
			"roadmap Gap R.499",
		},
		{
			"an element that reads a double answers with a number (the subscript door's row)",
			"print(len([[1.5][0][0]]))",
			"answer:1", "", "TypeError: 'float' object is not subscriptable",
			"roadmap Gap R.499",
		},
		{
			"that element beside a spelling, the same road (the subscript door's row)",
			"print(len([[[None][0][0]], 1]))",
			"answer:2", "", "TypeError: 'NoneType' object is not subscriptable",
			"roadmap Gap R.499",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			// The reference is asked through the same door the oracle uses: a row whose other side has
			// stopped saying this is no longer a divergence, and must move.
			pyOut, pyErr, runErr := lang.PythonRun(tt.src + "\n")
			ref := strings.TrimSpace(pyOut)
			if runErr != nil {
				lines := strings.Split(strings.TrimSpace(pyErr), "\n")
				ref = lines[len(lines)-1]
			}
			if ref != tt.reference {
				t.Fatalf("the reference's answer is not what this row records: got %q, the row says %q (out %q err %q)", ref, tt.reference, pyOut, pyErr)
			}
			joined := stdout + stderr
			if strings.HasPrefix(tt.kind, "answer:") {
				// The toolchain answers this at the exit code of success where the reference raises: the
				// exit-0 wrong answer this house files loudest, pinned with today's bytes because the recorder
				// will not certify it (the legs disagree) and a deleted case is invisible.
				if code != 0 {
					t.Fatalf("%q exited %d, where this row pins today's answer %q at exit 0 (%s):\n%s", tt.src, code, tt.kind[7:], tt.row, joined)
				}
				if got := strings.TrimSpace(stdout); got != tt.kind[7:] {
					t.Fatalf("%q printed %q, not the %q this row pins; the reference answers %q (%s)", tt.src, got, tt.kind[7:], tt.reference, tt.row)
				}
				return
			}
			switch tt.kind {
			case "exit2":
				if code != 2 {
					t.Fatalf("%q exited %d, where this row is filed because the module is rejected at exit 2 (%s):\n%s", tt.src, code, tt.row, joined)
				}
			case "refusal":
				if code != 1 {
					t.Fatalf("%q exited %d, where this row is filed for a refusal at exit 1 (%s):\n%s", tt.src, code, tt.row, joined)
				}
			case "trap3":
				// The program RUNS and raises what the reference does not raise — a wrong answer wearing a
				// traceback, which is neither the exit-0 answer above nor a build that never finished.
				if code != 3 {
					t.Fatalf("%q exited %d, where this row is filed because the program raises where the reference answers %q (%s):\n%s", tt.src, code, tt.reference, tt.row, joined)
				}
			}
			if !strings.Contains(joined, tt.contains) {
				t.Fatalf("%q failed without the words this row pins (%q) (%s):\n%s", tt.src, tt.contains, tt.row, joined)
			}
		})
	}
}

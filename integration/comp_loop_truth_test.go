package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// A comprehension standing where the LOOP, the CONDITION or `bool()` asks for a container is walked,
// tested and measured as the object it is, through the CLI (roadmap **Gap R.490** + **Gap R.500**,
// **ADR 0443**).
//
// Before this commit two of these programs never became modules at all — the loop head compared its
// counter with the comprehension's compile-time GLOBAL (`%_ctr1.ld1, @.lst2`), which `llc-20` rejects and
// the exit-code contract charges the compiler with (**ADR 0166**'s exit **2**) — and a third answered a
// number the program never wrote: `for v in [g(x) for x in [1, 2, 3]]:` printed `0` for CPython's `2`, `4`,
// `6`, and `print(bool([g(x) for x in [1, 2, 3]]))` printed `False` for `True`, all at the exit code of
// success. Exit **2** is forbidden on every row here, and every expectation is what `python3` prints for
// the same source, asked live.

const cliCompFn = "def g(v):\n    return v * 2\n"

func TestTheLoopWalksAComprehensionThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`for v in [x for x in [1]]:` + "\n" + `    print(v)`,
		cliCompFn + `for v in [g(x) for x in [1, 2, 3]]:` + "\n" + `    print(v)`,
		"xs = [1, 2, 3]\n" + cliCompFn + `for v in [g(x) for x in xs]:` + "\n" + `    print(v)`,
		cliCompFn + `for v in [g(x) for x in [1, 2, 3] if g(x) > 2]:` + "\n" + `    print(v)`,
		cliCompFn + `for v in [g(x) for x in range(3)]:` + "\n" + `    print(v)`,
		`for v in {x for x in [3, 3, 4]}:` + "\n" + `    print(v)`,
		cliCompFn + `for v in {g(x) for x in [2, 2, 4]}:` + "\n" + `    print(v)`,
		`for k in {x: 1 for x in ["a", "b"]}:` + "\n" + `    print(k)`,
		cliCompFn + `for v in [[g(y) for y in [1, 2]] for x in [1, 2]]:` + "\n" + `    print(v)`,
		`for v in [x for x in [1, 2, 3]]:` + "\n" + `    if v == 2:` + "\n" + `        break` + "\n" + `    print(v)`,
		`for v in [x for x in [1, 2, 3]]:` + "\n" + `    if v == 2:` + "\n" + `        continue` + "\n" + `    print(v)`,
		`for v in [x for x in [1, 2]]:` + "\n" + `    print(v)` + "\n" + `else:` + "\n" + `    print("else ran")`,
		`for v in [x for x in []]:` + "\n" + `    print(v)` + "\n" + `else:` + "\n" + `    print("else ran")`,
		`for v in [x for x in [9]]:` + "\n" + `    print(v)` + "\n" + `for v in [4, 5]:` + "\n" + `    print(v)`,
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program (exit 2 — ADR 0166's own code):\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: the reference walks this loop\nstderr %q", code, stderr)
			}
			if got := strings.TrimSpace(stdout); got != strings.TrimSpace(want) {
				t.Fatalf("printed %q, the reference prints %q", got, strings.TrimSpace(want))
			}
		})
	}
}

func TestTheConditionAndBoolAskAComprehensionThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`if [x for x in [1]]:` + "\n" + `    print("yes")` + "\n" + `else:` + "\n" + `    print("no")`,
		cliCompFn + `if [g(x) for x in [1, 2, 3]]:` + "\n" + `    print("yes")` + "\n" + `else:` + "\n" + `    print("no")`,
		`if [x for x in []]:` + "\n" + `    print("yes")` + "\n" + `else:` + "\n" + `    print("no")`,
		"def g(v):\n    return v\n" + `if [g(x) for x in []]:` + "\n" + `    print("yes")` + "\n" + `else:` + "\n" + `    print("no")`,
		`print(bool([x for x in [1]]))`,
		cliCompFn + `print(bool([g(x) for x in [1, 2, 3]]))`,
		`print(bool([x for x in []]))`,
		"def g(v):\n    return v\n" + `print(bool({g(x) for x in [1, 1]}))`,
		`print(bool({x: 1 for x in []}))`,
		"def g(v):\n    return v\n" + `print(bool({g(x): 1 for x in [7]}))`,
		`print(not [x for x in [1]])`,
		`if [x for x in [1]] and [x for x in []]:` + "\n" + `    print("both")` + "\n" + `else:` + "\n" + `    print("not both")`,
		`if [x for x in []] or [x for x in [2]]:` + "\n" + `    print("one")` + "\n" + `else:` + "\n" + `    print("none")`,
		// The controls: the doors beside these keep answering the way they always did.
		`print(bool(""))`,
		`print(bool(0))`,
		`print(bool(1))`,
		`xs = [1, 2]` + "\n" + `if xs:` + "\n" + `    print("truthy")`,
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program (exit 2):\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: the reference answers this program\nstderr %q", code, stderr)
			}
			if got := strings.TrimSpace(stdout); got != strings.TrimSpace(want) {
				t.Fatalf("printed %q, the reference prints %q", got, strings.TrimSpace(want))
			}
		})
	}
	// The instruction shape, through the machine door: the loop walks a handle and no module compares a
	// counter with a container's global — the instruction this row was filed for.
	t.Run("module/no loop compares its counter with a comprehension global", func(t *testing.T) {
		t.Parallel()
		for _, src := range []string{
			`for v in [x for x in [1]]:` + "\n" + `    print(v)`,
			cliCompFn + `for v in [g(x) for x in [1, 2, 3]]:` + "\n" + `    print(v)`,
		} {
			ir, stderr, code := cliStreams(t, "--emit-llvm", src)
			if code != 0 {
				t.Fatalf("--emit-llvm failed on a program the reference walks: %s", stderr)
			}
			if !strings.Contains(ir, "call i32 @rt_list_len(") {
				t.Fatalf("the loop never asked the object how many slots it holds:\n%s", ir)
			}
			for _, line := range strings.Split(ir, "\n") {
				if strings.Contains(line, "icmp slt i32 %_ctr") && strings.Contains(line, "@.") {
					t.Fatalf("the module compares a loop counter with a container global (ADR 0188, ADR 0166's exit 2):\n%s", line)
				}
			}
		}
	})
}

// TestWhatTheComprehensionLoopStillOwesThroughTheCLI pins the residue with today's exit code beside the
// reference's.
//
// The three answer rows this table carried while **Gap R.502** was open — a loop item of a built
// comprehension over a mixed iterable, the same comprehension printed whole, and a built dict
// comprehension's KEY — print the reference's bytes now and moved to `integration/forwarded_answer_test.go`
// with **ADR 0445**; the rows that remain are positions this door deliberately does not widen.
func TestWhatTheComprehensionLoopStillOwesThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		src       string
		kind      string
		contains  string
		reference string
		row       string
	}{
		{
			// Not this door's debt: `bool` of a container VARIABLE or a spelled-out literal has refused in
			// **Gap R.131**'s own words for as long as `bool` had a road — identically on the build before
			// **ADR 0443** and on it. A COMPREHENSION is answered now because its object is the one this
			// backend already built and can hand to `rt_heap_len`; the name's and the literal's truthiness
			// stay the neighbouring row's.
			"bool of a named container still refuses in Gap R.131's words",
			"xs = [1, 2]\nprint(bool(xs))",
			"refusal", "a container's truthiness is its length", "True",
			"roadmap Gap R.131",
		},
		{
			"a comprehension loop item handed to a call is refused naming the one-word position",
			"def show(v):\n    print(v)\nfor v in [x * 2 for x in [1, 2]]:\n    show(v)",
			"refusal", "the element a loop stepped over", "2\n4",
			"roadmap L11.1 · Gap R.146 (the same program was exit 2 before ADR 0443)",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
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
				if code != 0 {
					t.Fatalf("%q exited %d, where this row pins today's answer %q at exit 0 (%s):\n%s", tt.src, code, tt.kind[7:], tt.row, joined)
				}
				if got := strings.TrimSpace(stdout); got != strings.TrimSpace(tt.kind[7:]) {
					t.Fatalf("%q printed %q, not the %q this row pins; the reference answers %q (%s)", tt.src, got, tt.kind[7:], tt.reference, tt.row)
				}
				return
			}
			if tt.kind == "refusal" {
				if code != 1 {
					t.Fatalf("%q exited %d, where this row is filed for a refusal at exit 1 (%s):\n%s", tt.src, code, tt.row, joined)
				}
			}
			if !strings.Contains(joined, tt.contains) {
				t.Fatalf("%q failed without the words this row pins (%q) (%s):\n%s", tt.src, tt.contains, tt.row, joined)
			}
		})
	}
}

// compBuildSurvivalCLIPrograms are the programs whose elements CALL — and therefore collect — while the
// comprehension that holds them is still being filled (roadmap **Gap R.500**, **ADR 0443**; **ADR 0181**,
// **ADR 0347**).
var compBuildSurvivalCLIPrograms = []string{
	"def g(v):\n    y = len([v * a for a in [2, 3]])\n    return v * 2\n\nfor v in [g(x) for x in [1, 2, 3]]:\n    print(v)",
	"def g(v):\n    total = 0\n    for a in [v, v + 1, v + 2]:\n        total = total + len([a * b for b in [2, 3]])\n    return v * 2\n\nfor v in [g(x) for x in [1, 2, 3]]:\n    print(v)",
	"def g(v):\n    total = 0\n    for a in [v, v + 1, v + 2]:\n        total = total + len([a * b for b in [2, 3]])\n    return v * 2\n\nprint(bool([g(x) for x in [1, 2, 3]]))\nprint(len([g(x) for x in [1, 2, 3]]))\nfor v in [g(x) for x in [1, 2, 3]]:\n    print(v)",
	"def g(v):\n    y = len([v * a for a in [2, 3]])\n    return v\n\nfor v in {g(x) for x in [1, 1, 2]}:\n    print(v)",
	"def g(v):\n    y = len([v * a for a in [2, 3]])\n    return v * 2\n\nfor k in {x: g(x) for x in [1, 2]}:\n    print(k)",
}

// TestABuiltComprehensionSurvivesItsBuildThroughTheCLI is the second pillar of **ADR 0443** at the command
// line: the object a comprehension builder allocates is rooted for the duration of its own BUILD, because the
// road that fills it is free to CALL and a call's body collects (**ADR 0347**). The attribution is a control
// build — with `gcRootDuringBuild` emptied and nothing else changed, the second program prints `10`, `15`,
// `6` and the first prints `4`, `6`, `4`, `6`, both at **exit 0** where the reference prints `2`, `4`, `6`. A
// container that is live and unrooted does not fail; the run answers a different program, which is the class
// **ADR 0166** files loudest.
func TestABuiltComprehensionSurvivesItsBuildThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, src := range compBuildSurvivalCLIPrograms {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program (exit 2 — ADR 0166's own code):\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: the reference runs this program\nstderr %q", code, stderr)
			}
			if got := strings.TrimSpace(stdout); got != strings.TrimSpace(want) {
				t.Fatalf("printed %q, the reference prints %q — a comprehension whose elements call must be "+
					"rooted for its own build (ADR 0181, ADR 0347, ADR 0443)", got, strings.TrimSpace(want))
			}
		})
	}
	// The machine door: the module parks the half-built object in a root slot before the element's call and
	// clears that root when the build ends, so nothing the program stopped holding stays live.
	t.Run("the module roots the object for its build and clears the root after", func(t *testing.T) {
		t.Parallel()
		ir, stderr, code := cliStreams(t, "--emit-llvm", compBuildSurvivalCLIPrograms[1])
		if code != 0 {
			t.Fatalf("--emit-llvm failed on a program the reference runs: %s", stderr)
		}
		for _, want := range []string{"call void @rt_root_put(i32* %_cb", "call void @rt_root_clear(i32* %_cb"} {
			if !strings.Contains(ir, want) {
				t.Fatalf("the built comprehension does not carry %q:\n%s", want, ir)
			}
		}
		alloc := strings.Index(ir, "= call i32 @rt_alloc(")
		root := strings.Index(ir, "call void @rt_root_put(i32* %_cb")
		call := strings.Index(ir, "call i32 @gy_g(")
		if !(alloc >= 0 && root > alloc && call > root) {
			t.Fatalf("the object is not rooted between its allocation and the call that can collect it")
		}
	})
}

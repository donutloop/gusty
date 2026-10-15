package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// integration/module_slot_scope_test.go — the reference leg of roadmap Gap R.49 / ADR 0351.
//
// The unit file (`pkg/lang/module_slot_scope_test.go`) owns the tables and the IR invariants. This one
// asks the same programs of the only interface an agent has: an exit code, stdout, and the traceback.
// A store that went to a slot the function never allocated is invisible at the source altitude and fatal
// at this one — it is how `for y in [10, 20]` reached `llc` and came back with exit 2, the code the
// contract reserves for a compiler bug (ADR 0166) — so every row here forbids exit 2 as well as a wrong
// line.
//
// Every expectation is produced by the pinned CPython in the test body (`pythonOutput`), not transcribed.
// The shapes are the crossings that hid the defect: a name that is a parameter AND a module variable, a
// name that is a loop target AND a module variable, and a name that is all three.

// moduleScopeCrossings is the family, at the CLI altitude.
var moduleScopeCrossings = []string{
	// the row as filed: a module name that is not the parameter's name
	"def f(x):\n    return x * 2\n\nx = 8\nx /= 2\nprint(x)",
	// the same binding on the integer road, which had been answering from a slot nobody read
	"def f(x):\n    return x * 2\n\nx = 8\nx += 2\nprint(x)",
	// a loop target that is also a module name, read before, inside and after the loop
	"def g(y):\n    return y\n\ny = 2\nfor y in [10, 20]:\n    print(y)\nprint(y)",
	// a loop body that rebinds its own target
	"def g(y):\n    return y\n\ny = 0\nfor y in [1, 2, 3]:\n    y += 1\n    print(y)\nprint(y)",
	// augmented assignment on a module name
	"def g(y):\n    return y\n\ny = 4\ny += 1\nprint(y)",
	// an accumulator bound by the module, written from the loop
	"def g(y):\n    return y\n\ntotal = 0\nfor y in [1, 2]:\n    total += y\nprint(total, y)",
	// the name no statement ever bound at module level before the loop did
	"def g(y):\n    return y\n\nfor y in [1, 2]:\n    print(y)\nprint(y)",
	// a body that reads a module name it does not bind — the one case that earns a global
	"def g():\n    return y\n\ny = 0\nfor y in [1, 2]:\n    print(y)\nprint(y)",
	// a parameter in one function, a module read in another
	"def g(y):\n    return y\n\ndef h():\n    return y\n\ny = 4\nprint(g(9), h(), y)",
	// a body's local assignment shadows the module for the whole body
	"def g(y):\n    y = y + 1\n    return y\n\ny = 1\nprint(g(1), y)",
	// a default that reads the module, beside a parameter of the same name
	"def f(a=2):\n    return a\n\na = 9\nprint(a, f())",
	// text through the same roads
	"def g():\n    return s\n\ns = \"a\"\ns = \"b\"\nprint(s)",
}

// TestAModuleNameAnswersWhatTheReferenceAnswersThroughTheCLI runs each crossing twice, once by the
// reference and once through the CLI, and refuses the three ways this row used to fail: exit 2 (the
// compiler's own bug), a refusal at exit 1 for a program the reference runs, and a wrong number at the
// exit code of success.
func TestAModuleNameAnswersWhatTheReferenceAnswersThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, src := range moduleScopeCrossings {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program — an ordinary program reached llc and was rejected:\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: the reference runs this program, so a refusal is a wrong answer\nstderr %q", code, stderr)
			}
			if got := strings.TrimSpace(stdout); got != strings.TrimSpace(want) {
				t.Fatalf("printed %q, the reference prints %q", got, strings.TrimSpace(want))
			}
		})
	}
}

// TestTheGlobalIsForTheReadAndNotForTheName is the gate at the IR altitude, through the same
// `lang.Compile` the CLI calls: a name whose only body mention is a parameter earns no module global, and
// a name a body reads without binding it earns exactly one — written by every road that binds it, which is
// the assertion the two roads that had gone their own way fail (a loop that stores only the main-frame
// alloca, an augmented assignment that stores nothing).
func TestTheGlobalIsForTheReadAndNotForTheName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		src     string
		stores  int // minimum writes to @gy_mod_<name>
		wantSym string
	}{
		{
			name:    "a name only read as a parameter earns no global",
			src:     "def g(y):\n    return y\n\ny = 2\nfor y in [10, 20]:\n    print(y)\nprint(y)\n",
			stores:  0,
			wantSym: "",
		},
		{
			name:    "a body that reads the name without binding it earns three writes",
			src:     "def g():\n    return y\n\ny = 0\nfor y in [1, 2, 3]:\n    print(y)\n",
			stores:  3,
			wantSym: "@gy_mod_y",
		},
		{
			name:    "augmented assignment writes it too",
			src:     "def g():\n    return y\n\ny = 1\ny += 3\nprint(y)\n",
			stores:  1,
			wantSym: "@gy_mod_y",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := lang.Compile(tc.src)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if tc.wantSym == "" {
				if strings.Contains(res.IR, "@gy_mod_") {
					t.Fatalf("the module carries a global for a name no body reads:\n%s", moduleGlobals(res.IR))
				}
				return
			}
			got := strings.Count(res.IR, "i32* "+tc.wantSym)
			if got < tc.stores {
				t.Fatalf("%q bound %s %d time(s), want at least %d — a road that writes only the alloca leaves every read looking at a value nothing produced\n%s",
					tc.src, tc.wantSym, got, tc.stores, moduleGlobals(res.IR))
			}
		})
	}
}

// TestTheModuleBindingLinksAndRunsThroughTheLinker is the leg where the row's exit-2 symptom could only
// be seen: `--build` runs `llc` on the emitted module and `cc` on the object, so a store to a slot the
// function never allocated is a hard rejection here (`use of undefined value '%_y'`) rather than a JIT
// that papers over it. The two shapes are the two roads that had gone their own way — the loop that bound
// where nobody read, and the augmented assignment that stored to an unallocated slot — and each is run as
// a native binary and compared with the reference in the same breath.
func TestTheModuleBindingLinksAndRunsThroughTheLinker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)
	for i, src := range []string{
		"def g(y):\n    return y\n\ny = 2\nfor y in [10, 20]:\n    print(y)\nprint(y)\n",
		"def g():\n    return y\n\ny = 1\ny += 3\nprint(y)\n",
		"def f(x):\n    return x * 2\n\nx = 8\nx /= 2\nprint(x)\n",
	} {
		t.Run(strings.ReplaceAll(strings.TrimSpace(src), "\n", "; "), func(t *testing.T) {
			t.Parallel()
			want := strings.TrimSpace(pythonOutput(t, src))
			srcPath := writeSrc(t, dir, "mod_slot.gy", src)
			out := filepath.Join(dir, fmt.Sprintf("prog_%d", i))
			build := exec.Command(bin, "--build", out, srcPath)
			if buildOut, err := build.CombinedOutput(); err != nil {
				t.Fatalf("the toolchain failed on its own program (exit-2 class, roadmap Gap R.49 / ADR 0351): %v\n%s", err, buildOut)
			}
			run := exec.Command(out)
			got, err := run.Output()
			if err != nil {
				t.Fatalf("run the built binary: %v\n%s", err, got)
			}
			if strings.TrimSpace(string(got)) != want {
				t.Fatalf("the linked binary printed %q, the reference prints %q", strings.TrimSpace(string(got)), want)
			}
		})
	}
}

// TestTheMachinePathReportsTheModuleBindingIs the feature's machine consumption path: an agent reading the
// JSON payload gets the same answer the human sees on stdout, and gets it at exit 0 — the answer this row
// used to withhold behind an exit 2.
func TestTheMachinePathReportsTheModuleBinding(t *testing.T) {
	t.Parallel()
	src := "def g(y):\n    return y\n\ny = 2\nfor y in [10, 20]:\n    print(y)\nprint(y)"
	out, code := cliRunCode(t, "--json", "--eval", src)
	if code != 0 {
		t.Fatalf("exit %d, want 0 — the contract (ADR 0166) reserves 2 for a compiler bug and this program used to leave it\n%s", code, out)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("the JSON payload does not parse: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"10\n20\n20\n"`) && !strings.Contains(out, "10\n20\n20") {
		t.Fatalf("the payload carries no answer for the loop that binds a module name:\n%s", out)
	}
}

// moduleGlobals lists the module-global definitions, for the failure messages.
func moduleGlobals(ir string) string {
	found := []string{}
	for _, line := range strings.Split(ir, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "@gy_mod_ = internal global") {
			found = append(found, strings.TrimSpace(strings.SplitN(strings.TrimSpace(line), " ", 2)[0]))
		}
	}
	if len(found) == 0 {
		return "no @gy_mod_ globals"
	}
	return "globals: " + strings.Join(found, ", ")
}

// TestTheModuleScopeProgramMatchesTheReference is the whole row as one program, line for line against
// the answer CPython gave it (`testdata/module_slot_scope.expected`, written by CPython and never by this
// toolchain). The program reuses one name across the module, a `def`'s parameter and a `for` target — the
// crossing the two defects lived on — and covers both roads that had gone their own way: the loop that
// bound where nobody read, and the augmented assignment that stored to a slot which had never existed.
func TestTheModuleScopeProgramMatchesTheReference(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/module_slot_scope.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if strings.TrimSpace(string(src)) == "" {
		t.Fatal("the program is empty")
	}
	want, err := os.ReadFile("testdata/module_slot_scope.expected")
	if err != nil {
		t.Fatalf("read the reference's answer: %v", err)
	}
	stdout, stderr, code := cliStreams(t, "--file", "programs/module_slot_scope.gy")
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

// TestTheModuleScopeProgramIsOnTheRecord is ADR 0302's rule at this altitude: the program's answer
// survives as a record, and a record that disappeared fails here rather than passing by having nothing to
// compare against.
func TestTheModuleScopeProgramIsOnTheRecord(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/module_slot_scope.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if !lang.HasGoldenAnswer(string(src)) {
		t.Fatal("integration/programs/module_slot_scope.gy is not on the record — record it from the reference with tools/recmerge, do not self-certify it from this backend")
	}
}

// TestWhatTheModuleScopeCycleFiledNextDoorThroughTheCLI pins the two exit-2 shapes this cycle measured
// and did not fix, with the reference's answer beside them. Both are the same neighbourhood as the row
// itself — an IR name minted from a NAME, as if a name were an identity — and both are ordinary programs.
//
//   - two nested `def`s sharing a name collide on `@h_slot` (roadmap Gap R.274); CPython prints `1 2`,
//     and naming the two inners differently makes the same program compile, which is the proof that what
//     collided was the name;
//   - a module name defined twice collides on `@gy_h` (roadmap Gap R.275); CPython prints the second
//     definition's answer, because in Python the later binding wins and the first is dead code.
func TestWhatTheModuleScopeCycleFiledNextDoorThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc     string
		src      string
		contains string
	}{
		{
			"two nested defs sharing a name",
			"def a():\n    x = 1\n    def h():\n        return x\n    return h()\n\ndef b():\n    x = 2\n    def h():\n        return x\n    return h()\n\nprint(a(), b())",
			"redefinition of global",
		},
		{
			"a module name defined twice",
			"def h():\n    return 1\n\ndef h():\n    return 2\n\nprint(h())",
			"invalid redefinition of function",
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			want := strings.TrimSpace(pythonOutput(t, tc.src+"\n"))
			stdout, stderr, code := cliStreams(t, "--eval", tc.src)
			if !strings.Contains(stderr, tc.contains) {
				t.Fatalf("this shape was filed as %q and now behaves differently (exit %d, stderr %q): the row needs re-measuring — and if it now prints %q, close the row and delete this case",
					tc.contains, code, strings.TrimSpace(stderr), want)
			}
			if code != 2 {
				t.Fatalf("exit %d, want 2 — the contract (ADR 0166) reserves 2 for a compiler bug, and llc refusing a program the reference runs is exactly that\n%s", code, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout %q is not empty while the module was rejected: a program that does not compile prints nothing and says it failed", stdout)
			}
		})
	}
}

package lang

// ternary_arms_test.go — `a if c else b` runs the arm the test chooses, and only that arm
// (Gap R.340, ADR 0374).
//
// The ternary used to be a `select`. Codegen walked BOTH arms into the block the test itself sits
// in, folded the pair into one LLVM value, and handed the condition to the instruction that picks
// after the choosing is already done:
//
//	def boom():
//	    print("boom")
//	    return 1
//	x = 5
//	print(1 if x > 0 else boom())      # printed `boom`, then `1`
//
// `select` has no unreachable arm — both operands are operands, so both arms' instructions run
// whichever way the test goes. Measured on the shipped binary before this file: the line above
// printed `boom`; `print(1 if x > 0 else 1 // 0)` died with a ZeroDivisionError from an arm the
// program never took; and
//
//	def fact(n):
//	    return 1 if n <= 1 else n * fact(n - 1)
//	print(fact(5))
//
// died with "the compiled root stack is full (recursion too deep)" and exit 2 — the recursive call
// had been emitted unconditionally, in the block with the test, so the function had no base case and
// the artifact was killed rather than returning.
//
// ADR 0275 fixed exactly this for `and`/`or` — a branch, the operand evaluated only in the block that
// reaches it, a block-parameter `phi` carrying the two results to the merge — and left the ternary on
// the eager path, filed as the second half of the same bug. These tests pin the branch: the non-taken
// arm's call is emitted once and after the branch that chooses it, the module still verifies, a pair
// of pure arms still folds to a `select`, and an arm that does trap still raises its own class.
//
// Both witness legs: the record leg below asks `goldenStdoutIs` for the sources the artifact holds,
// and integration/ternary_arms_test.go asks the reference for the same rows through the CLI.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ternaryArmAnswer is one compiled run: what the program printed, the exit it ended on, and the class
// and sentence of the raise when it trapped.
type ternaryArmAnswer struct {
	stdout  string
	exit    int
	class   string
	message string
}

func ternaryArmRun(t *testing.T, src string) ternaryArmAnswer {
	t.Helper()
	res, err := RunSnippet(src + "\n")
	if err != nil {
		t.Fatalf("the ternary did not compile: %v\n%s", err, src)
	}
	_, class, message := ParseTrapReport(res.Stderr)
	return ternaryArmAnswer{stdout: res.Output, exit: res.Code, class: class, message: message}
}

// ternaryArmModule is the module the JIT was handed for this source — the codegen's own text.
func ternaryArmModule(t *testing.T, src string) string {
	t.Helper()
	res, err := RunSnippet(src + "\n")
	if err != nil {
		t.Fatalf("the ternary did not compile: %v\n%s", err, src)
	}
	if res.IR == "" {
		t.Fatalf("the run reported no module for:\n%s", src)
	}
	return res.IR
}

// ternaryArmMainBody is the `@main` block of that module. The shape assertions read the program's own
// code and not the runtime preamble the same module carries, whose blocks legitimately contain
// `select` and `phi` of their own.
func ternaryArmMainBody(t *testing.T, src string) string {
	t.Helper()
	ir := ternaryArmModule(t, src)
	start := strings.Index(ir, "define i32 @main()")
	if start < 0 {
		t.Fatalf("the module has no @main:\n%s", ir)
	}
	end := strings.Index(ir[start:], "\n}")
	if end < 0 {
		t.Fatalf("@main is unterminated:\n%s", ir)
	}
	return ir[start : start+end]
}

// ternaryArmMustPrint pins a program that answers without trapping: an arm that ran when it should not
// have shows up either in the bytes printed or in the trap that never should have happened.
func ternaryArmMustPrint(t *testing.T, name, src, want string) {
	t.Helper()
	got := ternaryArmRun(t, src)
	if got.exit != 0 {
		t.Errorf("%s: exit %d (%s: %s), want exit 0 printing %q — a trap from the arm the test did not "+
			"choose is the eager-select defect", name, got.exit, got.class, got.message, want)
		return
	}
	if got.stdout != want {
		t.Errorf("%s: printed %q, want %q", name, got.stdout, want)
	}
}

// ternaryArmMustTrap is the other half of the branch: short-circuiting must not turn a raise into
// silence. The class and the sentence come from the arm that actually ran.
func ternaryArmMustTrap(t *testing.T, name, src, wantClass, wantMessage, wantStdout string) {
	t.Helper()
	got := ternaryArmRun(t, src)
	if got.exit == 0 {
		t.Errorf("%s: exit 0 printing %q, want a %s from the arm the test chose", name, got.stdout, wantClass)
		return
	}
	if got.class != wantClass {
		t.Errorf("%s: raised %q (%s), want %s", name, got.class, got.message, wantClass)
	}
	if wantMessage != "" && got.message != wantMessage {
		t.Errorf("%s: message %q, want %q", name, got.message, wantMessage)
	}
	if wantStdout != "" && got.stdout != wantStdout {
		t.Errorf("%s: printed %q before the raise, want %q", name, got.stdout, wantStdout)
	}
}

// TestTheUntakenArmOfATernaryDoesNotRun is the gap in one table. Every row's non-taken arm does
// something the program would notice — prints, raises, allocates — and the row passes only if the
// program never notices it.
func TestTheUntakenArmOfATernaryDoesNotRun(t *testing.T) {
	t.Parallel()
	ternaryArmMustPrint(t, "a call in the untaken else arm of an int ternary",
		"def boom():\n    print(\"boom\")\n    return 1\n\n\nx = 5\nprint(1 if x > 0 else boom())\n", "1\n")
	ternaryArmMustPrint(t, "a call in the untaken then arm of an int ternary",
		"def boom():\n    print(\"boom\")\n    return 1\n\n\nx = 5\nprint(boom() if x < 0 else 2)\n", "2\n")
	ternaryArmMustPrint(t, "a call in the untaken else arm of a double ternary",
		"def boom():\n    print(\"boom\")\n    return 1.5\n\n\nx = 5\nprint(2.5 if x > 0 else boom())\n", "2.5\n")
	ternaryArmMustPrint(t, "a call in the untaken else arm of a text ternary",
		"def boom():\n    print(\"SHOUT\")\n    return \"b\"\n\n\nx = 5\nprint(\"yes\" if x > 0 else boom())\n", "yes\n")
	ternaryArmMustPrint(t, "a call in the untaken else arm of a verdict ternary",
		"def boom():\n    print(\"boom\")\n    return True\n\n\nx = 5\nprint(False if x > 0 else boom())\n", "False\n")
	ternaryArmMustPrint(t, "a call in the untaken else arm of a value ternary",
		"def boom():\n    print(\"boom\")\n    return 7\n\n\nx = 5\nprint(3 if x > 0 else boom())\n", "3\n")
	// The raises the eager path produced from the arm that never runs.
	ternaryArmMustPrint(t, "a division by zero in the dead arm",
		"x = 5\nprint(1 if x > 0 else 1 // 0)\n", "1\n")
	ternaryArmMustPrint(t, "a division by zero in the dead arm of the double half",
		"x = 5.0\nprint(x if x > 0 else 1.0 // 0.0)\n", "5.0\n")
	ternaryArmMustPrint(t, "a raise from inside a function in the dead arm",
		"def boom():\n    xs = [1, 2]\n    return xs[9]\n\n\nx = 5\nprint(1 if x > 0 else boom())\n", "1\n")
	ternaryArmMustPrint(t, "a missing dictionary key in the dead arm",
		"d = {}\nprint(1 if 1 else d[\"k\"])\n", "1\n")
	ternaryArmMustPrint(t, "an out-of-range index in the dead arm",
		"xs = [1, 2]\nprint(1 if 1 else xs[9])\n", "1\n")
	// A slot the dead arm reads may be unwritten on the path that reaches the ternary. The ADR 0228
	// guard cannot be hoisted out of a branch — both arms of a select dominate the test, so the guard
	// runs at the test — and the eager path therefore raised UnboundLocalError for a name the taken
	// arm never mentions.
	ternaryArmMustPrint(t, "a maybe-unwritten slot behind the dead arm",
		"def f(flag):\n    if flag:\n        y = 7\n    return (1 if flag else y) + 0\n\n\nprint(f(True))\n", "1\n")
	// Two ternaries in one expression, and one nested in the other: each picks its own arm.
	ternaryArmMustPrint(t, "two ternaries in one sum",
		"def boom():\n    print(\"boom\")\n    return 1\n\n\nc = 1\nd = 1\nprint((1 if c else 2) + (3 if d else boom()))\n", "4\n")
	ternaryArmMustPrint(t, "a ternary inside the untaken arm of another",
		"def boom():\n    print(\"boom\")\n    return 1\n\n\nc = 1\ne = 1\nprint((2 if e else boom()) if c else boom())\n", "2\n")
	// An arm is built in the block that evaluates it, so the call inside it belongs to that block and
	// not to the block with the test. (A container arm is a different door: `[1] if c else [boom()]` is
	// refused by the list-literal element typing, before and after this change — the refusal is
	// unchanged, so the row asks about a scalar arm.)
	ternaryArmMustPrint(t, "through an assignment",
		"def boom():\n    print(\"boom\")\n    return 1\n\n\nx = 5\ny = 4 if x > 0 else boom()\nprint(y)\n", "4\n")
	ternaryArmMustPrint(t, "through a lambda body",
		"def boom():\n    print(\"boom\")\n    return 1\n\n\nf = lambda x: 1 if x else boom()\nprint(f(True))\n", "1\n")
	// The rows that never had a problem, pinned so this cycle cannot pay for the branch with them.
	ternaryArmMustPrint(t, "constant test, then arm", `print(7 if 1 else 9)`, "7\n")
	ternaryArmMustPrint(t, "constant test, else arm", `print(7 if 0 else 9)`, "9\n")
	ternaryArmMustPrint(t, "run-time test, both arms literal", "x = 3\nprint(1 if x > 2 else 2)\n", "1\n")
	ternaryArmMustPrint(t, "run-time test, the other arm", "x = 1\nprint(1 if x > 2 else 2)\n", "2\n")
	ternaryArmMustPrint(t, "a dead call on the double road",
		"def boom():\n    print(\"boom\")\n    return 3.5\n\n\nx = 1\nprint(2.5 if x else boom())\n", "2.5\n")
	ternaryArmMustPrint(t, "arms that are arithmetic on literals",
		"x = 5\nprint(1 + 1 if x > 0 else 2 + 2)\n", "2\n")
	ternaryArmMustPrint(t, "a method call in the dead arm",
		"x = 5\nprint(1 if x > 0 else \"ab\".upper())\n", "1\n")
}

// TestTheChosenArmOfATernaryStillTraps is the other half of the branch. The class and sentence come
// from the arm that ran; a print that happened before the ternary is still on the record.
func TestTheChosenArmOfATernaryStillTraps(t *testing.T) {
	t.Parallel()
	ternaryArmMustTrap(t, "the taken arm divides by zero",
		"x = 0\nprint(1 if x > 0 else 1 // 0)\n", "ZeroDivisionError", "", "")
	ternaryArmMustTrap(t, "the taken arm calls a function whose body traps",
		"def boom():\n    return 1 // 0\n\n\nx = 5\nprint(boom() if x > 0 else 1)\n", "ZeroDivisionError", "", "")
	ternaryArmMustTrap(t, "the taken arm reads a missing dictionary key",
		"d = {}\nprint(1 if 0 else d[\"k\"])\n", "KeyError", "'k'", "")
	ternaryArmMustTrap(t, "the taken arm indexes out of range",
		"xs = [1, 2]\nprint(1 if 0 else xs[9])\n", "IndexError", "list index out of range", "")
	ternaryArmMustTrap(t, "the taken arm calls a function whose body raises",
		"def boom():\n    raise TypeError(\"late\")\n\n\nx = 5\nprint(boom() if x > 0 else 1)\n", "TypeError", "late", "")
	ternaryArmMustTrap(t, "what printed before the ternary survives the raise",
		"x = 0\nprint(\"before\")\nprint(1 if x > 0 else 1 // 0)\n", "ZeroDivisionError", "", "before\n")
	// The reverse: a trap inside a function the dead arm names must not escape — the call is simply
	// never made.
	ternaryArmMustPrint(t, "a function whose body would trap is not called",
		"def boom():\n    return 1 // 0\n\n\nx = 5\nprint(1 if x > 0 else boom())\n", "1\n")
}

// TestTheTernaryRecordLegKeepsTheTrapOfTheArmItChose is the record's trap half. Each source raises from
// the arm its test chose; the record holds the reference's class and sentence, and the compiled run has
// to end on that raise rather than answering a value — a branch that swallowed the arm's trap would look
// like a program that finally works.
func TestTheTernaryRecordLegKeepsTheTrapOfTheArmItChose(t *testing.T) {
	t.Parallel()
	for _, row := range []struct{ src, class, sentence string }{
		{"x = 0\nprint(1 if x > 0 else 1 // 0)\n", "ZeroDivisionError", ""},
		{"xs = [1, 2]\nprint(1 if 0 else xs[9])\n", "IndexError", "list index out of range"},
		{"d = {}\nprint(1 if 0 else d[\"k\"])\n", "KeyError", "'k'"},
		{"def boom():\n    return 1 // 0\n\n\nx = 5\nprint(boom() if x > 0 else 1)\n", "ZeroDivisionError", ""},
	} {
		row := row
		t.Run(row.class+"/"+strings.ReplaceAll(strings.TrimSpace(row.src), "\n", " ;; "), func(t *testing.T) {
			t.Parallel()
			err := goldenRunError(t, row.src)
			if err == nil {
				t.Fatalf("the record says this program traps with %s; the compiled backend answered it", row.class)
			}
			var te *TrapError
			if !errors.As(err, &te) {
				t.Fatalf("the trap came back as %T (%v), want a *TrapError", err, err)
			}
			if te.ExnType != row.class {
				t.Errorf("raised %q (%s), want %s", te.ExnType, te.ExnMsg, row.class)
			}
			if row.sentence != "" && te.ExnMsg != row.sentence {
				t.Errorf("message %q, want %q", te.ExnMsg, row.sentence)
			}
		})
	}
}

// TestATernaryWithPureArmsIsStillASelect pins the half of the folding that stays. A pair of pure arms
// has no effect to order, and `select` is what the instruction scheduler wants there.
func TestATernaryWithPureArmsIsStillASelect(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{"int arms", "x = 3\nprint(1 if x > 2 else 2)\n"},
		{"double arms", "x = 3\nprint(1.5 if x > 2 else 2.5)\n"},
		{"verdict arms", "x = 3\nprint(True if x > 2 else False)\n"},
		{"arith arms", "x = 3\nprint(1 + 1 if x > 2 else 2 + 2)\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := ternaryArmMainBody(t, tc.src)
			if !strings.Contains(body, "select ") {
				t.Errorf("a pair of pure arms folded to a branch instead of a select:\n%s", body)
			}
			if strings.Contains(body, "tern.merge") {
				t.Errorf("a pair of pure arms built the branch shape; the select path was meant to stay:\n%s", body)
			}
		})
	}
}

// TestATernaryWithAnEffectfulArmIsABranchAndPhi is the IR shape the fix commits to: the test branches,
// each arm is evaluated in the block that reaches it, and the two arm values meet at a block-parameter
// `phi` in the merge block. The eager `select` is gone; the module still verifies, and the backend
// accepts it at the top optimisation level.
func TestATernaryWithAnEffectfulArmIsABranchAndPhi(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		// armCallee is the function whose call must live inside one arm block and nowhere else.
		armCallee string
	}{
		{
			name:      "a call in the else arm of an int ternary",
			src:       "def boom():\n    return 1\n\n\nx = 5\nprint(1 if x > 0 else boom())\n",
			armCallee: "@gy_boom",
		},
		{
			name:      "a call in the else arm of a double ternary",
			src:       "def boom():\n    return 1.5\n\n\nx = 5\nprint(2.5 if x > 0 else boom())\n",
			armCallee: "@gy_boom",
		},
		{
			name:      "a call in the then arm of a text ternary",
			src:       "def boom():\n    return \"b\"\n\n\nx = 5\nprint(boom() if x > 0 else \"n\")\n",
			armCallee: "@gy_boom",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ir := ternaryArmModule(t, tc.src)
			body := ternaryArmMainBody(t, tc.src)
			if strings.Contains(body, "select ") {
				t.Errorf("the eager select survived; the arm would run unconditionally:\n%s", body)
			}
			for _, label := range []string{"tern.then", "tern.else", "tern.merge"} {
				if !strings.Contains(body, label) {
					t.Errorf("no %q block — the arms are not being evaluated in blocks:\n%s", label, body)
				}
			}
			if !strings.Contains(body, " phi ") {
				t.Errorf("no block-parameter phi for the two arms to meet at:\n%s", body)
			}
			// The arm's call is emitted exactly once, and AFTER the branch that chooses it. The eager
			// path also emitted it once — but in the block with the test, which is the whole defect;
			// the ordering is what tells the two shapes apart.
			if n := strings.Count(body, tc.armCallee); n != 1 {
				t.Errorf("the arm's call %s appears %d times in @main, want exactly 1\n%s", tc.armCallee, n, body)
			}
			brAt, callAt := strings.Index(body, "br i1"), strings.Index(body, tc.armCallee)
			if brAt < 0 || callAt < 0 {
				t.Fatalf("the branch or the arm call is missing from @main:\n%s", body)
			}
			if callAt < brAt {
				t.Errorf("the arm's call is emitted before the test that chooses it — the arm runs eagerly:\n%s", body)
			}
			// The makefile's rule, asked of the new IR on the spot: every module this compiler emits
			// passes LLVM's own checker, and the backend takes it at the top optimisation level too.
			ternaryArmModuleVerifies(t, tc.name, ir)
		})
	}
}

// ternaryArmModuleVerifies asks the external tools the questions the build rule asks of every module:
// `opt -passes=verify` for the IR itself, and `llc -O3` for what the backend does with it. A missing
// toolchain is a skip, never a pass (ADR 0164's rule for verification).
func ternaryArmModuleVerifies(t *testing.T, name, ir string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ternary-arms.ll")
	if err := os.WriteFile(path, []byte(ir), 0o644); err != nil {
		t.Fatalf("write module: %v", err)
	}
	opt := ternaryArmTool(t, "opt-20", "opt")
	llc := ternaryArmTool(t, "llc-20", "llc")
	if opt != "" {
		if out, err := exec.Command(opt, "-passes=verify", "-disable-output", path).CombinedOutput(); err != nil {
			t.Errorf("%s rejected the ternary's module (%s):\n%s\n%s", opt, name, string(out), ir)
		}
	}
	if llc != "" {
		if out, err := exec.Command(llc, "-O3", "-filetype=null", path).CombinedOutput(); err != nil {
			t.Errorf("%s -O3 rejected the ternary's module (%s):\n%s\n%s", llc, name, string(out), ir)
		}
	}
	if opt == "" && llc == "" {
		t.Skip("neither opt nor llc is on PATH — the module was not checked")
	}
}

// ternaryArmTool finds a pinned-LLVM tool, then its bare name; "" means the machine has neither.
func ternaryArmTool(t *testing.T, pinned, plain string) string {
	t.Helper()
	for _, tool := range []string{pinned, plain} {
		if p, err := exec.LookPath(tool); err == nil {
			return p
		}
	}
	return ""
}

// TestTheTernaryBranchTerminatesTheRecursionItIsUsedFor is the shape the gap was found on. With the
// eager select the recursive call sat in the block with the test, so the base case never stopped
// anything: `fact(5)` overflowed the root stack and the harness reported a dead artifact at exit 2.
func TestTheTernaryBranchTerminatesTheRecursionItIsUsedFor(t *testing.T) {
	t.Parallel()
	ternaryArmMustPrint(t, "a countdown through the ternary",
		"def f(n):\n    return 0 if n == 0 else 1 + f(n - 1)\n\n\nprint(f(3))\nprint(f(0))\nprint(f(10))\n", "3\n0\n10\n")
	ternaryArmMustPrint(t, "a double countdown through the ternary",
		"def f(n):\n    return 0.0 if n == 0 else 1.5 + f(n - 1)\n\n\nprint(f(3))\n", "4.5\n")
	ternaryArmMustPrint(t, "a value recursion through the ternary",
		"def fact(n):\n    return 1 if n <= 1 else n * fact(n - 1)\n\n\nprint(fact(5))\n", "120\n")
	ternaryArmMustPrint(t, "a text answer from a nested ternary recursion",
		"def sign(n):\n    return \"zero\" if n == 0 else (\"pos\" if n > 0 else \"neg\")\n\n\nprint(sign(0), sign(3), sign(-2))\n",
		"zero pos neg\n")
	// A loop whose else arm prints. The arm is taken on the odd passes, so the right answer is three
	// `boom`s and the sum of the evens; the eager `select` printed `boom` on every pass, six times, and
	// summed the arm that was chosen on none of them.
	ternaryArmMustPrint(t, "a loop whose arm runs once per pass the test chose, not once per pass",
		strings.Join([]string{
			"def boom():",
			"    print(\"boom\")",
			"    return 0",
			"",
			"total = 0",
			"for i in range(6):",
			"    total = total + (i if i % 2 == 0 else boom())",
			"print(total)",
			"",
		}, "\n"), "boom\nboom\nboom\n6\n")
}

// TestTheTernaryRecordLegStandsForTheArmsItAlreadyAnswers asks the record the question for the sources
// that hold it: the conditional form already has answers on file (lambda arms, an arm that indexes, an
// arm whose value comes from a call), and the branch reproduces them byte for byte. `goldenStdoutIs`
// checks the compiled run against the record AND the record against the expectation the row states, so
// a row cannot pass by both drifting together.
func TestTheTernaryRecordLegStandsForTheArmsItAlreadyAnswers(t *testing.T) {
	t.Parallel()
	for _, row := range []struct{ src, want string }{
		// The arms the record already held: a lambda arm, an arm whose value comes from indexing.
		{"print(1 if [\"z\", None][0] else 0)", "1\n"},
		{"print(1 if [None, \"a\"][0] else 0)", "0\n"},
		{"print(-1 if True else 1)", "-1\n"},
		{"f = lambda x: 1 if x else 2\nprint(f(True), f(False))", "1 2\n"},
		{"label = lambda v: \"a\" if v > 1 else \"b\"\nprint(sorted([3, 1, 2], key=label))", "[3, 2, 1]\n"},
		// The arms this cycle put on the record — each one is a program whose answer used to include the
		// side effect of the arm its test rejected.
		{"def boom():\n    print(\"boom\")\n    return 1\n\n\nx = 5\nprint(1 if x > 0 else boom())\n", "1\n"},
		{"def boom():\n    print(\"SHOUT\")\n    return \"b\"\n\n\nx = 5\nprint(\"yes\" if x > 0 else boom())\n", "yes\n"},
		{"x = 5\nprint(1 if x > 0 else 1 // 0)\n", "1\n"},
		{"def boom():\n    xs = [1, 2]\n    return xs[9]\n\n\nx = 5\nprint(1 if x > 0 else boom())\n", "1\n"},
		{"def fact(n):\n    return 1 if n <= 1 else n * fact(n - 1)\n\n\nprint(fact(5))\n", "120\n"},
		{"def f(n):\n    return 0 if n == 0 else 1 + f(n - 1)\n\n\nprint(f(3), f(0), f(10))\n", "3 0 10\n"},
		{"def boom():\n    print(\"boom\")\n    return 0\n\n\ntotal = 0\nfor i in range(6):\n    total = total + (i if i % 2 == 0 else boom())\nprint(total)\n", "boom\nboom\nboom\n6\n"},
		{"def f(flag):\n    if flag:\n        y = 7\n    return (1 if flag else y) + 0\n\n\nprint(f(True))\n", "1\n"},
	} {
		row := row
		t.Run(strings.ReplaceAll(strings.TrimSpace(row.src), "\n", " ;; "), func(t *testing.T) {
			t.Parallel()
			goldenStdoutIs(t, row.src, row.want)
		})
	}
}

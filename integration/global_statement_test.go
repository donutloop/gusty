package integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// Roadmap row **Gap R.48** at the interface: the `global` statement, judged against a live CPython.
//
// The unit file (`pkg/lang/global_stmt_test.go`) asks the same shapes on the compiled leg and the record
// leg; this file asks the CLI — the in-process JIT for the family, and a `cc` link-and-run for the one
// program that carries the whole family — against the reference, which is asked INSIDE each row rather
// than from a table of expected bytes. A row here passes only while both legs say the same thing; a row
// whose compiled answer drifts fails even though CPython never changed, and a row where CPython starts
// disagreeing with the record fails too.

// globalStatementFamily is the promise at the interface: a body that DECLARES a name writes the MODULE's
// binding, and the module reads what the body wrote. Every `want` here was read off a live CPython 3.12.
func globalStatementFamily() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{
			"a helper bumps the module's counter",
			"count = 0\n\ndef bump():\n    global count\n    count = count + 1\n\nbump()\nbump()\nprint(count)\n", "2\n",
		},
		{
			"an accumulator a loop body fills",
			"total = 0\n\ndef acc():\n    global total\n    for i in [1, 2, 3]:\n        total = total + i\n\nacc()\nprint(total)\n", "6\n",
		},
		{
			"a while body counts the module's name down",
			"n = 0\n\ndef down():\n    global n\n    n = 3\n    while n > 0:\n        n = n - 1\n\ndown()\nprint(n)\n", "0\n",
		},
		{
			"a branch sets the module's flag, and it prints as a verdict",
			"flag = False\n\ndef raise_flag():\n    global flag\n    if True:\n        flag = True\n\nraise_flag()\nprint(flag)\n", "True\n",
		},
		{
			"one declaration names two",
			"a = 0\nb = 0\n\ndef set_both():\n    global a, b\n    a = 7\n    b = 8\n\nset_both()\nprint(a, b)\n", "7 8\n",
		},
		{
			"a method writes the module's name",
			"hits = 0\n\nclass Counter:\n    def tick(self):\n        global hits\n        hits = hits + 1\n\nc = Counter()\nc.tick()\nc.tick()\nprint(hits)\n", "2\n",
		},
		{
			"a nested body reaches the module, not the enclosing frame",
			"steps = 0\n\ndef outer():\n    def inner():\n        global steps\n        steps = steps + 4\n    inner()\n\nouter()\nprint(steps)\n", "4\n",
		},
		{
			"a body reads the module's word and leaves it alone",
			"x = 5\n\ndef use_and_keep():\n    global x\n    y = x + 1\n    return y\n\nprint(use_and_keep(), x)\n", "6 5\n",
		},
		{
			"the module reads a name only a body ever wrote",
			"def make_ready():\n    global status\n    status = 1\n\nmake_ready()\nprint(status)\n", "1\n",
		},
		{
			"the call and the module agree on one word",
			"calls = 0\n\ndef logged():\n    global calls\n    calls = calls + 1\n    return calls\n\nprint(logged(), logged(), calls)\n", "1 2 2\n",
		},
		{
			"two bodies, one name, in the order the program ran them",
			"a = 1\n\ndef f():\n    global a\n    a = a + 1\n\ndef g():\n    global a\n    a = a * 10\n\nf()\ng()\nprint(a)\n", "20\n",
		},
	}
}

func TestTheGlobalStatementAnswersWhatCPythonAnswers(t *testing.T) {
	t.Parallel()
	for _, tc := range globalStatementFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, code := cliRunCode(t, "--eval", tc.src)
			if code != 0 {
				t.Fatalf("the compiled leg exited %d on a program the reference prints %q:\n%s", code, tc.want, got)
			}
			if got != tc.want {
				t.Fatalf("the compiled leg printed %q, the family says %q\n%s", got, tc.want, tc.src)
			}
			// The reference leg, asked now rather than remembered: a row that passes only because
			// both legs drifted together is no witness at all.
			wantPy, _, refErr := lang.PythonRun(tc.src)
			if refErr != nil {
				t.Fatalf("the reference did not answer a program this row prints %q: %v\n%s", tc.want, refErr, tc.src)
			}
			if wantPy != tc.want {
				t.Fatalf("the family drifted from the reference: CPython prints %q, the row says %q\n%s", wantPy, tc.want, tc.src)
			}
		})
	}
}

// TestTheGlobalStatementLinkAndRunAnswersToo is the second execution leg: the same program built to a
// binary by `cc` and run as a process, byte-for-byte what CPython prints for the same file.
func TestTheGlobalStatementLinkAndRunAnswersToo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "global_state.gy")
	const program = `count = 0

def bump():
    global count
    count = count + 1

total = 0

def acc():
    global total
    for i in [1, 2, 3]:
        total = total + i

bump()
bump()
acc()
print(count, total)
`
	if err := os.WriteFile(src, []byte(program), 0o644); err != nil {
		t.Fatalf("write program: %v", err)
	}
	bin := cliBin(t)
	exe := filepath.Join(dir, "global_state")
	build := exec.Command(bin, "--build", exe, src)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("link-and-run build failed: %v\n%s", err, out)
	}
	run := exec.Command(exe)
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("the built program failed: %v\n%s", err, out)
	}
	if got := string(out); got != "2 6\n" {
		t.Fatalf("the built program printed %q, the reference prints \"2 6\\n\"\n%s", got, program)
	}
	wantPy, _, refErr := lang.PythonRun(program)
	if refErr != nil {
		t.Fatalf("the reference did not answer the program: %v", refErr)
	}
	if wantPy != "2 6\n" {
		t.Fatalf("CPython disagrees with the row: %q", wantPy)
	}
}

// cliRefusalRun runs the CLI over `--json --eval` and reports BOTH the merged output and the exit code,
// without the compiled-refusal filing that `cliRunCode` performs. That filing skips a case whose program the
// compiled backend refuses — which is exactly right for an answer row that drifted, and wrong for a row whose
// whole purpose is to read a filed refusal's sentence: a skipped row testifies to nothing.
func cliRefusalRun(t *testing.T, src string) (string, int) {
	t.Helper()
	cmd := exec.Command(cliBin(t), "--json", "--eval", src)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	code := 0
	if err := cmd.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("gustyc --json --eval failed to start: %v", err)
		}
		code = ee.ExitCode()
	}
	return out.String(), code
}

// TestTheGlobalCheckerRefusesAtTheCLI is the machine leg of the same refusals: the CLI exits 1 (never 2 —
// an exit 2 is our own crash, and this suite forbids it) and the JSON diagnostic names the defect, the
// reference's own sentence and the roadmap row that owns what this row does not answer.
func TestTheGlobalCheckerRefusesAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		src   string
		says  string
		wants string
	}{
		{
			"a parameter and a global are one name two ways",
			"def f(x):\n    global x\n    return x\n\nprint(f(1))\n",
			"is parameter and global", "Gap R.48",
		},
		{
			"a read above its own declaration",
			"x = 1\n\ndef f():\n    print(x)\n    global x\n    x = 2\n\nf()\n",
			"used prior to global declaration", "Gap R.48",
		},
		{
			"a binding above its own declaration",
			"def f():\n    x = 1\n    global x\n    x = 2\n\nf()\n",
			"assigned to before global declaration", "Gap R.48",
		},
		{
			"a write the program guards does not license the read",
			"def touch():\n    global gz\n    if 0:\n        gz = 1\n    return gz\n\nprint(touch())\n",
			"NameError: name 'gz' is not defined", "Gap R.48",
		},
		{
			"a class body may not declare a name into the module",
			"x = 0\n\nclass A:\n    global x\n    x = 4\n\nprint(x)\n",
			"as a MEMBER of the class", "Gap R.48",
		},
		{
			"a slot whose writes spell two kinds is the tagged word L11.1 owns",
			"reg = 0\n\ndef f():\n    global reg\n    reg = [1, 2]\n\nf()\nprint(reg)\n",
			"one word", "L11.1",
		},
		{
			// CPython prints `ready`; the module's word is an int-sized slot and a text is an
			// (index, length) pair, so the answer would be the intern table's index printed as a number.
			"a text the body writes is the tagged word L11.1 owns, not the module's one word",
			"word = \"\"\n\ndef spell():\n    global word\n    word = \"ready\"\n\nspell()\nprint(word)\n",
			"interned", "L11.1",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, code := cliRefusalRun(t, tc.src)
			if code == 2 {
				t.Fatalf("exit 2 is our own crash, not a refusal:\n%s", out)
			}
			if code != 1 {
				t.Fatalf("expected exit 1 for a refusal, got %d:\n%s", code, out)
			}
			for _, want := range []string{tc.says, tc.wants} {
				if !strings.Contains(out, want) {
					t.Fatalf("the diagnostic did not contain %q:\n%s", want, out)
				}
			}
			// The reference refuses the first five too; the sixth it answers, and that difference is
			// what the named row is for — so only ask the oracle where the two legs are supposed to meet.
			if tc.wants == "Gap R.48" && strings.Contains(tc.says, "global declaration") {
				if _, _, err := lang.PythonRun(tc.src); err == nil {
					t.Fatalf("the reference ACCEPTS a program this row refuses — re-measure the row\n%s", tc.src)
				}
			}
		})
	}
}

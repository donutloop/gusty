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

// Gap K.6 — an uncaught exception must be *reported* and must fail the process, on
// the compiled path.
//
// The AOT used to branch to its raise-exit block and `ret i32 0`, so
//
//	xs = [1, 2, 3]
//	xs[9] = 5
//
// compiled, linked, ran, printed nothing and exited 0: to any script that ran it, the
// program had succeeded. the record printed a traceback but on stdout, where it
// mixes with program output. Both now write a Python-shaped report to stderr and exit
// non-zero, and the runtime errors that reach it (index/key/type) are typed, so
// `except IndexError:` catches them in the record as well as in AOT.

type runResult struct {
	stdout string
	stderr string
	code   int
}

func (r runResult) failed() bool { return r.code == 0 }

// compiledRun runs src on the record through the real CLI, keeping the streams apart.
func compiledRun(t *testing.T, src string) runResult {
	t.Helper()
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "prog.gy")
	if err := os.WriteFile(srcPath, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	cmd := exec.Command(cliBin(t), "--file", srcPath)
	cmd.Stdout, cmd.Stderr = &out, &errb
	runErr := cmd.Run()
	code := 0
	if runErr != nil {
		ee, ok := runErr.(*exec.ExitError)
		if !ok {
			t.Fatalf("interpreter run: %v", runErr)
		}
		code = ee.ExitCode()
	}
	return runResult{out.String(), errb.String(), code}
}

// aotRun compiles src through the LLVM pipeline, runs the binary, keeps the streams apart.
func aotRun(t *testing.T, src string) runResult {
	t.Helper()
	res, err := lang.Compile(src)
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	if v, err := lang.VerifyModuleIR(res.IR, 0); err != nil {
		t.Fatalf("verify: %v", err)
	} else if !v.Skipped && !v.OK {
		t.Fatalf("module rejected for %q: %v\nIR:\n%s", src, v.Errors, res.IR)
	}
	dir := t.TempDir()
	irPath := filepath.Join(dir, "prog.ll")
	objPath := filepath.Join(dir, "prog.o")
	binPath := filepath.Join(dir, "prog")
	if err := os.WriteFile(irPath, []byte(res.IR), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(llc, "-filetype=obj", "-relocation-model=pic", irPath, "-o", objPath).CombinedOutput(); err != nil {
		t.Fatalf("llc rejected module for %q: %v\n%s\nIR:\n%s", src, err, out, res.IR)
	}
	if out, err := exec.Command("cc", objPath, "-lm", "-o", binPath).CombinedOutput(); err != nil {
		t.Fatalf("link failed for %q: %v\n%s", src, err, out)
	}
	var outb, errb bytes.Buffer
	cmd := exec.Command(binPath)
	cmd.Stdout, cmd.Stderr = &outb, &errb
	runErr := cmd.Run()
	code := 0
	if runErr != nil {
		ee, ok := runErr.(*exec.ExitError)
		if !ok {
			t.Fatalf("aot run: %v", runErr)
		}
		code = ee.ExitCode()
	}
	return runResult{outb.String(), errb.String(), code}
}

// uncaughtCases: every program raises past the last handler, and the compiled path must
// report the same exception line on stderr while keeping stdout clean.
var uncaughtCases = []struct {
	name string
	src  string
	// wantOut is what the program printed before the raise (stdout must not contain
	// the traceback — that is the whole point of stderr).
	wantOut string
	// wantErr is the exception line the compiled path must print.
	wantErr string
}{
	{
		"out-of-range item assignment",
		"xs = [1, 2, 3]\nxs[9] = 5\nprint(\"after\")\n",
		"", // nothing printed before the raise, and "after" must not appear
		"IndexError: index out of range",
	},
	{
		"raise with a message",
		"raise ValueError(\"boom\")\n",
		"",
		"ValueError: boom",
	},
	{
		"raise the class itself",
		"raise IndexError\n",
		"",
		"IndexError",
	},
	{
		"raise after output",
		"print(\"before\")\nraise TypeError(\"nope\")\n",
		"before\n",
		"TypeError: nope",
	},
	{
		"raise from inside a function",
		"def boom():\n    raise RuntimeError(\"from fn\")\n\nboom()\nprint(\"never\")\n",
		"",
		"RuntimeError: from fn",
	},
}

func TestUncaughtExceptionReportsAndFailsOnTheCompiledBackend(t *testing.T) {
	for _, tc := range uncaughtCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, run := range []struct {
				name string
				fn   func(*testing.T, string) runResult
			}{{"cli", compiledRun}, {"artifact", aotRun}} {
				r := run.fn(t, tc.src)
				if r.code == 0 {
					t.Errorf("%s: exit = 0, want non-zero (an uncaught exception must fail the process)\nstdout=%q stderr=%q", run.name, r.stdout, r.stderr)
				}
				if !strings.Contains(r.stderr, tc.wantErr) {
					t.Errorf("%s: stderr must name the exception %q, got %q", run.name, tc.wantErr, r.stderr)
				}
				if !strings.Contains(r.stderr, "Traceback") {
					t.Errorf("%s: stderr should carry a traceback header, got %q", run.name, r.stderr)
				}
				if r.stdout != tc.wantOut {
					t.Errorf("%s: stdout = %q, want %q (diagnostics must not mix with program output)", run.name, r.stdout, tc.wantOut)
				}
				if strings.Contains(r.stdout, "Traceback") || strings.Contains(r.stdout, tc.wantErr) {
					t.Errorf("%s: the report leaked to stdout: %q", run.name, r.stdout)
				}
			}
		})
	}
}

// caughtCases: a runtime error raised by the machine (not by a `raise` statement) must
// be catchable, with the class name the `except` clause matches on. the record
// used to abort on these instead of unwinding.
var caughtCases = []struct {
	name string
	src  string
	want string
}{
	{"item assignment out of range", "xs = [1]\ntry:\n    xs[5] = 2\nexcept IndexError:\n    print(\"caught\")\n", "caught\n"},
	{"list read out of range", "xs = [1]\ntry:\n    print(xs[5])\nexcept IndexError:\n    print(\"caught\")\n", "caught\n"},
	{"missing dict key", "d = {1: 2}\ntry:\n    print(d[9])\nexcept KeyError:\n    print(\"caught\")\n", "caught\n"},
	{"raise then catch by class", "try:\n    raise ValueError(\"x\")\nexcept ValueError:\n    print(\"caught\")\n", "caught\n"},
	{"bare except catches anything", "try:\n    raise KeyError(\"x\")\nexcept:\n    print(\"caught\")\n", "caught\n"},
}

func TestRuntimeErrorsAreCatchableOnTheCompiledBackend(t *testing.T) {
	for _, tc := range caughtCases {
		t.Run(tc.name, func(t *testing.T) {
			gotI := runCompiled(t, tc.src)
			if gotI != tc.want {
				t.Errorf("the record leg = %q, want %q (Python's answer)", gotI, tc.want)
			}
			gotA := runAOT(t, tc.src)
			if gotA != tc.want {
				t.Errorf("AOT = %q, want %q (Python's answer)", gotA, tc.want)
			}
		})
	}
}

// TestUncaughtReportIsNotEmittedForCleanPrograms keeps the raise runtime honest: a
// program that cannot raise carries no raise state at all, so the golden IR for the
// trivial program is untouched.
func TestUncaughtReportIsNotEmittedForCleanPrograms(t *testing.T) {
	res, err := lang.Compile("print(42)\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if strings.Contains(res.IR, "rt_die") || strings.Contains(res.IR, "@exn_msg") {
		t.Errorf("a program with no raise should carry no raise runtime:\n%s", res.IR)
	}
	res2, err := lang.Compile("raise ValueError(\"boom\")\n")
	if err != nil {
		t.Fatalf("compile raise: %v", err)
	}
	for _, want := range []string{"rt_die", "@exn_msg", "\"ValueError: boom"} {
		if !strings.Contains(res2.IR, want) {
			t.Errorf("raise IR missing %q:\n%s", want, res2.IR)
		}
	}
	// The uncaught path must exit with the *trap* code, not fall off the end. It used to be 1,
	// which is the compile-error code: ADR 0211 says a failure class has one code whichever path
	// produces it, and a script running the linked binary was being told "the compiler failed" about
	// a program that simply raised (ADR 0228).
	if !strings.Contains(res2.IR, "ret i32 3") {
		t.Errorf("the uncaught path should return the trap code 3 from main:\n%s", res2.IR)
	}
}

// staticallyRejectedCases are assignments the AOT refuses at compile time because the
// target's kind is known from the source: writing to a string index or a set element can
// never succeed, so ADR 0166 makes it a diagnostic rather than code that traps at runtime.
// the record still raises the TypeError a handler can catch — the divergence is
// deliberate and documented in docs/language.md.
var staticallyRejectedCases = []struct {
	name string
	src  string
	want string // the AOT diagnostic
}{
	{
		"string index assignment",
		"s = \"abc\"\ntry:\n    s[0] = \"z\"\nexcept TypeError:\n    print(\"caught\")\n",
		"strings are immutable",
	},
	{
		"set item assignment",
		"s = {1, 2}\ntry:\n    s[0] = 1\nexcept TypeError:\n    print(\"caught\")\n",
		"sets do not support item assignment",
	},
}

func TestStaticallyImpossibleAssignmentsAreDiagnostics(t *testing.T) {
	for _, tc := range staticallyRejectedCases {
		t.Run(tc.name, func(t *testing.T) {
			lang.RecordedStdoutIs(t, tc.src, "caught\n")
			_, err := lang.Compile(tc.src)
			if err == nil {
				t.Fatalf("AOT should refuse to compile: %s", tc.src)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("AOT diagnostic = %q, want it to say %q", err.Error(), tc.want)
			}
		})
	}
}

// TestTracebackFramesNameTheRaiseSite (roadmap Gap K.8): the compiled report carries the
// frame the record's last frame carries — file, line and function — so "where did it
// fail" has the same answer whichever backend ran the program. the record prints one
// frame per stack level; the AOT prints the raise site's own frame until the DWARF line
// tables of L8.5 land.
func TestTracebackFramesNameTheRaiseSite(t *testing.T) {
	cases := []struct {
		name string
		src  string
		// the frame line the compiled path must print for the raise site itself
		wantFrame string
	}{
		{
			"raise inside a function",
			"def boom():\n    raise ValueError(\"boom\")\n\nboom()\n",
			`  File "prog", line 2, in boom`,
		},
		{
			"raise at module level",
			"print(\"first\")\nraise ValueError(\"boom\")\n",
			`  File "prog", line 2, in <module>`,
		},
		{
			"machine-detected error in a function",
			"def boom():\n    xs = [1]\n    xs[5] = 2\n\nboom()\n",
			`  File "prog", line 3, in boom`,
		},
		{
			"out-of-range pop names its line",
			"xs = [1, 2, 3]\nys = xs.pop()\nprint(xs.pop(9))\n",
			`  File "prog", line 3, in <module>`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ri := compiledRun(t, tc.src)
			if !strings.Contains(ri.stderr, tc.wantFrame) {
				t.Errorf("interpreter stderr must contain the frame %q, got %q", tc.wantFrame, ri.stderr)
			}
			ra := aotRun(t, tc.src)
			if !strings.Contains(ra.stderr, tc.wantFrame) {
				t.Errorf("AOT stderr must contain the frame %q, got %q", tc.wantFrame, ra.stderr)
			}
			// The exception line is the last line on both, and identical.
			lastI := lastLine(ri.stderr)
			lastA := lastLine(ra.stderr)
			if lastI != lastA {
				t.Errorf("final report line differs: interpreter %q vs AOT %q", lastI, lastA)
			}
		})
	}
}

func lastLine(s string) string {
	t := strings.TrimRight(s, "\n")
	if i := strings.LastIndex(t, "\n"); i >= 0 {
		return t[i+1:]
	}
	return t
}

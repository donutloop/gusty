//go:build !windows

// Package lang — the gusty language toolchain.
//
// jit_llvm.go implements an in-process JIT for fast REPL feedback: source is
// compiled ahead-of-time through the existing LLVM-IR pipeline (codegen ->
// llc -> object -> link), the resulting native shared object is dlopen'd into
// this process, and its generated `main` is invoked directly — no process
// spawn per expression, so interactive feedback stays snappy while still
// running real machine code underneath.
package lang

/*
#include <stdlib.h>
#include <stdio.h>
#include <unistd.h>
#include <dlfcn.h>

static void* jit_dlopen(const char* path) {
	return dlopen(path, RTLD_NOW | RTLD_LOCAL);
}
static void* jit_dlsym(void* h, const char* sym) {
	return dlsym(h, sym);
}
static void jit_dlclose(void* h) {
	dlclose(h);
}
static const char* jit_dlerror(void) {
	return dlerror();
}
static void jit_unbuffered(void) {
	setvbuf(stdout, NULL, _IONBF, 0);
}
static int jit_call(void* fn) {
	return ((int (*)(void)) fn)();
}
static int jit_dup(int oldfd) {
	return dup(oldfd);
}
static int jit_dup2(int oldfd, int newfd) {
	return dup2(oldfd, newfd);
}
static void jit_close(int fd) {
	close(fd);
}
*/
import "C"

import (
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unsafe"
)

// JITResult is the structured outcome of a JIT compile-and-run: the stdout
// the generated `main` produced, the stderr it produced (tracebacks and the
// collector's self-report live there, not in the program's output), the emitted
// LLVM IR, and the exact llc/cc toolchain commands executed. It is
// JSON-serializable so agents and tooling can consume a JIT session without
// scraping process output.
type JITResult struct {
	Output string `json:"output"`
	Stderr string `json:"stderr"`
	// Code is what the generated `main` returned: 0 when the program ran to
	// completion, non-zero when it trapped (an uncaught exception, a failed
	// built-in). Before it existed the in-process JIT threw the status away, so
	// `--aot` reported success for a program whose own binary exits 1 — the answer
	// an agent asks first ("did it work?") was the one answer we withheld
	// (roadmap Gap R.17, ADR 0211).
	Code int    `json:"code"`
	IR   string `json:"ir"`
	// Result is what a snippet's final bare expression evaluated to, as the module reported it:
	// the kind and the repr. It is nil unless the caller asked for the echo (JITOptions
	// .EchoResult — the REPL and `--eval` do) and the pair named a form for the expression.
	//
	// It is not program output. The answer arrives on the tool channel (fd 2) and is lifted out
	// of Stderr here, so `output` stays byte-for-byte what the program printed and the answer is
	// still separable from the traceback and the collector line that share the channel
	// (ADR 0179's channel rule, ADR 0302's REPL echo).
	Result *EchoResult `json:"result,omitempty"`
	// Debug is what the module handed to `llc` really carries, read back from it; nil when
	// the build asked for no debug info (L8.5).
	Debug       *DebugInfo   `json:"debug,omitempty"`
	Commands    []string     `json:"commands"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// JIT compiles src to a native shared object (codegen -> llc -> cc -shared),
// dlopen's it into this process, and calls its generated `main`. It returns
// the machine-executed stdout alongside the IR and toolchain commands. This is the compiled
// in-process path powering `gustyc --eval`, `--file` and the REPL — the only execution path the
// language has, since ADR 0302 retired the AST interpreter.
// JIT is JITWithOptions with no debug request: the module is the one every other path
// builds, with no metadata in it.
func JIT(src string, optLevel int) (*JITResult, error) {
	return JITWithOptions(src, optLevel, nil)
}

// RunSource compiles and runs one source program in-process and returns what it printed to
// stdout, together with the failure it ran into (a compile refusal, a trap). It is the single
// "run this program" entry point: the CLI's run paths, the harnesses and the test suite all go
// through it, so a program cannot behave differently depending on who asked.
//
// Like the InterpreterRun it replaces, it captures the process-wide stdout for the duration of
// the call, so it is not safe for concurrent use.
func RunSource(src string) (string, error) {
	res, err := JIT(src, 0)
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		return res.Output, fmt.Errorf("program exited with status %d: %s", res.Code, firstLine(res.Stderr))
	}
	return res.Output, nil
}

// RunSnippet compiles and runs a REPL/`--eval` snippet with the echo asked for, and returns the
// whole structured result: the program's stdout, the tool-channel diagnostics with the echo
// lifted out of them, and Result — the value of the snippet's final bare expression (ADR 0302).
func RunSnippet(src string) (*JITResult, error) {
	return JITWithOptions(src, 0, &JITOptions{EchoResult: true})
}

// JITOptions is what a caller asks of the in-process JIT beyond the source. Debug is nil
// for the ordinary build; a --debug run asks for the same module `--build --debug` would
// link, records included, so `GUSTY_KEEP_LLVM=1 gustyc --jit --debug` leaves a .ll and a
// .so on disk that a debugger can read (L8.5, ADR 0231).
type JITOptions struct {
	Debug *DebugOptions
	// EchoResult asks the module to report the value of the program's final bare expression on
	// the tool channel, which is what turns the in-process runner into a REPL: an interactive
	// caller that typed `1 + 1` wants `2` back, not silence. Only a snippet caller sets it; a
	// program is never echoed (ADR 0204). Roadmap L13.1, ADR 0302.
	EchoResult bool
}

// JITWithOptions compiles the program, lowers it to an object and a shared library, loads
// it in-process and runs it. The Debug member of the result is read back from the module
// that was actually handed to `llc`, not from the request.
func JITWithOptions(src string, optLevel int, opts *JITOptions) (*JITResult, error) {
	res := &JITResult{}
	prog, err := parseProgram(src)
	if err != nil {
		return nil, err
	}
	diags := Analyze(prog)
	if anyErr(diags) {
		res.Diagnostics = diags
		// A count is not an error message. The checker knew `line 1:6: unexpected character ";"`
		// while this path said only "jit: 1 error(s) in source", so a program the interpreter runs
		// (`x = 5; print(x+1)`) disagreed with the compiled path and the reader could not see why
		// (ADR 0240; the rule is ADR 0166's and ADR 0233's: name what is missing).
		errMsg := fmt.Sprintf("jit: %d error(s) in source", nErrs(diags))
		for _, d := range diags {
			if d.Level != LevelError {
				continue
			}
			// The same rendering the Diagnostic carries, so the two surfaces say one thing.
			errMsg += "\n  - " + d.Error()
		}
		return res, fmt.Errorf("%s", errMsg)
	}
	var ir string
	var derr error
	if opts != nil && opts.Debug != nil {
		var dbg *DebugInfo
		ir, dbg, derr = GenerateIRReport(prog, &IRGenOptions{Debug: opts.Debug.normalized(), EchoResult: opts.EchoResult})
		res.Debug = dbg
	} else if opts != nil && opts.EchoResult {
		ir, _, derr = GenerateIRReport(prog, &IRGenOptions{EchoResult: true})
	} else {
		ir, derr = GenerateIR(prog)
	}
	if derr != nil {
		// One stage prefix per line. The codegen errors carry their own ("codegen: …"), and wrapping
		// them produced `jit: codegen: codegen: …` — the same message the build path is tested not to
		// repeat, arriving twice on this one, which is exactly the diagnostic noise that makes an agent
		// grep for a substring that no longer exists (ADR 0166's readability rule).
		if strings.HasPrefix(derr.Error(), "codegen: ") {
			return nil, fmt.Errorf("jit: %w", derr)
		}
		return nil, fmt.Errorf("jit: codegen: %w", derr)
	}
	ir = OptimizeIR(ir, optLevel)
	res.IR = ir

	dir, err := os.MkdirTemp("", "gusty-jit-")
	if err != nil {
		return nil, fmt.Errorf("jit: temp dir: %w", err)
	}
	// GUSTY_KEEP_LLVM=1 leaves the scratch directory behind and names it in the error.
	// When a failure is "llc rejected the module", the only useful artifact is the .ll
	// text, and a toolchain whose IR failures cannot be inspected is a toolchain you
	// cannot debug (roadmap L11.8's contract: a refusal must be diagnosable).
	if os.Getenv("GUSTY_KEEP_LLVM") != "" {
		fmt.Fprintf(os.Stderr, "gustyc: keeping JIT scratch dir %s\n", dir)
	} else {
		defer os.RemoveAll(dir)
	}

	irPath := filepath.Join(dir, "jit.ll")
	objPath := filepath.Join(dir, "jit.o")
	soPath := filepath.Join(dir, "jit.so")
	if err := os.WriteFile(irPath, []byte(ir), 0o600); err != nil {
		return nil, fmt.Errorf("jit: write IR: %w", err)
	}

	llc := exec.Command(llcCmd, "-relocation-model=pic", "-filetype=obj", irPath, "-o", objPath)
	if out, err := llc.CombinedOutput(); err != nil {
		return nil, toolchainFailure("llc", llcCmd, err, out)
	}
	res.Commands = append(res.Commands, llcCmd+" -relocation-model=pic -filetype=obj "+irPath+" -o "+objPath)

	cc := exec.Command(ccCmd, "-shared", "-fPIC", objPath, "-o", soPath, "-lm")
	if out, err := cc.CombinedOutput(); err != nil {
		return nil, toolchainFailure("cc", ccCmd, err, out)
	}
	res.Commands = append(res.Commands, ccCmd+" -shared -fPIC "+objPath+" -o "+soPath+" -lm")

	out, errOut, code, err := dlopenRun(soPath)
	if err != nil {
		return nil, err
	}
	res.Output = out
	res.Stderr = errOut
	res.Code = code
	// Lift the snippet's answer out of the tool channel before anyone reads Stderr: the answer is
	// a fact about the run, and the transcript of the run must not show it twice (ADR 0302).
	if opts != nil && opts.EchoResult {
		if echo, ok := ParseEchoLine(errOut); ok {
			e := echo
			res.Result = &e
			res.Stderr = StripEchoLine(errOut)
		}
	}
	return res, nil
}

// captureFD1 runs fn with file descriptor 1 (stdout) redirected to a pipe. It is
// the common case of captureFD.
func captureFD1(fn func()) (string, error) { return captureFD(1, fn) }

// dlopenRun loads the shared object, dlsym's its `main`, and runs it with fd 1
// and fd 2 redirected to pipes, so the generated printf output and its fd-2
// diagnostics (uncaught-exception reports, the collector self-report) are both
// recoverable in Go instead of disappearing into the terminal. It also returns
// the status `main` returned, which is the only place the compiled backend
// records that a program trapped.
func dlopenRun(soPath string) (string, string, int, error) {
	cpath := C.CString(soPath)
	defer C.free(unsafe.Pointer(cpath))
	h := C.jit_dlopen(cpath)
	if h == nil {
		return "", "", 0, fmt.Errorf("jit: dlopen: %s", C.GoString(C.jit_dlerror()))
	}
	defer C.jit_dlclose(h)

	cmain := C.CString("main")
	defer C.free(unsafe.Pointer(cmain))
	fn := C.jit_dlsym(h, cmain)
	if fn == nil {
		return "", "", 0, fmt.Errorf("jit: dlsym(main): %s", C.GoString(C.jit_dlerror()))
	}

	C.jit_unbuffered()
	// Both descriptors are redirected at once: fd 1 holds the program's own output,
	// fd 2 its diagnostics (tracebacks, the collector self-report).
	var (
		out    string
		errOut string
		outErr error
		errErr error
		code   int
	)
	errOut, errErr = captureFD(2, func() {
		out, outErr = captureFD1(func() { code = int(C.jit_call(fn)) })
	})
	if errErr != nil {
		return "", "", 0, errErr
	}
	if outErr != nil {
		return "", "", 0, outErr
	}
	return out, errOut, code, nil
}

// captureFD runs fn with file descriptor fd redirected to a pipe, then restores
// it and returns everything the C code wrote. The generated main uses printf
// against the C runtime's stdout (fd 1) and write(2, ...) for diagnostics, so
// dup'ing the descriptor captures both C and Go writes during the call.
func captureFD(fd int, fn func()) (string, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return "", fmt.Errorf("jit: pipe: %w", err)
	}

	old := int(C.jit_dup(C.int(fd)))
	if old < 0 {
		r.Close()
		w.Close()
		return "", fmt.Errorf("jit: dup(fd %d) failed", fd)
	}
	restore := func() {
		C.jit_dup2(C.int(old), C.int(fd))
		C.jit_close(C.int(old))
	}
	defer restore()

	if int(C.jit_dup2(C.int(w.Fd()), C.int(fd))) < 0 {
		r.Close()
		w.Close()
		return "", fmt.Errorf("jit: dup2(fd %d) failed", fd)
	}
	// fd now owns the pipe write end; closing the Go wrapper keeps it alive.
	w.Close()

	fn()

	restore()
	b, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		return "", fmt.Errorf("jit: read output: %w", err)
	}
	return string(b), nil
}

// benchSO loads the shared object once, resolves its generated `main`, and
// calls it runs times, returning wall-clock timing. Keeping the handle open
// across all runs makes the measurement warm execution only — no per-run
// dlopen/dlsym or build cost is included.
func benchSO(soPath string, runs int) (BenchReport, error) {
	cpath := C.CString(soPath)
	defer C.free(unsafe.Pointer(cpath))
	h := C.jit_dlopen(cpath)
	if h == nil {
		return BenchReport{}, fmt.Errorf("bench: dlopen %s", soPath)
	}
	defer C.jit_dlclose(h)

	cmain := C.CString("main")
	defer C.free(unsafe.Pointer(cmain))
	fn := C.jit_dlsym(h, cmain)
	if fn == nil {
		return BenchReport{}, fmt.Errorf("bench: dlsym(main)")
	}

	var total float64
	best := math.Inf(1)
	for i := 0; i < runs; i++ {
		t0 := time.Now()
		C.jit_call(fn) // exit code discarded; timing only
		ms := float64(time.Since(t0).Nanoseconds()) / 1e6
		total += ms
		if ms < best {
			best = ms
		}
	}
	if math.IsInf(best, 1) {
		best = 0
	}
	return BenchReport{TotalMs: total, MeanMs: total / float64(runs), BestMs: best}, nil
}

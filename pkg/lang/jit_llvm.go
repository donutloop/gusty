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
	"time"
	"os"
	"os/exec"
	"path/filepath"
	"unsafe"
)

// JITResult is the structured outcome of a JIT compile-and-run: the stdout
// the generated `main` produced, the emitted LLVM IR, and the exact
// llc/cc toolchain commands executed. It is JSON-serializable so agents and
// tooling can consume a JIT session without scraping process output.
type JITResult struct {
	Output      string       `json:"output"`
	IR          string       `json:"ir"`
	Commands    []string     `json:"commands"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// JIT compiles src to a native shared object (codegen -> llc -> cc -shared),
// dlopen's it into this process, and calls its generated `main`. It returns
// the machine-executed stdout alongside the IR and toolchain commands. This is
// the AOT-underneath-in-process path powering `gustyc --jit` REPL/eval.
func JIT(src string, optLevel int) (*JITResult, error) {
	res := &JITResult{}
	prog, err := parseProgram(src)
	if err != nil {
		return nil, err
	}
	diags := Analyze(prog)
	if anyErr(diags) {
		res.Diagnostics = diags
		return res, fmt.Errorf("jit: %d error(s) in source", nErrs(diags))
	}
	ir, err := GenerateIR(prog)
	if err != nil {
		return nil, fmt.Errorf("jit: codegen: %w", err)
	}
	ir = OptimizeIR(ir, optLevel)
	res.IR = ir

	dir, err := os.MkdirTemp("", "gusty-jit-")
	if err != nil {
		return nil, fmt.Errorf("jit: temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	irPath := filepath.Join(dir, "jit.ll")
	objPath := filepath.Join(dir, "jit.o")
	soPath := filepath.Join(dir, "jit.so")
	if err := os.WriteFile(irPath, []byte(ir), 0o600); err != nil {
		return nil, fmt.Errorf("jit: write IR: %w", err)
	}

	llc := exec.Command(llcCmd, "-relocation-model=pic", "-filetype=obj", irPath, "-o", objPath)
	if out, err := llc.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("jit: llc: %v\n%s", err, out)
	}
	res.Commands = append(res.Commands, llcCmd+" -relocation-model=pic -filetype=obj "+irPath+" -o "+objPath)

	cc := exec.Command(ccCmd, "-shared", "-fPIC", objPath, "-o", soPath, "-lm")
	if out, err := cc.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("jit: cc: %v\n%s", err, out)
	}
	res.Commands = append(res.Commands, ccCmd+" -shared -fPIC "+objPath+" -o "+soPath+" -lm")

	out, err := dlopenRun(soPath)
	if err != nil {
		return nil, err
	}
	res.Output = out
	return res, nil
}

// dlopenRun loads the shared object, dlsym's its `main`, and runs it with fd 1
// redirected to a pipe so the generated printf output can be captured in Go.
func dlopenRun(soPath string) (string, error) {
	cpath := C.CString(soPath)
	defer C.free(unsafe.Pointer(cpath))
	h := C.jit_dlopen(cpath)
	if h == nil {
		return "", fmt.Errorf("jit: dlopen: %s", C.GoString(C.jit_dlerror()))
	}
	defer C.jit_dlclose(h)

	cmain := C.CString("main")
	defer C.free(unsafe.Pointer(cmain))
	fn := C.jit_dlsym(h, cmain)
	if fn == nil {
		return "", fmt.Errorf("jit: dlsym(main): %s", C.GoString(C.jit_dlerror()))
	}

	C.jit_unbuffered()
	out, err := captureFD1(func() { C.jit_call(fn) })
	if err != nil {
		return "", err
	}
	return out, nil
}

// captureFD1 runs fn with file descriptor 1 (stdout) redirected to a pipe, then
// restores it and returns everything the C code wrote. The generated main uses
// printf against the C runtime's stdout (fd 1), so dup'ing fd 1 captures both
// C and Go writes to stdout during the call.
func captureFD1(fn func()) (string, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return "", fmt.Errorf("jit: pipe: %w", err)
	}

	old := int(C.jit_dup(1))
	if old < 0 {
		r.Close()
		w.Close()
		return "", fmt.Errorf("jit: dup(fd 1) failed")
	}
	restore := func() {
		C.jit_dup2(C.int(old), 1)
		C.jit_close(C.int(old))
	}
	defer restore()

	if int(C.jit_dup2(C.int(w.Fd()), 1)) < 0 {
		r.Close()
		w.Close()
		return "", fmt.Errorf("jit: dup2(fd 1) failed")
	}
	// fd 1 now owns the pipe write end; closing the Go wrapper keeps it alive.
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

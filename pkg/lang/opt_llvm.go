package lang

// This file drives the *real* LLVM optimization pipeline via the external
// `opt` tool (opt-20). The pure-Go optimizer in opt.go operates textually and
// can only fold patterns it explicitly recognizes; the real `opt` tool runs
// the actual LLVM scalar/interprocedural passes (instcombine, gvn, licm,
// sroa, simplifycfg, ...) over the whole module, so constant folding like
// `add i32 2, 3` -> `5` that the textual pass cannot see gets eliminated.
//
// GC-correctness is preserved: the codegen roots heap slots through
// module-global arrays (@gc.roots / @gc_roots_used) rather than allocas, and
// the runtime `rt_*` functions read/write those globals, so the real optimizer
// keeps every live root registered. The pass pipeline never promotes the
// rooted allocas (they are address-taken), and the fallback below guarantees
// the build never fails if the `opt` tool is missing.

import (
	"os"
	"os/exec"
	"path/filepath"
)

// runLLVMopt invokes the external LLVM `opt` tool on the module IR at the
// requested optimization level and returns the optimized textual IR. It is
// purely defensive: on any error (tool missing, IR rejected, I/O failure) it
// returns ir unchanged so the caller's pipeline always produces valid output.
func runLLVMopt(ir string, level int) string {
	if level <= 0 || ir == "" || optCmd == "" {
		return ir
	}
	dir, err := os.MkdirTemp("", "gusty-opt-")
	if err != nil {
		return ir
	}
	defer os.RemoveAll(dir)

	inPath := filepath.Join(dir, "prog.ll")
	outPath := filepath.Join(dir, "prog.opt.ll")
	if err := os.WriteFile(inPath, []byte(ir), 0o600); err != nil {
		return ir
	}

	// Level -> real LLVM pipeline. -O1 is the conservative default for
	// interactive/REPL paths; -O2/-O3 get the full scalar + loop passes for
	// AOT builds and benchmarks.
	pipeline := "-O1"
	switch {
	case level >= 3:
		pipeline = "-O3"
	case level == 2:
		pipeline = "-O2"
	}

	out, err := exec.Command(optCmd, pipeline, "-S", inPath, "-o", outPath).CombinedOutput()
	if err != nil {
		// Defensive: never fail the build because opt is unavailable.
		return ir
	}
	_ = out

	data, err := os.ReadFile(outPath)
	if err != nil || len(data) == 0 {
		return ir
	}
	return string(data)
}

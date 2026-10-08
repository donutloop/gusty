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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// runLLVMopt invokes the external LLVM `opt` tool on the module IR at the
// requested optimization level and returns the optimized textual IR. It is
// purely defensive: on any error (tool missing, IR rejected, I/O failure) it
// returns ir unchanged so the caller's pipeline always produces valid output.
// Optimization reports what the optimization stage actually did. The build used to say
// "built" with no hint that the real LLVM pipeline had been skipped because `opt-20` was
// missing, so a script could not tell an optimized binary from an unoptimized one — the
// same class of silent substitution that verification had before L8.2 (roadmap Gap J.4).
type Optimization struct {
	Tool     string `json:"tool"`
	Pipeline string `json:"pipeline"`
	Level    int    `json:"level"`
	Applied  bool   `json:"applied"`
	// Fallback names what ran instead when the real pipeline did not ("textual").
	Fallback string `json:"fallback,omitempty"`
	Note     string `json:"note,omitempty"`
	Err      string `json:"error,omitempty"`
}

func runLLVMopt(ir string, level int) (string, *Optimization) {
	if level <= 0 || ir == "" {
		return ir, nil // nothing asked for: not a skipped step, an absent one
	}
	if optCmd == "" {
		return ir, &Optimization{
			Tool: "opt", Level: level, Pipeline: "none", Applied: false,
			Fallback: "textual",
			Note:     "no LLVM opt toolchain found; the textual pass ran instead, so this module is NOT LLVM-optimized",
		}
	}
	rep := &Optimization{Tool: optCmd, Level: level, Pipeline: "none", Applied: false}
	dir, err := os.MkdirTemp("", "gusty-opt-")
	if err != nil {
		rep.Err = err.Error()
		rep.Fallback = "textual"
		return ir, rep
	}
	defer os.RemoveAll(dir)

	inPath := filepath.Join(dir, "prog.ll")
	outPath := filepath.Join(dir, "prog.opt.ll")
	if err := os.WriteFile(inPath, []byte(ir), 0o600); err != nil {
		rep.Err = err.Error()
		rep.Fallback = "textual"
		return ir, rep
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
	rep.Pipeline = pipeline

	out, err := runToolStage(ToolBudget, "opt", optCmd, pipeline, "-S", inPath, "-o", outPath)
	if err != nil {
		// Never fail the build because opt is unavailable — but say so. When the tool
		// exists and still rejects our module, that is a compiler bug worth reading about.
		rep.Err = fmt.Sprintf("%v: %s", err, strings.TrimSpace(string(out)))
		rep.Fallback = "textual"
		var timeout *ToolTimeoutError
		switch {
		case errors.As(err, &timeout):
			// A tool that ran out of patience is neither a rejection nor an absent tool, and the two
			// notes below would each send the reader somewhere wrong: one opens a compiler bug against a
			// loaded machine, the other says the tool is missing when it is sitting right there.
			rep.Note = fmt.Sprintf("the LLVM optimizer was killed after %s without answering; the textual pass ran instead, so this module is NOT LLVM-optimized", timeout.Budget)
		case !strings.Contains(rep.Err, "executable file not found") && !strings.Contains(rep.Err, "no such file"):
			rep.Note = "the LLVM optimizer rejected the emitted module; the textual pass ran instead"
		default:
			rep.Note = "the LLVM optimizer is not installed; the textual pass ran instead, so this module is NOT LLVM-optimized"
		}
		return ir, rep
	}

	data, err := os.ReadFile(outPath)
	if err != nil || len(data) == 0 {
		rep.Err = "no optimizer output"
		rep.Fallback = "textual"
		rep.Note = "the LLVM optimizer produced no output; the textual pass ran instead"
		return ir, rep
	}
	rep.Applied = true
	return string(data), rep
}

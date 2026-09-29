package lang

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// ToolchainRejectionError says the *external* LLVM toolchain refused to process the module
// the compiler produced. That is a statement about the toolchain, not about the source: the
// program parsed, analysed and lowered, and then `llc` said no. Under the exit-code contract
// (ADR 0164, ADR 0166) that event has its own class — a compiler bug must never be reported
// to a user as an error in their program.
//
// Until ADR 0211 the class was only reachable through `--build`: the in-process run path
// (`--aot`/`--jit`) wrapped the same failure in a plain error and the CLI reported it as
// compile-error class 1, so one compiler bug was described two different ways depending on
// which flag you used. Callers classify with errors.As, not by matching the message.
type ToolchainRejectionError struct {
	Tool   string // the tool that said no, e.g. "llc-20"
	Stage  string // the pipeline stage, e.g. "llc" or "cc"
	Err    error  // the process failure (exit status)
	Output string // the tool's own words, kept verbatim for diagnostics
}

func (e *ToolchainRejectionError) Error() string {
	return fmt.Sprintf("jit: %s: %v\n%s", e.Stage, e.Err, e.Output)
}

// Unwrap keeps the underlying process error reachable, so `errors.Is(err, context.DeadlineExceeded)`
// and friends keep working through the classification.
func (e *ToolchainRejectionError) Unwrap() error { return e.Err }

// toolchainFailure classifies a toolchain subprocess failure. A tool that could not be
// *started* is not a rejection: reporting an uninstalled `llc-20` as "LLVM rejected the module
// we emitted" would accuse the compiler of a bug it did not commit and send the reader looking
// for one that isn't there (the same honesty rule as a refusal message's workaround, ADR 0210).
func toolchainFailure(stage, tool string, err error, output []byte) error {
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("jit: %s (%s) could not be run: %w", stage, tool, err)
	}
	return &ToolchainRejectionError{Tool: tool, Stage: stage, Err: err, Output: string(output)}
}

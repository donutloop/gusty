// Package lang exposes the shared-lowering conformance matrix: a stable,
// machine-readable JSON schema that records whether every whole-program
// integration case produces identical stdout on BOTH backends — the AST
// interpreter (EvalExpr) and the LLVM AOT compiler (Compile).
//
// The shared lowering contract (see docs/shared-lowering-spec.md) says that
// evaluating a source with the interpreter and compiling+running it through
// AOT must produce byte-identical stdout. The conformance matrix asserts that
// contract over every integration case and records the observed outputs so a
// semantics drift between the two backends is caught as a matrix row failing
// parity instead of being silently accepted.
package lang

import (
	"fmt"
	"os"
)

// ConformanceSchemaVersion is the machine-readable JSON schema version for the
// conformance matrix emitted by the integration conformance test.
const ConformanceSchemaVersion = "1.0"

// ConformanceCase is one whole-program conformance case. Every case is a single
// merged source that both backends lower independently. Shared marks whether
// the construct is expected to be shared surface: run on BOTH backends and
// produce identical stdout. Non-shared (backend-specific) cases are recorded
// in the matrix but not asserted for parity.
type ConformanceCase struct {
	ID     string `json:"id"`     // stable identifier, e.g. "programs/single"
	Name   string `json:"name"`   // human label, e.g. "single.gy"
	Source string `json:"source"` // merged single-source program
	Shared bool   `json:"shared"` // expected to run on both backends with equal stdout
}

// ConformanceResult records the observed behaviour of one case on both backends:
// the interpreter stdout (InterpOut) and the AOT binary stdout (AOTOut). Parity
// is true when the two stdout streams are byte-identical.
type ConformanceResult struct {
	Case      ConformanceCase `json:"case"`
	InterpOK  bool            `json:"interp_ok"`
	InterpOut string          `json:"interp_stdout"`
	InterpErr string          `json:"interp_error,omitempty"`
	AOTOK     bool            `json:"aot_ok"`
	AOTOut    string          `json:"aot_stdout"`
	AOTErr    string          `json:"aot_error,omitempty"`
	Parity    bool            `json:"parity"` // interpreter stdout == AOT stdout
}

// ConformanceMatrix is the machine-readable conformance matrix artifact. It is
// deterministic: for the same case registry and compiler toolchain the pass/fail
// counts and per-case parity flags are stable, so an agent or script can diff two
// runs to detect a new semantics drift.
type ConformanceMatrix struct {
	SchemaVersion string              `json:"schema_version"`
	GeneratedBy   string              `json:"generated_by"`
	Results       []ConformanceResult `json:"results"`
	Pass          int                 `json:"pass"`
	Fail          int                 `json:"fail"`
}

// InterpreterRun evaluates src with the AST interpreter and returns everything
// written to stdout plus any runtime error. This is the interpreter half of a
// conformance case; the AOT half is produced by compiling and running the native
// binary (the integration test shells out to llc/cc).
//
// InterpreterRun is not safe for concurrent use: it redirects the process-wide
// os.Stdout for the duration of evaluation.
func InterpreterRun(src string) (stdout string, err error) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return "", fmt.Errorf("pipe: %w", err)
	}
	os.Stdout = w
	_, _, evalErr := EvalExpr(src)
	os.Stdout = old
	w.Close()
	out := make([]byte, 1<<20)
	n, _ := r.Read(out)
	if evalErr != nil {
		return string(out[:n]), evalErr
	}
	return string(out[:n]), nil
}

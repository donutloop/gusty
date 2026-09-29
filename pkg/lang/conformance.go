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
	"strings"
)

// ConformanceSchemaVersion is the machine-readable JSON schema version for the
// conformance matrix emitted by the integration conformance test.
//
// 1.1 (L11.9, ADR 0186) adds the third leg: `python_stdout`/`python_ok`/`python_error`,
// the `interp_matches_python`/`aot_matches_python` flags, the computed `oracle`
// classification with its declared counterpart, reason, roadmap reference, notes
// and the matrix-level oracle counters. 1.0 rows compared the two backends to
// each other only; a 1.1 row compares them to CPython as well.
const ConformanceSchemaVersion = "1.2"

// ConformanceCase is one whole-program conformance case. Every case is a single
// merged source that both backends lower independently. Shared marks whether
// the construct is expected to be shared surface: run on BOTH backends and
// produce identical stdout. Non-shared (backend-specific) cases are recorded
// in the matrix but not asserted for parity.
//
// The Oracle fields are the case's *declared* CPython-oracle state (L11.9): the
// classification the harness must observe, why it is that way, and which roadmap
// item owns the debt. Declaration is required — an undocumented oracle state is a
// harness failure — and the observed state must equal it, so both a regression and
// an unrecorded fix fail the build.
type ConformanceCase struct {
	ID     string `json:"id"`     // stable identifier, e.g. "programs/single"
	Name   string `json:"name"`   // human label, e.g. "single.gy"
	Source string `json:"source"` // merged single-source program
	Shared bool   `json:"shared"` // expected to run on both backends with equal stdout

	Oracle string      `json:"oracle"`           // declared: match | debt | not_applicable
	Reason string      `json:"reason,omitempty"` // why the case is in that state
	Ref    string      `json:"ref,omitempty"`    // roadmap/gap/ADR that owns the debt
	Rules  []string    `json:"rules,omitempty"`  // extra documented comparison rules
	Pins   []OraclePin `json:"pins,omitempty"`   // what each leg prints today (ratchet)
}

// OraclePin records what one leg of a debt row does today: the stdout it produces,
// or that it fails (with an optional substring the failure must contain). Pinning
// the current wrong answer is what turns a debt into a test: an unrelated change
// that moves the output fails the row, and a fix that closes the debt fails the row
// with "debt paid" instead of quietly disappearing from the ledger.
type OraclePin struct {
	Backend string `json:"backend"`           // "interpreter" | "aot"
	Stdout  string `json:"stdout,omitempty"`  // expected stdout when Err is empty
	Err     string `json:"error,omitempty"`   // expected failure; "" + Missing means any failure
	Missing bool   `json:"missing,omitempty"` // this leg is expected to fail (refuse/trap)
}

// ConformanceResult records the observed behaviour of one case on all three legs:
// the interpreter stdout, the AOT binary stdout, and the CPython stdout. Parity is
// interpreter stdout == AOT stdout; the oracle flag is what each backend prints
// against CPython.
type ConformanceResult struct {
	Case      ConformanceCase `json:"case"`
	InterpOK  bool            `json:"interp_ok"`
	InterpOut string          `json:"interp_stdout"`
	InterpErr string          `json:"interp_error,omitempty"`
	AOTOK     bool            `json:"aot_ok"`
	AOTOut    string          `json:"aot_stdout"`
	AOTErr    string          `json:"aot_error,omitempty"`
	Parity    bool            `json:"parity"` // interpreter stdout == AOT stdout

	PythonOK            bool     `json:"python_ok"`
	PythonOut           string   `json:"python_stdout"`
	PythonErr           string   `json:"python_error,omitempty"`
	InterpMatchesPython bool     `json:"interp_matches_python"`
	AOTMatchesPython    bool     `json:"aot_matches_python"`
	Oracle              string   `json:"oracle"` // computed classification
	OracleDeclared      string   `json:"oracle_declared"`
	OracleReason        string   `json:"oracle_reason,omitempty"`
	OracleRef           string   `json:"oracle_ref,omitempty"`
	OracleRules         []string `json:"oracle_rules,omitempty"`
	OracleNotes         []string `json:"oracle_notes,omitempty"`
	OracleDrift         []string `json:"oracle_drift,omitempty"`
}

// ConformanceMatrix is the machine-readable conformance matrix artifact. It is
// deterministic: for the same case registry and compiler toolchain the pass/fail
// counts and per-case parity flags are stable, so an agent or script can diff two
// runs to detect a new semantics drift.
type ConformanceMatrix struct {
	SchemaVersion string              `json:"schema_version"`
	GeneratedBy   string              `json:"generated_by"`
	Toolchain     OracleToolchain     `json:"toolchain"`
	Results       []ConformanceResult `json:"results"`
	Rows          int                 `json:"rows"`
	Skipped       int                 `json:"skipped"` // non-shared rows: recorded, parity not asserted
	Pass          int                 `json:"pass"`
	Fail          int                 `json:"fail"`
	OracleMatched int                 `json:"oracle_match"`
	OracleDebt    int                 `json:"oracle_debt"`
	OracleNA      int                 `json:"oracle_not_applicable"`
	OracleDrift   int                 `json:"oracle_drift"`
}

// OracleToolchain records what produced the artifact, so a matrix row can be
// reproduced: the pinned LLVM for the compiled leg and the oracle interpreter for
// the CPython leg (docs/operations.md § The oracle leg).
type OracleToolchain struct {
	Python string `json:"python"`
	LLVM   string `json:"llvm,omitempty"`
	// MinPython is the pinned minimum the corpus is validated against
	// (OracleMinPython). It is recorded rather than implied because the ledger's
	// declared verdicts presuppose it: `type Count = int` has no oracle answer on a
	// CPython that cannot parse PEP 695, and on an older interpreter a whole set of
	// rows drifts for reasons that have nothing to do with the compiler (ADR 0193).
	MinPython string `json:"min_python,omitempty"`
}

// InterpreterRunOptions tunes the interpreter for harnesses that need to prove
// something about the collector rather than infer it from program output.
type InterpreterRunOptions struct {
	// GCStress collects at every statement boundary instead of only when
	// allocation pressure asks for it (the L7.2 soundness probe).
	GCStress bool
	// GCAllocThreshold overrides the allocation count that triggers a collection
	// (0 keeps the default).
	GCAllocThreshold int64
}

// InterpreterRunOpts is InterpreterRun plus the collector's self-report, so a
// conformance case can assert that collections really happened while the program
// ran instead of trusting a threshold to have been crossed.
//
// Like InterpreterRun it redirects the process-wide stdout and is not safe for
// concurrent use.
func InterpreterRunOpts(src string, opt InterpreterRunOptions) (stdout string, stats GCStats, err error) {
	prog, perr := Parse(src)
	if perr != nil {
		return "", GCStats{}, perr
	}
	ev := NewEvaluator()
	if opt.GCStress {
		ev.SetGCStress(true)
	}
	if opt.GCAllocThreshold > 0 {
		ev.SetGCAllocThreshold(opt.GCAllocThreshold)
	}
	old := os.Stdout
	r, w, werr := os.Pipe()
	if werr != nil {
		return "", GCStats{}, fmt.Errorf("pipe: %w", werr)
	}
	os.Stdout = w
	_, evalErr := ev.EvalProgram(prog)
	os.Stdout = old
	w.Close()
	out := make([]byte, 1<<20)
	n, _ := r.Read(out)
	return string(out[:n]), ev.GCStats(), evalErr
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

// OracleCheck classifies one case's three legs, writes the verdict onto the
// result, and returns the drift between what the registry *declared* and what the
// legs actually showed. An empty slice means the row is honest: the recorded
// classification, its reason, its owner and its per-leg pins all still describe
// reality.
//
// Drift fires in both directions on purpose. A row that got worse fails, and a row
// that got *better* fails too — an unrecorded fix is how a ledger turns into fiction
// (ADR 0186).
func (c ConformanceCase) OracleCheck(r *ConformanceResult) []string {
	rep := BuildOracleReport(r.InterpOK, r.InterpOut, r.InterpErr,
		r.AOTOK, r.AOTOut, r.AOTErr, c.Rules, r.PythonOK, r.PythonOut, r.PythonErr)
	r.Oracle = rep.Status
	r.InterpMatchesPython = rep.Legs[0].Matches
	r.AOTMatchesPython = rep.Legs[1].Matches
	r.OracleDeclared = c.Oracle
	r.OracleReason = c.Reason
	r.OracleRef = c.Ref
	r.OracleRules = rep.Rules
	r.OracleNotes = rep.Notes

	var drift []string
	switch c.Oracle {
	case OracleMatch, OracleDebt, OracleNA:
	case "":
		drift = append(drift, "case declares no oracle state — classify it as match, debt or not_applicable")
	default:
		drift = append(drift, fmt.Sprintf("unknown declared oracle state %q", c.Oracle))
	}
	if c.Oracle == OracleNA && c.Reason == "" {
		drift = append(drift, "not_applicable row without a reason (which gusty-only surface makes CPython unable to run it?)")
	}
	if c.Oracle == OracleDebt {
		if c.Reason == "" {
			drift = append(drift, "debt row without a reason")
		}
		if c.Ref == "" {
			drift = append(drift, "debt row without a roadmap reference (who owns the fix?)")
		}
		if len(c.Pins) == 0 {
			drift = append(drift, "debt row without a pin (what does each leg print today?)")
		}
	}
	if c.Oracle != "" && r.Oracle != c.Oracle {
		if r.Oracle == OracleMatch {
			drift = append(drift, "oracle debt is paid: both backends now print CPython's answer — update the registry (declared "+c.Oracle+")")
		} else {
			drift = append(drift, fmt.Sprintf("declared oracle %q, observed %q (%s)", c.Oracle, r.Oracle, strings.Join(r.OracleNotes, "; ")))
		}
	}
	for _, p := range c.Pins {
		ok, out, errMsg := r.leg(p.Backend)
		switch {
		case p.Missing && ok:
			drift = append(drift, fmt.Sprintf("pin says the %s leg fails, but it ran and printed %q — the refusal is gone", p.Backend, out))
		case p.Missing && p.Err != "" && !strings.Contains(errMsg, p.Err):
			drift = append(drift, fmt.Sprintf("pin says the %s leg fails with %q, got %q", p.Backend, p.Err, firstLine(errMsg)))
		case !p.Missing && !ok:
			drift = append(drift, fmt.Sprintf("pin says the %s leg prints %q, but it failed: %s", p.Backend, p.Stdout, firstLine(errMsg)))
		case !p.Missing && OracleNormalize(out, rep.Rules) != OracleNormalize(p.Stdout, rep.Rules):
			drift = append(drift, fmt.Sprintf("pin says the %s leg prints %q, got %q", p.Backend, p.Stdout, out))
		}
	}
	return drift
}

// leg returns one observed leg by backend name.
func (r *ConformanceResult) leg(backend string) (ok bool, stdout, stderr string) {
	switch backend {
	case "interpreter":
		return r.InterpOK, r.InterpOut, r.InterpErr
	case "aot":
		return r.AOTOK, r.AOTOut, r.AOTErr
	case "python":
		return r.PythonOK, r.PythonOut, r.PythonErr
	}
	return false, "", "unknown leg " + backend
}

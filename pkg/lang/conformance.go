// Package lang exposes the conformance matrix: a stable, machine-readable JSON schema
// that records, for every whole-program integration case, whether the program the one
// backend compiles prints what CPython prints for the same source.
//
// The contract (see docs/conformance-spec.md) is the reference comparison: compiling and
// running a source must produce byte-identical stdout to the pinned CPython, under the
// documented comparison rules. The matrix asserts that over every case and records the
// observed outputs, so a semantics drift is caught as a row failing its pin rather than
// being silently accepted.
//
// Until ADR 0302 the matrix had a second leg — the AST interpreter — and a second
// contract, "the two backends print the same thing" (parity). That contract could not
// catch a wrong answer both engines shared, which is why the CPython leg was added
// (ADR 0186); once the reference is in the room, engine-vs-engine agreement is a
// restatement of "we both got it right" rather than a fact of its own. With one backend
// the matrix asks one question.
package lang

import (
	"fmt"
	"strings"
)

// ConformanceSchemaVersion is the machine-readable JSON schema version for the
// conformance matrix emitted by the integration conformance test.
//
// 1.0 (Phase 8) compared the two backends to each other. 1.1 (L11.9, ADR 0186) added the
// CPython leg: `python_stdout`/`python_ok`/`python_error`, the `*_matches_python` flags,
// the computed `oracle` classification with its declared counterpart, reason, roadmap
// reference, notes and the matrix-level oracle counters; 1.2 (ADR 0193) added the pinned
// `toolchain.min_python`.
//
// 2.0 (ADR 0302) retires the AST interpreter and every field that described it:
// `interp_ok`/`interp_stdout`/`interp_error`, `interp_matches_python`, the row's `parity`
// flag, and the `shared` marker — which said "this case runs on both backends" and is now
// written as `asserted`, because what an asserted row claims is that the compiled program
// matches the reference. A pin's `backend` is `"aot"` or `"python"`; `"interpreter"` is no
// longer a legal value, and a ledger that still carries one fails the build rather than
// being silently ignored.
const ConformanceSchemaVersion = "2.0"

// ConformanceCase is one whole-program conformance case: a single merged source the
// compiler lowers and runs. Asserted marks the rows whose claim is measured — the compiled
// program printed what the pinned CPython printed. A row that is not asserted is still
// recorded (its refusal, its trap, its known-wrong answer, each pinned), it just makes no
// conformance claim: that is where the gusty-only surface and the open debts live.
//
// The Oracle fields are the case's *declared* CPython-oracle state (L11.9): the
// classification the harness must observe, why it is that way, and which roadmap
// item owns the debt. Declaration is required — an undocumented oracle state is a
// harness failure — and the observed state must equal it, so both a regression and
// an unrecorded fix fail the build.
type ConformanceCase struct {
	ID       string `json:"id"`       // stable identifier, e.g. "programs/single"
	Name     string `json:"name"`     // human label, e.g. "single.gy"
	Source   string `json:"source"`   // merged single-source program
	Asserted bool   `json:"asserted"` // the compiled leg's stdout is asserted against CPython

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
	Backend string `json:"backend"`           // "aot" | "python"
	Stdout  string `json:"stdout,omitempty"`  // expected stdout when Err is empty
	Err     string `json:"error,omitempty"`   // expected failure; "" + Missing means any failure
	Missing bool   `json:"missing,omitempty"` // this leg is expected to fail (refuse/trap)
}

// ConformanceResult records the observed behaviour of one case on both legs: the stdout
// of the native binary the compiler produced, and the stdout of the pinned CPython.
//
// `parity` is gone with the second engine. Its replacement is `conformant`, which is the
// assertion an asserted row makes: the compiled stdout equals the reference stdout under
// the row's comparison rules.
type ConformanceResult struct {
	Case   ConformanceCase `json:"case"`
	AOTOK  bool            `json:"aot_ok"`
	AOTOut string          `json:"aot_stdout"`
	AOTErr string          `json:"aot_error,omitempty"`

	PythonOK         bool     `json:"python_ok"`
	PythonOut        string   `json:"python_stdout"`
	PythonErr        string   `json:"python_error,omitempty"`
	AOTMatchesPython bool     `json:"aot_matches_python"`
	Conformant       bool     `json:"conformant"` // asserted rows: compiled stdout == reference stdout
	Oracle           string   `json:"oracle"`     // computed classification
	OracleDeclared   string   `json:"oracle_declared"`
	OracleReason     string   `json:"oracle_reason,omitempty"`
	OracleRef        string   `json:"oracle_ref,omitempty"`
	OracleRules      []string `json:"oracle_rules,omitempty"`
	OracleNotes      []string `json:"oracle_notes,omitempty"`
	OracleDrift      []string `json:"oracle_drift,omitempty"`
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
	Skipped       int                 `json:"skipped"` // non-asserted rows: recorded, no conformance claim made
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
	rep := BuildOracleReport(r.AOTOK, r.AOTOut, r.AOTErr,
		c.Rules, r.PythonOK, r.PythonOut, r.PythonErr)
	r.Oracle = rep.Status
	r.AOTMatchesPython = rep.Legs[0].Matches
	r.OracleDeclared = c.Oracle
	r.OracleReason = c.Reason
	r.OracleRef = c.Ref
	r.OracleRules = rep.Rules
	r.OracleNotes = rep.Notes

	var drift []string
	pinsOK := true
	pinDriftStart := 0
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
			drift = append(drift, "oracle debt is paid: the compiled leg now prints CPython's answer — update the registry (declared "+c.Oracle+")")
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
	pinsOK = pinsOK && len(drift) == pinDriftStart
	// Conformance, and which of the two possible assertions a row is under.
	//
	// Where the reference can answer, conformance is equality with it: that is the claim an asserted
	// row makes, and the one the matrix exists to check. Where the reference *cannot* answer — a
	// `string.DIGITS` the Python stdlib does not have, an `await` at module scope CPython calls a
	// SyntaxError, a debt row whose divergence the registry owns by name — demanding equality with an
	// empty or truncated CPython run would not be strictness, it would be a test that fails for the
	// reason the row documents. There the assertion is the row's pin: the answer the compiled leg was
	// measured to produce, checked above and drifted on when it moves. A row cannot be conformant
	// because nobody could disprove it, so a not_applicable/debt row with no pin is drift, and a row
	// whose pin no longer holds is neither conformant nor quiet.
	switch r.Oracle {
	case OracleNA, OracleDebt:
		r.Conformant = c.Asserted && pinsOK && len(c.Pins) > 0
	default:
		r.Conformant = c.Asserted && r.AOTMatchesPython
	}
	return drift
}

// leg returns one observed leg by backend name. An unknown name is reported as a failure
// rather than an empty success: a ledger row that pins a leg the harness no longer runs (the
// interpreter, before ADR 0302 retired it) has to read as drift, because a pin nobody checks
// is how a ledger starts describing a compiler that does not exist.
func (r *ConformanceResult) leg(backend string) (ok bool, stdout, stderr string) {
	switch backend {
	case "interpreter":
		return false, "", `no "interpreter" leg: the AST interpreter was retired by ADR 0302 — delete this pin (the compiled leg is the leg that runs)`
	case "aot":
		return r.AOTOK, r.AOTOut, r.AOTErr
	case "python":
		return r.PythonOK, r.PythonOut, r.PythonErr
	}
	return false, "", "unknown leg " + backend
}

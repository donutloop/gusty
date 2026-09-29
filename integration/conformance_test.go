package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// runAOTConformance evaluates src through the LLVM AOT pipeline (Compile ->
// llc -> cc -> run) and returns the stdout of the produced native binary.
func runAOTConformance(t *testing.T, src string) (string, error) {
	t.Helper()
	res, err := lang.Compile(src)
	if err != nil {
		return "", err
	}
	dir := t.TempDir()
	irPath := filepath.Join(dir, "prog.ll")
	objPath := filepath.Join(dir, "prog.o")
	binPath := filepath.Join(dir, "prog")
	if err := os.WriteFile(irPath, []byte(res.IR), 0o600); err != nil {
		return "", err
	}
	if out, err := exec.Command(llc, "-filetype=obj", "-relocation-model=pic", irPath, "-o", objPath).CombinedOutput(); err != nil {
		return "", fmt.Errorf("%w: %s", err, firstNonEmptyLine(string(out)))
	}
	if out, err := exec.Command("cc", objPath, "-lm", "-o", binPath).CombinedOutput(); err != nil {
		return "", fmt.Errorf("%w: %s", err, firstNonEmptyLine(string(out)))
	}
	got, err := exec.Command(binPath).Output()
	if err != nil {
		return string(got), err
	}
	return string(got), nil
}

func firstNonEmptyLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			return t
		}
	}
	return ""
}

// safeInterpreterRun is lang.InterpreterRun with a net under it. A Go panic in the
// compiler or the evaluator used to take the whole test binary down, which turns
// one bad program into "the suite crashed" and loses every other row; recorded as
// a leg failure it is instead a matrix row with a name (ADR 0186, L11.8).
func safeInterpreterRun(t *testing.T, src string) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = "", fmt.Errorf("compiler panic: %v", r)
		}
	}()
	return lang.InterpreterRun(src)
}

// safeAOTRun is runAOTConformance with the same net: codegen panics are recorded,
// not fatal, so the corpus keeps reporting the other programs.
func safeAOTRun(t *testing.T, src string) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = "", fmt.Errorf("compiler panic: %v", r)
		}
	}()
	return runAOTConformance(t, src)
}

// safeOracleRun runs the CPython leg, recovering from an unavailable interpreter
// so the harness reports "no oracle" instead of crashing the suite.
func safeOracleRun(t *testing.T, src string) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = "", fmt.Errorf("oracle panic: %v", r)
		}
	}()
	out, stderr, err := lang.PythonRun(src)
	if err != nil {
		return out, fmt.Errorf("%w: %s", err, firstNonEmptyLine(stderr))
	}
	return out, nil
}

// toolchainRecord names the interpreters that produced this artifact, including the
// pinned minimum the ledger's declared verdicts presuppose. Recording it is the point:
// a matrix row that says "match" means "matches the pinned oracle", and without the
// version in the artifact two machines can produce different matrices from the same
// code and neither can tell why (ADR 0193).
func toolchainRecord() lang.OracleToolchain {
	rec := lang.OracleToolchain{Python: lang.PythonBinary(), MinPython: lang.OracleMinPython}
	if out, err := exec.Command(lang.PythonBinary(), "--version").CombinedOutput(); err == nil {
		rec.Python = strings.TrimSpace(firstNonEmptyLine(string(out)))
	}
	if out, err := exec.Command(llc, "--version").Output(); err == nil {
		rec.LLVM = strings.TrimSpace(firstNonEmptyLine(string(out)))
	}
	return rec
}

// requirePinnedOracle fails the run with the remedy rather than letting an oracle older
// than the pin present as a pile of oracle drift. `type Count = int` (PEP 695) is the
// live example: on CPython 3.10 it is a SyntaxError on line 1, so programs/typealias
// "drifted" from match to not_applicable on a CI runner and the note blamed the
// compiler. A stale oracle is an environment fault, and an environment fault has to
// read like one.
func requirePinnedOracle(t *testing.T) {
	t.Helper()
	out, err := exec.Command(lang.PythonBinary(), "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("oracle %q is not runnable: %v", lang.PythonBinary(), err)
	}
	banner := strings.TrimSpace(firstNonEmptyLine(string(out)))
	if lang.OracleVersionTooOld(banner) {
		t.Fatalf("the oracle is %s but the corpus is validated against %s (docs/operations.md): install a newer python3 or set GUSTY_PYTHON. A matrix run on an older oracle reports drift that is not the compiler's.",
			banner, lang.OracleMinPython)
	}
}

// runLegs is the three legs of one case, and nothing else: interpreter, compiled
// binary, CPython. No assertion lives here — a leg that fails is data.
func runLegs(t *testing.T, c lang.ConformanceCase) lang.ConformanceResult {
	row := lang.ConformanceResult{Case: c}
	out, ierr := safeInterpreterRun(t, c.Source)
	row.InterpOut, row.InterpErr = out, errText(ierr)
	row.InterpOK = ierr == nil

	aout, aerr := safeAOTRun(t, c.Source)
	row.AOTOut, row.AOTErr = aout, errText(aerr)
	row.AOTOK = aerr == nil

	pout, perr := safeOracleRun(t, c.Source)
	row.PythonOut, row.PythonErr = pout, errText(perr)
	row.PythonOK = perr == nil

	row.Parity = row.InterpOK && row.AOTOK && row.InterpOut == row.AOTOut
	row.OracleDrift = c.OracleCheck(&row)
	return row
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

var (
	matrixOnce   sync.Once
	matrixResult lang.ConformanceMatrix
)

// conformanceMatrix runs the whole corpus once per test binary, writes the
// machine-readable artifact, and hands the same matrix to every test that wants a
// row — so an oracle assertion cannot depend on which test happened to run first.
func conformanceMatrix(t *testing.T) lang.ConformanceMatrix {
	t.Helper()
	matrixOnce.Do(func() {
		matrixResult = buildConformanceMatrix(t)
		b, err := json.MarshalIndent(matrixResult, "", "  ")
		if err != nil {
			matrixResult.Results = nil
			return
		}
		if err := os.WriteFile("conformance-matrix.json", append(b, '\n'), 0o644); err != nil {
			t.Errorf("write matrix artifact: %v", err)
		}
	})
	if matrixResult.Results == nil {
		t.Fatal("conformance matrix could not be built")
	}
	return matrixResult
}

func buildConformanceMatrix(t *testing.T) lang.ConformanceMatrix {
	cases := conformanceCases()
	matrix := lang.ConformanceMatrix{
		SchemaVersion: lang.ConformanceSchemaVersion,
		GeneratedBy:   "integration/conformance_test.go",
		Toolchain:     toolchainRecord(),
	}
	for _, c := range cases {
		row := runLegs(t, c)
		matrix.Results = append(matrix.Results, row)
		matrix.Rows++
		if c.Shared {
			if row.Parity {
				matrix.Pass++
			} else {
				matrix.Fail++
			}
		} else {
			// A probe row is not asserted for parity (it is a recorded refusal or a
			// known-wrong answer); its contract is its pin, checked by OracleCheck.
			matrix.Skipped++
		}
		switch row.Oracle {
		case lang.OracleMatch:
			matrix.OracleMatched++
		case lang.OracleDebt:
			matrix.OracleDebt++
		case lang.OracleNA:
			matrix.OracleNA++
		}
		if len(row.OracleDrift) > 0 {
			matrix.OracleDrift++
		}
	}
	return matrix
}

// TestConformanceMatrix runs every whole-program integration case through THREE
// legs — the AST interpreter, the LLVM AOT compiler, and CPython — records the
// outcome in a machine-readable matrix, and asserts two contracts:
//
//  1. parity: every shared case prints identical stdout on both backends; and
//  2. the oracle: every case's observed CPython state equals the state the
//     registry declares, with its reason, owner and per-leg pins still true.
//
// Contract 2 is what L11.9 adds (ADR 0186): before it, a construct that *both*
// backends got wrong was invisible to CI, because parity only compares the two
// implementations to each other. The artifact goes to
// integration/conformance-matrix.json for agent and script consumption.
func TestConformanceMatrix(t *testing.T) {
	requirePinnedOracle(t)
	matrix := conformanceMatrix(t)

	// Assert the shared-lowering contract: every shared case must pass parity.
	if matrix.Fail != 0 {
		for _, r := range matrix.Results {
			if !r.Parity && r.Case.Shared {
				t.Errorf("conformance case %s (%s): interp=%q aot=%q", r.Case.ID, r.Case.Name, r.InterpOut, r.AOTOut)
			}
		}
		t.Fatalf("conformance matrix: %d/%d cases failed parity", matrix.Fail, len(matrix.Results))
	}

	// Assert the oracle contract: the registry must describe reality in both
	// directions — a new divergence and an unrecorded fix are both failures.
	if matrix.OracleDrift != 0 {
		for _, r := range matrix.Results {
			for _, d := range r.OracleDrift {
				t.Errorf("oracle drift %s (%s): %s", r.Case.ID, r.Case.Name, d)
			}
		}
		t.Fatalf("conformance matrix: %d/%d cases drifted from their declared CPython-oracle state",
			matrix.OracleDrift, len(matrix.Results))
	}

	t.Logf("conformance matrix: %d/%d parity, oracle %d match / %d debt / %d not-applicable over %d cases (artifact: integration/conformance-matrix.json)",
		matrix.Pass, len(matrix.Results), matrix.OracleMatched, matrix.OracleDebt, matrix.OracleNA, len(matrix.Results))
}

// TestConformanceMatrixRecordsTheOracleLeg checks the artifact itself, not the
// programs: a matrix that silently stopped recording the third leg would keep
// every parity assertion green while the oracle went away.
func TestConformanceMatrixRecordsTheOracleLeg(t *testing.T) {
	b, err := os.ReadFile("conformance-matrix.json")
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	var m lang.ConformanceMatrix
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal artifact: %v", err)
	}
	if m.SchemaVersion != lang.ConformanceSchemaVersion {
		t.Errorf("artifact schema %q, want %q", m.SchemaVersion, lang.ConformanceSchemaVersion)
	}
	if m.Toolchain.Python == "" || !strings.Contains(strings.ToLower(m.Toolchain.Python), "python") {
		t.Errorf("artifact does not name the oracle interpreter: %q", m.Toolchain.Python)
	}
	if m.Toolchain.LLVM == "" {
		t.Errorf("artifact does not name the LLVM that produced the compiled leg")
	}
	for _, r := range m.Results {
		if r.Oracle == "" {
			t.Errorf("%s: row has no oracle classification", r.Case.ID)
		}
		if r.OracleDeclared != r.Oracle {
			t.Errorf("%s: declared %q but observed %q", r.Case.ID, r.OracleDeclared, r.Oracle)
		}
		if len(r.Case.Pins) > 0 && r.Oracle == lang.OracleMatch {
			t.Errorf("%s: a match row still carries a debt pin", r.Case.ID)
		}
	}
	if m.OracleMatched+m.OracleDebt+m.OracleNA != len(m.Results) {
		t.Errorf("oracle counters %d+%d+%d do not cover %d rows", m.OracleMatched, m.OracleDebt, m.OracleNA, len(m.Results))
	}
}

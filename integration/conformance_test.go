package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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
	if _, err := exec.Command(llc, "-filetype=obj", "-relocation-model=pic", irPath, "-o", objPath).CombinedOutput(); err != nil {
		return "", err
	}
	if _, err := exec.Command("cc", objPath, "-lm", "-o", binPath).CombinedOutput(); err != nil {
		return "", err
	}
	got, err := exec.Command(binPath).Output()
	if err != nil {
		return "", err
	}
	return string(got), nil
}

// TestConformanceMatrix runs every whole-program integration case through BOTH
// backends (the AST interpreter and the LLVM AOT compiler), diffs the stdout,
// records the outcome in a machine-readable matrix, and asserts every shared
// case passes parity. It also writes the JSON matrix artifact to
// integration/conformance-matrix.json for agent/script consumption.
func TestConformanceMatrix(t *testing.T) {
	cases := conformanceCases()
	matrix := lang.ConformanceMatrix{
		SchemaVersion: lang.ConformanceSchemaVersion,
		GeneratedBy:   "integration/conformance_test.go",
	}

	for _, c := range cases {
		row := lang.ConformanceResult{Case: c}

		// Interpreter half.
		out, ierr := lang.InterpreterRun(c.Source)
		row.InterpOut = out
		if ierr != nil {
			row.InterpErr = ierr.Error()
		} else {
			row.InterpOK = true
		}

		// AOT half (native binary).
		aout, aerr := runAOTConformance(t, c.Source)
		row.AOTOut = aout
		if aerr != nil {
			row.AOTErr = aerr.Error()
		} else {
			row.AOTOK = true
		}

		// Parity: identical stdout on both backends.
		row.Parity = row.InterpOK && row.AOTOK && row.InterpOut == row.AOTOut

		matrix.Results = append(matrix.Results, row)
		if row.Parity {
			matrix.Pass++
		} else {
			matrix.Fail++
		}
	}

	// Emit the machine-readable matrix artifact.
	b, err := json.MarshalIndent(matrix, "", "  ")
	if err != nil {
		t.Fatalf("marshal matrix: %v", err)
	}
	if err := os.WriteFile("conformance-matrix.json", append(b, '\n'), 0o644); err != nil {
		t.Fatalf("write matrix artifact: %v", err)
	}

	// Assert the shared-lowering contract: every shared case must pass parity.
	if matrix.Fail != 0 {
		for _, r := range matrix.Results {
			if !r.Parity {
				t.Errorf("conformance case %s (%s): interp=%q aot=%q", r.Case.ID, r.Case.Name, r.InterpOut, r.AOTOut)
			}
		}
		t.Fatalf("conformance matrix: %d/%d cases failed parity", matrix.Fail, len(matrix.Results))
	}
	t.Logf("conformance matrix: %d/%d cases passed parity (artifact: integration/conformance-matrix.json)", matrix.Pass, len(matrix.Results))
}

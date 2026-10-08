package lang

// The in-package cases that ask CPython directly (rather than through the harness) are the reference
// leg too, and the leg only counts as evidence if it answers the same question the same way twice.
//
// `PythonRun` — the production entry — pins PYTHONHASHSEED=0 and the oracle binary, and the conformance
// matrix depends on that (docs/operations.md § The pinned toolchains). These three helpers wanted the
// *combined* stream rather than stdout and stderr apart, because a raise's sentence arrives on stderr
// and the row prints the last line of it; that difference of shape was the whole reason they grew their
// own `exec.Command`, and they grew it without the pin. A set holding a text is ordered by that text's
// hash, CPython randomises string hashing per process, and a row that prints one would have been a coin
// flip: measured at three differing answers in twenty runs of `s = {1}; s.add("a"); print(s)`.

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// pythonTwin runs one source through the pinned oracle and hands back everything it wrote.
//
// The two lines that make it a leg rather than a subprocess are PythonBinary() (GUSTY_PYTHON, so a case
// cannot judge gusty against an interpreter the harness would not name in the matrix) and
// PYTHONHASHSEED=0 (so it cannot judge it against a different answer on the next run).
func pythonTwin(t *testing.T, src string) (string, error) {
	t.Helper()
	py := PythonBinary()
	if _, err := exec.LookPath(py); err != nil {
		t.Skipf("no %s to cross-check (set GUSTY_PYTHON)", py)
	}
	cmd := exec.Command(py, "-c", src)
	cmd.Env = append(os.Environ(), "PYTHONHASHSEED=0")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// pythonTwinLine is the reference's one-line answer, trimmed — the shape most rows assert against.
func pythonTwinLine(t *testing.T, src string) string {
	t.Helper()
	out, err := pythonTwin(t, src)
	if err != nil {
		// A raise: the last line is the sentence the program would print, which is what the row pins.
		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
		return strings.TrimSpace(lines[len(lines)-1])
	}
	return out
}

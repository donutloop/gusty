package integration

// The reference leg is a subprocess, and a subprocess has an environment.
//
// `lang.PythonRun` — the conformance matrix's oracle call — pins `PYTHONHASHSEED=0` and says why: the
// matrix artifact is committed, and a set rendering whose order changes between two runs of the same
// source would read as a compiler regression (pkg/lang/oracle.go). The CLI-level cases in this package
// ask CPython the same questions through their own `exec.Command(py, path)` calls, and seven of them
// never set the seed. So any case whose reference answer prints a set containing a text was a coin flip
// on the hash of that text:
//
//	$ for i in $(seq 20); do python3 -c "s={1}; s.add('a'); print(s)"; done | sort | uniq -c
//	     17 {1, 'a'}
//	      3 {'a', 1}
//
// while the same program in gusty prints `{1, 'a'}` every time, because gusty's sets are
// insertion-ordered and always have been. That is `TestCLIAgentPairBoundNameMutatesAContainer…/
// the_a_text_slot_added_to_a_set` failing one run in six and passing the next, on an unchanged tree.
//
// `TestTheReferenceLegsSetOrderIsPinned` is the guard: it asks the reference the same question twenty
// times and requires one answer. Without the pin it fails essentially always (three flips in twenty is
// the measured rate); with it, never.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// oracleCommand is the only way a case in this package starts CPython: the pinned oracle binary
// (GUSTY_PYTHON, docs/operations.md § The pinned toolchains) and the pinned hash seed.
//
// One function owns both, because both are silently wrong when a call site forgets them: a case that
// uses whatever `python3` is first on PATH is judging gusty against an unpinned interpreter, and a case
// that lets the seed randomise is judging gusty against a different answer on every run. The matrix
// already refused the first (ADR 0193); this file refuses the second.
// oracleBinaryName names the interpreter a message should blame. It is the same lookup
// oracleCommand makes, and the two must not disagree — a message that says "python3 failed" while a
// GUSTY_PYTHON override actually ran is the kind of detail that sends a reader to the wrong binary.
func oracleBinaryName() string {
	if p := os.Getenv("GUSTY_PYTHON"); p != "" {
		return p
	}
	return "python3"
}

func oracleCommand(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	py := oracleBinaryName()
	if _, err := exec.LookPath(py); err != nil {
		t.Skipf("no %s to act as the oracle (set GUSTY_PYTHON)", py)
	}
	cmd := exec.Command(py, args...)
	// The same pin lang.PythonRun sets: one answer per source, per machine, per run.
	cmd.Env = append(os.Environ(), "PYTHONHASHSEED=0")
	return cmd
}

// TestTheReferenceLegsSetOrderIsPinned is the coin-flip guard.
//
// A set holding a text is ordered by that text's hash, and CPython randomises string hashing per
// process unless PYTHONHASHSEED says otherwise. The compiled backend's sets are insertion-ordered, so
// the moment the reference's order drifts the two disagree about a program neither of them got wrong —
// and the row that fails is whichever set-shaped case happens to sit where the randomiser fancies a
// different table. Twenty runs is above the measured flipping rate with the pin off (3 in 20), so this
// case fails essentially always without the pin and never with it.
func TestTheReferenceLegsSetOrderIsPinned(t *testing.T) {
	dir := t.TempDir()
	const src = "xs = []\nxs.append(\"a\")\nn = xs[0]\ns2 = {1}\ns2.add(n)\nprint(s2)\n"
	answers := map[string]int{}
	for i := 0; i < 20; i++ {
		out, ok := cpythonPlainOut(t, dir, src)
		if !ok {
			t.Fatalf("run %d: the reference refused a program it prints: %s", i, src)
		}
		answers[out]++
	}
	if len(answers) != 1 {
		keys := make([]string, 0, len(answers))
		for k := range answers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%q x%d", k, answers[k]))
		}
		t.Fatalf("the reference gave %d different answers to one source in 20 runs (%s) — a leg that is not deterministic cannot be evidence against the compiler",
			len(answers), strings.Join(parts, ", "))
	}
	for got := range answers {
		// The pinned answer, spelled out: insertion order, which is what gusty's sets have always
		// printed. If the reference ever stops agreeing here, that row is debt to file, not a flake.
		if got != "{1, 'a'}\n" {
			t.Errorf("the reference printed %q for a set built by inserting 1 then \"a\"", got)
		}
	}
}

// TestTheReferenceLegRunsTheFileItIsGiven keeps oracleCommand honest about argv: a helper that ran the
// wrong path would make every reference answer in this package wrong in the same direction, quietly.
func TestTheReferenceLegRunsTheFileItIsGiven(t *testing.T) {
	dir := t.TempDir()
	path := writeSrc(t, dir, "argv_probe.py", "print(\"from the file\")\n")
	out, err := oracleCommand(t, path).CombinedOutput()
	if err != nil {
		t.Fatalf("oracle run: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != "from the file" {
		t.Fatalf("oracle printed %q, wanted the file's own line", out)
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("the probe path should be absolute for this argument to mean anything: %s", path)
	}
}

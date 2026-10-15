package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// --oracle is the CLI's answer to "does this program behave like Python?" (roadmap L11.9, ADR 0186):
// one program, two legs, one verdict — the artifact gusty compiles, and the pinned CPython. A third
// leg used to run the AST interpreter and report whether the two engines agreed; ADR 0302 retired it,
// and with it the `parity` member, because an engine agreeing with itself was never the interesting
// question. What is checked here is the contract an agent consumes — payload shape and exit code —
// by driving the real binary, not just the classifier.

type oraclePayload struct {
	Legs []struct {
		Backend string `json:"backend"`
		OK      bool   `json:"ok"`
		Stdout  string `json:"stdout"`
		Error   string `json:"error"`
		Matches bool   `json:"matches_python"`
	} `json:"legs"`
	Status string   `json:"oracle"`
	Notes  []string `json:"notes"`
	Rules  []string `json:"rules"`
}

func TestCLIOracleReportsBothLegs(t *testing.T) {
	t.Parallel()
	out, code := benchCLI(t, "--json", "--oracle", "print(1 + 1)\n")
	if code != exitOK {
		t.Fatalf("--oracle on a conformant program: exit = %d, want 0\n%s", code, out)
	}
	var p oraclePayload
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("--oracle --json payload: %v\n%s", err, out)
	}
	if p.Status != lang.OracleMatch {
		t.Errorf("oracle = %q, want %q", p.Status, lang.OracleMatch)
	}
	want := []string{lang.BackendName, "python"}
	if len(p.Legs) != len(want) {
		t.Fatalf("legs = %d, want 2 (%s, python): %s", len(p.Legs), lang.BackendName, out)
	}
	for i, name := range want {
		if p.Legs[i].Backend != name {
			t.Errorf("leg %d = %q, want %q", i, p.Legs[i].Backend, name)
		}
		if !p.Legs[i].OK {
			t.Errorf("leg %s did not complete: %s", name, p.Legs[i].Error)
		}
		if p.Legs[i].Stdout != "2\n" {
			t.Errorf("leg %s stdout = %q, want \"2\\n\"", name, p.Legs[i].Stdout)
		}
	}
	if !p.Legs[0].Matches {
		t.Errorf("the compiled leg should match the oracle here: %+v", p.Legs)
	}
	if len(p.Rules) == 0 {
		t.Errorf("the payload should name the comparison rules it applied")
	}
}

func TestCLIOracleExitCodeIsTheDivergence(t *testing.T) {
	t.Parallel()
	// A valid program that ran, printed the wrong thing, and owes an open debt: a `min`/`max` fold can
	// say what its answer is only when it can say which candidate won, and a candidate the compiler has
	// not folded to a constant is decided by a run-time `select` that keeps the payload and nothing
	// beside it that came from `True` (roadmap Gap R.124, pinned by
	// `programs/probe_minmax_candidate_unreadable`). The class is exit 6 — not a compile error (1) and
	// not a crash (3). This fixture has been migrated four times — `print(True)` until ADR 0257,
	// `print([True, 1])` until ADR 0259, `show(1 == 1)` until Gap R.111 carried a verdict across the
	// call boundary, and a `print` whose argument prints until ADR 0355 made the line atomic — because
	// a fixture pointed at a paid-off debt asserts a contract nothing violates.
	const foldCannotNameItsWinner = "i = 0\nprint(max([True, i]))\n"
	out, code := benchCLI(t, "--json", "--oracle", foldCannotNameItsWinner)
	if code != exitOracleDivergence {
		t.Fatalf("--oracle on a print whose argument prints: exit = %d, want %d\n%s", code, exitOracleDivergence, out)
	}
	var p oraclePayload
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("payload: %v\n%s", err, out)
	}
	if p.Status != lang.OracleDebt {
		t.Errorf("oracle = %q, want %q", p.Status, lang.OracleDebt)
	}
	if p.Legs[0].Matches {
		t.Errorf("the compiled leg claims a match on a program the reference answers differently: %+v", p.Legs)
	}
	human, hcode := benchCLICombined(t, "--oracle", foldCannotNameItsWinner)
	if hcode != exitOracleDivergence {
		t.Errorf("human form exit = %d, want %d", hcode, exitOracleDivergence)
	}
	for _, want := range []string{"oracle: debt", lang.BackendName, "python", "True", "rules:"} {
		if !strings.Contains(human, want) {
			t.Errorf("human --oracle output should mention %q:\n%s", want, human)
		}
	}
}

func TestCLIOracleHasNoVerdictWhenPythonCannotRunTheSource(t *testing.T) {
	t.Parallel()
	// `await` outside a coroutine is gusty surface, not Python's: there is no third
	// opinion, so the honest answer is "no verdict" (exit 7), never success.
	src := "async def f():\n    return 1\n\nprint(await f())\n"
	out, code := benchCLI(t, "--json", "--oracle", src)
	if code != exitOracleNoVerdict {
		t.Fatalf("--oracle on gusty-only surface: exit = %d, want %d\n%s", code, exitOracleNoVerdict, out)
	}
	var p oraclePayload
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("payload: %v\n%s", err, out)
	}
	if p.Status != lang.OracleNA {
		t.Errorf("oracle = %q, want %q", p.Status, lang.OracleNA)
	}
	if p.Legs[1].OK {
		t.Errorf("the python leg should be recorded as failed: %+v", p.Legs[1])
	}
	if len(p.Notes) == 0 || !strings.Contains(p.Notes[0], "CPython") {
		t.Errorf("a no-verdict report must say why: %v", p.Notes)
	}
}

func TestCLIOracleRecordsARefusedCompiledLegInsteadOfDying(t *testing.T) {
	t.Parallel()
	// This test used to drive `print([1, 2, 3][-1])`, which panicked in codegen; ADR 0210 fixed
	// that shape, so the fixture is a program whose compiled leg still fails while the
	// interpreter and CPython answer — iterating a string computed at run time (Gap R.16).
	// What the test is actually about has not changed: a leg that fails violently must be
	// *recorded* — the CLI stays alive, the exit code stays a verdict class, and the reason is
	// readable in the payload. The panic classification itself is pinned where it can be pinned
	// deterministically, in pkg/lang/oracle_test.go.
	// List concatenation still refuses on the compiled path (roadmap L11.1, Gap R.175): the
	// backend builds no runtime list-concatenate, so the leg fails while CPython prints [1, 2].
	// `str * int` used to stand here and has been answerable since ADR 0230 — a stub-detection
	// fixture has to point at a refusal that still exists, or it stops detecting anything.
	src := "print([1] + [2])\n"
	out, code := benchCLI(t, "--json", "--oracle", src)
	if code != exitOracleDivergence && code != exitOracleNoVerdict {
		t.Fatalf("--oracle on a refused compiled leg: exit = %d, want 6 or 7\n%s", code, out)
	}
	var p oraclePayload
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("payload: %v\n%s", err, out)
	}
	if p.Legs[0].OK {
		t.Fatalf("the compiled leg should have failed: %+v", p.Legs[0])
	}
	if !strings.Contains(p.Legs[0].Error, "no compiled lowering") {
		t.Errorf("the refusal should be named in the leg error, got %q", p.Legs[0].Error)
	}
	if !p.Legs[1].OK || p.Legs[1].Stdout != "[1, 2]\n" {
		t.Errorf("the reference should have answered while the compiled leg refused: %+v", p.Legs[1])
	}
}

func TestCLIOracleUsageErrors(t *testing.T) {
	t.Parallel()
	// --oracle with an unreadable file is a usage error (4), not a compile error (1).
	if _, code := benchCLI(t, "--oracle-file", "does/not/exist.gy"); code != exitUsage {
		t.Errorf("--oracle-file on a missing file: exit = %d, want %d", code, exitUsage)
	}
}

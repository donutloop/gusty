package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// --oracle is the CLI's third leg (roadmap L11.9, ADR 0186): one program, three
// engines, one verdict. These tests drive the real binary, because the thing being
// tested is the contract an agent consumes — the payload shape and the exit code —
// not just the classifier.

type oraclePayload struct {
	Legs []struct {
		Backend string `json:"backend"`
		OK      bool   `json:"ok"`
		Stdout  string `json:"stdout"`
		Error   string `json:"error"`
		Matches bool   `json:"matches_python"`
	} `json:"legs"`
	Parity bool     `json:"parity"`
	Status string   `json:"oracle"`
	Notes  []string `json:"notes"`
	Rules  []string `json:"rules"`
}

func TestCLIOracleReportsThreeLegs(t *testing.T) {
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
	if !p.Parity {
		t.Errorf("parity = false for a program both backends get right")
	}
	want := []string{"interpreter", "aot", "python"}
	if len(p.Legs) != len(want) {
		t.Fatalf("legs = %d, want 3 (interpreter, aot, python): %s", len(p.Legs), out)
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
	if !p.Legs[0].Matches || !p.Legs[1].Matches {
		t.Errorf("both backends should match the oracle here: %+v", p.Legs)
	}
	if len(p.Rules) == 0 {
		t.Errorf("the payload should name the comparison rules it applied")
	}
}

func TestCLIOracleExitCodeIsTheDivergence(t *testing.T) {
	// print(True) prints 1 on both backends and True in CPython: the program is
	// valid, it ran, and the answer is wrong. That class is exit 6 — not a compile
	// error (1) and not a crash (3).
	out, code := benchCLI(t, "--json", "--oracle", "print(True)\n")
	if code != exitOracleDivergence {
		t.Fatalf("--oracle on print(True): exit = %d, want %d\n%s", code, exitOracleDivergence, out)
	}
	var p oraclePayload
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("payload: %v\n%s", err, out)
	}
	if p.Status != lang.OracleDebt {
		t.Errorf("oracle = %q, want %q", p.Status, lang.OracleDebt)
	}
	if !p.Parity {
		t.Errorf("the backends agree with each other here — that is the point of the oracle leg")
	}
	if p.Legs[0].Matches || p.Legs[1].Matches {
		t.Errorf("neither leg may claim a match: %+v", p.Legs)
	}
	human, hcode := benchCLICombined(t, "--oracle", "print(True)\n")
	if hcode != exitOracleDivergence {
		t.Errorf("human form exit = %d, want %d", hcode, exitOracleDivergence)
	}
	for _, want := range []string{"oracle: debt", "interpreter", "aot", "python", "True", "rules:"} {
		if !strings.Contains(human, want) {
			t.Errorf("human --oracle output should mention %q:\n%s", want, human)
		}
	}
}

func TestCLIOracleHasNoVerdictWhenPythonCannotRunTheSource(t *testing.T) {
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
	if p.Legs[2].OK {
		t.Errorf("the python leg should be recorded as failed: %+v", p.Legs[2])
	}
	if len(p.Notes) == 0 || !strings.Contains(p.Notes[0], "CPython") {
		t.Errorf("a no-verdict report must say why: %v", p.Notes)
	}
}

func TestCLIOracleRecordsARefusedCompiledLegInsteadOfDying(t *testing.T) {
	// This test used to drive `print([1, 2, 3][-1])`, which panicked in codegen; ADR 0210 fixed
	// that shape, so the fixture is a program whose compiled leg still fails while the
	// interpreter and CPython answer — iterating a string computed at run time (Gap R.16).
	// What the test is actually about has not changed: a leg that fails violently must be
	// *recorded* — the CLI stays alive, the exit code stays a verdict class, and the reason is
	// readable in the payload. The panic classification itself is pinned where it can be pinned
	// deterministically, in pkg/lang/oracle_test.go.
	// str * int still refuses on the compiled path (roadmap Gap R.33); the runtime-string
	// loop that used to stand here has been answerable since ADR 0229.
	src := "print(\"ab\" * 2)\n"
	out, code := benchCLI(t, "--json", "--oracle", src)
	if code != exitOracleDivergence && code != exitOracleNoVerdict {
		t.Fatalf("--oracle on a refused compiled leg: exit = %d, want 6 or 7\n%s", code, out)
	}
	var p oraclePayload
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("payload: %v\n%s", err, out)
	}
	if p.Legs[1].OK {
		t.Fatalf("the compiled leg should have failed: %+v", p.Legs[1])
	}
	if !strings.Contains(p.Legs[1].Error, "not supported") {
		t.Errorf("the refusal should be named in the leg error, got %q", p.Legs[1].Error)
	}
}

func TestCLIOracleUsageErrors(t *testing.T) {
	// --oracle with an unreadable file is a usage error (4), not a compile error (1).
	if _, code := benchCLI(t, "--oracle-file", "does/not/exist.gy"); code != exitUsage {
		t.Errorf("--oracle-file on a missing file: exit = %d, want %d", code, exitUsage)
	}
}

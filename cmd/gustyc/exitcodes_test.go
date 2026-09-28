package main

import (
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// The exit-code table in docs/operations.md is the CLI's ABI for scripts. These tests
// pin the classification rule itself; TestCLIExitCodeContract (integration) drives the
// real binary through the same table.

func TestBuildExitCodeClassifiesVerifierRejection(t *testing.T) {
	cases := []struct {
		name string
		res  *lang.BuildResult
		want int
	}{
		{
			"nil result is a compile error",
			nil,
			exitCompileError,
		},
		{
			"source diagnostics are a compile error",
			&lang.BuildResult{Diagnostics: []lang.Diagnostic{{Level: lang.LevelError, Msg: "undefined name"}}},
			exitCompileError,
		},
		{
			"LLVM rejecting our module is a compiler bug",
			&lang.BuildResult{Verification: &lang.IRVerification{OK: false, Skipped: false, Tool: "opt-20"}},
			exitIRVerify,
		},
		{
			"a missing toolchain is not a rejection", // skipped must never read as "broken"
			&lang.BuildResult{Verification: &lang.IRVerification{OK: false, Skipped: true, Note: "no LLVM toolchain"}},
			exitCompileError,
		},
		{
			"a verified build does not fail at all",
			&lang.BuildResult{Verification: &lang.IRVerification{OK: true, Tool: "opt-20"}},
			exitCompileError, // only reached when the build failed for another reason
		},
	}
	for _, tc := range cases {
		if got := buildExitCode(tc.res); got != tc.want {
			t.Errorf("%s: buildExitCode = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestExitCodesAreDistinctAndDocumented guards against collapsing two classes back into
// one code, which is exactly what made the table unusable (roadmap Gap J.3).
func TestExitCodesAreDistinctAndDocumented(t *testing.T) {
	seen := map[int]string{}
	for code, name := range map[int]string{
		exitOK:              "exitOK",
		exitCompileError:    "exitCompileError",
		exitIRVerify:        "exitIRVerify",
		exitRuntime:         "exitRuntime",
		exitUsage:           "exitUsage",
		exitBenchRegression: "exitBenchRegression",
		// The oracle leg (L11.9, ADR 0186): "gusty printed something else" and "the
		// oracle could not judge" are each their own class, and neither may share a
		// code with "the program is broken".
		exitOracleDivergence: "exitOracleDivergence",
		exitOracleNoVerdict:  "exitOracleNoVerdict",
	} {
		if prev, dup := seen[code]; dup {
			t.Errorf("exit codes collide: %s and %s are both %d", prev, name, code)
		}
		seen[code] = name
	}
	if exitCompileError == exitRuntime || exitUsage == exitIRVerify {
		t.Error("the failure classes must not share a code")
	}
	// Every verdict maps to a code, and the mapping is what docs/operations.md says.
	for status, want := range map[string]int{
		lang.OracleMatch: exitOK,
		lang.OracleDebt:  exitOracleDivergence,
		lang.OracleNA:    exitOracleNoVerdict,
		"gibberish":      exitOracleNoVerdict, // an unknown verdict is never success
	} {
		if got := oracleExit(status); got != want {
			t.Errorf("oracleExit(%q) = %d, want %d", status, got, want)
		}
	}
	if oracleExit(lang.OracleDebt) == exitOK || oracleExit(lang.OracleNA) == exitOK {
		t.Error("no verdict other than `match` may exit 0 — that is how 'we never checked' becomes 'it works'")
	}
}

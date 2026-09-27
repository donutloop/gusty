package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// Strings across a function boundary: interpreter-only today (Gap I.2 / J.5).
//
// A string in the AOT backend is an `i8*` constant, while a parameter slot is an
// `i32`, so `greet("ada")` used to lower to `call i32 @greet(i32 @.str1)` — IR LLVM
// rejects ("global variable reference must have pointer type"). Codegen now refuses
// the case with a message that names the parameter and points at the backend that
// works, instead of emitting IR that only the verifier would notice.

const stringArgProgram = `def greet(name):
    print("hello", name)
    return 1

print(greet("ada"))
`

func TestStringArgumentRunsOnTheInterpreter(t *testing.T) {
	if got := runInterp(t, stringArgProgram); got != "hello ada\n1\n" {
		t.Errorf("interpreter = %q, want %q", got, "hello ada\n1\n")
	}
}

func TestStringArgumentIsADiagnosticNotBadIR(t *testing.T) {
	res, err := lang.Compile(stringArgProgram)
	if err == nil {
		t.Fatalf("expected a compile diagnostic; got IR:\n%s", res.IR)
	}
	if res != nil && res.IR != "" {
		t.Errorf("no IR should be produced for the unsupported case:\n%s", res.IR)
	}
	msg := err.Error()
	for _, want := range []string{
		"strings are not supported as function arguments",
		`parameter "name" of greet`,
		"the interpreter supports them",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q should contain %q", msg, want)
		}
	}
}

// The same message reaches the CLI's machine path unchanged, so an agent can match
// on it rather than on an llc error.
func TestStringArgumentDiagnosticReachesTheCLI(t *testing.T) {
	_, err := lang.Compile(stringArgProgram)
	if err == nil {
		t.Fatalf("expected a compile diagnostic")
	}
	// --emit-llvm --json reports it as {"ok": false, "phase": "compile", ...}.
	out := cliRun(t, "--json", "--emit-llvm", stringArgProgram)
	for _, want := range []string{`"ok": false`, `"phase": "compile"`, "strings are not supported as function arguments"} {
		if !strings.Contains(out, want) {
			t.Errorf("CLI json %s should contain %q", out, want)
		}
	}
}

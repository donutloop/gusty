package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/donutloop/gusty/pkg/lang"
)

// A module that calls a runtime helper it never defines is a compiler bug under the exit-code
// contract, and which helpers a module calls used to be tracked by flags that individual codegen paths
// had to remember to set. Emission is now derived from the emitted code: reference a name a runtime
// block defines and the block comes along (roadmap Gap R.2, ADR 0209).

// TestR2ReproCompilesAndRuns is R.2 as the roadmap wrote it — `await` plus `while True: return "ok"` —
// which used to fail llc with `use of undefined value '@rt_str_intern2'`. The async leg still prints
// the wrong thing (that is Gap R.1, compiled coroutines running eagerly), and this test says so
// explicitly rather than pretending: what must be true now is that the module compiles and runs.
func TestR2ReproCompilesAndRuns(t *testing.T) {
	src := `async def f(x):
    return x + 1


async def g():
    await f(1)
    while True:
        return "ok"


print(await g())
`
	interpreted := runInterp(t, src)
	if strings.TrimSpace(interpreted) != "ok" {
		t.Errorf("interpreted output = %q, want ok", interpreted)
	}
	compiled, err := runAOTWithTimeout(t, src, 120*time.Second)
	if err != nil {
		t.Fatalf("the async repro still fails to build: %v", err)
	}
	if strings.Contains(compiled, "rt_str_intern2") || strings.Contains(compiled, "llc") {
		t.Errorf("the compiled leg reported a toolchain failure as output: %q", compiled)
	}
	if strings.TrimSpace(compiled) == "ok" {
		t.Skip("compiled coroutine now matches the interpreter (Gap R.1 closed?) — update roadmap R.1")
	}
}

// TestStringReturningFunctionRunsEverywhere is the await-free repro that re-scoped the gap: the same
// source printed `h i` on the interpreter and in CPython while the compiled leg died in llc.
func TestStringReturningFunctionRunsEverywhere(t *testing.T) {
	src := `def txt():
    return "hi"


print(txt())
`
	want := "hi\n"
	if got := runInterp(t, src); got != want {
		t.Errorf("interpreted output =\n%q\nwant\n%q", got, want)
	}
	built, err := runAOTWithTimeout(t, src, 90*time.Second)
	if err != nil {
		t.Fatalf("compiled run: %v", err)
	}
	if built != want {
		t.Errorf("compiled output =\n%q\nwant\n%q", built, want)
	}
	pyOut, pyErr, perr := lang.PythonRun(src)
	if perr != nil {
		t.Skipf("no usable oracle: %v\n%s", perr, pyErr)
	}
	if pyOut != want {
		t.Errorf("CPython output =\n%q\nwant\n%q", pyOut, want)
	}
}

// TestRuntimeStringIterableIsRefusedNotSilentlyWrong pins the refusal that replaced a clean compile
// that printed nothing: a function's interned index read as a repeat count (roadmap Gap R.16).
func TestRuntimeStringIterableIsRefusedNotSilentlyWrong(t *testing.T) {
	src := "def txt():\n    return \"hi\"\n\nfor c in txt():\n    print(c)\n"
	if got := runInterp(t, src); got != "h\ni\n" {
		t.Errorf("interpreted output =\n%q\nwant h i", got)
	}
	_, err := lang.Compile(src)
	if err == nil {
		t.Fatalf("the compiled backend accepted a program whose loop cannot run: it must refuse")
	}
	if !strings.Contains(err.Error(), "iterating a string computed at run time") {
		t.Errorf("the refusal is not the actionable one: %v", err)
	}
}
